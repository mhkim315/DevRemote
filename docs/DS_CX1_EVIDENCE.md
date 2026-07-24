# DS-CX1 — Codex 0.145.0 Stock-Binary Co-Presence Conformance

**Status:** CONFORMANCE COMPLETE — `ACCEPT/FALLBACK_REQUIRED`

**Plan reference:** `docs/BASE_ALPHA_DUAL_SURFACE_EXECUTION_PLAN.md` §DS-CX1

**Conformance SHA:** 725a41bd3

**Date:** 2026-07-25

## 1. Binary Identity

| Field | Value |
|---|---|
| Binary path | `/opt/homebrew/bin/codex` |
| Version | `codex-cli 0.145.0` |
| SHA-256 | `134063e133f0b4244fa3b251acf973d4fe4b4aeeacbdc135211bf480f59f1477` |
| Platform | `darwin/arm64` (Mac OS 26.5.1) |
| Source | Unmodified stock Homebrew installation |

The binary is the unmodified stock Codex 0.145.0. No source patch, no private fork.

## 2. Test Results

### 2.1 Binary Identity — PASS

- Version parsing confirms `0.145.0` (major=0, minor=145, patch=0)
- SHA-256 digest matches pinned value
- `app-server` subcommand is present and executable

### 2.2 App-Server Stdio Handshake — PASS

- `codex app-server --stdio` starts successfully
- JSON-RPC `initialize` → `initialized` handshake completes
- `thread/start` returns server-assigned UUID (e.g., `019f9531-ac72-7701-9435-30631373507d`)
- `thread.id == thread.sessionId` — one session per thread
- `thread.cliVersion` = `"0.145.0"`
- **Note:** Responses omit `"jsonrpc":"2.0"` wrapper (non-standard)

### 2.3 One App-Server, One Thread — PASS (with finding)

- Exactly one app-server OS process is spawned
- `thread/start` with a different `threadId` creates a **new server-side thread**
  with a different UUID
- **CATEGORY-B FINDING:** The app-server allows multiple threads per process.
  Thread authority is not singular; each `thread/start` creates an independent
  thread identity.

### 2.4 JSON-RPC Turn Identity — PASS

- `thread.id` and `thread.sessionId` are valid ULID-format UUIDs
- `turn/start` with server-assigned thread ID returns `turn.id` (server-assigned UUID)
- Turn identity is deterministic per `turn/start` request
- Thread and turn IDs are server-authoritative, not client-assigned

### 2.5 App-Server Exit Cleanup — PASS

- Closing stdin causes the app-server to exit
- Exit is clean (no panic, no zombie)
- Process group cleanup is handled by the OS

### 2.6 Remote Control Daemon — SKIP

- `codex remote-control start --json` fails in this environment
- Remote control daemon requires system-level daemon management
- This path is not available for programmatic conformance testing with the
  stock binary alone

### 2.7 Multi-Client Fan-Out — PASS (with critical finding)

- **Two clients successfully connect** to the same `unix://` socket
- Both clients complete independent `initialize` handshakes
- A thread started by Client A is visible to both clients (same socket)
- **CATEGORY-B CRITICAL FINDING:** `turn/started` notifications are NOT
  broadcast to non-initiating clients. Only the client that sent `turn/start`
  receives the `turn/started` notification. Client B receives zero
  notifications.

## 3. Category B Verdict

### Confirmed (shared-core seam exists)

1. Stock binary identity (version, hash) is deterministic and verifiable
2. App-server starts and responds to JSON-RPC over stdio and Unix sockets
3. Thread identity is server-assigned with stable UUID format
4. Turn identity is server-assigned per `turn/start`
5. Multiple clients can connect to the same app-server Unix socket
6. `--listen unix://PATH` is the shipped remote connection path

### Not Confirmed (required for DUAL_SUPPORTED)

1. **Multi-client turn notification broadcast:** `turn/started` events are NOT
   fanned out to all connected clients. Only the initiating client observes
   turn lifecycle events.
2. **One-thread authority:** The app-server allows multiple concurrent threads
   per process. Thread identity is not singular.
3. **Complete turn lifecycle visibility:** Without auth/model credentials,
   full turn completion (turn/completed) cannot be verified.

### Disposition

**`ACCEPT/FALLBACK_REQUIRED`**

The stock Codex 0.145.0 app-server has a shared-core seam (Category B
confirmed). Multiple clients can connect to the same transport. However,
the app-server does NOT broadcast turn notifications to all connected
clients. This means a POKIT observer client cannot see TUI-originated turns
in real time, and a TUI cannot see POKIT-originated turns.

Codex remains:
- **Explicit interactive Terminal mode** using the existing controlled PTY path
- **Explicit headless structured mode** using the existing app-server path

These are separate sessions. The UI must disclose that they are separate.

## 4. Test Artifacts

- Test package: `companion-daemon/internal/term/ds_cx1_conformance_test.go`
- 7 conformance tests (5 PASS, 1 PASS with findings, 1 SKIP)
- All tests use the unmodified stock binary
- No production code was modified

## 5. Next Steps

- `DS-CX2` (Codex provider-TUI host) is **SKIPPED** per the plan
- `DS-CL1` (Claude conformance) proceeds independently
- Existing explicit Codex headless/interactive modes are preserved
