package refresh

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/TorBox-App/torbox-fuse/internal/media"
	"github.com/TorBox-App/torbox-fuse/internal/store"
	"github.com/TorBox-App/torbox-fuse/internal/torbox"
)

type libraryServer struct {
	mu       sync.Mutex
	items    map[torbox.DownloadType][]torbox.Item
	requests []string
	fail     bool
}

func (s *libraryServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	typ := torbox.DownloadType(parts[0])
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	s.requests = append(s.requests, string(typ)+":"+strconv.Itoa(limit)+":"+strconv.Itoa(offset))
	items := s.items[typ]
	if offset > len(items) {
		offset = len(items)
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	data := make([]map[string]any, 0, end-offset)
	for _, item := range items[offset:end] {
		data = append(data, map[string]any{"id": item.ID, "name": item.Name, "cached": item.Cached, "files": []map[string]any{{"id": item.ID + "f", "name": "Movie.2024.mkv", "short_name": "Movie.2024.mkv", "size": 1, "mimetype": "video/x-matroska"}}})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func TestPollDetectsChangesAndPreservesBaselines(t *testing.T) {
	s := &libraryServer{items: map[torbox.DownloadType][]torbox.Item{}}
	for _, typ := range torbox.AllDownloadTypes() {
		s.items[typ] = testItems(6, true)
	}
	srv := httptest.NewServer(http.HandlerFunc(s.serveHTTP))
	defer srv.Close()
	st, err := store.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	client := torbox.New("key", "test")
	client.SetBaseURL(srv.URL)
	mgr := New(client, st, log.New(io.Discard, "", 0), torbox.AllDownloadTypes())
	if _, err := mgr.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.requests = nil
	s.mu.Unlock()
	changed, err := mgr.Poll(context.Background())
	if err != nil || changed {
		t.Fatalf("unchanged poll = %v, %v", changed, err)
	}
	s.mu.Lock()
	requests := append([]string(nil), s.requests...)
	s.mu.Unlock()
	if len(requests) != 6 {
		t.Fatalf("requests = %v, want head and boundary for every source", requests)
	}
	for _, typ := range torbox.AllDownloadTypes() {
		found := false
		for _, req := range requests {
			found = found || strings.HasPrefix(req, string(typ)+":")
		}
		if !found {
			t.Fatalf("%s was not polled: %v", typ, requests)
		}
	}

	s.mu.Lock()
	s.items[torbox.DownloadTorrent] = append([]torbox.Item{{ID: "new", Cached: true}}, s.items[torbox.DownloadTorrent]...)
	s.mu.Unlock()
	changed, err = mgr.Poll(context.Background())
	if err != nil || !changed {
		t.Fatalf("add poll = %v, %v", changed, err)
	}
	if _, err := mgr.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.items[torbox.DownloadTorrent] = append(s.items[torbox.DownloadTorrent][:3], s.items[torbox.DownloadTorrent][4:]...)
	s.mu.Unlock()
	changed, err = mgr.Poll(context.Background())
	if err != nil || !changed {
		t.Fatalf("middle delete poll = %v, %v", changed, err)
	}
	if _, err := mgr.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	s.mu.Lock()
	s.items[torbox.DownloadUsenet][0].Cached = false
	s.mu.Unlock()
	if _, err := mgr.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.items[torbox.DownloadUsenet][0].Cached = true
	s.mu.Unlock()
	changed, err = mgr.Poll(context.Background())
	if err != nil || !changed {
		t.Fatalf("cache completion poll = %v, %v", changed, err)
	}

	s.mu.Lock()
	s.fail = true
	s.mu.Unlock()
	changed, err = mgr.Poll(context.Background())
	if err == nil || changed {
		t.Fatalf("failed poll = %v, %v", changed, err)
	}
	s.mu.Lock()
	s.fail = false
	s.mu.Unlock()
	changed, err = mgr.Poll(context.Background())
	if err != nil || !changed {
		t.Fatalf("baseline was not preserved: %v, %v", changed, err)
	}
}

func TestVisibleMediaChanged(t *testing.T) {
	movie := media.FileRecord{Key: "movie", MetadataMediaType: "movie", MetadataFileName: "movie.mkv"}
	series := media.FileRecord{Key: "series", MetadataMediaType: "series"}
	unknown := media.FileRecord{Key: "unknown", MetadataMediaType: "unknown"}
	if visibleMediaChanged(nil, []media.FileRecord{unknown}) {
		t.Fatal("unknown-only change was visible")
	}
	if !visibleMediaChanged(nil, []media.FileRecord{movie, series}) {
		t.Fatal("visible addition was missed")
	}
	if !visibleMediaChanged([]media.FileRecord{movie}, nil) {
		t.Fatal("visible removal was missed")
	}
	changed := movie
	changed.MetadataFileName = "renamed.mkv"
	if !visibleMediaChanged([]media.FileRecord{movie}, []media.FileRecord{changed}) {
		t.Fatal("visible mutation was missed")
	}
	if visibleMediaChanged([]media.FileRecord{movie}, []media.FileRecord{movie}) {
		t.Fatal("no-op was visible")
	}
}

func testItems(n int, cached bool) []torbox.Item {
	items := make([]torbox.Item, n)
	for i := range items {
		items[i] = torbox.Item{ID: strconv.Itoa(n - i), Name: "Movie 2024", Cached: cached}
	}
	return items
}
