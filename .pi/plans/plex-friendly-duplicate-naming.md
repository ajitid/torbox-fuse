# Plan: Plex-friendly duplicate/version naming for IntelliSeg/VFS

## Goal

Change IntelliSeg/media processing so duplicate movie torrents or duplicate TV episode torrents are never hidden by identical virtual paths, and so the resulting paths follow Plex naming expectations.

User decisions locked:

- Apply Plex-friendly naming consistently for all movie/series paths, even if this is a breaking path change.
- Duplicate/version labels use: parsed technical tokens → cleaned release/file stem → stable ID.
- TV alternate cuts/editions are treated as same-episode versions, not Plex `{edition-...}`.
- If a collision still remains, append a short stable ID automatically.

## References

Repo files inspected:

- `internal/media/intelliseg.go` — current classification and generated metadata paths.
- `internal/media/process.go` — video/subtitle record creation; subtitle records copy video metadata.
- `internal/vfs/tree.go` — final virtual paths; path collision currently overwrites `t.files[p]`.
- `internal/media/types.go` — `FileRecord`, `Metadata`, `SafePathName`.
- `internal/media/torrent_name.go` — existing release/title parsing helpers.

Plex references:

- Movies/multiple versions: <https://support.plex.tv/articles/200381043-multi-version-movies/>
- Movie editions: <https://support.plex.tv/articles/multiple-editions/>
- Movie naming: <https://support.plex.tv/articles/naming-and-organizing-your-movie-media-files/>
- TV naming: <https://support.plex.tv/articles/naming-and-organizing-your-tv-show-files/>

Baseline verification:

- `go test ./...` passes before changes.

## Desired output examples

### Single movie

```text
/movies/Dune (2021)/Dune (2021).mkv
```

### Multiple versions of same movie

```text
/movies/Dune (2021)/Dune (2021) - 1080p BluRay x264.mkv
/movies/Dune (2021)/Dune (2021) - 2160p WEB-DL HEVC.mkv
```

### Movie edition

```text
/movies/Blade Runner (1982) {edition-Director's Cut}/Blade Runner (1982) {edition-Director's Cut}.mkv
```

If there are multiple versions of that edition:

```text
/movies/Blade Runner (1982) {edition-Director's Cut}/Blade Runner (1982) {edition-Director's Cut} - 1080p h264.mkv
/movies/Blade Runner (1982) {edition-Director's Cut}/Blade Runner (1982) {edition-Director's Cut} - 2160p HEVC.mkv
```

### Single TV episode

```text
/series/Arcane (2021)/Season 01/Arcane (2021) - s01e01.mkv
```

### Multiple versions of same TV episode

```text
/series/Arcane (2021)/Season 01/Arcane (2021) - s01e01 - 1080p WEB-DL H264.mkv
/series/Arcane (2021)/Season 01/Arcane (2021) - s01e01 - 2160p WEB-DL HEVC.mkv
```

## Design

Keep `Classify` responsible for base media classification and Plex-style base names. Add a separate post-processing pass over video `FileRecord`s to add Plex version labels only when there are multiple records for the same underlying Plex item.

Reason: duplicate/version detection requires seeing all records globally; `Classify` only sees one file at a time.

High-level flow after changes:

1. Build all video records using `BuildVideoRecord` / `Classify`.
2. Apply global Plex naming/version deconfliction to video records.
3. Build `byItem` from renamed video records.
4. Build subtitles by copying renamed video metadata, so subtitles match their video basename.
5. Sort final records.

## Patch spec

### 1. Update base Plex naming in `internal/media/intelliseg.go`

#### Change `buildSeriesFilename`

Current signature:

```go
func buildSeriesFilename(title string, season, episode *int, ext string) string
```

Replace with:

```go
func buildSeriesFilename(title string, year, season, episode *int, ext string) string
```

Behavior:

- Base title should include year when present: `Show (2021)`.
- Filename format should be Plex TV style:
  - season+episode: `Show (2021) - s01e02.ext`
  - season only: `Show (2021) - s01.ext` or keep `Show (2021) - Season 01.ext` if preferred for season packs; tests should lock one. Recommended: `Show (2021) - s01.ext`.
  - episode only: `Show (2021) - e02.ext`.
  - neither: `Show (2021) - Episode.ext`.
- Use lowercase `s`/`e` as in Plex examples.

Suggested implementation:

```go
func titleWithYear(title string, year *int) string {
    if year != nil {
        return SafePathName(title + " (" + strconv.Itoa(*year) + ")")
    }
    return SafePathName(title)
}

func buildSeriesFilename(title string, year, season, episode *int, ext string) string {
    base := titleWithYear(title, year)
    suffix := "Episode"
    switch {
    case season != nil && episode != nil:
        suffix = "s" + pad(*season, 2) + "e" + pad(*episode, 2)
    case season != nil:
        suffix = "s" + pad(*season, 2)
    case episode != nil:
        suffix = "e" + pad(*episode, 2)
    }
    return SafePathName(base+" - "+suffix) + ext
}
```

