# A1.2 Claude Approval Extension — Production Implementation Plan

Status: **C0D ACCEPT — C1D ACCEPT — C2D AUTHORIZED IN STAGED CHECKPOINTS — C3D NOT AUTHORIZED**

Accepted research evidence: `e42d4c570e64462ce813861017cc635e338e68bf`.
Frozen provider-neutral A1 core and accepted Codex SP1 baseline:
`2b940a6fce6e878ffa0da17b5df4d39438af144d` (implementation `dd6d05c`).

Accepted C1D implementation: `4794ce7f42380c388c1a5614b4b2518bc1722870`.
Accepted C1D evidence/report HEAD: `e9e661c550c0a78f8f6544f8db911bea9fd5cac1`.

## 1. Historical findings remain independent

1. **C0H BLOCKED** — the stable `PermissionRequest` hook did not fire in
   headless `claude -p`. Static permission rules handled allow/deny instead.
2. **C0R BLOCKED** — Claude Code 2.1.209 accepted
   `--permission-prompt-tool`, but its MCP request lacked an exact invocation
   identity and an authoritative consumed-decision join.
3. **C0D ACCEPT** — exact Claude Code 2.1.209 proved a different official
   lifecycle: `PreToolUse` defer/resume preserves the exact `session_id`,
   `tool_use_id`, tool name and canonical input digest. Allow is consumed by a
   matching `PostToolUse`; deny is consumed by a matching provider
   `permission_denials` result. Deterministic replay evidence admitted exactly
   one resume and rejected the duplicate.

C0D does not remediate or weaken C0H/C0R. Only the deferred-tool lifecycle is
eligible for implementation.

## 2. Scope and product claim

A1.2 adds one certified Claude approval path to the existing POKIT-managed
runtime. It does not create a generic hook SDK or generic provider framework.

The first supported tuple is deliberately narrow:

- provider: Claude Code;
- exact certified version: 2.1.209;
- mode: POKIT-owned headless managed invocation;
- request surface: `PreToolUse` returning `defer`;
- actions: `allow_once` and `deny` only;
- supported request: one structurally certified tool invocation at a time;
- consumption: matching provider-native `PostToolUse`/tool result for allow,
  or matching `permission_denials` result for deny.

Interactive PTY prompts, terminal text, JSONL inference, `waiting_approval`,
process names, CWD, timing and FIFO are never approval authority.

## 3. Frozen authority ownership

The provider-neutral A1 core remains the sole owner of authentication,
ApprovalID, requester context, canonical ActionDigest, idempotency, atomic
claim, delivery receipt validation, commit, expiry and supersession.

Claude-specific production code owns only:

- direct launch of the certified Claude executable with isolated hook settings;
- strict receipt of one structured `PreToolUse` invocation;
- exact join to the provider's `tool_deferred` result;
- exact-session resume and one-shot decision delivery to the repeated hook;
- routing matching allow/deny consumption evidence back to A1 delivery;
- provider-specific cancellation, timeout and process cleanup.

Codex-specific request/response schemas and `serverRequest/resolved` semantics
must not be reused. Shared code may be extracted only when both accepted
provider contracts require the same provider-neutral behavior.

## 4. Immutable Claude request binding

Every actionable Claude approval is bound to all fields below.

| Field | Canonical authority |
| --- | --- |
| ApprovalID | daemon-generated A1 identity; never the provider tool-use ID |
| POKIT SessionID | managed-session registry |
| RuntimeRef | adapter `claude_headless`, version `2.1.209`, current managed-service epoch as LaunchGeneration, StreamGeneration zero |
| provider artifact | certified version plus platform capability tuple and opaque artifact identity |
| Claude session ID | structured provider result and exact resume target |
| tool invocation ID | identical `PreToolUse.tool_use_id`, `deferred_tool_use.id`, and consumed result ID |
| tool name | strict bounded `PreToolUse.tool_name` |
| tool input | provider-specific canonical bytes kept private; bounded digest enters the binding |
| action | stored `allow_once` or `deny`; never caller-derived |
| delivery schema | exact `claude.pretooluse.decision.v1` identity |
| resume attempt | daemon-generated one-shot nonce internal to the delivery transaction |
| requester/idempotency | server-derived authenticated context and canonical A1 key |

PID alone, a resume command, MCP IDs, command equality or event order cannot
substitute for any binding field.

