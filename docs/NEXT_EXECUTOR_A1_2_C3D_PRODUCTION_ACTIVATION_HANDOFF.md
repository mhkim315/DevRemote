# Next Executor Handoff — A1.2 C3D Production Activation

Status: **C2D ACCEPTED — C3D AUTHORIZED IN THREE REVIEWED CHECKPOINTS**

This is the only active A1.2 executor handoff. Use a fresh execution-agent
session. Do not continue from a scratch or alternate checkout.

## 1. Canonical start state

- Repository: `https://github.com/mhkim315/DevRemote.git`
- Branch: `feature/phase10-multi-adapter`
- Checkout: `/Users/mhk/Documents/codex/DevRemote`
- Accepted C1D: `4794ce7f42380c388c1a5614b4b2518bc1722870`
- Accepted C2D-C: `48ce57d81781646fc1c6c7445b5900b58fd5cef3`
- Accepted catalog P1: `f29e1b6dc776bc8f4799809b59accd7ceb755072`
- Accepted catalog P2A: `f93e81ef126c2f8f6e1013697a66ca363a442cf3`
- Accepted catalog P2B: `d841a88b5f371b41d78e5d2958c029b7114b8d6b`
- Accepted catalog P3/C2D: `91160409c9fc7e41a0c60b1c97a6ec6a7c4cffb4`
- Accepted Codex baseline: `2b940a6fce6e878ffa0da17b5df4d39438af144d`

At startup fetch and fast-forward only. Require local HEAD equal to the remote,
all accepted SHAs to be ancestors, and an empty worktree. Never force-push or
rewrite accepted history. Stop on any mismatch.

## 2. Mandatory reading

Read, in order:

1. `docs/A1_2_C2D_CATALOG_FINAL_ACCEPTANCE.md`;
2. `docs/A1_2_CLAUDE_APPROVAL_EXTENSION_PLAN.md`, especially sections 8-11;
3. `docs/A1_2_C2D_D_PLANNER_CATALOG_AMENDMENT.md`;
4. the accepted P1/P2A/P2B contract notes and final P3 tests;
5. `docs/A1_APPROVAL_SAFETY_PLAN.md` and `docs/SP1_NATIVE_APPROVAL_FINAL_ACCEPTANCE.md`;
6. `companion-daemon/internal/term/managed_claude.go`,
   `companion-daemon/internal/term/claude_catalog_classifier.go`,
   `claude_resume_coordinator.go`, `claude_approval_delivery.go`,
   `approval_store_gen.go` and `approval_execution.go`;
7. `companion-daemon/cmd/devremote/app.go` and
   `companion-daemon/cmd/devremote/claude_delivery_composition_test.go`;
8. mobile `src/lib/approvalRequest.ts`, `src/lib/client.ts`,
   `src/components/ApprovalCard.tsx` and dashboard use.

Preserve C0H/C0R BLOCKED and D1 REJECTED. C3D supports one catalog action, not
arbitrary Claude Bash approval.

## 3. Contract note required before code

Before changing production code, create and commit
`docs/A1_2_C3D_ACTIVATION_CONTRACT_NOTE.md`. It must identify:

- the single production install owner and exact linearization point;
- canonical store ownership and rollback on every failed precondition;
- the complete immutable tuple from catalog classification through Store, claim,
  delivery, consumption receipt and commit;
- current-runtime resolution and invalidation on exit, timeout, stop/delete,
  replacement and daemon restart;
- authenticated requester derivation and mobile idempotency behavior;
- exact live success evidence and all failure outcomes;
- capacity behavior and explicit non-goals;
- one known-bad counterexample for partial install and one for provider-wide
  actionability.

Include a field-by-field binding table. Commit this note separately, push, and
stop for independent review. Do not start C3D-A implementation before approval.

## 4. Non-negotiable activation design

One production-owned transition on `ManagedClaudeService` must, before the first
runtime exists, atomically verify and install:

- the one canonical `AuthoritativeApprovalStore` already configured in `app.go`;
- exact adapter `claude_headless`, version `2.1.209`, launch certification and
  supported platform tuple;
- exact catalog ID `claude.bash.approval_probe.v1` and its compiled action schema;
- actionable ingestion for subsequently created matching runtimes only;
- current `RuntimeOf` resolution;
- `ClaudeManagedApprovalDelivery`.

Any failure leaves observation working but actionability, options, RuntimeOf and
Claude delivery unavailable. Existing records are never upgraded. A second install,
install after runtime creation, store mismatch, unsupported version/platform,
shutdown state or incomplete launch certification fails closed.

