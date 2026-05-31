# Plan: merge media root folders that differ only by case

## Problem

The mounted tree currently has both:

- `/srv/torbox/series/mad men`
- `/srv/torbox/series/Mad Men`

They are distinct FUSE directories because VFS paths are built directly from `FileRecord.MetadataRootFolderName` and the maps in `internal/vfs/tree.go` are case-sensitive.

Confirmed current mount state:

- `mad men` and `Mad Men` have different inodes.
- `mad men` contains `Season 05` files.
- `Mad Men` contains seasons 01, 03, 04.

Relevant code paths:

- Classification creates root names in `internal/media/intelliseg.go` (`Classify`, `buildRootFolder`).
- Records are produced in `internal/media/process.go` (`ProcessItems`).
- Plex duplicate version naming runs in `internal/media/plex_naming.go` (`ApplyPlexVersionNaming`).
- VFS paths are derived in `internal/vfs/tree.go` (`RecordPath`, `Build`).
- Refresh stores processed records in `internal/refresh/refresh.go`.

Baseline check run:

```sh
go test ./internal/media ./internal/vfs
```

passed before changes.

## Decisions locked

- Normalize during media processing, before storing metadata, not only inside VFS.
- Case-only root folder conflicts should merge into one canonical `MetadataRootFolderName`.
- Canonical casing preference should be human-looking casing:
  1. mixed case, e.g. `Mad Men`, `The Man in a Dying Castle`
  2. all caps, e.g. `MAD MEN`, `S.T.A.L.K.E.R.`
  3. all lower, e.g. `mad men`

## Non-goals

- Do not merge names that differ by more than case (`Mad Men` vs `Mad-Men`, `Mad Men (2007)` vs `Mad Men`).
- Do not add a VFS fallback alias for the old casing. This is intended to remove the split directory.
- Do not change title parsing heuristics in `Classify`; this is a post-processing canonicalization step.

## Implementation patch spec

### 1. Add canonical root casing helper

Create a new file:

`internal/media/canonical_case.go`

Add package `media` code with these functions:

```go
package media

import (
    "sort"
    "strings"
    "unicode"
)

// ApplyCanonicalRootCasing rewrites MetadataRootFolderName in-place for records
// whose media type and root folder differ only by case.
func ApplyCanonicalRootCasing(records []FileRecord) {
    type candidate struct {
        name     string
        firstKey string
        count    int
    }

    groups := map[string]map[string]*candidate{}
    sorted := append([]FileRecord(nil), records...)
    sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })

    for _, r := range sorted {
        if r.MetadataMediaType == "" || r.MetadataRootFolderName == "" {
            continue
        }
        groupKey := r.MetadataMediaType + "\x00" + strings.ToLower(r.MetadataRootFolderName)
        if groups[groupKey] == nil {
            groups[groupKey] = map[string]*candidate{}
        }
        c := groups[groupKey][r.MetadataRootFolderName]
        if c == nil {
            c = &candidate{name: r.MetadataRootFolderName, firstKey: r.Key}
            groups[groupKey][r.MetadataRootFolderName] = c
        }
        c.count++
    }

    canonical := map[string]string{}
    for groupKey, candidates := range groups {
        if len(candidates) < 2 {
            continue
        }
        var best *candidate
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

func betterRootCasing(a, b *candidate) bool {
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
```

Notes for implementer:

- Keep grouping key as `mediaType + lower(root)`, so a movie and series with the same case-insensitive root do not affect each other.
- Use exact `strings.ToLower` only; do not normalize punctuation/spacing.
- The helper mutates `records` in-place like `ApplyPlexVersionNaming` does.

### 2. Call helper before Plex version naming

Edit `internal/media/process.go` in `ProcessItems`.

Find:

```go
	ApplyPlexVersionNaming(records)
```

Replace with:

```go
	ApplyCanonicalRootCasing(records)
	ApplyPlexVersionNaming(records)
```

Reason: case-only roots must be merged before duplicate episode/movie path detection. If `mad men` season 5 contains an episode that also exists under `Mad Men`, Plex version naming should see the records as one root.

### 3. Add unit tests for canonical root casing

Create:

`internal/media/canonical_case_test.go`

Add tests covering:

1. Mixed case beats lower and all-caps:

```go
func TestApplyCanonicalRootCasingPrefersMixedCase(t *testing.T) {
    recs := []FileRecord{
        {Key: "2", MetadataMediaType: "series", MetadataRootFolderName: "mad men", MetadataFolderName: "Season 05", MetadataFileName: "mad men - s05e01.mkv"},
        {Key: "1", MetadataMediaType: "series", MetadataRootFolderName: "MAD MEN", MetadataFolderName: "Season 02", MetadataFileName: "MAD MEN - s02e01.mkv"},
        {Key: "3", MetadataMediaType: "series", MetadataRootFolderName: "Mad Men", MetadataFolderName: "Season 01", MetadataFileName: "Mad Men - s01e01.mkv"},
    }
    ApplyCanonicalRootCasing(recs)
    for _, r := range recs {
        if r.MetadataRootFolderName != "Mad Men" {
            t.Fatalf("root = %q, want Mad Men", r.MetadataRootFolderName)
        }
    }
}
```

