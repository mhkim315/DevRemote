# SP1 Native Approval — Final Evidence Report (P1→P3)

Status: **SP1 P3 DELIVERED — submitted for final independent SP1/A1.1
review. LIVE accept AND deny resolved-after-written proof PASSED on the
pinned 0.144.1 provider. Full repository gate run on the frozen
implementation HEAD.**

Date: 2026-07-15. Executor: Claude Code, canonical checkout
`/Users/mhk/Documents/codex/DevRemote`, branch `feature/phase10-multi-adapter`.

- SP1 baseline (corrected contract + handoff): `f8a69c6c…`
- P1 ACCEPTED: `7acd7d4e…` (note `d1d9342`, impl `6c4389b` + R1)
- P2A ACCEPTED: `2345211b…` (note `f30ab08`, impl `fcd702d` + R1)
- P2B ACCEPTED: `190f699e…` (note `3e31a4d`, impl `4dd2a7d` + R1)
- P3 note: `9c2fc74c…` (`docs/SP1_P3_PACKET_CONTRACT_NOTE.md`)
- **Frozen P3 implementation HEAD: `dd6d05c8ba0f92af196374226c01d72ce3ef6440`**

## 1. What SP1 built (accepted chain)

| Packet | Authority delivered |
| --- | --- |
| P1 | Strict pump-only structured observation of `item/commandExecution/requestApproval`: closed-field decode with duplicate-key rejection, per-field byte bounds, lossless bounded top-level id, exact session/epoch/thread/turn binding, pinned authority version + environment + decision fingerprint; atomic pending+record (IngestObserved); per-record invalidation on resolved/turn-completion; non-actionable safe DTO on the authenticated read |
| P2A | Narrow store extension carrying daemon-generated immutable per-option delivery material (fingerprint + action-digest coverage, exact-bytes claim); pump-owned resolved router with the armed→write_claimed→written state machine on ONE respMu linearization point; success ONLY on a resolved observed strictly after the exact write; binding gains internal OptionID+DeliverySchema and the boundary admits only `allow_once→accept` / `deny→decline` under `codex.appserver.decision.v1` |
| P2B | ONE production-owned `InstallApprovalExecution` transition (activation precedes the first epoch; canonical store immutable after configure/install/runtime/shutdown; concurrent configure/install single winner); activation-gated actionable ingest for NEW certified requests only; managed `RuntimeOf`; dispatching delivery (certified boundary vs capacity-0 gate); `NewAppWithDeps` fail-fast on any unowned managed service |
| P3 | No new mobile code (the existing A1-E `SafeApproval` DTO + `resolveApproval` consume the managed rows); env-gated live production proof + live-certification corrections below |

## 2. Live-certification corrections (captured against the REAL pinned binary)

The CP0 redacted traces were a projection allowlist, not the raw wire. Two
deviations surfaced on live capture and were corrected evidence-exact, each
pinned by `TestManagedApproval_LiveWireShapeAdmitted` (exact captured lines,
secrets substituted):

1. **Extra params fields**: the real request carries `startedAtMs` and
   `commandActions` (the latter already named display/corroboration-only by
   the amended CP0 plan). Both are allowlisted as byte-bounded UNINTERPRETED
   raw fields — never authority, never stored, never projected or logged.
2. **No `jsonrpc` member on the live wire**: the real server request, the
   resolved notification, and the proven CONSUMED response all omit it. The
   envelope rule is now optional-if-present-exactly-`"2.0"` on all three;
   production response material is the live-proven
   `{"id":N,"result":{"decision":"accept|decline"}}`. `jsonrpc:"1.0"` remains
   rejected.

A third live finding is recorded for honesty: a naive "run this command"
prompt makes the model request escalated permissions, which the 0.144.1
router AUTO-REJECTS under `UnlessTrusted` without emitting any approval
request — the CP0 prompt recipe is required to force a real approval.

## 3. LIVE production proof (reviewer-mandated closure) — PASSED

`POKIT_SP1_LIVE=1 go test ./cmd/devremote -run TestSP1P3_LiveAcceptAndDeny`
on the REAL production composition: pinned `PinnedConfig0x144` identity with
fail-closed Verify (version + realpath + shim/native sha256), real exec
launcher, atomic P2B activation inside `NewAppWithDeps`, real IPC
`pokit run codex` structured create, authenticated (device challenge/verify
bearer) REST prompt/read/action routes, ambient `~/.codex` auth, per-run
temp CWDs (cleaned up).

