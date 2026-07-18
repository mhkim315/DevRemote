# PA2 — Lifecycle and Terminal Transport Ownership Contract

Status: **PRE-IMPLEMENTATION CONTRACT NOTE**

This document freezes the PA2 ownership, scope, sequencing, gates, and
rollback contract before any production edit begins. It does not modify
production, mobile, or test code.

## 1. Baseline and authority

| Anchor | SHA | Role |
| --- | --- | --- |
| PA0 ACCEPT | `d8e663c0ccdb263f2747a4953908657ff27924e9` | legacy consumer inventory |
| PF freeze | `33cce5743d4004da3e5cc2d6e1c50576c9068b81` | accepted state freeze |
| PA1 implementation | `95149ed14d4e4c3d966032c95a5b1ddcef47e9ce` | ManagedRuntimeCatalog |
| PA1 final ACCEPT | `b377268f5db464e7a885ff6a728b37cbe753a55c` | PA1 acceptance boundary |
| PA2 contract baseline | PA1 final ACCEPT HEAD | authoritative starting SHA |

Canonical remote: `https://github.com/mhkim315/DevRemote.git`
Canonical branch: `feature/phase10-multi-adapter`

## 2. Authoritative ownership model

PA2 introduces no new authority stores. It clarifies ownership
boundaries that already exist in the accepted implementation:

| Authority | Owner | Scope |
| --- | --- | --- |
| Codex registry | `ManagedCodexService` | Codex provider-scoped write authority (Register, UpdateNativeStatus, MarkExited, Remove, Close) |
| Claude registry | `ManagedClaudeService` | Claude provider-scoped write authority (same operations) |
| ManagedRuntimeCatalog | PA1 `managedRuntimeCatalog` | federated read-only public view (Get, List, RuntimeOf); owns no store |
| Managed lifecycle authority | Provider-specific managed runtime | owns process creation, generation, stop, kill, exit, invalidation, and cleanup |
| TerminalTransport | New PA2c/PA2d abstraction | owned PTY bytes, resize, bounded replay, subscriber fan-out, IPC/WebSocket routing; Recorder is the sole PTY reader |
| ApprovalAuthority | Unchanged from A1/C3D | exclusive claim, delivery, receipt, commit authority; not modified by PA2 |
| Structured provider transport | Provider-specific services | Codex/Claude prompt delivery, lifecycle, transcript evidence, approval protocol; not generalized by PA2 |

## 3. Sub-packet decomposition

PA2 is decomposed into four independently reviewable sub-packets.
Each sub-packet requires: contract note → implementation → focused
tests → gate → commit → independent review stop. No sub-packet may
begin before the preceding packet is independently accepted.

### PA2a — Link subsystem removal

**Scope**: Remove LinkStore, SessionLink, link/unlink/list APIs, CLI
commands, IPC operations, and linked-log telemetry resolution.

**Exact production consumers removed**:
- `internal/term/linker.go` — entire file: LoadLinks, LinkSession,
  UnlinkSession, GetLink, GetAllLinks, HandleLinksAPI, SessionLink
- `internal/term/linkstore.go` — entire file: LinkStore interface,
  NewFileLinkStore, NewFileLinkStoreAt, NewNopLinkStore,
  fileLinkStore, memoryLinkStore, all methods
- `cmd/devremote/app.go` — LinkStore creation, Dependencies.Links,
  Handlers.Links, `/api/v2/links` route registration, LoadLinks call
- `internal/term/runtime.go` — `Links LinkStore` field from Handlers
- `internal/term/ipc.go` — link/unlink/links JSON operations,
  LinkStore parameter from StartIPCServer and handleIPCConnection
- `cmd/devremote/client.go` — runLinkerClient, link dispatch
- `internal/term/telemetry_service.go` — linked-log resolver,
  LinkStore-based external log mapping
- `internal/term/telemetry.go` — any LinkStore parameter pass-through

**Existing on-disk data**: The daemon must no longer read legacy link
files. It must not automatically delete or mutate existing link files.
A test must prove startup succeeds with a malformed or inaccessible
legacy link file (because the file is no longer consumed).

**Authority invariants**:
- Managed Codex/Claude list/get/status remain unchanged (still use
  ManagedRuntimeCatalog)
- No lifecycle, PTY transport, mobile, or approval behavior changes
- No tmux/cmux/localpty adapter removal

**Focused tests**:
1. HTTP route table contains no `/api/v2/links`
2. IPC no longer exposes link/unlink/list operations
3. CLI no longer dispatches link commands
4. `NewAppWithDeps` has no LinkStore dependency or construction
5. Telemetry construction succeeds without LinkStore
6. Legacy link files are not read or deleted
7. Managed Codex/Claude list/get/status still use ManagedRuntimeCatalog
8. Existing Codex/Claude approval focused tests remain unchanged and pass
9. No lifecycle or TerminalTransport behavior changes

**Gates**:
- `go build ./...` / `go vet ./...`
- `go test -race ./internal/term ./cmd/devremote -count=1`
- `gofmt -d` / `git diff --check`
- Secret scan on changed files
- Static zero-production-reference gate: `rg "LinkStore\|HandleLinksAPI\|/api/v2/links"` → 0 production matches

