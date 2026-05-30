# Plan: add `PLEX_ACCESS_TOKEN` path-based Plex library refresh

## Goal

After torbox-fuse successfully refreshes the TorBox mount contents, optionally tell Plex to scan the mounted TorBox media paths too.

User-selected behavior:

- Use `PLEX_ACCESS_TOKEN` for Plex API auth.
- Optional integration:
  - If `PLEX_ACCESS_TOKEN` is unset, do nothing.
  - If Plex calls fail, do not fail TorBox refresh and do not log warnings.
- Refresh specific Plex paths derived from `MOUNT_PATH`:
  - `${MOUNT_PATH}/movies`
  - `${MOUNT_PATH}/series`
- Discover Plex section keys dynamically from Plex `/library/sections`; do not hard-code local section ids.

## Verified facts / references

- Local Plex server is reachable at `http://127.0.0.1:32400`.
- The provided Plex token was verified without printing it:
  - `GET /library/sections` returned 200.
  - `POST /butler/RefreshLibraries` returned 200.
- Local Plex libraries found:
  - Movies key `3`, locations `/srv/torbox/movies`, `/mnt/ajit-files/plex-local/movies`
  - TV Shows key `4`, locations `/srv/torbox/series`, `/mnt/ajit-files/plex-local/tv-shows`
  - Music key `5`, location `/mnt/ajit-files/plex-local/music`
- Current `.env` has `MOUNT_PATH=/srv/torbox`, so derived paths match Plex locations.
- Plex API references:
  - Plex Support URL commands: `GET /library/sections/{id}/refresh?path=...&X-Plex-Token=...` — https://support.plex.tv/articles/201638786-plex-media-server-url-commands/
  - Plexopedia partial scan: `GET http://{ip}:32400/library/sections/{id}/refresh?path={folder}&X-Plex-Token={token}` — https://www.plexopedia.com/plex-media-server/api/library/scan-partial/
  - Plexopedia Butler task: `POST /butler/RefreshLibraries` exists, but user chose path-based refresh — https://www.plexopedia.com/plex-media-server/api/server/task-refresh-libraries/

## Design

Add a small internal Plex package that:

1. Reads optional config from `config.Config`:
   - `PLEX_ACCESS_TOKEN` — optional, no default.
   - `PLEX_BASE_URL` — optional, default `http://127.0.0.1:32400`.
2. Builds scan paths from the configured mount path:
   - `filepath.Join(cfg.MountPath, "movies")`
   - `filepath.Join(cfg.MountPath, "series")`
3. On refresh:
   - If token is empty, return immediately.
   - `GET {base}/library/sections` with `X-Plex-Token` as a query param or header.
   - Parse XML `Directory` elements and their `Location path` values.
   - For each desired scan path, find the section whose location equals that path, or contains it as a child/prefix match if useful.
   - Call `GET {base}/library/sections/{key}/refresh?path={scanPath}` with token.
   - Ignore all Plex errors by returning no error from the integration call.
4. Wire it after FUSE root is updated:
   - `mgr.Run` updates bbolt.
   - `root.Swap(recs)` applies refreshed records to the mount.
   - Then call Plex scan silently.
5. Also trigger once after initial mount:
   - initial `mgr.Run` happens before `root.Mount`, so call Plex after successful mount, when the mount exists.

## Patch spec

### 1. `internal/config/config.go`

Add fields to `Config`:

```go
PlexAccessToken string
PlexBaseURL     string
```

In `Load()`, read:

```go
plexAccessToken := strings.TrimSpace(os.Getenv("PLEX_ACCESS_TOKEN"))
```

In returned `Config`, add:

```go
PlexAccessToken: plexAccessToken,
PlexBaseURL:     envDefault("PLEX_BASE_URL", "http://127.0.0.1:32400"),
```

Do not include the Plex token in startup logs.

### 2. New file `internal/plex/client.go`

Create package `plex` with roughly:

```go
package plex

type Client struct {
    baseURL string
    token   string
    http    *http.Client
}

func New(baseURL, token string) *Client
func (c *Client) Enabled() bool
func (c *Client) RefreshMountPaths(ctx context.Context, mountPath string)
```

Implementation details:

- `Enabled()` returns `strings.TrimSpace(c.token) != ""`.
- `RefreshMountPaths` intentionally returns no error.
- Use `context.WithTimeout(ctx, 15*time.Second)` internally so Plex cannot hang refresh flows.
- Normalize `baseURL` by trimming trailing slash.
- Use `net/url` to build URLs.
- Use query param `X-Plex-Token`, not logs.
- Parse Plex XML:

```go
type sectionsResponse struct {
    Directories []section `xml:"Directory"`
}
type section struct {
    Key       string     `xml:"key,attr"`
    Title     string     `xml:"title,attr"`
    Locations []location `xml:"Location"`
}
type location struct {
    Path string `xml:"path,attr"`
}
```

- Desired paths:

```go
[]string{
    filepath.Join(mountPath, "movies"),
    filepath.Join(mountPath, "series"),
}
```

- Match only exact normalized location paths to start. This avoids accidentally scanning unrelated library roots. Use `filepath.Clean` before comparing.
- De-duplicate section/path pairs if needed.
- For each matched pair, call `GET /library/sections/{key}/refresh?path={path}`.
- Treat 2xx as success. Treat non-2xx as silent no-op/failure.

### 3. `cmd/torbox-fuse/main.go`

Add import:

```go
"github.com/TorBox-App/torbox-fuse/internal/plex"
```

After TorBox client creation:

```go
plexClient := plex.New(cfg.PlexBaseURL, cfg.PlexAccessToken)
```

After successful mount and mounted log:

```go
plexClient.RefreshMountPaths(ctx, cfg.MountPath)
```

Inside `refreshOnce`, after `root.Swap(recs)` and before/after the applied log:

```go
plexClient.RefreshMountPaths(ctx, cfg.MountPath)
```

Keep `refreshOnce` signature unchanged. Do not return Plex errors.

### 4. `.env.example`

Add:

```dotenv
# Optional. When set, torbox-fuse asks Plex to scan `${MOUNT_PATH}/movies` and `${MOUNT_PATH}/series` after successful mount refreshes.
PLEX_ACCESS_TOKEN=

# Defaults to `http://127.0.0.1:32400`.
PLEX_BASE_URL=http://127.0.0.1:32400
```

### 5. `README.md`

In Environment section add:

- `PLEX_ACCESS_TOKEN` is optional. When set, torbox-fuse requests Plex partial scans for `${MOUNT_PATH}/movies` and `${MOUNT_PATH}/series` after TorBox refreshes.
- `PLEX_BASE_URL` defaults to `http://127.0.0.1:32400`.

Mention that Plex refresh failures are ignored by design.

### 6. Tests

Add `internal/plex/client_test.go` using `httptest.Server`.

Test cases:

1. Token empty:
   - Call `RefreshMountPaths`.
   - Server should not receive any request.
2. Discovers sections and refreshes exact matching paths:
   - `/library/sections` returns XML with movie/show/music sections.
   - Assert received refresh requests for:
     - `/library/sections/3/refresh?path=/srv/torbox/movies&X-Plex-Token=...`
     - `/library/sections/4/refresh?path=/srv/torbox/series&X-Plex-Token=...`
   - Assert no request for music.
3. Missing matching section/location:
   - No panic and no refresh requests.
4. Plex errors are silent:
   - `/library/sections` returns 500 or refresh returns 500.
   - `RefreshMountPaths` returns normally.

Run:

```bash
go test ./...
```

## Open decisions / assumptions

- Use exact Plex library location matches for `${MOUNT_PATH}/movies` and `${MOUNT_PATH}/series` in the initial implementation. If later needed, add prefix matching as a separate explicit behavior.
- Do not log Plex failures, per user request. Success logging is not necessary either; keep the integration quiet.
- Do not use Plex Butler for implementation because user chose path-specific refresh.
