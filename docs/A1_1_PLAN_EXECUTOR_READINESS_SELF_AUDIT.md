# A1.1 Codex Provider-Positive Plan — Executor Readiness Self-Audit

Author role: **execution agent (Claude Code)** — this is an EXECUTOR-SIDE readiness
self-audit, **NOT** an independent plan verdict. It does not record ACCEPT / ACCEPT
WITH REQUIRED CHANGES / REJECT. Per `NEXT_SESSION_A1_1_CODEX_PROVIDER_POSITIVE_HANDOFF.md`
§4/§13 the plan ACCEPT must come from an independent reviewer in a separate doc commit,
and CP0 must not begin until then. No CP0 work has started; production capacity remains
zero.

Purpose: map the plan against `EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md` and the frozen
A1 boundary, confirm the plan's code claims against the actual tree, and surface
gaps/risks the independent reviewer should weigh — so the plan can reach ACCEPT without
a round-trip and so CP0 does not start on an unstated assumption.

## 0. Repository grounding (verified this turn)

```text
root:   /Users/mhk/Documents/codex/DevRemote
branch: feature/phase10-multi-adapter
HEAD == origin == bacb66cb68609d11105908904d4c254f47266718
R11 accepted report HEAD 997a697b is an ancestor of HEAD; worktree clean.
```

Every frozen symbol the plan §3 relies on exists as described, and the current wiring
matches the plan's claims:

| Plan claim | Verified location |
| --- | --- |
| `ApprovalDelivery` is the delivery abstraction | `approval_delivery.go:96` |
| unavailable fallback exists | `NewUnavailableApprovalDelivery` `approval_delivery.go:103` |
| production wires a capacity-0 gated delivery | `app.go:229` `NewGatedApprovalDelivery(deliveryGate)`; handler uses `h.ApprovalDelivery` at `approval_handler.go:114-118` |
| `CanonicalAction.Digest()` is the selected-action digest | `approval_execution.go:123` |
| store owns claim/commit | `AuthoritativeApprovalStore` `approval_store_gen.go:119`; `ClaimForExecution` `:395`; `RecordDelivery` `:528` |
| provider mapping is empty | `provenActionMapping` returns `(nil,false)` `approval_ingest.go:27` |
| existing `codex` preset resolves by PATH (weak identity) | `profiles.go:56-57` `exec.LookPath("codex")`; launch binding `create.go:97-99` |
| no pre-existing app-server/JSON-RPC/stdio infra | grep found none in `internal/`,`cmd/` (CP1 builds it new) |

The roadmap invariant "Recorder is the single PTY reader" (`ROADMAP_AFTER_E10B.md`) is
directly protected by plan §9 (app-server stdio must not route through Recorder/PTY).

## 1. Protocol §14 handoff-author checklist coverage

| Required item | Plan/handoff location | Status |
| --- | --- | --- |
| risk class + required pre-implementation contract note | handoff §6, plan §10 (per-packet note + binding table) | present |
| complete binding fields (not just object names) | plan §5.3 native key + §6 fingerprint fields | present at field level; the FILLED binding table is deferred to each CP contract note (plan §10) |
| authority owner + deepest callable enforcement boundary | plan §3, handoff §5 | present |
| forbidden caller-supplied authority fields | plan §6 (payload server-generated, never from mobile), §3 (provider cannot mint authority) | present |
| explicit capacity + restart semantics | plan §8 | present |
| failing race interleaving + deterministic test seams | plan §7/§8, handoff §6 | present at policy level; concrete interleavings enumerated for CP3 (reservation race, resolved-before-write) |
| production-positive evidence for acceptance | plan §11.1 (real E2E; fixtures/mocks explicitly insufficient) | present |
| honest blocked outcome | plan §9/§10/§14 (CP0 stop-BLOCKED) | present |
| pre-commit audit, stable-HEAD gate, stop condition | handoff §12/§13 | present |

Conclusion: the plan satisfies the §14 checklist at planning altitude. The only
deferral is the filled binding table (correctly pushed to per-packet contract notes).

## 2. Frozen-boundary preservation

The plan keeps the R11-frozen core authoritative and confines provider code to a thin
certified boundary (plan §3, handoff §5). Design choices that preserve the frozen
public/core contract:

- provider request identity is folded into the existing `ApprovalID` as a
  domain-separated digest (plan §5.3), so no provider field is added to
  `ApprovalExecutionBinding` or the public DTO;
- the server-generated JSON-RPC response payload is never accepted from the mobile
  client and never enters a DTO (plan §6);
- `RecordDelivery` remains the sole commit boundary; a queue admission or process write
  is explicitly not acceptance (plan §3).

No redesign of the accepted core is proposed. This is consistent with the exec/review
discipline that produced the R9→R11 acceptances.

## 3. Risks and open questions for the independent reviewer

These are not objections; they are the points most likely to decide ACCEPT vs ACCEPT
WITH REQUIRED CHANGES, and the ones CP0 must not paper over.

1. **Production entry path is the central unproven risk (plan §9, handoff §8).** The
   plan's blocking question — can a real user start/resume a thread and start a turn
   through a *narrow* authenticated production path without building a terminal renderer
   or a generic Task/Dispatch API — is answered only by the CP0 spike. If no defensible
   bounded user entry exists in `0.144.1` app-server, A1.1 stalls at CP0 BLOCKED. The
   reviewer should judge whether the plan's "narrowest authenticated provider-specific
   production entry" is concretely achievable or still hypothetical.
2. **`approvalId == null` initial scope (plan §5.1).** The initial scope includes only
   `commandExecution/requestApproval` with `approvalId == null` and excludes
   zsh-exec-bridge subcommand callbacks. CP0 must confirm the normal `0.144.1` command
   approval actually presents `approvalId == null`; otherwise the initial scope matches
   nothing.
3. **Certification must not reuse the PATH-lookup preset.** The plan §4 forbids
   PATH-lookup identity, but the existing `codex` preset (`profiles.go:57`) uses
   `exec.LookPath`. CP1 must spawn a digest-pinned executable via a *new*
   `codex_app_server` profile, not the existing PATH-resolving preset.
4. **Schema-identity mechanism (plan §4).** "Pinned generated app-server schema digest"
   should be made concrete at CP0: how the daemon obtains/generates the schema and what
   exactly the pinned digest covers, so schema validation at runtime is well-defined.
5. **`serverRequest/resolved` consumption proof (plan §7).** The entire success semantics
   hinge on resolved-after-our-write being distinguishable from provider-side
   cancellation/resolution. CP0's trace matrix (§11.2: resolved-before-write vs after)
   must prove this; if indistinguishable, A1.1 is BLOCKED. This is correctly gated but is
   the single highest-value CP0 trace.
6. **Delivery wiring at CP3/CP5.** Production currently wires one
   `NewGatedApprovalDelivery` (capacity 0). The reviewer should confirm how the Codex
   bridge `ApprovalDelivery` composes with the default (replace vs. per-runtime dispatch)
   so non-certified runtimes stay capacity-0 while the certified tuple activates at CP5.
7. **Concurrency test parity.** CP3's deterministic reservation/resolution interleavings
   should mirror the accepted R9-B3 pattern (nil-in-production hook + contested-state
   inspection + known-bad control), not sleeps.

## 4. Strengths worth preserving

- Evidence rigor: §11.1 forbids fixtures/mocks/injected requests as positive-path proof;
  §11.2 lists a broad negative/race matrix; §11.3 mandates the full gate on a frozen
  HEAD — matching the discipline that got A1 core accepted.
- Honest BLOCKED outcomes are wired at CP0 and §14, not deferred.
- Capacity stays zero through CP4; activation is a separate focused CP5 change confined
  to an exact certified tuple, not `provider == codex`.
- A1.2 Claude is explicitly optional and not an automatic N1 prerequisite (plan §12,
  handoff §11).

## 5. Executor stance

- I have not started CP0 and will not until an independent plan ACCEPT is recorded in a
  separate doc commit.
- No production code changed in this turn; production remains non-actionable
  (`provenActionMapping` empty, delivery capacity zero).
- When CP0 is authorized, the first artifact will be the per-packet contract note +
  filled binding table required by protocol §5 and handoff §6, followed by the redacted
  controlled-trace evidence.
