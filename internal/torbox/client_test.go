package torbox

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
