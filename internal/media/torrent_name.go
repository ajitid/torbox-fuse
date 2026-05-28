package media

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type torrentNameParts struct {
	Title   string
	Year    *int
	Season  *int
	Episode *int
}

type span struct {
	start int
	end   int
}

var (
	seasonEpisodeREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bS(\d{1,2})E(\d{1,3})\b`),
		regexp.MustCompile(`(?i)\b(\d{1,2})x(\d{1,3})\b`),
	}
	seasonOnlyRE    = regexp.MustCompile(`(?i)\bSeason[ ._-]?(\d{1,2})\b|\bS(\d{1,2})\b`)
	episodeOnlyRE   = regexp.MustCompile(`(?i)\bE(\d{1,3})\b`)
	releaseNoiseREs = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(480p|576p|720p|1080p|1440p|2160p|4k|8k|uhd|hdrip|web[- .]?dl|web[- .]?rip|bluray|blu[- .]?ray|brrip|bdrip|hdtv|remux|dvdrip)\b`),
		regexp.MustCompile(`(?i)\b(x264|x265|h[ .]?264|h[ .]?265|hevc|av1|xvid|mpeg[ .]?2)\b`),
		regexp.MustCompile(`(?i)\b(aac|aac5[ .]?1|ac[ .]?3|e[ .]?ac[ .]?3|eac3|ddp?|dts|truehd|atmos|5[ .]?1|7[ .]?1|2[ .]?0)\b`),
		regexp.MustCompile(`(?i)\b(hdr10\+?|hdr|dv|dolby[ .]?vision)\b`),
		regexp.MustCompile(`(?i)\b(8bit|10bit|12bit|10[ .-]?bit)\b`),
		regexp.MustCompile(`(?i)\b(multisub|msubs?|subbed|proper|repack|internal|limited|extended)\b`),
	}
	trailingBracketRE = regexp.MustCompile(`(?i)[\[(][^\])]{0,40}[\])]\s*$`)
	trailingGroupRE   = regexp.MustCompile(`(?i)\s+-\s*[A-Z0-9]{2,20}\s*$`)
)

func parseTorrentName(raw string) torrentNameParts {
	stem := strings.TrimSuffix(raw, filepath.Ext(raw))
	stem = strings.TrimSpace(stem)
	if stem == "" {
		return torrentNameParts{}
	}
	normalized := strings.NewReplacer(".", " ", "_", " ").Replace(stem)
	season, episode := parseSeasonEpisode(normalized)
	years := yearMatches(normalized)
	year := lastYear(normalized)

	spans := markSpans(normalized, append([]*regexp.Regexp{yearRE, seasonOnlyRE, episodeOnlyRE}, releaseNoiseREs...)...)
	for _, re := range seasonEpisodeREs {
		spans = append(spans, markSpans(normalized, re)...)
	}
	spans = append(spans, trailingNoiseSpans(stem)...)
	spans = mergeSpans(spans)

	title := firstUnmatchedTitle(stem, spans)
	if title == "" && len(years) > 0 && years[0].start == 0 {
		title = stem[years[0].start:years[0].end]
		if len(years) == 1 {
			year = nil
		}
	}

	return torrentNameParts{Title: title, Year: year, Season: season, Episode: episode}
}

func markSpans(text string, res ...*regexp.Regexp) []span {
	var spans []span
	for _, re := range res {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			if len(loc) == 2 && loc[0] < loc[1] {
				spans = append(spans, span{start: loc[0], end: loc[1]})
			}
		}
	}
	return spans
}

func mergeSpans(spans []span) []span {
	if len(spans) == 0 {
		return nil
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i].start == spans[j].start {
			return spans[i].end < spans[j].end
		}
		return spans[i].start < spans[j].start
	})
	merged := []span{spans[0]}
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s.start <= last.end {
			if s.end > last.end {
				last.end = s.end
			}
			continue
		}
		merged = append(merged, s)
	}
	return merged
}

func firstUnmatchedTitle(stem string, spans []span) string {
	spans = mergeSpans(spans)
	pos := 0
	for _, s := range spans {
		if s.start > pos {
			if title := cleanParsedTitle(stem[pos:s.start]); title != "" {
				return title
			}
		}
		if s.end > pos {
			pos = s.end
		}
	}
	if pos < len(stem) {
		return cleanParsedTitle(stem[pos:])
	}
	return ""
}

func cleanParsedTitle(s string) string {
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = strings.TrimSpace(s)
	s = strings.Trim(s, " \t\r\n-–—_.,+()[]{}")
	for {
		old := s
		s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
		s = regexp.MustCompile(`\s+[-–—]+\s*$`).ReplaceAllString(s, "")
		s = regexp.MustCompile(`\s*[\[(][\s.-]*[\])]\s*`).ReplaceAllString(s, " ")
		s = strings.Trim(s, " \t\r\n-–—_.,+()[]{}")
		if s == old {
			break
		}
	}
	if s == "" {
		return ""
	}
	return SafePathName(s)
}

func lastYear(text string) *int {
	matches := yearRE.FindAllStringSubmatch(text, -1)
	if len(matches) == 0 {
		return nil
	}
	return ptr(atoi(matches[len(matches)-1][1]))
}

func parseSeasonEpisode(text string) (season, episode *int) {
	for _, re := range seasonEpisodeREs {
		if m := re.FindStringSubmatch(text); m != nil {
			return ptr(atoi(m[1])), ptr(atoi(m[2]))
		}
	}
	if m := seasonOnlyRE.FindStringSubmatch(text); m != nil {
		if m[1] != "" {
			season = ptr(atoi(m[1]))
		} else if m[2] != "" {
			season = ptr(atoi(m[2]))
		}
	}
	if m := episodeOnlyRE.FindStringSubmatch(text); m != nil {
		episode = ptr(atoi(m[1]))
	}
	return season, episode
}

func yearMatches(text string) []span {
	var years []span
	for _, loc := range yearRE.FindAllStringIndex(text, -1) {
		years = append(years, span{start: loc[0], end: loc[1]})
	}
	return years
}

func trailingNoiseSpans(stem string) []span {
	var spans []span
	for _, re := range []*regexp.Regexp{trailingBracketRE, trailingGroupRE} {
		if loc := re.FindStringIndex(stem); loc != nil {
			content := stem[loc[0]:loc[1]]
			if re == trailingGroupRE || bracketLooksLikeReleaseNoise(content) {
				spans = append(spans, span{start: loc[0], end: loc[1]})
			}
		}
	}
	return spans
}

func bracketLooksLikeReleaseNoise(s string) bool {
	s = strings.Trim(s, " []()")
	if s == "" {
		return true
	}
	if len(s) <= 12 && regexp.MustCompile(`^[A-Za-z0-9.-]+$`).MatchString(s) {
		return true
	}
	for _, re := range releaseNoiseREs {
		if re.MatchString(s) {
			return true
		}
	}
	if _, err := strconv.Atoi(s); err == nil {
		return true
	}
	return false
}
