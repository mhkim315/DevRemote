# A1.1 Codex Provider-Positive Path Plan

Status: **INDEPENDENT PLAN ACCEPT — CP0 AUTHORIZED; PRODUCTION CAPACITY REMAINS ZERO**

Date: 2026-07-14

Revision: incorporates the four required plan changes from the ACCEPT-WITH-REQUIRED-
PLAN-CHANGES review — (1) app-server reclassified shipped-but-experimental / exact-
`0.144.1`-only (§1, §2); (2) TOCTOU-hardened binary + schema identity mechanism (§4.1,
CP0/CP1); (3) typed-parse JSON canonicalization that does not reject whitespace/key
order, plus the initial optional-field freeze (§6, CP2); (4) frozen provider delivery
routing boundary — default unavailable, server-derived certified-tuple routing only,
atomic with supersession (§3.1, CP3).

Independent final review additionally removed adjacent path re-verification as a
TOCTOU mitigation, froze `environmentId` to null/absent, classified best-effort
`commandActions` as non-authoritative, and corrected WebSocket from “unsupported” to
experimental/available-but-uncertified.

Repository: `https://github.com/mhkim315/DevRemote.git`

Branch: `feature/phase10-multi-adapter`

Canonical planning baseline: `997a697b55b421aa8590aa05a672c604762fd0d2`

Frozen provider-neutral implementation: `2e70512345718bef47e83a08b1ac8af5a6294aaf`

Accepted R11 test-evidence remediation: `d5a965cad4d9b499a5c3bc132be2aecd64f91e86`

Accepted S1.1 ancestor: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

Independent verification accepted R11 at canonical report HEAD `997a697b`. The
provider-neutral A1 safety core is therefore frozen as the foundation for this
track. The product A1 milestone is still BLOCKED because no production provider
positive path exists. A1.1 is the bounded completion track for the first such path;
it is not N1, O1 or a redesign of the accepted core.

No implementation packet may begin until this updated A1.1 plan receives an
independent plan ACCEPT. The execution handoff records the repository identity,
entry gate and packet stop rules.

## 1. Decision

Use the exact Codex CLI `0.144.1` **app-server v2 stdio JSON-RPC path** for the
first provider-positive A1 integration.

Do not use Codex `PermissionRequest` command hooks as the first positive path.
The hook is a real blocking allow/deny surface, but its documented input exposes
`session_id`, `turn_id`, `tool_name` and `tool_input` without a provider request or
tool invocation ID. The hook process also has no separate provider-consumption
acknowledgement after it writes stdout and exits. Those omissions prevent the
exact request ownership and consumption proof required by A1.

Do not use app-server WebSocket transport. It is experimental and available in the
`0.144.1` binary, but it is excluded and uncertified for A1.1. The initial certified
transport candidate is local stdio JSONL only. Do not use experimental app-server
request fields or decisions.

This is the smallest production-credible candidate, not a completed capability, and
the app-server surface is **shipped but experimental**: the `0.144.1` CLI itself marks
`app-server` as `[experimental]`. It is accepted here only as an exact-version
certification candidate, with no stability or backward/forward-compatibility guarantee.
No version other than `0.144.1` is automatically compatible; any other version is
non-actionable until separately certified. App-server supplies a provider-owned
JSON-RPC request ID, thread/turn/item identity, a response channel on the same
connection, and `serverRequest/resolved`. A controlled exact-version trace must still
prove the resolution semantics before capacity may become non-zero.

## 2. Evidence classification

