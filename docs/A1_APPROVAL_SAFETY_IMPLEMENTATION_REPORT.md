# A1 Approval Safety — Implementation Report

Status: **READY FOR INDEPENDENT A1 VERIFICATION**

## 1. Canonical identity & SHAs

```text
remote:  https://github.com/mhkim315/DevRemote.git
branch:  feature/phase10-multi-adapter
baseline (accepted S1.1 + A1 handoff):  ace458056699c26a323eea49d44f8dc1efdaded7
implementation HEAD (A1-E, final code): 3b56c4f05d16653b5fd9c494d8f1682135701603

per-packet commits:
  A1-A  41a6b83e9  freeze state machine + outcome vocabulary
  A1-B  143d256e1  generation-bound authoritative ApprovalStore
  A1-C  15fd6d20e  accepted-adapter ingestion, legacy parser authority removed
  A1-D  c03fb06e7  authenticated exact-action execution boundary
  A1-E  3b56c4f05  strict mobile approval path over host-bound device transport
```

Baseline ancestry verified: HEAD descends from the accepted S1.1 implementation
`02c8385` and report `895a6b3`, both ancestors of the A1 handoff commit
`ace458056`. Worktree clean; local == origin after push.

## 2. Production call graph (final)

```text
TelemetryService.processSession (accepted, version/correlation-gated batch)
  └─ status path (unchanged S1.1)
  └─ approval path (A1-C):
       version conflict / correlation loss / stream-gen change / launch replace
          → AuthoritativeApprovalStore.InvalidateSession
       session delete/disappearance → .Clear
       managed-launch correlated batch → ingestApprovals:
          adapter.DetectApproval  (ONLY if descriptor declares CapApprovalDetection;
                                    panic/error isolated → zero approvals)
          → re-bind exact source-event provenance (matched by ApprovalID)
          → frozen per-provider option mapping (no terminal-derived, no blind y/n)
          → AuthoritativeApprovalStore.Ingest(sessionID, launchGen, streamGen,
                                               provider, version, items)

HTTP POST /api/sessions/{id}/approvals/{approvalId}
  insecure-local: h.AuthMiddleware (dev)          → Handlers.InsecureLocalOnly=true
  remote (prod):  RequirePrincipal(PermTerminalInput) → Handlers.InsecureLocalOnly=false
  → HandleApprovalAction (A1-D):
       strict bounded decode (MaxBytesReader, DisallowUnknownFields, no trailing,
                              bounded/valid input)
       → LookupRecord
       → auth-context bind (non-local: principal carries the record's RequiredPerm)
       → validate exact option + input contract
       → revalidate runtime identity (LaunchGenOf vs record.LaunchGen)
       → Reserve (atomic pending→executing, at-most-once)
       → deliver ONLY the exact action:
            decision requiring confirmation (approve) → Fail → delivery_failed
            reject/cancel → Commit, no command
            fire-and-forget input → enqueue → Commit
       → finishApproval: closed ActionOutcome + HTTP; audit = IDs/outcome only

Mobile: /api/sessions row → decodeApprovals(raw, sessionId) [strict, fail-closed]
  → pendingApprovals (status==='pending') → ApprovalCard
  → resolveApproval via apiPost (host-bound device bearer; fail-closed non-paired
    origin; no non-idempotent replay) → strict 2xx decode
  waiting_approval activity: display-only, drives no CTA (unchanged, retested).
```

## 3. Authority & state matrices

Internal state machine (`approval_contract.go`, closed & total):

| state | terminal | public projection |
|---|---|---|
| pending | no | pending |
| executing | no | pending |
| approved | yes | approved |
| rejected | yes | rejected |
| resolved | yes | resolved |
| delivery_failed | yes | delivery_failed |
| expired | yes | expired |
| invalidated | yes | invalidated |

Legal transitions (only these): pending→{executing,expired,invalidated};
executing→{approved,rejected,resolved,delivery_failed}. No executing→pending
(no re-offer); no executing→invalidated (a committed decision resolves to one
terminal outcome; mid-delivery replacement → delivery_failed).

Authority sources: an approval exists ONLY via accepted `DetectApproval` gated by
`CapApprovalDetection` + `SafeApprovalGate` (authoritative provenance, confidence
≥ floor, non-empty ApprovalID) + a frozen provider option mapping. `waiting_approval`
runtime status, legacy parser text, screen/PTY/process, and terminal capability
never create a record, option, or CTA.

## 4. Action delivery semantics

The owned delivery boundary is the CommandBroker (enqueue). It cannot confirm the
agent consumed a command. Therefore:

