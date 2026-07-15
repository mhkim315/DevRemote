#!/bin/bash
# C0D-R2 PostToolUse hook: consumption witness only. Never blocks.
set -euo pipefail
IN=$(cat)
echo "POSTTOOL pid=$$ t=$(date +%s)" >> "${C0D_CAP}/life.txt"
# PostToolUse input carries tool_use_id + tool_input + tool_result; project bounded.
echo "$IN" > "${C0D_CAP}/posttool_raw.txt"
python3 3>"${C0D_CAP}/posttool_identity.json" -c '
import json, sys
try:
    d = json.load(sys.stdin)
    rec = {"tool_use_id": d.get("tool_use_id",""), "is_error": d.get("is_error",False)}
    json.dump(rec, open(3,"w"))
except Exception as e:
    open(3,"w").write(json.dumps({"error": str(e)}))
' <<< "$IN" 2>/dev/null || echo "PTU_PARSE_FAIL" >> "${C0D_CAP}/life.txt"
printf '{}'
