# A1.1 CP0 — Executor Contract Note + Binding Table

Packet: **CP0 (evidence freeze + production-path spike)**. Risk class: authority
canonicalization + exact-artifact certification + bounded state + deterministic
concurrency. Capacity stays ZERO; CP0 authorizes no production CTA and does not
authorize CP1.

Aligned to the amended plan `A1_CODEX_PROVIDER_POSITIVE_PATH_PLAN.md` and
`A1_1_CP0_CHECKPOINT_REVIEW_1.md` at baseline `4e3357d`. Grounded in the installed
`codex-cli 0.144.1` schema + live traces captured this session.

## 0. Provenance + certified tuple (OS-neutral in the contract)

The provider-neutral certification tuple exposes ONLY platform-neutral, opaque values.
No macOS-specific identity leaks into the provider-neutral or Codex protocol contract:

```text
provider                 codex
provider version         0.144.1 exactly (app-server shipped-but-experimental; 0.144.1 only)
platform capability      OS + architecture + attestor kind/version
artifact identity        opaque bounded digest over the COMPLETE certified launch chain
process image identity   opaque bounded attestation from the platform verifier
schema identity          per-file digests + deterministic manifest digest (generated WITHOUT --experimental)
transport                local stdio JSONL
launch profile           explicit codex_app_server managed profile
protocol mode            app-server v2; experimentalApi disabled
request kind             normal commandExecution requestApproval only
actions                  allow_once, deny
```

Session evidence (macOS-specific details are attestor-internal, recorded only in the
darwin/arm64 evidence bundle, never in the provider-neutral contract):

```text
codex --version : codex-cli 0.144.1   (retrieval 2026-07-14)
launch chain    : node → codex.js shim (sha256 134063e1…) → vendored native
                  aarch64-apple-darwin/bin/codex (Mach-O arm64, sha256 29915529…)
schema bundle   : 267 files; deterministic manifest sha256 cafb9881…
initialize      : platformOs=macos, version=0.144.1, environmentId(init)=null
runtime approval: environmentId="local"; approvalId absent for shell; auth=ChatGPT
```

## 1. Authority owner

The frozen provider-neutral A1 core (`AuthoritativeApprovalStore`) is the sole authority
for requester auth, `ApprovalID`, RuntimeRef, selected-action digest, idempotency,
atomic claim, expiry, supersession, receipt comparison and final commit
(`RecordDelivery`). CP0 mints NO authority and changes NO production code. The Codex
bridge (CP1+) is a thin certified boundary that binds a native request to the current
RuntimeRef + connection epoch and delivers one native response; it never mints authority
or commits success.

## 2. Platform-neutral boundary (four explicit interfaces)

macOS inode, file descriptors, `/dev/fd`, Mach-O, code-signing, POSIX signals, path
grammar and local IPC live ONLY inside these implementations, which return
platform-neutral observable results:

- `ProcessImageAttestor` → OS, arch, attestor kind/version, opaque artifact identity,
  opaque process-image attestation, certification result + bounded reason;
- `ManagedRuntimeLauncher` → start/stop the certified runtime, opaque process identity +
  lifecycle result;
- `ProviderTransport` → ordered framed reads/writes, closure, connection generation,
  without OS-specific pipe/handle types;
- `ProviderPathCanonicalizer` → validates/canonicalizes provider path fields for the
  certified OS tuple without imposing POSIX grammar on other platforms.

First CP0 target: `darwin/arm64`. Windows is not built in A1.1 but must be certifiable
later through the SAME observable interfaces with a separate OS/arch/attestor tuple.

## 3. Canonical stored inputs (bounded provider registry, CP2)

Per native request (never in the frozen public DTO or `ApprovalExecutionBinding`):
SessionID; RuntimeRef{Adapter,Version,LaunchGen,StreamGen}; provider=codex; platform
capability + opaque artifact identity + opaque process-image attestation + schema
manifest digest; connection epoch; outer JSON-RPC request id + method; threadId; turnId;
itemId; approvalId (absent/null in scope); the typed-canonical native fingerprint (§5);
`startedAtMs` (bounded sanity only).

## 4. Complete immutable binding

