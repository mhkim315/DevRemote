# Pre-Device QR and Terminal-Input Remediation Plan

**Status:** EXECUTED THROUGH INPUT-B — DEVICE GATE PAUSED FOR TERMINAL REGRESSION REMEDIATION

**Branch:** `feature/phase10-multi-adapter`

**Current non-device checkpoint:** `f052e7f8a60ae0ece8b7a5535045f59c55b8e3c4`

**Target device:** Samsung SM-S926N (Galaxy S24 Ultra), Android 16

**PB ACCEPT SHA:** **UNSET**

> **Authoritative device-gate amendment:** The first physical run exposed a
> managed-PTY geometry regression and split/duplicated WebView control
> authority. Device testing is paused. The required TERM-G1 and TERM-C1 packets,
> evidence flow, and restart criteria are frozen in
> [`PB_DEVICE_GATE_TERMINAL_REGRESSION_REMEDIATION_PLAN.md`](PB_DEVICE_GATE_TERMINAL_REGRESSION_REMEDIATION_PLAN.md).
> This amendment does not change the already accepted QR, Input-A, or Input-B
> contracts. `PB_DEVICE_CANDIDATE_SHA` and `PB_ACCEPT_SHA` remain unset.

## 1. Purpose and authority

The physical PB device matrix starts with pairing and includes truthful terminal
input. The current QR presentation is not reliably usable in the available
terminal geometry, and the current input UX can claim success after an
unauthorized frame was discarded. Running the device gate before correcting
those two prerequisites would produce ambiguous evidence rather than a useful
PB comparison.

This document is the sole authority for the bounded pre-device exception to the
original post-PB debt order. It does not reopen PB legacy removal, PA4 managed
isolation, pairing trust, or terminal product redesign. Historical PB evidence
remains immutable; new evidence is regenerated after all three packets are
independently accepted.

## 2. Frozen execution sequence

```text
CURRENT_NON_DEVICE_CHECKPOINT
→ QR renderer/security implementation
→ independent QR ACCEPT
→ Input-A effective-permission/read-only UX
→ independent Input-A ACCEPT
→ Input-B exact-generation acknowledged input
→ independent Input-B ACCEPT
→ PB.6/PB.7 full regeneration
→ PB_DEVICE_CANDIDATE_SHA freeze
→ daemon and APK built from the same production candidate
→ SM-S926N physical-device matrix
→ final independent PB ACCEPT
```

No packet may begin before the preceding independent acceptance. QR, Input-A,
and Input-B each use an implementation commit followed by an evidence-only
commit. Rejection is repaired within the same packet before the sequence
advances.

## 3. Identity model

The following identities are deliberately distinct:

| Identity | Meaning | Current value |
|---|---|---|
| `CURRENT_NON_DEVICE_CHECKPOINT_SHA` | Accepted automated PB state before remediation | `f052e7f8a60ae0ece8b7a5535045f59c55b8e3c4` |
| `QR_IMPL_SHA` / `QR_EVIDENCE_SHA` / `QR_ACCEPT_SHA` | QR production change, evidence-only head, and independent acceptance | **UNSET** |
| `INPUT_A_IMPL_SHA` / `INPUT_A_EVIDENCE_SHA` / `INPUT_A_ACCEPT_SHA` | Input-A production change, evidence-only head, and independent acceptance | **UNSET** |
| `INPUT_B_IMPL_SHA` / `INPUT_B_EVIDENCE_SHA` / `INPUT_B_ACCEPT_SHA` | Input-B production change, evidence-only head, and independent acceptance | **UNSET** |
| `PB_DEVICE_CANDIDATE_SHA` | Exact production/mobile source tree used for device binaries | `ab1884662` |
| `PB_AUTOMATED_EVIDENCE_SHA` | Evidence-only head recording regenerated PB.6/PB.7 gates | `b18123e77` |
| `DAEMON_BUILD_SOURCE_SHA` | Source identity embedded in the tested daemon | `ab1884662` |
| `APK_BUILD_SOURCE_SHA` | Source identity embedded in the tested APK | `ab1884662` |
| `PB_ACCEPT_SHA` | Final independent acceptance after the physical matrix | **UNSET** |

