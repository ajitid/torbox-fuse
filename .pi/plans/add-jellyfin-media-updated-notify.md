# Plan: Add Jellyfin `/Library/Media/Updated` notifications

## Goal

Add optional Jellyfin integration mirroring the existing Plex post-refresh notification flow, but using Jellyfin's path-based media update endpoint:

```http
POST /Library/Media/Updated
X-Emby-Token: <api-key>
Content-Type: application/json

{
  "Updates": [
    { "Path": "<MOUNT_PATH>/movies", "UpdateType": "Modified" },
    { "Path": "<MOUNT_PATH>/series", "UpdateType": "Modified" }
  ]
}
```

The integration should be best-effort: it must not fail TorBox refreshes, but it should log Jellyfin notification failures/status codes.

## References and findings

- Existing Plex implementation:
  - `internal/plex/client.go`
  - `cmd/torbox-fuse/main.go`
  - `internal/config/config.go`
  - `README.md`
- Current Plex flow:
  - `plex.New(cfg.PlexBaseURL, cfg.PlexAccessToken)`
  - `plexClient.RefreshMountPaths(ctx, cfg.MountPath)` after initial mount and after every successful refresh.
  - Plex is enabled only when `PLEX_ACCESS_TOKEN` is non-empty.
- Jellyfin official OpenAPI, stable 10.11.10:
  - `POST /Library/Media/Updated`, operation `PostUpdatedMedia`.
  - Request body schema `MediaUpdateInfoDto` with `Updates: MediaUpdateInfoPathDto[]`.
  - `MediaUpdateInfoPathDto` fields:
    - `Path string`
    - `UpdateType string`; description says `Created, Modified, Deleted`.
  - Auth security scheme is an API key in the `Authorization` header in the OpenAPI, but real-world Jellyfin examples commonly use `X-Emby-Token` / `X-MediaBrowser-Token`; use `X-Emby-Token` for compatibility with existing Jellyfin scripts/docs/forum examples.
- Jellyfin source checked at `Jellyfin.Api/Controllers/LibraryController.cs`:
  - `PostUpdatedMedia([FromBody] MediaUpdateInfoDto dto)` loops over `dto.Updates` and calls `_libraryMonitor.ReportFileSystemChanged(item.Path)`.
  - Current server code does **not** inspect `UpdateType` at all for `/Library/Media/Updated`.
  - Therefore using `Modified` has no current behavioral downside versus `Created` for this endpoint; the path is what triggers the library monitor. If Jellyfin later starts honoring `UpdateType`, `Modified` is the safest documented generic value for “path contents changed” after a torbox-fuse refresh. Deletions are less certain in a future where `UpdateType` is honored strictly, but current implementation ignores it.
- Forum example for path-scoped update:
  - `curl -H "X-MediaBrowser-Token: ..." -H "Content-Type: application/json" -d '{"Updates":[{"Path":"/path/to/folder/","UpdateType":"scan"}]}' https://.../Library/Media/Updated`
  - We will not use `scan` because it is not documented and current server ignores the value anyway.
- Baseline verification before planning: `go test ./...` passes.

## Product decisions

- Environment variables:
  - `JELLYFIN_API_KEY` enables Jellyfin notifications.
  - `JELLYFIN_BASE_URL` defaults to `http://127.0.0.1:8096`.
- Paths to notify:
  - exactly the same mount subdirectories as Plex: `<MOUNT_PATH>/movies` and `<MOUNT_PATH>/series`.
- Notification behavior:
  - Best-effort.
  - Do not return errors to refresh callers.
  - Log failures and non-2xx responses.
- `UpdateType`:
  - Use documented value `Modified` for both paths.

## Implementation patch spec

### 1. Add `internal/jellyfin/client.go`

Create a package similar in style to `internal/plex`, but JSON-based and with logging.

Suggested contents:

```go
package jellyfin

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "net/http"
    "net/url"
    "path"
    "path/filepath"
    "strings"
    "time"
)

type Client struct {
    baseURL string
    apiKey  string
    log     *log.Logger
    http    *http.Client
}

type mediaUpdatedRequest struct {
    Updates []mediaUpdate `json:"Updates"`
}

type mediaUpdate struct {
    Path       string `json:"Path"`
    UpdateType string `json:"UpdateType"`
}

func New(baseURL, apiKey string, logger *log.Logger) *Client {
    return &Client{
        baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
        apiKey:  strings.TrimSpace(apiKey),
        log:     logger,
        http:    http.DefaultClient,
    }
}

func (c *Client) Enabled() bool {
    return strings.TrimSpace(c.apiKey) != ""
}

func (c *Client) NotifyMountPaths(ctx context.Context, mountPath string) {
    if !c.Enabled() {
        return
    }
    ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
    defer cancel()

    reqBody := mediaUpdatedRequest{Updates: []mediaUpdate{
        {Path: filepath.Join(mountPath, "movies"), UpdateType: "Modified"},
        {Path: filepath.Join(mountPath, "series"), UpdateType: "Modified"},
    }}
    if err := c.postMediaUpdated(ctx, reqBody); err != nil {
        c.printf("jellyfin media update notify failed: %v", err)
    }
}

func (c *Client) postMediaUpdated(ctx context.Context, body mediaUpdatedRequest) error {
    u, ok := c.apiURL("/Library/Media/Updated")
    if !ok {
        return fmt.Errorf("invalid base url %q", c.baseURL)
    }
    data, err := json.Marshal(body)
    if err != nil {
        return err
    }
    req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
    if err != nil {
        return err
    }
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("X-Emby-Token", c.apiKey)

    resp, err := c.http.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    if resp.StatusCode < 200 || resp.StatusCode > 299 {
        snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
        if len(snippet) > 0 {
            return fmt.Errorf("POST /Library/Media/Updated returned %s: %s", resp.Status, strings.TrimSpace(string(snippet)))
        }
        return fmt.Errorf("POST /Library/Media/Updated returned %s", resp.Status)
    }
    return nil
}

func (c *Client) apiURL(apiPath string) (string, bool) {
    u, err := url.Parse(c.baseURL)
    if err != nil {
        return "", false
    }
    u.Path = path.Join(u.Path, apiPath)
    return u.String(), true
}

func (c *Client) printf(format string, args ...any) {
    if c.log != nil {
        c.log.Printf(format, args...)
    }
}
```

Notes:
- Keep the internal HTTP client field unexported to allow tests in package `jellyfin` to use `httptest` by changing `baseURL`, not by replacing transport.
- Use `path.Join` for URL path and `filepath.Join` for local mount paths, matching Plex code.
- Do not include query `api_key`; use header auth only.

### 2. Add `internal/jellyfin/client_test.go`

Tests to add:

1. `TestNotifyMountPathsAPIKeyEmptyDoesNotRequest`
   - Start `httptest.Server` with request counter.
   - `New(server.URL, "", logger).NotifyMountPaths(context.Background(), "/srv/torbox")`.
   - Assert no requests.

2. `TestNotifyMountPathsPostsMediaUpdatedPayload`
   - Assert method `POST`.
   - Assert path `/Library/Media/Updated`.
   - Assert `X-Emby-Token` equals token.
   - Assert `Content-Type` starts with `application/json`.
   - Decode JSON body.
   - Expected updates:
     - `{Path:"/srv/torbox/movies", UpdateType:"Modified"}`
     - `{Path:"/srv/torbox/series", UpdateType:"Modified"}`
   - Return `204`.

3. `TestNotifyMountPathsLogsErrorsButDoesNotPanicOrReturn`
   - Use a `bytes.Buffer` logger.
   - Server returns `500` and body `nope`.
   - Call `NotifyMountPaths`.
   - Assert log contains `jellyfin media update notify failed` and `500` or `Internal Server Error`.

