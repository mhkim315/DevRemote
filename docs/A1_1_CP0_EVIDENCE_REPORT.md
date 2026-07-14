# A1.1 CP0 — Evidence Report (IN PROGRESS; not a completion claim)

Status: **CP0 IN PROGRESS — real allow/deny/resolved proven; three hard gates remain;
two findings contradict the plan. A1.1 / A1 / N1 remain BLOCKED. Capacity stays ZERO.**

Date: 2026-07-14. Executor: Claude Code. Authorized scope: CP0 only (plan §10, verdict
`docs/A1_1_PLAN_INDEPENDENT_VERIFICATION.md` §5). No production code changed; no CP1.

Evidence artifacts (redacted, in `docs/a1_1_cp0_evidence/`): `certification_identity.txt`,
`init_handshake.redacted.jsonl`, `command_approval_accept.json`,
`command_approval_decline.json`. Contract note + binding table:
`docs/A1_1_CP0_CONTRACT_NOTE.md`. The full 267-file schema bundle and 260 MB native
binary are NOT committed; they are identified by digest and reproducible with
`codex app-server generate-json-schema` on `codex-cli 0.144.1`.

## Gate-by-gate status (verdict §5 hard gates)

| # | Gate | Status | Evidence |
| --- | --- | --- | --- |
| 1 | exact binary/process-image + deterministic schema-bundle identity | **PARTIAL / at-risk** | binary chain + digests + schema manifest `cafb9881…` proven; the macOS *verified==spawned process image* + adversarial-replacement proof is NOT done (see §Blocked) |
| 2 | strict request shape + optional-field freeze | **PROVEN (with 1 contradiction)** | schema + live `command_approval_*.json`: `approvalId` absent, `command`/`cwd` present, `commandActions` display-only; BUT `environmentId="local"` not null (finding 1) |
| 3 | matching `item/started` + request identity | **PROVEN** | traces show `item/started`(commandExecution) then `item/commandExecution/requestApproval` with `threadId`/`turnId`/`itemId` + outer int id |
| 4 | real allow-once and deny response shapes | **PROVEN** | `{"id":0,"result":{"decision":"accept"}}` and `…"decline"` both accepted by the server |
| 5 | `serverRequest/resolved` ordering + consumption meaning | **PROVEN (happy path)** | resolved `{requestId:0,threadId}` observed AFTER our response, matching the request id, for both accept and decline |
| 6 | cancellation, duplicate response, timeout, child/orphan cleanup | **NOT DONE** | not yet traced |
| 7 | bounded real PRODUCTION thread/turn entry path | **NOT PROVEN** | the spike drove `thread/start`+`turn/start` directly; a bounded POKIT *production* entry that is not Task/Dispatch/terminal-replacement is a design question, unproven |
| 8 | redacted reproducible evidence | **DONE (for what exists)** | artifacts above; UUIDs/host/paths/email redacted |

## Proven this session

- Installed `codex-cli 0.144.1`; app-server is `[experimental]`, `stdio://` default,
  `ws://` experimental/excluded — matches the plan.
- Binary is a **node shim + vendored native** chain (not a single binary): PATH →
  `codex.js` (sha256 `134063e1…`) → spawns `aarch64-apple-darwin/bin/codex` (Mach-O
  arm64, sha256 `29915529…`). The certification must cover this chain or spawn the
  vendored native directly.
- Non-experimental schema bundle: 267 files, deterministic manifest `cafb9881…`.
- `CommandExecutionRequestApprovalParams`/`Response` confirm the plan's field roles and
  the `accept`/`decline` decisions (enum also has `acceptForSession`/`cancel`/amendments,
  all excluded by scope).
- Live `initialize` handshake (`platformOs=macos`, `version=0.144.1`).
- **Real E2E: a genuine `item/commandExecution/requestApproval` → our `accept`/`decline`
  → matching `serverRequest/resolved`**, driven only through the real authenticated
  app-server. `approvalId` absent for the shell approval, resolved `requestId` matches
  our response id, resolved arrives after our write.

## Findings that CONTRADICT the plan (reviewer decision needed)

1. **`environmentId` is `"local"` at runtime, not null.** The plan §6 and verdict §3.2
   froze `environmentId` to null/absent. The real `0.144.1` request carries
   `environmentId:"local"` for local execution. The scope must be amended to pin/accept
   the exact `"local"` value as authority (rejecting any other), or the freeze is
   incorrect. This is a required plan amendment; I did not silently change scope.
2. **`availableDecisions` is present WITHOUT `--experimental`** and lists
   `accept`/`acceptWithExecpolicyAmendment`/`cancel` (not `decline`) — yet `decline`
   still resolved. It should remain non-authoritative (the plan already excludes it),
   but the §2 "experimental" classification is inaccurate for the field's presence.

## Blocked / high-risk gate (verdict §3.1)

**macOS verified==spawned process image.** The verdict rejects an adjacent path recheck
and requires binding the actual spawned image to the verified digest, demonstrated with
a deterministic adversarial replacement, or CP0 is BLOCKED. This is unstarted and is the
highest-risk gate: macOS lacks `/proc/self/exe`; the distribution adds a node→shim→native
indirection. Per the mid-review platform constraint, this must be implemented behind an
explicit `ProcessImageAttestor` interface (OS/arch in the certification tuple; Windows
certifiable via the same observable protocol contract), with the Codex protocol contract
kept OS-neutral. Whether a deterministic macOS mechanism exists is an open CP0 question;
if it cannot be proven, CP0 stops BLOCKED on this gate.

## Environment constraint encountered

The live agent-driving traces were repeatedly blocked/interrupted by the harness safety
classifier (driving an external agent to execute escalated shell commands). The allow/deny/
resolved evidence above was captured in the windows where it was permitted. Gates #6 and
#7, and the §3.1 macOS spike, need either a permission allowance for the CP0 harness or a
follow-up session.

## Next steps (still CP0; no CP1, no capacity)

1. Reviewer decision on findings 1–2 (environmentId `"local"`; availableDecisions) — a
   plan amendment likely required before CP0 can be declared complete.
2. Cancellation/duplicate-response/timeout/child-orphan cleanup traces (#6).
3. macOS `ProcessImageAttestor` spike + adversarial replacement (#1/§3.1) — or BLOCKED.
4. Define/justify the bounded production thread/turn entry path (#7) — or BLOCKED.

A1.1, A1 and N1 remain BLOCKED. `provenActionMapping` empty; production delivery
capacity zero. Stopping at the CP0 boundary for review.
