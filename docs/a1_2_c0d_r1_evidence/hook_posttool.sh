#!/bin/bash
# C0D-R1 PostToolUse hook: consumption witness.
# Fires AFTER tool execution. Captures tool_use_id + is_error state as the
# provider-native consumed-decision proof.
set -euo pipefail
IN=$(cat)
echo "POSTTOOL pid=$$ t=$(date +%s)" >> "${C0D_CAP}/life.txt"
# Project bounded structural record: tool_use_id + is_error only.
python3 -c "
import json, sys
d = json.load(sys.stdin)
rec = {'tool_use_id': d.get('tool_use_id',''), 'is_error': d.get('is_error',False)}
json.dump(rec, open('${C0D_CAP}/posttool_identity.json','w'))
" <<< "$IN" 2>/dev/null
# Never block PostToolUse — the tool already ran. This hook is observation only.
printf '{}'
