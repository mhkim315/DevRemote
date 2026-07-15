#!/bin/bash
set -uo pipefail
D="$(dirname "$0")"; CAP="${D}/cap-h"; mkdir -p "$CAP" /tmp/c0d-r4-claim; export C0D_CAP="$CAP"
P=0; F=0
c(){ rm -f "$CAP"/life.txt "$CAP"/deferred_full.txt "$CAP"/deferred_identity.json; rm -rf /tmp/c0d-r4-claim; mkdir -p /tmp/c0d-r4-claim; }
al(){ grep -q "$1" "$CAP/life.txt" 2>/dev/null&&{ P=$((P+1));echo "PASS $2";}||{ F=$((F+1));echo "FAIL $2 (no '$1')";}; }
ae(){ [ "$3" = "$1" ]&&{ P=$((P+1));echo "PASS $2";}||{ F=$((F+1));echo "FAIL $2 (exit=$3 want=$1)";}; }
SID="s-00000000-0000-0000-0000-000000000001"; TU="call_test_R4_Xy9"; TN="Bash"
GI='{"command":"echo ok","description":"test"}'
J="{\"session_id\":\"$SID\",\"tool_use_id\":\"$TU\",\"tool_name\":\"$TN\",\"tool_input\":$GI}"

# T1
c; echo "NOT JSON"|"$D/hook_defer.sh">/dev/null 2>&1;e1=$?; ae 2 "T1-def-parse-exit2" "$e1"; al "DEFER_PARSE_FAIL" "T1-def-parsed-failed"
# T2
c; echo "NOT JSON"|"$D/hook_resume.sh">/dev/null 2>&1;e1=$?; ae 2 "T2-res-parse-exit2" "$e1"; al "RESUME_PARSE_FAIL" "T2-res-failed"
# T3
c; echo "$J"|"$D/hook_resume.sh">/dev/null 2>&1;e1=$?; ae 2 "T3-missing-def-exit2" "$e1"; al "MISSING_DEFERRED" "T3-missing-def"
# T4 changed sid
c; echo "$J"|"$D/hook_defer.sh">/dev/null 2>&1
echo "{\"session_id\":\"FAKE_SID\",\"tool_use_id\":\"$TU\",\"tool_name\":\"$TN\",\"tool_input\":$GI}"|C0D_DECISION=allow "$D/hook_resume.sh">/dev/null 2>&1;e1=$?; ae 2 "T4-sid-mis-exit2" "$e1"; al "MISMATCH sid" "T4-sid-mis"
# T5 REPLAY: simultaneous resumes, exactly 1 MATCH_OK, 1 REPLAY_BLOCKED
c; J5="{\"session_id\":\"$SID\",\"tool_use_id\":\"${TU}R\",\"tool_name\":\"$TN\",\"tool_input\":$GI}"
echo "$J5"|"$D/hook_defer.sh">/dev/null 2>&1
echo "$J5"|C0D_DECISION=allow "$D/hook_resume.sh">/dev/null 2>&1 & P1=$!
echo "$J5"|C0D_DECISION=allow "$D/hook_resume.sh">/dev/null 2>&1 & P2=$!
wait $P1 2>/dev/null||true; wait $P2 2>/dev/null||true
al "MATCH_OK" "T5-replay-one-match"; al "REPLAY_BLOCKED" "T5-replay-one-blocked"
M=$(grep -c "MATCH_OK" "$CAP/life.txt" 2>/dev/null);R=$(grep -c "REPLAY_BLOCKED" "$CAP/life.txt" 2>/dev/null)
{ [ "$M" = "1" ] && [ "$R" = "1" ]; } && { P=$((P+1));echo "PASS T5-counts M=$M R=$R";} || { F=$((F+1));echo "FAIL T5-counts M=$M R=$R";}
# T6 unknown decision (fresh ID: TU6)
c; J6="{\"session_id\":\"$SID\",\"tool_use_id\":\"${TU}6\",\"tool_name\":\"$TN\",\"tool_input\":$GI}"
echo "$J6"|"$D/hook_defer.sh">/dev/null 2>&1
echo "$J6"|C0D_DECISION=BOGUS "$D/hook_resume.sh">/dev/null 2>&1;e1=$?; ae 2 "T6-unknown-exit2" "$e1"; al "UNKNOWN_DECISION" "T6-unknown"
# T7 deny (fresh ID: TU7)
c; J7="{\"session_id\":\"$SID\",\"tool_use_id\":\"${TU}7\",\"tool_name\":\"$TN\",\"tool_input\":$GI}"
echo "$J7"|"$D/hook_defer.sh">/dev/null 2>&1
echo "$J7"|C0D_DECISION=deny "$D/hook_resume.sh">/dev/null 2>&1;e1=$?; ae 0 "T7-deny-exit0" "$e1"; al "RESEARCH_DENIAL" "T7-deny"
# T8 allow (fresh ID: TU8)
c; J8="{\"session_id\":\"$SID\",\"tool_use_id\":\"${TU}8\",\"tool_name\":\"$TN\",\"tool_input\":$GI}"
echo "$J8"|"$D/hook_defer.sh">/dev/null 2>&1
echo "$J8"|C0D_DECISION=allow "$D/hook_resume.sh">/dev/null 2>&1;e1=$?; ae 0 "T8-allow-exit0" "$e1"; al "MATCH_OK" "T8-allow-match"

echo "=== $P PASS, $F FAIL ==="; [ "$F" -eq 0 ]
