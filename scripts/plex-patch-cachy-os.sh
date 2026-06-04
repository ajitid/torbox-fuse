#!/usr/bin/env bash
set -euo pipefail

# Plex Desktop tweaks for CachyOS/Linux:
# 1. Configure text subtitle appearance through Plex's bundled mpv config.
# 2. Patch bundled Plex web-client autoplay countdown.
#
# Re-run after Plex Desktop updates, since the web-client JS filename/content may change.
#
# Change this later by editing the variable below, or by running:
#   AUTOPLAY_TIMEOUT_SECONDS=5 ./plex-patch-cachy-os.sh
AUTOPLAY_TIMEOUT_SECONDS="${AUTOPLAY_TIMEOUT_SECONDS:-7}"

MPV_CONF="${HOME}/.local/share/plex/mpv.conf"
WEB_CLIENT_ROOT="/opt/plex-desktop/resources/web-client"
WEB_CLIENT_JS_DIR="${WEB_CLIENT_ROOT}/js"
INDEX_HTML="${WEB_CLIENT_ROOT}/index.html"

usage() {
  cat <<EOF
Usage:
  $0            Apply Plex subtitle + autoplay patches
  $0 --revert   Revert patches from backups where possible

Config:
  AUTOPLAY_TIMEOUT_SECONDS=${AUTOPLAY_TIMEOUT_SECONDS}
EOF
}

REVERT=0
case "${1:-}" in
  --revert) REVERT=1 ;;
  -h|--help) usage; exit 0 ;;
  "") ;;
  *) echo "ERROR: unknown argument: $1" >&2; usage >&2; exit 1 ;;
esac

remove_subtitle_patch() {
  [[ -f "$MPV_CONF" ]] || return 0
  printf '==> Removing managed subtitle settings from: %s\n' "$MPV_CONF"
  tmp_file="$(mktemp)"
  grep -vE '^(# Managed by plex-patch-cachy-os\.sh|sub-font|sub-font-size|sub-border-size|sub-shadow-offset)=' "$MPV_CONF" \
    | grep -v '^# Managed by plex-patch-cachy-os\.sh$' > "$tmp_file" || true
  mv "$tmp_file" "$MPV_CONF"
}

locate_js_file() {
  local js_file
  js_file="$(grep -REIl --include='*.js' 'secondsLeft:[0-9]+' "$WEB_CLIENT_JS_DIR" 2>/dev/null | head -n 1 || true)"
  if [[ -z "$js_file" ]]; then
    js_file="$(find "$WEB_CLIENT_JS_DIR" -maxdepth 1 -type f -name '*.js.bak' 2>/dev/null | head -n 1 | sed 's/\.bak$//' || true)"
  fi
  printf '%s' "$js_file"
}

if [[ "$REVERT" == 1 ]]; then
  remove_subtitle_patch

  JS_FILE="$(locate_js_file)"
  if [[ -n "$JS_FILE" && -f "${JS_FILE}.bak" ]]; then
    printf '==> Restoring JS backup: %s -> %s\n' "${JS_FILE}.bak" "$JS_FILE"
    sudo cp "${JS_FILE}.bak" "$JS_FILE"
  else
    echo "WARNING: no JS backup found to restore." >&2
  fi

  if [[ -f "${INDEX_HTML}.bak" ]]; then
    printf '==> Restoring index.html backup: %s -> %s\n' "${INDEX_HTML}.bak" "$INDEX_HTML"
    sudo cp "${INDEX_HTML}.bak" "$INDEX_HTML"
  else
    echo "WARNING: no index.html backup found to restore." >&2
  fi

  echo 'OK: revert completed. Restart Plex Desktop fully.'
  exit 0
fi

if ! [[ "$AUTOPLAY_TIMEOUT_SECONDS" =~ ^[0-9]+$ ]] || (( AUTOPLAY_TIMEOUT_SECONDS < 2 )); then
  echo "ERROR: AUTOPLAY_TIMEOUT_SECONDS must be an integer >= 2." >&2
  exit 1
fi

printf '==> Updating subtitle config: %s\n' "$MPV_CONF"
mkdir -p "$(dirname "$MPV_CONF")"
touch "$MPV_CONF"

# Remove previous copies of only the settings this script manages, then append desired values.
tmp_file="$(mktemp)"
grep -vE '^(# Managed by plex-patch-cachy-os\.sh|sub-font|sub-font-size|sub-border-size|sub-shadow-offset)=' "$MPV_CONF" \
  | grep -v '^# Managed by plex-patch-cachy-os\.sh$' > "$tmp_file" || true