`ApprovalID = domainDigest(nativeKey ‖ nativeFingerprint)`. This binds the exact provider
request into the existing `ApprovalExecutionBinding.ApprovalID` WITHOUT adding any
provider field to the frozen core/public contract. `allow_once`→`accept` and
`deny`→`decline` map to distinct fixed Pokit option IDs, so `CanonicalAction.Digest()`
differs.

## 5. Canonicalization (typed, not raw JSON) + confirmed field roles

Strict typed decode: reject duplicate keys + unknown fields; validate
types/UTF-8/int-vs-float/length; build the Pokit canonical fingerprint from typed values.
Raw JSON serialization is never authority/digest input; whitespace/key-order never
rejects. Confirmed at 0.144.1 from live `command_approval_*.json`:

| Field | Fact (0.144.1 live) | Role (amended) |
| --- | --- | --- |
| outer JSON-RPC `id` | int, required | authority (request ownership) |
| `threadId`/`turnId`/`itemId` | required | authority + fingerprint |
| `approvalId` | absent for shell/unified_exec (UUID for zsh-bridge) | authority; must be absent/null in scope |
| `command` | string\|null | authority + fingerprint; reject null/empty |
| `cwd` | path\|null | fingerprint (canonicalized via `ProviderPathCanonicalizer`); display redacted |
| `environmentId` | **"local"** at runtime (init-time is null) | **accept EXACTLY "local"**; authority + fingerprint + cert binding; null/other non-actionable |
| `availableDecisions` | present WITHOUT --experimental | NOT experimental; strictly decoded, bounded, in fingerprint (substitution/conflict); NEVER creates a Pokit action/authorization |
| `proposedExecpolicyAmendment` | non-null in live requests | bounded INTERNAL request data; in fingerprint; never public/logged/DTO; never an amendment action |
| `commandActions` | "best-effort … friendly display" | display/corroboration only; excluded from fingerprint |
| `networkApprovalContext`, `proposedNetworkPolicyAmendments`, `reason` | optional | excluded / display-only (null/absent) |
| `startedAtMs` | int64 | bounded sanity/replay only, not unique authority |

Decision enum: `accept`, `acceptForSession`, `acceptWithExecpolicyAmendment`,
`applyNetworkPolicyAmendment`, `decline`, `cancel`. Scope maps ONLY `allow_once→accept`
and `deny→decline`; all others excluded (including `acceptWithExecpolicyAmendment`).

## 6. State transitions + single linearization point

```text
pending → response_reserved → response_written → provider_resolved
pending/response_reserved/response_written → cancelled | stale | timed_out | ambiguous
```

The **reservation** is the single per-request linearization point shared by competing
mobile decisions and provider cancellation. One single-owner ordered writer serializes
all app-server writes. No lock is held across the child process or mobile network. The
delivery router's route register/replace/delete shares the runtime-supersession
linearization (plan §3.1).

## 7. Exact success evidence

Success = a fully-bound `DeliveryReceipt` committed by `RecordDelivery` ONLY after our
exact JSON-RPC response for the owning request id was written on the owning connection
epoch AND a matching `serverRequest/resolved` (carrying that requestId + threadId) was
observed AFTER our write on that same epoch. Resolved-before-write is provider-side
cancellation/resolution, not success. Queue admission / stdout write / green test are
NOT success. (CP0 additionally corroborates: accept ⇒ the exact invocation proceeded;
decline ⇒ it did not — with `resolved` remaining the A1 receipt authority.)

## 8. Invalidation / restart / capacity

Runtime/process/launch/stream/connection replacement, correlation loss, deletion,
termination, or binary/schema/version drift → revoke certification + supersede the native
request + A1 approval. Timeout before reservation → cancel, send nothing; timeout after
write before resolved → ambiguous, mark non-success, disable the certified runtime, close
the boundary. Daemon restart → restore nothing; prove the child dead or kill the orphan
before new certification. Capacity: default `Unavailable` route; only the exact
server-derived certified tuple routes to the bridge; every other runtime/provider stays
capacity 0; bounded provider registry with fail-closed exhaustion.

## 9. Adversarial counterexamples (one per critical invariant)

