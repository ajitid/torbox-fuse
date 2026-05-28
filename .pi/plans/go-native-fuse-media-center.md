# Plan: Go native-FUSE TorBox Media Center

## Goal

Implement `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main` in Go in this repo (`/home/ajit/ghq/github.com/TorBox-App/torbox-rclone`) as a single read-only native FUSE media filesystem.

User decisions locked in:

- Native Go FUSE, not rclone/HTTP bridge.
- Two milestones:
  1. Correctness/minimal streaming.
  2. Bounded cache + read-ahead performance tuning.
- Persistence: single embedded bbolt DB file.
- Media organization: IntelliSeg only. Do not expose `RAW_MODE`, `ENABLE_METADATA`, or a mode selector.
- Breaking changes are okay when they simplify the Go implementation.

## Current Python behavior to port

References inspected:

- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/README.md`
- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/main.py`
- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/library/app.py`
- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/library/filesystem.py`
- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/library/http.py`
- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/library/virtual_filesystem.py`
- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/functions/appFunctions.py`
- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/functions/torboxFunctions.py`
- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/functions/databaseFunctions.py`
- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/functions/rcloneFilesystemFunctions.py`
- `/home/ajit/ghq/github.com/TorBox-App/torbox-media-center-main/functions/intelliseg.py`

Important behavior:

- Fetch cached user downloads from TorBox for `torrents`, `usenet`, and `webdl` via `/v1/api/{type}/mylist?limit=1000&offset=N&bypass_cache=true`.
- Include only video and matching subtitle MIME types.
- Build permanent request-download URLs with `token`, `{torrent_id|usenet_id|web_id}`, `file_id`, `redirect=true`.
- Resolve a fresh download URL by requesting that permanent URL and reading the redirect `Location`.
- Organize files under:
  - `/movies/<root>/<file>` or `/movies/<root>/<extra-folder>/<file>`
  - `/series/<root>/<season>/<file>` or `/series/<root>/<season>/<extra-folder>/<file>`
  - `/unknown/<root>/<file>`
- Match subtitles to already-classified videos from the same TorBox item, then place/rename subtitles next to the video.
- Refresh downloads periodically and support manual refresh if feasible.

## Web/package findings

- FUSE: `github.com/hanwen/go-fuse/v2/fs` is the recommended high-level Go FUSE API. Docs expose `NodeOpener`, `FileReader`, `NodeReaddirer`, and mount server APIs. Source: https://pkg.go.dev/github.com/hanwen/go-fuse/v2/fs and https://github.com/hanwen/go-fuse
- FUSE caveat: go-fuse docs warn about same-process access potentially tying OS threads; avoid reading the mounted path from inside the service. Source: https://pkg.go.dev/github.com/hanwen/go-fuse/v2/fuse
- Embedded DB: `go.etcd.io/bbolt` is active Bolt fork for embedded transactional key/value data. Source: https://pkg.go.dev/go.etcd.io/bbolt and https://github.com/etcd-io/bbolt
- Scheduling: `github.com/robfig/cron/v3` supports interval jobs with `@every <duration>`, but a plain `time.Ticker` is enough for fixed refresh intervals. Source: https://pkg.go.dev/github.com/robfig/cron/v3
- Torrent title parsing: `github.com/razsteinmetz/go-ptn` or `github.com/middelink/go-parse-torrent-name` parse title/year/season/episode. Source: https://github.com/razsteinmetz/go-ptn and https://pkg.go.dev/github.com/middelink/go-parse-torrent-name
- TorBox API docs exist at https://api-docs.torbox.app/; changelog confirms `/XXXX/mylist` and permanent `/requestdl?...redirect=true` URLs. Source: https://feedback.torbox.app/changelog/v713

Recommended packages:

```bash
go get github.com/hanwen/go-fuse/v2 go.etcd.io/bbolt github.com/razsteinmetz/go-ptn
```

Use stdlib for env parsing, HTTP client, logging, signals, and scheduling.

## Architecture

```text
cmd/torbox-media-center/main.go
  -> internal/config       env + validation
  -> internal/torbox       API client, pagination, redirect resolution, ranged downloads
  -> internal/media        IntelliSeg classification + subtitle matching
  -> internal/store        bbolt persistence of classified files
  -> internal/vfs          in-memory virtual tree from store records
  -> internal/fusefs       read-only go-fuse filesystem
  -> internal/refresh      initial/periodic/manual refresh orchestration
```

Data path for media read:

```text
media player reads /mount/movies/Foo/Foo.mkv offset,size
  -> FUSE Read(ctx, dest, off)
  -> torbox.ResolveDownloadURL(permanent URL), cached briefly per file
  -> HTTP GET with Range: bytes=off-off+len(dest)-1
  -> return bytes to kernel
```

Milestone 2 data path adds:

```text
Read -> block cache lookup -> missing blocks via Range GET -> optional async next-block prefetch
```

## Public env/config spec

Keep only the env variables useful to this Go implementation:

- `TORBOX_API_KEY` required.
- `MOUNT_PATH` optional, default `./torbox`.
- `MOUNT_REFRESH_TIME` optional: `slowest`, `very_slow`, `slow`, `normal`, `fast`, `ultra_fast`, `instant`; default `normal`.
- `DATA_PATH` optional, default `./torbox-media-center.db`.
- `CACHE_PATH` optional for milestone 2, default OS cache dir plus `torbox-media-center`.
- `CACHE_SIZE` optional for milestone 2, default `7GiB`.
- `READ_AHEAD` optional for milestone 2, default `600MiB` or simpler block-count equivalent.
- `FUSE_ALLOW_OTHER` optional bool, default `false`; when true set `AllowOther` and document Linux `/etc/fuse.conf` requirement.

Remove/ignore from Python design:

- `MOUNT_METHOD`
- `RCLONE_HTTP_HOST`
- `RCLONE_HTTP_PORT`
- `ENABLE_METADATA`
- `INTELLISEG`
- `RAW_MODE`

## Patch spec

Current repo only has `go.mod`, so implementation is mostly new files.

### 1. `go.mod`

Change module to a real module name and set a realistic supported Go version if local toolchain supports it. If the installed toolchain is not Go 1.26, do not keep `go 1.26.2`.

Target content shape:

```go
module github.com/TorBox-App/torbox-rclone

go 1.25

require (
    github.com/hanwen/go-fuse/v2 v2.x.x
    github.com/razsteinmetz/go-ptn v0.x.x
    go.etcd.io/bbolt v1.x.x
)
```

Use `go get ...` instead of manually pinning versions during execution.

### 2. Add `cmd/torbox-media-center/main.go`

Responsibilities:

- Initialize logger.
- Load config.
- Ensure mount path exists and is empty/not already mounted.
- Open bbolt store.
- Create TorBox client.
- Run initial refresh before mounting.
- Build in-memory VFS from store.
- Start periodic refresh ticker.
- Start optional manual Ctrl+R listener only if stdin is a TTY.
- Mount go-fuse read-only filesystem.
- Trap `SIGINT`/`SIGTERM`, unmount cleanly, close DB.

Key behavior:

- Fail fast on missing API key, invalid refresh interval, non-empty mount dir, unsupported OS, or FUSE mount failure.
- Do not add rclone fallback.

### 3. Add `internal/config/config.go`

Types:

```go
type RefreshPreset string

type Config struct {
    APIKey       string
    MountPath    string
    DataPath     string
    RefreshEvery time.Duration
    AllowOther   bool
    Version      string
    CachePath    string
    CacheSize    int64
    ReadAhead    int64
}
```

Functions:

- `Load() (Config, error)`
- `parseRefreshPreset(string) (time.Duration, error)`
- `parseBoolEnv(string) bool`
- `maskAPIKey(string) string`

Preset mapping must match Python:

- `slowest` = 24h
- `very_slow` = 12h
- `slow` = 6h
- `normal` = 3h
- `fast` = 2h
- `ultra_fast` = 1h
- `instant` = 6m, with warning in caller or config.

### 4. Add `internal/torbox/client.go`

Types:

```go
type DownloadType string
const (
    DownloadTorrent DownloadType = "torrents"
    DownloadUsenet  DownloadType = "usenet"
    DownloadWebDL   DownloadType = "webdl"
)

type Item struct { ID any; Name string; Hash string; Cached bool; Files []RemoteFile }
type RemoteFile struct { ID any; Name string; ShortName string; Size int64; MIMEType string }
type Client struct { apiKey string; http *http.Client; baseURL string; userAgent string }
```

Functions:

- `New(apiKey, version string) *Client`
- `ListDownloads(ctx context.Context, typ DownloadType) ([]Item, error)` with limit/offset pagination.
- `PermanentDownloadURL(typ DownloadType, item Item, file RemoteFile) string`.
- `ResolveDownloadURL(ctx context.Context, permanentURL string) (string, error)` using an HTTP client that does not auto-follow redirects for this call.
- `ReadRange(ctx context.Context, url string, off int64, size int) ([]byte, error)` requiring `206 Partial Content` except when reading entire tiny files from offset 0.

