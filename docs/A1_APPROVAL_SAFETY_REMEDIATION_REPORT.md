# A1 Approval Safety — Remediation Report (B1–B8)

Status: **READY FOR INDEPENDENT A1 RE-VERIFICATION**

## 1. Identity & SHAs

```text
remote:  https://github.com/mhkim315/DevRemote.git
branch:  feature/phase10-multi-adapter
rejected implementation:  3b56c4f05d16653b5fd9c494d8f1682135701603
verification/handoff:     85c34182d96aac7310c4cc694e83a1cfe26242f8
accepted S1.1 ancestor:   02c8385e3270fbbc4df45e0c71ccad6ebe11a076
remediation implementation HEAD: ed466094cd7a38d148d845d7057c63fe2019e1c9

remediation commits:
  c66233bba  B1-B6 backend: atomic claim, canonical digest, delivery receipt, safe DTO
  ed466094c  B6(mobile)+B7: safe DTO decoder + device-only transport
```

Accepted foundation preserved (verification §2): legacy/parser/status/PTY/prompt
authority removed, capability + provenance gating, `waiting_approval` display-only,
generation-bound records, host-bound transport, strict decode, no blind Y/N.

## 2. Standing blocker (reported per handoff R-D, not fabricated)

**No accepted provider action-delivery channel exists.** Codex 0.144.1's log only
OBSERVES its own resolution (`waiting_for_approval` → provider-internal
`approval_resolved`); there is no verified resolution protocol, and blind terminal
Y/N synthesis is prohibited. Claude 2.1.202 declares no `CapApprovalDetection`.

Consequently, per the frozen plan §4 and R-C:
- every ingested approval is **non-actionable intervention information** — no
  options, no claim path, no delivery (`provenActionMapping` returns none);
- the production delivery boundary is `unavailable`;
- **acceptance-gate item 2 (a real positive accepted-provider production path)
  cannot be satisfied.** It is reported here as the standing blocker rather than
  fabricated with a fake mapping. The full claim → delivery-receipt → commit
  machinery IS implemented and proven at the contract level with a controlled
  fixture; it activates when a provider resolution channel is separately verified
  with controlled redacted evidence.

A1 is therefore submitted as **structurally remediated (B1–B8) with item 2
blocked on the missing provider channel** — not claimed complete.

## 3. Production path (current)

```text
accepted correlated AgentEvent
 → capability-gated DetectApproval (safeDetectApproval; panic/error isolated)
 → SafeApprovalGate (authoritative provenance + confidence floor + ApprovalID)
 → generation-bound Ingest → NON-ACTIONABLE record (no proven mapping)
 → /api/sessions emits SafeApprovalDTO (bounded, redacted; actionable=false)
 → mobile decodes SafeApproval, renders intervention info, NO CTA

[actionable path — proven by fixtures, dormant until a provider channel exists]
 → host-bound device-authenticated POST (idempotency key)
 → HandleApprovalAction: strict decode → display lookup → actionability gate
   → server-derived requester (PrincipalFromContext) → canonical ActionDigest
   → atomic ClaimForExecution (full binding, opaque token)
   → runtime revalidation immediately before delivery
   → ApprovalDelivery boundary → DeliveryReceipt
   → RecordDelivery: commit ONLY on accepted/already_accepted bound to token+digest
```

## 4. Claim & receipt contracts

- **ClaimForExecution(ClaimRequest) ClaimResult** (`approval_store_gen.go`): one
  atomic transition validating existence, exact session, actionability, expiry,
  state, idempotency, server-derived requester + permission, current runtime
  (adapter/provider/version + launch/stream gen), selected option, and canonical
  ActionDigest. Issues an opaque 128-bit `crypto/rand` token (not derived from
  ApprovalID, never in a public DTO). Closed `ClaimOutcome`.
- **CanonicalAction.Digest()** (`approval_execution.go`): length-framed SHA-256
  over option ID, kind, schema version, input type/placement, normalized input —
  never the option list or raw payload. Same digest binds claim, delivery, receipt,
  idempotency, commit.
- **ApprovalDelivery / DeliveryReceipt** (`approval_delivery.go`): frozen closed
  outcomes `{accepted, already_accepted, stale_runtime, runtime_mismatch,
  unavailable, conflict, rejected}`. Production = `unavailable`. `CommandBroker`
  removed from the approval authority path entirely.
- **RecordDelivery** commits a successful terminal state ONLY for
  accepted/already_accepted bound to the exact token + digest; every other receipt,
  a missing record, or a token/digest mismatch fails closed (delivery_failed).
- Idempotency: same key+digest → `already_accepted`; same key+different digest →
  `conflict`; non-idempotent delivery is never auto-retransmitted.

## 5. Actionability, requester auth, DTO/privacy evidence

- **Actionability**: `provenActionMapping` yields no options for any provider →
  every approval non-actionable; the handler's actionability gate rejects a claim
  before any runtime/delivery work; the safe DTO exposes `actionable=false` and no
  options; mobile renders no buttons.
- **Requester auth**: derived solely from `devicetrust.PrincipalFromContext`
  (DeviceID/HostID/BearerSessionID/BootID/Permissions). The handler ALWAYS requires
  a device principal (no insecure-local bypass); `DisallowUnknownFields` rejects any
  client-supplied identity field; the remote route enforces `PermTerminalInput`.