`PB_DEVICE_CANDIDATE_SHA` is a production identity.
`PB_AUTOMATED_EVIDENCE_SHA` is a later documentation identity and must never be
substituted for it. The daemon and APK used on the device must both attest the
same `PB_DEVICE_CANDIDATE_SHA`; rebuilding from the evidence head is prohibited.
`PB_ACCEPT_SHA` remains unset until the physical matrix passes and an
independent verifier accepts the exact evidence.

## 4. QR renderer and security packet

### 4.1 Frozen boundary

This packet changes presentation and temporary-file handling only. Pairing
payload bytes, fields, values, cryptographic transcript, host-key and
fingerprint verification, bootstrap-token single-use and expiry semantics,
mobile parser contract, and protocol version remain byte-for-byte unchanged.

Required behavior:

- Query real terminal width and height only for a TTY.
- Render ANSI only when the compact QR, including its quiet zone, fits both
  measured dimensions. Query failure, zero dimensions, or non-TTY selects PNG.
- Compress two module rows into one terminal row with half-block glyphs.
- Emit exactly four white modules on all four sides. Quiet rows are blank white
  rows and must never repeat QR data.
- Set foreground and background colors explicitly for every rendered cell and
  reset ANSI state at every row boundary and once after final output.
- Create PNG fallback files atomically with an unpredictable name, mode `0600`,
  and current-user ownership. Refuse symlinks, non-regular targets, permissive
  modes, or ownership mismatch.
- On macOS, invoke the opener directly with an argv vector. Never interpolate a
  path through a shell, environment command string, log message, or URL.
- Opener failure is non-fatal after secure creation: print only the safe local
  PNG path and continue waiting for pairing. Secure creation failure is fatal
  and aborts the pairing attempt.
- Never emit the raw pairing JSON or bootstrap token to stdout, stderr, logs,
  diagnostics, process arguments, or error wrapping.

### 4.2 Renderer selection state machine

```text
build the unchanged payload bytes
→ encode QR modules
→ is stdout a TTY?
    no  → secure PNG
    yes → query width and height
          query failed or either dimension unknown → secure PNG
          compact QR plus exact quiet zone fits both → ANSI half-block
          otherwise → secure PNG
→ if PNG created, attempt direct-argv open
    open succeeds → continue waiting
    open fails → print safe path only and continue waiting
    secure creation fails → abort pairing
```

Selection must be based on the complete rendered footprint, not the raw module
count. ANSI output may not be attempted optimistically when geometry is
unknown.

### 4.3 Cleanup lifecycle

Track only the exact file created by the active pairing attempt. Best-effort,
bounded removal runs on pairing success, rejection, timeout/expiry,
cancellation, signal-aware normal shutdown, and ordinary return. Cleanup
failure must not reveal payload data.

Startup cleanup may inspect only the dedicated temporary directory and the
precise `pokit-pair` filename pattern. Before deleting an expired candidate it
must use non-following metadata and prove regular-file type, no symlink,
current-user ownership, restrictive mode, and expiry. It must cap work by entry
count or elapsed time and never scan or remove unrelated files.

### 4.4 Mandatory tests

- Payload byte equality and successful decode by the existing mobile parser.
- Width and height boundary tables: exact fit, one-column short, one-row short,
  unknown size, failed query, and non-TTY.
- Half-block mapping for even and odd module heights.
- Exact four-module white quiet zone on every side; no copied data rows.
- Explicit color and per-row/final reset assertions, including error exits.
- PNG round-trip decode to exactly the original payload bytes.
- Atomic unpredictable creation, `0600`, current UID, no overwrite, symlink
  rejection, and safe concurrent attempts.
- Direct-argv opener proof with hostile path characters and no shell execution.
- Opener failure continues pairing and exposes only the safe path; secure-file
  creation failure aborts.
