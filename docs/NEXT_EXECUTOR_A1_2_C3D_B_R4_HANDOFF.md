# Next Executor Handoff — A1.2 C3D-B R4 Final Negative Evidence

Status: **C3D-B-R3 REJECTED — TWO NARROW TEST BLOCKERS ONLY**

Canonical repository: `/Users/mhk/Documents/codex/DevRemote`

Branch: `feature/phase10-multi-adapter`

Rejected implementation HEAD: `df5186a8db20e2c9c253bb19a37350a44ca73d07`

Accepted prerequisite: C3D-A-R2
`bbd9bd4a1b41244f4a3daac3124d13a4f40dae47`

Accepted C3D-B plan: `a3f9cc961cbe86eb628b205d0ecc1d363deea3f0`

Do not start C3D-C. Do not run a live Claude turn. This packet is deterministic
negative evidence only.

## 1. Startup and repository identity

Use only the canonical checkout above. Before editing:

1. `git fetch origin feature/phase10-multi-adapter`
2. fast-forward only; no rebase, force-push or history rewrite;
3. record local HEAD and remote HEAD and require equality;
4. require clean worktree;
5. require ancestry for `df5186a`, `a3f9cc9` and `bbd9bd4`;
6. read this handoff, `docs/A1_2_C3D_B_CONTRACT_NOTE.md`, and the two files
   named in §3.

If any check fails, stop and report BLOCKED. Do not use another scratch checkout.

## 2. Preserve the accepted work

The following are already correct and must not be redesigned:

- composed remote HTTP allow and deny from route through hook/denial witness,
  receipt and Store commit;
- duplicate `already_accepted` and changed-option conflict through the route;
- strict client authority-field rejection;
- exact eight-field DTO, option shape and privacy allowlist;
- production `approvalError.ts` classifier used by `ApprovalCard`;
- production dispatcher accessors remain removed;
- C3D-A authority, launch certification, Store, coordinator, delivery and
  mobile transport contracts.

Expected change scope is test-only. Do not change production backend or mobile
code unless a real production defect is first demonstrated and reported for a
new review.

## 3. Why R3 was rejected

### R4-A — foreign-host and boot evidence was only a comment

Affected file:
`companion-daemon/cmd/devremote/claude_composed_route_test.go`

`TestC3DB_PrincipalNegativeMatrix` executes missing-bearer, member and revoked
cases. It does **not** execute foreign-host or old-boot bearer requests. Lines
after the function merely describe them. Comments are not evidence.

Required production-route proof:

1. Create the target remote-mode App and seed a non-actionable or actionable
   Claude approval record without starting delivery.
2. Mint a bearer from a genuinely different App/host fixture and POST it to
   the target App's actual
   `/api/sessions/{id}/approvals/{approvalId}` route.
3. Require 401/403, unchanged pending record and zero resume/provider delivery.
4. Mint a bearer under an old boot/session-manager incarnation, then send it
   to an otherwise current App route using the new boot incarnation.
5. Require 401/403, unchanged pending record and zero resume/provider delivery.
6. For each negative, include a current-host/current-boot bearer control that
   reaches the handler. Prefer a non-actionable record for the control so it
   returns the expected 409 without entering the 120-second delivery path.

Do not call `HandleApprovalAction` directly. Do not inject a `Principal` into a
request context. Do not claim that another route is structurally equivalent.
The exact approval route must be exercised.

The test must inspect an observable zero-delivery state, for example unchanged
launcher resume count/coordinator entry count plus the pending Store record.

### R4-B — the test named waiting_approval never created that status

Affected file:
`companion-daemon/internal/term/claude_mobile_dto_test.go`

`TestClaudeDTO_WaitingApprovalCreatesNoRecord` drives a non-catalog Claude
PreToolUse/deferred observation and correctly receives one non-actionable
Approval record. It never creates runtime status `waiting_approval`, and its
name/comment incorrectly says the Store remains empty while asserting one
record exists.

Required separation proof:

1. Keep or rename the current test to describe its real purpose:
   `TestClaudeDTO_NonCatalogObservationIsNonActionable` (name may differ).
2. Add a separate test that produces an actual `waiting_approval` runtime
   status through the current production status path without emitting or
   ingesting a provider approval request.
3. Assert the status projection is exactly `waiting_approval`.
4. Assert the canonical `AuthoritativeApprovalStore` has zero records and
   `ListSafe(sessionID)` is empty.
5. Assert a POST/claim using a fabricated ApprovalID cannot resolve and causes
   zero delivery/provider write.
6. Connect the empty approval list to the existing production mobile gate:
   `decodeApprovals`/`actionableApprovals` yields zero CTA. A named accepted
   test may be cited only if it consumes this same produced API/DTO shape.

Do not substitute a hand-built `SafeApproval{actionable:false}` for the runtime
status proof. Do not use terminal text, prompt parsing or screen heuristics to
create an Approval.

## 4. Required pre-implementation note

Before editing tests, append a short section to the commit message or create a
small R4 contract note recording:

- authority owner: paired-device middleware and `AuthoritativeApprovalStore`;
- immutable binding: exact session, approval, runtime generation, requester
  context and action digest;
- success evidence for R4-A: rejection at the composed route before claim;
- success evidence for R4-B: status present, Approval Store empty;
- negative controls described above;
- explicit non-goals: no production change, no C3D-C, no live provider.

Do not begin by changing product code.

## 5. Focused implementation order

Commit one narrow implementation packet after both focused groups pass:

1. Remove the dangling foreign-host/boot comments that pretend to be tests.
2. Add executable foreign-host and old-boot route subtests with positive
   controls and zero-delivery assertions.
3. Rename/correct the non-catalog observation test.
4. Add the real status-only `waiting_approval` separation test.
5. Run each new test independently, then repeat it at least five times under
   `-race` where applicable.
6. Run the focused and final gates in §6 on a frozen HEAD.

No sleep-based success assumption. Use existing fixture completion, session
manager state, channels or bounded observable polling. A comment or a test that
constructs the expected output directly is vacuous.

## 6. Acceptance gate

Focused:

```sh
cd companion-daemon
go test -race ./cmd/devremote -run 'TestC3DB_PrincipalNegativeMatrix' -count=5
go test -race ./internal/term -run 'TestClaudeDTO_(NonCatalog|WaitingApproval)' -count=5
```

Adjust the second regex to the final test names, but show that both tests ran.

Final frozen HEAD:

```sh
cd companion-daemon
go vet ./internal/term ./cmd/devremote
go test -race ./internal/term ./cmd/devremote -count=1
cd ../mobile
npx tsc --noEmit
npx jest --runInBand
cd ..
git diff --check
```

Also run the repository privacy/secret scan used by the build gate. No live
Claude invocation is allowed in R4.

## 7. Stop condition and report

Commit and push a clean fast-forward. Report:

- implementation SHA;
- local/remote equality, clean worktree and prerequisite ancestry;
- exact foreign-host and boot request/control results;
- exact produced `waiting_approval` status and zero Store/CTA evidence;
- focused repetition and final gate results;
- confirmation that production backend/mobile files are unchanged.

Use marker:

`REVIEW REQUEST: A1.2 C3D-B R4 Final Negative Evidence — <implementation SHA>`

Stop for independent review. C3D-C remains prohibited until C3D-B receives an
independent ACCEPT.
