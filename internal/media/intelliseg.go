package media

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const sampleMaxBytes int64 = 50 * 1024 * 1024

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
func titleWithYear(title string, year *int) string {
	if year != nil {
		return SafePathName(title + " (" + strconv.Itoa(*year) + ")")
	}
	return SafePathName(title)
}
func buildSeriesFilename(title string, year, season, episode *int, ext string) string {
	base := titleWithYear(title, year)
	suffix := "Episode"
	if season != nil && episode != nil {
		suffix = "s" + pad(*season, 2) + "e" + pad(*episode, 2)
	} else if season != nil {
		suffix = "s" + pad(*season, 2)
	} else if episode != nil {
		suffix = "e" + pad(*episode, 2)
	}
	return SafePathName(base+" - "+suffix) + ext
}
func buildRootFolder(title, mt string, year *int) string {
	if (mt == "movie" || mt == "series") && year != nil {
		return SafePathName(title + " (" + strconv.Itoa(*year) + ")")
	}
	return SafePathName(title)
}
func cleanExtraFilename(name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	stem = regexp.MustCompile(`(?i)^Season[ ._-]?\d{1,2}[ ._-]*`).ReplaceAllString(stem, "")
	stem = strings.Trim(stem, " ._-")
	return SafePathName(stem) + ext
}
func isSmallSample(fileShortName string, fileSize int64) bool {
	stem := strings.TrimSuffix(fileShortName, filepath.Ext(fileShortName))
	return fileSize < sampleMaxBytes && strings.HasPrefix(strings.ToLower(stem), "sample")
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

var movieEditionAliases = []struct{ alias, canonical string }{
	{"director's cut", "Director's Cut"},
	{"directors cut", "Director's Cut"},
	{"theatrical cut", "Theatrical"},
	{"extended cut", "Extended Cut"},
	{"special edition", "Special Edition"},
	{"final cut", "Final Cut"},
	{"theatrical", "Theatrical"},
	{"extended", "Extended"},
	{"unrated", "Unrated"},
	{"uncut", "Uncut"},
	{"3d", "3D"},
}

func detectMovieEdition(text string) string {
	norm := strings.ToLower(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(text))
	norm = spaces.ReplaceAllString(norm, " ")
	for _, edition := range movieEditionAliases {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(edition.alias) + `\b`)
		if re.MatchString(norm) {
			return edition.canonical
		}
	}
	return ""
}

func movieBaseWithEdition(title string, year *int, edition string) string {
	base := buildRootFolder(title, "movie", year)
	if edition != "" {
		base = SafePathName(base + " {edition-" + edition + "}")
	}
	return base
}

func Classify(downloadItemName, fileShortName, filePath string, fileSize int64) Metadata {
	ext := filepath.Ext(fileShortName)
	combined := strings.Join([]string{downloadItemName, fileShortName, filePath}, " ")
	parsed := parseTorrentName(firstNonEmpty(fileShortName, downloadItemName))
	itemParsed := parseTorrentName(firstNonEmpty(downloadItemName, fileShortName))

	title := SafePathName(firstNonEmpty(parsed.Title, guessTitle(firstNonEmpty(fileShortName, downloadItemName))))
	itemTitle := SafePathName(firstNonEmpty(itemParsed.Title, guessTitle(firstNonEmpty(downloadItemName, fileShortName))))

	isSeries, season, episode := extractSeriesMarkers(combined)
	movieLike, year := extractMovieMarkers(combined)

	if parsed.Season != nil {
		season = parsed.Season
		isSeries = true
	}
	if parsed.Episode != nil {
		episode = parsed.Episode
		isSeries = true
	}
	if parsed.Year != nil {
		year = parsed.Year
	}
	if itemParsed.Year != nil {
		year = itemParsed.Year
	}

	if isSmallSample(fileShortName, fileSize) && itemTitle != "" {
		if isSeries {
			folder := ""
			if season != nil {
				folder = "Season " + pad(*season, 2)
			}
			return Metadata{Title: itemTitle, MediaType: "series", Years: year, Season: season, RootFolderName: buildRootFolder(itemTitle, "series", year), FolderName: folder, ExtraFolderName: "Other", FileName: SafePathName(fileShortName)}
		}
		base := movieBaseWithEdition(itemTitle, year, detectMovieEdition(combined))
		return Metadata{Title: itemTitle, MediaType: "movie", Years: year, RootFolderName: base, FolderName: "Other", FileName: SafePathName(fileShortName)}
	}

	extra := detectMovieExtraFolder(filePath)
	if extra != "" && itemTitle != "" {
		if isSeries {
			fn := cleanExtraFilename(fileShortName)
			folder := "Season 01"
			if season != nil {
				folder = "Season " + pad(*season, 2)
			}
			return Metadata{Title: itemTitle, MediaType: "series", Years: year, Season: season, RootFolderName: buildRootFolder(itemTitle, "series", year), FolderName: folder, ExtraFolderName: extra, FileName: fn}
		}
		base := movieBaseWithEdition(itemTitle, year, detectMovieEdition(combined))
		return Metadata{Title: itemTitle, MediaType: "movie", Years: year, RootFolderName: base, FolderName: extra, FileName: SafePathName(fileShortName)}
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
		folder = "Season " + pad(s, 2)
		fn = buildSeriesFilename(title, year, season, episode, ext)
	} else if movieLike {
		mt = "movie"
		base := movieBaseWithEdition(title, year, detectMovieEdition(combined))
		fn = base + ext
	}
	rootTitle := title
	if mt == "unknown" {
		rootTitle = SafePathName(firstNonEmpty(downloadItemName, title))
	} else if mt == "movie" {
		rootTitle = strings.TrimSuffix(fn, ext)
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