The initial macOS certification may use a macOS launcher/attestor, but no common
contract may depend on macOS-specific paths, code-signing or process APIs. The
staged requirement is deliberately narrow:

- **C1D observation-only:** exact version, canonical real path and configured
  SHA-256 artifact digest checked immediately before direct spawn are sufficient.
  The runtime record may preserve that certified pre-launch digest, but must not
  call it process-image attestation.
- **C2D:** remains non-actionable and does not expand this requirement.
- **C3D before actionability:** prove a platform-specific spawned-process identity
  or obtain an explicit independent contract decision accepting an equivalent
  non-reusable launch binding. The provider-neutral observable tuple remains
  `{OS, arch, attestor kind/version, opaque artifact identity, opaque
  process-image attestation or approved equivalent, certification result/reason}`.

This staging prevents C1D from becoming a code-signing project while preserving
the same certifiable boundary for a later Windows implementation.

## 5. Production lifecycle and linearization

```text
managed Claude epoch created
  → initial process emits PreToolUse(binding)
  → private hook bridge validates and records observation
  → hook returns defer
  → provider emits tool_deferred(same session, same ID, same input)
  → C1D may create non-actionable bounded observation

C3D actionable path only:
  → A1 Store creates pending Approval with exact binding/options
  → authenticated mobile decision
  → A1 ClaimForExecution atomically grants one claim token
  → Claude delivery atomically reserves one resume attempt for that claim
  → exact session resumed under current RuntimeRef
  → repeated PreToolUse must match the full binding
  → one-shot bridge consumes stored allow_once/deny
  → allow: matching PostToolUse/tool result for the same ID
     deny: matching permission_denials result for the same ID
  → bound DeliveryReceipt accepted by A1
  → approval committed delivered
```

The Claude delivery reservation is the provider-specific linearization point.
It must serialize duplicate taps, resume attempts, stop/delete, runtime
replacement and provider cancellation. It must not hold internal state locks
across process spawn, hook IPC or provider I/O.

Queue admission, hook response write, process exit, a side effect, or absence of
execution is not success. Only the matching provider-native result after the
exact decision was consumed may produce `accepted`/`already_accepted`.

## 6. Closed failure behavior

- Missing or changed session/ID/name/input digest: zero decision delivery.
- Unsupported version/artifact/platform tuple: non-actionable.
- Multi-tool, batched, parallel or ambiguous deferred results: non-actionable.
- Duplicate resume: exactly one owner; later attempts conflict/replay-block.
- Timeout, hook crash, malformed hook output or IPC disconnect: non-success.
- Stop, delete, exit or runtime replacement: revoke the pending bridge entry and
  defeat any stale completion.
- Provider-side cancellation/resolution before claim: zero resume.
- Ambiguous outcome after decision delivery: delivery failure, never automatic
  retransmission.
- Daemon restart: no pending or executing Claude authority is restored in A1.2;
  recovery may be unknown/non-actionable only.
- Capacity exhaustion: reject before mutating canonical state.

The frozen A1 manual-retry/idempotency rules remain authoritative. A retry never
creates a second provider execution owner.

## 7. C1D — Managed launch and non-actionable observation

**Independently accepted.** C1D does not enable mobile actions, certified options,
delivery capacity or A1 claims.

Required production work:

1. Add a provider-specific `ManagedClaudeService` (name may differ) beside, not
   inside, `ManagedCodexService`.
2. Route the real structured `pokit run claude` preset to a direct executable
   launch; no `bash -c`, command string, attached session or observer discovery.
3. Verify the certified version/artifact through an OS-neutral launcher/attestor
   seam. PATH lookup alone is availability, not certification.
4. Launch with session-isolated hook settings. Never mutate user, project or
   managed Claude settings and never copy authentication material.
5. Provide a daemon-owned private local hook bridge with an unguessable
   per-runtime capability. It accepts only strict, bounded `PreToolUse` fields
   and initially returns `defer` only.
6. The sole managed provider pump must join the hook observation to the exact
   structured `tool_deferred` result and publish a bounded **non-actionable**
   intervention record with zero options.
7. Own bounded pending observations and clean them on timeout, exit, stop,
   delete and epoch replacement.

C1D acceptance evidence:

