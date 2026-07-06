#!/bin/sh
set -eu

LABEL="com.pokit.daemon"
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
DAEMON_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)
BIN_DIR="$HOME/.local/bin"
BIN_PATH="$BIN_DIR/devremote"
PLIST_DIR="$HOME/Library/LaunchAgents"
PLIST_PATH="$PLIST_DIR/$LABEL.plist"
LOG_DIR="$HOME/Library/Logs/POKIT"
UID_VALUE=$(id -u)

if [ "${POKIT_INSECURE_LOCAL_ONLY:-}" != "1" ]; then
  echo "Refusing an unauthenticated install." >&2
  echo "Set production auth flags in this installer, or explicitly run:" >&2
  echo "  POKIT_INSECURE_LOCAL_ONLY=1 $0" >&2
  exit 2
fi

# Build the daemon binary that LaunchAgent will keep alive.
mkdir -p "$BIN_DIR" "$PLIST_DIR" "$LOG_DIR"

(
  cd "$DAEMON_DIR"
  GOCACHE="${GOCACHE:-/tmp/devremote-go-cache}" \
    go build -o "$BIN_PATH.tmp" ./cmd/devremote
)
chmod 755 "$BIN_PATH.tmp"
mv "$BIN_PATH.tmp" "$BIN_PATH"

cat >"$PLIST_PATH.tmp" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>$LABEL</string>
  <key>ProgramArguments</key>
  <array>
    <string>$BIN_PATH</string>
    <string>daemon</string>
    <string>--insecure-local-only</string>
  </array>
  <key>WorkingDirectory</key>
  <string>$HOME</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>HOME</key>
    <string>$HOME</string>
    <key>PATH</key>
    <string>/Applications/cmux.app/Contents/Resources/bin:$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin</string>
    <key>CMUX_SOCKET_PATH</key>
    <string>$HOME/.local/state/cmux/cmux.sock</string>
  </dict>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Interactive</string>
  <key>StandardOutPath</key>
  <string>$LOG_DIR/daemon.log</string>
  <key>StandardErrorPath</key>
  <string>$LOG_DIR/daemon.log</string>
</dict>
</plist>
EOF

plutil -lint "$PLIST_PATH.tmp"
mv "$PLIST_PATH.tmp" "$PLIST_PATH"

# launchctl can briefly report the previous agent as present right after bootout,
# so give it a short grace period and retry bootstrap before declaring failure.
launchctl bootout "gui/$UID_VALUE/$LABEL" 2>/dev/null || true
sleep 1
BOOTSTRAP_OK=0
for _ in 1 2 3; do
  if launchctl bootstrap "gui/$UID_VALUE" "$PLIST_PATH"; then
    BOOTSTRAP_OK=1
    break
  fi
  sleep 1
done
if [ "$BOOTSTRAP_OK" -ne 1 ]; then
  echo "Failed to bootstrap $LABEL" >&2
  exit 1
fi
launchctl kickstart -k "gui/$UID_VALUE/$LABEL"

echo "Installed $LABEL"
echo "Binary: $BIN_PATH"
echo "Plist:  $PLIST_PATH"
echo "Log:    $LOG_DIR/daemon.log"