- Cleanup on every terminal outcome plus bounded stale-file cleanup with
  ownership, mode, age, symlink, non-regular, and unrelated-file negatives.
- Capture stdout, stderr, logs, and opener argv and prove absence of raw payload
  serialization and bootstrap-token material.

### 4.5 Stop conditions and evidence

Stop if payload bytes or parser behavior change, trust or expiry is weakened, a
token reaches an output surface, an unsafe file can be followed or overwritten,
or ANSI is selected without proven fit. Envelope compaction, protocol V2,
CBOR, key shortening, trust-flow changes, remote pairing, and token-policy
changes are out of scope.

The implementation commit contains only the renderer/security change and its
tests. The evidence commit contains documentation only and records exact
ancestry, focused tests, full daemon/mobile gates, byte-equality proof,
dimension/decoding tests, output-leak scans, file-security tests, diff scope,
HEAD/upstream equality, and a clean worktree. Independent QR ACCEPT names both
exact SHAs.

## 5. Input-A — effective permission and read-only UX

### 5.1 Authority contract

Server-issued effective permissions are the sole authorization truth. Mobile
state must expose one immutable authentication snapshot containing token,
expiry, effective permissions, and monotonically changing auth generation.
Cold start and renewal must re-run server verification before permissions
become known. Stored pairing data, local storage, QR role, UI role labels, and
session capabilities cannot grant permission. Unknown, expired, mismatched, or
renewing permissions fail closed to read-only.

`sessionCanInput` describes session/transport capability.
`deviceCanInput` describes the authenticated principal. Input is enabled only
when both are true. An unauthorized device has TextInput, Send, macros, paste,
programmatic input entry points, and direct xterm keyboard input disabled and
sees a precise viewer/member read-only reason. Owner-only input policy remains
unchanged.

### 5.2 Transitional daemon behavior

Existing binary framing and the current 40 ms text/Enter split remain
unchanged in Input-A. Unauthorized binary input must perform zero
`TerminalTransport.WriteInput` calls and cause zero Transcript mutation. The
daemon sends a bounded connection-level `read_only` denial control notification
and may coalesce repeated denials. This notification means only that the
connection is not input-authorized; it is not a per-input delivery ACK and the
mobile must not describe it as one.

### 5.3 Owner-state precondition

Before implementation evidence closes, inspect the actual device registry and
record the SM-S926N role and server-returned effective permissions. If it is the
usable owner, proceed. If the owner is orphaned or unusable, stop and propose a
separate host-local owner-recovery micro-packet. No member auto-promotion,
remote self-promotion, permission-array shortcut, or silent ownership transfer
is allowed in Input-A.

### 5.4 Mandatory tests

- Server verification populates token/expiry/permissions/auth generation as one
  immutable snapshot; replacement is atomic and stale renewal cannot win.
- Cold-start restoration and renewal both verify server permissions; stored or
  QR-derived roles cannot grant input.
- Unknown, expired, renewal-in-progress, verification-failed, and generation-
  mismatched permission states are read-only.
- Truth table for independent `sessionCanInput` and `deviceCanInput` values.
- Every mobile input surface is disabled for unauthorized devices and enabled
  for an authorized input-capable session; the reason text is accurate.
- Owner binary input reaches the exact transport on the retained production
  path. Member input yields a bounded denial, zero write, and zero Transcript
  mutation while output/replay remains available.
- Denial coalescing cannot amplify traffic and cannot be confused with delivery
  success.
- Existing binary wire and 40 ms split remain byte-compatible.

### 5.5 Stop conditions and evidence

Stop if local state becomes authorization authority, unknown permission enables
input, member policy expands, output-only access regresses, the daemon silently
discards without denial, or the denial is presented as delivery success.

The implementation and evidence commits are separate. Evidence records the
registry owner precondition, exact permission response, focused daemon/mobile
tests, full gates, no-policy-expansion proof, diff scope, exact ancestry,
HEAD/upstream equality, and clean worktree. Independent Input-A ACCEPT is
required before Input-B begins.

