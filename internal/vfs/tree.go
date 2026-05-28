package vfs

import (
	"path"
	"sort"
	"strings"

	"github.com/TorBox-App/torbox-rclone/internal/media"
)

type DirEntry struct {
	Name  string
	IsDir bool
	Size  int64
}
type Tree struct {
	dirs  map[string]map[string]DirEntry
	files map[string]media.FileRecord
}

func Build(records []media.FileRecord) *Tree {
	t := &Tree{dirs: map[string]map[string]DirEntry{"/": {}}, files: map[string]media.FileRecord{}}
	for _, root := range []string{"movies", "series", "unknown"} {
		t.addDir("/", root)
	}
	for _, r := range records {
		p := recordPath(r)
		if p == "" {
			continue
		}
		t.addFile(p, r)
	}
	return t
}
func (t *Tree) IsDir(p string) bool                       { _, ok := t.dirs[clean(p)]; return ok }
func (t *Tree) IsFile(p string) bool                      { _, ok := t.files[clean(p)]; return ok }
func (t *Tree) GetFile(p string) (media.FileRecord, bool) { r, ok := t.files[clean(p)]; return r, ok }
func (t *Tree) ListDir(p string) []DirEntry {
	m := t.dirs[clean(p)]
	out := make([]DirEntry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (t *Tree) addDir(parent, name string) {
	parent = clean(parent)
	full := clean(path.Join(parent, name))
	if t.dirs[parent] == nil {
		t.dirs[parent] = map[string]DirEntry{}
	}
	t.dirs[parent][name] = DirEntry{Name: name, IsDir: true}
	if t.dirs[full] == nil {
		t.dirs[full] = map[string]DirEntry{}
	}
}
func (t *Tree) addFile(p string, r media.FileRecord) {
	p = clean(p)
	dir, name := path.Split(p)
	dir = clean(dir)
	parts := strings.Split(strings.Trim(clean(dir), "/"), "/")
	cur := "/"
	for _, part := range parts {
		if part == "" {
			continue
		}
		t.addDir(cur, part)
		cur = clean(path.Join(cur, part))
	}
	t.files[p] = r
	if t.dirs[dir] == nil {
		t.dirs[dir] = map[string]DirEntry{}
	}
	t.dirs[dir][name] = DirEntry{Name: name, IsDir: false, Size: r.FileSize}
}
func clean(p string) string {
	p = path.Clean("/" + strings.TrimPrefix(p, "/"))
	if p == "." {
		return "/"
	}
	return p
}
func recordPath(r media.FileRecord) string {
	root := r.MetadataRootFolderName
	fn := r.MetadataFileName
	if root == "" || fn == "" {
		return ""
	}
	switch r.MetadataMediaType {
	case "movie":
		if r.MetadataFolderName != "" {
			return path.Join("/movies", media.SafePathName(root), media.SafePathName(r.MetadataFolderName), media.SafePathName(fn))
		}
		return path.Join("/movies", media.SafePathName(root), media.SafePathName(fn))
	case "series":
		if r.MetadataExtraFolderName != "" {
			return path.Join("/series", media.SafePathName(root), media.SafePathName(r.MetadataFolderName), media.SafePathName(r.MetadataExtraFolderName), media.SafePathName(fn))
		}
		return path.Join("/series", media.SafePathName(root), media.SafePathName(r.MetadataFolderName), media.SafePathName(fn))
	default:
		return path.Join("/unknown", media.SafePathName(root), media.SafePathName(fn))
	}
}
