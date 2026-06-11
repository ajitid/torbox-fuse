#!/usr/bin/env bash
set -euo pipefail

# KDE Wayland workaround for Plex Desktop:
# Force Plex's Qt launcher to use the X11/xcb backend, which runs through XWayland.
# This avoids Plex's problematic Wayland path on KDE mixed-DPI setups.

DESKTOP_FILE="/usr/share/applications/tv.plex.PlexDesktop.desktop"

usage() {
  cat <<EOF
Usage:
  $0            Patch Plex desktop launcher to force QT_QPA_PLATFORM=xcb
  $0 --revert   Restore desktop launcher from backup
EOF
}

REVERT=0
case "${1:-}" in
  --revert) REVERT=1 ;;
  -h|--help) usage; exit 0 ;;
  "") ;;
  *) echo "ERROR: unknown argument: $1" >&2; usage >&2; exit 1 ;;
esac

if [[ "$REVERT" == 1 ]]; then
  if [[ -f "${DESKTOP_FILE}.bak" ]]; then
    printf '==> Restoring desktop launcher backup: %s -> %s\n' "${DESKTOP_FILE}.bak" "$DESKTOP_FILE"
    sudo cp "${DESKTOP_FILE}.bak" "$DESKTOP_FILE"
    echo 'OK: reverted Plex KDE launcher patch. Restart Plex Desktop fully.'
  else
    echo "ERROR: no desktop launcher backup found: ${DESKTOP_FILE}.bak" >&2
    exit 1
  fi
  exit 0
fi

if [[ ! -f "$DESKTOP_FILE" ]]; then
  echo "ERROR: desktop launcher not found: $DESKTOP_FILE" >&2
  exit 1
fi

printf '==> Patching desktop launcher to force xcb: %s\n' "$DESKTOP_FILE"
if [[ ! -f "${DESKTOP_FILE}.bak" ]]; then
  sudo cp "$DESKTOP_FILE" "${DESKTOP_FILE}.bak"
  printf '==> Backup created: %s\n' "${DESKTOP_FILE}.bak"
else
  printf '==> Backup already exists: %s\n' "${DESKTOP_FILE}.bak"
fi

sudo python - <<'PY' "$DESKTOP_FILE"
from pathlib import Path
import sys

path = Path(sys.argv[1])
lines = path.read_text().splitlines()
for i, line in enumerate(lines):
    if line.startswith("Exec="):
        command = line.removeprefix("Exec=")
        if not command.startswith("env QT_QPA_PLATFORM=xcb "):
            command = command.removeprefix("QT_QPA_PLATFORM=xcb ")
            lines[i] = f"Exec=env QT_QPA_PLATFORM=xcb {command}"
        break
else:
    raise SystemExit("ERROR: no Exec= line found in desktop file")
path.write_text("\n".join(lines) + "\n")
PY

if grep -q '^Exec=env QT_QPA_PLATFORM=xcb Plex' "$DESKTOP_FILE"; then
  echo 'OK: Plex desktop launcher now forces QT_QPA_PLATFORM=xcb.'
  echo 'Restart Plex Desktop fully for changes to take effect.'
else
  echo 'ERROR: patch verification failed.' >&2
  exit 1
fi