- real `pokit run claude` production entry reaches the managed service;
- direct argv launch of exact 2.1.209, isolated settings and ambient auth;
- exact SessionID/RuntimeRef/session/tool-use/name/input-digest join;
- unknown fields, oversized input, wrong version, mismatched result, duplicate
  ID, multi-tool and capacity exhaustion fail closed;
- terminal/PTY/JSONL/observer evidence cannot create or overwrite the record;
- provider payload, prompt, command, paths, hook secret and input source material
  do not enter public DTOs or logs;
- zero actionable options, zero ClaimForExecution, zero provider resume and zero
  mobile CTA;
- focused build/vet/race tests plus one bounded live defer observation.

C1D was independently accepted at the implementation and evidence SHAs recorded
above. Its managed observation behavior is frozen while C2D is implemented.

## 8. C2D — Exact decision delivery and consumption routing

**Authorized only through the staged C2D checkpoints in the current executor
handoff.** Production actionability remains off throughout C2D. Each checkpoint
must stop for verification before the next one begins.

C2D builds the Claude-specific `ApprovalDelivery` boundary and proves it with
controlled production-composition tests:

- only the concretely C0D-certified `Bash` tool shape is a candidate in the first
  slice; every other tool remains non-actionable until separately certified;
- immutable option mapping: `allow_once → allow`, `deny → deny`;
- canonical private delivery material and schema identity;
- atomic one-shot resume reservation bound to claim token, RuntimeRef, Claude
  session, tool-use ID and input digest;
- exact repeated-hook validation and one-shot decision consumption;
- allow witness routing from matching PostToolUse/tool result;
- deny witness routing from matching permission_denials result;
- bound receipt only after the matching result;
- deterministic duplicate, replacement, stop/delete, timeout, malformed output,
  provider cancellation and result-before/while/after-decision interleavings;
- no lock across external I/O and no generic terminal or delivery-gate queue as
  success authority.

C2D must also freeze a safe review projection for every candidate tool. It must be
bounded, deterministic and sufficient for the user to distinguish the exact action
whose digest is claimed, while excluding secrets, unsafe paths and arbitrary raw
provider payload. Truncation or redaction that can hide behavior makes the request
non-actionable. If the frozen public A1 DTO cannot represent a safe review for the
certified Bash input without violating its privacy contract, stop BLOCKED before
C3D rather than weakening either contract.

No handler, mobile CTA or new actionable record is enabled in C2D.

## 9. C3D — Atomic activation, mobile path and live acceptance

**Not authorized until independent C2D ACCEPT.** C3D is the only packet that may
activate Claude actionability.

One production-owned install transition must atomically bind:

- the canonical A1 store;
- the certified Claude service/version/platform tuple;
- actionable ingestion for subsequently created runtimes only;
- current RuntimeRef resolution;
- Claude delivery dispatch;
- the existing authenticated mobile approval handler and safe DTO.

The transition is all-or-nothing. Existing non-actionable records are never
upgraded. Uninstall, failed install, partial wiring or unsupported tuples leave
Claude capacity zero.

Final live evidence requires two fresh managed Claude sessions or invocations:

1. allow-once: mobile-authenticated claim, exact resume, matching consumed result,
   exactly one harmless execution and committed receipt;
2. deny: mobile-authenticated claim, exact resume, matching denial result, zero
   execution and committed receipt.

Also prove duplicate tap, stale epoch, replacement, timeout, stop/delete, daemon
restart, unsupported version and cross-session/request substitution negatives.
Run backend build/vet/full race, mobile TypeScript/Jest, Android/native gate when
available, invariant/secret scan, final-SHA ancestry/equality/clean checks, and
independent final A1.2 review.

## 10. Explicit non-goals

- C0H/C0R implementation or reinterpretation;
- generic provider/hook/plugin SDK;
- Claude Agent SDK or Channels;
- interactive PTY/key injection approval;
- automatic approval policy or allow-always;
- multiple parallel tool approvals;
- Task/Dispatch, N1, O1/O2 or Executor-Verifier;
- observer cleanup, tmux/cmux deletion, CLI redesign or cloud relay;
- persistence of pending approval authority across daemon restart;
- Windows implementation in this track (only the OS-neutral seam is required).

## 11. Final A1.2 acceptance gate

A1.2 remains incomplete until C1D, C2D and C3D are independently accepted and the
real provider-positive allow and deny paths pass end to end. Until then Claude
managed observation may be shown as non-actionable information only and N1 remains
blocked under the current product sequencing decision.