## 6. Input-B — exact-generation acknowledged input

### 6.1 Production compatibility policy

Acknowledged input is the only paired-device production path. Protocol mismatch
or an older client is read-only with an explicit update-required reason. Raw
binary input may exist only behind the explicit `explicit_local_dev` mode and
must be unreachable in normal paired production. No content sniffing, implicit
downgrade, hidden binary fallback, or permission bypass is allowed.

### 6.2 Strict request and result protocol

Introduce one versioned terminal-input control message. It contains a
client-generated `inputId`, session identity, exact generation, and bounded
base64 payload. Decode under a raw-frame limit, reject unknown fields and
trailing JSON, validate every field before decoding/writing, and return only
closed result values:

- `accepted`
- `permission_denied`
- `stale_generation`
- `session_not_found`
- `transport_closed`
- `write_failed`
- `invalid_request`
- `input_too_large`

At connection establishment the server generates a connection identity and
captures the exact session generation and `TerminalTransport` instance. Every
request and response is bound to connection identity, session identity,
generation, and `inputId`. The socket must never re-resolve a newer transport by
session ID after replacement.

`accepted` means every decoded byte was written to that captured transport. A
short/partial write is `write_failed`. Acceptance does not mean the shell,
command, provider, or workflow succeeded.

### 6.3 Frozen protocol bounds

These values reuse existing repository limits rather than introducing arbitrary
new scale:

| Bound | Frozen value | Existing derivation |
|---|---:|---|
| Maximum raw control frame | 8192 bytes | Existing `maxApprovalBodyBytes = 8 << 10`; holds a 4096-byte base64 payload and closed envelope |
| Maximum decoded input | 4096 bytes | Existing managed prompt, approval input, and delivery item bounds |
| `inputId` | exactly 64 lowercase hexadecimal characters | Existing 32-byte cryptographic challenge/ticket identifiers serialized as lowercase hex |
| Recent-result cache | 64 entries per connection | Existing bounded gate capacity and default global WS-ticket capacity |
| Recent-result TTL | 30 seconds, cleared earlier on connection close | Existing single-use WS-ticket TTL; retries are never automatic or cross-connection |
| Read-only denial limiter | per connection, burst 3 and refill 10/minute | Existing device-trust challenge limiter defaults |

The raw-frame check occurs before JSON allocation; decoded length is checked
after strict base64 decoding and before cache/write. Limits are protocol
constants covered by exact boundary tests. Oversized or malformed requests do
not enter the recent-result cache unless the closed protocol explicitly returns
a bounded correlated error.

### 6.4 Duplicate, ACK, and UI semantics

Maintain a bounded per-connection recent-result cache keyed by `inputId` plus a
digest of all execution-relevant request data. An identical duplicate returns
the stored terminal outcome without another write. Reusing the same `inputId`
with different session, generation, payload, or version is `invalid_request`
and never writes.

ACK loss means `delivery_unknown`. The client must not automatically retry,
replay after reconnect, or infer acceptance from socket send success. Preserve
the original input until the complete UI command operation is acknowledged.

Text and Enter remain two acknowledged writes separated by the unchanged 40 ms
delay but belong to one UI operation. Only two `accepted` results permit the
label **Delivered to terminal**. If either result fails or is unknown, retain
the original text and report possible partial delivery without claiming command
failure or success.

### 6.5 Mandatory tests

- Strict schema/version/unknown-field/trailing-data validation and every exact
  frame, decoded-byte, base64, and `inputId` boundary.
- Permission denied, stale generation, absent session, closed transport, short
  write, write error, malformed request, and oversized input each map to exactly
  one closed outcome with zero unintended write.
- Exact captured-transport positive proof and same-session replacement negative
  proof: an old socket never writes to or resolves the replacement.
- Full write is `accepted`; partial write is never accepted.
- Duplicate identity returns the stored result exactly once; conflicting reuse
  is invalid; cache capacity, TTL, eviction, and connection-close cleanup are
  deterministic and race-safe.
