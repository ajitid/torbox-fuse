package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"github.com/TorBox-App/torbox-fuse/internal/media"
	"github.com/TorBox-App/torbox-fuse/internal/torbox"
	"github.com/TorBox-App/torbox-fuse/internal/vfs"
	"github.com/a-h/templ"
)

type refreshFunc func(context.Context, string) (int, error)
type listFunc func(context.Context) ([]media.FileRecord, error)
type addTorrentFunc func(context.Context, string) error
type deleteTorrentFunc func(context.Context, string) error

//go:embed assets/app.css
var webAssets embed.FS

func startWebServer(ctx context.Context, logger *log.Logger, refresh refreshFunc, list listFunc, add addTorrentFunc, deleteTorrent deleteTorrentFunc) (*http.Server, error) {
	ln, err := net.Listen("tcp", "0.0.0.0:3939")
	if err != nil {
		return nil, err
	}
	srv := &http.Server{Handler: newWebHandler(refresh, list, add, deleteTorrent)}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	go func() {
		logger.Printf("web UI listening on http://0.0.0.0:3939")
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("web UI stopped: %v", err)
		}
	}()
	return srv, nil
}

func newWebHandler(refresh refreshFunc, list listFunc, add addTorrentFunc, deleteTorrent deleteTorrentFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/assets/app.css", assetHandler("assets/app.css"))
	mux.HandleFunc("/assets/preflight.css", assetHandler("assets/preflight.css"))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/mount", http.StatusSeeOther)
	})
	mux.HandleFunc("/mount", mountHandler(list))
	mux.HandleFunc("/mount/", mountHandler(list))
	mux.HandleFunc("/torrents", torrentsHandler(list, add))
	mux.HandleFunc("/torrents/", torrentDetailHandler(list, deleteTorrent))
	mux.HandleFunc("/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		if _, err := refresh(r.Context(), "web"); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, redirectTarget(r, "/mount"), http.StatusSeeOther)
	})
	return mux
}

type fileStats struct {
	Files   int
	Movies  int
	Series  int
	Unknown int
}

type breadcrumb struct {
	Name string
	Path string
}

type mountEntry struct {
	Name  string
	Path  string
	IsDir bool
	Size  int64
	File  *media.FileRecord
}

type mountPageData struct {
	Path        string
	Breadcrumbs []breadcrumb
	Entries     []mountEntry
	Stats       fileStats
	Error       string
}

type torrentSummary struct {
	ID   string
	Name string
	Hash string

	FileCount int
	Size      int64
	Files     []media.FileRecord
}

type torrentsPageData struct {
	Torrents []torrentSummary
	Stats    fileStats
	Error    string
}

type torrentPageData struct {
	Torrent torrentSummary
	Stats   fileStats
	Error   string
}

func assetHandler(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		http.ServeFileFS(w, r, webAssets, name)
	}
}

func mountHandler(list listFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		files, err := list(r.Context())
		data := mountPageData{Path: mountPathFromURL(r.URL.Path), Stats: buildStats(files)}
		data.Breadcrumbs = buildBreadcrumbs(data.Path)
		if err != nil {
			data.Error = err.Error()
			render(w, r, http.StatusInternalServerError, mountPage(data))
			return
		}
		data.Entries = buildMountEntries(files, data.Path)
		render(w, r, http.StatusOK, mountPage(data))
	}
}

func torrentsHandler(list listFunc, add addTorrentFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			files, err := list(r.Context())
			data := torrentsPageData{Stats: buildStats(files)}
			if err != nil {
				data.Error = err.Error()
				render(w, r, http.StatusInternalServerError, torrentsPage(data))
				return
			}
			data.Torrents = groupTorrentSummaries(files)
			render(w, r, http.StatusOK, torrentsPage(data))
		case http.MethodPost:
			if err := r.ParseForm(); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			magnet := strings.TrimSpace(r.FormValue("magnet"))
			if magnet == "" {
				http.Error(w, "magnet is required", http.StatusBadRequest)
				return
			}
			if err := add(r.Context(), magnet); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/torrents", http.StatusSeeOther)
		default:
			methodNotAllowed(w, http.MethodGet, http.MethodPost)
		}
	}
}

