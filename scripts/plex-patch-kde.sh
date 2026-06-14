#!/usr/bin/env bash
set -euo pipefail

# KDE Wayland workaround for Plex Qt apps:
# Force Plex Desktop / Plex HTPC launchers to use the X11/xcb backend, which runs through XWayland.
# This avoids Plex's problematic native Wayland path on KDE mixed-DPI/fractional-scale setups.

APPS=(
  "Plex Desktop|/usr/share/applications/tv.plex.PlexDesktop.desktop"
  "Plex HTPC|/usr/share/applications/tv.plex.PlexHTPC.desktop"
)

usage() {
  cat <<EOF
Usage:
  $0            Patch installed Plex desktop launchers to force QT_QPA_PLATFORM=xcb
  $0 --revert   Restore installed Plex desktop launchers from backups
EOF
}

REVERT=0
case "${1:-}" in
  --revert) REVERT=1 ;;
  -h|--help) usage; exit 0 ;;
  "") ;;
  *) echo "ERROR: unknown argument: $1" >&2; usage >&2; exit 1 ;;
esac

patch_desktop_file() {
  local app_name="$1"
  local desktop_file="$2"

  printf '==> Patching %s launcher to force xcb: %s\n' "$app_name" "$desktop_file"
  if [[ ! -f "${desktop_file}.bak" ]]; then
    sudo cp "$desktop_file" "${desktop_file}.bak"
    printf '==> Backup created: %s\n' "${desktop_file}.bak"
  else
    printf '==> Backup already exists: %s\n' "${desktop_file}.bak"
  fi

  sudo python - <<'PY' "$desktop_file"
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

  if grep -q '^Exec=env QT_QPA_PLATFORM=xcb ' "$desktop_file"; then
    printf 'OK: %s launcher now forces QT_QPA_PLATFORM=xcb.\n' "$app_name"
  else
    printf 'ERROR: patch verification failed for %s.\n' "$app_name" >&2
    exit 1
  fi
}

revert_desktop_file() {
  local app_name="$1"
  local desktop_file="$2"

  if [[ -f "${desktop_file}.bak" ]]; then
    printf '==> Restoring %s launcher backup: %s -> %s\n' "$app_name" "${desktop_file}.bak" "$desktop_file"
    sudo cp "${desktop_file}.bak" "$desktop_file"
    printf 'OK: reverted %s KDE launcher patch.\n' "$app_name"
    return 0
  fi

  printf 'WARNING: no backup found for %s: %s.bak\n' "$app_name" "$desktop_file" >&2
  return 1
}

if [[ "$REVERT" == 1 ]]; then
  restored=0
  seen=0
  for app in "${APPS[@]}"; do
    IFS='|' read -r app_name desktop_file <<< "$app"
    if [[ -f "$desktop_file" ]]; then
      seen=1
      if revert_desktop_file "$app_name" "$desktop_file"; then
        restored=1
      fi
    else
      printf '==> Skipping %s; launcher not found: %s\n' "$app_name" "$desktop_file"
    fi
  done

  if [[ "$seen" == 0 ]]; then
    echo 'ERROR: no Plex desktop launchers found.' >&2
    exit 1
  fi
  if [[ "$restored" == 0 ]]; then
    echo 'ERROR: no Plex launcher backups restored.' >&2
    exit 1
  fi

  echo 'Restart patched Plex apps fully for changes to take effect.'
  exit 0
fi

patched=0
for app in "${APPS[@]}"; do
  IFS='|' read -r app_name desktop_file <<< "$app"
  if [[ -f "$desktop_file" ]]; then
    patch_desktop_file "$app_name" "$desktop_file"
    patched=1
  else
    printf '==> Skipping %s; launcher not found: %s\n' "$app_name" "$desktop_file"
  fi
done

if [[ "$patched" == 0 ]]; then
  echo 'ERROR: no Plex desktop launchers found.' >&2
  exit 1
fi

echo 'Restart patched Plex apps fully for changes to take effect.'
