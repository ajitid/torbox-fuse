package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/TorBox-App/torbox-rclone/internal/media"
)

const DefaultBlockSize int64 = 4 * 1024 * 1024

// FetchFunc fetches exactly size bytes at off when available. It returns the HTTP
// status observed by the caller so auth/expiry failures can invalidate resolved URLs.
type FetchFunc func(ctx context.Context, off int64, size int) ([]byte, int, error)

type BlockCache struct {
	dir       string
	maxBytes  int64
	readAhead int64
	blockSize int64

	mu          sync.Mutex
	entries     map[string]*entry
	used        int64
	inflight    map[string]*call
	lastEnd     map[string]int64
	prefetchGen map[string]uint64
	prefetchSem chan struct{}
}

type entry struct {
	path  string
	size  int64
	atime time.Time
}

type call struct {
	done   chan struct{}
	data   []byte
	status int
	err    error
}

func New(dir string, maxBytes, readAhead int64) (*BlockCache, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("cache size must be positive")
	}
	if readAhead < 0 {
		return nil, fmt.Errorf("read ahead must not be negative")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	c := &BlockCache{dir: dir, maxBytes: maxBytes, readAhead: readAhead, blockSize: DefaultBlockSize, entries: map[string]*entry{}, inflight: map[string]*call{}, lastEnd: map[string]int64{}, prefetchGen: map[string]uint64{}, prefetchSem: make(chan struct{}, 2)}
	if err := c.scan(); err != nil {
		return nil, err
	}
	c.evictLocked()
	return c, nil
}

func (c *BlockCache) Read(ctx context.Context, rec media.FileRecord, off int64, size int, fetch FetchFunc) ([]byte, int, error) {
	if size <= 0 {
		return nil, 0, nil
	}
	if rec.FileSize > 0 {
		if off >= rec.FileSize {
			return nil, 0, nil
		}
		if remain := rec.FileSize - off; int64(size) > remain {
			size = int(remain)
		}
	}
	out := make([]byte, 0, size)
	end := off + int64(size)
	for pos := off; pos < end; {
		blockNo := pos / c.blockSize
		blockStart := blockNo * c.blockSize
		block, status, err := c.getBlock(ctx, rec, blockNo, fetch)
		if err != nil {
			return nil, status, err
		}
		startInBlock := pos - blockStart
		if startInBlock >= int64(len(block)) {
			break
		}
		n := minInt64(int64(len(block))-startInBlock, end-pos)
		out = append(out, block[startInBlock:startInBlock+n]...)
		pos += n
		if n == 0 || int64(len(block)) < c.blockSize {
			break
		}
	}
	c.maybePrefetch(rec, off, int64(len(out)), fetch)
	return out, 0, nil
}

func (c *BlockCache) getBlock(ctx context.Context, rec media.FileRecord, blockNo int64, fetch FetchFunc) ([]byte, int, error) {
	key := c.blockKey(rec, blockNo)
	if b, ok := c.readCached(key); ok {
		return b, 0, nil
	}

	c.mu.Lock()
	if existing := c.inflight[key]; existing != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-existing.done:
			return existing.data, existing.status, existing.err
		}
	}
	cl := &call{done: make(chan struct{})}
	c.inflight[key] = cl
	c.mu.Unlock()

	cl.data, cl.status, cl.err = c.fetchBlock(ctx, rec, blockNo, fetch)
	close(cl.done)
	c.mu.Lock()
	delete(c.inflight, key)
	c.mu.Unlock()
	return cl.data, cl.status, cl.err
}

func (c *BlockCache) fetchBlock(ctx context.Context, rec media.FileRecord, blockNo int64, fetch FetchFunc) ([]byte, int, error) {
	blockStart := blockNo * c.blockSize
	blockLen := c.blockSize
	if rec.FileSize > 0 {
		if blockStart >= rec.FileSize {
			return nil, 0, nil
		}
		blockLen = minInt64(blockLen, rec.FileSize-blockStart)
	}
	b, status, err := fetch(ctx, blockStart, int(blockLen))
	if err != nil {
		return nil, status, err
	}
	if len(b) == 0 {
		return b, status, nil
	}
	key := c.blockKey(rec, blockNo)
	if err := c.writeCached(key, b); err != nil {
		return nil, status, err
	}
	return b, status, nil
}