- ACK correlation rejects wrong connection/session/generation/input identity.
- ACK loss produces `delivery_unknown`, no automatic retry, and no reconnect
  replay.
- Two-write operation tests cover both accepted, first failed/unknown, and
  second failed/unknown, preserving text and reporting possible partial delivery.
- Production mode rejects legacy binary input as update-required/read-only;
  only `explicit_local_dev` permits it, with no hidden fallback.
- Mobile renders **Delivered to terminal** only after both accepted ACKs and
  never equates it with shell success.
- Focused race tests cover transport replacement, duplicate arrival, cache
  eviction, connection close, permission refresh, and concurrent ACK handling.

### 6.6 Stop conditions and evidence

Stop if the server re-resolves transport by session ID, accepts a partial write,
replays across a connection, permits production binary fallback, lets cache
eviction duplicate a write, trusts client generation without comparing the
captured generation, or labels socket send/shell outcome as delivery.

The implementation and evidence commits are separate. Evidence includes exact
protocol vectors, all outcome/limit/duplicate/replacement tests, focused and full
race gates, mobile tests, production compatibility negatives, diff scope,
ancestry, HEAD/upstream equality, and clean worktree. Independent Input-B ACCEPT
is required before PB evidence regeneration.

## 7. PB.6/PB.7 regeneration and candidate freeze

After Input-B ACCEPT, regenerate PB.6 and PB.7 evidence from the complete new
production tree. Do not edit or reinterpret historical evidence. Re-run daemon
build/vet/full race, mobile typecheck/tests/native gates, formatting and diff
checks, PB legacy zero-consumer/deletion scans, PA4 managed-isolation scans,
pairing security/decoding tests, Input-A permission tests, and Input-B protocol
tests.

Freeze the final production/mobile commit as `PB_DEVICE_CANDIDATE_SHA`. Commit
regenerated evidence separately as `PB_AUTOMATED_EVIDENCE_SHA`. Build both the
daemon and APK from clean checkouts of the exact production candidate, embed or
record that SHA in each artifact, verify hashes/signatures, and install only
those artifacts for the physical matrix.

## 8. Device-gate entry criteria

The SM-S926N gate may start only when all of the following are true:

- QR, Input-A, and Input-B each have exact independent ACCEPT identities.
- All regenerated PB.6/PB.7 automated gates pass at the documented production
  candidate and evidence head.
- Local/upstream ancestry is exact and the worktree is clean.
- Daemon and APK identities both equal `PB_DEVICE_CANDIDATE_SHA`.
- The SM-S926N is confirmed as the usable owner with server-issued
  `terminal:input`, or a separately accepted host-local recovery packet exists.
- Pairing uses the unchanged payload through the accepted renderer/security
  path, and terminal input uses the acknowledged protocol with no binary
  production fallback.
- `PB_ACCEPT_SHA` is still **UNSET**.

The matrix must exercise QR pairing, saved identity/cold start, authenticated
REST and WS, managed discovery, Codex/Claude lifecycle and approval,
Transcript/output/replay, exact-generation cleanup, owner acknowledged input,
member read-only output, and regression comparison for the pre-existing empty-
terminal/restart debt. Only after the matrix passes may an independent verifier
assign `PB_ACCEPT_SHA`.

## 9. Explicitly out of scope

- Pairing envelope compaction, protocol V2, CBOR, key shortening, trust-flow or
  token-semantics changes, and remote/tunnel pairing.
- Broadening member permissions, automatic owner promotion, remote
  self-promotion, or owner transfer without a separate recovery contract.
- Empty-terminal/bootstrap diagnosis, terminal restart redesign, shell/Codex
  Enter tuning, or changing the 40 ms split.
- Canonical Timeline, common provider abstraction, Grok/ACP,
  Navigator/Guard, or durable orchestration work.
- Reintroducing any PB-deleted adapter, Registry, discovery, observer, tmux,
  cmux, localpty, link, attach, or legacy mobile authority.
