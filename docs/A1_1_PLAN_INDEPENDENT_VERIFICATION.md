# A1.1 Codex Provider-Positive Path — Independent Plan Verification

Verdict: **ACCEPT — CP0 ONLY**

Historical note: CP0 live evidence at `042dddf` superseded the field-specific
assumptions that approval-request `environmentId` would be null and that proposal/
decision fields would be absent. `docs/A1_1_CP0_CHECKPOINT_REVIEW_1.md` records the
amended decision. The CP0-only authorization and all other boundaries remain valid.

Date: 2026-07-14

Reviewed baseline: `809b11b82805d55b45b1f04e343a4f82241cc42c`

Accepted R11 report ancestor: `997a697b55b421aa8590aa05a672c604762fd0d2`

Accepted S1.1 ancestor: `02c8385e3270fbbc4df45e0c71ccad6ebe11a076`

## 1. Repository and scope verification

- local HEAD equaled `origin/feature/phase10-multi-adapter` at review start;
- accepted R11 and S1.1 ancestry passed;
- worktree was clean;
- the reviewed change was documentation-only;
- CP0 and production provider work had not started;
- `provenActionMapping` remained empty and production delivery capacity remained
  zero.

The plan remains A1.1-only. It does not begin N1, A1.2 Claude, Task/Dispatch, O1/O2,
generic CLI redesign, terminal rendering, cloud relay or Windows work.

## 2. Provider-surface verification

The installed `codex-cli 0.144.1` was inspected without starting CP0. Its CLI and
generated non-experimental schema confirm:

- app-server is shipped but marked experimental;
- local stdio is an available transport;
- `item/commandExecution/requestApproval` carries an outer JSON-RPC request ID plus
  `threadId`, `turnId` and `itemId`;
- regular shell/unified-exec callbacks use `approvalId: null`;
- the narrow response decisions include `accept` and `decline`;
- `serverRequest/resolved` carries `requestId` and `threadId`.

Those structural facts make app-server stdio the smallest credible exact-version
candidate. They do not prove provider consumption or production support. CP0 must
still prove ordering, cancellation, response consumption, cleanup and the bounded
real product entry path.

WebSocket is available but experimental and remains excluded/uncertified. Codex
PermissionRequest hooks, ordinary TUI sessions and generic `pokit run codex` remain
non-actionable.

## 3. Final authority corrections

The independent review made the following final corrections directly in the
authoritative plan before recording ACCEPT.

### 3.1 Verified artifact must equal spawned process image

A digest/device/inode recheck immediately before path-based `exec` is not a
linearization point and does not close the replacement window. It is no longer an
accepted alternative.

CP0 must prove an OS-supported mechanism that binds the actual spawned process image
to the verified digest, such as execution from the same verified open file
description or equivalent post-spawn process-image attestation. Any proposed
`/dev/fd` or inherited-descriptor mechanism must be demonstrated on supported macOS
with a deterministic adversarial replacement. If different bytes can start, CP0 is
BLOCKED.

### 3.2 Environment identity cannot be ignored

`environmentId` changes where a command executes. The initial scope therefore
requires it to be null/absent. Supporting a non-null environment later requires a
separately reviewed fingerprint rule. Public redaction does not remove it from the
authority problem.

### 3.3 Best-effort command actions are not authority

The schema describes `commandActions` as best-effort parsed data for friendly
display. It is display/corroborating evidence only and is excluded from the native
authority fingerprint. The bounded non-empty command and normalized absolute cwd
are the initial execution-meaning inputs.

### 3.4 Accurate WebSocket classification

WebSocket is described as experimental, available in the exact binary and excluded/
uncertified for A1.1—not as unavailable or unsupported.

## 4. Accepted architecture

The provider-neutral A1 core remains frozen and authoritative for requester auth,
ApprovalID, RuntimeRef, selected-action digest, idempotency, atomic claim, expiry,
supersession, receipt comparison and final commit.

Provider-specific code is limited to:

- exact-version binary/schema/connection certification;
- bounded native request identity and registry;
- typed strict decoding and internal canonical fingerprinting;
- fixed allow-once/deny action mapping;
- one-response native delivery;
- proof that the exact provider request was resolved.

The delivery router defaults to unavailable. It may route only an exact server-
derived certified tuple to the Codex bridge, and route registration/replacement/
deletion must share the runtime-supersession linearization. Provider name or client
input alone never selects an actionable route.

## 5. CP0 authorization and hard gates

This verdict authorizes **CP0 only**. CP0 must begin with the executor contract note
and a filled field-level binding table. Production capacity remains zero.

CP0 must independently prove:

1. exact binary/process-image and deterministic schema-bundle identity;
2. strict request shape and the initial optional-field freeze;
3. matching `item/started` and request identity;
4. real allow-once and deny response shapes;
5. `serverRequest/resolved` ordering and provider-consumption meaning;
6. cancellation, duplicate response, timeout and child/orphan cleanup;
7. a bounded real production thread/turn entry path that does not become Task,
   Dispatch, terminal replacement or generic remote execution;
8. redacted reproducible evidence with no private prompt, token, path or repository
   data.

If any gate cannot be proven, the executor must report CP0 BLOCKED and stop. Fixture-
only evidence, queue admission, stdout write or green tests cannot substitute for
provider consumption.

## 6. Downstream boundary

- CP1 is not authorized by this verdict.
- A1.1, A1 and N1 remain BLOCKED during CP0.
- A1.2 Claude remains optional and outside this track.
- Capacity stays zero through CP4.
- Only a separately reviewed CP5 may activate the exact certified tuple after real
  production allow-once and deny E2E evidence.
