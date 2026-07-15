# SP0.5 Managed I/O and Lifecycle — Evidence Report

Status: **SP0.5 A–C + R1 remediation DELIVERED — resubmitted for independent
review. SP1 / Cleanup NOT started. Approval capacity ZERO; `waiting_approval`
display-only; frozen A1 contracts and the accepted SP0 core (owned registry,
native authority, pinned runtime, process lease) untouched in shape.**

Date: 2026-07-15. Executor: Claude Code, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

- Handoff baseline: `3e535974288cbf2afc2894c89ff0d640cabdab47`
- Accepted SP0 implementation ancestry `2b35f524…`: PASS; CP0 `42429c8…`: PASS
- Contract note: `docs/SP0_5_MANAGED_IO_CONTRACT_NOTE.md` (`4e1f099`)
- Implementation commits: `9a77bab` (A), `4ecff55` (B), `5d45c9c` (C),
  `e38877f2b2110a198c71d57af56c6c17e2ff07b0` (R1 — final)
- Authoritative handoff: `docs/NEXT_EXECUTOR_SP0_5_MANAGED_IO_LIFECYCLE_HANDOFF.md`

## 1. Packet A — local structured I/O (`9a77bab`)

| Requirement (§4) | Evidence | Level |
| --- | --- | --- |
| Same structured create + ManagedCodexService | `CreateAttached` (no auto turn) via the same pipeline; `TestManagedInteractiveCreate_StructuredNoShellNoAutoTurn` (exact pinned exe/argv, 0 automatic turns) | contract proof |
| Legacy command-string fallback removed; disabled → unavailable | CLI sends the structured profile for the exact `codex` token detached or not; daemon rejects legacy `command:"codex"`; `TestManagedIPCCreate_LegacyCodexCommandRemoved`, `TestBuildRunCreateRequest_CodexAlwaysStructured`, existing disabled-fail-closed test | contract proof |
| managed-attach bound to SessionID (+cursor) | IPC op `managed-attach` → `handleManagedAttach`; wrong session fails closed | contract proof |
| Bounded UTF-8 line prompts only | `validateManagedPrompt` (non-empty, single line, UTF-8, no control bytes, ≤4096B); `TestManagedPrompt_BoundsFailClosed` (zero provider writes) | contract proof |
| One active turn; concurrent → conflict | turn claim under lock / write after release / rollback on failure; `TestManagedPrompt_SecondWhileActiveConflicts` (exactly 1 write) | contract proof |
| Prompts only via owned transport | `SubmitPrompt` is the single entry; runtime `send` is the only writer | production-wired |
| Structural allowlist projection, byte-bounded, no raw JSON-RPC | pump projects `turn/started`/`turn/completed`/`item/completed` `agentMessage.text` (schema-exact variant of the pinned 0.144.1 `ThreadItem`); stream leak scan (`jsonrpc`, threadId, item/turn ids, prompt echo all absent) | contract proof |
| Render without terminal takeover | thin line client `attachManagedSession` (no raw mode, no keymaps, no TUI) | production-wired |
| Ctrl-D detaches only | `TestManagedAttach_DetachDoesNotKillRuntime` (+ live) | contract proof |
| One bounded live turn | LIVE (below) | physical/live |

**Live A proof** (ONE pinned turn, `POKIT_SP05_LIVE=1`, run once):

```text
SP05-LIVE created interactive managed session codex_app_server:codex-app-1784089433554553000
SP05-LIVE event → working / assistant READY / completed
SP05-LIVE ordered kinds: [working assistant completed]
SP05-LIVE detach left session alive (status=completed)
SP05-LIVE shutdown clean          --- PASS (18.64s)
```

**Defect found and fixed by the first live run**: the IPC request decoder
buffered bytes past the request line (kernel write coalescing), silently
swallowing an early prompt. Fixed by recovering `dec.Buffered()` into the
attach reader; deterministic regression
`TestManagedAttach_CoalescedRequestAndPrompt`.

## 2. Packet B — authenticated mobile projection (`4ecff55`)

- Event surface: `GET /api/managed-sessions/{id}/events?epoch&cursor` —
  bounded snapshot (owned-registry DTO) + monotonic events; duplicate cursor
  idempotent; wrong session 404 / stale epoch 409 / missing epoch 400 /
  malformed cursor 400 / cursor-ahead 409 / disabled 404
  (`TestManagedEventsAPI_*`). Overflow inserts an explicit `gap`; text bound
  4096B (`TestManagedEventsAPI_OverflowGapAndTextBound`).
- Input: `POST /api/managed-sessions/{id}/prompt {epoch,text}` —
  `DisallowUnknownFields` + `MaxBytesReader`; delivery only via
  `SubmitPrompt`; conflict/stale/oversize/unknown-field fail closed with zero
  extra provider writes (`TestManagedPromptAPI_FailClosed`).
- Auth: both modes wired; remote anonymous events/prompt → 401 (device
  principal, sessions:read / terminal:input); dev token reaches handlers in
  insecure-local (route test). Server derives identity; the body carries only
  epoch+text. No `/term/ws` PTY framing is reused.
- Mobile: strict fail-closed decoder (`managedSession.ts`: exact contract
  version, unknown-field rejection at every level, closed kind/status
  vocabularies, monotonic seq, session/epoch-bound events, bounded text) +
  `ManagedSessionFeed` stale-commit guard (session switch, epoch change,
  unmount, replay). `ManagedSessionView` shows bounded output + one prompt
  input, clearly labeled native managed; `FeedScreen` routes
  `codex_app_server:` sessions to it (adapter-namespace branch). 11 Jest
  tests; full mobile suite 394 green; tsc clean.
