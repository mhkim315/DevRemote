#!/bin/bash
# C0D PreToolUse hook: resume phase. On the SAME tool_use_id re-entering,
# captures the input again, then allows or denies per C0D_DECISION.
set -euo pipefail
IN=$(cat)
echo "$IN" > "${C0D_CAP}/resume_input_$$.json"
echo "RESUME pid=$$ t=$(date +%s) decision=${C0D_DECISION:-allow}" >> "${C0D_CAP}/life.log"
# Verify the re-entering tool_use_id matches the deferred one.
if [ -f "${C0D_CAP}/deferred_id.txt" ]; then
  WANT=$(head -2 "${C0D_CAP}/deferred_id.txt" | tail -1)
elif [ -f "${C0D_WORK}/deferred_id.txt" ]; then
  WANT=$(head -2 "${C0D_WORK}/deferred_id.txt" | tail -1)
else
  WANT="UNKNOWN"
fi
GOT=$(echo "$IN" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("tool_use_id",""))' 2>/dev/null || true)
echo "RESUME_ID_CHECK want=${WANT} got=${GOT}" >> "${C0D_CAP}/life.log"
if [ "${C0D_DECISION}" = "deny" ]; then
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"C0D research denial"}}'
else
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"allow"}}'
fi
