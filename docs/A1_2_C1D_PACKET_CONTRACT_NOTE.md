# A1.2 C1D Packet Contract Note — Managed Claude Observation

Status: **PRE-IMPLEMENTATION — C1D ONLY — ZERO ACTIONABILITY**

Canonical plan: `docs/A1_2_CLAUDE_APPROVAL_EXTENSION_PLAN.md` section 7.
Accepted C0D evidence: `e42d4c570e64462ce813861017cc635e338e68bf`.
Frozen A1/SP1 baseline: `2b940a6fce6e878ffa0da17b5df4d39438af144d`.

## 1. Authority ownership

The provider-neutral A1 core (`AuthoritativeApprovalStore`) is the sole owner of
ApprovalID, requester context, canonical ActionDigest, idempotency, atomic claim,
delivery receipt, commit, expiry and supersession. C1D does not grant any of
these authorities to Claude-specific code.

Claude-specific production code (all new files, no Codex modification) owns only:

| Concern | Owner | C1D scope |
| --- | --- | --- |
| Direct launch | `ManagedClaudeService` | Pinned 2.1.209 executable, direct argv, own process group |
| Version certification | OS-neutral launcher/attestor seam | PATH lookup alone is not certification |
| Hook bridge | Daemon-owned private local HTTP endpoint | Unguessable per-runtime capability token |
| Structured observation | Exact `PreToolUse` field decode | Strict allowlist, closed vocabulary |
| Defer join | Hook observation → `tool_deferred` result | Exact session ID, tool_use_id, tool_name, input digest |
| Non-actionable record | Bounded private pending observation | Zero options, zero ClaimForExecution, zero mobile CTA |

## 2. Canonical stored inputs

Every observation is bound to ALL of:

| Field | Source | Canonical authority |
| --- | --- | --- |
| `sessionID` | Daemon-generated `claude_headless:<local-id>` | `ManagedSessionRegistry` |
| `epoch` | `ManagedClaudeService.gen` at create time | Monotonic launch generation |
| `adapter` | Constant `claude_headless` | Never registered in `mux.Registry` |
| `version` | Constant `2.1.209` | Pinned certified authority version |
| `claudeSessionID` | Provider `tool_deferred.session_id` | Exact structured provider result |
| `toolUseID` | `PreToolUse.tool_use_id` = `deferred_tool_use.id` | Exact match across hook + deferred result |
| `toolName` | `PreToolUse.tool_name` | Strict bounded allowlist |
| `inputDigest` | SHA-256 of canonical `PreToolUse.tool_input` bytes | Computed once at observation; raw input never stored |
| `observedAt` | `time.Now()` at join completion | Daemon wall clock |

No field is optional. Missing or mismatched any field → zero state.

## 3. Binding table

| Operation | Storage | Comparison | Invalidation | Test |
| --- | --- | --- | --- | --- |
| Create session | `ManagedSessionRegistry.Register` | Epoch monotonic | `MarkExited` on child exit | Epoch uniqueness, duplicate reject |
| Launch certification | `OSAttestor.Verify(version, artifact)` | Exact version + platform tuple | N/A (fail-closed at create) | Wrong version, missing binary, platform mismatch |
| Start hook bridge | Private `http.Server` on random port | Unguessable per-runtime token in CLI arg | Server close on exit/stop/delete | Missing token, wrong token, duplicate token |
| Hook observation | `pendingClaudeObservation` struct (private, runtime-owned) | Session + tool_use_id + tool_name + input digest + epoch | Timeout, child exit, stop/delete, epoch replacement, turn completion | Mismatched any field → no join |
| Deferred join | Compare hook fields vs `tool_deferred` result | Exact equality of all binding fields | Same as observation | Mismatched result, duplicate ID, multi-tool |
| Non-actionable record | `ApprovalStore.IngestObserved` with `Actionable: false`, zero `Options`, zero `DeliveryMaterial` | Standard A1 admission rules | `InvalidateRecord` on timeout/exit/stop | Record exists, zero options, zero CTA |

## 4. State transitions

```text
IDLE
  │
  ├─ CreateDetached("claude")
  │   ├─ [certify version] ──FAIL→ rolled back (no visible session)
  │   ├─ [launch child] ──FAIL→ rolled back
  │   ├─ [start hook bridge] ──FAIL→ kill child + rolled back
  │   └─ [register + start pump] → RUNNING
  │
RUNNING
  │
  ├─ Hook receives PreToolUse
  │   ├─ [strict decode] ──FAIL→ defer (fail-closed, no observation)
  │   ├─ [field validation] ──FAIL→ defer
  │   └─ [store pending observation] → OBSERVING
  │       └─ hook returns `defer`
  │
  ├─ Pump reads tool_deferred
  │   ├─ [extract session_id, tool_use_id, tool_name]
  │   ├─ [join against pending observation]
  │   │   ├─ MATCH → ingest non-actionable record, clear pending, return to RUNNING
  │   │   └─ MISMATCH → clear pending (no record)
  │   └─ [no pending observation] → ignore
  │
  ├─ [timeout] → clear pending, invalidate record, return to RUNNING
  ├─ [child exit] → MarkExited, clear all pending, invalidate all records → EXITED
  ├─ [stop/delete] → kill child, clear all pending, invalidate all records → EXITED
  └─ [epoch replacement] → kill old child, clear all pending, invalidate all records
```

**Linearization points:**

