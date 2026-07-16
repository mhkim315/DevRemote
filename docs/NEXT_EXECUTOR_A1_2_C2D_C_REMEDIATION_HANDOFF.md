# A1.2 C2D-C Remediation Handoff

Status: **C2D-C REJECTED — remediation required**  
Reviewed implementation: `cf877aaa64460b1e996d8756464ffb1ce41eb36a`  
Accepted prerequisite: C2D-B `27bd45162feb6bdf6fec52fa44192208c8276e32`  
Canonical repository: `/Users/mhk/Documents/codex/DevRemote`  
Branch: `feature/phase10-multi-adapter`

> **Superseding execution packet:** the first remediation implementation at
> `08be1ad3711cb0549e157413ea8ba4cef5798834` was also rejected. Continue from
> `NEXT_EXECUTOR_A1_2_C2D_C_REMEDIATION_2_HANDOFF.md`, which corrects the missing
> resume/hook ownership model, exact response-byte binding, terminal cleanup,
> and production-shaped composition proof.

This packet is intentionally narrower than C2D-D. Repair the uninstalled Claude
delivery boundary and its controlled composition proof only. Do not start safe
review projection, production activation, mobile work, C3D, N1, O1/O2, observer
cleanup, or another provider.

## 1. Rejection summary

The focused race tests pass, but the current tests prove an internal simulation,
not exact Claude decision delivery.

The blocking counterexample is:

1. `ClaudeManagedApprovalDelivery.Deliver` calls `ClaimWrite`.
2. It discards the returned `WriteHandle` and therefore never uses the stored
   provider decision.
3. No hook HTTP response, resumed Claude process, or other provider boundary is
   written.
4. It nevertheless calls `ConfirmWrite(claimToken, true)`.
5. A synthetic `WitnessProvider` returns a matching struct.
6. The method returns `DeliveryAccepted`.

This violates the frozen C2D requirement that the exact private decision be
delivered once and that a provider-native consumption witness follow that write.
The implementation locations are
`companion-daemon/internal/term/claude_approval_delivery.go:112`, `:126`, and
`:137`. The fixture-only success path is at
`companion-daemon/internal/term/claude_approval_delivery_test.go:100`.

The current code must remain **uninstalled**. Claude production actionability,
the production delivery capacity, and C3D authorization remain zero/off.

## 2. Contract note required before code changes

Write a short amendment to the existing C2D contract before implementation. It
must identify:

- authority owner: the C2D-B coordinator plus the concrete managed Claude resume
  boundary;
- immutable binding: A1 `ApprovalExecutionBinding`, private Claude identity,
  resume nonce, exact decision, expected witness kind, runtime epoch and receipt
  ID;
- states: reserved -> resume started -> repeated PreToolUse matched -> write
  claimed -> exact response accepted by the hook boundary -> decision written ->
  exact witness -> receipt;
- linearization point for reservation, claim, invalidation, and witness routing;
- external-I/O boundary, with no coordinator or service lock held across spawn,
  hook response I/O, or stream reading;
- cleanup and restart behavior for every nonterminal state;
- capacity behavior and entropy failure before provider delivery;
- one known-bad counterexample for fake confirmation, substituted response,
  early witness, stale runtime, and orphaned reservation;
- explicit non-goals listed in section 1.

Include a binding table showing where the decision bytes, expected witness kind,
private invocation identity, receipt ID, and runtime generation are created,
stored, compared, invalidated, and tested.

Do not start implementation until this amendment exists in the worktree.

## 3. Remediation packet C-R1 — real decision delivery boundary

Replace the simulated confirmation path with the smallest concrete provider
boundary supported by the accepted C0D/C2D-B design.

Required properties:

1. Obtain and use the `WriteHandle` returned by `ClaimWrite`. The response is
   derived from its stored decision; it is not reconstructed from caller payload
   text.
2. Strictly encode the frozen Claude hook response for only:
   `allow_once -> allow` and `deny -> deny`. Reject unknown fields, schema,
   option/decision mismatch, malformed UTF-8, and substituted payload before any
   provider write.
3. Route the response through a concrete resume/hook boundary owned by the
   managed Claude runtime. A generic callback that simply returns success is not
   delivery authority.
4. Call `ConfirmWrite(..., true)` only after that concrete boundary accepted the
   exact response bytes. On a short write or error, confirm failure and return a
   non-success result.
5. Bind the expected witness kind before I/O:
   - allow: authenticated `PostToolUse` for the exact invocation;
   - deny: bounded `permission_denials` projection for the exact invocation and
     input digest.
   Passing `WitnessKind(0)` is prohibited.
6. Witnesses must enter through production-shaped routes: authenticated hook
   routing for allow and the bounded resumed-process projection for deny. A
   test may stub the external Claude process, but it may not fabricate the
   witness after bypassing those routes.
7. Preallocate the bounded opaque receipt ID before the first provider write.
   Entropy failure must stop before Claude can consume a decision.
8. Keep the boundary uninstalled in `app.go`.