Do **not** make Claude actionable by changing the provider-only
`provenActionMapping(provider)` switch. Provider name/version alone is insufficient.
The deepest Store admission boundary must recompute or verify the exact certified
catalog tuple and daemon-generated options/delivery material. It must reject a
forged `Actionable`, catalog ID, option, schema, summary or material even when a
trusted internal caller supplies it.

Extend delivery dispatch and `RuntimeOf` composition without replacing or
weakening accepted Codex routing. Unknown adapters continue to the capacity-zero
gate. Avoid provider-specific branches inside generic claim/receipt authority;
provider dispatch at the composition boundary is acceptable.

## 5. C3D-A — atomic backend composition

Implement only the install transition, exact Store admission validation, combined
current-runtime resolver and multi-provider delivery dispatcher. Wire it in
`cmd/devremote/app.go` as one owned composition step.

Required deterministic production-path tests include:

1. install succeeds before the first certified runtime and all returned components
   belong to the same service/store;
2. install failure mutates nothing and leaves no Claude action path;
3. concurrent install/create has one defined winner and no partially actionable
   record;
4. catalog match becomes actionable with exactly the static summary and the two
   options; non-catalog, malformed, wrong-version/platform and forged records do not;
5. Codex and Claude dispatch route only their exact RuntimeRef; cross-provider and
   stale-runtime substitutions fail before provider write;
6. exit, timeout, stop/delete, replacement and restart invalidate current RuntimeOf
   and leave no restored authority;
7. accepted P1/P2A/P2B/P3 and Codex regression suites remain green under `-race`.

No live model turn and no mobile code in C3D-A. Freeze HEAD, commit, push, and stop
for independent review.

## 6. C3D-B — authenticated API and mobile path

Start only after C3D-A ACCEPT. Prefer no mobile production change: the existing
strict `SafeApproval` decoder, host-bound `resolveApproval`, idempotency key and
`ApprovalCard` should consume provider-independent safe DTOs. Change mobile code
only if a demonstrated contract gap exists.

Prove through the composed production route:

- only paired-device server context with `PermTerminalInput` may claim;
- client-supplied provider/runtime/requester authority is absent or rejected;
- the exact static summary and two options render only for a pending actionable
  catalog record;
- `waiting_approval`, non-catalog Claude observations and stale DTOs create no CTA;
- duplicate tap reuses the exact idempotency binding; changed option/digest conflicts;
- unsupported/stale/expired/delivery-failed outcomes remain non-successful and the
  UI reports them honestly;
- no command, description, path, token, raw hook payload, digest source, claim token
  or delivery bytes cross the public/mobile boundary.

Run focused backend race plus mobile TypeScript/Jest and strict DTO/privacy tests.
Commit, push and stop for independent review. Do not run live proof yet.

## 7. C3D-C — live proof and final A1.2 gate

Start only after C3D-B ACCEPT. Use the exact pinned Claude Code `2.1.209`
executable and recorded artifact identity; do not silently use a global drifted
binary. Use ambient authentication only, an owned temporary workspace and the
harmless catalog probe command. Record bounded/redacted evidence.

Run exactly the minimum positive live evidence:

1. **allow once**: fresh managed session, exact catalog request, authenticated API
   decision, matching resume/consumption witness, one marker creation, bound receipt
   commit and clean exit;
2. **deny**: fresh managed session, exact catalog request, authenticated API
   decision, matching denial witness, zero marker creation, bound receipt commit and
   clean exit.

Also run automated negative coverage for duplicate tap, stale epoch, replacement,
timeout, stop/delete, daemon restart, unsupported version and cross-session/request
substitution. Do not spend live turns re-proving deterministic unit interleavings.

Then freeze the implementation HEAD and run the complete repository gate: backend
build/vet/full race, mobile TypeScript/Jest, Android/native gate when available,
invariants, privacy/secret scan, ancestry, local/remote equality and clean worktree.
Commit the report only after the implementation gate; re-run documentation/diff and
secret checks on the exact final report HEAD. Push that exact tree and stop for
independent final A1.2 review.

## 8. Forbidden scope

- arbitrary Bash, additional catalog entries or allow-always;
- PermissionRequest, permission-prompt-tool, Agent SDK, Channels or PTY key injection;
- public DTO expansion or raw provider text exposure;
- A1 Store/claim/receipt contract weakening;
- Codex protocol changes;
- N1, O1/O2, tmux/cmux cleanup, canonical timeline, Grok or Navigator work;
- daemon-restart restoration of pending approval authority;
- Windows implementation or generic provider SDK extraction.

If the exact production positive path cannot be proven, keep it disabled, report
`A1.2 BLOCKED`, and stop. Never substitute a fixture or controlled P3 path for live
production evidence.
