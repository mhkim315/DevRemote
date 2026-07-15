#!/bin/bash
# C0D-R1 PreToolUse hook: defer phase.
# Captures tool_use_id, session_id, tool_name, bounded tool_input.
# Exits 0 with defer decision.
set -euo pipefail
IN=$(cat)
echo "DEFER pid=$$ t=$(date +%s)" >> "${C0D_CAP}/life.txt"
# Structural projection (privacy-bounded): session_id + tool_use_id + tool_name + input digest only.
python3 -c "
import json, sys, hashlib
d = json.load(sys.stdin)
sid = d.get('session_id','')
tuid = d.get('tool_use_id','')
tname = d.get('tool_name','')
tinp = json.dumps(d.get('tool_input',{}), sort_keys=True)
inhash = hashlib.sha256(tinp.encode()).hexdigest()[:16]
rec = {'session_id':sid, 'tool_use_id':tuid, 'tool_name':tname, 'input_sha256_16':inhash}
json.dump(rec, open('${C0D_CAP}/deferred_identity.json','w'))
# Keep the full identity for the resume phase (never committed).
open('${C0D_CAP}/deferred_full.txt','w').write(f'{sid}\n{tuid}\n{tname}\n{tinp}\n')
" <<< "$IN" 2>/dev/null
printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}'
