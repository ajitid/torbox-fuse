package torbox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListDownloadsPaginationAndResolveAndRange(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/torrents/mylist", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("offset") {
		case "0":
			fmt.Fprint(w, `{"data":[{"id":1,"name":"Item","hash":"h","cached":true,"files":[{"id":2,"name":"Item/Movie.mkv","short_name":"Movie.mkv","size":4,"mimetype":"video/x-matroska"}]}]}`)
		default:
			fmt.Fprint(w, `{"data":[]}`)
		}
	})
	mux.HandleFunc("/torrents/requestdl", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/file", http.StatusFound) })
	mux.HandleFunc("/file", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=1-2" {
			t.Fatalf("bad range %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Range", "bytes 1-2/4")
		w.WriteHeader(http.StatusPartialContent)
		fmt.Fprint(w, "bc")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New("key", "test")
	c.SetBaseURL(srv.URL)
	items, err := c.ListDownloads(context.Background(), DownloadTorrent)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "1" || items[0].Files[0].ID != "2" {
		t.Fatalf("unexpected %#v", items)
	}
	u, err := c.ResolveDownloadURL(context.Background(), c.PermanentDownloadURL(DownloadTorrent, items[0], items[0].Files[0]))
	if err != nil {
		t.Fatal(err)
	}
	b, status, err := c.ReadRange(context.Background(), u, 1, 2)
	if err != nil || status != http.StatusPartialContent || string(b) != "bc" {
		t.Fatalf("range %q %d %v", b, status, err)
	}
}

func TestCreateTorrent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/torrents/createtorrent", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Fatalf("authorization = %q", got)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data;") {
			t.Fatalf("content-type = %q", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("magnet") != "magnet:?xt=urn:btih:abc" || r.FormValue("name") != "" {
			t.Fatalf("form magnet=%q name=%q", r.FormValue("magnet"), r.FormValue("name"))
		}
		fmt.Fprint(w, `{"success":true,"data":{"torrent_id":123,"auth_id":"a","hash":"h"}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New("key", "test")
	c.SetBaseURL(srv.URL)
	created, err := c.CreateTorrent(context.Background(), "magnet:?xt=urn:btih:abc")
	if err != nil {
		t.Fatal(err)
	}
	if created.AuthID != "a" || created.Hash != "h" || created.TorrentID == nil {
		t.Fatalf("unexpected %#v", created)
	}
}

func TestDeleteTorrent(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/torrents/controltorrent", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer key" {
			t.Fatalf("authorization = %q", got)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content-type = %q", r.Header.Get("Content-Type"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["operation"] != "delete" || body["torrent_id"] != "42" || body["all"] != false {
			t.Fatalf("body = %#v", body)
		}
		fmt.Fprint(w, `{"success":true,"data":{}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := New("key", "test")
	c.SetBaseURL(srv.URL)
	if err := c.DeleteTorrent(context.Background(), "42"); err != nil {
		t.Fatal(err)
	}
}
