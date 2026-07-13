# A1 Approval Safety — Authority Audit & Contract Freeze (A1-A)

Baseline: `feature/phase10-multi-adapter` @ `ace458056` (accepted S1.1).
Scope: A1 only (A1-A..A1-E). No N1/O1/O2, no new adapter, no distribution.

This is the A1-A deliverable required by
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_HANDOFF.md` §4: current-vs-target call
graph, DTO diff decision, threat matrix, frozen state machine / error vocabulary,
and the per-packet blocker→test matrix. The frozen vocabulary itself is code:
`internal/term/approval_contract.go` (proved closed/total by
`internal/term/approval_contract_test.go`).

## 1. Current production call graph (what actually runs today)

```text
TelemetryService.processSession (internal/term/telemetry_service.go)
  └─ legacy parser path (lines ~278-313)
       parser.Parse(line) → models.AgentEvent{Type:"approval_requested"}
       → for each: AgentApproval{
            ID:        fmt("%s-%s", sessionID, e.ID)   // DERIVED, not provider ID
            AgentKind: logRef.Agent
            Kind:      "approval"
            Status:    "pending"
            Prompt:    firstNonEmpty(e.Detail,e.Summary,…)
            Options:   buildInteractionOptions(sess)    // from TERMINAL capability
            Default:   "reject"                         // hardcoded
            Source:    "jsonl"                          // fixed
            Confidence:0.9                              // fixed
         }
       → s.approvals.Upsert(id, newApprovals)          // no generation binding

  └─ accepted-adapter path B1 (lines ~182-276)  [Transcript + status ONLY]
       callAcceptedAdapter → adapter.ReadEvents → []agent.AgentEvent
         (events DO carry ApprovalID, native_log provenance, confidence 0.9)
       → Transcript.ProjectAgentEvents / statusStore.Update
       → DetectApproval is NEVER called on this batch          ← A1-C gap

memoryApprovalStore (internal/term/approvalstore.go)
  key: sessionID → []AgentApproval, merge by ID, 5-min expiry, cap 50
  NO launch/stream generation, NO provenance digest, NO action contract,
  NO authorization context, mutable-by-reference slice.                ← A1-B gap

HTTP: POST /api/sessions/{id}/approvals/{approvalId}
  insecure-local: h.AuthMiddleware (Supabase JWT/dev-token)
  remote (prod):  devicetrust.RequirePrincipal(sessionMgr, …, PermTerminalInput)
  → HandleApprovalAction (internal/term/approval_handler.go)
       Lookup → findOption → validate input
       → Resolve(store) FIRST                                    ← resolve-before-delivery
       → Cmds.Put(sessionID, payload)  (fire-and-forget, no ack) ← A1-D gap
       → 200 {"status":"ok"}

Mobile: DashboardScreen → pendingApprovals (status==='pending') → ApprovalCard
  → resolveApproval() uses checkedFetch + optional Supabase token   ← A1-E gap
     (NOT the host-bound device transport the prod route requires)
  waiting_approval status = display-only ("Awaiting Approval"),
     sessionNeedsApproval reads approvals[] only  ✓ already correct
```

## 2. Target production call graph (A1 end state)

```text
processSession accepted-adapter batch (the SAME version/correlation-gated events
  already fed to Transcript/status)
  └─ IF descriptor declares CapApprovalDetection:
        adapter.DetectApproval(events)  (panic/error isolated → no approval + diag)
        → []AgentApproval bound to exact ApprovalID + source event
     ELSE (Claude, no-capability): zero approvals
  └─ legacy parser approval creation REMOVED as authority
        (may persist only as clearly-separated non-actionable diagnostic history)
  → ApprovalStore.Ingest(sessionID, launchGen, streamGen, version, provider, reqs)
        each request immutably bound; older generation cannot update/resolve/restore;
        runtime replacement / stream-gen change / correlation loss / delete → invalidate

