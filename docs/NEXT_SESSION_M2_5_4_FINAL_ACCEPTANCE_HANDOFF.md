# Pokit Executor Onboarding — M2.5-4 Final Acceptance

Status: implementation complete, independent acceptance blocked by production-path proof gaps

Branch: `feature/phase10-multi-adapter`

Implementation baseline: `d2dfcf9` (`feat: complete M2.5-4 unified device authentication`)

This is the context-reset handoff for the next execution agent. Read it before
changing code. The immediate task is deliberately narrow: close the remaining
independent-verification evidence gaps around M2.5-4. Do not redesign the
accepted authentication architecture and do not begin M2.5-5, M3, or Transcript.

## 1. What Pokit is

Pokit is a local-first AI-agent runtime and mobile cockpit:

```text
pokit run <agent-or-command>
        ↓
controlled PTY owned by the user's Mac
        ↓
one session-owned Recorder (the only PTY reader)
        ↓
local terminal / mobile terminal / web terminal subscribers
        ↓
Activity / Transcript / status / approval / notifications
```

Pokit is not a cloud IDE. Commands, PTY state, recordings, and lifecycle
ownership stay on the user's computer. The primary product relationship is
`my computer ↔ my paired phone`.

The most important runtime invariant is:

> The Recorder is the single PTY reader. Every viewer is a subscriber.

No authentication or test change may cause a WebSocket, local attach client, or
mobile client to open or read the PTY independently.

## 2. Architecture already completed

Do not reopen these decisions unless a new production defect proves them wrong.

### Platform and runtime

- A5–A10: multi-agent platform, common contracts, capability-aware interaction,
  and diagnostics are accepted.
- E8f2: the session-owned Recorder is accepted. Capture is independent of viewer
  lifetime and multiple viewers do not duplicate PTY reads.
- Adapter classification is capability-based:
  - `controlled_pty`, `tmux`, and `localpty` use byte streams and may expose live
    terminal/reliable transcript capabilities;
  - `cmux` is observe-only with best-effort snapshot transcript behavior.
- E9/E10/E10b: controlled PTY runtime, `pokit run`, local/mobile attach, raw
  terminal bootstrap, and session lifecycle foundations are implemented.
- M0/M1/M1.5/M2: typed managed-session creation and Stop/Kill/Delete lifecycle
  contracts are accepted.

### Device trust security

- M2.5-0: security and Cloudflare transport-trust contract documented.
- M2.5-1: P-256 host identity and persistent device registry accepted.
- M2.5-2: LAN-only QR pairing and mutual key-possession proof accepted.
- M2.5-3: device challenge authentication, short-lived bearer sessions,
  canonical permissions, rate/cap limits, replacement, and revoke semantics
  accepted.
- M2.5-4 implementation exists at `d2dfcf9`; only final production-boundary
  acceptance remains.

Read these before editing:

- `docs/M2_5_DEVICE_TRUST_SECURITY_PLAN.md`
- `docs/M2_5_3_DEVICE_CHALLENGE_AUTH_PLAN.md`
- `docs/M2_5_4_UNIFIED_AUTH_ACCEPTANCE.md`
- `docs/ROADMAP_AFTER_E10B.md`
- `docs/M3_MOBILE_SESSION_LIFECYCLE_UX_PLAN.md`

## 3. Current M2.5-4 product contract

Pokit has exactly one authentication mode per listener configuration.

### Local development mode

`InsecureLocalOnly=true`:

- loopback binding and no tunnel;
- legacy development/Supabase authentication may remain for local compatibility;
- local diagnostics and terminal HTML may remain available.

### Remote device-auth mode

`InsecureLocalOnly=false`:

- challenge and verify are public authentication bootstrap routes;
- operational REST routes require a device bearer and canonical permission;
- `/term/ws` requires a short-lived, one-time WS ticket;
- legacy `dev-token` and Supabase credentials cannot authorize product routes;
- unsafe raw debug endpoints are not registered;
- invalid credentials produce `401`; a valid principal without permission gets
  `403` before the product handler runs.

The canonical permission vocabulary is defined in the device-trust package. Do
not duplicate permission strings inside handlers.

### WebSocket ticket contract

A ticket is random, digest-only in storage, single-use, and bound to:

