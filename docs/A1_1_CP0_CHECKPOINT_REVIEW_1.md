# A1.1 CP0 — Independent Checkpoint Review 1

Verdict: **CONTINUE CP0 UNDER AMENDED PLAN — CP0 NOT COMPLETE; CP1 NOT AUTHORIZED**

Date: 2026-07-14

Reviewed HEAD: `042dddf3153d8b9c6ab89ee84d1544728281b2a5`

Plan-accept ancestor: `cc7ff7e2508d7738955e7346dfcc73aa4df77835`

## 1. Repository and scope

- local HEAD equaled `origin/feature/phase10-multi-adapter`;
- worktree was clean;
- the checkpoint added contract/evidence documents only;
- production code, `provenActionMapping` and delivery capacity were unchanged;
- A1.1, A1 and N1 remain BLOCKED;
- CP1 is not authorized.

Committed JSON/JSONL parsed successfully, `git diff --check` passed and the focused
secret scan found no credential-shaped content.

## 2. Accepted CP0 progress

The checkpoint provides credible exact-version progress:

- installed/authenticated `codex-cli 0.144.1` app-server lifecycle;
- discovery of the Node → JS shim → vendored arm64 native launch chain;
- generation of a non-experimental 267-file schema bundle and aggregate manifest
  digest;
- live command approval requests with provider request/thread/turn/item identity;
- real `accept` and `decline` responses followed by matching
  `serverRequest/resolved` notifications;
- correct continued capacity-zero/non-actionable production state.

These observations justify continuing CP0. They do not independently complete its
identity, ordering, cleanup or product-entry gates.

## 3. Authoritative field decisions

Live evidence corrects the initial request-field assumptions.

### 3.1 Environment

Approval requests must carry `environmentId == "local"` exactly for the initial
certified tuple. The value is included in the native authority fingerprint and
certification binding. Null or any other request value is non-actionable.
Initialize-time `environmentId:null` is a separate protocol phase.

### 3.2 Available decisions

`availableDecisions` is present in the non-experimental schema/request and is no
longer classified as experimental. It is strictly decoded, bounded and included in
the request fingerprint for substitution/conflict detection, but never creates Pokit
actions or authorization.

### 3.3 Proposed exec-policy amendment

The committed requests also contain non-null `proposedExecpolicyAmendment`; the CP0
report did not list this third plan conflict. Its presence is admitted only as
bounded internal request data and included in the request fingerprint. It is never
public, logged or mapped to an action. `acceptWithExecpolicyAmendment` remains
excluded; the only Pokit actions are fixed `accept` and `decline`.

`commandActions` remains best-effort display/corroboration and is not authority.

## 4. Evidence claims that remain unproven

### 4.1 Ordered provider lifecycle

The committed accept/decline files are hand-structured summaries. They do not carry
wire direction or monotonic sequence and omit `item/started`. Therefore gate 3 and
the after-write ordering portion of gate 5 are not independently proven by the
committed artifacts.

The next evidence must contain a redacted ordered trace with:

```text
monotonic sequence
provider→daemon or daemon→provider direction
message type
bounded canonical payload
```

and cover:

```text
item/started
→ requestApproval
→ exact response write
→ serverRequest/resolved
→ corroborating provider outcome
```

For accept, prove the exact invocation proceeded. For decline, prove it did not.
`serverRequest/resolved` remains the proposed A1 receipt authority; the outcome is
CP0 corroboration of its consumption meaning.

### 4.2 Schema identity

Only the aggregate schema manifest digest is committed. Add the sorted relative-path
and per-file digest manifest that produces it. Do not commit the generated schema
bundle if the manifest is sufficient and reproducible.

### 4.3 Complete launch-chain/process-image identity

The Node executable identity is absent. Either attest Node + JS shim + vendored native
and the actual spawned image, or prove that direct vendored-native launch preserves
the same protocol/auth behavior. Path/shim/native digests alone do not prove the
running process image.

## 5. Platform-neutral certification boundary

The initial certification target may be `darwin/arm64`, but the provider-neutral and
Codex protocol contracts must expose only:

- OS and architecture;
- attestor kind/version;
- opaque bounded artifact identity;
- opaque bounded process-image attestation;
- certification result and bounded reason;
- opaque process/lifecycle and ordered transport results.

macOS inode, file descriptors, `/dev/fd`, Mach-O, code signing, path, signals and IPC
remain inside `ProcessImageAttestor`, `ManagedRuntimeLauncher`,
`ProviderPathCanonicalizer` and `ProviderTransport` implementations. Windows is not
implemented in A1.1, but must be certifiable through the same observable interfaces
with a separate OS/architecture tuple.

## 6. Remaining CP0 gates

The executor may continue CP0 only after syncing the amended plan. It must still
prove:

1. actual process-image identity with a deterministic adversarial replacement;
2. ordered item/request/write/resolved/outcome evidence;
3. cancellation, duplicate-response and timeout behavior;
4. child/orphan cleanup and restart behavior;
5. the bounded production thread/turn entry path without Task/Dispatch or terminal
   replacement;
6. per-file schema manifest and complete launch-chain identity.

Failure of any hard gate yields an honest CP0 BLOCKED result. Capacity remains zero.

## 7. Harness permission boundary

Do not grant a broad Bash permission. The executor may propose one exact, reviewable
CP0 harness command/prefix with a dedicated temporary directory, fixed harmless
command and named output files. It must pass no credential or user prompt in argv,
must not authorize arbitrary shell evaluation, and must clean only artifacts it
created. Configuration requires separate user approval after the exact rule is shown.
