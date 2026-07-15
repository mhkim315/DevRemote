#!/bin/bash
# C0D PreToolUse hook: defer phase. Captures the EXACT tool_use_id,
# session_id, tool_name and bounded tool_input, writes a life marker,
# then returns permissionDecision "defer".
set -euo pipefail
IN=$(cat)
echo "$IN" > "${C0D_CAP}/defer_input_$$.json"
echo "DEFER pid=$$ t=$(date +%s)" >> "${C0D_CAP}/life.log"
# Structural allowlist: session_id + tool_use_id + tool_name + tool_input must all be present
SID=$(echo "$IN" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("session_id",""))' 2>/dev/null || true)
TUID=$(echo "$IN" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("tool_use_id",""))' 2>/dev/null || true)
TNAME=$(echo "$IN" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("tool_name",""))' 2>/dev/null || true)
# Save the deferred identity for the resume phase. The WORK dir is shared
# across hook invocations for the same C0D session.
python3 -c "
import json, sys
d = json.load(sys.stdin)
sid = d.get('session_id','')
tuid = d.get('tool_use_id','')
tname = d.get('tool_name','')
tinp = d.get('tool_input',{})
f = open('${C0D_CAP}/deferred_id.txt','w')
f.write(f'{sid}\n{tuid}\n{tname}\n{json.dumps(tinp)}\n')
f.close()
f2 = open('${C0D_WORK}/deferred_id.txt','w')
f2.write(f'{sid}\n{tuid}\n{tname}\n{json.dumps(tinp)}\n')
f2.close()
" <<< "$IN" 2>/dev/null
printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}'
