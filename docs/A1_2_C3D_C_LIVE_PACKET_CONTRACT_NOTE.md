# A1.2 C3D-C — Live Proof Packet Contract Note

Status: **PRE-IMPLEMENTATION — C3D-B ACCEPTED AT `3ade9e3c…` — LIVE PACKET AUTHORIZED**

Parent authority: `docs/A1_2_C3D_ACTIVATION_CONTRACT_NOTE.md` §8 (accepted R1
`98ca6d9`), executed per
`docs/NEXT_EXECUTOR_A1_2_C3D_PRODUCTION_ACTIVATION_HANDOFF.md` §7.
Prerequisites: C3D-A-R2 `bbd9bd4`, C3D-B `3ade9e3` (independent ACCEPT).

## 1. The one production change in this packet

`claudeCertificationPrompt` (`internal/term/managed_claude.go:30`) changes

    from: "Use your Bash tool to run exactly this command: echo c1d-probe-ok"
    to:   "Use your Bash tool to run exactly this command: echo pokitclaudeapprovalprobe"

Why this is required and in-scope:

- Accepted §8 live evidence requires: "fresh `CreateDetached`; the REAL
  initial process emits `PreToolUse` for the exact probe; classifier match;
  deferred join; pending actionable record". `CreateDetached` has exactly one
  model turn (`-p <prompt>`; there is deliberately NO Claude prompt API), so
  the launch prompt is the only production path that can elicit the frozen
  catalog command `echo pokitclaudeapprovalprobe`
  (`claude.bash.approval_probe.v1`).
- The C1D constant predates the C2D-D catalog freeze; `echo c1d-probe-ok` is
  a non-catalog command and can only ever produce a NON-actionable
  observation. Aligning the certification prompt with the one frozen catalog
  entry completes the accepted design; it adds NO catalog entry, NO arbitrary
  Bash, NO new tool, and NO policy change. The prompt-shape
  ("Use your Bash tool to run exactly this command: …") is the exact shape
  already proven to elicit the exact command on pinned 2.1.209 by the
  accepted C1D live proof and the accepted R6-A7 deny probes.
- The model remains free to refuse or deviate; a deviated command fails the
  classifier and stays non-actionable — visible in evidence, never coerced.

Consequential test updates (test-only): `cmd/devremote/c1d_live_test.go`
markers (`cmd`/`payload` marker strings) track the constant;
`internal/term/managed_claude_test.go:551` already asserts via the constant.
No other production backend or mobile change is expected; any further gap
stops this packet for review, per handoff §7 final paragraph.

## 2. Live harness design (new `cmd/devremote/c3dc_live_test.go`)

Gate: `POKIT_CLAUDE_DIGEST` must equal the recorded pinned artifact identity
(realpath `~/.local/share/claude/versions/2.1.209`, SHA-256
`59d2de7f49db2f75d5c33bbb46a6b8f288ad24d40b61e30602a502bb7ddc380c`) — the
same fail-closed convention as the accepted C1D live proof. Skipped in every
deterministic gate run; never uses a drifted global binary
(global `claude` is currently 2.1.210 and is NOT used).

Composition — the full remote-mode production App:

- `NewAppWithDeps` with `EnableManagedClaude`, `deps.ManagedClaude` built
  from `PinnedClaudeConfigWithDigest` (`Bin = PinnedPath`), the REAL
  production attestor (`NewClaudeAttestor`), ambient user authentication,
  owned temporary workspace as cwd;
- `InstallApprovalExecution` runs at App construction (production wiring) —
  the live sessions are actionable-capable from birth;
- device pairing + challenge/verify handshake mint the owner bearer; the
  claim is POSTed to the registered
  `/api/sessions/{id}/approvals/{approvalId}` route — the same composed
  route accepted in C3D-B. No direct handler call, no injected principal.

Stream observation seam: a test launcher that replicates the production
spawn byte-for-byte (`exec.Command` + `cmd.Dir` + `Setpgid` + stdin/stdout
pipes, as in `execLauncherWithDir.LaunchInDir`) and tees the child's stdout
into a bounded in-memory buffer before the production pump consumes it. The
tee observes; it never injects, filters, or reorders. Witness AUTHORITY
(PreToolUse/PostToolUse hooks, `permission_denials`, `MarkWitnessed`,
receipt, `RecordDelivery`) is untouched production code. The Claude pump
retains no raw stream anywhere (the managed event ring is Codex-only and
payload-free), so the tee is the minimal capture that §8's
"bounded/redacted stream captures" requires.

