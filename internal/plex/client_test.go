package plex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRefreshMountPathsTokenEmptyDoesNotRequest(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
	}))
	defer server.Close()

	New(server.URL, "").RefreshMountPaths(context.Background(), "/srv/torbox")

	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}

func TestRefreshMountPathsDiscoversSectionsAndRefreshesExactMatches(t *testing.T) {
	const token = "plex-token"
	var mu sync.Mutex
	refreshes := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("X-Plex-Token"); got != token {
			t.Errorf("token = %q, want %q", got, token)
		}
		switch r.URL.Path {
		case "/library/sections":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(`<MediaContainer>
	<Directory key="3" title="Movies"><Location path="/srv/torbox/movies"/><Location path="/mnt/local/movies"/></Directory>
	<Directory key="4" title="TV Shows"><Location path="/srv/torbox/series"/></Directory>
	<Directory key="5" title="Music"><Location path="/mnt/local/music"/></Directory>
</MediaContainer>`))
		case "/library/sections/3/refresh", "/library/sections/4/refresh", "/library/sections/5/refresh":
			mu.Lock()
			refreshes[r.URL.Path] = r.URL.Query().Get("path")
			mu.Unlock()
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	New(server.URL, token).RefreshMountPaths(context.Background(), "/srv/torbox")

	mu.Lock()
	defer mu.Unlock()
	want := map[string]string{
		"/library/sections/3/refresh": "/srv/torbox/movies",
		"/library/sections/4/refresh": "/srv/torbox/series",
	}
	if !reflect.DeepEqual(refreshes, want) {
		t.Fatalf("refreshes = %#v, want %#v", refreshes, want)
	}
}

func TestRefreshMountPathsMissingMatchingSectionDoesNotRefresh(t *testing.T) {
	var refreshes int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library/sections":
			_, _ = w.Write([]byte(`<MediaContainer><Directory key="5" title="Music"><Location path="/mnt/local/music"/></Directory></MediaContainer>`))
		case "/library/sections/5/refresh":
			atomic.AddInt32(&refreshes, 1)
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	New(server.URL, "token").RefreshMountPaths(context.Background(), "/srv/torbox")

	if got := atomic.LoadInt32(&refreshes); got != 0 {
		t.Fatalf("refreshes = %d, want 0", got)
	}
}

func TestRefreshMountPathsPlexErrorsAreSilent(t *testing.T) {
	t.Run("sections 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusInternalServerError)
		}))
		defer server.Close()

		New(server.URL, "token").RefreshMountPaths(context.Background(), "/srv/torbox")
	})

	t.Run("refresh 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/library/sections":
				_, _ = w.Write([]byte(`<MediaContainer><Directory key="3" title="Movies"><Location path="/srv/torbox/movies"/></Directory></MediaContainer>`))
			case "/library/sections/3/refresh":
				http.Error(w, "nope", http.StatusInternalServerError)
			default:
				t.Errorf("unexpected request path %s", r.URL.Path)
			}
		}))
		defer server.Close()

		New(server.URL, "token").RefreshMountPaths(context.Background(), "/srv/torbox")
	})
}
