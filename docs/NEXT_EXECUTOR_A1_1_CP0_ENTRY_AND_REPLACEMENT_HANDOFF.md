# Next Executor Onboarding — A1.1 CP0 Production Entry + Runtime Replacement

Status: **PACKET AUTHORIZED — final CP0 evidence stage — CAPACITY ZERO — CP1/N1/A1.2
UNAUTHORIZED — gate #1 (cdhash/supply-chain) remains FORBIDDEN/DEFERRED**

Date: 2026-07-15. Authorized by independent review after ACCEPT of H0/H0-R1
(`b60ed2474`) and CP0 lifecycle evidence #6 (`42429c8ac`).

This is the authoritative onboarding document for the execution agent. Follow
`docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md` at every step. Do not restart CP0
research from the beginning; do not re-collect accepted evidence.

## 1. Role and single objective

You are the **execution agent**, not the independent verifier. This packet closes
the LAST TWO open CP0 items:

- **Packet A (gate #7)** — a real bounded PRODUCTION entry path that starts the
  pinned Codex app-server under POKIT managed launch and binds POKIT identity to
  provider thread/request identity.
- **Packet B** — runtime replacement evidence: provider restart / session
  replacement / correlation loss immediately invalidates prior approval requests,
  and a late response/resolved from a previous generation can never regain
  authority.

Nothing else. After both, stop for an independent review that will ALSO decide
whether gate #1 is dropped from the CP0 requirements or A1.1 stays BLOCKED — that
decision belongs to the reviewer, never to you.

## 2. Canonical repository identity and startup checks

```text
checkout: /Users/mhk/Documents/codex/DevRemote   (ONLY this checkout)
remote:   https://github.com/mhkim315/DevRemote.git
branch:   feature/phase10-multi-adapter
baseline: 42429c8acd4c2840a93e1ab11c53f8e21c34bc19
```

At startup: fetch; local == remote == baseline; clean worktree; ancestry
`997a697b5…` and `b60ed2474…` both pass. Stale-run sweep per H0 rules
(`.probe.lock` owner line; no probe/app-server leftovers). Pinned toolchain
verification (fail-closed; never proceed on mismatch):
`~/.pokit-cp0-toolchain/node_modules/.bin/codex --version` == `codex-cli 0.144.1`;
global `codex --version` (0.144.4) is NEVER used for evidence; harness
`test_compile` passes. Production spawn code in Packet A must meet the same
identity bar (shim `134063e1…`, native `29915529…`, realpath check). Do not
modify the global toolchain.

## 3. Frozen state (unchanged by this packet)

- S1.1 + provider-neutral A1 core accepted and FROZEN (`internal/term`:
  `AuthoritativeApprovalStore`, `ApprovalExecutionBinding`, `CanonicalAction`,
  `ApprovalDelivery`, `RecordDelivery`, RuntimeRef + generation invalidation,
  bounded DTOs). Bind to them; never redesign or edit their contracts.
- Production approval-delivery capacity ZERO; `provenActionMapping` empty; no
  CTA; no terminal/prompt/PTY/send-text path may become approval authority.
- Accepted evidence and tombstoned legacy files in `docs/a1_1_cp0_evidence/` are
  reviewer-owned history — never rewrite them.
- Redaction standard (accepted in #6, mandatory for all new evidence):
  method-specific structural projectors; nested dict/list NEVER passes the
  allowlist verbatim; `input` -> `<REDACTED-INPUT>`; `availableDecisions` tags
  only; commands -> `<REDACTED-CMD>`; stable `(field-domain, original-value)`
  pseudonyms with `requestId` sharing the `objid` domain; projected item fields
  use `itemType`/`itemStatus` (never clobber the method key).

## 4. Minimal read order

1. this handoff;
2. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
3. `docs/A1_CODEX_PROVIDER_POSITIVE_PATH_PLAN.md` sections 7-10 — section 9 holds
   the blocking product question this packet must answer;
4. `docs/A1_1_CP0_EVIDENCE_REPORT_3.md` (already proven; CP3 finding: the
   provider silently ignores duplicate/late responses — POKIT ordering is the
   only defense);
5. `scripts/cp0/appserver_probe.py` at HEAD (pinned spawn, projectors, run-lock
   and PG-ownership discipline);
6. `cmd/devremote/app.go` + `internal/term/runtime.go` and the accepted runtime
   contracts you must bind to (read, do not redesign).

## 5. Packet A — gate #7: bounded production entry path

The blocking question (plan section 9), answered with production code or an
honest BLOCKED verdict:

> Can a real user start/resume a Codex thread and turn through a narrow
> production POKIT app-server path without building a new terminal renderer or a
> generic task API?

Required, in production Go (companion-daemon):

- an explicit managed `codex_app_server` provider runtime: stdio JSON-RPC pipes,
  reachable from the real daemon composition root (`NewAppWithDeps` wiring)
  behind an explicit default-false feature flag — NOT from tests, NOT from the
  CP0 harness;
- spawn digest-pinned to the exact 0.144.1 toolchain path with fail-closed
  version + artifact + realpath checks (no `exec.LookPath`; the existing `codex`
  PATH preset stays untouched);
- launch binding recorded at spawn from canonical server state: SessionID,
  LaunchGeneration, connection epoch, opaque process identity (PID +
  process-start identity), pinned artifact identity;
- provider correlation recorded server-side: threadId/turnId/native request id
  observed on that connection epoch, bound to the launch binding;
- the entry is authenticated, provider-specific and limited to starting/resuming
  a thread and starting the controlled certification turn; it must cause a REAL
  `item/commandExecution/requestApproval` on that connection;
- the approval request stays NON-ACTIONABLE: display/record only, no actionable
  A1 ingestion, no delivery, no response write from production code.

Blocker matrix (every row needs a test that fails if the row is reintroduced):

```text
ID: P7-1  Bad: test-only driver claimed as production path
  Required: spawn/thread/turn reachable from the wired daemon entry
  Test: production-path test through the wired entry (real pinned binary;
  environmental skip recorded honestly if unavailable — never faked)

ID: P7-2  Bad: entry generalizes into Task/Dispatch/remote-exec/terminal
  Required: closed vocabulary; fixed controlled certification turn; no
  caller-supplied command/prompt passthrough; no PTY/renderer
  Test: API-level negative tests — arbitrary input/action rejected

ID: P7-3  Bad: app-server stdio routed through Recorder/PTY/echo paths
  Required: pipes isolated; "Recorder is single PTY reader" invariant intact
  Test: invariant test proving no Recorder/PTY attachment for this runtime

ID: P7-4  Bad: provider identity not bound to POKIT identity
  Required: launch binding + correlation compared at every transition;
  stale/mismatched SessionID/LaunchGeneration/epoch fails closed
  Test: direct API-level negative tests on the binding object itself

ID: P7-5  Bad: approval becomes actionable anywhere on this path
  Required: capacity zero preserved; provenActionMapping empty; no CTA
  Test: existing A1 regressions + explicit test this runtime cannot reach
  delivery

ID: P7-6  Bad: unpinned or PATH-resolved spawn
  Required: explicit absolute pinned path + fail-closed verification before exec
  Test: wrong-digest/missing-binary fixture -> fail-closed, no spawn
```

If a defensible bounded user path does NOT exist within these constraints, stop
and report **BLOCKED** per plan section 9 — an acceptable outcome that must not
be papered over with a heuristic or a test-only stand-in.

## 6. Packet B — runtime replacement evidence

Prove with the pinned provider and deterministic tests:

- **B-1 provider restart**: kill/replace the app-server while an approval request
  is pending -> on the new connection the old request has no authority: an
  old-id response yields NO `serverRequest/resolved` (provider ignores silently,
  so POKIT-side refusal is the enforced boundary — record both);
- **B-2 stale-generation response**: a response attempt bound to a previous
  LaunchGeneration/connection epoch is refused BEFORE any write, proven with a
  deterministic seam + known-bad control (no sleeps as race evidence; assert the
  contested intermediate state);
- **B-3 late resolved**: a resolved associated with a previous generation or
  arriving before our exact write never counts as success (plan section 7);
  ambiguous -> non-success;
- **B-4 correlation loss**: pipe close / garbled framing / epoch bump invalidates
  ALL pending native requests immediately; nothing retried or synthesized.

Blocker matrix:

```text
ID: RR-1  Bad: old-generation response regains authority after replacement
  Required: generation/epoch compared at the single write linearization point
  Test: deterministic stale-generation write attempt -> refused; known-bad
  control (comparison bypassed) proves the test catches it

ID: RR-2  Bad: late/foreign resolved counted as success
  Required: resolved matched on (same epoch, same request id, after our write)
  Test: resolved-before-write and resolved-on-old-epoch -> non-success both

ID: RR-3  Bad: replacement leaves pending requests actionable
  Required: epoch close supersedes ALL pending native requests atomically
  Test: pending set observed invalidated at the contested intermediate state

ID: RR-4  Bad: replaced child or its process group survives
  Required: owned-PG kill + cleanup discipline extends to the production child
  Test: replacement test asserts no live/zombie process in the old owned PG
```

Live B-1 traces extend `scripts/cp0/appserver_probe.py` (a `replace` mode under
the run lock); B-2/B-3/B-4 use deterministic Go tests. Redaction standard
section 3 applies to every committed trace.

## 7. Mandatory pre-implementation contract note

Risk class: **authority + concurrency** -> full protocol section 5 discipline
BEFORE any production edit. Write
`docs/A1_1_CP0_ENTRY_REPLACEMENT_CONTRACT_NOTE.md` with: authority owner;
canonical stored inputs; complete immutable binding; states; the single write
linearization point shared by response, replacement and invalidation; exact
success evidence; failure/invalidation/restart/capacity behavior; one
adversarial counterexample per invariant; explicit non-goals; and the binding
table (`Field | Created/derived at | Stored at | Recomputed/compared at |
Copied into request/receipt at | Invalidated at | Negative test`) covering at
least SessionID, LaunchGeneration, connection epoch, process identity, pinned
artifact identity, provider threadId/turnId/request id.

## 8. Gates

Focused first: new Go tests; harness `test_compile`; the 8 accepted no-model
harness tests stay green; new replace-trace smoke; hard wall-clock watchdogs
kept. Because production Go changes, the FULL repository gate runs on the frozen
final tree: `sh scripts/build-gate.sh` (build, vet, race tests, diff-check,
invariant + secret scans; mobile per CLAUDE.md — record `not-run` with reason if
unavailable). Never commit or amend mid-gate (doctor E2E is HEAD-stable).

## 9. Report, evidence and stop condition

- `docs/A1_1_CP0_EVIDENCE_REPORT_4.md`: gate-by-gate rows for #7 and replacement
  with exact code refs + tests; protocol section 11 evidence levels; honest
  environmental skips; BLOCKED verdicts stated plainly;
- redacted traces + summaries in `docs/a1_1_cp0_evidence/` (leak scan CLEAN
  before commit: prompt/command/payload//Users//tmp/hostname patterns);
- invariant self-audit mapping every P7-*/RR-* row to code and a non-vacuous
  test; production-wired vs test-only vs unavailable vs skipped labeled;
- stage-prefixed commits, push without history rewrite, verify local/remote
  equality + ancestry + clean worktree;
- required marker, then STOP for independent verification:

```text
REVIEW REQUEST: A1.1 CP0 Production Entry + Runtime Replacement — <implementation SHA>
```

Do not call CP0, A1.1 or A1 complete. The gate #1 disposition (drop from CP0 vs
keep A1.1 BLOCKED) is decided by the independent review AFTER this packet.

## 10. Explicit non-goals

- no approval capability activation; capacity stays ZERO; `provenActionMapping`
  stays empty; no CTA;
- no CP1 buildout beyond the minimal entry slice of section 5 (no
  reconnect/resume machinery, no delivery bridge, no registry ingestion);
- no cdhash, code-signing, EndpointSecurity, Windows or supply-chain attestation;
- no edits to frozen A1 core contracts, accepted evidence or tombstones;
- no Recorder/PTY/terminal renderer/send-text/send-key involvement;
- no generic Task/Dispatch/`pokit run` redesign; no WebSocket/relay transport;
- no plan-document edits; no global toolchain changes; no credential copies;
- no N1/A1.2/O1/O2.

## 11. Environment notes

- The Bash safety classifier flaps for long stretches. Single, non-compound
  commands matching existing allow rules (`python3 <absolute path> ...`,
  `python3 - <<EOF`) run without it; `cd X && ...` compounds do not. Do NOT add
  broad permission rules (explicitly denied earlier); if commit/push is blocked,
  leave the worktree ready and report — the reviewer commits from the canonical
  checkout.
- Live provider traces spend real model turns; run once per scenario,
  sequentially, under the run lock; verify lock/PG cleanup after each.
