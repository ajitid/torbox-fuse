package jellyfin

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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

func TestNotifyMountPathsPostsMediaUpdatedPayload(t *testing.T) {
	const token = "jellyfin-token"
	var gotBody mediaUpdatedRequest
	var requests int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if got := r.Method; got != http.MethodPost {
			t.Errorf("method = %q, want %q", got, http.MethodPost)
		}
		if got := r.URL.Path; got != "/Library/Media/Updated" {
			t.Errorf("path = %q, want %q", got, "/Library/Media/Updated")
		}
		if got := r.Header.Get("X-Emby-Token"); got != token {
			t.Errorf("X-Emby-Token = %q, want %q", got, token)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	New(server.URL, token, nil).NotifyMountPaths(context.Background(), "/srv/torbox")

	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	want := mediaUpdatedRequest{Updates: []mediaUpdate{
		{Path: "/srv/torbox/movies", UpdateType: "Modified"},
		{Path: "/srv/torbox/series", UpdateType: "Modified"},
	}}
	if !reflect.DeepEqual(gotBody, want) {
		t.Fatalf("body = %#v, want %#v", gotBody, want)
	}
}

func TestNotifyMountPathsLogsErrorsButDoesNotPanicOrReturn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	New(server.URL, "token", logger).NotifyMountPaths(context.Background(), "/srv/torbox")

	got := buf.String()
	if !strings.Contains(got, "jellyfin media update notify failed") {
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
