#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Build torbox-fuse and install it as a per-user macOS LaunchAgent.

Usage:
  scripts/install-macos-launch-agent.sh [--no-start] [--uninstall]

Options:
  --no-start   Install the LaunchAgent without starting it now.
  --uninstall  Stop, unload, and remove the LaunchAgent.
USAGE
}

start_now=1
uninstall=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-start) start_now=0 ;;
    --uninstall) uninstall=1 ;;
    -h|--help) usage; exit 0 ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [[ "$(uname -s)" != "Darwin" ]]; then
  echo "This installer is for macOS only." >&2
  exit 1
fi
if [[ "${EUID}" -eq 0 ]]; then
  echo "Do not run this installer with sudo; torbox-fuse must run as your login user." >&2
  exit 1
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd -- "$script_dir/.." && pwd)"
label="com.ajitid.torbox-fuse"
uid="$(id -u)"
agent_dir="$HOME/Library/LaunchAgents"
agent_file="$agent_dir/$label.plist"
log_dir="$HOME/Library/Logs"
stdout_log="$log_dir/torbox-fuse.log"
stderr_log="$log_dir/torbox-fuse.error.log"
torbox_bin="$repo_dir/bin/torbox-fuse"
service_target="gui/$uid/$label"

if [[ "$uninstall" -eq 1 ]]; then
  launchctl bootout "$service_target" 2>/dev/null || true
  rm -f -- "$agent_file"
  echo "Removed LaunchAgent: $agent_file"
  exit 0
fi

for cmd in go launchctl plutil; do
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "Required command not found in current PATH: $cmd" >&2
    exit 1
  fi
done
if [[ ! -f "$repo_dir/.env" ]]; then
  echo "Missing $repo_dir/.env; create it from .env.example and configure it first." >&2
  exit 1
fi

mkdir -p -- "$agent_dir" "$log_dir" "$(dirname -- "$torbox_bin")"
(
  cd -- "$repo_dir"
  go build -o "$torbox_bin" ./cmd/torbox-fuse
)

cat >"$agent_file" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$label</string>
  <key>ProgramArguments</key>
  <array>
    <string>$torbox_bin</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$repo_dir</string>
  <key>ProcessType</key>
  <string>Background</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>ThrottleInterval</key>
  <integer>10</integer>
  <key>StandardOutPath</key>
  <string>$stdout_log</string>
  <key>StandardErrorPath</key>
  <string>$stderr_log</string>
</dict>
</plist>
EOF
chmod 644 "$agent_file"
plutil -lint "$agent_file" >/dev/null

launchctl bootout "$service_target" 2>/dev/null || true
launchctl bootstrap "gui/$uid" "$agent_file"

if [[ "$start_now" -eq 1 ]]; then
  launchctl kickstart -k "$service_target"
fi

echo "Installed LaunchAgent: $agent_file"
echo "Binary: $torbox_bin"
echo "Logs:"
echo "  $stdout_log"
echo "  $stderr_log"
echo "Status:"
echo "  launchctl print $service_target"
echo "Uninstall:"
echo "  $0 --uninstall"