4. Optional `TestNotifyMountPathsInvalidBaseURLLogsError`
   - `New("://bad", "token", logger).NotifyMountPaths(...)`.
   - Assert log contains invalid base URL message.

### 3. Update `internal/config/config.go`

Add fields to `Config` near Plex fields:

```go
JellyfinAPIKey  string
JellyfinBaseURL string
```

In `Load()`:

```go
jellyfinAPIKey := strings.TrimSpace(os.Getenv("JELLYFIN_API_KEY"))
```

In returned `Config`:

```go
JellyfinAPIKey:  jellyfinAPIKey,
JellyfinBaseURL: envDefault("JELLYFIN_BASE_URL", "http://127.0.0.1:8096"),
```

No config tests currently exist, so either leave untested or add a focused `internal/config/config_test.go` if desired. If adding, use `t.Setenv` for required `TORBOX_API_KEY` and assert default/custom Jellyfin values.

### 4. Update `cmd/torbox-fuse/main.go`

Imports:

Add:

```go
"github.com/TorBox-App/torbox-fuse/internal/jellyfin"
```

Near Plex client construction:

```go
plexClient := plex.New(cfg.PlexBaseURL, cfg.PlexAccessToken)
jellyfinClient := jellyfin.New(cfg.JellyfinBaseURL, cfg.JellyfinAPIKey, logger)
```

After mount, replace:

```go
plexClient.RefreshMountPaths(ctx, cfg.MountPath)
```

with:

```go
plexClient.RefreshMountPaths(ctx, cfg.MountPath)
jellyfinClient.NotifyMountPaths(ctx, cfg.MountPath)
```

Inside `refreshOnce`, after `root.Swap(recs)`, replace:

```go
plexClient.RefreshMountPaths(ctx, cfg.MountPath)
```

with the same two calls:

```go
plexClient.RefreshMountPaths(ctx, cfg.MountPath)
jellyfinClient.NotifyMountPaths(ctx, cfg.MountPath)
```

Do not change refresh error handling; Jellyfin client logs internally and does not return an error.

### 5. Update `README.md`

In Environment section, after Plex lines, add:

```md
- `JELLYFIN_API_KEY` is optional. When set, torbox-fuse notifies Jellyfin that `${MOUNT_PATH}/movies` and `${MOUNT_PATH}/series` changed after TorBox refreshes via `POST /Library/Media/Updated`.
- `JELLYFIN_BASE_URL` defaults to `http://127.0.0.1:8096`.
- Jellyfin notification failures are logged but do not fail TorBox refreshes.
```

Consider changing the existing Plex failure bullet to clearly distinguish behavior:

```md
- Plex refresh failures are ignored by design.
```

can stay as-is.

### 6. Verification

Run:

```bash
go test ./...
```

Optional manual smoke test with a fake server:

```bash
JELLYFIN_API_KEY=test JELLYFIN_BASE_URL=http://127.0.0.1:<httptest-or-local-port> TORBOX_API_KEY=... go run ./cmd/torbox-fuse
```

But regular automated tests should be enough for this patch.

## Risks / open notes

- `UpdateType` is currently ignored by Jellyfin server for `/Library/Media/Updated`; future Jellyfin versions may honor it. `Modified` is documented and broad, but if future Jellyfin requires exact per-file `Created`/`Deleted` semantics, this path-level approach may need revisiting.
- This plan intentionally does not discover/match Jellyfin virtual folders first. It directly reports changes for torbox-fuse's two mounted subpaths, which is closer to the chosen endpoint and avoids scanning unrelated libraries.
- Header choice: `X-Emby-Token` is widely used for Jellyfin API-key auth examples. If a deployment requires the OpenAPI `Authorization` header instead, a future enhancement could send both, but this initial implementation sends only `X-Emby-Token` to avoid unnecessary ambiguity.
