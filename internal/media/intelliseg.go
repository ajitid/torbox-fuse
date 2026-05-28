package media

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var qualityOrRelease = regexp.MustCompile(`(?i)\b(480p|576p|720p|1080p|1440p|2160p|4k|x264|x265|h\.?264|h\.?265|hevc|bluray|brrip|webrip|web[- ]?dl|dvdrip|remux)\b`)
var yearRE = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
var seriesPatterns = []*regexp.Regexp{regexp.MustCompile(`(?i)\bS(\d{1,2})E(\d{1,3})\b`), regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`), regexp.MustCompile(`(?i)\bSeason[ ._-]?(\d{1,2})\b`), regexp.MustCompile(`(?i)\bE(\d{1,3})\b`)}
var extraAliases = map[string]string{"behind the scenes": "Behind The Scenes", "deleted scenes": "Deleted Scenes", "featurettes": "Featurettes", "interviews": "Interviews", "scenes": "Scenes", "shorts": "Shorts", "trailers": "Trailers", "other": "Other", "behindthescenes": "Behind The Scenes", "behind-the-scenes": "Behind The Scenes", "deleted": "Deleted Scenes", "featurette": "Featurettes", "features": "Featurettes", "extras": "Other", "special features": "Other"}

func guessTitle(raw string) string {
	name := strings.TrimSuffix(raw, filepath.Ext(raw))
	repl := strings.NewReplacer(".", " ", "_", " ")
	name = repl.Replace(name)
	for _, re := range []*regexp.Regexp{regexp.MustCompile(`(?i)\bS\d{1,2}E\d{1,3}\b`), regexp.MustCompile(`(?i)\b\d{1,2}x\d{1,3}\b`), regexp.MustCompile(`(?i)\bSeason[ ._-]?\d{1,2}\b`), regexp.MustCompile(`(?i)\bEpisode[ ._-]?\d{1,3}\b`), yearRE, qualityOrRelease} {
		name = re.ReplaceAllString(name, "")
	}
	return SafePathName(name)
}

func extractSeriesMarkers(text string) (bool, *int, *int) {
	for i, re := range seriesPatterns {
		m := re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		if i == 0 || i == 1 {
			return true, ptr(atoi(m[1])), ptr(atoi(m[2]))
		}
		if i == 2 {
			return true, ptr(atoi(m[1])), nil
		}
		return true, nil, ptr(atoi(m[1]))
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "/season ") || strings.Contains(lower, " season ") || strings.Contains(lower, "/tv/") || strings.Contains(lower, " episode ") {
		return true, nil, nil
	}
	return false, nil, nil
}
func extractMovieMarkers(text string) (bool, *int) {
	if m := yearRE.FindStringSubmatch(text); m != nil {
		return true, ptr(atoi(m[1]))
	}
	return qualityOrRelease.MatchString(text), nil
}
func buildSeriesFilename(title string, season, episode *int, ext string) string {
	suffix := "Episode"
	if season != nil && episode != nil {
		suffix = "S" + pad(*season, 2) + "E" + pad(*episode, 2)
	} else if season != nil {
		suffix = "S" + pad(*season, 2)
	} else if episode != nil {
		suffix = "E" + pad(*episode, 2)
	}
	return SafePathName(title+" "+suffix) + ext
}
func buildRootFolder(title, mt string, year *int) string {
	if mt == "movie" && year != nil {
		return SafePathName(title + " (" + strconv.Itoa(*year) + ")")
	}
	return SafePathName(title)
}
func cleanExtraFilename(name string) string {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	stem = regexp.MustCompile(`(?i)^Season[ ._-]?\d{1,2}[ ._-]*`).ReplaceAllString(stem, "")
	return SafePathName(stem) + filepath.Ext(name)
}
func detectMovieExtraFolder(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ""
	}
	for _, p := range parts[1 : len(parts)-1] {
		compact := spaces.ReplaceAllString(strings.ReplaceAll(strings.ToLower(strings.TrimSpace(p)), "_", " "), " ")
		if v := extraAliases[compact]; v != "" {
			return v
		}
	}
	return ""
}

func Classify(downloadItemName, fileShortName, filePath string) Metadata {
	ext := filepath.Ext(fileShortName)
	combined := strings.Join([]string{downloadItemName, fileShortName, filePath}, " ")
	title := SafePathName(guessTitle(firstNonEmpty(fileShortName, downloadItemName)))
	itemTitle := SafePathName(guessTitle(firstNonEmpty(downloadItemName, fileShortName)))
	isSeries, season, episode := extractSeriesMarkers(combined)
	movieLike, year := extractMovieMarkers(combined)
	extra := detectMovieExtraFolder(filePath)
	if extra != "" && itemTitle != "" {
		if isSeries {
			fn := cleanExtraFilename(fileShortName)
			folder := "Season 1"
			if season != nil {
				folder = "Season " + strconv.Itoa(*season)
			}
			return Metadata{Title: itemTitle, MediaType: "series", Years: year, Season: season, RootFolderName: buildRootFolder(itemTitle, "series", year), FolderName: folder, ExtraFolderName: extra, FileName: fn}
		}
		return Metadata{Title: itemTitle, MediaType: "movie", Years: year, RootFolderName: buildRootFolder(itemTitle, "movie", year), FolderName: extra, FileName: SafePathName(fileShortName)}
	}
	mt := "unknown"
	folder := ""
	fn := SafePathName(fileShortName)
	if isSeries {
		mt = "series"
		s := 1
		if season != nil {
			s = *season
		}
		folder = "Season " + strconv.Itoa(s)
		fn = buildSeriesFilename(title, season, episode, ext)
	} else if movieLike {
		mt = "movie"
		if year != nil {
			fn = SafePathName(title+" ("+strconv.Itoa(*year)+")") + ext
		} else {
			fn = SafePathName(title) + ext
		}
	}
	rootTitle := title
	if mt == "unknown" {
		rootTitle = SafePathName(firstNonEmpty(downloadItemName, title))
	} else {
		rootTitle = buildRootFolder(title, mt, year)
	}
	return Metadata{Title: title, MediaType: mt, Years: year, Season: season, Episode: episode, RootFolderName: rootTitle, FolderName: folder, FileName: fn}
}
func atoi(s string) int   { i, _ := strconv.Atoi(s); return i }
func pad(i, n int) string { return fmtInt(i, n) }
func fmtInt(i, n int) string {
	s := strconv.Itoa(i)
	for len(s) < n {
		s = "0" + s
	}
	return s
}
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
