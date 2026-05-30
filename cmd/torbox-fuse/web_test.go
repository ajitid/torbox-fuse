package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/TorBox-App/torbox-fuse/internal/media"
	"github.com/TorBox-App/torbox-fuse/internal/torbox"
)

func TestWebMountRendersMountedFile(t *testing.T) {
	h := newWebHandler(noRefresh, testFiles, noAdd, noDelete)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/mount", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	body := res.Body.String()
	if !strings.Contains(body, "movies") || !strings.Contains(body, "series") {
		t.Fatalf("body did not contain mount entries: %s", body)
	}
}

func TestWebMountSubfolderOnlyShowsChildren(t *testing.T) {
	h := newWebHandler(noRefresh, testFiles, noAdd, noDelete)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/mount/movies/Dune%20(2021)", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	body := res.Body.String()
	if !strings.Contains(body, "Dune (2021).mkv") || strings.Contains(body, "Breaking Bad") {
		t.Fatalf("unexpected subfolder body: %s", body)
	}
}

func TestWebTorrentsGroupsAcceptedTorrentFiles(t *testing.T) {
	h := newWebHandler(noRefresh, testFiles, noAdd, noDelete)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/torrents", nil))
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d", res.Code)
	}
	body := res.Body.String()
	if !strings.Contains(body, "Dune Pack") || strings.Contains(body, "Usenet Movie") {
		t.Fatalf("unexpected torrents body: %s", body)
	}
}

func TestWebRefreshRedirects(t *testing.T) {
	called := false
	h := newWebHandler(func(context.Context, string) (int, error) { called = true; return 3, nil }, testFiles, noAdd, noDelete)
	req := httptest.NewRequest(http.MethodPost, "/refresh", nil)
	req.Header.Set("Referer", "/torrents")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if !called || res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/torrents" {
		t.Fatalf("called=%v status=%d location=%q", called, res.Code, res.Header().Get("Location"))
	}
}

func TestWebAddTorrentRedirects(t *testing.T) {
	var gotMagnet string
	h := newWebHandler(noRefresh, testFiles, func(_ context.Context, magnet string) error {
		gotMagnet = magnet
		return nil
	}, noDelete)
	form := url.Values{"magnet": {"magnet:?xt=urn:btih:abc"}}
	req := httptest.NewRequest(http.MethodPost, "/torrents", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/torrents" || gotMagnet != "magnet:?xt=urn:btih:abc" {
		t.Fatalf("status=%d location=%q magnet=%q", res.Code, res.Header().Get("Location"), gotMagnet)
	}
}

func TestWebDeleteTorrentRedirects(t *testing.T) {
	var gotID string
	h := newWebHandler(noRefresh, testFiles, noAdd, func(_ context.Context, id string) error { gotID = id; return nil })
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/torrents/t1/delete", nil))
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/torrents" || gotID != "t1" {
		t.Fatalf("status=%d location=%q id=%q", res.Code, res.Header().Get("Location"), gotID)
	}
}

func TestWebWrongMethod(t *testing.T) {
	h := newWebHandler(noRefresh, testFiles, noAdd, noDelete)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/mount", nil))
	if res.Code != http.StatusMethodNotAllowed || res.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status=%d allow=%q", res.Code, res.Header().Get("Allow"))
	}
}

func TestBuildStats(t *testing.T) {
	stats := buildStats([]media.FileRecord{{MetadataMediaType: "movie"}, {MetadataMediaType: "series"}, {MetadataMediaType: "unknown"}, {}})
	if stats.Files != 4 || stats.Movies != 1 || stats.Series != 1 || stats.Unknown != 2 {
		t.Fatalf("unexpected stats %#v", stats)
	}
}

func testFiles(context.Context) ([]media.FileRecord, error) {
	return []media.FileRecord{
		{Key: "dune", ItemID: "t1", Type: torbox.DownloadTorrent, FolderName: "Dune Pack", FileSize: 100, MetadataMediaType: "movie", MetadataRootFolderName: "Dune (2021)", MetadataFileName: "Dune (2021).mkv", OriginalPath: "Dune/Dune.mkv"},
		{Key: "bb", ItemID: "t2", Type: torbox.DownloadTorrent, FolderName: "Breaking Bad", FileSize: 200, MetadataMediaType: "series", MetadataRootFolderName: "Breaking Bad", MetadataFolderName: "Season 1", MetadataFileName: "Breaking Bad - S01E01.mkv"},
		{Key: "usenet", ItemID: "u1", Type: torbox.DownloadUsenet, FolderName: "Usenet Movie", FileSize: 300, MetadataMediaType: "movie", MetadataRootFolderName: "Usenet Movie", MetadataFileName: "Usenet Movie.mkv"},
	}, nil
}

func noRefresh(context.Context, string) (int, error) { return 0, nil }
func noAdd(context.Context, string) error            { return nil }
func noDelete(context.Context, string) error         { return nil }