The exact type names may differ. Do not introduce a provider SDK or a second
provider-neutral approval core. Extend the existing managed Claude runtime,
hook bridge, and C2D-B coordinator only as needed to make this path real.

Checkpoint after C-R1 focused tests. Do not request independent acceptance yet;
continue through C-R3 unless a production capability is genuinely unavailable.

## 4. Remediation packet C-R2 — terminal cleanup and fail-closed lifecycle

Every reservation must have one bounded terminal disposition.

Implement a single ownership/finalization scheme that covers:

- identity lookup failure after reservation;
- write-claim rejection;
- response write failure or ambiguity;
- witness timeout, malformed witness, wrong witness kind, and cross-session
  witness;
- stop, kill, delete, runtime epoch replacement, process exit, and coordinator
  close;
- receipt entropy failure before delivery;
- daemon restart restoring no reservation or actionable authority.

Rules:

- pre-write failures cancel the entry and remove the claim-owned identity;
- after a possibly accepted provider write, never auto-retransmit; return an
  honest ambiguous/conflict outcome and retain only the minimum bounded
  tombstone needed to reject replay;
- timed-out or rejected entries cannot consume live capacity indefinitely;
- cleanup cannot remove a newer runtime's identity or entry;
- nil service, nil coordinator, nil transport/witness route, zero capacity, and
  invalid timeout fail closed without panic;
- do not hold internal locks across external I/O.

Add direct state inspection for capacity before and after every failure class.
Use deterministic channels/barriers, not sleeps.

## 5. Remediation packet C-R3 — non-vacuous A1 composition proof

Replace the current fixture-only success setup. In particular, tests must not
manually construct `ApprovalExecutionBinding`, set `PayloadDigest`, invent a
claim token, then inject a witness. That bypasses the A1 authority boundary.

The controlled composition test must execute the real chain:

1. controlled accepted Claude observation with exact retained private identity;
2. `AuthoritativeApprovalStore` admission with frozen options and delivery
   material;
3. authenticated `ClaimForExecution`, allowing the Store to derive the binding,
   digest, payload, requester context, and claim token;
4. the uninstalled `ClaudeManagedApprovalDelivery`;
5. concrete external-provider test double at the final process/hook I/O boundary;
6. exact production-shaped PostToolUse or permission-denials routing;
7. `RecordDelivery` with the returned receipt;
8. final Store state committed only after the exact witness.

Required positive tests:

- allow_once: exact response write, matching PostToolUse, bound receipt, Store
  commit;
- deny: exact response write, matching permission denial plus input digest,
  bound receipt, Store commit.

Required negative and concurrency tests:

- known-bad current behavior (`ConfirmWrite(true)` without an actual response
  write) is rejected;
- option/payload/schema substitution before write;
- witness before response acceptance;
- wrong witness kind, session, tool ID, tool name, input digest, or runtime;
- result before reservation, during write, after write, duplicate and late
  result;
- duplicate claim, allow-versus-deny, cross-session substitution;
- stop/delete/exit/epoch replacement at reservation, write-claim, write, and
  witness barriers;
- provider write failure, timeout, capacity exhaustion, entropy failure, and nil
  dependencies;
- no success receipt or Store commit on any non-success path.

Concurrency tests must use named barriers or channels and assert the contested
intermediate state. Remove `time.Sleep` synchronization. Include a fault-injected
known-bad control so a later successful operation cannot mask the regression.

## 6. Acceptance and gate discipline

Before the implementation commit, re-read:

1. `docs/A1_2_C2D_PACKET_CONTRACT_NOTE.md`, especially sections 2.3, 3, 5,
   6, and 10.3;
2. `docs/NEXT_EXECUTOR_A1_2_CLAUDE_APPROVAL_HANDOFF.md`, section 6;
3. this remediation handoff.

Then produce an invariant-by-invariant self-audit mapping each item above to
exact production code and a non-vacuous test. Explicitly distinguish concrete
production-shaped routing from stubs and unavailable paths.

Run proportionate gates on a frozen implementation HEAD:

```sh
cd companion-daemon
go test -race ./internal/term -run 'Claude.*(Delivery|Resume|Hook|Coordinator)' -count=5
go test -race ./cmd/devremote -run 'Claude.*(Delivery|Composition)' -count=1
go test -race ./internal/term ./cmd/devremote -count=1
go build ./...
go vet ./...
git diff --check
```

If the `cmd/devremote` composition test does not yet exist, that is a remediation
deliverable, not a skippable gate. Do not run live model turns for this packet.

Freeze HEAD before the authoritative final gate. If a report commit changes the
tree, rerun the required final gate on that exact report HEAD.

Push only after all packets and the self-audit are complete. Stop with:

```text
REVIEW REQUEST: A1.2 C2D-C Remediation — <implementation SHA>
```

Do not start C2D-D or C3D. If exact response delivery or production-shaped
witness routing cannot be implemented from the accepted Claude surfaces, keep
the boundary uninstalled, report **C2D-C BLOCKED**, and stop without weakening
the contract.
