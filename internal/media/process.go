package media

import (
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/TorBox-App/torbox-rclone/internal/torbox"
)

func ProcessItems(c *torbox.Client, typ torbox.DownloadType, items []torbox.Item) []FileRecord {
	type pair struct {
		item torbox.Item
		file torbox.RemoteFile
	}
	var videos, subs []pair
	for _, item := range items {
		if !item.Cached {
			continue
		}
		for _, f := range item.Files {
			if IsVideoMIME(f.MIMEType) {
				videos = append(videos, pair{item, f})
			} else if IsSubtitleMIME(f.MIMEType) {
				subs = append(subs, pair{item, f})
			}
		}
	}
	workers := runtime.NumCPU()*2 - 1
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan pair)
	out := make(chan FileRecord, len(videos))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				out <- BuildVideoRecord(c, typ, p.item, p.file)
			}
		}()
	}
	go func() {
		for _, p := range videos {
			jobs <- p
		}
		close(jobs)
		wg.Wait()
		close(out)
	}()
	var records []FileRecord
	byItem := map[string][]FileRecord{}
	for r := range out {
		records = append(records, r)
		byItem[r.ItemID] = append(byItem[r.ItemID], r)
	}
	for _, p := range subs {
		if m, ok := findMatchingVideo(p.file, byItem[p.item.ID]); ok {
			records = append(records, BuildSubtitleRecord(c, typ, p.item, p.file, m))
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].Key < records[j].Key })
	return records
}

func BuildVideoRecord(c *torbox.Client, typ torbox.DownloadType, item torbox.Item, f torbox.RemoteFile) FileRecord {
	r := baseRecord(c, typ, item, f)
	md := Classify(item.Name, f.ShortName, f.Name)
	applyMetadata(&r, md)
	return r
}
func BuildSubtitleRecord(c *torbox.Client, typ torbox.DownloadType, item torbox.Item, f torbox.RemoteFile, video FileRecord) FileRecord {
	r := baseRecord(c, typ, item, f)
	r.MetadataTitle = video.MetadataTitle
	r.MetadataMediaType = video.MetadataMediaType
	r.MetadataYears = video.MetadataYears
	r.MetadataSeason = video.MetadataSeason
	r.MetadataEpisode = video.MetadataEpisode
	r.MetadataRootFolderName = video.MetadataRootFolderName
	r.MetadataFolderName = video.MetadataFolderName
	r.MetadataExtraFolderName = video.MetadataExtraFolderName
	ext := filepath.Ext(f.ShortName)
	stem := strings.TrimSuffix(firstNonEmpty(video.MetadataFileName, video.FileName), filepath.Ext(firstNonEmpty(video.MetadataFileName, video.FileName)))
	if suffix := subtitleTrackSuffix(f.ShortName, video); suffix != "" {
		r.MetadataFileName = stem + "." + suffix + ext
	} else {
		r.MetadataFileName = stem + ext
	}
	return r
}

func baseRecord(c *torbox.Client, typ torbox.DownloadType, item torbox.Item, f torbox.RemoteFile) FileRecord {
	ext := filepath.Ext(f.ShortName)
	key := string(typ) + "/" + item.ID + "/" + f.ID
	return FileRecord{Key: key, ItemID: item.ID, Type: typ, FolderName: item.Name, FolderHash: item.Hash, FileID: f.ID, FileName: f.ShortName, FileSize: f.Size, MIMEType: f.MIMEType, OriginalPath: f.Name, DownloadLink: c.PermanentDownloadURL(typ, item, f), Extension: ext}
}
func applyMetadata(r *FileRecord, md Metadata) {
	r.MetadataTitle = md.Title
	r.MetadataMediaType = md.MediaType
	r.MetadataYears = md.Years
	r.MetadataSeason = md.Season
	r.MetadataEpisode = md.Episode
	r.MetadataRootFolderName = md.RootFolderName
	r.MetadataFolderName = md.FolderName
	r.MetadataExtraFolderName = md.ExtraFolderName
	r.MetadataFileName = md.FileName
}

var seriesKeyREs = []*regexp.Regexp{regexp.MustCompile(`(?i)\bS(\d{1,2})E(\d{1,3})\b`), regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`)}

func extractSeriesKey(text string) (int, int, bool) {
	for _, re := range seriesKeyREs {
		if m := re.FindStringSubmatch(text); m != nil {
			return atoi(m[1]), atoi(m[2]), true
		}
	}
	return 0, 0, false
}
func findMatchingVideo(sub torbox.RemoteFile, videos []FileRecord) (FileRecord, bool) {
	if len(videos) == 0 {
		return FileRecord{}, false
	}
	if len(videos) == 1 {
		return videos[0], true
	}
	if s, e, ok := extractSeriesKey(sub.Name + " " + sub.ShortName); ok {
		for _, v := range videos {
			if v.MetadataSeason != nil && v.MetadataEpisode != nil && *v.MetadataSeason == s && *v.MetadataEpisode == e {
				return v, true
			}
		}
	}
	path := strings.ToLower(sub.Name)
	for _, v := range videos {
		stem := strings.ToLower(strings.TrimSuffix(v.FileName, filepath.Ext(v.FileName)))
		if stem != "" && strings.Contains(path, stem) {
			return v, true
		}
	}
	var movies []FileRecord
	for _, v := range videos {
		if v.MetadataMediaType == "movie" {
			movies = append(movies, v)
		}
	}
	if len(movies) == 1 {
		return movies[0], true
	}
	return FileRecord{}, false
}
func subtitleTrackSuffix(name string, video FileRecord) string {
	subStem := strings.TrimSuffix(name, filepath.Ext(name))
	orig := strings.TrimSuffix(video.FileName, filepath.Ext(video.FileName))
	meta := strings.TrimSuffix(video.MetadataFileName, filepath.Ext(video.MetadataFileName))
	norm := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(s), "_", "."), " ", ".")
	}
	if norm(subStem) == norm(orig) || subStem == meta {
		return ""
	}
	toks := regexp.MustCompile(`[^A-Za-z0-9]+`).Split(subStem, -1)
	var kept []string
	for _, t := range toks {
		t = strings.ToLower(t)
		if t != "" && !regexp.MustCompile(`^\d+$`).MatchString(t) {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, ".")
}