cat >> "$tmp_file" <<EOF

# Managed by plex-patch-cachy-os.sh
sub-font=Adwaita Sans
sub-font-size=28
sub-border-size=1
sub-shadow-offset=0
EOF
mv "$tmp_file" "$MPV_CONF"

printf '==> Locating Plex web-client JS containing autoplay countdown...\n'
JS_FILE="$(locate_js_file)"

if [[ -z "${JS_FILE}" ]]; then
  echo "ERROR: Could not find Plex web-client JS with secondsLeft countdown under: $WEB_CLIENT_JS_DIR" >&2
  exit 1
fi

printf '==> Patching autoplay countdown in: %s\n' "$JS_FILE"
printf '==> Autoplay timeout: %ss\n' "$AUTOPLAY_TIMEOUT_SECONDS"

# Need root to edit /opt files. Use sudo only for the privileged operations.
if [[ ! -f "${JS_FILE}.bak" ]]; then
  sudo cp "$JS_FILE" "${JS_FILE}.bak"
  printf '==> Backup created: %s\n' "${JS_FILE}.bak"
else
  printf '==> Backup already exists: %s\n' "${JS_FILE}.bak"
fi

sudo python - <<'PY' "$JS_FILE" "$AUTOPLAY_TIMEOUT_SECONDS"
from pathlib import Path
import re
import sys

path = Path(sys.argv[1])
timeout = int(sys.argv[2])
denominator = timeout - 1
content = path.read_text()

content, n1 = re.subn(r'secondsLeft:\d+', f'secondsLeft:{timeout}', content)
content, n2 = re.subn(r'this\.setState\(\{secondsLeft:\d+\}\)', f'this.setState({{secondsLeft:{timeout}}})', content)
content, n3 = re.subn(r'\(\d+-u\)/\d+\*100', f'({timeout}-u)/{denominator}*100', content)

if n1 < 1 or n2 < 1 or n3 < 1:
    raise SystemExit(f"ERROR: unexpected patch counts: secondsLeft={n1}, reset={n2}, progress={n3}")

path.write_text(content)
PY

printf '==> Updating index.html integrity hash for patched JS...\n'
if [[ ! -f "${INDEX_HTML}.bak" ]]; then
  sudo cp "$INDEX_HTML" "${INDEX_HTML}.bak"
  printf '==> Backup created: %s\n' "${INDEX_HTML}.bak"
else
  printf '==> Backup already exists: %s\n' "${INDEX_HTML}.bak"
fi

JS_BASENAME="$(basename "$JS_FILE")"
NEW_INTEGRITY="sha384-$(openssl dgst -sha384 -binary "$JS_FILE" | openssl base64 -A)"
sudo python - <<'PY' "$INDEX_HTML" "$JS_BASENAME" "$NEW_INTEGRITY"
from pathlib import Path
import re
import sys

index = Path(sys.argv[1])
js_basename = sys.argv[2]
new_integrity = sys.argv[3]
script_src = "js/" + js_basename

content = index.read_text()
pattern = rf'(<script src="{re.escape(script_src)}" integrity=")[^"]+(" crossorigin="anonymous"></script>)'
updated, count = re.subn(pattern, rf'\1{new_integrity}\2', content)
if count != 1:
    raise SystemExit(f"ERROR: expected 1 integrity replacement for {script_src}, got {count}")
index.write_text(updated)
PY

printf '==> Verifying patch...\n'
PROGRESS_EXPR="(${AUTOPLAY_TIMEOUT_SECONDS}-u)/$((AUTOPLAY_TIMEOUT_SECONDS - 1))*100"
if grep -q "secondsLeft:${AUTOPLAY_TIMEOUT_SECONDS}" "$JS_FILE" && grep -qF "$PROGRESS_EXPR" "$JS_FILE" && grep -q "$NEW_INTEGRITY" "$INDEX_HTML"; then
  echo "OK: subtitle config updated, autoplay countdown patched to ${AUTOPLAY_TIMEOUT_SECONDS}s, and index.html integrity hash updated."
  echo 'Restart Plex Desktop fully for changes to take effect.'
else
  echo 'WARNING: Patch verification did not find all expected strings. Check the JS/index manually.' >&2
  exit 1
fi
