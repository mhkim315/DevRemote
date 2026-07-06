#!/bin/sh
set -eu

LABEL="com.pokit.daemon"
UID_VALUE=$(id -u)
PLIST_PATH="$HOME/Library/LaunchAgents/$LABEL.plist"

# Best-effort detach; the file removal only matters after launchd has released it.
launchctl bootout "gui/$UID_VALUE/$LABEL" 2>/dev/null || true
rm -f "$PLIST_PATH"

echo "Uninstalled $LABEL"