Need robust JSON decoding because TorBox IDs can be numeric. Use `json.RawMessage` or stringify IDs safely for URL params.

### 5. Add `internal/media/types.go`

Canonical metadata record:

```go
type FileRecord struct {
    Key string
    ItemID string
    Type torbox.DownloadType
    FolderName string
    FolderHash string
    FileID string
    FileName string
    FileSize int64
    MIMEType string
    OriginalPath string
    DownloadLink string
    Extension string

    MetadataTitle string
    MetadataMediaType string // movie, series, unknown
    MetadataYears *int
    MetadataSeason *int
    MetadataEpisode *int
    MetadataRootFolderName string
    MetadataFolderName string
    MetadataExtraFolderName string
    MetadataFileName string
    MetadataLink string
    MetadataImage string
    MetadataBackdrop string
}
```

Add helpers:

- `IsVideoMIME(mime string) bool`
- `IsSubtitleMIME(mime string) bool`
- `AcceptableMIME(mime string) bool`
- `SafePathName(string) string`

MIME sets must match Python:

- Video: `video/x-matroska`, `video/mp4`, `video/quicktime`, `video/mpeg`, `video/x-msvideo`, `video/webm`
- Subtitle: `application/x-subrip`, `text/vtt`, `text/x-ass`, `text/x-ssa`

### 6. Add `internal/media/intelliseg.go`

Port Python `functions/intelliseg.py` closely:

- `sanitizeNameForFS`
- `_guessTitle`
- `extractSeriesMarkers`
- `extractMovieMarkers`
- `buildSeriesFilename`
- `buildRootFolder`
- `cleanExtraFilename`
- `detectMovieExtraFolder`
- `Classify(downloadItemName, fileShortName, filePath string) Metadata`

Use `go-ptn` for parsed `Title`, `Season`, `Episode`, `Year`, but preserve Python regex fallbacks. If `go-ptn` is too stale or fails to build, implement minimal parser in this file and record the package rejection in the final execution notes.

### 7. Add `internal/media/process.go`

Port Python `getUserDownloads` processing:

- For each cached item, collect videos and subtitles separately.
- Process video files concurrently with worker count `max(1, runtime.NumCPU()*2-1)`.
- For each video, build `FileRecord`, classify with IntelliSeg, append to results, group by item ID.
- Process subtitles after videos:
  - `findMatchingVideo(subtitle, videos)` using series key, filename stem in subtitle path, single movie fallback.
  - `buildSubtitleMetadata(subtitle, matchedVideo)` copying video metadata and naming subtitle next to video with optional suffix.
- Return `[]FileRecord`.

### 8. Add `internal/store/store.go`

Use bbolt buckets:

- `files`: key = stable path or `<type>/<itemID>/<fileID>`, value = JSON `FileRecord`.
- `meta`: optional refresh timestamps/version.

Functions:

- `Open(path string) (*Store, error)`
- `ReplaceAll(ctx context.Context, records []media.FileRecord) error` using one write transaction.
- `All(ctx context.Context) ([]media.FileRecord, error)` using one read transaction.
- `Close() error`

Do not keep per-type JSON DB files.

### 9. Add `internal/vfs/tree.go`

Port `library/virtual_filesystem.py` non-RAW mode only.

Types:

```go
type Tree struct {
    dirs map[string][]string
    files map[string]media.FileRecord
}
```

Functions:

- `Build(records []media.FileRecord) *Tree`
- `IsDir(path string) bool`
- `IsFile(path string) bool`
- `GetFile(path string) (media.FileRecord, bool)`
- `ListDir(path string) []DirEntry`
- `Swap(records []media.FileRecord)` or build new immutable tree and atomically replace at owner level.

Rules:

- Root entries are exactly `movies`, `series`, `unknown` when applicable; okay to include all three for stable UX.
- Sort all directory entries.
- Sanitize/normalize all path components.

### 10. Add `internal/fusefs/fs.go`

Implement go-fuse read-only FS.

Types:

- `type FS struct { root fs.Inode; tree atomic.Value; torbox *torbox.Client; resolver *URLResolver }`
- Directory node implementing `Readdir`, `Lookup`, `Getattr`.
- File node implementing `Open`, `Getattr`.
- File handle implementing `Read`.

Implementation guidance:

- Use `github.com/hanwen/go-fuse/v2/fs` high-level API.
- Mount options:
  - Read-only semantics: reject writes with `EROFS`.
  - `AllowOther` only from config.
  - Set sensible `AttrTimeout`/`EntryTimeout` (e.g. 1-5s) so refreshes become visible.