| Surface | Classification | Confirmed evidence | A1 decision |
| --- | --- | --- | --- |
| Codex `PermissionRequest` hook | shipped, blocking; payload contract still incomplete for A1 | exact-version hook/schema exposes session/turn/tool fields and allow/deny response, but no external invocation ID or post-response acknowledgement | research/future only; non-actionable |
| app-server v2 stdio | shipped but EXPERIMENTAL protocol surface (exact-version certification candidate; no stability/compat guarantee; `0.144.1` CLI marks `app-server` `[experimental]`) | initialization, thread/turn/item events, approval server requests and matching response IDs | selected candidate — `0.144.1` only; other versions non-actionable |
| app-server WebSocket | experimental and available in the binary; uncertified for A1.1 | exact-version CLI help and official app-server README | excluded |
| `additionalPermissions`, `availableDecisions`, permission amendments | experimental | protocol annotations/schema | excluded |
| external peer attachment to an existing Codex TUI | unavailable/unverified | current TUI internally uses app-server, but no stable external peer-attach contract is documented | must not be claimed |
| existing POKIT `pokit run codex` | managed lifecycle only for this purpose | CLI joins argv into a command string and local create can enter the legacy shell-command path; it is not the certified app-server client | remains non-actionable |
| existing HTTP `codex` preset | recognized direct executable + launch binding | `term/profiles.go`, `term/create.go` | does not itself provide app-server approval delivery |

Pinned primary sources for the initial experiment:

- Codex tag `rust-v0.144.1`, commit
  `44918ea10c0f99151c6710411b4322c2f5c96bea`;
