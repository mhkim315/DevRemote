#!/bin/bash
# R6-A5: Live Claude 2.1.209 denial evidence probe
# Run this script manually to capture the actual permission_denials wire format.
# Production code is NOT modified — this is evidence gathering only.

set -e

CLAUDE_209="$HOME/.local/share/claude/versions/2.1.209"
echo "Claude: $CLAUDE_209"
echo "SHA-256: $(shasum -a 256 "$CLAUDE_209" | awk '{print $1}')"

TMPDIR=$(mktemp -d)
echo "Working dir: $TMPDIR"

# Hook that captures PreToolUse input for identity verification
cat > "$TMPDIR/hook.sh" << 'HOOKEOF'
#!/bin/sh
tee /tmp/r6a5_pretool_stdin.json
echo '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"defer"}}'
HOOKEOF
chmod +x "$TMPDIR/hook.sh"

cat > "$TMPDIR/settings.json" << SETEOF
{"hooks":{"PreToolUse":[{"matcher":"","hooks":[{"type":"command","command":"$TMPDIR/hook.sh"}]}]}}
SETEOF

echo ""
echo "=== Running harmless deny probe ==="
"$CLAUDE_209" \
  --settings "$TMPDIR/settings.json" \
  --setting-sources "" \
  --output-format stream-json \
  --include-partial-messages \
  -p "Use your Bash tool to run exactly this command: echo hello" \
  > "$TMPDIR/stream.jsonl" 2>&1
EXIT_CODE=$?

echo ""
echo "Exit code: $EXIT_CODE"
echo ""

# Show all result-type events and their stop_reasons
echo "=== All result events ==="
python3 -c "
import json
with open('$TMPDIR/stream.jsonl') as f:
    for line in f:
        line = line.strip()
        if not line: continue
        try:
            obj = json.loads(line)
            t = obj.get('type','')
            sr = obj.get('stop_reason','')
            if t == 'result':
                keys = sorted(obj.keys())
                print(f'type={t} stop_reason={sr} keys={keys}')
        except Exception as e:
            print(f'PARSE: {e}')
"

echo ""
echo "=== Looking for permission_denials ==="
python3 -c "
import json
with open('$TMPDIR/stream.jsonl') as f:
    for line in f:
        line = line.strip()
        if not line: continue
        try:
            obj = json.loads(line)
            if 'permission_denials' in obj:
                print('FOUND permission_denials event:')
                pd = obj['permission_denials']
                print(f'  type={type(pd).__name__}')
                if isinstance(pd, list):
                    for i, entry in enumerate(pd):
                        print(f'  entry[{i}] keys={sorted(entry.keys())}')
                        for k, v in entry.items():
                            print(f'    {k}: {type(v).__name__} = {v}')
                else:
                    print(f'  (not a list): {pd}')
        except Exception as e:
            pass
"

echo ""
echo "=== PreToolUse identity (from hook) ==="
python3 -c "
import json, hashlib
with open('/tmp/r6a5_pretool_stdin.json') as f:
    obj = json.load(f)
print('session_id:', obj.get('session_id'))
print('tool_use_id:', obj.get('tool_use_id'))
print('tool_name:', obj.get('tool_name'))
if 'tool_input' in obj:
    ti = json.dumps(obj['tool_input'], sort_keys=True)
    print('tool_input canonical:', ti)
    print('input_sha256:', hashlib.sha256(ti.encode()).hexdigest())
"

rm -rf "$TMPDIR"
echo ""
echo "Done. Raw stream saved (now deleted)."
