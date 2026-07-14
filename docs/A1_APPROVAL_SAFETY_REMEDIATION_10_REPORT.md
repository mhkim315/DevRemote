# A1 Approval Safety — Remediation 10 Report

Status: **R10 complete and gated; A1 / provider path / N1 remain BLOCKED**

Remediation 10 closes the six blockers from the R9 re-verification. All work is in
`companion-daemon/internal/term`; no provider-positive path, N1, O1, or O2 work was done.

## 0. Repository state

```text
repository:  https://github.com/mhkim315/DevRemote.git   root: /Users/mhk/Documents/codex/DevRemote
branch:      feature/phase10-multi-adapter
R9 baseline (rejected):  23f09e149944ab927b5f8e4a59416b9e4c1555fb
R10 implementation:      2e70512345718bef47e83a08b1ac8af5a6294aaf
accepted S1.1 ancestor:  02c8385e3270fbbc4df45e0c71ccad6ebe11a076  (ancestor ✓)
```

Onboarded (fetch, HEAD==origin, ancestry, clean) before editing; fast-forward only.

## 1. Blocker resolutions

### Blocker 1 — string backing-storage bound bypass (RESOLVED)

Go strings share backing arrays, so a short but valid substring of a multi-MB caller
string would pin the whole array through the queued item and the endpoint while
`len()`-based accounting charged only a few bytes. `Accept` now deep-clones every
retained binding string and the ClaimToken (`cloneBinding` → `strings.Clone`), and
`Activate` clones the endpoint identity — the current-map KEY, `genEndpoint.sessionID`,
and `RuntimeRef.Adapter/Version` (`cloneRuntimeRef`). Retained strings therefore own
bounded, exact-length allocations independent of the caller's backing array.

Proof: `TestDeliveryGate_RetainedStringsAreDeepCloned` submits identity/token fields
that are short substrings of distinct ~1 MiB arrays, then asserts (via
`unsafe.StringData`) that every retained endpoint and queued-item string has a
DIFFERENT backing pointer than the caller's, while content is preserved exactly.

### Blocker 2 — fixed charge not conservative (RESOLVED)

`gateItemFixedCharge` was 64 while the retained `AcceptedDelivery` fixed overhead alone
exceeds that. It is now 320, with a documented worst-case breakdown: 9 string headers
(144) + payload slice header (24) + RuntimeRef ints (16) + ReceiptID content (≤53) =
237, rounded up for alignment/slice/map bookkeeping. Because Accept deep-clones the
strings (blocker 1), the retained heap for content equals its counted `len`, so
`chargedItemBytes` = exact variable content + a conservative fixed upper bound is now a
genuine conservative heap bound, not merely a logical charge. No estimate is called
"exact".

### Blocker 3 — version traversal + SessionID UTF-8 (RESOLVED)

`validVersion` now rejects any `".."` run (so `"1..2"` — alphanumeric-first but
traversal-shaped — fails), in addition to the existing grammar rejecting
slash/backslash/control/whitespace. `validSessionID` now requires `utf8.ValidString`
before parse/compare, since `SessionRef.Validate` permits Unicode in the local id but
does not reject invalid UTF-8. Covered by
`TestDeliveryGate_AcceptRevalidatesIdentityGrammar` (version-traversal, version-slash,
version-backslash) and version-grammar bound cases.

### Blocker 4 — shallow snapshotGate (RESOLVED)

`gateSnap` is now a DEEP snapshot: the full `current` map (sid→id), the `order` slice
(handle sequence), every endpoint's id/session/active/runtime(adapter,version,
launchGen,streamGen)/capacity/queue-length/queued-bytes/seq/nonce, and the global byte
total. `assertUnchanged` uses `reflect.DeepEqual`, so a destructive delete+replace that
preserves counts (including all-zero capacity-0 endpoints) is now detected. Every R9-A
rejection test uses this deep comparison.

### Blocker 5 — production replacement evidence missed leaks (RESOLVED)

`TestDeliveryGate_ProductionTelemetryPathActivation` now captures the endpoint count
across the launch-generation change and asserts net-zero (no endpoint/order leak): the
old handle is reclaimed (`endpoints[h1]==nil`), `len(order)==count`. On correlation
loss it now verifies the old endpoint is inactive AND that a fully-canonical `Accept`
for the session fails (acceptance genuinely disabled, not merely unmapped). Activation
evidence still comes only from the `processSession` path; the Accept-failure check is a
deactivation assertion.

### Blocker 6 — contested state missing seq/count/order (RESOLVED)

`TestDeliveryGate_DeterministicAcceptVsReplacementInterleaving` now also asserts, at the
paused contested point, the endpoint B `seq==0`, the endpoint count (`==1`, A reclaimed),
and the `order` slice (`==[hB]`), alongside the prior current-mapping/ownership/queue/
byte checks.

## 2. Provider blocker (unchanged)

`provenActionMapping` returns `(nil, false)`; production activates every endpoint with
capacity 0; no heuristic CTA/claim/delivery. A1 and N1 remain BLOCKED. No
provider-positive work performed.

## 3. Gate result (full, on frozen HEAD 2e70512)

`scripts/build-gate.sh` → **ALL GATES PASSED**: backend build/vet/`-race`/diff-check;
mobile typecheck/test/kotlin; vendor-branch + ID-inference invariants; secret scan.
The R10 diff introduces no secret. Interleaving test stable at 30×. Full-gate proof per
`EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md` §11; Android reported PASS.

## 4. Stop condition

Stopping for independent verification. No provider-positive work, N1, O1, or O2 begun.

REVIEW REQUEST: A1 Approval Safety remediation 10 — 2e70512345718bef47e83a08b1ac8af5a6294aaf
