# Plan: HTTP refresh endpoint over Unix socket

## Goal

Add a local-only control interface so an already-running `torbox-fuse` process can refresh mounted contents without keyboard input.

User decisions:

- Use HTTP over a Unix domain socket.
- Add `CONTROL_SOCKET_PATH`, defaulting to `/tmp/torbox-fuse.sock`.
- `POST /refresh` should wait until the refresh is complete and return success/failure.
- Only refresh is needed; no status/shutdown API for now.

## Current references

- Entrypoint and refresh wiring: `cmd/torbox-fuse/main.go`
- Keyboard-triggered refresh: `cmd/torbox-fuse/keyboard.go`
- Refresh manager locking behavior: `internal/refresh/refresh.go`
- Config/env loading: `internal/config/config.go`
- Docs/env examples: `README.md`, `.env.example`

Current `main.go` has:

- `mgr := refresh.New(...)`
- initial `mgr.Run(ctx)` before mounting
- `refreshOnce := func(reason string) { ... }`
- keyboard shortcut calls `go refreshOnce("manual")`
- ticker calls `refreshOnce("scheduled")`

Current `refresh.Manager.Run` uses `TryLock()` and returns existing store contents if another refresh is already running. For a synchronous HTTP refresh endpoint, this should be changed to a blocking `Lock()` so `POST /refresh` only returns after a real refresh run is complete, not after a skipped refresh.

## API spec

### Socket

Default socket path:

```text
/tmp/torbox-fuse.sock
```

Configurable with:

```bash
CONTROL_SOCKET_PATH=/path/to/socket
```

### Endpoint

```bash
curl --unix-socket /tmp/torbox-fuse.sock -X POST http://localhost/refresh
```

Responses:

- `200 OK` with JSON on success:

```json
{"ok":true,"files":123}
```

- `405 Method Not Allowed` for non-POST `/refresh`.
- `404 Not Found` for other paths.
- `500 Internal Server Error` with JSON if refresh fails:

```json
{"ok":false,"error":"..."}
```

Implementation should log refresh start/failure/success using the existing logger.

## Implementation steps

### 1. Extend config

File: `internal/config/config.go`

Add field to `Config`:

```go
ControlSocketPath string
```

In `Load()`, populate it with:

```go
ControlSocketPath: envDefault("CONTROL_SOCKET_PATH", "/tmp/torbox-fuse.sock"),
```

Update startup log in `cmd/torbox-fuse/main.go` to include `control_socket=%s`.

### 2. Make refresh runs blocking instead of skipped

File: `internal/refresh/refresh.go`

Replace:

```go
if !m.mu.TryLock() {
	m.log.Printf("refresh already running; skipping")
	return m.store.All(ctx)
}
defer m.mu.Unlock()
```

with:

```go
m.mu.Lock()
defer m.mu.Unlock()
```

Rationale: `POST /refresh` is specified to wait until refresh completion. Blocking also gives simpler semantics across keyboard/manual/scheduled/API refreshes.

### 3. Refactor `refreshOnce` to return result

File: `cmd/torbox-fuse/main.go`

Change current:

```go
refreshOnce := func(reason string) {
	logger.Printf("%s refresh starting", reason)
	recs, err := mgr.Run(ctx)
	if err != nil {
		logger.Printf("%s refresh failed: %v", reason, err)
		return
	}
	root.Swap(recs)
	logger.Printf("%s refresh applied: %d files", reason, len(recs))
}
```

into:

```go
refreshOnce := func(ctx context.Context, reason string) (int, error) {
	logger.Printf("%s refresh starting", reason)
	recs, err := mgr.Run(ctx)
	if err != nil {
		logger.Printf("%s refresh failed: %v", reason, err)
		return 0, err
	}
	root.Swap(recs)
	logger.Printf("%s refresh applied: %d files", reason, len(recs))
	return len(recs), nil
}
```

Then update callers:

```go
restoreTerminal := enableTerminalShortcuts(ctx, cancel, logger, func() { go func() { _, _ = refreshOnce(ctx, "manual") }() })
```

Ticker:

```go
_, _ = refreshOnce(ctx, "scheduled")
```

### 4. Add Unix-socket HTTP control server

New file: `cmd/torbox-fuse/control.go`

Suggested implementation shape:

```go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
)

type refreshFunc func(context.Context, string) (int, error)

func startControlServer(ctx context.Context, socketPath string, logger *log.Logger, refresh refreshFunc) (*http.Server, error) {
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed\n", http.StatusMethodNotAllowed)
			return
		}
		files, err := refresh(r.Context(), "api")
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "files": files})
	})
	srv := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
		_ = os.Remove(socketPath)
	}()
	go func() {
		logger.Printf("control socket listening on %s", socketPath)
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("control socket stopped: %v", err)
		}
	}()
	return srv, nil
}
```

Notes for implementer:

- `http.Server.Serve` closes the listener on shutdown.
- The stale socket file should be removed before listening and after shutdown.
- This intentionally fails startup if the socket cannot be created, because the feature is enabled by default via a default socket path.

### 5. Wire control server in `main.go`

After defining `refreshOnce` and before blocking on `<-ctx.Done()`:

```go
controlServer, err := startControlServer(ctx, cfg.ControlSocketPath, logger, refreshOnce)
if err != nil {
	return fmt.Errorf("start control socket: %w", err)
}
_ = controlServer
```

This can be placed before or after starting keyboard shortcuts/ticker. Prefer before ticker for clearer startup order.

### 6. Update docs/env example

File: `.env.example`

Add:

```bash
CONTROL_SOCKET_PATH=/tmp/torbox-fuse.sock
```

File: `README.md`

In the Run section, add usage example:

```bash
curl --unix-socket /tmp/torbox-fuse.sock -X POST http://localhost/refresh
```

In Environment section, add:

```markdown
- `CONTROL_SOCKET_PATH` defaults to `/tmp/torbox-fuse.sock`; POST `/refresh` over this Unix socket triggers a synchronous refresh.
```

## Verification

Run:

```bash
go test ./...
```

Optional manual smoke test, using a fake/real env depending on availability:

1. Start `torbox-fuse`.
2. In another shell:

```bash
curl --unix-socket /tmp/torbox-fuse.sock -X POST http://localhost/refresh
```

Expected success response:

```json
{"ok":true,"files":...}
```

Also verify:

```bash
curl --unix-socket /tmp/torbox-fuse.sock http://localhost/refresh
```

returns HTTP 405.

## Future extension, not part of this plan

- `GET /status`
- `POST /shutdown`
- A `torbox-fusectl` helper command
- Per-user runtime-dir socket default such as `$XDG_RUNTIME_DIR/torbox-fuse.sock`
