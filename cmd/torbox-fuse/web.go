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
	"time"

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
	ID      string
	Name    string
	Hash    string
	AddedAt time.Time

	FileCount int
	Size      int64
	Files     []media.FileRecord
}

type torrentsPageData struct {
	Torrents []torrentSummary
	Stats    fileStats
	Query    string
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
			data := torrentsPageData{Stats: buildStats(files), Query: strings.TrimSpace(r.URL.Query().Get("q"))}
			if err != nil {
				data.Error = err.Error()
				render(w, r, http.StatusInternalServerError, torrentsPage(data))
				return
			}
			data.Torrents = filterTorrentSummaries(groupTorrentSummaries(files), data.Query)
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
			http.Redirect(w, r, torrentsPath(r.URL.Query().Get("q")), http.StatusSeeOther)
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
			http.Redirect(w, r, torrentsPath(r.URL.Query().Get("q")), http.StatusSeeOther)
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
		if s.AddedAt.IsZero() || (!f.ItemAddedAt.IsZero() && f.ItemAddedAt.After(s.AddedAt)) {
			s.AddedAt = f.ItemAddedAt
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
	sort.Slice(out, func(i, j int) bool {
		if !out[i].AddedAt.Equal(out[j].AddedAt) {
			if out[i].AddedAt.IsZero() {
				return false
			}
			if out[j].AddedAt.IsZero() {
				return true
			}
			return out[i].AddedAt.After(out[j].AddedAt)
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func filterTorrentSummaries(torrents []torrentSummary, query string) []torrentSummary {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return torrents
	}
	type match struct {
		torrent torrentSummary
		rank    int
	}
	matches := make([]match, 0, len(torrents))
	for _, torrent := range torrents {
		if rank, ok := torrentMatchRank(torrent, query); ok {
			matches = append(matches, match{torrent: torrent, rank: rank})
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].rank != matches[j].rank {
			return matches[i].rank < matches[j].rank
		}
		return strings.ToLower(matches[i].torrent.Name) < strings.ToLower(matches[j].torrent.Name)
	})
	out := make([]torrentSummary, 0, len(matches))
	for _, match := range matches {
		out = append(out, match.torrent)
	}
	return out
}

func torrentMatchRank(torrent torrentSummary, query string) (int, bool) {
	best := 100
	queryTokens := searchTokens(query)
	considerContains := func(rank int, value string) {
		value = strings.ToLower(value)
		if value == "" || !strings.Contains(value, query) || rank >= best {
			return
		}
		best = rank
	}
	considerTokenMatch := func(rank int, values ...string) {
		if len(queryTokens) == 0 || rank >= best || !containsAllSearchTokens(queryTokens, values...) {
			return
		}
		best = rank
	}
	name := strings.ToLower(torrent.Name)
	if name == query {
		best = 0
	} else if strings.HasPrefix(name, query) {
		best = 1
	} else if strings.Contains(name, query) {
		best = 2
	}
	considerTokenMatch(3, torrent.Name)
	considerContains(4, torrent.ID)
	considerContains(4, torrent.Hash)
	for _, file := range torrent.Files {
		fileName := strings.ToLower(file.FileName)
		if fileName == query && best > 5 {
			best = 5
		} else if strings.HasPrefix(fileName, query) && best > 6 {
			best = 6
		} else if strings.Contains(fileName, query) && best > 7 {
			best = 7
		}
		considerContains(8, file.OriginalPath)
		considerContains(8, vfs.RecordPath(file))
		considerTokenMatch(9, torrent.Name, file.FileName, file.OriginalPath, vfs.RecordPath(file), file.MetadataTitle, file.MetadataRootFolderName, file.MetadataFolderName, file.MetadataExtraFolderName, file.MetadataFileName)
	}
	return best, best < 100
}

func containsAllSearchTokens(tokens []string, values ...string) bool {
	haystack := normalizedSearchText(strings.Join(values, " "))
	if haystack == "" {
		return false
	}
	for _, token := range tokens {
		if !strings.Contains(haystack, token) {
			return false
		}
	}
	return true
}

func searchTokens(value string) []string {
	fields := strings.Fields(normalizedSearchText(value))
	if len(fields) <= 1 {
		return fields
	}
	out := fields[:0]
	for _, field := range fields {
		if _, ok := searchStopWords[field]; ok {
			continue
		}
		out = append(out, field)
	}
	if len(out) == 0 {
		return fields
	}
	return out
}

func normalizedSearchText(value string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}), " ")
}

var searchStopWords = map[string]struct{}{
	"a": {}, "an": {}, "and": {}, "of": {}, "the": {},
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

func torrentsPath(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return "/torrents"
	}
	v := url.Values{}
	v.Set("q", query)
	return "/torrents?" + v.Encode()
}

func torrentPath(id string) string {
	return "/torrents/" + url.PathEscape(id)
}

func torrentDeletePath(id string, query string) string {
	p := "/torrents/" + url.PathEscape(id) + "/delete"
	query = strings.TrimSpace(query)
	if query == "" {
		return p
	}
	v := url.Values{}
	v.Set("q", query)
	return p + "?" + v.Encode()
}

func torrentMagnet(t torrentSummary) string {
	hash := strings.TrimSpace(t.Hash)
	if hash == "" {
		return ""
	}
	return "magnet:?xt=urn:btih:" + hash
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
