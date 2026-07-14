# Next Session Handoff — A1 Approval Safety Remediation

Status: **SUPERSEDED — B1-B8 REVIEWED; USE REMEDIATION 2 HANDOFF**

The implementation produced from this handoff was independently rejected at
`ed466094cd7a38d148d845d7057c63fe2019e1c9`. Continue only from
`docs/NEXT_SESSION_A1_APPROVAL_SAFETY_REMEDIATION_2_HANDOFF.md` after reading
`docs/A1_APPROVAL_SAFETY_REVERIFICATION.md`.

This handoff targets a fresh Claude Code execution session backed by DeepSeek V4
Pro. Preserve all accepted A1 work and fix only the independent verification
findings. Do not begin N1/O1/O2 or unrelated platform/product work.

## 0. Canonical baseline

```text
remote: https://github.com/mhkim315/DevRemote.git
branch: feature/phase10-multi-adapter
rejected A1 implementation: 3b56c4f05d16653b5fd9c494d8f1682135701603
rejected report HEAD: 706750ec46ddc76fbabf15c66013e2e920505513
accepted S1.1 ancestor: 02c8385e3270fbbc4df45e0c71ccad6ebe11a076
```

Fetch and fast-forward first. Read, in order:

1. `docs/EXECUTION_AGENT_TASK_PACKET_PROTOCOL.md`;
2. `docs/A1_APPROVAL_SAFETY_PLAN.md`;
3. `docs/A1_APPROVAL_SAFETY_VERIFICATION.md`;
4. this handoff;
5. the rejected A1 audit/report and touched production code/tests.

Do not rewrite or squash reviewed commits. Add narrow remediation commits and a
new report marker.

## 1. Preserve these accepted properties

- legacy parser/status/PTY/prompt/process/CWD cannot create approval authority;
- only accepted correlated adapter evidence through capability,
  `DetectApproval`, and `SafeApprovalGate` may enter the approval boundary;
- Claude/no-capability remains zero-actionable;
- stale ingestion generations remain rejected;
- request decoding remains bounded/strict;
- paired-device host binding remains fail closed;
- no blind Y/N synthesis;
- no production/mobile DTO regression outside A1.

## 2. Remediation packets

Execute sequentially. Each packet gets focused tests and a narrow commit. Request
independent review only after all packets and the final stable gate pass.

### R-A — full-binding atomic claim and canonical action

Replace `Reserve(sessionID, approvalID)` as execution authority with one
linearizable `ClaimForExecution(ClaimRequest)` that atomically validates the full
binding frozen in the plan. It returns an opaque, unforgeable claim token and one
exclusive owner.

Freeze a versioned canonical selected action. Its ActionDigest includes option ID,
schema version, normalized arguments/input, input type/placement, and every
delivery-semantic field. The digest is not the digest of the whole option list.

Bind the server-derived DeviceID, HostID, BearerSessionID, boot/auth context,
permission set, and idempotency key during claim. Reject client identity/runtime
authority fields. Prove approve-vs-reject, duplicate, expiry, substitution,
cross-session, stale launch/stream, adapter/provider/version mismatch, and
same-key/different-digest races.

The claim must be linearizable against S1.1 launch replacement and A1 stream/
correlation/delete/unlink/termination invalidation. Do not add a check-before-lock
window or a competing generation model.

### R-B — approval-specific delivery receipt and lifecycle races

Introduce the dedicated approval delivery operation from the plan. It consumes
the exact claim token/runtime/action digest/idempotency key and returns a bound
closed receipt: `accepted`, `already_accepted`, `stale_runtime`,
`runtime_mismatch`, `unavailable`, `conflict`, or `rejected`.

`CommandBroker.Put` and every generic overwrite queue are prohibited as approval
delivery authority. Success commit consumes the exact claim token and matching
receipt; failed/missing commit is never HTTP success. Replacement, stream change,
correlation loss, delete, unlink, or termination after claim must yield a
non-success receipt and no wrong-runtime delivery.

Use deterministic barriers/hooks, not sleeps, to prove contested state. Same key
and digest returns `already_accepted`; same key with changed digest returns
`conflict`; non-idempotent delivery is never automatically retransmitted.

### R-C — honest provider actionability and safe DTO

Do not expose Codex action buttons merely because an approval event exists. A
positive action requires controlled redacted evidence for the exact provider
resolution channel and its daemon delivery receipt. If unavailable, preserve the
event only as non-actionable intervention information and make claim unavailable.
Do not locally commit reject as if the agent received it.

Replace the public/mobile DTO with the frozen structural allowlist. Remove raw
prompt, option/delivery payload, full command, arbitrary provider label/
placeholder, source payload, secret/path, internal claim token, and digest source
material. Use Pokit-owned/verified bounded summary and labels, explicit UTF-8 byte
bounds, versioned closed vocabularies, and unknown-field rejection on backend and
mobile.

Audit records use bounded safe IDs or digests and outcome codes only; untrusted
client/provider strings cannot forge logs.

### R-D — authenticated mobile path and complete regression gate

Send an idempotency key and strictly decode the closed receipt/result. The remote
approval write fails closed when host-bound device auth is absent; it does not
fall back to a legacy bearer. Preserve user input on safe failure and preserve the
original key/digest for explicit manual retry.

Complete every item in the authoritative 25-item acceptance gate, including a
real positive accepted-provider production path. If no accepted provider action
delivery channel exists, stop and report that blocker rather than fabricating a
mapping or claiming A1 complete.

## 3. Prohibited shortcuts

- no separate `Lookup -> validate -> Reserve` authority path;
- no option-list digest in place of selected ActionDigest;
- no public/client claim token or client-supplied identity authority;
- no generic command queue receipt, enqueue-as-success, local-only reject-as-agent-
  resolution, blind Y/N, or automatic non-idempotent retry;
- no raw prompt/provider payload in the public DTO;
- no test-only fake positive used to claim a production provider path;
- no N1/O1/O2, automatic policy, generic command execution, CLI redesign,
  ConPTY/Windows, cloud relay, lock-screen actions, or workflow work.

## 4. Final gate and stop

Run focused tests after each packet, then on one frozen final tree:

```sh
cd companion-daemon
gofmt -l <touched-go-files>
go build ./...
go vet ./...
go test -race ./internal/agent/... ./internal/term/... ./internal/transcript/... -count=1
cd ..
sh scripts/build-gate.sh
git diff --check
```

The implementation report must map B1-B8 and all 25 acceptance items to exact
production tests, state any honest environmental skip, and include:

```text
REVIEW REQUEST: A1 Approval Safety remediation — <full implementation SHA>
```

Commit and push, verify canonical local/remote equality, accepted S1.1 ancestry,
and clean worktree, then stop. Do not start N1.
