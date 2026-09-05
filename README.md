# TorBox Fuse

A read-only Go FUSE filesystem for TorBox media. It fetches cached TorBox downloads, classifies videos/subtitles into `movies`, `series`, and `unknown`, stores metadata in bbolt, and streams reads through a bounded disk block cache with sequential read-ahead.

## Run

```bash
TORBOX_API_KEY=... MOUNT_PATH=/tmp/torbox-test DATA_PATH=/tmp/torbox-test.db go run ./cmd/torbox-fuse
```

The mount directory must exist or be creatable and must be empty.

## macOS

Install and approve [macFUSE](https://macfuse.github.io/) first. On Apple silicon, the macFUSE kernel backend requires enabling third-party kernel extensions in Startup Security Utility.

For Plex Media Server, run the mount and Plex as the same login user. For example:

```bash
mkdir -p "$HOME/Movies/torbox"
```

Set `MOUNT_PATH` to that empty directory and leave `FUSE_ALLOW_OTHER=false` in `.env`.

To build and run torbox-fuse automatically at login as a user LaunchAgent:

```bash
scripts/install-macos-launch-agent.sh
```

The installer builds `bin/torbox-fuse`, reads credentials from the repository `.env` through its working directory, and writes logs to `~/Library/Logs/torbox-fuse{,.error}.log`. It does not use `sudo`. Remove it with:

```bash
scripts/install-macos-launch-agent.sh --uninstall
```

Open the web UI at:

```text
http://0.0.0.0:4747
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
- `MEDIA_CHANGE_CHECK_POLL_TIME` defaults to `15s`. It checks for external TorBox-library changes and accepts standard Go durations such as `15s`, `2m`, or `2h5s`. A detected change triggers a full mount refresh; Plex/Jellyfin are notified only if the visible `movies` or `series` view changed.
- `FUSE_ALLOW_OTHER=true` enables `allow_other` and requires system FUSE configuration.
- `CACHE_PATH` defaults to your OS cache directory plus `torbox-fuse`.
- `PLEX_ACCESS_TOKEN` is optional. When set, torbox-fuse requests Plex partial scans for `${MOUNT_PATH}/movies` and `${MOUNT_PATH}/series` at startup and after explicit manual refreshes, plus detected external changes to the visible media view.
- `PLEX_BASE_URL` defaults to `http://127.0.0.1:32400`.
- Plex refresh failures and missing matching sections are logged but do not fail TorBox refreshes.
- `JELLYFIN_API_KEY` is optional. When set, torbox-fuse asks Jellyfin to refresh matching virtual folders at startup and after explicit manual refreshes, plus detected external changes to the visible media view.
- `JELLYFIN_BASE_URL` defaults to `http://127.0.0.1:8096`.
- Jellyfin refresh failures and missing matching virtual folders are logged but do not fail TorBox refreshes.
- `WEBAPP_PORT` defaults to `4747`.
- The web UI listens on all interfaces at `http://0.0.0.0:${WEBAPP_PORT}`.
