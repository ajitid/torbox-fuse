package jellyfin

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestNotifyMountPathsAPIKeyEmptyDoesNotRequest(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
	}))
	defer server.Close()

	New(server.URL, "", nil).NotifyMountPaths(context.Background(), "/srv/torbox")

	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}

func TestNotifyMountPathsDiscoversVirtualFoldersAndRefreshesExactMatches(t *testing.T) {
	const token = "jellyfin-token"
	var mu sync.Mutex
	refreshes := map[string]map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Emby-Token"); got != token {
			t.Errorf("X-Emby-Token = %q, want %q", got, token)
		}
		switch r.URL.Path {
		case "/Library/VirtualFolders":
			if got := r.Method; got != http.MethodGet {
				t.Errorf("method = %q, want %q", got, http.MethodGet)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[
	{"Name":"Movies","Locations":["/srv/torbox/movies","/mnt/local/movies"],"CollectionType":"movies","ItemId":"movie-lib"},
	{"Name":"Shows","Locations":["/srv/torbox/series"],"CollectionType":"tvshows","ItemId":"series-lib"},
	{"Name":"Music","Locations":["/mnt/local/music"],"CollectionType":"music","ItemId":"music-lib"}
]`))
		case "/Items/movie-lib/Refresh", "/Items/series-lib/Refresh", "/Items/music-lib/Refresh":
			if got := r.Method; got != http.MethodPost {
				t.Errorf("method = %q, want %q", got, http.MethodPost)
			}
			mu.Lock()
			refreshes[r.URL.Path] = map[string]string{
				"Recursive":           r.URL.Query().Get("Recursive"),
				"ImageRefreshMode":    r.URL.Query().Get("ImageRefreshMode"),
				"MetadataRefreshMode": r.URL.Query().Get("MetadataRefreshMode"),
				"ReplaceAllImages":    r.URL.Query().Get("ReplaceAllImages"),
				"RegenerateTrickplay": r.URL.Query().Get("RegenerateTrickplay"),
				"ReplaceAllMetadata":  r.URL.Query().Get("ReplaceAllMetadata"),
			}
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	New(server.URL, token, nil).NotifyMountPaths(context.Background(), "/srv/torbox")

	mu.Lock()
	defer mu.Unlock()
	wantQuery := map[string]string{
		"Recursive":           "true",
		"ImageRefreshMode":    "Default",
		"MetadataRefreshMode": "Default",
		"ReplaceAllImages":    "false",
		"RegenerateTrickplay": "false",
		"ReplaceAllMetadata":  "false",
	}
	want := map[string]map[string]string{
		"/Items/movie-lib/Refresh":  wantQuery,
		"/Items/series-lib/Refresh": wantQuery,
	}
	if !reflect.DeepEqual(refreshes, want) {
		t.Fatalf("refreshes = %#v, want %#v", refreshes, want)
	}
}

func TestNotifyMountPathsDeduplicatesMatchedVirtualFolders(t *testing.T) {
	var refreshes int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Library/VirtualFolders":
			_, _ = w.Write([]byte(`[{"Name":"TorBox","Locations":["/srv/torbox/movies","/srv/torbox/series"],"ItemId":"torbox-lib"}]`))
		case "/Items/torbox-lib/Refresh":
			atomic.AddInt32(&refreshes, 1)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	New(server.URL, "token", nil).NotifyMountPaths(context.Background(), "/srv/torbox")

	if got := atomic.LoadInt32(&refreshes); got != 1 {
		t.Fatalf("refreshes = %d, want 1", got)
	}
}

func TestNotifyMountPathsLogsWhenNoVirtualFoldersMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Library/VirtualFolders" {
			t.Errorf("unexpected request path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`[{"Name":"Music","Locations":["/mnt/local/music"],"ItemId":"music-lib"}]`))
	}))
	defer server.Close()
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	New(server.URL, "token", logger).NotifyMountPaths(context.Background(), "/srv/torbox")

	if got := buf.String(); !strings.Contains(got, "no matching virtual folders") {
		t.Fatalf("log = %q, want no matching virtual folders message", got)
	}
}

func TestNotifyMountPathsLogsErrorsButDoesNotPanicOrReturn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Library/VirtualFolders":
			_, _ = w.Write([]byte(`[{"Name":"Movies","Locations":["/srv/torbox/movies"],"ItemId":"movie-lib"}]`))
		case "/Items/movie-lib/Refresh":
			http.Error(w, "nope", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	New(server.URL, "token", logger).NotifyMountPaths(context.Background(), "/srv/torbox")

	got := buf.String()
	if !strings.Contains(got, "jellyfin library refresh failed") {
		t.Fatalf("log = %q, want failure message", got)
	}
	if !strings.Contains(got, "500") && !strings.Contains(got, "Internal Server Error") {
		t.Fatalf("log = %q, want status", got)
	}
}

func TestNotifyMountPathsInvalidBaseURLLogsError(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	New("://bad", "token", logger).NotifyMountPaths(context.Background(), "/srv/torbox")

	got := buf.String()
	if !strings.Contains(got, "invalid base url") {
		t.Fatalf("log = %q, want invalid base url message", got)
	}
}
