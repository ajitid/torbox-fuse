#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Install user-level systemd startup units for:
  - torbox-fuse: go run ./cmd/torbox-fuse from this repository
  - plextraktsync-watch: plextraktsync watch, after plexmediaserver is active

Usage:
  scripts/install-user-systemd-startup.sh [--no-start] [--enable-linger] [--uninstall]

Options:
  --no-start       Install and enable units, but do not start/restart them now.
  --enable-linger  Run: sudo loginctl enable-linger "$USER"
                   This lets user services start at boot before login.
  --uninstall      Stop, disable, and remove the installed user units.
USAGE
}

start_now=1
enable_linger=0
uninstall=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-start)
      start_now=0
      ;;
    --enable-linger)
      enable_linger=1
      ;;
    --uninstall)
      uninstall=1
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd -- "$script_dir/.." && pwd)"
user_unit_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"

torbox_unit="$user_unit_dir/torbox-fuse.service"
plextraktsync_unit="$user_unit_dir/plextraktsync-watch.service"

systemctl_user() {
  systemctl --user "$@"
}

if [[ "$uninstall" -eq 1 ]]; then
  systemctl_user disable --now torbox-fuse.service plextraktsync-watch.service 2>/dev/null || true
  rm -f -- "$torbox_unit" "$plextraktsync_unit"
  systemctl_user daemon-reload
  echo "Removed user units:"
  echo "  $torbox_unit"
  echo "  $plextraktsync_unit"
  exit 0
fi

for cmd in systemctl bash go plextraktsync; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Required command not found in current PATH: $cmd" >&2
    exit 1
  fi
done

mkdir -p -- "$user_unit_dir"

bash_path="$(command -v bash)"

cat >"$torbox_unit" <<EOF
[Unit]
Description=TorBox FUSE
Documentation=https://github.com/TorBox-App/torbox-fuse
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$repo_dir
EnvironmentFile=-$repo_dir/.env
ExecStart=$bash_path -lc 'exec go run ./cmd/torbox-fuse'
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
EOF

cat >"$plextraktsync_unit" <<EOF
[Unit]
Description=PlexTraktSync watch
After=default.target

[Service]
Type=simple
ExecStartPre=$bash_path -lc 'until systemctl is-active --quiet plexmediaserver.service; do echo "Waiting for plexmediaserver.service..."; sleep 5; done'
ExecStart=$bash_path -lc 'exec plextraktsync watch'
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
EOF

systemctl_user daemon-reload
systemctl_user enable torbox-fuse.service plextraktsync-watch.service

if [[ "$enable_linger" -eq 1 ]]; then
  sudo loginctl enable-linger "$USER"
fi

if [[ "$start_now" -eq 1 ]]; then
  systemctl_user restart torbox-fuse.service plextraktsync-watch.service
fi

echo "Installed user units:"
echo "  $torbox_unit"
echo "  $plextraktsync_unit"
echo
echo "Status:"
echo "  systemctl --user status torbox-fuse.service"
echo "  systemctl --user status plextraktsync-watch.service"
echo
echo "Logs:"
echo "  journalctl --user -u torbox-fuse.service -f"
echo "  journalctl --user -u plextraktsync-watch.service -f"

if [[ "$enable_linger" -eq 0 ]]; then
  echo
  echo "Note: user services normally start after login. To start them at boot before login, run:"
  echo "  $0 --enable-linger"
fi
