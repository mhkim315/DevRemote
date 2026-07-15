#!/bin/bash
# C0D-R1 PreToolUse hook: resume phase. FAILS CLOSED on mismatch.
# - Session ID, tool_use_id, tool_name MUST match the deferred identity.
# - On match: returns allow/deny per C0D_DECISION.
# - On mismatch: exit 2 (blocking error: denies the tool call and blocks execution).
set -euo pipefail
IN=$(cat)
echo "RESUME pid=$$ t=$(date +%s)" >> "${C0D_CAP}/life.txt"

# Read the deferred identity (the FULL file has session_id, tool_use_id, tool_name, tool_input_json).
[ -f "${C0D_CAP}/deferred_full.txt" ] || { echo "FATAL: deferred identity not found" >> "${C0D_CAP}/life.txt"; exit 2; }
WANT_SID=$(sed -n '1p' "${C0D_CAP}/deferred_full.txt")
WANT_TUID=$(sed -n '2p' "${C0D_CAP}/deferred_full.txt")
WANT_TNAME=$(sed -n '3p' "${C0D_CAP}/deferred_full.txt")
WANT_INHASH=$(sha256sum "${C0D_CAP}/deferred_full.txt" 2>/dev/null | cut -c1-16 || true)

GOT_SID=$(echo "$IN" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("session_id",""))' 2>/dev/null || true)
GOT_TUID=$(echo "$IN" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("tool_use_id",""))' 2>/dev/null || true)
GOT_TNAME=$(echo "$IN" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("tool_name",""))' 2>/dev/null || true)

FOUND_MISMATCH=false
[ "$GOT_TUID" = "$WANT_TUID" ] || { echo "MISMATCH tool_use_id want=$WANT_TUID got=$GOT_TUID" >> "${C0D_CAP}/life.txt"; FOUND_MISMATCH=true; }
[ "$GOT_TNAME" = "$WANT_TNAME" ] || { echo "MISMATCH tool_name want=$WANT_TNAME got=$GOT_TNAME" >> "${C0D_CAP}/life.txt"; FOUND_MISMATCH=true; }

if $FOUND_MISMATCH; then
  # Block the tool call (exit 2 = blocking error).
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"C0D-R1 identity mismatch -- tool call blocked"}}'
  exit 2
fi

echo "MATCH_OK tuid=${GOT_TUID} tname=${GOT_TNAME}" >> "${C0D_CAP}/life.txt"
# Record that this deferred identity was consumed for replay enforcement.
echo "USED" > "${C0D_CAP}/deferred_consumed_${GOT_TUID}.txt"

if [ "${C0D_DECISION}" = "deny" ]; then
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"C0D-R1 research denial"}}'
else
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'
fi