- Cross-end contract: `TestManagedEventsFixture_MatchesMobileDecoderInput`
  pins the backend-marshaled JSON byte-identical to the fixture the real
  mobile decoder test consumes.
- **Honest scope**: mobile physical-device smoke was NOT run (handoff: "only
  if required"); the mobile path is proven by the shared fixture, decoder
  tests, and the production REST tests. No approval buttons, no TUI, no
  notifications.

## 3. Packet C — lifecycle and reconnect (`5d45c9c`)

- `Stop` (input closed → group TERM → bounded wait → KILL escalation → reap;
  non-current on return; idempotent), `Kill` (immediate), `Delete`
  (terminal-only; record + bounded output data removed; later events/prompts
  inert; duplicate → clean not-found). Exact SessionID+epoch binding on every
  op; wrong/stale binding never touches the child. Direct
  `ManagedCodexService` binding — no mux.Adapter discovery.
- REST stop/kill/delete in both auth modes behind sessions:stop /
  sessions:kill / history:delete with bounded `{epoch}` bodies and bounded
  post-op DTOs (`TestManagedLifecycleAPI_FailClosedAndIdempotent`).
- Reconnect = transport reconnect to the live daemon: re-attach with the
  consumed cursor streams only strictly-newer seqs
  (`TestManagedReconnect_SnapshotCursor_NoStaleState`); prior-epoch reads are
  epoch-gated (B tests); daemon restart recovery remains deferred.
- Interleavings: prompt-vs-stop, prompt-vs-kill, natural exit then idempotent
  lifecycle, shutdown during a live turn (no child/lease/runtime/current
  state — SP0 lease drain preserved), concurrent
  prompt/stop/kill/read race under `-race`.
- Resize is honestly N/A for native sessions (no PTY; no fake capability).

## 4. Acceptance self-audit (§10)

Every checklist item maps to the rows above; approval stays non-actionable
(capacity zero, no CTA); no terminal emulator, provider SDK, plugin system,
persistence, restart recovery, Claude/Windows, or observer cleanup was built.
The only interface additions are the handoff-allowed ones: the versioned
managed event DTO, `Term()` on the existing narrow process seam, and the
Codex-specific projection isolated in the pump.

## 4a. Remediation round R1 (reviewer blockers 1–4, all fixed)

Reviewer verdict on `ce9add6`: REJECT with four blockers. All fixed in
implementation commit `e38877f2b2110a198c71d57af56c6c17e2ff07b0`
(`fix(SP0.5-R1)`):

1. **Mobile polling kept stale status current** — new `ManagedSessionPoller`
   (production controller consumed by `ManagedSessionView`): one request in
   flight, per-request deadline, freshness expiry → `unavailable`/
   non-current (history preserved, rendered as stale); only a NEW fresh
   snapshot restores current; `close()` rejects post-unmount commits.
   Fake-timer tests cover one-in-flight, deadline release, failure-past-
   freshness invalidation, reconnect restore, and closed-poller inertness.
2. **Byte bounds diverged from the contract** — backend `boundUTF8`
   truncates on UTF-8 rune boundaries (multibyte never split; exact bound
   reachable, proven); mobile validates UTF-8 BYTES via `utf8ByteLength` —
   the reviewer counterexample (`'😀'.repeat(2048)` = 4096 UTF-16 units /
   8192 bytes) is rejected while exact ASCII (4096) and multibyte (1024
   emoji) bounds pass; `ManagedSessionFeed` capped at 256 with an explicit
   leading gap marker (long-poll accumulation test).
3. **one-active-turn not bound to turn identity** — the runtime now binds
   the claim to the EXACT provider turn id taken from the turn/start
   RESPONSE via JSON-RPC id correlation (no first-come adoption); working/
   completed/assistant projections all require exact current-turn equality;
   an error response releases the claim; fakes upgraded to the schema-exact
   payload shape (`params.turn.id` / `params.turnId`). Deterministic
   interleaving test reproduces the reviewer scenario (late duplicate
   completion of turn A during turn B → claim held, status working, third
   prompt conflicts) with a known-bad control (the exact id releases), plus
   ghost started/completed fabrication tests.
4. **Missing paired-device production-route evidence** — the remote fixture
   now injects a deterministic managed service (exported narrow
   `ManagedLauncher`/`ManagedProcess` seam + `NewManagedCodexServiceForTest`
   + `Dependencies.Managed`; the composition root still always builds the
   pinned production service). With REAL challenge/verify bearers on the
   real remote router: owner reads list/events and prompts (exactly one
   provider write); read-only member reads but prompt → 403 with ZERO
   provider writes; legacy dev token → 401; a bearer minted by another
   host → 401; the projected assistant output is readable by the paired
   member. The host-bound mobile transport is proven to drive the same
   routes (URL/method/body) and to refuse non-paired hosts. Trust-boundary
   hardening: REST prompt/lifecycle decoders reject trailing JSON; the
   local attach input line is Scanner-bounded.

No live provider turn was re-run (the A live proof stands; R1 did not
change the pinned launch pipeline's happy path — full deterministic suites
re-run green).

## 5. Final gate (frozen remediation HEAD)

Run once on frozen implementation HEAD
`e38877f2b2110a198c71d57af56c6c17e2ff07b0` via `sh scripts/build-gate.sh`:

```text
--- Backend ---  go build OK / go vet OK / go test -race OK / git diff --check OK
--- Mobile ---   npm run typecheck OK / npm test OK / android kotlin compile OK
--- Invariants --- vendor branch scan OK / ID inference scan OK
--- Security --- secret scan OK
=== ALL GATES PASSED ===
```

This report commit is docs-only on top of that verified tree.

## 6. Review marker

```text
REVIEW REQUEST: SP0.5 Managed I/O and Lifecycle — e38877f2b2110a198c71d57af56c6c17e2ff07b0
```