func torrentDetailHandler(list listFunc, deleteTorrent deleteTorrentFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, action := parseTorrentPath(r.URL.Path)
		if id == "" {
			http.NotFound(w, r)
			return
		}
		if action == "delete" {
			if r.Method != http.MethodPost {
				methodNotAllowed(w, http.MethodPost)
				return
			}
			if err := deleteTorrent(r.Context(), id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/torrents", http.StatusSeeOther)
			return
		}
		if action != "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		files, err := list(r.Context())
		data := torrentPageData{Stats: buildStats(files)}
		if err != nil {
			data.Error = err.Error()
			render(w, r, http.StatusInternalServerError, torrentPage(data))
			return
		}
		for _, s := range groupTorrentSummaries(files) {
			if s.ID == id {
				data.Torrent = s
				render(w, r, http.StatusOK, torrentPage(data))
				return
			}
		}
		http.NotFound(w, r)
	}
}

func buildMountEntries(files []media.FileRecord, dir string) []mountEntry {
	dir = cleanMountPath(dir)
	tree := vfs.Build(files)
	entries := tree.ListDir(dir)
	out := make([]mountEntry, 0, len(entries))
	for _, entry := range entries {
		p := path.Join(dir, entry.Name)
		me := mountEntry{Name: entry.Name, Path: p, IsDir: entry.IsDir, Size: entry.Size}
		if !entry.IsDir {
			if f, ok := tree.GetFile(p); ok {
				ff := f
				me.File = &ff
			}
		}
		out = append(out, me)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func groupTorrentSummaries(files []media.FileRecord) []torrentSummary {
	byID := map[string]*torrentSummary{}
	for _, f := range files {
		if f.Type != torbox.DownloadTorrent || f.ItemID == "" {
			continue
		}
		s := byID[f.ItemID]
		if s == nil {
			s = &torrentSummary{ID: f.ItemID, Name: firstNonEmpty(f.FolderName, f.ItemID), Hash: f.FolderHash}
			byID[f.ItemID] = s
		}
		if s.Name == "" && f.FolderName != "" {
			s.Name = f.FolderName
		}
		s.FileCount++
		s.Size += f.FileSize
		s.Files = append(s.Files, f)
	}
	out := make([]torrentSummary, 0, len(byID))
	for _, s := range byID {
		sort.Slice(s.Files, func(i, j int) bool { return vfs.RecordPath(s.Files[i]) < vfs.RecordPath(s.Files[j]) })
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func mountEntryIcon(entry mountEntry) string {
	if entry.IsDir {
		return "📁"
	}
	if entry.File != nil && media.IsSubtitleMIME(entry.File.MIMEType) {
		return "💬"
	}
	return "🎬"
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

func buildBreadcrumbs(p string) []breadcrumb {
	p = cleanMountPath(p)
	crumbs := []breadcrumb{{Name: "mount", Path: "/"}}
	if p == "/" {
		return crumbs
	}
	cur := ""
	for _, part := range strings.Split(strings.Trim(p, "/"), "/") {
		cur = path.Join(cur, part)
		crumbs = append(crumbs, breadcrumb{Name: part, Path: "/" + cur})
	}
	return crumbs
}

func mountPathFromURL(requestPath string) string {
	p := strings.TrimPrefix(requestPath, "/mount")
	if p == "" {
		return "/"
	}
	return cleanMountPath(p)
}

func cleanMountPath(p string) string { return path.Clean("/" + strings.TrimPrefix(p, "/")) }

func parseTorrentPath(p string) (id, action string) {
	rest := strings.Trim(strings.TrimPrefix(p, "/torrents/"), "/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	if len(parts) == 2 && parts[1] == "delete" {
		return parts[0], "delete"
	}
	return parts[0], strings.Join(parts[1:], "/")
}

func render(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		log.Printf("render web UI: %v", err)
	}
}

func methodNotAllowed(w http.ResponseWriter, methods ...string) {
	w.Header().Set("Allow", strings.Join(methods, ", "))
	http.Error(w, "method not allowed\n", http.StatusMethodNotAllowed)
}

func redirectTarget(r *http.Request, fallback string) string {
	ref := r.Header.Get("Referer")
	if ref == "" {
		return fallback
	}
	if strings.HasPrefix(ref, "/") {
		return ref
	}
	u, err := url.Parse(ref)
	if err == nil && u.Host == r.Host {
		return ref
	}
	return fallback
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n/div >= unit && exp < 4 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