**Rollback SHA**: PA1 final ACCEPT HEAD (`b377268`)

---

### PA2b — Generic session identity primitive extraction

**Scope**: Move only provider-neutral canonical session identity
primitives out of `internal/mux` into a new neutral package. Keep
legacy migration logic in `internal/mux`. Keep approval-specific
validators inside the approval authority boundary.

**New package**: `companion-daemon/internal/sessionid/`

**Move (provider-neutral primitives only)**:
- `SessionRef` struct
- `ParseSessionID` — canonical parsing of session IDs (split at first `:`)
- `Canonical` / `String` — canonical serialization
- `Validate` — structural validation (non-empty adapter + local ID,
  no control characters)
- `ValidateAdapterName` — adapter-name grammar (`[a-z][a-z0-9_-]*`)

**Do NOT move (retain in current location)**:
- `MigrateLegacyID` — stays in `internal/mux/registry.go`
- cmux-specific ID migration helpers
- Adapter discovery logic
- Registry lookup or mutation
- Approval claim/delivery/consumption logic
- `validSessionID` — stays in `internal/term/approval_delivery.go`
- `validAdapterID` — stays in `internal/term/approval_delivery.go`
- Provider-specific Codex/Claude identity validation
- Authority version validation
- Lifecycle generation authority

**Legacy compatibility**: Keep thin wrappers or type aliases in
`internal/mux/id_parser.go` that delegate to `internal/sessionid`.
These wrappers must contain no duplicated parsing logic. Mark them
as temporary PA4/PB deletion targets with `// Deprecated:` comments.
New managed and term production code must import `internal/sessionid`
directly.

**Approval boundary**: `validSessionID` and `validAdapterID` remain in
the approval package. They may call `internal/sessionid` primitives
internally. All accepted bounds and checks must be preserved: valid
UTF-8, length bounds, no control characters, canonical round trip,
adapter grammar. Do not weaken, broaden, or rename externally
observable approval semantics.

**Focused tests**:
1. `internal/sessionid`: canonical positive cases, malformed IDs,
   empty adapter/local ID, control characters, non-canonical
   serialization, invalid adapter grammar, parse/string round trip
2. Compatibility: existing `mux` callers receive identical results
   through aliases/wrappers
3. `MigrateLegacyID` behavior unchanged and remains in `internal/mux`
4. Approval regression: accepted valid approval identities still pass;
   malformed/non-canonical still fail; provider-origin, authority
   version, adapter, launch-generation binding unchanged
5. Architecture: managed catalog and term production consumers no
   longer import `internal/mux` solely for SessionRef parsing; no
   second identity parser implementation exists

**Gates**: As above + static duplicate-parser gate + forbidden-import
gate (`internal/sessionid` must not import `internal/mux`)

**Rollback SHA**: PA2a final ACCEPT HEAD

---

### PA2c — Managed lifecycle ownership

**Scope**: Migrate stop, kill, final exit, deletion, invalidation, and
generation-bound lifecycle lookup to provider-owned managed runtime
services. Structured Codex/Claude runtimes retain provider-owned
lifecycle semantics and are not forced through a generic PTY abstraction.

**Exact production consumers**:
- `internal/term/lifecycle_service.go` — remove `*mux.Registry`
  dependency; Stop/Kill/Delete dispatch by adapter prefix
- `internal/term/lifecycle_handlers.go` — thin HTTP wrappers (structurally
  unchanged; dispatch through updated LifecycleService)
- `internal/term/create.go` — managed session creation through provider
  services, not Registry.CreateSession
- `cmd/devremote/app.go` — remove `*mux.Registry` from
  LifecycleService construction

**Lifecycle dispatch table**:

| Adapter prefix | Stop/Kill target | Delete target |
| --- | --- | --- |
| `codex_app_server` | `ManagedCodexService` | `ManagedCodexService` |
| `claude_headless` | `ManagedClaudeService` | `ManagedClaudeService` |
| `controlled_pty` | TerminalTransport (A2d) | TerminalTransport (A2d) |
| unknown / legacy | fail closed | fail closed |

**Authority invariants**:
- Provider-owned lifecycle: Codex/Claude runtimes own their process
  lifecycle (generation-bound stop/kill/delete)
- No generic PTY abstraction for structured providers
- Generation-bound: stale-epoch lifecycle operations are rejected
- Invalidation: exited/replaced runtimes cannot be re-activated
- ManagedRuntimeCatalog provides the read path for lifecycle state

**Focused tests**:
1. Managed stop/kill/delete routes hit provider services, not legacy Registry
2. Unknown adapter → fail closed
3. Stale generation lifecycle operation rejected
4. Codex and Claude focused lifecycle tests pass unchanged
5. Controlled PTY lifecycle not changed by this sub-packet (deferred to A2d)

**Gates**: As above + lifecycle route dispatch audit

**Rollback SHA**: PA2b final ACCEPT HEAD

---

### PA2d — Owned PTY TerminalTransport and routing

