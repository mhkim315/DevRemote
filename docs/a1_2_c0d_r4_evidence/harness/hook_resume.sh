#!/bin/bash
# C0D-R3 PreToolUse hook: resume phase. SAME-SESSION resume (no fork).
# Full 64-char SHA-256. Closed decision vocabulary. Shared-scope atomic claim.
# FAILS CLOSED: malformed JSON, missing identity, mismatch, replay, unknown decision → exit 2.
set -euo pipefail
IN=$(cat)
echo "RESUME pid=$$ t=$(date +%s)" >> "${C0D_CAP}/life.txt"

# Parse incoming input
GOT_DATA=$(python3 -c "
import json, sys, hashlib
try: d = json.load(sys.stdin)
except Exception: sys.exit(2)
sid = d.get('session_id','')
tuid = d.get('tool_use_id','')
tname = d.get('tool_name','')
tinp = json.dumps(d.get('tool_input',{}), sort_keys=True)
inhash = hashlib.sha256(tinp.encode()).hexdigest()
print(f'{sid}\n{tuid}\n{tname}\n{inhash}')
" <<< "$IN" 2>/dev/null) || { echo "RESUME_PARSE_FAIL exit=2" >> "${C0D_CAP}/life.txt"; printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"malformed input - tool call blocked"}}'; exit 2; }

GOT_SID=$(echo "$GOT_DATA" | sed -n '1p')
GOT_TUID=$(echo "$GOT_DATA" | sed -n '2p')
GOT_TNAME=$(echo "$GOT_DATA" | sed -n '3p')
GOT_INHASH=$(echo "$GOT_DATA" | sed -n '4p')

# Read deferred identity
[ -f "${C0D_CAP}/deferred_full.txt" ] || { echo "MISSING_DEFERRED exit=2" >> "${C0D_CAP}/life.txt"; printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"no deferred identity - tool call blocked"}}'; exit 2; }
WANT_SID=$(sed -n '1p' "${C0D_CAP}/deferred_full.txt")
WANT_TUID=$(sed -n '2p' "${C0D_CAP}/deferred_full.txt")
WANT_TNAME=$(sed -n '3p' "${C0D_CAP}/deferred_full.txt")
WANT_INHASH=$(sed -n '4p' "${C0D_CAP}/deferred_full.txt")

# FULL identity comparison (session_id + tool_use_id + tool_name + input_sha256)
FOUND_MISMATCH=false
[ "$GOT_SID"   = "$WANT_SID"   ] || { echo "MISMATCH sid want=$WANT_SID got=$GOT_SID" >> "${C0D_CAP}/life.txt"; FOUND_MISMATCH=true; }
[ "$GOT_TUID"  = "$WANT_TUID"  ] || { echo "MISMATCH tuid want=$WANT_TUID got=$GOT_TUID" >> "${C0D_CAP}/life.txt"; FOUND_MISMATCH=true; }
[ "$GOT_TNAME" = "$WANT_TNAME" ] || { echo "MISMATCH tname want=$WANT_TNAME got=$GOT_TNAME" >> "${C0D_CAP}/life.txt"; FOUND_MISMATCH=true; }
[ "$GOT_INHASH" = "$WANT_INHASH" ] || { echo "MISMATCH input_sha256 want=$WANT_INHASH got=$GOT_INHASH" >> "${C0D_CAP}/life.txt"; FOUND_MISMATCH=true; }

if $FOUND_MISMATCH; then
  echo "IDENTITY_MISMATCH_DENY tuid=${GOT_TUID}" >> "${C0D_CAP}/life.txt"
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"identity mismatch - tool call blocked"}}'
  exit 2
fi

# Atomic one-shot claim (shared scope: /tmp/c0d-r3-claim/)
CLAIM_DIR="/tmp/c0d-r3-claim/${GOT_TUID}"
if ! mkdir "${CLAIM_DIR}" 2>/dev/null; then
  echo "REPLAY_BLOCKED tuid=${GOT_TUID} exit=2" >> "${C0D_CAP}/life.txt"
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"already consumed - tool call blocked"}}'
  exit 2
fi

echo "MATCH_OK tuid=${GOT_TUID} tname=${GOT_TNAME} sid=${GOT_SID} inhash=${GOT_INHASH}" >> "${C0D_CAP}/life.txt"

# CLOSED decision vocabulary
case "${C0D_DECISION:-}" in
  allow)
    printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'
    ;;
  deny)
    echo "RESEARCH_DENIAL tuid=${GOT_TUID}" >> "${C0D_CAP}/life.txt"
    printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"C0D-R3 research denial"}}'
    ;;
  *)
    echo "UNKNOWN_DECISION deny=${C0D_DECISION:-UNSET} exit=2" >> "${C0D_CAP}/life.txt"
    printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"unknown/unsupported decision - tool call blocked"}}'
    exit 2
    ;;
esac
