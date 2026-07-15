# A1.1 CP0 Production Entry + Runtime Replacement — Contract Note (pre-implementation)

Status: **CP0 EVIDENCE ONLY — capacity ZERO; no CP1; H0 and lifecycle #6 ACCEPTED**

Date: 2026-07-15. Executor: Claude Code. Baseline `63834fa5` (authorized packet handoff).
Authority: `docs/NEXT_EXECUTOR_A1_1_CP0_ENTRY_AND_REPLACEMENT_HANDOFF.md`.

## 1. Authority owner

- **Launch/spawn**: production composition root (`NewAppWithDeps`, feature flag
  `EnableCodexAppServerEntry`), spawning the pinned exact-version 0.144.1
  executable via explicit absolute path (no `exec.LookPath`, no PATH) with
  fail-closed version + artifact digest + realpath checks.
- **Generation invalidation**: the frozen `RuntimeRef.LaunchGen` /
  `RuntimeRef.StreamGen` comparison carried by `ApprovalExecutionBinding` and
  verified by `RuntimeDeliveryGate` at every write claim. The existing frozen
  `equal()` method on `RuntimeRef` is the comparison primitive — the gate
  enforces it; zero new fields on the frozen types.
- **Request correlation**: server-side mapping from (connection epoch, native
  request id) to POKIT identity. Owned by the entry-path runtime; invalidated
  atomically on epoch close, replacement or correlation loss.
- **Run lock + PG ownership**: same H0/H0-R1 discipline — the production
  child inherits the same supervisor/SCM_RIGHTS PG-ownership protocol the
  harness uses.

## 2. States (provider connection epoch + generational binding)

idle → spawn-pending (pinned identity verified) → connected (initialize handshake
complete; LaunchGeneration recorded) → correlated (thread/started + turn/started
+ requestApproval observed, all bound to the same LaunchGeneration) → stopping
(epoch close: all pending requests superseded atomically; owned PG killed;
fd/timer cleanup) → stopped.

For the response write: `pending → write_claim (generation + epoch compared) →
written (write + flush under the single serialization boundary; resolved
observed) | refused (generation/epoch mismatch).`

## 3. Linearization points

1. **Generation publish**: typed `RuntimeRef{Adapter, Version, LaunchGen,
   StreamGen}` is captured at spawn and never mutated; the entry-path runtime
   stores it under the connection-lifetime lock. Every correlated request
   inherits the same generation.
2. **Write claim** (shared by response and replacement): `RuntimeDeliveryGate`
   compares the caller's `RuntimeRef` against the current live generation
   **under the gate's internal lock**. Any mismatch (stale adapter, LaunchGen
   bump, StreamGen bump) → refuse before any write.
3. **Wire publication**: unchanged from H0-A — write + flush + seq under the
   single AppServer lock.

## 4. Cleanup success evidence

H0-C evidence standard applies: bounded poll after deterministic SIGKILL;
`_assert_pg_cleaned`; no live/zombie in the owned PG; unrelated Codex PIDs
preserved; failed ps/pgrep observation → test failure.

## 5. Timeout, exception, and replacement behavior

- **Timeout before correlation**: entry path closes the epoch and falls back to
  non-actionable display. Nothing synthesized.
- **Replacement mid-flight**: epoch close invalidates all pending native request
  ids **atomically under the gate lock** (the epoch bump is the single
  linearization point); a late write attempt with the old generation is refused
  before the write; a late `resolved` on the old epoch never counts as success
  (matched on (epoch, id, after-our-write) — plan §7).
- **Correlation loss**: garbled framing, pipe close, or unexpected app-server
  exit closes the epoch and invalidates. No retry, no replay, no synthesized
  decision.
- **Provider-side restart** (Packet B live trace): kill the app-server child
  while approval is pending; spawn a fresh instance; a response attempt with the
  old id on the new connection yields NO resolved (provider ignores silently —
  already proven in #6). Combined with the stale-generation gate refusal, the
  enforcement chain is: provider-side silence AND POKIT-side refusal. Neither
  alone is sufficient for authority; together they provide the fail-closed
  enforcement.

## 6. Binding table

| Field | Created at | Stored at | Compared at | Negative test |
| --- | --- | --- | --- | --- |
| `RuntimeRef.Adapter` | spawn (closed vocabulary `"codex_app_server"`) | entry runtime field | `RuntimeRef.equal()` at write claim | `TestRuntimeDeliveryGate_StaleGenerationRefused` — changed adapter → refused |
| `RuntimeRef.LaunchGen` | spawn (monotonic counter) | same | `RuntimeRef.equal()` at write claim | same test — stale LaunchGen → refused |
| `RuntimeRef.StreamGen` | spawn (0, immutable; stream gen unused in CP0) | same | `RuntimeRef.equal()` at write claim | same test — stale StreamGen → refused |
| Connection epoch | spawn (run id / timestamp) | entry runtime field | compared at every pending-request transition; epoch close invalidates | `TestRuntimeEntry_EpochBumpInvalidatesAll` — late write with old epoch refused |
| Pinned artifact identity | spawn (toolchain path + shim/native sha256 + version string) | entry runtime field | verified fail-closed BEFORE exec; recorded in trace header | harness `_pinned_codex_bin` fail-closed paths |
| Provider threadId/turnId/requestId | observed on the connection (JSON-RPC notification/response) | server-side correlation store | matched at (id, epoch, generation) for resolved/ack | `TestRuntimeEntry_CorrelationLoss` — late write on mismatched epoch refused |
| Owned PGID | supervisor `setsid` at spawn (H0 protocol) | entry runtime field (for cleanup) | `_terminate_pg` on stop/replace; `_assert_pg_cleaned` post-replacement | `TestRuntimeEntry_ReplaceChild_Cleaned` — old PG dead, no process leak |

## 7. Non-goals (handoff §10 + reviewer boundary)

No approval activation, no capacity over zero, no CTA; no CP1 buildout (no
reconnect/resume, no delivery bridge, no registry ingestion); no
cdhash/EndpointSecurity/supply-chain/Windows; no frozen A1 contract edits; no
Recorder/PTY involvement; no Task/Dispatch/pokit-run redesign; no plan edits; no
global toolchain changes; no credential copies; no N1/A1.2/O1/O2.
