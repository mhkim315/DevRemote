# A1.1 CP0 — Executor Contract Note + Binding Table

Packet: **CP0 (evidence freeze + production-path spike)**. Risk class: authority
canonicalization + exact-artifact certification + bounded state + deterministic
concurrency. Capacity stays ZERO; CP0 authorizes no production CTA and does not
authorize CP1.

Status of this note: written before completing the CP0 spike, per protocol §5 /
handoff §6. It is grounded in the installed `codex-cli 0.144.1` schema + a live
`initialize` handshake captured this session; the live command-approval/resolution
trace and the macOS process-image attestation are the remaining CP0 gates (see the CP0
evidence report).

## 0. Provenance (this session)

```text
codex --version         : codex-cli 0.144.1
PATH entry              : /opt/homebrew/bin/codex  (symlink → node shim codex.js)
node shim               : @openai/codex/bin/codex.js  sha256 134063e1…  (7236 bytes)
vendored native binary  : @openai/codex-darwin-arm64/vendor/aarch64-apple-darwin/bin/codex
                          Mach-O 64-bit arm64, 260405808 bytes, sha256 29915529…
launch chain            : node → codex.js → spawn(native codex app-server)
schema bundle (no --experimental): 267 files, deterministic manifest sha256 cafb9881…
transport               : stdio:// (default); ws:// experimental/excluded
auth                    : "Logged in using ChatGPT"
initialize result       : platformFamily=unix, platformOs=macos, version 0.144.1,
                          environmentId=null
```

## 1. Authority owner

The frozen provider-neutral A1 core (`AuthoritativeApprovalStore`) is the sole authority
for requester auth, `ApprovalID`, RuntimeRef, selected-action digest, idempotency,
atomic claim, expiry, supersession, receipt comparison and final commit
(`RecordDelivery`). CP0 mints NO authority and changes NO production code; it only
gathers exact-version evidence. The Codex bridge (CP1+) is a thin certified boundary
that receives a native request, binds it to the current RuntimeRef + connection epoch,
and delivers one native response — never minting authority or committing success.

## 2. Platform-neutral contract boundary (per the mid-review constraint)

The Codex **protocol** contract is OS-neutral: JSON-RPC over a byte transport,
`initialize`/`thread/start`/`turn/start`, a provider-owned request id + thread/turn/item
identity, `commandExecution/requestApproval`, an `accept`/`decline` response, and a
`serverRequest/resolved` consumption signal. None of that depends on macOS.

Everything OS-specific — binary/process-image identity, process lifecycle/cleanup,
filesystem paths, code-signing, and local IPC — lives behind an explicit
`ProcessImageAttestor` / `ManagedRuntimeLauncher` interface. The certification tuple
carries an explicit **OS + architecture** capability, so Windows (and other targets)
are certifiable through the same observable protocol contract with a separate
OS/arch/binary/schema tuple. macOS is only the first certification target, never a
contract dependency.

## 3. Canonical stored inputs (bounded provider registry, CP2)

Stored per native request in the bounded provider registry (never in the frozen public
DTO or `ApprovalExecutionBinding`):

- Pokit `SessionID`; `RuntimeRef{Adapter,Version,LaunchGen,StreamGen}`;
- provider = codex; OS/arch; binary digest + filesystem identity; schema manifest
  digest; app-server connection epoch;
- outer JSON-RPC request id + method; `threadId`; `turnId`; `itemId`; `approvalId`
  (must be null in scope);
- typed-canonical native fingerprint (see §5); `startedAtMs` (bounded sanity only).

## 4. Complete immutable binding

`ApprovalID = domainDigest(nativeKey ‖ nativeFingerprint)` where `nativeKey` is the
tuple in §3. This binds the exact provider request into the existing
`ApprovalExecutionBinding.ApprovalID` **without adding any provider field to the frozen
core/public contract**. Allow and deny map to distinct fixed Pokit option IDs, so
`CanonicalAction.Digest()` differs between them.

## 5. Canonicalization (typed, not raw JSON)

Parse the request into typed structures with a strict decoder that rejects duplicate
keys and unknown fields; validate types/UTF-8/integer-vs-float/length; then build the
Pokit canonical fingerprint from the typed values. Raw JSON serialization is never
authority or digest input; insignificant whitespace / key order never rejects.

Confirmed field roles at 0.144.1 (`CommandExecutionRequestApprovalParams`):

| Field | Schema fact (0.144.1) | Role |
| --- | --- | --- |
| outer JSON-RPC `id` | required by JSONRPCRequest envelope | authority (request ownership) |
| `threadId`/`turnId`/`itemId` | required | authority + fingerprint |
| `approvalId` | "null for regular shell/unified_exec"; UUID for zsh-bridge | authority; MUST be null in scope |
| `command` | string\|null | authority + fingerprint; **reject null/empty** |
| `cwd` | LegacyAppPathString\|null | fingerprint (normalized absolute); redacted for display |
| `environmentId` | default null | MUST be null/absent in scope |
| `commandActions` | "best-effort parsed … for friendly display" | display/corroboration only — **excluded** from fingerprint |
| `networkApprovalContext`, `proposed*Amendments`, `reason` | optional | excluded (null/absent) / display-only |
| `startedAtMs` | int64 | bounded sanity/replay only, not unique authority |