#### Change series season folders

In `Classify`, when setting season folder:

```go
folder = "Season " + strconv.Itoa(s)
```

Replace with padded Plex folder:

```go
folder = "Season " + pad(s, 2)
```

Also in movie-extra + series branch:

```go
folder := "Season 1"
...
folder = "Season " + strconv.Itoa(*season)
```

Replace with `Season 01` / `Season ` + `pad(*season, 2)`.

#### Include year in series root

`buildRootFolder(title, "series", year)` currently returns only title for series. For Plex TV Series agent, include year when known.

Either update `buildRootFolder`:

```go
func buildRootFolder(title, mt string, year *int) string {
    if year != nil && (mt == "movie" || mt == "series") {
        return SafePathName(title + " (" + strconv.Itoa(*year) + ")")
    }
    return SafePathName(title)
}
```

or add a separate `titleWithYear` and call it for series roots. Recommended: update `buildRootFolder` as above.

#### Call new `buildSeriesFilename`

Replace:

```go
fn = buildSeriesFilename(title, season, episode, ext)
```

with:

```go
fn = buildSeriesFilename(title, year, season, episode, ext)
```

#### Movie edition detection

Add helper in `intelliseg.go` or new `plex_naming.go`:

```go
var movieEditionAliases = map[string]string{
    "director's cut": "Director's Cut",
    "directors cut": "Director's Cut",
    "final cut": "Final Cut",
    "theatrical": "Theatrical",
    "theatrical cut": "Theatrical",
    "extended": "Extended",
    "extended cut": "Extended Cut",
    "special edition": "Special Edition",
    "unrated": "Unrated",
    "uncut": "Uncut",
    "3d": "3D",
}
```

Add function:

```go
func detectMovieEdition(text string) string
```

Implementation notes:

- Normalize separators `.`, `_`, `-` to spaces and lowercase.
- Match aliases by word boundaries where possible.
- Return canonical edition string or empty.

In `Classify`, after media type has been determined as movie, apply edition only for movie records:

- For movie extras branch: root should be `Movie (Year) {edition-X}` if edition detected.
- For normal movie branch: same.
- Filename should include `{edition-X}` before extension:

```go
base := buildRootFolder(title, "movie", year)
if edition != "" {
    base = SafePathName(base + " {edition-" + edition + "}")
}
fn = base + ext
rootTitle = base
```

Do **not** apply movie edition tags to TV series.

### 2. Add global Plex version labeling in a new file `internal/media/plex_naming.go`

Create file with public/internal function:

```go
func ApplyPlexVersionNaming(records []FileRecord)
```

It mutates only video records (`IsVideoMIME(r.MIMEType)`) and only records with `MetadataMediaType` `movie` or `series`.

#### Grouping

Group videos by underlying Plex item identity:

Movie key:

```text
movie|MetadataRootFolderName
```

This intentionally includes `{edition-X}` in root, so different movie editions do not get merged as versions of each other.

Series key:

```text
series|MetadataRootFolderName|MetadataFolderName|season|episode
```

Use `MetadataSeason`/`MetadataEpisode`; fallback to existing metadata folder/filename if nil.

#### Version label generation

Add helper:

```go
func plexVersionLabel(r FileRecord) string
```

Strategy:

1. Parse technical tokens from combined `FolderName`, `FileName`, `OriginalPath`, `MetadataFileName`.
2. If enough tokens exist, use compact technical label.
3. Else use cleaned release/file stem.
4. If empty, use short stable ID.

Technical token categories to parse:

- Resolution: `480p`, `576p`, `720p`, `1080p`, `1440p`, `2160p`, `4K`, `8K`.
- Source: `BluRay`, `BRRip`, `BDRip`, `WEB-DL`, `WEBRip`, `HDTV`, `DVDRip`, `Remux`.
- Codec: `x264`, `x265`, `H264`, `H265`, `HEVC`, `AV1`, `XviD`.
- HDR/video profile optional: `HDR`, `HDR10`, `HDR10+`, `DV`, `Dolby Vision`.
- Audio optional if easy: `AAC`, `AC3`, `EAC3`, `DTS`, `TrueHD`, `Atmos`.

Canonicalize spelling:

- `web dl`, `web-dl`, `web.dl` → `WEB-DL`
- `h.264`, `h264` → `H264`
- `h.265`, `h265` → `H265`
- `x265` stays `x265`, `hevc` → `HEVC`

Clean release/file stem fallback:

- Prefer `FolderName` when not equal to generic title/root; otherwise `FileName` stem; otherwise `OriginalPath` stem.
- Strip extension.
- Replace separators with spaces.
- `SafePathName`.
- Avoid producing a label identical to the movie/show base title.

Stable ID helper:

```go
func shortStableID(r FileRecord) string
```

Use SHA-1 or FNV over `r.Key` and return 8 lowercase hex chars.

#### Applying labels

For each group:

