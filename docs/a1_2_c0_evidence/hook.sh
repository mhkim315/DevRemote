#!/bin/bash
# Write a LIFE marker on every invocation so we know the hook actually ran.
echo "HOOK_RAN pid=$$ ppid=$PPID t=$(date +%s)" >> "${C0_CAP}/life.log"
in=$(cat)
echo "$in" > "${C0_CAP}/perm_input_$$.json"
if [ "${C0_DECISION}" = "allow" ]; then
  printf '{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}'
else
  printf '{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny"}}}'
fi