- host identity;
- paired device identity;
- exact target terminal session;
- authorizing bearer-session identity;
- an immutable permission snapshot;
- the earlier of ticket TTL and bearer-session expiry.

Wrong host/session use consumes the ticket. Replacement, revoke, expiry, and
daemon restart invalidate authorization. Pending state is bounded by injectable
per-device and global limits. Consume, expiry, and revoke release capacity.

After upgrade, the actual device connection is registered in
`AuthenticatedConnRegistry`. Replacement, revoke, and exact bearer expiry must
close it. A principal without `terminal:input` may read output, but rejected
input must neither reach the PTY nor append Activity.

## 4. What `d2dfcf9` changed

The implementation kept the accepted middleware, ticket store, session manager,
and connection registry design. Primary files:

- `companion-daemon/cmd/devremote/app.go`
  - explicit local/remote route groups;
  - method-level permissions;
  - replacement/revoke invalidation wiring;
  - ticket-store configuration.
- `companion-daemon/internal/devicetrust/ws_ticket.go`
  - bounded ticket store, immutable grant, effective expiry, atomic bound
    consumption, and capacity accounting.
- `companion-daemon/internal/devicetrust/session.go`
  - bearer identity/expiry in `Principal` and invalidation callbacks outside
    locks.
- `companion-daemon/internal/devicetrust/conn_registry.go`
  - authenticated connection registration and device-wide close.
- `companion-daemon/internal/term/pty.go`
  - ticket-only remote upgrade, actual-device registration, expiry timer, and
    input authorization before Activity/PTY mutation.
- `companion-daemon/cmd/devremote/auth_e2e_test.go`
  - real app/router, route, controlled-PTY, and WS evidence.
- `companion-daemon/internal/devicetrust/ws_ticket_contract_test.go`
  - ticket expiry, binding, capacity, and invalidation contracts.

At `d2dfcf9`, `scripts/build-gate.sh` passes build, vet, race tests, mobile
typecheck, architecture invariants, and secret scanning.

## 5. Independent verification result

An independent verifier checked a clean detached checkout of `d2dfcf9`.

### Passed

- full build gate;
- targeted remote, WS-ticket, and device-session expiry race tests repeatedly;
- route/permission matrix against the acceptance document;
- structural rejection of legacy credentials in remote mode;
- per-device/global cap implementation and capacity release;
- real controlled-PTY WebSocket closure at bearer expiry;
- tracked worktree cleanliness.

### Verdict and remaining blockers

Final verdict was `REJECT` because of evidence gaps, not a newly identified
production implementation bug:

1. Replacement invalidation is proven with `authTestCloser`, not an actual
   controlled-PTY WebSocket through the real application router.
2. Device revoke invalidation is proven with `authTestCloser`, not an actual
   controlled-PTY WebSocket through the real application router.
3. Missing, invalid, replayed, wrong-host, and wrong-session tickets are not all
   proven through real WebSocket dial/upgrade boundaries.
4. Global ticket capacity is structurally mutex-atomic but lacks a concurrent
   cross-device production-contract proof.

Secret scanning detects realistic JWT bearer, `sk-`, and `ghp_` patterns. Broad
line/file exclusions are future hardening, not this task.

## 6. Your immediate role

You are the execution agent for a focused acceptance-remediation pass.

Add the missing production-boundary evidence and make only the smallest
implementation correction if a new test exposes a real defect. This is primarily
a test task. Do not weaken assertions to fit the implementation.

Exercise the real constructor, router, controlled PTY runtime, ticket issuance,
HTTP-to-WebSocket upgrade, connection registry, and invalidation callback chain:

```text
real App/router in remote mode
→ real paired-device principal/bearer
→ real controlled_pty session
→ ticket issued by authenticated production handler
→ dial /term/ws through the real router
→ confirm upgrade/subscription
→ trigger replacement or revoke through authoritative session manager
→ prove the real WebSocket closes promptly
→ prove Recorder/runtime ownership remains intact
```

Do not replace this with a fake `io.Closer`, direct registry call, parallel
test-only handler, or direct `HandleWS` invocation that bypasses routing.

## 7. Required acceptance tests

### A. Actual WebSocket replacement closure

- Establish a real remote controlled-PTY WS using a valid one-time ticket.
- Replace the device bearer through the real session-manager contract.
- Assert the original WS closes within a bounded timeout.
- Assert a pending ticket from the old bearer is invalid.
- Assert no second PTY reader or duplicate Recorder is created.

