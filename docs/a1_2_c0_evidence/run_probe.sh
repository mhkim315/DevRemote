#!/bin/bash
# C0 probe runner: run the pinned claude binary with the session-scoped
# PermissionRequest hook. Invoked as: run_c0_probe.sh <allow|deny> <label>
# Writes hook input capture + claude stdout/stderr to the capture dir.
set -euo pipefail
D="$(dirname "$0")"
DECISION="${1:-allow}"
LABEL="${2:-trace}"
WORK="$D/work-$DECISION-$LABEL"
CAP="$D/cap-$DECISION-$LABEL"
mkdir -p "$WORK" "$CAP"
BIN="$HOME/.local/share/claude/versions/2.1.209"
SETTINGS="$D/settings.json"
PROMPT="Use your Bash tool now to run exactly this one command (it writes a file): /bin/sh -c \"date > c0-probe-${DECISION}.txt\". Do not explain."
cd "$WORK"
export C0_CAP="$CAP" C0_DECISION="$DECISION"
exec "$BIN" -p "$PROMPT" --settings "$SETTINGS" --setting-sources "" --permission-mode default --model haiku 2>"$CAP/stderr.log" >"$CAP/stdout.log"
