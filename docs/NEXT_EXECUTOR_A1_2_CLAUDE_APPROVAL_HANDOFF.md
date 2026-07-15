# Next Executor Handoff — A1.2-C0D Deferred Tool Research

Status: **C0D EVIDENCE ONLY — NO PRODUCTION CODE — NO ACTIONABILITY**

Repository: `/Users/mhk/Documents/codex/DevRemote`

Branch: `feature/phase10-multi-adapter`

Accepted Codex baseline: `2b940a6fce6e878ffa0da17b5df4d39438af144d`

## 1. Mandatory reading

1. `docs/A1_2_CLAUDE_APPROVAL_EXTENSION_PLAN.md`
2. `docs/A1_2_C0_CLAUDE_EVIDENCE_REPORT.md` — C0H BLOCKED
3. `docs/A1_2_C0R_PERMISSION_PROMPT_TOOL_REPORT.md` — C0R BLOCKED
4. official `PreToolUse` defer section: <https://code.claude.com/docs/en/hooks>
5. `docs/SP1_NATIVE_APPROVAL_FINAL_ACCEPTANCE.md`
6. `docs/A1_APPROVAL_SAFETY_PLAN.md`

## 2. Frozen findings

- Do not retry or reinterpret `PermissionRequest` hooks in headless mode.
- Do not infer `--permission-prompt-tool` support from `--help`: 2.1.209 accepts
  the flag and connects an isolated MCP server.
- Do not proceed to implementation: exact permission invocation identity and a
  provider-native consumed-decision join are not proven.
- Do not use MCP transport IDs, FIFO, command equality, timing, terminal text,
  static rules, a side effect, or a single in-flight request as substitutes.

## 3. C0D scope and prerequisite

Research exact Claude Code 2.1.209 only. Verify its version, SHA-256 and architecture
before every live packet. Use ambient authentication only; never copy credentials.
The user must complete Claude login. If `claude auth status` is not logged in, report
the environment blocker and stop without treating it as a protocol verdict.

Create a session-only isolated `PreToolUse` hook. Do not modify user/project/managed
settings or use a shell/terminal permission prompt. The hook must structurally decode
and bound all fields before recording evidence.

## 4. Mandatory pre-probe contract note

Before live execution, write `docs/A1_2_C0D_DEFER_CONTRACT_NOTE.md` containing:

- authority owner and complete binding table;
- `PreToolUse(ID) → defer → result(ID) → resume → PreToolUse(ID)` state machine;
- exact allow and deny consumption witnesses;
- replay, duplicate, timeout, exit and replacement behavior;
- single-tool-only capacity and multi-tool fail-closed behavior;
- privacy projection and evidence bounds;
- adversarial wrong-ID and duplicate-resume interleavings;
- explicit non-goals.

## 5. Live evidence packets

Run these in order and stop on the first authority failure:

1. **D0 identity:** one harmless tool call; prove initial hook
   `tool_use_id == deferred_tool_use.id`, exact session ID and full bounded input.
2. **D1 allow:** resume the exact session; prove the same ID re-enters the hook,
   executes once, and reaches matching `PostToolUse`/`PostToolUseFailure`.
3. **D2 deny:** resume a fresh deferred invocation; prove exact same-ID denial and
   zero execution using a stable provider-native result, not absence/timing alone.
4. **D3 replay/lifecycle:** duplicate resume, duplicate decision, wrong ID/session,
   changed input, timeout, hook failure, process exit, stop/delete and replacement.
5. **D4 multiplicity:** two sequential requests get distinct IDs; parallel/batched
   and multi-tool turns fail closed/non-actionable.

Use uniquely named harmless temp markers only as corroboration. Capture monotonic
direction/sequence plus bounded structural projections. Remove temporary probes and
processes after evidence collection.

## 6. Forbidden shortcuts

- no identity from command/input equality, timestamps, ordering or FIFO;
- no terminal/PTY text, `waiting_approval`, generic key/text delivery or static
  permission rules;
- no treatment of `PostToolUse` side effects without matching `tool_use_id` as
  consumed-decision authority;
- no hidden serialization assumption;
- no parallel/multi-tool actionability;
- no reuse of C0H/C0R as positive evidence.

## 7. Production state

Claude approval actionability remains off. No Claude CTA, delivery capacity,
provider mapping or store admission may be enabled. Managed observation and prompt
submission are not actionable approval support. The Codex A1.1/SP1 path is frozen
and must not be modified.

## 8. Stop and verdict

Commit only the contract note, isolated bounded probe/evidence and report. End with
exactly one: `C0D PROCEED`, `C0D PROCEED WITH CONTRACT CHANGES`, or `C0D BLOCKED`.
Push and stop for independent review.

Do not start C1D, production Claude wiring, A1 core changes, Codex changes, mobile,
N1, O1 or O2. Passing C0D only permits the reviewer to write a new implementation
plan.
