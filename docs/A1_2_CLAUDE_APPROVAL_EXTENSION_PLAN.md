# A1.2 Claude Approval Extension — Execution Plan

Status: **AUTHORIZED FOR C0 EVIDENCE ONLY. NO CLAUDE ACTIONABILITY YET.**

Baseline: SP1/A1.1 independently accepted at `2b940a6` with implementation
`dd6d05c`. The provider-neutral A1 store, authentication, immutable binding,
claim, idempotency, delivery receipt, commit, expiry, supersession, DTO, and
mobile transport are frozen.

## 1. Objective and estimate

Add one production-actionable Claude approval path for the exact managed
`pokit run claude` launch without introducing a generic provider SDK.

Expected effort if the stable hook surface passes C0:

| Packet | Scope | Engineering estimate |
| --- | --- | --- |
| C0 | exact-version surface and consumption evidence | 0.5–1 day |
| C1 | recognized managed launch + non-actionable hook observation | 0.5–1 day |
| C2 | exact allow/deny delivery, receipt, activation, lifecycle | 1–2 days |
| C3 | authenticated mobile/live proof and final gate | 0.5–1 day |

Expected implementation total: **3–5 engineering days**. With independent
review/remediation checkpoints, plan for **5–8 calendar days**. If C0 cannot
prove exact consumption from the stable hook, stop after at most one day; an
Agent SDK or permission-prompt-tool pivot is a separately reviewed expansion
and may add 2–4 days.

## 2. Official surfaces and stability classification

The current official Claude Code documentation establishes:

1. `PermissionRequest` command/HTTP hooks are a documented blocking decision
   surface. They carry `session_id`, `tool_name`, full `tool_input`, CWD,
   permission mode and optional permission suggestions, and return
   `allow`/`deny`. They explicitly do **not** carry `tool_use_id`.
2. `--settings` can inject a session-specific settings file. The implementation
   must not edit user or project settings. `--setting-sources` behavior must be
   proven so unrelated rules/hooks cannot bypass the managed approval path.
3. `--permission-prompt-tool` is documented for print mode and may be assessed
   only if the hook is insufficient.
4. Channel permission relay supplies a provider request ID and accepts only a
   verdict for an open request, but it is a **research preview**, custom
   channels require a dangerous development flag/allowlist bypass, and its
   request exposes only a truncated input preview. It is not the initial
   production authority surface.

Official references:

- <https://code.claude.com/docs/en/hooks>
- <https://code.claude.com/docs/en/cli-usage>
- <https://code.claude.com/docs/en/channels-reference>
- <https://code.claude.com/docs/en/agent-sdk/user-input>

Current local discovery (`2026-07-15`) reports Claude Code `2.1.209`. C0 must
pin and verify the exact executable/version/artifact used for evidence without
changing the user's global installation or copying credentials.

## 3. Non-negotiable authority contract

The provider-neutral A1 core stays authoritative for:

- authenticated requester and permission;
- ApprovalID and exact SessionID;
- provider/version and current runtime generation;
- canonical action and payload digest;
- claim ownership, idempotency, expiry and supersession;
- delivery receipt and terminal commit.

The Claude-specific boundary may only:

- receive one structured provider permission invocation;
- bind it to the exact managed runtime and exact hook process/invocation;
- validate and canonicalize one certified request type;
- block while the A1 decision is pending;
- deliver exactly one `allow` or `deny` response through the documented hook
  protocol;
- prove the exact invocation consumed the response or resolved to the expected
  provider outcome.

The following never create authority: terminal/PTY text, prompt matching,
screen state, `waiting_approval`, process name, CWD, transcript JSONL,
Notification hooks, timing proximity, fuzzy matching, generic send-text/key,
or Channel `input_preview`.

## 4. Candidate Claude request binding

C0 must prove or reject this complete binding before implementation:

| Field | Required source |
| --- | --- |
| POKIT SessionID | canonical recognized managed launch |
| LaunchGeneration | POKIT launch registry/current runtime |
| StreamGeneration | current managed stream where applicable |
| provider/version | exact pinned Claude executable certification |
| Claude session ID | hook `session_id`, exact equality to launch-owned state |
| hook invocation nonce | daemon generated, one blocking hook process/connection |
| hook process identity | daemon-owned child/connection identity, not PID alone |
| tool name | closed supported vocabulary |
| tool input digest | strict tool-specific canonicalization of full hook input |
| action | `allow_once` or `deny` only |

The daemon nonce is not a substitute for provider consumption evidence. It
only prevents cross-hook and replay confusion. No timestamp or input equality
heuristic may manufacture missing correlation.

## 5. Initial supported scope

Start with exactly one live-proven tool request type, preferably `Bash` if the
exact version exposes a stable bounded input schema. Initial actions:

- `allow_once` → hook decision `behavior: "allow"` with no persistent
  permission update and no input mutation;
- `deny` → hook decision `behavior: "deny"`, bounded Pokit-owned message,
  `interrupt:false` unless exact-version evidence requires otherwise.

Do not initially support always-allow rules, `updatedPermissions`,
`updatedInput`, session permission mode changes, AskUserQuestion, MCP tools,
file/network policy families, or concurrent batched approvals.

Public DTOs expose only the existing bounded Pokit-owned summary and option
labels. Full tool input, command, paths, hook payload, transcript path,
settings path, nonce, response bytes and digests remain server-side.

## 6. Packet C0 — bounded evidence gate

Timebox: one engineering day. No production actionability and no A1 core
change.

Required evidence against one exact Claude Code version:

1. dedicated exact executable identity and unchanged global installation;
2. direct recognized launch feasibility without `bash -c`;
3. session-scoped hook configuration without modifying user/project settings;
4. proof that unrelated allow/deny rules or hooks cannot bypass the managed
   hook; managed/organization policy conflicts fail closed;
5. exact raw-but-redacted PermissionRequest input shape and per-field bounds;
6. one hook subprocess/connection per invocation, blocking behavior, timeout,
   cancellation and child cleanup;
7. exact allow and deny output shape;
8. deterministic evidence of provider consumption/resolution for the same
   invocation, including concurrent/identical requests and local-terminal
   decision races;
9. daemon/Claude termination while pending produces no stale authority;
10. no secrets, raw command, transcript path or home path in committed
    evidence.

Decision:

- **PASS** only if the stable hook supplies a defensible exact invocation and
  consumption boundary. Proceed to C1.
- **BLOCKED** if hook stdout/exit is merely delivery with no exact-consumption
  proof. Do not infer success from later screen/transcript text.
- If blocked, separately compare the documented print-mode
  `--permission-prompt-tool` and Agent SDK callback. Do not silently switch
  runtime architecture.
- Do not promote Channel permission relay as production support while it
  remains research preview and lacks full canonical input.

## 7. Packet C1 — managed launch and observation

Only after independent C0 acceptance:

- make exact `pokit run claude` a structured recognized profile;
- launch the exact executable directly, never via a shell command string;
- inject only the POKIT-owned session settings/hook endpoint;
- bind PID + start identity where naturally available and preserve launch
  generation;
- ingest one exact request into the existing A1 store as **non-actionable**;
- maintain one bounded pending invocation per managed session initially;
- prove arbitrary `pokit run <command>`, ordinary attached Claude, old T2
  JSONL, terminal text and Notification hooks create zero actionable records.

Checkpoint, push, and stop for independent review.

## 8. Packet C2 — delivery and atomic activation

Only after C1 acceptance:

- add a thin Claude delivery boundary using the frozen
  `ApprovalExecutionBinding`;
- generate exact provider response server-side from the selected stored option;
- return one response to the exact blocking invocation;
- commit only after the C0-proven provider consumption/resolution witness;
- preserve duplicate/same-key behavior and reject modified digests;
- atomically install Claude actionability, RuntimeOf and delivery together;
- invalidate on hook cancellation, local decision, runtime replacement,
  stop/kill/delete, timeout and daemon shutdown;
- keep Codex routing and all other providers unchanged/capacity zero.

Deterministic barriers must cover approve-vs-deny, local-vs-mobile decision,
timeout-vs-response, replacement-vs-response and two identical concurrent
requests. Check intermediate state, not only final state.

Checkpoint, push, and stop for independent review.

## 9. Packet C3 — production closure

Only after C2 acceptance:

- use the existing authenticated SafeApproval DTO and mobile
  `resolveApproval`; add no second CTA authority;
- run real managed `pokit run claude` allow and deny turns;
- prove the exact provider invocation consumed each response;
- corroborate allow by exact tool execution and deny by non-execution after
  the provider turn resolves;
- prove duplicate tap, stale runtime, deletion, privacy and cleanup;
- run the full repository gate once on a frozen final implementation HEAD and
  again if a later report commit changes the scanned tree.

A1.2 is complete only after independent final review. If exact provider
consumption cannot be proven, retain non-actionable observation and report
BLOCKED.

## 10. Explicit non-goals

- no generic provider/plugin SDK;
- no modification of the frozen Codex boundary or provider-neutral A1 core
  without a demonstrated unavoidable dependency;
- no automatic approval policy or persistent allow rules;
- no terminal parsing or synthetic input;
- no Channel/chat product, N1 notification implementation, O1/O2, worker
  protocol, workspace isolation, observer cleanup, Windows/SSH expansion, or
  cloud relay;
- no support claim for arbitrary attached Claude sessions.