- **decision requiring confirmation** (Kind `approve`): the terminal fallback
  cannot confirm agent acceptance → **fail closed** (`delivery_failed`, HTTP 502),
  no command synthesized, never reported as success. For Codex this means remote
  approve is surfaced (exact ID bound) but honestly non-executable until a
  separately-accepted Codex resolution channel exists; the user resolves in the
  live terminal.
- **denial** (Kind `reject`/`cancel`): recorded as the user's decision with **no
  terminal command** (no-command-on-rejection). A denial is the safe default and
  needs no agent receipt to be a valid decision record.
- **fire-and-forget raw input** (neutral option with an explicit input contract):
  the user's literal input is enqueued (enqueue is the accepted semantics) then
  committed `resolved`. Never a synthesized decision keystroke.

A decision is committed to a resolved state ONLY after its delivery path is
handled; the at-most-once `executing` reservation is entered before delivery, so a
concurrent second submit is refused and delivery occurs at most once.

## 5. Privacy & auth evidence

- Audit log lines carry `session, approval, action, kind, outcome, http` only —
  never raw input, prompt, path, or token. Diagnostics are bounded and secret-
  sanitized upstream (`SanitizeDiagnostic`).
- Remote route requires a device bearer with `PermTerminalInput`
  (`RequirePrincipal`); the handler additionally binds the principal's permission
  to the record's `RequiredPerm` (defense-in-depth). A legacy/Supabase/arbitrary
  bearer, a member (read-only) device, a missing/revoked bearer, or a non-local
  request without a principal all fail closed with no store mutation.
- Public DTO change is bounded and consumer-justified: the `AgentApproval.status`
  vocabulary is extended to the closed set {pending, approved, rejected, resolved,
  delivery_failed, expired, invalidated}; internal binding (launch/stream gen,
  provenance, action digest, required perm) is NOT exposed. `executing` stays
  internal.
- Mobile sends the device bearer ONLY to the paired origin (host-bound apiPost),
  never to a non-paired host (zero requests), and never leaks it in errors.

## 6. Tests & environmental skips

Backend (`internal/term`, race):
- contract-freeze proof (closed/total state machine, transitions, projection,
  outcome vocabulary);
- store (19, race): cross-session, older-gen create/update/restore, changed
  action-set, expiry boundary, replay, mutation aliasing (+ negative control),
  bounded eviction (terminal-first), delete/recreate, restart, correlation-loss
  invalidation, concurrent churn;
- ingestion: Codex positive (exact ID/provenance/generation binding, no blind
  payload), Claude/no-capability/unknown-provider → zero, non-authoritative /
  low-confidence / id-less dropped, DetectApproval panic+error isolation;
- action boundary + A1-D: reject-no-command, approve-fails-closed, unknown-action
  / not-found / expired / duplicate / invalidated, input-contract, fire-and-forget
  delivery, strict decode (unknown field / trailing / oversize / invalid-UTF8
  normalized), launch replaced/disappeared/matching, concurrent double-submit
  at-most-once, cross-session, composed device-auth route (owner 200, missing 401,
  member 403, revoked 401, non-local-no-principal 403) — each proving no store
  mutation on auth failure.

Mobile (jest, 385 total incl. 28 new; tsc clean):
- strict decoder: unknown field, missing/mistyped field, closed status set,
  session binding, option kind/fields, bounds, confidence, RFC3339, resolvedAt;
- host-bound transport: exact route + device bearer only (not legacy), input body,
  fail-closed non-paired origin (0 requests), outcome status surfacing (502/409/
  410/400), 401-vs-403, malformed-2xx no-false-success, network one-attempt,
  bearer never leaked, legacy path;
- E2E: accepted-Codex DTO → strict decode → delivered reject over device bearer;
- status-only / no-capability negatives (waiting_approval and Claude → no CTA).

Gate (handoff §6) on `3b56c4f05`:
- `gofmt -l <touched-go-files>` clean; `go build ./...` OK; `go vet ./...` OK;
  `go test -race ./internal/agent/... ./internal/term/... ./internal/transcript/...`
  OK; `git diff --check` OK.
- `scripts/build-gate.sh`: Backend OK, Mobile typecheck + test OK.
- **Environmental skip**: the android-native Kotlin compile step needs `./gradlew`,
  which is absent in this environment. A1 changes **zero** android/kotlin/gradle
  files (Go backend + TypeScript only), so this step is not-run for environment
  reasons, not an A1 regression.

## 7. Scope adherence

No N1/O1/O2, no new adapter, no distribution, no Task/Dispatch/notifications/
orchestration/automatic execution. Legacy parser approval authority removed;
terminal-derived options removed; no blind `y\n`/`n\n`; no resolve-before-delivery
success; no legacy auth fallback on the paired-device route.

```text
REVIEW REQUEST: A1 Approval Safety — 3b56c4f05d16653b5fd9c494d8f1682135701603
```
