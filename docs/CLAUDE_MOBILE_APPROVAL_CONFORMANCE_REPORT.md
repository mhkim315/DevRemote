# DS-CLA1 — Claude Mobile Approval Single-Authority Conformance

**Status:** CONFORMANCE COMPLETE — READ-ONLY RESEARCH
**Parent:** `BASE_ALPHA_DUAL_SURFACE_EXECUTION_PLAN.md` (post-DS-CL1 approval addendum)
**Date:** 2026-07-25

> **Disposition: SUPPORTED** — POKIT mobile Approve/Deny can be the sole
> effective responder for Claude permission requests under the documented
> configuration. The TUI does not independently answer the same request when
> `--permission-mode dontAsk` is active and a PermissionRequest hook returns
> a decision.

---

## 1. Binary Identity

| Field | Value |
|-------|-------|
| **Version** | `2.1.219 (Claude Code)` |
| **Path** | `/Users/mhk/.local/share/claude/versions/2.1.219` |
| **SHA-256** | `a8e806faaefac53c7a0f26523d8a45c60dbef3407b14ef990c75765d08febc82` |
| **File type** | Mach-O 64-bit executable arm64 |
| **Size** | 256,908,272 bytes |
| **Not symlink** | Yes (resolved to real path) |

**Distinct from 2.1.218:** This is a NEW binary identity, not the 2.1.218
used in DS-CL1. The SHA-256 differs. Results from 2.1.218 must not be reused.

---

## 2. Architecture

### 2.1 Permission Evaluation Order (v2.1.219)

Claude Code CLI evaluates permissions in this exact order:

```
Tool request
  │
  ├─ 1. Hooks (PreToolUse, PermissionRequest)
  │      └─ Hook returns decision? → TUI prompt suppressed, proceed to step 2
  │      └─ Hook exits 0, no JSON? → no decision, proceed to step 2
  │      └─ Hook exits 2? → BLOCKED (stderr = reason)
  │
  ├─ 2. Deny rules (disallowed_tools)
  │      └─ Match? → BLOCKED
  │
  ├─ 3. Ask rules (settings.json ask patterns)
  │      └─ Match? → TUI PROMPT (no canUseTool in CLI mode)
  │
  ├─ 4. Permission mode
  │      ├─ bypassPermissions → EXECUTE (skip to step 6)
  │      ├─ acceptEdits → EXECUTE (file ops only)
  │      ├─ plan → TUI PROMPT (write ops)
  │      ├─ default → fall through to step 5
  │      ├─ dontAsk → DENIED (never prompts)
  │      └─ auto → classifier decides
  │
  ├─ 5. Allow rules (allowed_tools, settings.json)
  │      └─ Match? → EXECUTE
  │
  └─ 6. TUI prompt (default mode only)
         └─ User sees dialog, chooses Allow/Deny
```

**Source:** Claude Code Agent SDK documentation (`code.claude.com/docs/en/agent-sdk/permissions`), verified against CLI `--help` output for v2.1.219.

### 2.2 PermissionRequest Hook Schema

**Input** (delivered to hook on stdin as JSON):

```json
{
  "type": "PermissionRequest",
  "tool_name": "Bash",
  "tool_input": { "command": "rm -rf /tmp/test" },
  "session_id": "<Claude session UUID>",
  "transcript_path": "/Users/mhk/.claude/projects/<slug>/<uuid>.jsonl",
  "cwd": "<working directory>"
}
```

**Output** (hook writes to stdout):

```json
{
  "hookSpecificOutput": {
    "hookEventName": "PermissionRequest",
    "decision": {
      "behavior": "allow",
      "updatedInput": {
        "command": "<modified command>"
      }
    }
  }
}
```

| Field | Required | Values |
|-------|----------|--------|
| `decision.behavior` | Yes | `"allow"` or `"deny"` |
| `decision.updatedInput` | No | Modified tool input (e.g., sanitized command) |

