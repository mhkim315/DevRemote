#!/bin/bash
# C0D-R2 PreToolUse hook: defer phase.
set -euo pipefail
IN=$(cat)
echo "DEFER pid=$$ t=$(date +%s)" >> "${C0D_CAP}/life.txt"

python3 3>"${C0D_CAP}/deferred_identity.json" 4>"${C0D_CAP}/deferred_full.txt" -c '
import json, sys, hashlib
try:
    d = json.load(sys.stdin)
except Exception as e:
    sys.stderr.write(f"DEFER_PARSE_FAIL {e}\n")
    sys.exit(2)
sid = d.get("session_id","")
tuid = d.get("tool_use_id","")
tname = d.get("tool_name","")
tinp = json.dumps(d.get("tool_input",{}), sort_keys=True)
inhash = hashlib.sha256(tinp.encode()).hexdigest()[:16]
rec = {"session_id":sid, "tool_use_id":tuid, "tool_name":tname, "input_sha256_16":inhash}
json.dump(rec, open(3, "w"))
open(4, "w").write(f"{sid}\n{tuid}\n{tname}\n{inhash}\n{tinp}\n")
' <<< "$IN"

PYEXIT=$?
if [ $PYEXIT -ne 0 ]; then
  echo "DEFER_FAILED exit=2 pyexit=${PYEXIT}" >> "${C0D_CAP}/life.txt"
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"malformed input - tool call blocked"}}'
  exit 2
fi
printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}'