- If group length is 1: keep base filename without version label.
- If group length > 1:
  - Sort group entries by `Key` for deterministic output.
  - Compute base filename stem from existing `MetadataFileName` without extension.
  - Generate desired filename: `stem + " - " + label + ext`.
  - If label repeats within group, fallback to cleaned release stem.
  - If still repeats, append ` - <shortStableID>`.
  - Ensure final full VFS paths are unique.

Important: use each record's own extension; duplicate versions can be `.mkv` and `.mp4`.

### 3. Update `internal/media/process.go` to apply global naming before subtitles

Current code builds `byItem` while consuming `out`:

```go
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
```

Replace with:

```go
var records []FileRecord
for r := range out {
    records = append(records, r)
}
ApplyPlexVersionNaming(records)

byItem := map[string][]FileRecord{}
for _, r := range records {
    byItem[r.ItemID] = append(byItem[r.ItemID], r)
}
for _, p := range subs {
    if m, ok := findMatchingVideo(p.file, byItem[p.item.ID]); ok {
        records = append(records, BuildSubtitleRecord(c, typ, p.item, p.file, m))
    }
}
```

Rationale: subtitles should inherit the final Plex-renamed video filename.

### 4. Keep `internal/vfs/tree.go` simple, but consider adding collision protection tests

No functional change required if `ApplyPlexVersionNaming` guarantees uniqueness. However, add tests to catch accidental overwrites at media-processing level.

Optional hardening: in `Tree.addFile`, if a path already exists, append stable ID there. Not recommended because `vfs` lacks enough semantic context and would produce less Plex-friendly paths. Keep deconfliction in `media`.

### 5. Update tests

#### `internal/media/intelliseg_test.go`

Update expected series metadata:

- `RootFolderName`: include year when available.
- `FolderName`: `Season 01`, not `Season 1`.
- `FileName`: `Show (Year) - s01e02.ext`, not `Show S01E02.ext`.

Add tests:

1. `Classify` movie edition:

```go
m := Classify("Blade Runner 1982 Directors Cut 1080p", "Blade.Runner.1982.Directors.Cut.mkv", "Blade.Runner.1982.Directors.Cut.mkv")
want root/file contain `Blade Runner (1982) {edition-Director's Cut}`
```

2. `Classify` does not apply edition tag to series:

```go
Classify("Some Show Extended S01E01 1080p", "Some.Show.S01E01.Extended.mkv", ...)
```

Expect `MediaType == "series"` and no `{edition-` in root/file.

#### New `internal/media/plex_naming_test.go`

Add table/unit tests for `ApplyPlexVersionNaming`:

1. Single movie unchanged:

```text
Dune (2021).mkv
```

2. Duplicate movies get labels:

Input two video records with same metadata root/file but different `FolderName` or `FileName` containing `1080p BluRay x264` and `2160p WEB-DL HEVC`.

Expect:

```text
Dune (2021) - 1080p BluRay x264.mkv
Dune (2021) - 2160p WEB-DL HEVC.mkv
```

3. Duplicate movies with same technical labels get stable IDs or release-stem fallback, and both final filenames differ.

4. Duplicate TV episodes:

Expect:

```text
Arcane (2021) - s01e01 - 1080p WEB-DL H264.mkv
Arcane (2021) - s01e01 - 2160p WEB-DL HEVC.mkv
```

5. Different movie editions are not grouped as versions when root folders differ:

```text
Blade Runner (1982)
Blade Runner (1982) {edition-Director's Cut}
```

Both remain unlabelled if each edition has a single file.

#### `internal/vfs/tree_test.go`

Update expected paths if series season folder/name changed. Add test with two duplicate movie records after applying `ApplyPlexVersionNaming` and verify `Tree` contains both unique file paths.

#### `cmd/torbox-fuse/control_test.go`

Update expectations for any hardcoded series/movie filenames if affected. Existing movie paths may remain mostly unchanged except edition/version cases.

### 6. Verification

Run:

```bash
go test ./...
```

Also run targeted tests while developing:

```bash
go test ./internal/media -run 'Classify|PlexVersion'
go test ./internal/vfs
```

Manual sanity script optional:

- Construct several `FileRecord`s in a small Go snippet or test and print `vfs.RecordPath` before/after `ApplyPlexVersionNaming`.
- Confirm no duplicate paths for groups of movie versions and TV episode versions.

## Notes / edge cases

- This is intentionally a breaking path change for TV series because season folders become `Season 01` and filenames become `Show (Year) - s01e01.ext`.
- Plex does not officially support TV editions, so `{edition-...}` must stay movie-only.
- Plex warns against split-file naming (`pt1`, `part1`) except for actual split media; do not use those labels for duplicate versions.
- If a record lacks a year, keep paths valid without year: `Show/Season 01/Show - s01e01.ext` or `Movie/Movie.ext`.
- If a duplicate group has files with different extensions but same label, extension alone can make paths unique; however, still prefer unique labels when labels collide so filenames are informative.
