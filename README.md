# TorBox native FUSE media center

Milestone 1 implements a read-only Go FUSE filesystem for TorBox media. It fetches cached TorBox downloads, classifies videos/subtitles into `movies`, `series`, and `unknown`, stores metadata in bbolt, and streams reads via TorBox HTTP range requests.

## Run

```bash
TORBOX_API_KEY=... MOUNT_PATH=/tmp/torbox-test DATA_PATH=/tmp/torbox-test.db go run ./cmd/torbox-media-center
```

The mount directory must exist or be creatable and must be empty. Unmount with Ctrl+C, or manually with:

```bash
fusermount3 -u /tmp/torbox-test
```

## Environment

See `.env.example`.

- `TORBOX_API_KEY` is required.
- `MOUNT_PATH` defaults to `./torbox`.
- `DATA_PATH` defaults to `./torbox-media-center.db`.
- `MOUNT_REFRESH_TIME`: `slowest`, `very_slow`, `slow`, `normal`, `fast`, `ultra_fast`, or `instant`.
- `FUSE_ALLOW_OTHER=true` enables `allow_other` and requires system FUSE configuration.

Milestone 2 disk cache/read-ahead is intentionally not implemented yet.