```text
SP1-LIVE created managed session codex_app_server:codex-app-1784116378… (accept CWD)
APPROVAL ACTION: … approval=codexas-1-0 action=allow_once kind=approve outcome=accepted http=200
SP1-LIVE accept: resolved-after-written commit outcome=accepted
APPROVAL ACTION: … action=allow_once outcome=already_accepted http=200   (duplicate tap, same key)
APPROVAL ACTION: … action=allow_once outcome=already_owned http=409     (new key on resolved record)
SP1-LIVE accept: probe file exists (command executed)
SP1-LIVE created managed session codex_app_server:codex-app-1784116389… (deny CWD)
APPROVAL ACTION: … approval=codexas-2-0 action=deny kind=reject outcome=accepted http=200
SP1-LIVE deny: resolved-after-written commit (rejected)
SP1-LIVE deny: probe file absent (command not executed)
APPROVAL ACTION: … action=deny outcome=stale_runtime http=409           (after stop)
APPROVAL ACTION: … action=deny outcome=not_found http=404               (after delete)
SP1-LIVE stale/deletion fail closed
SP1-LIVE shutdown clean                                --- PASS (22.74s)
```

- **Accept**: HTTP 200/`accepted` is reachable ONLY through the P2A
  write→resolved witness (no other commit path exists), and the approved
  command **executed** (probe file present).
- **Deny**: same witness; record committed `rejected`; the command **did
  not execute** (probe file absent).
- **The reviewer's during-write concern did NOT materialize**: the real
  provider delivered `serverRequest/resolved` strictly after our exact
  response write for BOTH decisions — the positive production path is
  proven, so the BLOCKED condition does not apply.
- Duplicate tap, stale epoch (post-stop), deletion, live DTO privacy scan
  (command/cwd/probe path/decision/schema/raw JSON-RPC absent from the
  authenticated `/api/sessions` body) and clean shutdown are all in the
  same run.

## 4. Deterministic evidence inventory (all -race)

- P1: 12 observation tests (strict decode, bounds one-under/one-over,
  identity binding, duplicate, exhaustion, resolved/turn/exit/delete
  invalidation, privacy, composition) + live-wire-shape regression.
- P2A: 5 material tests + 17 boundary tests incl. known-bad
  check-then-write control, post-arm/post-write-claim/post-written-mark
  barriers, timeout+late-resolved, exit, stale bindings, malformed resolved
  non-vacuous, semantic mapping (swap/amendment/cancel/extra-field/tamper).
- P2B: 12 activation tests (production E2E via real HandleApprovalAction,
  no-queue-commit, preconditions, no-upgrade, RuntimeOf lifecycle,
  kill/delete deactivation, dispatcher, store immutability, concurrent
  single winner, degraded-off) + cmd fail-fast (3 unowned variants +
  control) + production composition (actionable safe DTO).

## 5. Final gate (frozen implementation HEAD `dd6d05c8ba0f92af196374226c01d72ce3ef6440`)

`sh scripts/build-gate.sh` run once on the frozen tree:

```text
--- Backend ---  go build OK / go vet OK / go test -race OK / git diff --check OK
--- Mobile ---   npm run typecheck OK / npm test OK / android kotlin compile OK
--- Invariants --- vendor branch scan OK / ID inference scan OK
--- Security --- secret scan OK
=== ALL GATES PASSED ===
```

This report commit is docs-only on top of that verified tree.

## 6. Honest scope notes

- Physical mobile-device smoke was not run (accepted SP0.5 precedent; the
  authenticated transport, strict decoder and approval UI are covered by the
  existing A1-E/SP0.5 mobile suites, re-run in the gate above).
- Approval state is in-memory by design (§7 of the corrected contract):
  daemon restart restores no authority.
- The live run consumed 2 pinned-provider turns (one per decision); traces
  above are bounded logs with no prompt echoes, tokens, or home paths.
- N1 remains blocked pending this review; nothing outside SP1 was started.

## 7. Review marker

```text
REVIEW REQUEST: SP1 FINAL (P3 + full contract) — dd6d05c8ba0f92af196374226c01d72ce3ef6440
```