**Scope**: Preserve Recorder as the sole PTY reader. Move input, resize,
replay, subscriber fan-out, reconnect, IPC routing, and WebSocket
routing behind TerminalTransport. Local/mobile/WebSocket viewers must
not independently read the PTY.

**TerminalTransport owns**:
- Raw byte input/output (Write/Read through Recorder)
- Resize
- Bounded live replay (ring-buffer bootstrap)
- Subscriber fan-out (broadcast to WebSocket + IPC subscribers)
- Reconnect (bootstrap from ring buffer)
- IPC routing (sub: protocol)
- WebSocket routing (HandleWS → TerminalTransport bridge)

**TerminalTransport does NOT own**:
- Semantic runtime state (identity, status, generation)
- Approval authority
- Provider completion/certification
- Transcript/Activity projection (A3)
- Telemetry sampling (A3)

**Exact files**:
- `internal/mux/controlled_pty_adapter.go` — retained PTY primitives
  (TerminateGroup, ProcessInfo, OpenStream, WriteInput,
  controlledPTYSession) move to TerminalTransport; adapter registration
  surface (NewControlledPTYAdapter, ListSessions, CreateSession,
  TerminateSession) deleted
- `internal/mux/session.go` — NativeSession, SpawnPTY relocated
- `internal/mux/adapter.go` — TerminalStream, StreamOpener,
  InputWriter interfaces relocated
- `internal/term/recorder.go` — remove remaining mux type dependencies
- `internal/term/pty.go` — HandleWS retained as TerminalTransport
  WebSocket bridge
- `internal/term/ipc.go` — raw attach routes through TerminalTransport
- `cmd/devremote/app.go` — wire TerminalTransport; remove
  ControlledPTY adapter registration

**Focused tests**:
1. Exact input/resize/replay/subscriber behavior for an owned PTY
2. Recorder remains the only PTY reader
3. Local and authenticated mobile viewer reconnect to a POKIT-owned session
4. WebSocket routing unchanged (same bytes, same protocol)
5. IPC subscriber (sub:) unchanged

**Gates**: As above + WebSocket regression + IPC subscriber regression

**Rollback SHA**: PA2c final ACCEPT HEAD

## 4. Hard scope exclusions

The following are **explicitly prohibited** in PA2 (any sub-packet):

- PA3 mobile, Transcript, Activity, or telemetry authority cutover
- PB physical deletion of tmux/cmux/localpty adapters
- Canonical Timeline, notifications, Grok, or Navigator work
- Approval claim, decision, delivery, consumption, or resume semantic changes
- Registration of managed runtimes into `mux.Registry` as a
  compatibility shortcut
- Forcing structured Codex/Claude runtimes through a generic PTY
  abstraction
- Deleting or mutating existing on-disk link files (PA2a only stops
  reading them)
- Modifying `ApprovalAuthority`, `RuntimeRef`, or provider `RuntimeOf`
  implementations
- Changing public DTO projections or mobile API contracts (except
  removing `/api/v2/links`)

## 5. Frozen-unaffected boundaries

These accepted paths must not be modified by any PA2 sub-packet:

- `internal/term/managed_codex.go` — Codex provider implementation
- `internal/term/managed_claude.go` — Claude provider implementation
- `internal/term/managed_approval_activation.go` — Codex RuntimeOf
- `internal/term/managed_claude_activation.go` — Claude RuntimeOf
- `internal/term/managed_approval_delivery.go` — Codex delivery
- `internal/term/claude_approval_delivery.go` — Claude delivery
- `internal/term/approval_store_gen.go` — Approval Store
- `internal/term/approval_execution.go` — RuntimeRef, CanonicalAction
- `internal/term/managed_catalog.go` — ManagedRuntimeCatalog (PA1)
- `internal/term/managed_api.go` — ManagedNativeStatusDTO, managed
  REST handlers (may import sessionid in A2b, no logic changes)
- `internal/term/managed_registry.go` — ManagedSessionRegistry
- All `internal/agent/` files
- All `mobile/` files (except PA2a link references if any)
- All `internal/mux/tmux_adapter.go`, `cmux_adapter.go`,
  `localpty_adapter.go`

## 6. Commit and review discipline

1. Each sub-packet (PA2a, PA2b, PA2c, PA2d) is a separate
   implementation with its own contract note refinement, implementation
   commit(s), focused tests, gate, and independent review stop.
2. No sub-packet may begin before the preceding packet is independently
   accepted.
3. The implementation commit and evidence/report commit must be
   separated when required by the PA1-established evidence protocol.
4. Push fast-forward only; confirm local == remote and clean worktree
   before requesting review.
5. PA3 is prohibited until all PA2 sub-packets are independently
   accepted.

## 7. Global verification gate (per sub-packet)

```sh
cd companion-daemon
go build ./... || FAIL
go vet ./... || FAIL
go test -race ./internal/term ./cmd/devremote -count=1 || FAIL
gofmt -d [changed .go files]  # must produce no diff
git diff --check || FAIL
# Secret scan on changed files
# Invariant scans on changed files
```

Report mobile gate as `not-run` with reason when no mobile source
changes exist.
