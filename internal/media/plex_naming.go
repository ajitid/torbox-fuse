package media

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// ApplyPlexVersionNaming adds Plex-compatible version labels for duplicate movie
// files or duplicate TV episode files that would otherwise share one VFS path.
func ApplyPlexVersionNaming(records []FileRecord) {
	groups := map[string][]int{}
	for i, r := range records {
		if !IsVideoMIME(r.MIMEType) || r.MetadataFileName == "" || r.MetadataRootFolderName == "" {
			continue
		}
		switch r.MetadataMediaType {
		case "movie":
			if r.MetadataExtraFolderName != "" || r.MetadataFolderName != "" {
				continue
			}
			groups["movie|"+r.MetadataRootFolderName] = append(groups["movie|"+r.MetadataRootFolderName], i)
		case "series":
			groups[seriesVersionKey(r)] = append(groups[seriesVersionKey(r)], i)
		}
	}

	usedPaths := map[string]bool{}
	for _, idxs := range groups {
		for _, idx := range idxs {
			usedPaths[metadataPathKey(records[idx])] = true
		}
	}

	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		sort.Slice(idxs, func(i, j int) bool { return records[idxs[i]].Key < records[idxs[j]].Key })
		seenNames := map[string]bool{}
		seenLabels := map[string]bool{}
		for _, idx := range idxs {
			r := records[idx]
			ext := filepath.Ext(r.MetadataFileName)
			stem := strings.TrimSuffix(r.MetadataFileName, ext)
			label := plexVersionLabel(r)
			if seenLabels[strings.ToLower(label)] {
				if fallback := cleanedReleaseStem(r); fallback != "" && !strings.EqualFold(fallback, label) {
					label = fallback
				}
			}
			if seenLabels[strings.ToLower(label)] {
				label = label + " - " + shortStableID(r)
			}
			name := SafePathName(stem+" - "+label) + ext
			pathKey := metadataPathKeyWithName(r, name)
			if seenNames[name] || (usedPaths[pathKey] && pathKey != metadataPathKey(r)) {
				label = label + " - " + shortStableID(r)
				name = SafePathName(stem+" - "+label) + ext
				pathKey = metadataPathKeyWithName(r, name)
			}
			delete(usedPaths, metadataPathKey(r))
			records[idx].MetadataFileName = name
			seenNames[name] = true
			seenLabels[strings.ToLower(label)] = true
			usedPaths[pathKey] = true
		}
	}
}

func seriesVersionKey(r FileRecord) string {
	season, episode := "", ""
	if r.MetadataSeason != nil {
		season = pad(*r.MetadataSeason, 2)
	}
	if r.MetadataEpisode != nil {
		episode = pad(*r.MetadataEpisode, 3)
	}
	if season == "" && episode == "" {
		return strings.Join([]string{"series", r.MetadataRootFolderName, r.MetadataFolderName, r.MetadataFileName}, "|")
	}
	return strings.Join([]string{"series", r.MetadataRootFolderName, r.MetadataFolderName, season, episode}, "|")
}

func plexVersionLabel(r FileRecord) string {
	text := strings.Join([]string{r.FolderName, r.FileName, r.OriginalPath, r.MetadataFileName}, " ")
	if label := technicalLabel(text); label != "" {
		return label
	}
	if label := cleanedReleaseStem(r); label != "" {
		return label
	}
	return shortStableID(r)
}

var tokenPatterns = []struct {
	re    *regexp.Regexp
	canon string
}{
	{regexp.MustCompile(`(?i)\b(480p|576p|720p|1080p|1440p|2160p|4k|8k)\b`), ""},
	{regexp.MustCompile(`(?i)\bblu[- .]?ray\b`), "BluRay"},
	{regexp.MustCompile(`(?i)\bbrrip\b`), "BRRip"},
	{regexp.MustCompile(`(?i)\bbdrip\b`), "BDRip"},
	{regexp.MustCompile(`(?i)\bweb[- .]?dl\b`), "WEB-DL"},
	{regexp.MustCompile(`(?i)\bweb[- .]?rip\b`), "WEBRip"},
	{regexp.MustCompile(`(?i)\bhdtv\b`), "HDTV"},
	{regexp.MustCompile(`(?i)\bdvdrip\b`), "DVDRip"},
	{regexp.MustCompile(`(?i)\bremux\b`), "Remux"},
	{regexp.MustCompile(`(?i)\bhdr10\+`), "HDR10+"},
	{regexp.MustCompile(`(?i)\bhdr10\b`), "HDR10"},
	{regexp.MustCompile(`(?i)\bdolby[ .]?vision\b`), "Dolby Vision"},
	{regexp.MustCompile(`(?i)\bdv\b`), "DV"},
	{regexp.MustCompile(`(?i)\bhdr\b`), "HDR"},
	{regexp.MustCompile(`(?i)\bx264\b`), "x264"},
	{regexp.MustCompile(`(?i)\bx265\b`), "x265"},
	{regexp.MustCompile(`(?i)\bh[ .]?264\b`), "H264"},
	{regexp.MustCompile(`(?i)\bh[ .]?265\b`), "H265"},
	{regexp.MustCompile(`(?i)\bhevc\b`), "HEVC"},
	{regexp.MustCompile(`(?i)\bav1\b`), "AV1"},
	{regexp.MustCompile(`(?i)\bxvid\b`), "XviD"},
	{regexp.MustCompile(`(?i)\baac\b`), "AAC"},
	{regexp.MustCompile(`(?i)\bac[ .-]?3\b`), "AC3"},
	{regexp.MustCompile(`(?i)\be[ .-]?ac[ .-]?3\b|\beac3\b`), "EAC3"},
	{regexp.MustCompile(`(?i)\bdts\b`), "DTS"},
	{regexp.MustCompile(`(?i)\btruehd\b`), "TrueHD"},
	{regexp.MustCompile(`(?i)\batmos\b`), "Atmos"},
}

func technicalLabel(text string) string {
	var parts []string
	seen := map[string]bool{}
	for _, p := range tokenPatterns {
		m := p.re.FindString(text)
		if m == "" {
			continue
		}
		canon := p.canon
		if canon == "" {
			canon = strings.ToLower(m)
			if canon == "4k" || canon == "8k" {
				canon = strings.ToUpper(canon)
			}
		}
		if !seen[canon] {
			parts = append(parts, canon)
			seen[canon] = true
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func cleanedReleaseStem(r FileRecord) string {
	candidates := []string{r.FolderName, r.FileName, r.OriginalPath, r.MetadataFileName}
	base := strings.TrimSuffix(r.MetadataFileName, filepath.Ext(r.MetadataFileName))
	root := r.MetadataRootFolderName
	for _, c := range candidates {
		if c == "" {
			continue
		}
		stem := strings.TrimSuffix(filepath.Base(c), filepath.Ext(c))
		stem = strings.NewReplacer(".", " ", "_", " ").Replace(stem)
		stem = SafePathName(stem)
		if stem != "" && !strings.EqualFold(stem, base) && !strings.EqualFold(stem, root) && stem != "Unknown" {
			return stem
		}
	}
	return ""
}

func shortStableID(r FileRecord) string {
	sum := sha1.Sum([]byte(r.Key))
	return hex.EncodeToString(sum[:])[:8]
}

func metadataPathKey(r FileRecord) string { return metadataPathKeyWithName(r, r.MetadataFileName) }

func metadataPathKeyWithName(r FileRecord, name string) string {
	return strings.Join([]string{r.MetadataMediaType, r.MetadataRootFolderName, r.MetadataFolderName, r.MetadataExtraFolderName, name}, "/")
}
