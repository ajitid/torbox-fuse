package fusefs

import (
	"context"
	"hash/fnv"
	"log"
	"path"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/TorBox-App/torbox-fuse/internal/cache"
	"github.com/TorBox-App/torbox-fuse/internal/media"
	"github.com/TorBox-App/torbox-fuse/internal/torbox"
	"github.com/TorBox-App/torbox-fuse/internal/vfs"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

type FS struct {
	fs.Inode
	tree     atomic.Value
	torbox   *torbox.Client
	resolver *URLResolver
	cache    *cache.BlockCache
}

func New(records []media.FileRecord, c *torbox.Client, bc *cache.BlockCache) *FS {
	f := &FS{torbox: c, resolver: NewURLResolver(c), cache: bc}
	f.tree.Store(vfs.Build(records))
	return f
}
func (f *FS) Swap(records []media.FileRecord) { f.tree.Store(vfs.Build(records)) }
func (f *FS) current() *vfs.Tree              { return f.tree.Load().(*vfs.Tree) }
func (f *FS) Mount(mountPath string, allowOther bool, logger *log.Logger) (*fuse.Server, error) {
	d := 2 * time.Second
	return fs.Mount(mountPath, f, &fs.Options{MountOptions: fuse.MountOptions{AllowOther: allowOther, Options: []string{"ro", "fsname=torbox-fuse"}}, EntryTimeout: &d, AttrTimeout: &d, Logger: logger})
}

var _ fs.NodeReaddirer = (*FS)(nil)
var _ fs.NodeLookuper = (*FS)(nil)
var _ fs.NodeGetattrer = (*FS)(nil)

func (f *FS) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	return listDir(f.current(), "/"), 0
}
func (f *FS) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return f.lookup(ctx, "/", name, out)
}
func (f *FS) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = fuse.S_IFDIR | 0555
	return 0
}
func (f *FS) lookup(ctx context.Context, parent, name string, entry *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	p := clean(path.Join(parent, name))
	t := f.current()
	if rec, ok := t.GetFile(p); ok {
		if entry != nil {
			entry.Attr.Mode = fuse.S_IFREG | 0444
			entry.Attr.Size = fuseSize(rec.FileSize)
		}
		out := &fileNode{root: f, path: p, rec: rec}
		return f.NewInode(ctx, out, fs.StableAttr{Mode: fuse.S_IFREG, Ino: ino(p)}), 0
	}
	if t.IsDir(p) {
		if entry != nil {
			entry.Attr.Mode = fuse.S_IFDIR | 0555
		}
		out := &dirNode{root: f, path: p}
		return f.NewInode(ctx, out, fs.StableAttr{Mode: fuse.S_IFDIR, Ino: ino(p)}), 0
	}
	return nil, syscall.ENOENT
}

type dirNode struct {
	fs.Inode
	root *FS
	path string
}

var _ fs.NodeReaddirer = (*dirNode)(nil)
var _ fs.NodeLookuper = (*dirNode)(nil)
var _ fs.NodeGetattrer = (*dirNode)(nil)

func (d *dirNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	return listDir(d.root.current(), d.path), 0
}
func (d *dirNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	return d.root.lookup(ctx, d.path, name, out)
}
func (d *dirNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = fuse.S_IFDIR | 0555
	return 0
}

type fileNode struct {
	fs.Inode
	root *FS
	path string
	rec  media.FileRecord
}

var _ fs.NodeOpener = (*fileNode)(nil)
var _ fs.NodeGetattrer = (*fileNode)(nil)

func (n *fileNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	if flags&(syscall.O_RDWR|syscall.O_WRONLY) != 0 {
		return nil, 0, syscall.EROFS
	}
	if rec, ok := n.root.current().GetFile(n.path); ok {
		n.rec = rec
	}
	return &fileHandle{root: n.root, rec: n.rec}, fuse.FOPEN_DIRECT_IO, 0
}
func (n *fileNode) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if rec, ok := n.root.current().GetFile(n.path); ok {
		n.rec = rec
	}
	out.Mode = fuse.S_IFREG | 0444
	out.Size = fuseSize(n.rec.FileSize)
	return 0
}

type fileHandle struct {
	root *FS
	rec  media.FileRecord
}

var _ fs.FileReader = (*fileHandle)(nil)

func (h *fileHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if h.rec.FileSize > 0 && off >= h.rec.FileSize {
		return fuse.ReadResultData(nil), 0
	}
	size := len(dest)
	if h.rec.FileSize > 0 {
		if remain := h.rec.FileSize - off; int64(size) > remain {
			size = int(remain)
		}
	}
	fetch := func(ctx context.Context, rangeOff int64, rangeSize int) ([]byte, int, error) {
		u, err := h.root.resolver.Resolve(ctx, h.rec)
		if err != nil {
			return nil, 0, err
		}
		return h.root.torbox.ReadRange(ctx, u, rangeOff, rangeSize)
	}
	b, status, err := h.root.cache.Read(ctx, h.rec, off, size, fetch)
	if err != nil && expiryStatus(status) {
		h.root.resolver.Invalidate(h.rec)
		b, _, err = h.root.cache.Read(ctx, h.rec, off, size, fetch)
	}
	if err != nil {
		return nil, syscall.EIO
	}
	return fuse.ReadResultData(b), 0
}

func listDir(t *vfs.Tree, p string) fs.DirStream {
	ents := t.ListDir(p)
	out := make([]fuse.DirEntry, 0, len(ents))
	for _, e := range ents {
		mode := uint32(fuse.S_IFREG)
		if e.IsDir {
			mode = fuse.S_IFDIR
		}
		out = append(out, fuse.DirEntry{Name: e.Name, Mode: mode, Ino: ino(path.Join(p, e.Name))})
	}
	return fs.NewListDirStream(out)
}
func clean(p string) string {
	p = path.Clean("/" + strings.TrimPrefix(p, "/"))
	if p == "." {
		return "/"
	}
	return p
}
func ino(p string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(clean(p)))
	v := h.Sum64()
	if v < 2 {
		v += 2
	}
	return v
}

func fuseSize(size int64) uint64 {
	if size > 0 {
		return uint64(size)
	}
	// TorBox can report 0 for remote file sizes. FUSE has no unknown-size
	// regular file, so expose a large sparse-looking size while Read keeps
	// fetching ranges until the HTTP source ends/errors.
	return 1 << 40
}
