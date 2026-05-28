package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TorBox-App/torbox-fuse/internal/media"
)

func TestControlFiles(t *testing.T) {
	h := newControlHandler(
		func(context.Context, string) (int, error) { return 0, nil },
		func(context.Context) ([]media.FileRecord, error) {
			return []media.FileRecord{{Key: "k", FileName: "movie.mkv", FileSize: 123}}, nil
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/files", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	var got struct {
		OK    bool               `json:"ok"`
		Files []media.FileRecord `json:"files"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || len(got.Files) != 1 || got.Files[0].Key != "k" || got.Files[0].FileName != "movie.mkv" {
		t.Fatalf("unexpected response %#v", got)
	}
}

func TestControlFilesPathMovies(t *testing.T) {
	h := newControlHandler(
		func(context.Context, string) (int, error) { return 0, nil },
		func(context.Context) ([]media.FileRecord, error) {
			return []media.FileRecord{
				{Key: "movie", MetadataMediaType: "movie", MetadataRootFolderName: "Dune (2021)", MetadataFileName: "Dune (2021).mkv"},
				{Key: "series", MetadataMediaType: "series", MetadataRootFolderName: "Breaking Bad", MetadataFolderName: "Season 1", MetadataFileName: "Breaking Bad - S01E01.mkv"},
				{Key: "unknown", MetadataMediaType: "unknown", MetadataRootFolderName: "Misc", MetadataFileName: "clip.mkv"},
			}, nil
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/files/movies", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	var got struct {
		OK    bool               `json:"ok"`
		Files []media.FileRecord `json:"files"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || len(got.Files) != 1 || got.Files[0].Key != "movie" {
		t.Fatalf("unexpected response %#v", got)
	}
}

func TestControlFilesPathMovieRoot(t *testing.T) {
	h := newControlHandler(
		func(context.Context, string) (int, error) { return 0, nil },
		func(context.Context) ([]media.FileRecord, error) {
			return []media.FileRecord{
				{Key: "dune", MetadataMediaType: "movie", MetadataRootFolderName: "Dune (2021)", MetadataFileName: "Dune (2021).mkv"},
				{Key: "dune-featurette", MetadataMediaType: "movie", MetadataRootFolderName: "Dune (2021)", MetadataFolderName: "Featurettes", MetadataFileName: "Making Dune.mkv"},
				{Key: "1917", MetadataMediaType: "movie", MetadataRootFolderName: "1917 (2019)", MetadataFileName: "1917 (2019).mkv"},
			}, nil
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/files/movies/Dune%20(2021)/", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	var got struct {
		OK    bool               `json:"ok"`
		Files []media.FileRecord `json:"files"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || len(got.Files) != 2 || got.Files[0].Key != "dune" || got.Files[1].Key != "dune-featurette" {
		t.Fatalf("unexpected response %#v", got)
	}
}

func TestControlFilesPathSeriesSeason(t *testing.T) {
	h := newControlHandler(
		func(context.Context, string) (int, error) { return 0, nil },
		func(context.Context) ([]media.FileRecord, error) {
			return []media.FileRecord{
				{Key: "bb-s1e1", MetadataMediaType: "series", MetadataRootFolderName: "Breaking Bad", MetadataFolderName: "Season 1", MetadataFileName: "Breaking Bad - S01E01.mkv"},
				{Key: "bb-s1e2", MetadataMediaType: "series", MetadataRootFolderName: "Breaking Bad", MetadataFolderName: "Season 1", MetadataFileName: "Breaking Bad - S01E02.mkv"},
				{Key: "bb-s2e1", MetadataMediaType: "series", MetadataRootFolderName: "Breaking Bad", MetadataFolderName: "Season 2", MetadataFileName: "Breaking Bad - S02E01.mkv"},
			}, nil
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/files/series/Breaking%20Bad/Season%201", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	var got struct {
		OK    bool               `json:"ok"`
		Files []media.FileRecord `json:"files"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || len(got.Files) != 2 || got.Files[0].Key != "bb-s1e1" || got.Files[1].Key != "bb-s1e2" {
		t.Fatalf("unexpected response %#v", got)
	}
}

func TestControlStats(t *testing.T) {
	h := newControlHandler(
		func(context.Context, string) (int, error) { return 0, nil },
		func(context.Context) ([]media.FileRecord, error) {
			return []media.FileRecord{
				{Key: "movie", MetadataMediaType: "movie"},
				{Key: "series", MetadataMediaType: "series"},
				{Key: "unknown", MetadataMediaType: "unknown"},
				{Key: "empty"},
			}, nil
		},
	)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusOK)
	}
	var got struct {
		OK    bool      `json:"ok"`
		Stats fileStats `json:"stats"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Stats.Files != 4 || got.Stats.Movies != 1 || got.Stats.Series != 1 || got.Stats.Unknown != 2 {
		t.Fatalf("unexpected response %#v", got)
	}
}

func TestControlFilesRequiresGET(t *testing.T) {
	h := newControlHandler(
		func(context.Context, string) (int, error) { return 0, nil },
		func(context.Context) ([]media.FileRecord, error) { return nil, nil },
	)

	req := httptest.NewRequest(http.MethodPost, "/files", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)

	if res.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusMethodNotAllowed)
	}
	if allow := res.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow = %q, want %q", allow, http.MethodGet)
	}
}