- [app-server protocol and transport README](https://github.com/openai/codex/blob/44918ea10c0f99151c6710411b4322c2f5c96bea/codex-rs/app-server/README.md);
- [server request and resolved notification definitions](https://github.com/openai/codex/blob/44918ea10c0f99151c6710411b4322c2f5c96bea/codex-rs/app-server-protocol/src/protocol/common.rs);
- [v2 command approval and item definitions](https://github.com/openai/codex/blob/44918ea10c0f99151c6710411b4322c2f5c96bea/codex-rs/app-server-protocol/src/protocol/v2/item.rs);
- [app-server response handling](https://github.com/openai/codex/blob/44918ea10c0f99151c6710411b4322c2f5c96bea/codex-rs/app-server/src/bespoke_event_handling.rs);
- [PermissionRequest input schema](https://github.com/openai/codex/blob/44918ea10c0f99151c6710411b4322c2f5c96bea/codex-rs/hooks/schema/generated/permission-request.command.input.schema.json);
- [PermissionRequest implementation](https://github.com/openai/codex/blob/44918ea10c0f99151c6710411b4322c2f5c96bea/codex-rs/hooks/src/events/permission_request.rs).

The implementation report must record retrieval date, installed binary digest,
`codex --version`, generated app-server schema digest and the controlled trace
provenance. A newer Codex release is not implicitly compatible.

## 3. Frozen authority boundary

The provider-neutral A1 core remains authoritative and is not redesigned:

- `AuthoritativeApprovalStore` owns authentication-derived requester binding,
  state, expiry, claim, retry and supersession;
- `ApprovalExecutionBinding` remains the immutable claim-to-receipt identity;
- `CanonicalAction.Digest()` remains the selected-action digest;
- `ApprovalDelivery` remains the delivery abstraction used by the handler;
- `RecordDelivery` remains the only successful commit boundary;
- S1/S1.1 `RuntimeRef` and launch/stream invalidation remain authoritative;
- public/mobile approval DTOs remain bounded structural allowlists.

Relevant production files are:

- `companion-daemon/internal/term/approval_execution.go`;
- `companion-daemon/internal/term/approval_store_gen.go`;
- `companion-daemon/internal/term/approval_delivery.go`;
- `companion-daemon/internal/term/approval_handler.go`;
- `companion-daemon/internal/term/approval_ingest.go`;
- `companion-daemon/internal/term/telemetry_service.go`.

Provider code is a thin certified boundary. It may receive and retain a bounded
native request, derive a safe A1 ingest item, and implement `ApprovalDelivery` for
that exact request. It must not mint requester authority, modify claims, bypass the
store, or commit success itself.

The existing generic `RuntimeDeliveryGate` remains disabled for provider-neutral
terminal delivery. A queue admission or process write is not Codex acceptance. A
Codex delivery implementation may reuse its validated binding rules, but must not
return `DeliveryAccepted` merely because an item entered that queue.

### 3.1 Delivery routing boundary (frozen before CP3)

Production currently wires exactly one `ApprovalDelivery` at
`companion-daemon/cmd/devremote/app.go:229` (`NewGatedApprovalDelivery`, capacity 0).
The Codex bridge must be introduced as a server-side ROUTER, not a global replacement:

- the **default path is always `NewUnavailableApprovalDelivery`** — every delivery is
  unavailable unless it is explicitly routed to the certified Codex bridge;
- **only the exactly-certified runtime tuple** (section 4) routes to the Codex bridge;
- the **routing key is derived by the server** from the stored `RuntimeRef` and the
  certification registry — never from a client- or provider-supplied string. A request
  claiming `provider == codex` without a matching certified live runtime routes to
  unavailable;
- every other runtime and every other provider keeps **capacity zero**;
- **bridge registration, replacement and deletion are atomic with runtime
  supersession**: a launch/stream/connection change or deletion removes the bridge
  route in the same linearization as the supersession, so a stale bridge can never
  receive or answer a request for a superseded runtime.

This routing layer is provider-neutral wiring; it does not add provider fields to the
frozen store/DTO contract.

## 4. Certified runtime, not provider-wide enablement

Certification is an exact tuple, not `provider == codex`:

```text
provider                 codex
provider version         0.144.1 exactly
binary identity          regular-file digest bound to filesystem identity (device+inode), recorded at launch
schema identity          per-file digests + deterministic manifest digest of the full generated schema bundle (generated WITHOUT --experimental)
transport                local stdio JSONL
launch profile           explicit codex_app_server managed profile
protocol mode            app-server v2; experimentalApi disabled
request kind             normal commandExecution requestApproval only
actions                  allow_once, deny
```

The daemon must resolve one executable, validate its exact version and schema, and
spawn that same executable directly. PATH lookup at certification and a different PATH
lookup at spawn are not sufficient identity.

### 4.1 Binary identity — TOCTOU-hardened (CP0/CP1 requirement)

A digest recorded at certification and a later `spawn` of a path do not by themselves
prove the spawned bytes equal the verified bytes: the file can be replaced between hash
and exec. CP0/CP1 must therefore:

- verify the resolved artifact is a **regular file, not a symlink** (reject symlinks and
  non-regular files);
- bind the recorded **binary digest to the file's filesystem identity** (device + inode)
  captured at verification;
- guarantee the **verified artifact is the spawned artifact**. A path check immediately
  before `exec` is NOT sufficient and is not an accepted alternative. CP0 must prove an
  OS-supported mechanism that executes the same verified open file description, or a
  post-spawn attestation of the actual process image that is cryptographically tied to
  the verified digest. If `/dev/fd`, an inherited descriptor or another platform
  primitive is proposed, CP0 must demonstrate it on the supported macOS runtime with a
  deterministic adversarial replacement between verification and spawn. If different
  bytes can start, or the actual process image cannot be bound to the certification
  digest, stop BLOCKED;
- generate the app-server schema **without `--experimental`**, compute a **per-file
  digest for every file in the generated bundle** and a **deterministic manifest digest**
  over the sorted (path, digest) list;
- include the full **binary/schema certification tuple in the launch binding and the
  app-server connection epoch**, so any binary/schema/version drift or reconnection
  revokes certification (see the revocation list below).

### 4.2 Certification lifetime

An arbitrary command, the current legacy local `pokit run codex`, a regular Codex TUI
profile, an attached session, a native-log-only adapter or a heuristic signal never
inherits this certification. Certification is current-runtime state and is revoked on
process replacement, app-server connection replacement, schema/version mismatch,
launch/stream change, correlation loss, deletion or termination.

Production capability/capacity remains zero until every exit gate in section 11
passes against this exact tuple. Activation is a separate focused change after the
evidence is reviewed.

## 5. Initial native request and actions

### 5.1 Included request

Only stable `item/commandExecution/requestApproval` requests are included, with:

- a valid outer JSON-RPC request ID;
- non-empty `threadId`, `turnId` and `itemId`;
- `approvalId == null` (exclude zsh-exec-bridge subcommand callbacks initially);
- a matching command-execution `item/started` observed on the same connection,
  thread, turn and item;
- bounded, valid command/cwd fields according to the pinned schema;
- no experimental permission amendment or decision fields.

File changes, network-only permission requests, `request_permissions`, MCP
elicitation, session-wide approvals and all unknown request types remain
non-actionable.

### 5.2 Included actions

Only two Pokit-owned options exist:

| Pokit option | app-server response | Meaning |
| --- | --- | --- |
| `codex.command_execution.allow_once.v1` | `decision: accept` | allow this one native request |
| `codex.command_execution.deny.v1` | `decision: decline` | deny this one native request |

`acceptForSession`, `cancel`, policy amendments, network amendments and synthetic
terminal input are excluded.

### 5.3 Native request identity

The provider-specific immutable native key is:

```text
Pokit SessionID
+ RuntimeRef.Adapter/Version/LaunchGen/StreamGen
+ provider = codex
+ binary/schema certification ID
+ app-server connection epoch
+ JSON-RPC request ID and method
+ threadId
+ turnId
+ itemId
+ approvalId (explicit null in the first scope)
```

`startedAtMs` is retained as bounded replay/sanity evidence but is not unique
authority. Because app-server supplies a request ID, no invented invocation nonce
is needed. A missing, repeated-with-different-content or malformed request ID fails
closed. Connection epoch plus RuntimeRef prevents reuse across restarts.

The A1 `ApprovalID` is a domain-separated digest of this native key plus the
canonical native request fingerprint. The full native key is stored only in the
bounded provider registry. This makes the existing `ApprovalExecutionBinding` bind
the exact provider request through its `ApprovalID` without adding provider fields
to the frozen public/core contract.

## 6. Canonicalization and privacy

Canonicalization has two distinct products:

1. **Native request fingerprint**, internal only. It is computed from a TYPED parse,
   never from the raw JSON bytes. The daemon must:
   - parse the request into typed Go structures with a strict decoder that **rejects
     duplicate keys and unknown fields**;
   - **validate types, UTF-8, integer-vs-float and length bounds** for every retained
     field (reject floats where integers are required, over-bound values, invalid
     UTF-8);
   - build a Pokit **canonical representation** from the typed values (length-framing
     the method, request-ID type/value, thread/turn/item IDs, explicit null approval
     ID, command, cwd and stable command-action fields), and digest THAT.

   Normal insignificant whitespace and JSON object key-ordering differences are NOT
   grounds for rejection, and the **original JSON serialization is never used as
   authority or as digest input** — only the typed, canonical representation is. Do not
   hash arbitrary display text.

   Initial command-approval optional-field freeze (any deviation is non-actionable):
   - `approvalId`: MUST be `null`;
   - network context and policy amendments: `null`/absent;
   - any unsupported semantic field: `null`/absent;
   - `command`: null/empty is rejected (a command approval must carry a bounded
     non-empty command);
   - `cwd`: null/empty is rejected for the initial scope; it must be a bounded,
     normalized absolute path and is included in the internal authority fingerprint
     while remaining redacted from public display;
   - `environmentId`: MUST be `null`/absent in the initial scope. A later non-null
     environment changes execution meaning and requires a separately reviewed
     fingerprint rule; it must never be silently ignored;
   - field roles are fixed — `command` and `cwd` are **authority + fingerprint**;
     `reason` and other free-form explanatory text are **display-only** and excluded
     from both fingerprints; `commandActions` is explicitly **best-effort display /
     corroborating evidence only**, is excluded from authority and the fingerprint,
     and cannot create or distinguish an actionable approval.
2. **Selected action digest**, the existing `CanonicalAction.Digest()`. Use the
   fixed Pokit option ID, `a1.action.v1`, a fixed kind such as
   `provider_approval_decision`, and no free-form input. Allow and deny have
   different option IDs and therefore different digests.

The provider delivery payload is generated server-side from the immutable native
registry and selected action:

```json
{"id": "<original request id>", "result": {"decision": "accept|decline"}}
```

It is never accepted from the mobile client and never placed in a public DTO.

The public summary is Pokit-owned, bounded and redacted. It may show a fixed request
kind and a short command summary after control stripping, token/path redaction and
UTF-8 byte bounding. It must not expose raw command arrays, cwd, full provider
payload, terminal text, absolute paths, secrets or arbitrary provider labels. The
display summary is never authority and is excluded from both fingerprints.

## 7. Exact lifecycle and linearization

```text
app-server request received on certified connection
→ validate schema, native identity and matching item/started
→ atomically register one pending native request
→ ingest one actionable A1 approval bound to current RuntimeRef
→ authenticated mobile decision
→ A1 ClaimForExecution (existing atomic authority boundary)
→ provider delivery reserves that exact native request for the claim token
→ revalidate RuntimeRef, certification, connection epoch and request pending state
→ write exact JSON-RPC response on the owning stdio connection
→ observe matching serverRequest/resolved on that same connection epoch
→ return fully bound DeliveryReceipt
→ A1 RecordDelivery commits delivered/resolved
```

Provider delivery has one per-request state machine:

```text
pending → response_reserved → response_written → provider_resolved
pending/response_reserved/response_written → cancelled | stale | timed_out | ambiguous
```

The reservation is the provider-specific linearization point for competing mobile
decisions and provider cancellation. The response writer is single-owner and
ordered with all app-server messages. No lock is held while waiting for the child
process or mobile network.

`serverRequest/resolved` counts only when it follows POKIT's exact response write
for the same JSON-RPC ID on the same certified connection epoch. A resolved event
that arrives first is provider-side cancellation/resolution, not success. An
ambiguous race fails closed. The controlled exact-version experiment must prove
this ordering and resulting Codex behavior before activation.

`item/completed` and `turn/completed` are retained as corroborating trace evidence,
not as A1 action-success authority. A1 proves provider response acceptance, not
that the command succeeded; worker/action completion belongs to O1.

## 8. Failure and recovery policy

- **Mobile duplicate/retry:** existing same-key/same-digest behavior applies.
  Different digest is conflict. The provider bridge accepts at most one response.
- **Provider cancellation/resolution first:** invalidate/cancel the A1 approval;
  send no response and commit no successful receipt.
- **Runtime, process, launch, stream or connection replacement:** supersede the
  native request and A1 approval. Old responses cannot be written on the new
  connection.
- **Timeout before response reservation:** expire/cancel and send nothing. Do not
  synthesize a provider decision.
- **Timeout after write but before matching resolved:** outcome is ambiguous. Mark
  non-success, disable the certified runtime and terminate/close the app-server
  boundary. Never automatically retransmit.
- **Daemon restart:** restore no claims, native requests or actionable approvals.
  The managed child and stdio pipes must be proven dead, or startup cleanup must
  kill the orphan before new certification. No response replay.
- **Codex restart/exit:** close the connection epoch, supersede all native requests,
  invalidate A1 records and accept no further delivery.
- **Unsupported version/schema/action:** non-actionable information at most; no CTA,
  claim, response or receipt.
- **Capacity exhaustion:** reject the native request from actionable ingestion,
  disable certification if required to preserve protocol safety, and fail closed.

## 9. Production path boundary

Add an explicit recognized managed `codex_app_server` profile/runtime. It is a
provider-protocol runtime, not a terminal UI and not an external attachment to an
existing Codex TUI. Its stdin/stdout are JSON-RPC pipes and must not be routed
through Recorder, PTY projection, terminal echo handling or generic send-text/key
paths. stderr may enter bounded redacted diagnostics only.

The first implementation slice must answer a blocking product-path question:

> Can a real user start/resume a Codex thread and turn through a narrow production
> POKIT app-server path without building a new terminal renderer or a generic task
> API?

`thread/start`/`thread/resume` and `turn/start` are required to cause a real approval
request. A test-only driver is insufficient. The narrowest authenticated,
provider-specific production entry may be added only if it is limited to creating
the controlled certification turn; it must not become Task/Dispatch, generic remote
execution, O1 worker protocol or a replacement terminal. If no such bounded user
path is defensible, stop this work **BLOCKED** before exposing actionable approval.

Do not silently replace the existing `codex` TUI profile or claim that generic
`pokit run codex` has this capability. Existing terminal behavior remains intact.

## 10. Implementation packets

No intermediate packet receives acceptance. Every packet begins with the permanent
executor contract note/binding table and ends with focused tests and a checkpoint
commit.

### CP0 — evidence freeze and production-path spike

- Entry: independent R11 ACCEPT recorded; frozen provider-neutral core ancestry;
  independent A1.1 plan ACCEPT recorded.
- Pin exact binary/source/schema and record classification, including that app-server
  is shipped-but-experimental and `0.144.1`-only (section 1/2).
- Verify binary identity per section 4.1: regular file (not symlink), digest bound to
  device+inode, and a spawn method that guarantees the verified artifact is the spawned
  artifact. Generate the schema WITHOUT `--experimental` and record per-file digests +
  the deterministic manifest digest.
- Run controlled redacted traces for allow, deny, cancel, timeout and duplicate
  response using a real `codex app-server` child.
- Prove `serverRequest/resolved` ordering and consumption semantics.
- Confirm the initial command-approval optional-field freeze holds in real traces
  (`approvalId == null`; `environmentId == null`/absent; no network/policy amendment
  or unsupported semantic fields; `commandActions` remains non-authoritative).
- Prove a bounded real production path can start/resume a thread and start a turn.
- Produce fixtures without prompts, secrets, repository paths or personal data.

Stop BLOCKED if request identity, response resolution, child cleanup or the real
production turn path cannot be proven. CP0 changes no production capacity.

### CP1 — managed app-server runtime, capacity zero

- Directly spawn the certified executable with stdio pipes using the section 4.1
  TOCTOU-hardened, verified-equals-spawned artifact method.
- Implement bounded strict JSONL framing, one reader, one ordered writer,
  initialize/initialized handshake and connection epoch.
- Bind SessionID, RuntimeRef, PID/start identity, and the binary/schema certification
  tuple (section 4.1) into the launch binding and connection epoch.
- Keep the existing Recorder/PTy paths untouched.

### CP2 — Codex request registry and safe ingestion, capacity zero

- Implement only the request/action scope in section 5.
- Add bounded immutable native request storage and exact duplicate/conflict rules.
- Derive ApprovalID, internal fingerprint (typed parse per section 6: reject duplicate
  keys/unknown fields, validate types/UTF-8/length, canonicalize from typed values, no
  raw-JSON digest input) and safe DTO summary.
- Feed the existing authoritative A1 store without bypassing its capability,
  provenance, generation or expiry checks.
- Unknown/unsupported requests remain non-actionable.

### CP3 — Codex delivery and resolution bridge, capacity zero

- Implement the section 3.1 delivery ROUTER: default `Unavailable`; route to the Codex
  bridge only for the exact server-derived certified runtime tuple; bridge
  register/replace/delete atomic with runtime supersession.
- Implement the existing `ApprovalDelivery` contract for an exact native request.
- Reserve, revalidate, write and resolve according to section 7.
- Return accepted/already-accepted only with matching provider resolution evidence.
- Preserve the current A1 receipt/commit comparison; no fake receipt or queue-only
  success.
- Prove cancellation/replacement/timeout interleavings with deterministic barriers.

### CP4 — authenticated product integration, capacity zero

- Wire the existing paired-device mobile resolve flow to the real Codex request.
- Exercise the actual managed app-server creation/turn path, not a helper-only path.
- Verify duplicate taps, retries, stale runtime, session deletion and daemon/Codex
  restart behavior.
- Keep generic terminal input and existing TUI profile outside the authority path.

### CP5 — exact-tuple certification and activation

- Run the complete acceptance matrix in section 11 on the final frozen tree.
- Independently review the controlled provider traces and source/schema evidence.
- In a separate focused commit, enable non-zero actionable capacity only for the
  exact certified tuple.
- Run the entire gate again on the exact report HEAD and request independent A1
  verification. N1 remains blocked until A1 ACCEPT.

## 11. Mandatory acceptance evidence

### 11.1 Real positive paths

Using the actual installed certified Codex process and supported POKIT-managed
profile:

1. launch and establish exact runtime/process/connection identity;
2. start a real thread/turn through the production path;
3. receive a real command approval request;
4. create one actionable A1 approval;
5. decide allow-once from a paired mobile device;
6. atomically claim, deliver the exact response, observe matching provider
   resolution and commit one bound receipt;
7. repeat with deny and prove the exact invocation is declined;
8. prove duplicate mobile tap does not send a second provider response.

Fixture-only helpers, mocked providers and manually injected requests do not satisfy
this gate.

### 11.2 Negative and race evidence

At minimum:

- wrong binary/version/schema/profile/transport;
- arbitrary `pokit run` and regular Codex TUI remain non-actionable;
- missing or conflicting JSON-RPC/thread/turn/item identity;
- duplicate request ID with same and different content;
- unsupported request kind, approvalId, action or experimental field;
- stale launch/stream/connection generation;
- cross-session and cross-thread response;
- modified command, cwd, native fingerprint, action digest or response decision;
- concurrent allow/deny and provider cancellation;
- duplicate response and same-key/different-digest mobile retry;
- timeout before reservation and ambiguous timeout after write;
- runtime/Codex replacement between claim, write and resolved;
- session delete/unlink/terminate and daemon restart/orphan cleanup;
- capacity exhaustion at request, writer and provider-registry bounds;
- resolved-before-write produces no success;
- response write without matching resolved produces no success;
- terminal text, prompt, screen, waiting status and generic input create no action;
- raw command/cwd/payload/token/path/claim/digest leakage tests;
- race-enabled non-vacuous tests with deterministic barriers and known-bad controls.

### 11.3 Final gates

- backend format, build, vet, full `-race`, fixed T0/T1/T2/S1/S1.1/A1 regressions;
- mobile TypeScript and full Jest;
- Android/native gate when available, otherwise a formally recorded environmental
  block without claiming pass;
- invariants, secret scan and `git diff --check`;
- final HEAD frozen before the authoritative gate;
- clean worktree, local/remote equality, accepted R11/S1.1 ancestry;
- independent provider-trace and A1 verification.

## 12. Optional A1.2 Claude extension review

Do not implement Claude in A1.1. If a separately reviewed Claude expansion is later
authorized, name that track **A1.2 Claude Approval Extension**. A1.2 is optional and
is not automatically an N1 prerequisite after A1.1 receives independent acceptance.

Claude's documented `PermissionRequest` hook can provide a session ID, tool name,
bounded tool input and blocking allow/deny response. Its command-hook input does not
provide `tool_use_id`, and writing hook output does not by itself prove that the
specific Claude invocation consumed the decision. Therefore Claude is not yet
certifiable from the command hook alone.

The Codex design leaves a clean extension seam without a generic plugin SDK:

| Provider-neutral | Provider-specific |
| --- | --- |
| A1 store, requester auth, RuntimeRef, canonical selected-action digest, claim, idempotency, receipt, commit, expiry, supersession, public DTO policy | native request key, schema/version certification, request fingerprint, safe summary, provider response payload, cancellation/resolution proof |

Only extract a small internal native-request carrier after Codex proves its concrete
shape and Claude evidence proves a second compatible need. A future Claude boundary
must supply an exact invocation identity (possibly a daemon-issued nonce bound to one
blocking process), a one-response channel, timeout/cancellation semantics and proof
of consumption. If those cannot be proven, Claude remains non-actionable without
changing the provider-neutral A1 core.

Official Claude reference:
[Hooks reference](https://code.claude.com/docs/en/hooks).

## 13. Explicit exclusions

- no generic provider/plugin SDK;
- no T0 contract change or accepted A1 core redesign;
- no generic `pokit run` or CLI redesign;
- no terminal renderer, PTY prompt matching, send-text or send-key approval;
- no external attachment claim for an existing Codex TUI;
- no file-change/network/MCP/session-wide approval in the first scope;
- no N1, Task, Dispatch, worker acknowledgement/completion, O1 or O2;
- no automatic approval policy;
- no WebSocket/cloud relay/remote app-server transport;
- no Claude implementation;
- no ConPTY/Windows/SSH expansion.

## 14. Exit decision

The implementation boundary is **INDEPENDENTLY ACCEPTED FOR CP0 ONLY**:

1. R11 and the provider-neutral safety foundation remain frozen ancestors;
2. CP0 must begin with its contract note and filled binding table;
3. CP0 must prove exact app-server request/response resolution and a bounded real
   production turn path;
4. capability remains zero through CP4;
5. A1 remains BLOCKED until CP5 real allow and deny E2E are independently verified.

If any condition fails, retain the provider-neutral safety core and non-actionable
display, record the provider surface as unsupported, and stop. Do not weaken A1's
acceptance contract.
