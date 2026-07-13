# S1.1 Runtime Status Hardening — Final Acceptance

Status: **ACCEPT**

Accepted implementation:
`02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

Canonical report HEAD reviewed:
`895a6b3fb0b5ff821b595dad1e0637133a3eb5bf`

Accepted S1 ancestor:
`70ef5df28dede7b0f3025eeaab7826f76a229fbf`

## Accepted contract

S1.1 adds internal runtime-status evidence and identity hardening without changing
the frozen T0 authority model or public `agentActivity` DTO:

- the resolved status is bound to the exact winning accepted event Seq, including
  normalized confidence in winner identification;
- launch generation and stream generation remain separate ordered axes;
- recognized preset launches bind provider/version, exact adapter, daemon-owned
  PID, and spawn start time where available;
- launch replacement invalidates status and ingestion authority before the new
  binding is observable;
- same-session transitions and removal are serialized, publication cannot regress
  generation, and concurrent first registrations cannot both bypass invalidation;
- fixed striped transition gates keep synchronization memory constant-bounded;
- first-registration-only and missing-invalidation paths fail closed on an
  existing binding;
- same-epoch replay/rewind cannot regress or restore a downgraded positive status;
- correlation loss, restart, clear, eviction, and same-ID reuse do not inherit
  stale positive evidence.

`waiting_approval` remains display-only. It is not ApprovalStore authority and
cannot authorize an action.

## Independent verification history

The green initial tree was rejected for exact-winner, adapter-binding, and
replacement-publication defects. Subsequent reviews separately rejected a split
non-atomic replacement, then an unbounded gate map and nil-invalidation bypass.
The accepted implementation closes every recorded blocker while preserving the
accepted R1/R2/R3 behavior.

Evidence documents:

- `docs/S1_1_RUNTIME_STATUS_HARDENING_VERIFICATION.md`;
- `docs/S1_1_RUNTIME_STATUS_HARDENING_REVERIFICATION.md`;
- `docs/S1_1_RUNTIME_STATUS_HARDENING_REVERIFICATION_2.md`;
- `docs/S1_1_RUNTIME_STATUS_HARDENING_IMPLEMENTATION_REPORT.md`.

## Gate evidence

Independent verification on stable report HEAD `895a6b3`:

- focused transcript/term `go test -race`: PASS;
- backend build, vet, and full race suite: PASS;
- mobile TypeScript and Jest: PASS;
- Android Kotlin compile: PASS;
- invariant, ID-inference, secret, and diff checks: PASS;
- `scripts/build-gate.sh`: **ALL GATES PASSED**.

## Explicit limitation carried forward

The ordinary local `pokit run claude/codex` command remains a legacy `bash -c`
managed-lifecycle path and does not receive recognized managed-agent authority.
The accepted recognized identity path is the preset profile path. No current
runtime is `orchestration-certified`; O1 must establish typed dispatch,
acknowledgement, completion, and dynamic eligibility before that term is used.

## Next boundary

Proceed to A1 Approval Safety through
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_HANDOFF.md`. A1 must not use S1/S1.1 status
as authorization input.