2. All-caps beats all-lower when no mixed-case candidate exists:

```go
func TestApplyCanonicalRootCasingPrefersAllCapsOverLower(t *testing.T) {
    recs := []FileRecord{
        {Key: "1", MetadataMediaType: "series", MetadataRootFolderName: "s.t.a.l.k.e.r.", MetadataFileName: "e1.mkv"},
        {Key: "2", MetadataMediaType: "series", MetadataRootFolderName: "S.T.A.L.K.E.R.", MetadataFileName: "e2.mkv"},
    }
    ApplyCanonicalRootCasing(recs)
    for _, r := range recs {
        if r.MetadataRootFolderName != "S.T.A.L.K.E.R." {
            t.Fatalf("root = %q, want S.T.A.L.K.E.R.", r.MetadataRootFolderName)
        }
    }
}
```

3. Different media types are not merged:

```go
func TestApplyCanonicalRootCasingDoesNotCrossMediaTypes(t *testing.T) {
    recs := []FileRecord{
        {Key: "1", MetadataMediaType: "movie", MetadataRootFolderName: "Mad Men", MetadataFileName: "Mad Men.mkv"},
        {Key: "2", MetadataMediaType: "series", MetadataRootFolderName: "mad men", MetadataFileName: "mad men - s01e01.mkv"},
    }
    ApplyCanonicalRootCasing(recs)
    if recs[0].MetadataRootFolderName != "Mad Men" || recs[1].MetadataRootFolderName != "mad men" {
        t.Fatalf("unexpected roots: %#v", recs)
    }
}
```

4. Non-case-only names are not merged:

```go
func TestApplyCanonicalRootCasingOnlyCaseInsensitiveExactMatches(t *testing.T) {
    recs := []FileRecord{
        {Key: "1", MetadataMediaType: "series", MetadataRootFolderName: "Mad Men", MetadataFileName: "a.mkv"},
        {Key: "2", MetadataMediaType: "series", MetadataRootFolderName: "Mad-Men", MetadataFileName: "b.mkv"},
    }
    ApplyCanonicalRootCasing(recs)
    if recs[0].MetadataRootFolderName != "Mad Men" || recs[1].MetadataRootFolderName != "Mad-Men" {
        t.Fatalf("unexpected roots: %#v", recs)
    }
}
```

### 4. Add integration-style VFS/process test

Add to `internal/vfs/tree_test.go` or a new `internal/media/process_canonical_test.go`.

Preferred small test in `internal/vfs/tree_test.go`:

```go
func TestBuildTreeAfterCanonicalRootCasingMergesCaseOnlySeriesRoots(t *testing.T) {
    recs := []media.FileRecord{
        {Key: "k1", MetadataMediaType: "series", MetadataRootFolderName: "Mad Men", MetadataFolderName: "Season 01", MetadataFileName: "Mad Men - s01e01.mkv"},
        {Key: "k2", MetadataMediaType: "series", MetadataRootFolderName: "mad men", MetadataFolderName: "Season 05", MetadataFileName: "mad men - s05e01.mkv"},
    }
    media.ApplyCanonicalRootCasing(recs)
    tr := Build(recs)
    if tr.IsDir("/series/mad men") {
        t.Fatalf("unexpected lower-case split root")
    }
    if !tr.IsDir("/series/Mad Men/Season 01") || !tr.IsDir("/series/Mad Men/Season 05") {
        t.Fatalf("missing merged Mad Men seasons")
    }
}
```

### 5. Verification

Run:

```sh
go test ./internal/media ./internal/vfs
```

Then run the full suite:

```sh
go test ./...
```

Manual verification after refresh/restart:

```sh
find /srv/torbox/series -maxdepth 1 -type d | grep -i '/mad men$' | sort
find /srv/torbox/series/'Mad Men' -maxdepth 2 -mindepth 1 -printf '%P\n' | sort | head -100
```

Expected:

- only `/srv/torbox/series/Mad Men` appears for case-insensitive `mad men`
- `Season 05` appears under `/srv/torbox/series/Mad Men`

## Risks / edge cases

- Existing DB contents will not change until the next refresh writes processed records again. If immediate cleanup is needed, run a refresh or restart path that triggers refresh.
- If two records become the exact same VFS path after root canonicalization, `ApplyPlexVersionNaming` should catch video duplicates because the helper runs before it. Existing subtitle behavior remains unchanged.
- This plan intentionally does not provide old-case aliases (`/series/mad men`), so consumers using the old lower-case path must switch to the canonical path.
