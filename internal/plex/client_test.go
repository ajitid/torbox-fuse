package plex

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

func TestRefreshMountPathsTokenEmptyDoesNotRequest(t *testing.T) {
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
	}))
	defer server.Close()

	New(server.URL, "", nil).RefreshMountPaths(context.Background(), "/srv/torbox")

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

	New(server.URL, token, nil).RefreshMountPaths(context.Background(), "/srv/torbox")

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

func TestRefreshMountPathsMissingMatchingSectionDoesNotRefreshAndLogs(t *testing.T) {
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
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	New(server.URL, "token", logger).RefreshMountPaths(context.Background(), "/srv/torbox")

	if got := atomic.LoadInt32(&refreshes); got != 0 {
		t.Fatalf("refreshes = %d, want 0", got)
	}
	if got := buf.String(); !strings.Contains(got, "no matching sections") {
		t.Fatalf("log = %q, want no matching sections message", got)
	}
}

func TestRefreshMountPathsPlexErrorsAreLoggedButNonFatal(t *testing.T) {
	t.Run("sections 500", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusInternalServerError)
		}))
		defer server.Close()
		var buf bytes.Buffer
		logger := log.New(&buf, "", 0)

		New(server.URL, "token", logger).RefreshMountPaths(context.Background(), "/srv/torbox")

		got := buf.String()
		if !strings.Contains(got, "plex library refresh failed") {
			t.Fatalf("log = %q, want failure message", got)
		}
		if !strings.Contains(got, "500") && !strings.Contains(got, "Internal Server Error") {
			t.Fatalf("log = %q, want status", got)
		}
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
		var buf bytes.Buffer
		logger := log.New(&buf, "", 0)

		New(server.URL, "token", logger).RefreshMountPaths(context.Background(), "/srv/torbox")

		got := buf.String()
		if !strings.Contains(got, "plex library refresh failed") {
			t.Fatalf("log = %q, want failure message", got)
		}
		if !strings.Contains(got, "500") && !strings.Contains(got, "Internal Server Error") {
			t.Fatalf("log = %q, want status", got)
		}
	})
}

func TestRefreshMountPathsInvalidBaseURLLogsError(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	New("://bad", "token", logger).RefreshMountPaths(context.Background(), "/srv/torbox")

	got := buf.String()
	if !strings.Contains(got, "invalid base url") {
		t.Fatalf("log = %q, want invalid base url message", got)
	}
}
