# A1 Approval Safety — Independent Re-verification 3

Verdict: **REJECT — remediation 4 required; production path remains BLOCKED**

Reviewed branch: `feature/phase10-multi-adapter`

Reviewed report HEAD: `c34d8ce4c57ae13e952e96c8550fcea333698e38`

Reviewed implementation: `0ac1acf38`

Accepted S1.1 ancestor: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

The remote report tree was reviewed in an isolated worktree. Local/remote
equality, accepted-S1.1 ancestry, clean worktree and `git diff --check` passed.
The submitted agent/term/transcript race tests passed after rerunning the
localhost-dependent term suite outside the filesystem/network sandbox. The full
build gate passed with backend build/vet/race, mobile TypeScript and 383 Jest
tests, invariants and secret scan; Android/Kotlin remained the documented
environmental skip. Green submitted tests do not close the reproduced authority
counterexamples below.

## 1. Accepted remediation-3 changes

Preserve these corrections:

- idempotent replay now follows stored permission, runtime and supersession
  checks and compares a copied/digested requester authorization context;
- the store computes canonical action and payload digests, the handler delivers
  the store payload, and substituted payload receipts cannot commit;
- retry is bounded to two manual retries with the original binding and only for
  outcomes classified as definite non-acceptance;
- log output is passed through the repository diagnostic redactor;
- production remains non-actionable: `provenActionMapping` is empty and no
  provider delivery sink is registered.

## 2. Blocking findings

### R4-A — incomplete requester context can acquire execution authority

`internal/term/approval_execution.go`, `RequesterContext.present`, requires only
`DeviceID` and `BearerSessionID`. It does not require the HostID or BootID that
the frozen A1 plan and remediation-3 contract table call part of the exact
server-derived requester binding. `ClaimForExecution` consequently grants a
fresh claim when either field is empty, provided the caller supplies the stored
permission.

The reviewer reproduced both cases through the store's real
`ClaimForExecution` boundary. Each failed with `Outcome: granted`:

- DeviceID + bearer + boot + permission, but empty HostID;
- DeviceID + host + bearer + permission, but empty BootID.

This is not only a handler concern. The store is declared the sole and deepest
execution-authority boundary, so it must reject an incomplete authenticated
context rather than depend on the current handler to populate it.

Minimum correction: make the exact mandatory requester/auth fields explicit and
validate all of them inside `ClaimForExecution` before initial claim, retry or
`already_accepted`. Add direct-store negatives for every empty field as well as
the existing changed-field tests. Do not introduce a client-controlled fallback.

### R4-B — registry disappearance leaves the delivery endpoint active

`internal/term/telemetry_service.go`, `reconcileSessions`, is the production owner
of registry-disappearance cleanup. It deletes telemetry state and clears the
status and approval stores, but never calls `deliveryGate.Deactivate(id)`.

The reviewer reproduced the actual production cleanup path by activating a
generation-owned endpoint, placing that session in `TelemetryService.sessions`,
calling `reconcileSessions(nil)`, and then calling the gate with the disappeared
session and old RuntimeRef. The old endpoint still accepted the payload.

This directly contradicts the remediation report's statement that termination
and registry disappearance are wired. `Clear` covers explicit delete/link/unlink,
and launch/stream/correlation paths contain deactivation calls, but registry
disappearance is a separate production path.

Minimum correction: deactivate the endpoint in the same disappearance cleanup
operation and add a production-path regression that observes the contested gate,
not only store cleanup. Verify explicit delete, unlink, correlation loss,
launch/stream replacement and registry disappearance independently.

### R4-C — delivery linearization holds a lock across an unconstrained interface

`internal/term/approval_delivery.go`, `RuntimeDeliveryGate.Accept`, holds
`RuntimeDeliveryGate.mu` while invoking `DeliverySink.Enqueue`. `DeliverySink` is
an arbitrary interface; a comment saying it must be fast and perform no external
I/O is not an enforceable boundary. The remediation contract note simultaneously
claims “no lock across external I/O”, while the deterministic test deliberately
blocks inside `Enqueue` and proves replacement is blocked on that mutex.

The intended generation linearization is valid, but the abstraction does not
enforce the required bounded in-memory acceptance operation. A future sink can
block lifecycle/replacement or perform provider I/O under the gate lock.

Minimum correction: make the under-lock operation a production-owned bounded,
non-blocking in-memory queue/reservation, not an arbitrary callback. External
drain/provider I/O must occur after acceptance against the captured immutable
generation endpoint. Capacity exhaustion must fail closed. Deterministic tests
must prove enqueue-versus-replacement, queue-full behavior and that external
drain cannot hold the transition gate.

### R4-D — the secret gate was weakened to suppress a heading false positive

`scripts/build-gate.sh` adds a `grep -v` exclusion keyed to the old section-five
heading. This excludes any secret-scan finding on a line containing that phrase.
The sole current match was the heading in
`docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`; the match is a short token-like
substring spanning an ordinary word boundary, not a secret.

Do not add content-based bypasses to a security gate for this. Remove the
exclusion and eliminate the false positive without suppressing an arbitrary
line—for example by harmlessly rewording the heading or tightening the scanner
with separately reviewed negative controls. Prove a secret-shaped value remains
detected even near documentation headings.

### R4-E — mandatory positive provider path remains unavailable

The implementation honestly preserves the blocker: no controlled provider
evidence-to-action mapping or production delivery channel exists. The production
gate is always activated with a nil sink, so all approvals remain non-actionable
and no bytes are accepted. Controlled fixture sinks prove machinery only.

Keep this fail-closed state. Do not synthesize Y/N, infer options from prompts or
PTY output, or claim A1 completion from fixture delivery. After R4-A through
R4-D are independently accepted, A1 still remains BLOCKED until mandatory
positive provider-path evidence exists or the acceptance contract is separately
reviewed and changed.

## 3. Required remediation-4 evidence

1. Empty DeviceID, HostID, BearerSessionID, BootID or required permission cannot
   acquire a claim, retry or idempotent success at the store boundary.
2. Existing changed requester, permission and stale-runtime replay negatives stay
   green.
3. Registry disappearance deactivates the exact delivery endpoint; old RuntimeRef
   accepts zero bytes afterward.
4. Delete, unlink, correlation loss, launch replacement and stream replacement
   each have production-owner cleanup evidence.
5. Acceptance is a bounded non-blocking in-memory operation owned by production;
   queue full fails closed and provider I/O runs outside the transition lock.
6. Deterministic contested-state tests remain non-vacuous and include a bypass
   negative control.
7. The heading-content secret-scan bypass is removed without weakening secret
   detection.
8. Accepted R3-B, R3-D and R3-E behavior and all full gates remain green.
9. Positive provider delivery remains an explicit release blocker if unavailable.

## 4. Scope and next action

This is A1-only remediation. Do not begin N1, Task/Dispatch, worker
acknowledgement/completion, automatic policy, generic command execution, CLI
redesign, ConPTY/Windows, cloud relay, lock-screen actions, O1 or O2.

Continue only from
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_4_HANDOFF.md`. N1 remains
blocked.