- **Binary replacement (TOCTOU):** hash the resolved artifact, swap it, then exec by path
  → different bytes run. Mitigation binds the actual spawned process image to the
  verified digest (exec the verified open file description, or a post-spawn attestation
  cryptographically tied to the digest), demonstrated on macOS with a deterministic swap.
  A pre-exec path recheck is NOT accepted. If different bytes can start → CP0 BLOCKED.
- **Raw-JSON digest:** whitespace/key-order-different but semantically equal requests
  digest differently → dup/conflict confusion. Mitigation: typed canonical fingerprint.
- **Display-field authority:** trusting `commandActions`/`reason`/`availableDecisions` as
  action authority. Mitigation: excluded from action authority; `availableDecisions` only
  in the fingerprint, `commandActions` only display.
- **environmentId drift:** accepting `environmentId != "local"` executes elsewhere.
  Mitigation: accept exactly "local"; anything else non-actionable.
- **Route by provider string:** routing on `provider=="codex"` alone. Mitigation:
  server-derived certified-tuple routing only.
- **Resolved-before-write:** treating any resolved as success. Mitigation: resolved counts
  only after our write on the same epoch.
- **Stale generation:** response written on a superseded connection epoch. Mitigation:
  epoch + RuntimeRef revalidation at reservation and write; atomic route deletion on
  supersession.

## 10. Explicit non-goals

No CP0 capacity change; no CTA; no CP1; no store/DTO/`ApprovalExecutionBinding` field
change; no PermissionRequest hook, WebSocket, attached TUI, generic `pokit run codex`,
terminal renderer, send-text/key, Task/Dispatch, O1/O2, N1, A1.2 Claude, automatic
approval policy, or amendment/session-scope actions.

## 11. Binding table

| Field | Created/derived at | Stored at | Recompared at | Copied into receipt at | Invalidated at | Negative test |
| --- | --- | --- | --- | --- | --- | --- |
| platform capability (OS/arch/attestor) | certification | provider registry + launch binding + epoch | reservation + write | (bound via ApprovalID) | OS/arch/attestor change | wrong OS/arch/attestor |
| opaque artifact identity (chain digest) | attestor at launch | provider registry + launch binding | reservation + write | (via ApprovalID) | binary/chain drift, process replacement | swapped chain artifact |
| opaque process-image attestation | attestor post-spawn | provider registry | reservation + write | (via ApprovalID) | process replacement | adversarial image replacement |
| schema manifest digest | schema gen (no --experimental) | provider registry | reservation | — | schema/version mismatch | wrong per-file/manifest digest |
| connection epoch | app-server connect | provider registry | reservation + write | — | reconnect/restart | write on stale epoch |
| outer JSON-RPC request id | provider (in request) | provider registry | reservation + write (owning id) | receipt via ApprovalID | request superseded | duplicate id same/different content |
| threadId/turnId/itemId | provider | provider registry + fingerprint | ingest + reservation | receipt via ApprovalID | supersession | cross-thread/turn/item response |
| environmentId ("local") | provider | provider registry + fingerprint + cert binding | ingest (must == "local") | (via ApprovalID) | supersession | environmentId != "local"; null |
| approvalId (absent/null) | provider | provider registry | ingest (must be absent/null) | — | — | non-null approvalId in scope |
| command (+cwd) | provider | provider registry + fingerprint | ingest (reject empty) | receipt via ApprovalID/PayloadDigest | supersession | empty command; modified command/cwd |
| availableDecisions | provider | provider registry + fingerprint | ingest (strict decode) | — | supersession | substituted availableDecisions |
| proposedExecpolicyAmendment | provider | provider registry (internal) + fingerprint | ingest | — | supersession | mutated amendment; leaked to DTO/log |
| selected action (allow/deny) | mobile decision → store | claim | claim/commit (CanonicalAction.Digest) | receipt | expiry/supersession | swapped decision; same-key different-digest retry |
| provider response payload | server-generated | transient (not queued authority) | write | delivered-payload digest | ambiguous/timeout | payload from client; substituted decision |
| resolved consumption | provider notification | transient | after-write ordering check | commit gate | epoch change | resolved-before-write; missing resolved |
