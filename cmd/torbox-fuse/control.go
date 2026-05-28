package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/TorBox-App/torbox-fuse/internal/media"
	"github.com/TorBox-App/torbox-fuse/internal/vfs"
)

type refreshFunc func(context.Context, string) (int, error)
type listFunc func(context.Context) ([]media.FileRecord, error)

func startControlServer(ctx context.Context, socketPath string, logger *log.Logger, refresh refreshFunc, list listFunc) (*http.Server, error) {
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	srv := &http.Server{Handler: newControlHandler(refresh, list)}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
		_ = os.Remove(socketPath)
	}()
	go func() {
		logger.Printf("control socket listening on %s", socketPath)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("control socket stopped: %v", err)
		}
	}()
	return srv, nil
}

func newControlHandler(refresh refreshFunc, list listFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed\n", http.StatusMethodNotAllowed)
			return
		}
		files, err := refresh(r.Context(), "api")
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "files": files})
	})
	mux.HandleFunc("/files", filesHandler(list))
	mux.HandleFunc("/files/", filesHandler(list))
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed\n", http.StatusMethodNotAllowed)
			return
		}
		files, err := list(r.Context())
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "stats": buildStats(files)})
	})
	return mux
}

type fileStats struct {
	Files   int `json:"files"`
	Movies  int `json:"movies"`
	Series  int `json:"series"`
	Unknown int `json:"unknown"`
}

func filesHandler(list listFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed\n", http.StatusMethodNotAllowed)
			return
		}
		files, err := list(r.Context())
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "files": filesUnderPath(files, filesPath(r.URL.Path))})
	}
}

func filesPath(requestPath string) string {
	p := strings.TrimPrefix(requestPath, "/files")
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "/"
	}
	return "/" + p
}

func filesUnderPath(files []media.FileRecord, prefix string) []media.FileRecord {
	prefix = path.Clean("/" + strings.TrimPrefix(prefix, "/"))
	if prefix == "/" {
		return files
	}
	out := make([]media.FileRecord, 0, len(files))
	for _, f := range files {
		p := vfs.RecordPath(f)
		if p == prefix || strings.HasPrefix(p, prefix+"/") {
			out = append(out, f)
		}
	}
	return out
}

func buildStats(files []media.FileRecord) fileStats {
	stats := fileStats{Files: len(files)}
	for _, f := range files {
		switch normalizeRecordMediaType(f) {
		case "movie":
			stats.Movies++
		case "series":
			stats.Series++
		default:
			stats.Unknown++
		}
	}
	return stats
}

func normalizeRecordMediaType(f media.FileRecord) string {
	typ, err := normalizeMediaType(f.MetadataMediaType)
	if err != nil || typ == "" {
		return "unknown"
	}
	return typ
}

func normalizeMediaType(typ string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "":
		return "", nil
	case "movie", "movies":
		return "movie", nil
	case "series":
		return "series", nil
	case "unknown":
		return "unknown", nil
	default:
		return "", errors.New("type must be one of movies, series, or unknown")
	}
}