func (c *BlockCache) maybePrefetch(rec media.FileRecord, off, got int64, fetch FetchFunc) {
	if got <= 0 || c.readAhead <= 0 {
		return
	}
	end := off + got
	fileKey := c.fileKey(rec)
	sequential := off == 0
	c.mu.Lock()
	if c.lastEnd[fileKey] == off {
		sequential = true
	}
	c.lastEnd[fileKey] = end
	if !sequential {
		// A real seek happened. Invalidate queued prefetch work for the old
		// position so interactive seeks are not stuck behind stale read-ahead.
		c.prefetchGen[fileKey]++
		c.mu.Unlock()
		return
	}
	gen := c.prefetchGen[fileKey]
	c.mu.Unlock()
	firstBlock := (end + c.blockSize - 1) / c.blockSize
	blocks := c.readAhead / c.blockSize
	if c.readAhead%c.blockSize != 0 {
		blocks++
	}
	for i := int64(0); i < blocks; i++ {
		blockNo := firstBlock + i
		if rec.FileSize > 0 && blockNo*c.blockSize >= rec.FileSize {
			return
		}
		key := c.blockKey(rec, blockNo)
		if c.hasOrInflight(key) {
			continue
		}
		go func() {
			if !c.sameGeneration(fileKey, gen) {
				return
			}
			select {
			case c.prefetchSem <- struct{}{}:
				defer func() { <-c.prefetchSem }()
			default:
				return
			}
			if !c.sameGeneration(fileKey, gen) {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			_, _, _ = c.getBlock(ctx, rec, blockNo, fetch)
		}()
	}
}

func (c *BlockCache) readCached(key string) ([]byte, bool) {
	c.mu.Lock()
	e := c.entries[key]
	if e != nil {
		e.atime = time.Now()
	}
	c.mu.Unlock()
	if e == nil {
		return nil, false
	}
	b, err := os.ReadFile(e.path)
	if err != nil {
		c.mu.Lock()
		c.removeLocked(key)
		c.mu.Unlock()
		return nil, false
	}
	return b, true
}

func (c *BlockCache) writeCached(key string, b []byte) error {
	p := filepath.Join(c.dir, key+".blk")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	c.mu.Lock()
	if old := c.entries[key]; old != nil {
		c.used -= old.size
	}
	c.entries[key] = &entry{path: p, size: int64(len(b)), atime: time.Now()}
	c.used += int64(len(b))
	c.evictLocked()
	c.mu.Unlock()
	return nil
}

func (c *BlockCache) hasOrInflight(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries[key] != nil || c.inflight[key] != nil
}

func (c *BlockCache) scan() error {
	return filepath.WalkDir(c.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(p) != ".blk" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		key := filepath.Base(p[:len(p)-len(filepath.Ext(p))])
		c.entries[key] = &entry{path: p, size: info.Size(), atime: info.ModTime()}
		c.used += info.Size()
		return nil
	})
}

func (c *BlockCache) evictLocked() {
	if c.used <= c.maxBytes {
		return
	}
	keys := make([]string, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return c.entries[keys[i]].atime.Before(c.entries[keys[j]].atime) })
	for _, k := range keys {
		if c.used <= c.maxBytes {
			return
		}
		c.removeLocked(k)
	}
}

func (c *BlockCache) removeLocked(key string) {
	e := c.entries[key]
	if e == nil {
		return
	}
	_ = os.Remove(e.path)
	c.used -= e.size
	delete(c.entries, key)
}

func (c *BlockCache) sameGeneration(fileKey string, gen uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.prefetchGen[fileKey] == gen
}

func (c *BlockCache) blockKey(rec media.FileRecord, blockNo int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", c.fileKey(rec), blockNo)))
	return hex.EncodeToString(sum[:])
}

func (c *BlockCache) fileKey(rec media.FileRecord) string {
	if rec.Key != "" {
		return rec.Key
	}
	return rec.DownloadLink
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
