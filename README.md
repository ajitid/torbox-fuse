# TorBox Fuse

A read-only Go FUSE filesystem for TorBox media. It fetches cached TorBox downloads, classifies videos/subtitles into `movies`, `series`, and `unknown`, stores metadata in bbolt, and streams reads through a bounded disk block cache with sequential read-ahead.

## Run

```bash
TORBOX_API_KEY=... MOUNT_PATH=/tmp/torbox-test DATA_PATH=/tmp/torbox-test.db go run ./cmd/torbox-fuse
```

The mount directory must exist or be creatable and must be empty.

Open the web UI at:

```text
http://0.0.0.0:3939
```

The UI provides:

- mounted folder browsing
- torrent-wise accepted media/subtitle browsing
- refresh
- add torrent by magnet link
- remove torrent

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
- The web UI listens on all interfaces at `http://0.0.0.0:3939`.