Decision enum (`CommandExecutionApprovalDecision`): `accept`, `acceptForSession`,
`acceptWithExecpolicyAmendment`, `applyNetworkPolicyAmendment`, `decline`, `cancel`.
Scope maps only `allow_once→accept` and `deny→decline`; all others excluded.

## 6. State transitions + single linearization point

```text
pending → response_reserved → response_written → provider_resolved
pending/response_reserved/response_written → cancelled | stale | timed_out | ambiguous
```

The **reservation** is the single per-request linearization point shared by competing
mobile decisions and provider cancellation. One single-owner ordered writer serializes
all app-server writes. No lock is held across the child process or mobile network.
Route registration/replacement/deletion in the delivery router shares the
runtime-supersession linearization (plan §3.1).

## 7. Exact success evidence

Success = a fully-bound `DeliveryReceipt` committed by `RecordDelivery` ONLY after: our
exact JSON-RPC response for the owning request id was written on the owning connection
epoch AND a matching `serverRequest/resolved` (carrying that requestId + threadId) was
observed AFTER our write on that same epoch. A resolved that arrives before our write is
provider-side cancellation/resolution, not success. Queue admission, a stdout write, or
a green test are NOT success.

## 8. Invalidation / restart / capacity

- runtime/process/launch/stream/connection replacement, correlation loss, deletion,
  termination, or binary/schema/version drift → revoke certification + supersede the
  native request + A1 approval;
- timeout before reservation → cancel, send nothing; timeout after write before resolved
  → ambiguous, mark non-success, disable the certified runtime, close the boundary;
- daemon restart → restore no claims/requests/approvals; prove the child dead or kill
  the orphan before new certification;
- capacity: default `Unavailable` route; only the exact server-derived certified tuple
  routes to the bridge; every other runtime/provider stays capacity 0; bounded provider
  registry with fail-closed exhaustion.

## 9. Adversarial counterexamples (one per critical invariant)

- **Binary replacement (TOCTOU):** the resolved binary is hashed, then swapped before
  exec → a path-based spawn runs different bytes. Mitigation must bind the actual
  spawned process image to the verified digest (exec from the verified open fd or
  equivalent post-spawn image attestation), demonstrated on macOS with a deterministic
  swap. If different bytes can start → CP0 BLOCKED. (Highest-risk CP0 gate.)
- **Raw-JSON digest:** two byte-different but semantically equal requests (whitespace/
  key order) produce different digests → duplicate/mismatch confusion. Mitigation: typed
  canonical fingerprint only.
- **Display-field authority:** trusting `commandActions`/`reason` as authority lets a
  friendly-display value change meaning. Mitigation: excluded from fingerprint.
- **Route by provider string:** routing on `provider=="codex"` alone lets an
  uncertified runtime deliver. Mitigation: server-derived certified-tuple routing only.
- **Resolved-before-write:** treating any resolved as success accepts provider-side
  cancellation as approval. Mitigation: resolved counts only after our write on the same
  epoch.
- **Stale generation:** a response written on a superseded connection epoch. Mitigation:
  epoch + RuntimeRef revalidation at reservation and write; atomic route deletion on
  supersession.

## 10. Explicit non-goals

No production capacity change in CP0; no CTA; no CP1; no store/DTO/`ApprovalExecutionBinding`
field change; no PermissionRequest hook, WebSocket, attached TUI, generic `pokit run
codex`, terminal renderer, send-text/key, Task/Dispatch, O1/O2, N1, A1.2 Claude, or
automatic approval policy.

## 11. Binding table

| Field | Created/derived at | Stored at | Recompared at | Copied into receipt at | Invalidated at | Negative test |
| --- | --- | --- | --- | --- | --- | --- |
| OS/arch/binary digest/schema manifest | launch certification | provider registry + launch binding + connection epoch | reservation + write (revalidate) | (bound via ApprovalID) | binary/schema/version drift, process/connection replacement | swapped binary; wrong schema manifest; wrong OS/arch |
| connection epoch | app-server connect | provider registry | reservation + write | — | reconnect/restart | write on stale epoch |
| outer JSON-RPC request id | provider (in request) | provider registry | reservation + write (owning id) | receipt via ApprovalID | request superseded | duplicate id same/different content |
| threadId/turnId/itemId | provider | provider registry + fingerprint | ingest + reservation | receipt via ApprovalID | supersession | cross-thread/turn/item response |
| approvalId (null) | provider | provider registry | ingest (must be null) | — | — | non-null approvalId rejected in scope |
| command (+cwd) | provider | provider registry + fingerprint | ingest (reject empty) | receipt via ApprovalID/PayloadDigest | supersession | empty command; modified command/cwd |
| selected action (allow/deny) | mobile decision → store | claim | claim/commit (CanonicalAction.Digest) | receipt | expiry/supersession | swapped decision; same-key different-digest retry |
| provider response payload | server-generated | transient (not queued as authority) | write | delivered-payload digest | ambiguous/timeout | payload from client; substituted decision |
| resolved consumption | provider notification | transient | after-write ordering check | commit gate | epoch change | resolved-before-write; missing resolved |