- **DTO/privacy**: `/api/sessions` emits `SafeApprovalDTO` — a bounded structural
  allowlist (ID, session, Pokit summary, state, actionability, safe option ID +
  Pokit label + required-input metadata, expiry) with explicit byte bounds. Raw
  prompt, payload, arbitrary label/placeholder, path, token, claim token, and digest
  source never leave the daemon. Mobile enforces the same closed shape + bounds +
  unknown-field rejection. Audit logs carry IDs/outcome codes only.

## 6. Acceptance gate (plan §9) — evidence

| # | item | evidence | status |
|---|---|---|---|
| 1 | waiting_approval creates nothing | mobile sessionNeedsApproval/actionableApprovals; backend status never ingests | PASS |
| 2 | Codex positive full path | machinery proven by fixture; **no provider delivery channel** | **BLOCKED (reported)** |
| 3 | Claude/heuristic/PTY/prompt/legacy zero-actionable | ingestion tests + non-actionable default | PASS |
| 4 | stale launch gen rejected | TestClaim_RejectsFullBindingMismatches(stale-launch) | PASS |
| 5 | stale stream gen rejected | (stale-stream) | PASS |
| 6 | adapter/provider/version mismatch | (adapter/version-mismatch) | PASS |
| 7 | cross-session replay rejected | (cross-session) + store isolation | PASS |
| 8 | modified action/args fail digest | TestDigest_* + RecordDelivery wrong-digest conflict | PASS |
| 9 | concurrent approve vs reject one owner | TestClaim_ConcurrentApproveVsRejectOneOwner (race) | PASS |
| 10 | duplicate claim no second owner | TestClaim_DuplicateNoSecondOwner | PASS |
| 11 | expiry before/during claim fails closed | TestClaim_ExpiryFailsClosed | PASS |
| 12 | replacement between claim & delivery no stale success | TestHandler_RuntimeReplacedBetweenClaimAndDelivery | PASS |
| 13 | delete/unlink/termination during claim invalidate | TestRecordDelivery_DeleteBetweenClaimAndDelivery | PASS |
| 14 | delivery failure no successful state | TestRecordDelivery_OnlyAcceptedCommits | PASS |
| 15 | identical idempotency retry → already_accepted, no dup delivery | TestClaim_Idempotency* + TestHandler_IdempotentReplayNoDuplicateDelivery | PASS |
| 16 | same key modified digest → conflict | TestClaim_IdempotencySameKeyDifferentDigestConflict | PASS |
| 17 | generic overwrite cannot affect approval delivery | CommandBroker removed from approval path; dedicated boundary | PASS |
| 18 | restart no stale actionable authority | TestClaim_RestartNoResidualAuthority | PASS |
| 19 | paired-device principal + PermTerminalInput enforced | TestHandler_StrictDecodeAndAuthGuards / NoPrincipalForbidden | PASS |
| 20 | legacy bearer / cross-host rejected remotely | mobile no-fallback tests + backend requester binding | PASS |
| 21 | mobile resolveApproval host-bound authenticated write | approvalClient.test.ts | PASS |
| 22 | bounded/redacted DTO backend+mobile | TestSafeDTO_* + approvalRequest.test.ts | PASS |
| 23 | secret/path/prompt/payload/command/log-leak negatives | TestSafeDTO_RedactsRawFieldsAndBounds + secret scan | PASS |
| 24 | build/vet/race, tsc/jest, Android, invariant, ID, secret, diff | build-gate: all PASS; Android env-skip (see §7) | PASS* |
| 25 | clean worktree, local/remote equality, S1.1 ancestry | verified post-push | PASS |

## 7. Gate commands & results (on ed466094c)

```sh
cd companion-daemon
gofmt -l <touched-go-files>                     # clean (env gofmt-drift noise on pre-existing files only)
go build ./...                                  # OK
go vet ./...                                     # OK
go test -race ./internal/agent/... ./internal/term/... ./internal/transcript/...  # OK
cd .. && SKIP_NATIVE_GATE=1 sh scripts/build-gate.sh
  Backend build/vet/race/diff ... OK
  Mobile typecheck (tsc) ... OK      Mobile test (jest, 381) ... OK
  Invariants: vendor branch ... OK   ID inference ... OK
  Security: secret scan ... OK
git diff --check                                 # OK
```

**Android/Kotlin gate — environmentally unavailable, confirmed by inspection**:
`mobile/.gitignore` line 26 ignores `/android/` — the root Android project
(including `gradlew`) is intentionally EXCLUDED from version control (Expo prebuild
output, regenerated by `expo prebuild` + an Android SDK, neither present in this
environment). A1 changed ZERO Android/Kotlin/Gradle files
(`git diff --name-only 85c34182d..HEAD` matches none). The gate was run with
`SKIP_NATIVE_GATE=1` and reports the native step as SKIPPED — it was NOT executed
and is not reported as passed.

## 8. Scope exclusions

No N1/O1/O2, Task/Dispatch, worker acknowledgement/completion, automatic approval
policy, generic command execution, CLI redesign, ConPTY/Windows, cloud relay,
lock-screen actions, or workflow work. A1 proves only once-only daemon-boundary
acceptance semantics; worker ACK remains O1.

```text
REVIEW REQUEST: A1 Approval Safety remediation — ed466094cd7a38d148d845d7057c63fe2019e1c9
```
