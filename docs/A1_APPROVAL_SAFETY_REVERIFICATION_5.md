# A1 Approval Safety — Independent Re-verification 5

Verdict: **REJECT — remediation 6 required; production path remains BLOCKED**

Reviewed branch: `feature/phase10-multi-adapter`

Reviewed report HEAD: `2e96f24f96eec30d117fd3d7ec2a16fcf678445a`

Reviewed implementation: `0f95c7c3cdac6dc1524a363027829782dd09579e`

Accepted S1.1 ancestor: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

Local/remote equality, accepted-S1.1 and implementation ancestry, clean worktree
and `git diff --check` passed. The submitted agent/term/transcript race suite
passed independently. The full build gate also passed at the exact report HEAD:
backend build/vet/race, mobile TypeScript and 383 Jest tests, invariants and
secret scan passed; Android/Kotlin remained the documented environmental skip.
R5-D is therefore closed. Green submitted tests do not close the two reproduced
delivery-authority counterexamples below.

## 1. Accepted remediation-5 changes

Preserve these corrections:

- endpoint handles are opaque, drain uses a captured handle, and an ordinary A→B
  replacement retains A's accepted queue;
- queue entries are typed `AcceptedDelivery` values with binding, claim token,
  ReceiptID and defensive payload copies;
- fixed capacity, endpoint, item and byte constants exist;
- entropy failure no longer falls back to a predictable nonce;
- the exact final documentation HEAD passes the real secret gate without an
  exclusion.

The reviewer's previous simple A→B accepted-item-loss test now passes.

## 2. Blocking findings

### R6-A — payload substitution is rejected only after daemon acceptance

`internal/term/approval_delivery.go`, `RuntimeDeliveryGate.Accept`, copies
`req.Payload` into the accepted queue but never checks that
`req.Binding.PayloadDigest == payloadDigest(req.Payload)`. The binding and payload
therefore remain independently caller-supplied at the deepest delivery boundary.

The reviewer reproduced this through the real gate:

```text
binding owns digest("authorized")
request payload = "substituted"
gate.Accept -> ok=true
captured endpoint contains "substituted"
```

The approval store would later reject the mismatched receipt, but that is too
late: the daemon delivery boundary has already accepted bytes that may be drained
externally. “No successful commit” is not equivalent to “no unauthorized
acceptance.” This is the same authority rule frozen in the earlier payload
remediation: substitution must fail before acceptance, receipt and commit.

Minimum correction: validate the complete queue item before append. At minimum,
require non-empty canonical binding identity/claim ownership and exact
domain-separated payload-digest equality. Return non-acceptance and append
nothing on any mismatch. Add a real-gate substituted-payload negative, not only a
store receipt test.

### R6-B — endpoint bounds silently destroy accepted receipt ownership

`evictLocked` enforces `maxGateEndpoints` by deleting the oldest inactive endpoint
or, if all are active, the oldest active endpoint. It does not inspect whether
the endpoint contains items for which successful receipts were already issued,
and it produces no failure outcome for those receipts.

The reviewer reproduced this:

```text
activate first endpoint -> accept item -> successful receipt
create enough additional endpoints to cross maxGateEndpoints
Drain(first handle) -> empty
```

Thus the simple A→B retention fix is undone by bounded churn. The remediation-5
handoff explicitly prohibited eviction that preserves a success receipt while
silently destroying its only accepted item. Calling this “bounded degradation”
does not create an observable terminal disposition.

Minimum correction: never evict a current endpoint or an endpoint containing
undrained accepted items. Evict only safely empty retired endpoints. If no safe
victim exists, fail the new activation before changing current ownership. The
existing endpoint and accepted queues must remain intact. Add all-active,
retired-nonempty, retired-empty and activation-capacity race tests.

### R6-C — production repeatedly replaces the same generation endpoint

`TelemetryService.processSession` calls `deliveryGate.Activate` on every
successful correlated poll. `Activate` always creates a fresh handle and retires
the prior endpoint even when SessionID, RuntimeRef and capacity are identical.

With today's production capacity zero this causes bounded but unnecessary
endpoint/RNG churn. With any future provider channel it would retire the current
delivery handle every poll and accelerate the destructive eviction in R6-B. This
is not a launch/stream replacement because the authoritative RuntimeRef did not
change.

Minimum correction: make exact same-runtime/no-channel activation idempotent, or
change the production caller to activate only when the authoritative endpoint
identity changes. Repeated same-generation polls must preserve the handle and
must not grow endpoint state. A real generation/capacity/channel change must
remain an explicit serialized replacement. Add a production-path repeated-poll
test, not only direct gate tests.

### R6-D — final-tree gate discipline is accepted and must remain

The exact report HEAD passed this review's independent full gate. Preserve the
rephrased documentation and the rule that any report/rebase/tree change requires
the documentation-sensitive checks again. Do not reintroduce scanner exclusions.

### R6-E — mandatory positive provider path remains unavailable

Production capacity remains zero, `provenActionMapping` remains empty, and no
controlled provider delivery channel exists. Keep all production approvals
non-actionable. The typed queue and tests are machinery, not positive production
evidence.

Even after R6-A through R6-C pass, A1 remains BLOCKED until the positive provider
path is separately proven or the acceptance contract is separately reviewed and
changed. N1 remains blocked.

## 3. Required remediation-6 evidence

1. Binding/payload mismatch is rejected before gate append; no item, handle or
   accepted receipt is produced.
2. Empty/malformed required internal binding fields and claim ownership fail
   closed at the delivery boundary.
3. Endpoint bounds never evict current or non-empty accepted endpoints.
4. When no safely empty retired endpoint is available, activation fails without
   retiring/replacing the current endpoint or deleting queued items.
5. Empty retired endpoints are reclaimed deterministically and total bounds hold.
6. Repeated production polls with an identical RuntimeRef preserve one handle and
   bounded state.
7. A genuine RuntimeRef change still retires A, publishes B and preserves already
   accepted A items for captured-handle drain.
8. Deterministic activation/accept/drain/eviction races inspect intermediate state
   and include known-bad controls.
9. The exact final report HEAD passes the full gate without exclusions.
10. Positive provider delivery remains an explicit blocker if unavailable.

## 4. Scope and next action

This remains A1 delivery-boundary remediation only. Do not begin N1, provider
implementation/research, Task/Dispatch, worker acknowledgement/completion,
generic commands, CLI redesign, ConPTY/Windows, cloud relay, lock-screen actions,
O1 or O2.

Continue only from
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_6_HANDOFF.md`.
