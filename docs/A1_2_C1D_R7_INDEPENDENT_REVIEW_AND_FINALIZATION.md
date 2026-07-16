# A1.2 C1D R7 Independent Review and Finalization Handoff

Status: **R7 REJECTED FOR TWO FINALIZATION BLOCKERS — C1D ONLY**

Reviewed implementation: `6be7ea37f2a6481f1cca2dadaeb2a90a33142894`

This is the authoritative next packet for the existing execution agent. It is
intentionally short. Do not reopen closed R2–R7 work and do not begin C2D/C3D.

## 1. Repository startup

Use only `/Users/mhk/Documents/codex/DevRemote` on
`feature/phase10-multi-adapter`. Fetch and fast-forward only. Require:

- local HEAD equals `origin/feature/phase10-multi-adapter`;
- the reviewed implementation above is an ancestor;
- accepted C0D `e42d4c570e64462ce813861017cc635e338e68bf` is an ancestor;
- worktree is clean.

Stop on any mismatch. Never use a scratch checkout, rebase accepted history or
force-push.

## 2. Independent R7 result

Confirmed closed:

- exact hook schema and strict bounded decoder;
- pump EOF and deterministic terminate cleanup;
- entropy failure and bounded pending/active state;
- exact CWD in the production default launcher;
- configured version/path/digest certification and non-vacuous digest mismatch;
- Store timeout invalidation for both staggered records;
- real `StartIPCServer` and Unix socket availability in the optional live test;
- production actionability remains zero.

Independent gates on the frozen R7 tree passed:

- Claude-focused `go test -race` ten repetitions;
- `go test -race ./internal/term ./cmd/devremote -count=1`;
- `go vet ./...` and `git diff --check`.

R7 is not accepted because the race test still exits at a local generation check
before exercising Store high-water, and the live test can succeed without proving
an approval observation.

## 3. F1 — finish Store-owned linearization, do not add another local check

Authority owner: `AuthoritativeApprovalStore` generation high-water.

The R7 production direction is correct: `terminate()` installs
`SupersedeRuntime(sessionID, launchGen, streamGen=1, ...)`; a late C1D ingest uses
stream generation zero and must be rejected inside the Store.

The current `preIngestHook` fires before the final local `ingestGen` check, so the
test returns locally and never proves Store rejection. Correct only the proof and
the post-admission cleanup window:

1. Place a narrow nil-in-production barrier after every local check and
   immediately before `IngestObserved`.
2. In the test, pause there, run `Stop`/`terminate`, assert the Store high-water is
   installed, resume the real `joinDeferred`, and prove the old generation creates
   zero records.
3. Add a known-bad control or fault mode that replaces `SupersedeRuntime` with
   `InvalidateSession`; the same test must expose the stale record. Do not merely
   inspect a missing ApprovalID.
4. Cover the opposite ordering: Store admission wins, then terminate runs before
   active-list commit. The final Store record must be invalidated and the
   terminated runtime must have zero pending and zero active entries.
5. After Store admission, revalidate the runtime generation before adding the
   active entry. Perform compensating Store invalidation outside `turnMu`.

Do not add more `terminated`/`ingestGen` checks around the same non-linearized
boundary. Do not hold `turnMu` across Store calls or external I/O.

## 4. F2 — one executable production proof with strict assertions

Keep the environment-gated live test, but make it genuine acceptance evidence
rather than a logging smoke test.

Required path:

```text
real pokit run claude client request
  -> real Unix socket
  -> StartIPCServer
  -> production ManagedClaudeService
  -> pinned Claude Code 2.1.209 in requested CWD
  -> PreToolUse hook defer
  -> matching tool_deferred result
  -> AuthoritativeApprovalStore
```

Requirements:

- use the production client serialization used by `pokit run`, not handwritten
  JSON directly to an internal handler;
- require exact version, resolved path and configured digest before launch;
- poll with a bounded deadline and structural state, never a fixed sleep;
- fail unless exactly one matching bounded record is observed;
- assert provider/version/session/epoch binding, `Actionable=false`, zero options,
  zero delivery material and no CTA/claim path;
- assert raw command, tool input, CWD, hook token and provider payload are absent
  from public DTO/log evidence;
- after exit/Stop, assert the exact record is invalidated, registry is exited,
  hook directory/socket are gone and no child remains;
- record the exact command, digest, bounded redacted output and cleanup result in
  `docs/A1_2_C1D_EVIDENCE_REPORT.md`.

If the pinned 2.1.209 binary, ambient authentication or harmless bounded provider
turn is unavailable, report C1D **BLOCKED**. Do not substitute a fake launcher or
fixture and do not claim the skipped test as passed.

## 5. Frozen scope decision

For C1D observation-only, exact version + canonical real path + configured
pre-launch SHA-256 digest is the accepted artifact claim. Describe it exactly that
way. Do not implement macOS code-signing, cdhash, fd-exec, a generic attestation
framework or Windows support in this packet.

Spawned process-image attestation or an independently approved equivalent is a
C3D pre-actionability requirement. This staging does not authorize actionability.

## 6. Prohibited work

- no Claude allow/deny delivery or resume protocol;
- no options, ClaimForExecution, delivery capacity or mobile CTA;
- no C2D/C3D, N1, O1/O2, Agent SDK, Channels or terminal injection;
- no Codex/A1 core redesign;
- no generic provider SDK or observer cleanup;
- no further attestation research in C1D.

## 7. Completion gate

Before the implementation commit, re-read this document and map F1/F2 to exact
production code and non-vacuous tests. Freeze HEAD, then run:

- formatting and `git diff --check`;
- focused Claude race tests repeatedly;
- backend build/vet and full relevant race suite;
- frozen A1/SP1 regressions;
- documentation secret scan;
- no mobile gate unless mobile code changes (mobile changes are prohibited).

Commit implementation first, run the bounded live proof on that exact SHA, then
commit only the redacted report. Re-run the documentation/diff/secret gates on the
final report HEAD. Push that exact tree and verify local/remote equality and a clean
worktree.

Stop with:

```text
REVIEW REQUEST: A1.2 C1D Managed Claude Observation — <implementation SHA>
```

Do not begin C2D in the same session.