HandleApprovalAction
  → strict bounded body decode (exactly one action + optional input; reject unknown
     fields, oversize, bad UTF-8)
  → revalidate CURRENT store request AND current runtime identity
     (session, approval id, provider/version, launch gen, stream gen, action, expiry,
      auth context) — any mismatch → closed ActionOutcome, fail closed
  → CAS pending→executing (at-most-once; concurrent 2nd submit → already_terminal)
  → deliver exact action to owned delivery boundary
  → commit executing→{approved|rejected|resolved} ONLY on durable acceptance,
     else executing→delivery_failed (never a false success)
  → audit log: IDs + outcome code only

Mobile
  → resolveApproval routed through host-bound device transport (device bearer)
  → strict bounded decoder for requests/options/results (closed states+kinds,
     exact session binding, field/array/string bounds, unknown-field policy)
  → CTA rendered only from pending authoritative store DTOs; waiting_approval alone
     shows no CTA; session switch/disconnect/unmount/expiry/replacement clears state
```

## 3. Frozen internal state machine & outcome vocabulary

Code: `internal/term/approval_contract.go`. Summary:

States (closed, total): `pending`, `executing`, `approved`, `rejected`,
`resolved`, `delivery_failed`, `expired`, `invalidated`.
Non-terminal: `pending`, `executing`. All others terminal (⇒ at-most-once).

Legal transitions (the ONLY edges):

| from | to | trigger |
|---|---|---|
| pending | executing | user decision accepted; reserve before delivery |
| pending | expired | TTL elapsed, no decision |
| pending | invalidated | launch/stream gen change, correlation loss, session delete |
| executing | approved / rejected / resolved | delivery durably accepted (by option Kind) |
| executing | delivery_failed | delivery unconfirmable — fail closed |

No `executing→pending` (no re-offer) and no `executing→invalidated` (a committed
decision resolves to exactly one terminal outcome; mid-delivery replacement →
`delivery_failed`).

Outcome vocabulary (closed): `ok`, `not_found`, `session_mismatch`, `expired`,
`already_terminal`, `stale_generation`, `unknown_action`, `input_rejected`,
`delivery_failed`. Only `ok` maps to a 2xx; every other outcome is a non-2xx
denial, and an unknown outcome maps to ≥500 — no denial can read as success.

## 4. DTO diff decision

- **Public `AgentApproval` shape**: unchanged fields. No new fields exposed for
  internal binding (launch gen, stream gen, provenance digest, action contract,
  authorization context). Those live on an INTERNAL store record and are projected
  away on `List`, mirroring the accepted `AgentActivityRecord`→public split.
- **Public `AgentApproval.Status` vocabulary**: BOUNDED extension from
  {`pending`,`approved`,`rejected`} to the closed set
  {`pending`,`approved`,`rejected`,`resolved`,`delivery_failed`,`expired`,
  `invalidated`}. Justification: the frozen safety invariant "a failed or
  unavailable delivery must not be falsely recorded as successfully executed"
  requires a consumer-visible `delivery_failed`; `resolved` distinguishes neutral
  resolution; `expired`/`invalidated` let the client render honestly instead of a
  value silently disappearing. `executing` stays internal (projects to `pending`).
- **Mobile decoder (A1-E)** mirrors exactly `publicApprovalStatusValid`. Adding
  values does not break the existing `status === 'pending'` CTA test.
- No other public DTO additions in A1.

## 5. Threat matrix (attacker/error → required fail-closed behavior)

| # | Threat / failure | Current | Required (packet) |
|---|---|---|---|
| T1 | Spoofable prompt/screen/PTY text manufactures an approval | legacy parser can emit approval_requested from text | authority only via `DetectApproval` gated by `SafeApprovalGate` (authoritative provenance + conf ≥ floor + non-empty ApprovalID) (C) |
| T2 | Adapter without capability fabricates approval | not gated | call `DetectApproval` only if `CapApprovalDetection` declared; Claude → 0 (C) |
| T3 | Options invented from terminal input capability | `buildInteractionOptions(sess)` | options only from accepted provider evidence / frozen exact mapping (C) |
| T4 | Cross-session approval id | store keys by id within session but ingestion doesn't verify | exact session binding at ingest and at action (B,D) |
| T5 | Duplicate approval id with a CHANGED action set | Upsert preserves first, ignores changed options silently | reject/replace under generation rule; changed contract cannot mutate a live request (B) |
| T6 | Stale/older launch or stream generation updates/resolves/restores | no generation at all | (launchGen,streamGen) rule: older can't update/resolve/restore; replacement invalidates (B) |
| T7 | Replay of a resolved/expired request | Resolve returns false if resolved/expired (partial) | terminal states never re-transition; generation/epoch guard (B,D) |
| T8 | Mutation aliasing of stored request | store returns slice of structs by value on List but holds mutable slice; options slice shared | immutable-by-copy in and out (B) |
| T9 | Resolve-before-delivery false success | Resolve() then Cmds.Put(); success reported regardless of delivery | reserve→deliver→commit; delivery_failed on unconfirmable (D) |
| T10 | Concurrent double-submit executes twice | Resolve idempotent but Cmds.Put runs before the conflict check on the losing path? (Put only after Resolve true) — still no in-flight lock | CAS pending→executing; at most one delivery (D) |
| T11 | Expiry/replacement between lookup and commit | expiry checked once, before Resolve; no re-check at commit | revalidate current request + identity immediately before delivery (D) |
| T12 | Unknown/oversized/bad-UTF-8 body fields | decoder ignores unknown fields; no size/utf8 bounds | strict bounded decode; reject unknown/oversize/bad-utf8 (D,E) |
| T13 | Wrong device / legacy bearer authorizes action | mobile sends legacy token; prod route needs device bearer | route through host-bound device transport; PermTerminalInput only (D,E) |
| T14 | Raw prompt/input/token/path leaked to logs/DTO | audit log avoids raw input already | keep IDs/outcome codes only; bounded diagnostics; no raw payloads (all) |
| T15 | `waiting_approval` runtime status drives a CTA | already display-only ✓ | keep display-only; no store record/option/CTA from status (all) |
| T16 | Daemon restart resurrects prior pending authority | in-memory store lost on restart (acceptable) but no launch-gen guard | begin without prior pending authority; new launch gen (B) |

## 6. Per-packet blocker → regression-test matrix

Each blocker maps to at least one negative/production-path test that fails if the
blocker is reintroduced. A1-A ships the frozen-vocabulary proof
(`approval_contract_test.go`); the blocker tests below land in their own packets
(they require code introduced by that packet, so they turn green at that
checkpoint — per the protocol, a packet's own tests must pass at its commit).

- **A1-B**: cross-session id; duplicate id w/ changed action set; older launchGen;
  older streamGen; expiry boundary; replay after resolve; mutation aliasing
  (in & out); bounded eviction determinism; delete/recreate; restart-fresh;
  race churn (`go test -race`).
- **A1-C**: Codex positive preserves exact ApprovalID + source binding; Claude →
  0; no-capability adapter → 0; DetectApproval panic/error → 0 + bounded
  diagnostic, no crash; legacy parser text/screen/PTY/heuristic/id-less/
  cross-session/unsupported-version/lost-correlation → 0 actionable requests;
  options not derived from terminal input capability.
- **A1-D**: exact success; delivery failure → delivery_failed (not ok); concurrent
  duplicate executes at most once; expired between lookup and commit; replacement
  between lookup and delivery; revoked device (401/403 via middleware); wrong
  permission; cross-host/session; no command emitted on rejection.
- **A1-E**: strict decoder rejects unknown fields / out-of-bounds / bad kind /
  wrong session; UI renders CTA only from pending store DTO; waiting_approval
  alone → no CTA; session switch/disconnect/unmount/expiry/replacement clears
  state; submission disabled while one request in flight; E2E accepted-Codex →
  store/API/device-bearer → mobile decode; Claude no-capability + status-only
  negatives stay green.

## 7. Prohibited (restated from handoff §5)

No approval from `agentActivity.status`; none from legacy parser text / screen /
prompt / process / terminal capability; no static `orchestrationSafe`; no
resolve-before-delivery success; no blind `y\n`/`n\n` without accepted provider
action mapping; no unreviewed public DTO expansion beyond §4; no raw prompt
exposure, automatic retry of non-idempotent input, or legacy auth fallback; no
N1/O1/O2.