## 3. Live evidence per accepted §8 (authority vs corroboration disjoint)

1. **allow once** — fresh `CreateDetached`; bounded poll for exactly ONE
   pending ACTIONABLE record with static summary
   "Run Claude approval verification probe" and exactly the two options;
   authenticated composed-route claim `allow_once`; one resume spawn;
   AUTHORITY = matching `PostToolUse` witness through `MarkWitnessed`;
   corroboration (non-authority) = probe stdout token present exactly once
   in the bounded captured tool result; commit `approved` (HTTP 200
   accepted); clean exit; hook dir removed; process group reaped.
2. **deny** — fresh session, same shape; claim `deny`; AUTHORITY = exact
   bound `permission_denials` witness; corroboration = no matching
   `PostToolUse` and zero probe-execution token in the captured stream;
   commit `rejected`; clean exit.

Privacy in evidence: committed captures are structural projections only —
`{seq, type/subtype, pseudonymized ids, contains_probe_token}` — never raw
model prose, cwd paths, hook URLs/tokens, or credentials. Pseudonyms use the
stable `(domain,value)→pseudonym` rule from the accepted R6-A7 evidence.
Daemon log and public DTO marker-absence checks mirror the accepted C1D live
proof.

## 4. Negative coverage map — deterministic, no live turns

| §7 negative | Accepted deterministic coverage |
|---|---|
| duplicate tap | `TestC3DB_DuplicateAlreadyAccepted`, `TestC3DB_ChangedOptionConflict` |
| stale epoch | `TestC3DB_StaleRuntimeRejected`, `TestClaudeDelivery_CatalogStaleRuntimeStop` |
| replacement | `TestP2A_EpochReplacementClearsCatalogIdentity`, `TestDeliveryGate_AcceptedItemSurvivesReplacement` |
| timeout | `TestClaudeDelivery_CompositionTimeout`, `TestClaudeDelivery_CatalogTimeoutFails` |
| stop/delete | `TestClaudeDelivery_DeferredExitThen{Stop,Kill,Delete}ClearsIdentity` |
| daemon restart | `TestClaudeRuntimeOf_DaemonRestartRestoresNothing`, `TestClaim_RestartNoResidualAuthority`, `TestC3DB_PrincipalNegativeMatrix/old_boot` |
| unsupported version/platform | `TestClaudeInstall_PreconditionFailuresMutateNothing`, `TestClaudeInstall_UncertifiedPlatformFails`, `TestClaudeCreate_UncertifiedPlatformFailsClosedWhenInstalled`, `TestClaudeInstall_DarwinAMD64Rejected` |
| cross-session/request substitution | `TestReserveEntryWrong*` suite, `TestClaudeDelivery_CatalogMutatedResumeInput`, `TestClaudeDelivery_DenyMutatedInputCannotCommit` |
| forged admission | §4 admission suite (`TestClaudeActionable_*`, forged-tuple rejections) |
| wrong witness kind | `TestClaudeDelivery_CatalogDenyWrongWitnessFails`, `TestClaudeDelivery_CatalogAllowWrongWitnessFails`, `TestClaudeDelivery_DenyPostToolUseCannotCommit` |
| principal negatives (route) | `TestC3DB_PrincipalNegativeMatrix` (missing/member/revoked/foreign-host/old-boot) |

No live turn re-proves any of these.

## 5. Non-goals

- No additional catalog entry, allow-always, or arbitrary Bash.
- No C0H/C0R, PermissionRequest, Agent SDK, Channels, PTY injection.
- No Claude prompt API; no daemon-restart restoration of pending authority.
- No DTO expansion; no raw provider text on any public surface.
- No Windows implementation.

## 6. Stop condition

After live evidence: freeze the implementation HEAD, run the complete
repository gate (backend build/vet/full `-race`, mobile tsc/Jest,
Android/native gate when available, invariant + canonical secret scan,
ancestry/equality/clean checks), commit the evidence report only after the
implementation gate, re-run documentation/diff/secret checks on the exact
final report HEAD, push, and STOP for the independent FINAL A1.2 review.
If the exact production positive path cannot be proven live, keep it
disabled, report `A1.2 BLOCKED`, and stop.