1. **Observation arm**: `turnMu.Lock()` → validate turn binding + duplicate + capacity → `ApprovalStore.IngestObserved` → arm pending entry → `turnMu.Unlock()`. The store admit and the pending arm are one critical section.

2. **Defer join**: pump reads `tool_deferred` → compare fields under `turnMu` → if match, ingest record and clear pending. The join and ingest are one critical section.

3. **Clear on exit**: pump sets `turnClosed=true`, clears `pendingObservations`, calls `InvalidateSession` → closes `exited` channel. All under `turnMu`.

## 5. C1D success evidence

1. **Production entry**: `pokit run claude` preset routes to `ManagedClaudeService.CreateDetached`.
2. **Direct launch**: exact `claude` argv (no shell, no `-p`, no `--permission-prompt-tool`).
3. **Version certification**: pinned 2.1.209 verified through OS-neutral attestor seam.
4. **Session isolation**: isolated hook settings file, unguessable bridge token, private endpoint.
5. **Strict decode**: only `PreToolUse.tool_use_id`, `tool_name`, `tool_input`, `session_id` extracted; unknown fields rejected.
6. **Exact join**: `PreToolUse` fields match `tool_deferred` fields exactly.
7. **Non-actionable record**: `Actionable: false`, `Options: nil`, `DeliveryMaterial: nil`, `RequiredPerm: ""`.
8. **Zero mobile CTA**: record has no options; mobile renders nothing actionable.
9. **Privacy**: no prompt, command, tool input, hook capability, auth data in DTOs or logs.
10. **One bounded live probe**: real `pokit run claude` with `Bash("echo ok")` → defer observation visible in store, zero options.

## 6. Failure behavior

| Scenario | Behavior |
| --- | --- |
| Missing/wrong version | Create fails before spawn |
| Executable not found | Create fails before spawn |
| Hook bridge fails to start | Kill child, roll back create |
| Hook capability missing/wrong | Hook returns defer (fail-closed), no observation |
| Unknown PreToolUse fields | Hook returns defer, no observation |
| Oversized tool_input | Hook returns defer, no observation |
| Malformed JSON | Hook returns defer, no observation |
| tool_deferred missing session_id | No join, pending cleared on timeout |
| tool_deferred mismatched tool_use_id | No join, pending cleared |
| Duplicate tool_use_id | Second observation rejected (capacity) |
| Multi-tool batch | No observation armed (ambiguous) |
| Hook timeout (no tool_deferred within bound) | Clear pending, invalidate record |
| Child exit while observing | Clear all pending, invalidate all records |
| Stop/Delete while observing | Kill child, clear all pending |
| Epoch replacement | Old child killed, old pending cleared, old records invalidated |
| Daemon restart | No pending Claude authority restored |
| Capacity exhaustion (max pending) | Reject before mutating state |

## 7. Privacy and evidence projection

**Never stored, logged, or exposed in any DTO:**
- Raw `tool_input` bytes (only SHA-256 digest stored)
- Prompt text, command text, paths
- Hook capability token
- Hook bridge port
- Provider authentication material
- Hook settings file content
- Claude session transcript

**Stored in private runtime-owned pending observation (never in DTOs):**
- `sessionID`, `epoch`, `claudeSessionID`, `toolUseID`, `toolName`, `inputDigest`, `observedAt`

**Exposed in non-actionable public DTO:**
- `ApprovalID` (daemon-generated, never the provider tool_use_id)
- `SessionID`, `AgentKind: "claude_headless"`, `Kind: "approval"`
- `Source: SourceProviderProtocol` (versioned C0D-certified lifecycle)
- `Confidence: 1`
- `Options: []` (empty — zero actionable options)
- Standard A1 metadata (created_at, generation)

## 8. C1D explicit non-goals

- No `PermissionRequest` hook or `--permission-prompt-tool`
- No `allow_once`, `deny`, or any action option
- No `ClaimForExecution`, `ApprovalDelivery`, or decision delivery
- No provider resume (`claude resume` or equivalent)
- No mobile CTA or action handler wiring
- No modification of Codex semantics, schemas, or files
- No generic provider SDK, plugin framework, or Agent SDK
- No channels, N1, O1/O2, observer cleanup, tmux/cmux deletion
- No Windows implementation (only OS-neutral attestor seam)
- No `Bash`-specific review projection (deferred to C2D)
- No interactive PTY/key injection approval
- No `waiting_approval` status authority
- No persistence across daemon restart

## 9. File manifest (planned)

New files (no existing file modifications except composition wiring in `app.go`):

| File | Purpose |
| --- | --- |
| `internal/term/managed_claude.go` | `ManagedClaudeService`, `claudeManagedRuntime`, launch, pump, hook bridge |
| `internal/term/managed_claude_test.go` | C1D contract tests |
| `internal/term/claude_hook_bridge.go` | Private HTTP hook endpoint, strict PreToolUse decode |
| `internal/term/claude_hook_bridge_test.go` | Hook bridge unit tests |
| `internal/term/claude_attestor.go` | OS-neutral version/artifact certification seam |
| `internal/term/claude_attestor_test.go` | Attestor unit tests |

Minimal composition wiring in:
| File | Change |
| --- | --- |
| `cmd/devremote/app.go` | Add `EnableManagedClaude` config, construct `ManagedClaudeService`, wire routes |
| `internal/term/profiles.go` | No change needed (existing `claude` profile resolves `claude` on PATH) |
| `internal/term/create.go` | Route `claude` profile to `ManagedClaudeService` instead of `controlled_pty` |
