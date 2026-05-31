package media

import (
	"sort"
	"strings"
	"unicode"
)

// ApplyCanonicalRootCasing rewrites MetadataRootFolderName in-place for records
// whose media type and root folder differ only by case.
func ApplyCanonicalRootCasing(records []FileRecord) {
	groups := map[string]map[string]*rootCasingCandidate{}
	sorted := append([]FileRecord(nil), records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

	for _, r := range sorted {
		if r.MetadataMediaType == "" || r.MetadataRootFolderName == "" {
			continue
		}
		groupKey := r.MetadataMediaType + "\x00" + strings.ToLower(r.MetadataRootFolderName)
		if groups[groupKey] == nil {
			groups[groupKey] = map[string]*rootCasingCandidate{}
		}
		c := groups[groupKey][r.MetadataRootFolderName]
		if c == nil {
			c = &rootCasingCandidate{name: r.MetadataRootFolderName, firstKey: r.Key}
			groups[groupKey][r.MetadataRootFolderName] = c
		}
		c.count++
	}

	canonical := map[string]string{}
	for groupKey, candidates := range groups {
		if len(candidates) < 2 {
			continue
		}
		var best *rootCasingCandidate
		for _, c := range candidates {
			if best == nil || betterRootCasing(c, best) {
				best = c
			}
		}
		canonical[groupKey] = best.name
	}

	for i := range records {
		groupKey := records[i].MetadataMediaType + "\x00" + strings.ToLower(records[i].MetadataRootFolderName)
		if name := canonical[groupKey]; name != "" {
			records[i].MetadataRootFolderName = name
		}
	}
}

type rootCasingCandidate struct {
	name     string
	firstKey string
	count    int
}

func betterRootCasing(a, b *rootCasingCandidate) bool {
	ar, br := casingClass(a.name), casingClass(b.name)
	if ar != br {
		return ar > br
	}
	as, bs := titleLikeScore(a.name), titleLikeScore(b.name)
	if as != bs {
		return as > bs
	}
	// Use count only as a tie-breaker after casing quality, so one good `Mad Men`
	// beats many bad `mad men`, but equally good variants can prefer consensus.
	if a.count != b.count {
		return a.count > b.count
	}
	return a.firstKey < b.firstKey
}

// casingClass returns: mixed case=3, all caps=2, all lower=1, no letters=0.
func casingClass(s string) int {
	hasUpper, hasLower := false, false
	for _, r := range s {
		if unicode.IsUpper(r) {
			hasUpper = true
		} else if unicode.IsLower(r) {
			hasLower = true
		}
	}
	switch {
	case hasUpper && hasLower:
		return 3
	case hasUpper:
		return 2
	case hasLower:
		return 1
	default:
		return 0
	}
}

func titleLikeScore(s string) int {
	score := 0
	atWordStart := true
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			atWordStart = true
			continue
		}
		if unicode.IsLetter(r) {
			switch {
			case atWordStart && unicode.IsUpper(r):
				score += 3
			case atWordStart && unicode.IsLower(r):
				score -= 3
			case !atWordStart && unicode.IsLower(r):
				score += 1
			case !atWordStart && unicode.IsUpper(r):
				score -= 1
			}
		}
		atWordStart = false
	}
	return score
}