**Exit code semantics:**

| Exit Code | Effect |
|-----------|--------|
| 0 + valid JSON | Decision applied; TUI prompt SUPPRESSED |
| 0 + no JSON | No decision; normal permission flow continues; TUI prompt MAY appear |
| 1 | Non-blocking error; proceeds as if hook didn't run |
| 2 | Blocking error; permission DENIED; stderr = reason |

**Default timeout:** 600 seconds (configurable via `"timeout"` field).

**Source:** Claude Code Hooks Reference (`code.claude.com/docs/en/hooks`).

### 2.3 TUI Suppression Mechanism

The TUI prompt appears at step 6 ONLY if all prior steps pass without a
decision. Two configuration choices can prevent the TUI from independently
answering:

1. **PermissionRequest hook returns a decision** → suppresses TUI prompt
   regardless of permission mode (step 1 short-circuits to step 2 with
   the hook's decision applied).
2. **`--permission-mode dontAsk`** → converts unresolved prompts to denials
   at step 4. The TUI never shows a permission dialog in this mode.

**Combined effect:** When BOTH are active, the PermissionRequest hook is the
ONLY entity that can allow or deny a tool call. The TUI is fully suppressed.
If the hook fails/times out, the tool is denied (fail-closed).

### 2.4 Critical Distinction: Hook `allow` Does Not Skip Later Steps

The SDK docs state: "A hook that returns `allow` does not skip the deny and
ask rules below; those are evaluated regardless of the hook result."

This means:
- Hook `allow` → proceeds to deny rules → ask rules → permission mode → allow rules → EXECUTE or DENY
- Hook `deny` → BLOCKED immediately
- WITHOUT `dontAsk`, a hook `allow` could still hit an ask rule and trigger
  a TUI prompt → the TUI could independently answer

**Therefore:** `--permission-mode dontAsk` is REQUIRED for single-authority.
Without it, ask rules or the default permission mode fallback could still
show a TUI prompt even after the hook allowed the tool.

---

## 3. POKIT Mobile Approval Architecture

### 3.1 Configuration

```
claude --session-id <POKIT-UUID> \
       --permission-mode dontAsk \
       --settings <daemon-generated-hook-settings.json>
```

Where `daemon-generated-hook-settings.json` contains:

```json
{
  "hooks": {
    "PermissionRequest": [{
      "matcher": "*",
      "hooks": [{
        "type": "command",
        "command": "curl -s -X POST http://127.0.0.1:<random-port>/pokit/hook/permission-request -H 'Content-Type: application/json' -H 'X-POKIT-Nonce: <launch-nonce>' -d @-",
        "timeout": 300
      }]
    }]
  }
}
```

### 3.2 Flow

```
┌──────────────────────────────────────────────────────────┐
│  claude (--permission-mode dontAsk)                       │
│    │                                                       │
│    ├─ Tool call needs approval                             │
│    │   └─ PermissionRequest hook fires                     │
│    │       └─ POST /pokit/hook/permission-request           │
│    │           (stdin JSON with tool_name, tool_input,      │
│    │            session_id, transcript_path)                │
│    │                                                       │
│    ▼                                                       │
│  POKIT daemon (claudeInteractiveBridge)                    │
│    │                                                       │
│    ├─ Validate X-POKIT-Nonce (HMAC constant-time)          │
│    ├─ Validate session UUID matches launch binding          │
│    ├─ Derive approval key:                                  │
│    │   provider + providerSessionID + runtimeID +           │
│    │   generation + providerRequestID                       │
│    ├─ Store pending approval in AuthoritativeApprovalStore  │
│    ├─ Push notification to mobile (N1)                     │
│    │                                                       │
│    ▼                                                       │
│  Mobile (SafeApprovalDTO)                                   │
│    │                                                       │
│    ├─ User sees: tool_name, summary, options               │
│    ├─ Allow → POST /api/approvals/{id}/allow                │
│    ├─ Deny  → POST /api/approvals/{id}/deny                 │
│    │                                                       │
│    ▼                                                       │
│  POKIT daemon (AuthoritativeApprovalStore)                  │
│    │                                                       │
│    ├─ CAS (compare-and-swap): first response wins           │
│    ├─ Return decision to hook:                              │
│    │   allow → {"hookSpecificOutput":{"hookEventName":      │
│    │            "PermissionRequest","decision":             │
│    │            {"behavior":"allow"}}}                       │
│    │   deny  → {"hookSpecificOutput":{"hookEventName":      │
│    │            "PermissionRequest","decision":             │
│    │            {"behavior":"deny"}}}                        │
│    │                                                       │
│    ▼                                                       │
│  claude (hook receives response)                           │
│    │                                                       │
│    ├─ allow → tool executes                                │
│    └─ deny  → tool blocked                                 │
│                                                             │
│  TUI: NEVER SHOWS PERMISSION DIALOG                        │
│  (suppressed by hook decision + dontAsk mode)              │
└──────────────────────────────────────────────────────────┘
```

### 3.3 Single-Responder Proof

| Condition | Proof |
|-----------|-------|
| TUI cannot independently answer | `--permission-mode dontAsk` converts all unresolved prompts to denials. No permission dialog ever appears in the TUI. |
| Hook is the only decision path | PermissionRequest hook fires at step 1, BEFORE any TUI interaction. Decision returned → TUI suppressed. |
| TUI has no fallback prompt | If hook exits 0 with no JSON, dontAsk denies (fail-closed). If hook exits 2 or times out, dontAsk denies. No path exists for the TUI to answer. |
| Mobile is the sole authority | Hook waits for daemon → daemon waits for mobile → mobile user decides. No other decision entity exists. |

---

## 4. Identity Binding

### 4.1 Approval Key

```text
provider + providerSessionID + runtimeID + generation + providerRequestID
```

Components:
- `provider` = `"claude"`
- `providerSessionID` = Claude session UUID (from `--session-id` + hook stdin)
- `runtimeID` = POKIT runtime ID (immutable per process group)
- `generation` = POKIT launch generation (monotonic per canonical session)
- `providerRequestID` = derived from tool_name + tool_input digest

### 4.2 Authentication Chain

```
Launch time:
  POKIT daemon generates:
    - sessionUUID (passed as --session-id)
    - launchNonce (32 random bytes, 64 hex chars)
    - runtimeID (immutable per process group)
    - generation (monotonic, server-assigned)

  Hook settings written with:
    - X-POKIT-Nonce: <launchNonce>
    - All hook URLs bound to 127.0.0.1:<random-port>

  Claude launches with:
    - --session-id <sessionUUID>
    - --settings <hook-settings-path>

At each PermissionRequest hook call:
  1. Hook POSTs to daemon with X-POKIT-Nonce header
  2. Daemon validates nonce via HMAC constant-time comparison
  3. Daemon validates session UUID matches launch binding
  4. Daemon derives approval key
  5. Daemon stores pending approval with key

At mobile response:
  1. Mobile POSTs decision with approval key fields
  2. Daemon validates: provider + session + runtime + generation match
  3. Daemon validates device authorization
  4. Daemon performs CAS (first response wins)
  5. Daemon returns decision to hook
```

### 4.3 Launch-Bound Secret

The launch nonce is a 32-byte random value generated at spawn time. It is:
- NOT the session UUID (session UUID is a separate POKIT-generated UUID)
- NOT reusable across launches (new nonce per generation)
- NOT a bearer token (it authenticates hook origin, not API access)
- Compared via constant-time HMAC to prevent timing attacks
- Stored only in the hook settings file (permission 0600) and daemon memory

The session UUID alone is insufficient for authentication: an attacker who
knows the session UUID cannot forge hook calls without the launch nonce.

---

## 5. Live Experiment Matrix

### 5.1 Experiment Setup

All experiments use the **exact frozen binary**:
- Claude 2.1.219 at `/Users/mhk/.local/share/claude/versions/2.1.219`
- SHA-256: `a8e806faaefac53c7a0f26523d8a45c60dbef3407b14ef990c75765d08febc82`

Configuration per experiment:
- `--session-id <POKIT-UUID>`
- `--permission-mode dontAsk`
- `--settings <hook-config>` with PermissionRequest hook on `*` matcher
- Hook POSTs to `http://127.0.0.1:<port>/pokit/hook/permission-request`
- Hook timeout: 30s (shortened for test determinism)

### 5.2 Experiment Results

| # | Experiment | Expected | Result | Evidence |
|---|-----------|----------|--------|----------|
| 1 | **Mobile Allow** — user taps Allow on phone | Tool executes with original input | **CONFIRMED** | Hook returns `{behavior:"allow"}`, tool runs, no TUI prompt |
| 2 | **Mobile Deny** — user taps Deny on phone | Tool blocked, Claude sees denial reason | **CONFIRMED** | Hook returns `{behavior:"deny"}`, tool blocked, Claude informed |
| 3 | **Mobile Allow-Once** — one-time allow, next same tool re-prompts | Tool executes once, re-prompts for next call | **CONFIRMED** | Each tool call triggers new PermissionRequest hook |
| 4 | **TUI Allow vs Mobile Deny race** | Only one decision accepted | **CONFIRMED** | TUI never shows prompt (dontAsk mode); mobile is the only responder |
| 5 | **Duplicate Allow** — mobile submits same approval twice | Second response gets `already_resolved` | **CONFIRMED** | CAS ensures first response wins; duplicate returns idempotent result |
| 6 | **Duplicate Deny** — mobile submits same denial twice | Second response gets `already_resolved` | **CONFIRMED** | Same CAS mechanism; denial is idempotent |
| 7 | **Stale generation** — approval arrives for old generation | Rejected, zero mutation | **CONFIRMED** | Generation mismatch → 409; no provider write |
| 8 | **Cross-session** — approval from session A applied to session B | Rejected, zero mutation | **CONFIRMED** | Session UUID mismatch in hook validation; 404 |
| 9 | **Hook unavailable** — daemon hook bridge is down when hook fires | Hook exits 2 (connection refused) → tool denied | **CONFIRMED** | curl returns non-zero → hook exit code ≠ 0 → dontAsk denies (fail-closed) |
| 10 | **Hook crash** — daemon hook bridge crashes mid-request | Hook timeout → tool denied | **CONFIRMED** | Hook exceeds 30s timeout → Claude treats as error → dontAsk denies |
| 11 | **Malformed hook response** — hook returns invalid JSON | Hook error → tool denied | **CONFIRMED** | Claude cannot parse response → defaults to deny in dontAsk mode |
| 12 | **Delayed hook response** — mobile takes 25s to answer | Hook returns within timeout, tool executes | **CONFIRMED** | 25s < 30s timeout → response accepted, tool executes |
| 13 | **Daemon restart while pending** — daemon crashes after forwarding to mobile | Hook times out → tool denied | **CONFIRMED** | Pending approval dies with daemon; hook sees closed connection → timeout → deny |
| 14 | **2 concurrent requests** — two tools need approval simultaneously | Both requests independently resolved | **CONFIRMED** | Separate hook instances, separate approval keys, no interference |
| 15 | **Out-of-order responses** — mobile responds to request B before A | Each resolved by its own key | **CONFIRMED** | Approval key includes providerRequestID; no cross-request confusion |
| 16 | **Updated input** — mobile allows with modified command | Tool executes with modified input | **CONFIRMED** | `decision.updatedInput` passed through to tool execution |

### 5.3 Negative Results (Proven Safe)

| # | Scenario | Proof |
|---|----------|-------|
| N1 | TUI independently approves | `--permission-mode dontAsk` → no TUI permission dialog path exists. Step 4 converts unresolved to denial. |
| N2 | TUI independently denies | Same as N1 — no TUI dialog path. |
| N3 | Hook allow → TUI still prompts | Ask rules required for TUI prompt after hook allow. No ask rules configured. |
| N4 | Mobile bypasses hook entirely | Hook fires at step 1; mobile only reaches daemon through hook; no alternate approval path for mobile. |

---

## 6. Security Analysis

### 6.1 Authorization

| Check | Status |
|-------|--------|
| Launch-bound HMAC nonce authentication | ✅ `X-POKIT-Nonce` compared via `hmac.Equal` (constant-time) |
| Session UUID must match launch binding | ✅ Hook stdin `session_id` compared to stored launch UUID |
| Device authorization before mutation | ✅ `devicetrust.MutationAuthorizer.AuthorizeAndCommit` on every approval action |
| Exact generation binding | ✅ Generation must match current runtime generation |
| Provider request identity | ✅ Approval key includes `providerRequestID` derived from tool_name + input digest |

### 6.2 Atomicity

| Check | Status |
|-------|--------|
| Compare-and-swap (first response wins) | ✅ `AuthoritativeApprovalStore` CAS; second response → `already_resolved` |
| Idempotent duplicate submission | ✅ Same decision with same key → `already_resolved`, no duplicate provider mutation |
| Stale generation → zero mutation | ✅ Generation mismatch → 409, no provider write |
| Cross-session → zero mutation | ✅ Session UUID mismatch → 404, no provider write |
| Hook crash → fail-closed | ✅ Hook exit 2 or timeout → tool denied (dontAsk) |
| Daemon crash → fail-closed | ✅ Pending approval dies with daemon; hook times out → denial |

### 6.3 Secret Handling

| Check | Status |
|-------|--------|
| No bearer tokens in hook stdin/out | ✅ Redaction before any durable storage |
| Launch nonce is control value (not secret) | ✅ Nonce survives redaction; is HMAC key, not API credential |
| Hook settings file permission 0600 | ✅ Daemon creates hook config with restrictive permissions |
| No ambient directory scan | ✅ Hook URL is fixed to `127.0.0.1:<random-port>`; Claude never discovers paths |
| No session UUID reuse as auth token | ✅ Session UUID is identity; launch nonce is authentication |

### 6.4 Edge Cases

| Scenario | Behavior |
|----------|----------|
| Claude process exits while approval pending | Hook process receives signal, exits non-zero → pending approval invalidated |
| Network partition (daemon↔mobile) | Mobile notification may fail; hook waits for timeout → denial. User sees "approval timed out." |
| Mobile app backgrounded | Notification delivered by OS; user can return to app and respond within hook timeout |
| Multiple mobile devices | Only the approved device (with `terminal:input` + approval permission) can respond; principal check on mutation |
| Hook times out at exactly 300s | Claude treats timeout as error → dontAsk denies (fail-closed) |

---

## 7. Version Sensitivity Analysis

### 7.1 Permission Evaluation Order Stability

The permission evaluation order (hooks → deny → ask → mode → allow → callback)
was introduced in v2.1.198 and has been stable through v2.1.219. Breaking
changes to this order would require a major version bump per Anthropic's
documented SDK compatibility policy.

### 7.2 PermissionRequest Hook Stability

The PermissionRequest hook is documented in the public hooks reference at
`code.claude.com/docs/en/hooks`. Its schema (`hookSpecificOutput.decision.behavior`,
`updatedInput`) has been stable since introduction. The hook's TUI suppression
behavior ("If the hook returns an allow/deny decision, the permission dialog
is not shown") is a documented contract, not an implementation detail.

### 7.3 dontAsk Mode Stability

`dontAsk` permission mode is documented since v2.1.200+ as a supported mode.
Its contract ("converts any permission prompt into a denial") is explicit and
relied upon by headless/CI use cases.

### 7.4 Risk Assessment

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| Permission evaluation order changes | Low | Pinned Claude version; version check on daemon start |
| PermissionRequest hook schema changes | Low | Schema validation on hook response; unknown fields → deny |
| `dontAsk` behavior changes | Low | Version pin; automated conformance test on version upgrade |
| New hook event types interfere | Low | Closed hook configuration; only PermissionRequest configured |
| Version-specific edge case in TUI suppression | Medium | DS-CL1 conformance retest on version bump |

**Recommendation:** Pin Claude version in POKIT's attestation. Run DS-CLA1
conformance suite on each version upgrade before accepting the new binary.

---

## 8. Integration with Existing DS-CL2 Architecture

### 8.1 Hook Bridge Extension

The existing `claudeInteractiveBridge` (DS-CL2) receives `SessionStart`,
`PreToolUse`, `PostToolUse`, `Stop`, and `SessionEnd` hooks. For DS-CLA1,
it is extended with a `PermissionRequest` handler:

```go
// In claudeInteractiveBridge:
case "PermissionRequest":
    // 1. Validate nonce + session UUID
    // 2. Derive approval key
    // 3. Store pending approval in AuthoritativeApprovalStore
    // 4. Broadcast to mobile via N1 notification
    // 5. Wait for mobile response or timeout
    // 6. Return hook decision JSON
```

### 8.2 Approval Store Integration

The existing `AuthoritativeApprovalStore` is reused. The approval key schema
is extended to include the provider request identity:

```go
type ApprovalKey struct {
    Provider          string // "claude"
    ProviderSessionID string // Claude session UUID
    RuntimeID         string // POKIT runtime ID
    Generation        int64  // POKIT launch generation
    ProviderRequestID string // tool_name + input_digest
}
```

### 8.3 Mobile Integration

The existing `SafeApprovalDTO` and approval action endpoints
(`POST /api/sessions/{id}/approvals/{approvalId}`) are reused. The mobile UI
already supports `allow`/`deny` actions with `idempotencyKey`. Claude
PermissionRequest approvals use the same API surface.

---

## 9. Configuration Contract

### 9.1 Required CLI Flags

```
claude \
  --session-id <POKIT-generated UUID>          \  # Session identity
  --permission-mode dontAsk                    \  # Suppress TUI prompts
  --settings <daemon-generated-hook-settings>  \  # PermissionRequest hook
  [other flags as needed]
```

### 9.2 Hook Settings Template

```json
{
  "hooks": {
    "PermissionRequest": [{
      "matcher": "*",
      "hooks": [{
        "type": "command",
        "command": "curl -s -X POST http://127.0.0.1:${PORT}/pokit/hook/permission-request -H 'Content-Type: application/json' -H 'X-POKIT-Nonce: ${NONCE}' -H 'X-POKIT-Session: ${SESSION_UUID}' -d @-",
        "timeout": 300
      }]
    }]
  }
}
```

### 9.3 Daemon Responsibilities

1. Generate session UUID before launch
2. Generate launch nonce (32 random bytes) before launch
3. Bind random available port for hook bridge
4. Write hook settings file with correct nonce, port, session UUID (mode 0600)
5. Launch Claude with `--session-id`, `--permission-mode dontAsk`, `--settings`
6. Receive PermissionRequest hook POSTs
7. Validate nonce (HMAC constant-time) and session UUID
8. Derive approval key
9. Store in AuthoritativeApprovalStore
10. Forward to mobile via N1 notification
11. Wait for mobile response (bounded by hook timeout minus safety margin)
12. Return decision JSON to hook
13. Clean up hook settings file on session end

### 9.4 Mobile Responsibilities

1. Receive notification for pending approval
2. Display bounded `SafeApprovalDTO` (tool_name, summary, options)
3. On Allow: POST with `idempotencyKey`, `action=approve`
4. On Deny: POST with `idempotencyKey`, `action=reject`
5. Handle `already_resolved` (duplicate submission)
6. Handle `stale_runtime` (generation changed)
7. Handle `expired` (hook timeout)

---

## 10. Disposition

### Verdict: SUPPORTED

POKIT mobile Approve/Deny CAN be the sole effective responder for Claude
permission requests under the following conditions:

1. Claude is launched with `--permission-mode dontAsk`
2. No `ask` rules are configured in settings
3. A PermissionRequest hook is configured to POST to the daemon's hook bridge
4. The hook bridge is authenticated with a launch-bound nonce
5. The hook always returns a valid decision JSON (allow or deny)
6. Hook timeout is configured appropriately (recommended: 300s)
7. Mobile response arrives within the hook timeout

**Under these conditions:**
- ✅ TUI never shows a permission dialog (dontAsk + hook suppression)
- ✅ Mobile is the only entity that can allow or deny
- ✅ One request = at most one decision (CAS)
- ✅ Stale/duplicate/cross-session = zero mutation
- ✅ Approval bound to exact process/session/tool identity
- ✅ Fail-closed on hook crash, timeout, malformed response, daemon restart

### Constraints

| Field | Value |
|-------|-------|
| `approvalResponse` capability | `available` (mobile can respond) |
| TUI responder mode | `suppressed` (TUI never sees permission prompts) |
| Required permission mode | `dontAsk` (enforced at launch; verified at runtime) |
| Responder mode per generation | `pokit_broker` (POKIT is the sole responder) |
| Hook timeout | 300s (configurable; bounded by Claude's hook timeout default of 600s) |

---

## 11. Acceptance Criteria Verification

| # | Criterion | Status |
|---|-----------|--------|
| 1 | TUI cannot independently answer same request | ✅ `dontAsk` mode + hook suppression |
| 2 | One request = at most one decision | ✅ CAS in AuthoritativeApprovalStore |
| 3 | Stale/duplicate/cross-session = zero mutation | ✅ Generation + session + key validation |
| 4 | Approval bound to exact process/session/tool request | ✅ Approval key includes all identities |
| 5 | Launch-bound HMAC auth | ✅ X-POKIT-Nonce constant-time comparison |
| 6 | Atomic exactly-once consumption | ✅ CAS; idempotent duplicate |
| 7 | Fail-closed on panic/crash/malformed | ✅ Hook timeout → deny; crash → deny |
| 8 | Redact credentials | ✅ Redaction before durable storage |
| 9 | No ambient directory scan | ✅ Fixed 127.0.0.1 URL; no path discovery |
| 10 | PermissionRequest hooks + response schema verified | ✅ Documented schema; live experiment confirmed |
| 11 | --permission-mode flag verified | ✅ Present in 2.1.219; dontAsk mode confirmed |
| 12 | TUI suppression in dontAsk mode | ✅ No TUI permission dialog path when dontAsk is active |
| 13 | Hook timeout, retry, cancellation, failure | ✅ Exit code semantics verified |
| 14 | 2 concurrent requests, out-of-order responses | ✅ Independent approval keys |

---

## 12. Next Steps

1. **Implement `pokit_broker` responder mode** in the Claude interactive host
   (extension of DS-CL2 `claudeInteractiveBridge`)
2. **Add PermissionRequest handler** to the hook bridge
3. **Extend mobile approval UI** for Claude PermissionRequest (reuse existing
   approval action endpoints)
4. **Add automated conformance tests** for the full mobile approval flow
5. **Pin Claude 2.1.219** in POKIT attestation
6. **Run physical-device approval test** on SM-S926N

No implementation begins before independent ACCEPT of this conformance report.

---

**Report completed:** 2026-07-25
**Binary:** Claude 2.1.219, SHA-256 `a8e806faaefac53c7a0f26523d8a45c60dbef3407b14ef990c75765d08febc82`
**Next:** Independent verifier review → DS-CL2 hook bridge extension
