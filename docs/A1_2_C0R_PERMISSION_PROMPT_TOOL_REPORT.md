# A1.2-C0R — `--permission-prompt-tool` Feasibility Report

Status: **C0R BLOCKED — exact request identity and authoritative consumed-decision
evidence are not proven.**

Date: 2026-07-15. Independent reviewer. Repository HEAD before this
documentation update: `96f185f9afc021ff23128384c99e0c3f0eefc514`.

This is a separate research checkpoint. It does not reopen the accepted historical
finding **C0H BLOCKED**: the stable `PermissionRequest` hook did not fire in
headless `claude -p`.

## 1. Exact provider artifact

- Executable: `~/.local/share/claude/versions/2.1.209`
- Reported version: `2.1.209 (Claude Code)`
- SHA-256: `59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c`
- Format/architecture: Mach-O 64-bit executable, arm64
- Size: 240,377,264 bytes
- Ambient authentication at probe time: unavailable
  (`loggedIn=false`, `authMethod=none`)

The global installation was not changed and no credentials were copied.

## 2. Direct flag probe

The pinned executable was invoked directly, not inferred from `--help`:

```text
/Users/mhk/.local/share/claude/versions/2.1.209 -p --model haiku \
  --permission-mode default \
  --permission-prompt-tool mcp__pokit_permission__permission_prompt \
  --strict-mcp-config \
  --mcp-config /Users/mhk/Documents/codex/tmp_c0r_mcp_config.json \
  --setting-sources '' \
  --settings '{"permissions":{"ask":["Bash"]}}' \
  --tools Bash --output-format stream-json --verbose \
  'Use Bash to create the file /private/tmp/pokit-c0r-provider-marker-20260715 \
  with the exact text C0R. Do not use any other tool.'
```

Result: the parser accepted the flag, the isolated MCP server connected, and Claude
Code completed `initialize` plus `tools/list`. The run then exited 1 with an
authentication failure before any provider turn or permission `tools/call`.
Separately captured stdout contained 2,955 bytes of structured init and failure
records; stderr contained zero bytes. The
requested side effect did not occur.

Committed bounded evidence:

- `docs/a1_2_c0r_evidence/direct_flag_result.txt`
- `docs/a1_2_c0r_evidence/mcp_handshake.jsonl`

Therefore the earlier statement “not in `--help`, therefore unavailable” was
incorrect. The official CLI reference also explicitly states that `--help` is not
exhaustive and documents this flag for non-interactive mode.

## 3. Official contract review

Current official references:

- <https://code.claude.com/docs/en/cli-usage>
- <https://code.claude.com/docs/en/agent-sdk/user-input>
- <https://code.claude.com/docs/en/permissions>
- <https://code.claude.com/docs/en/hooks>

The CLI reference documents MCP startup and two response restrictions, but does not
freeze the permission-tool request arguments or expose a provider request ID,
`tool_use_id`, Claude session ID, turn/message ID, or retry identity. The documented
Agent SDK permission callback receives tool name, full input, suggestions and a
cancellation signal; it does not document an invocation identifier. The stable hook
reference explicitly states that `PermissionRequest` omits `tool_use_id`.

These official contracts do not establish an exact structural join between an MCP
permission call and a later Claude-native tool execution/result event.

## 4. Required probes and blocked evidence

The following could not be run because the exact pinned executable had no usable
ambient authentication at verification time:

- allow and deny;
- malformed response, disconnect and timeout;
- duplicate and delayed response;
- two distinct requests and wrong-request response;
- response after termination/replacement;
- exact command execution/non-execution corroboration.

More importantly, even a successful tool call and side-effect trace would not by
itself close the contract. C0R requires a provider-issued identity shared by the
permission request and the authoritative execution/result event. No such shared
field is documented, and no live `tools/call` was available to prove an additional
exact-version field.

Returning the MCP JSON-RPC response, its transport request ID, later command
execution, timing, ordering, command equality or single-flight behavior are not
accepted substitutes for provider-consumption identity.

## 5. Security counterexamples retained

Until an exact shared identity is demonstrated, these remain unresolved and must
fail closed:

- two identical permission requests cannot be distinguished without FIFO or content;
- a delayed response could target a replacement runtime;
- a duplicate/wrong response could be mistaken for the current invocation;
- process termination after response write leaves consumption ambiguous;
- static allow/deny rules can resolve a tool before the prompt tool is consulted.

## 6. Verdict

**C0R BLOCKED**

Narrow reason: the pinned binary accepts the official surface, but exact permission
request identity and authoritative consumed-decision evidence are unavailable. The
complete behavior matrix was additionally environment-blocked by missing ambient
authentication. Neither gap may be replaced by heuristic correlation.

No production, mobile, store, Codex, N1, O1 or O2 code changed. C1/C2/C3 must not
start. Stock Claude Code headless approval remains unsupported under POKIT's current
exact-authority contract.
