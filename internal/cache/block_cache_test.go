package cache

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TorBox-App/torbox-rclone/internal/media"
)

func TestReadComposesBlocksAndHitsCache(t *testing.T) {
	c, err := New(t.TempDir(), 1024, 0)
	if err != nil {
		t.Fatal(err)
	}
	c.blockSize = 4
	rec := media.FileRecord{Key: "file", FileSize: 10}
	data := []byte("0123456789")
	var calls int32
	fetch := func(ctx context.Context, off int64, size int) ([]byte, int, error) {
		atomic.AddInt32(&calls, 1)
		return append([]byte(nil), data[off:off+int64(size)]...), 206, nil
	}
	got, _, err := c.Read(context.Background(), rec, 2, 6, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "234567" {
		t.Fatalf("got %q", got)
	}
	if calls != 2 {
		t.Fatalf("fetch calls = %d, want 2", calls)
	}
	got, _, err = c.Read(context.Background(), rec, 1, 2, fetch)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "12" {
		t.Fatalf("got %q", got)
	}
	if calls != 2 {
		t.Fatalf("cache miss: calls = %d, want 2", calls)
	}
}

func TestEvictsLeastRecentlyUsed(t *testing.T) {
	c, err := New(t.TempDir(), 8, 0)
	if err != nil {
		t.Fatal(err)
	}
	c.blockSize = 4
	rec := media.FileRecord{Key: "file", FileSize: 12}
	data := []byte("abcdefghijkl")
	fetch := func(ctx context.Context, off int64, size int) ([]byte, int, error) {
		return append([]byte(nil), data[off:off+int64(size)]...), 206, nil
	}
	if _, _, err := c.Read(context.Background(), rec, 0, 4, fetch); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, _, err := c.Read(context.Background(), rec, 4, 4, fetch); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, _, err := c.Read(context.Background(), rec, 8, 4, fetch); err != nil {
		t.Fatal(err)
	}
	if c.used > 8 {
		t.Fatalf("used = %d, want <= 8", c.used)
	}
	if _, ok := c.readCached(c.blockKey(rec, 0)); ok {
		t.Fatalf("oldest block was not evicted")
	}
}

func TestDuplicateConcurrentFetchIsShared(t *testing.T) {
	c, err := New(t.TempDir(), 1024, 0)
	if err != nil {
		t.Fatal(err)
	}
	c.blockSize = 4
	rec := media.FileRecord{Key: "file", FileSize: 4}
	var calls int32
	fetch := func(ctx context.Context, off int64, size int) ([]byte, int, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond)
		return bytes.Repeat([]byte{'x'}, size), 206, nil
	}
	done := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			got, _, err := c.Read(context.Background(), rec, 0, 4, fetch)
			if err == nil && string(got) != "xxxx" {
				err = context.Canceled
			}
			done <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("fetch calls = %d, want 1", calls)
	}
}
