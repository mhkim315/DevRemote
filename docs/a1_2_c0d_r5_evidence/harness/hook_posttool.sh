#!/bin/bash
# C0D-R3 PostToolUse hook: consumption witness. Records tool_use_id + is_error.
set -euo pipefail
IN=$(cat); echo "POSTTOOL pid=$$ t=$(date +%s)" >> "${C0D_CAP}/life.txt"
python3 3>"${C0D_CAP}/posttool_identity.json" -c '
import json, sys
try: d = json.load(sys.stdin)
except: d = {}
json.dump({"tool_use_id":d.get("tool_use_id",""),"is_error":d.get("is_error",False)}, open(3,"w"))
' <<< "$IN" 2>/dev/null
printf '{}'
