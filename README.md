# TorBox Fuse

A read-only Go FUSE filesystem for TorBox media. It fetches cached TorBox downloads, classifies videos/subtitles into `movies`, `series`, and `unknown`, stores metadata in bbolt, and streams reads through a bounded disk block cache with sequential read-ahead.

## Run

```bash
TORBOX_API_KEY=... MOUNT_PATH=/tmp/torbox-test DATA_PATH=/tmp/torbox-test.db go run ./cmd/torbox-fuse
```

The mount directory must exist or be creatable and must be empty. Refresh over the local Unix control socket:

```bash
curl --unix-socket /tmp/torbox-fuse.sock -X POST http://localhost/refresh
```

List stored file metadata over the same socket. The path after `/files` mirrors the mounted folder structure and returns all files under that folder:

```bash
curl --unix-socket /tmp/torbox-fuse.sock http://localhost/files
curl --unix-socket /tmp/torbox-fuse.sock http://localhost/files/movies
curl --unix-socket /tmp/torbox-fuse.sock 'http://localhost/files/movies/1917%20%282019%29/'
curl --unix-socket /tmp/torbox-fuse.sock 'http://localhost/files/series/Breaking%20Bad/Season%201'
curl --unix-socket /tmp/torbox-fuse.sock http://localhost/stats
```

Unmount with Ctrl+C, or manually with:

```bash
fusermount3 -u /tmp/torbox-test
```

## Environment

See `.env.example`.

- `TORBOX_API_KEY` is required.
- `MOUNT_PATH` defaults to `./torbox`.
- `DATA_PATH` defaults to `./torbox-fuse.db`.
- `MOUNT_REFRESH_TIME` defaults to `3h`. Valid values: `24h`, `12h`, `6h`, `3h`, `1h`, or `6min`.
- `FUSE_ALLOW_OTHER=true` enables `allow_other` and requires system FUSE configuration.
- `CACHE_PATH` defaults to your OS cache directory plus `torbox-fuse`.
- `CONTROL_SOCKET_PATH` defaults to `/tmp/torbox-fuse.sock`; POST `/refresh` triggers a synchronous refresh; GET `/files` returns stored file metadata; GET `/files/<mounted-folder-path>` returns files under that mounted folder path; GET `/stats` returns counts.