- `Getattr` for files returns stable mode `0444`, size from `FileSize`, regular file type.
- `Getattr` for dirs returns mode `0555`.
- `Read` clamps size to EOF, resolves current fresh URL, performs range read, returns bytes.
- Do not read from the mounted path inside the process.

### 11. Add `internal/fusefs/url_resolver.go`

Cache resolved temporary TorBox URLs per file to reduce redirect requests.

- Key by `FileRecord.Key` or permanent `DownloadLink`.
- TTL: conservative 5 minutes initially, configurable later if TorBox URL expiry details are known.
- On range read failure that looks auth/expiry related (`401`, `403`, maybe `404`), invalidate and resolve once more.

### 12. Milestone 2: add `internal/cache/block_cache.go`

Do after minimal FUSE works.

- Fixed block size: start with 4 MiB or 8 MiB.
- Bounded disk cache under `CACHE_PATH`, max bytes `CACHE_SIZE`.
- LRU index persisted in memory; cache files named by hash(file key + block number).
- Read path composes requested bytes from cache + missing range fetches.
- Async read-ahead: when sequential read pattern detected, prefetch next N blocks up to `READ_AHEAD` budget.
- Protect against duplicate concurrent fetches with singleflight-style per-block locks. Use `golang.org/x/sync/singleflight` if desired, or a small internal map of in-flight calls.

### 13. Add tests

Files:

- `internal/media/intelliseg_test.go`
- `internal/media/process_test.go`
- `internal/vfs/tree_test.go`
- `internal/torbox/client_test.go`
- `internal/store/store_test.go`

Test cases:

- Movies with year and quality markers.
- Series `S01E02`, `1x02`, season-only, episode-only.
- Movie extras folders (`Trailers`, `Featurettes`, aliases).
- Subtitle matching by season/episode and filename stem.
- VFS paths for movie/series/unknown.
- TorBox pagination and redirect resolution using `httptest.Server`.
- bbolt `ReplaceAll` transaction and `All` round-trip.

### 14. Add operational files/docs

- Rewrite `README.md` for Go native-FUSE usage.
- Add `.env.example` with only supported env vars.
- Add `dockerfile` or `Dockerfile` for static-ish Go build plus FUSE runtime requirements.
- Add `docker-compose.yaml` matching native FUSE requirements:
  - `/dev/fuse`
  - `SYS_ADMIN`
  - `apparmor:unconfined`
  - mount propagation `rshared`
- Optionally add `.gitignore` for binary, DB, cache, mount dir.

## Execution phases / todos file

When implementing, create `.pi/todos/go-native-fuse-media-center.md` with these phases and checkboxes:

1. Project skeleton and dependencies.
2. Config + TorBox client.
3. IntelliSeg + processing pipeline.
4. bbolt store + VFS tree.
5. Minimal read-only FUSE mount.
6. Refresh orchestration + signal cleanup.
7. Tests and docs.
8. Milestone 2 cache/read-ahead.

## Verification commands

Run during execution:

```bash
go test ./...
go vet ./...
go run ./cmd/torbox-media-center --help  # if CLI flags are added
```

Manual verification with a real TorBox API key:

```bash
TORBOX_API_KEY=... MOUNT_PATH=/tmp/torbox-test DATA_PATH=/tmp/torbox-test.db go run ./cmd/torbox-media-center
find /tmp/torbox-test -maxdepth 4 -type f | head
ffprobe /tmp/torbox-test/...  # or play via VLC/Jellyfin/Plex scan
```

FUSE cleanup checks:

```bash
mount | grep torbox-test || true
fusermount3 -u /tmp/torbox-test || true
```

## Risks and mitigations

- Native FUSE may initially stream worse than rclone. Mitigation: milestone 1 correctness first, milestone 2 block cache/read-ahead.
- macOS support may need extra testing because go-fuse maintainers call out macOS concurrency/performance caveats. Mitigation: Linux-first plan; document macFUSE as experimental until tested.
- TorBox temporary URL expiry is not documented in inspected sources. Mitigation: short resolver TTL and one forced re-resolve on auth/expiry read errors.
- Media parser package may not match Python `parse-torrent-title` exactly. Mitigation: keep regex fallbacks and tests based on existing Python IntelliSeg behavior.
- FUSE `AllowOther` can fail unless `/etc/fuse.conf` enables `user_allow_other`. Mitigation: default false and fail with clear error when explicitly enabled but unsupported.

## Non-goals

- No TorBox metadata search API.
- No raw original hierarchy mode.
- No rclone backend/fallback.
- No write/delete/rename support in the mounted filesystem.
- No Windows support in the initial native FUSE implementation.