### B. Actual WebSocket revoke closure

- Establish a real remote controlled-PTY WS.
- Revoke through the authoritative device/session invalidation path.
- Assert the established WS closes within a bounded timeout.
- Assert pending device tickets are invalid.
- Assert the revoked device cannot regain authorization without pairing.

### C. Upgrade rejection matrix

Use a real WebSocket client against the real remote router. Prove rejection
before upgrade/subscription for:

- missing ticket;
- malformed/invalid ticket;
- replayed ticket;
- wrong-host ticket;
- wrong-session ticket.

Wrong binding retains the documented one-shot policy: the failed attempt consumes
the ticket and a later correct-target attempt also fails. Prove no recorder
subscription, PTY input, or Activity mutation occurs on rejection.

### D. Concurrent global ticket capacity

- Configure a small injectable global cap.
- Issue tickets concurrently from distinct device principals.
- Prefer the production issuance handler.
- Prove outstanding successes never exceed the global cap under `-race`.
- Prove consume/expiry releases a slot for a later device.

## 8. Non-goals and forbidden shortcuts

Do not:

- redesign authentication middleware, ticket grants, or the connection registry
  without a concrete failing production test;
- change Recorder or PTY ownership;
- add parallel `/api/d/*`, `/term/ws-ticket`, or test-only product routes;
- accept multiple credential types opportunistically in remote mode;
- restore legacy credentials to remote product routes;
- log or persist bearer tickets, keys, pairing secrets, or terminal input;
- weaken single-use or host/session/bearer binding;
- recreate owner permissions during upgrade;
- alter Live Terminal or Transcript behavior;
- begin M2.5-5, M3, R1 production integration, T0/T1 Transcript, notifications,
  or orchestration;
- refactor helpers solely for unit-test convenience.

If tests expose a product defect, fix the smallest root cause and add a regression
assertion. If they pass without production changes, a test-only commit is ideal.

## 9. Validation commands

Use focused tests while iterating, then the full gate:

```bash
cd companion-daemon
go test -race ./cmd/devremote -run 'TestRemote|Test.*WSTicket' -count=1 -v
go test -race ./internal/devicetrust -run 'Test.*WSTicket|Test.*Session' -count=1 -v
cd ..
sh scripts/build-gate.sh
```

Repeat new concurrency/WS tests with `-count=10` before handoff. Avoid sleeps as
correctness proof when a channel, deadline, or observable close is available.

## 10. Definition of done

M2.5-4 returns to independent verification when:

- replacement closes an actual production controlled-PTY WS;
- revoke closes an actual production controlled-PTY WS;
- all listed bad-ticket cases fail before actual upgrade and subscription;
- concurrent cross-device issuance cannot exceed global capacity;
- new tests pass under `-race` and repeated execution;
- full build gate passes;
- no secrets or unrelated generated artifacts are committed;
- code and `docs/M2_5_4_UNIFIED_AUTH_ACCEPTANCE.md` still agree;
- worktree is clean after commit.

Do not self-accept M2.5-4. Report `implementation evidence complete` and request
a fresh independent final verification.

## 11. Required executor report

Commit and push to `origin/feature/phase10-multi-adapter`, then stop. Report:

```text
M2.5-4 final evidence status
Commit and baseline

Production-path tests added
- replacement WS close
- revoke WS close
- upgrade rejection matrix
- concurrent cross-device global cap

Any product defect discovered and minimal fix
Recorder / controlled-PTY ownership proof
Focused race-test repetitions
Full build-gate result
Known non-blocking follow-ups
Verifier prompt requesting independent acceptance
```

## 12. What comes after acceptance

Only after independent acceptance:

1. M2.5-5 — paired-device revoke management and minimal content-free audit.
2. M3 — Android Keystore identity, challenge login, bearer refresh, WS-ticket
   acquisition, and mobile Create/Stop/Kill/Delete lifecycle UX.
3. T0/T1 — Transcript contract reset and byte-stream projection foundation.

R1 runtime-signal evidence already exists for later Transcript/status work. It
does not belong in this remediation pass.

Move narrowly: prove the security boundary through real product paths, preserve
the runtime architecture, and hand the result to a fresh verifier.
