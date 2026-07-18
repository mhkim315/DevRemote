# PA0 — Legacy Consumer Inventory and Ownership Contract

Status: **REVIEW REQUEST**

This document fulfills the Packet A0 requirement from `POST_CLAUDE_MANAGED_ONLY_RESTRUCTURING_PLAN.md`. It inventories all production consumers of legacy constructs, assigns their target migration owners, and defines the frozen minimum interfaces for the new managed boundaries. 

No production code is modified in this phase.

## 1. Legacy Consumer Inventory

| Consumer / Package | Legacy Construct | Current Owner | Target Owner / Resolution | Migration Packet | Deletion Criterion |
| --- | --- | --- | --- | --- | --- |
| `cmd/devremote/app.go` | `*mux.Registry`, `LinkStore` | Global app state | `ManagedRuntimeCatalog` | A1 | No legacy store initialization |
| `internal/term/create.go` | `*mux.Registry` | Legacy registration | `ManagedRuntimeCatalog` | A1 | Register managed sessions to catalog only |
| `internal/term/ipc.go` (REST/IPC) | `*mux.Registry`, discovery | Session routing/lookup | `ManagedRuntimeCatalog` | A1, A2 | No legacy lookup or discovery imports |
| `internal/term/lifecycle_service.go` | `*mux.Registry` | Stop/kill routing | `ManagedRuntimeCatalog` & `ManagedRuntime` | A2 | Stop/kill operate on `ManagedRuntime` directly |
| `internal/term/telemetry_service.go` | `*mux.Registry`, adapter capabilities | Observer telemetry | Managed-only DTOs | A3 | Does not read screen/PTY text for semantic status |
| `internal/term/linker.go` | `LinkStore`, `*mux.Registry` | Manual session linking | **DELETED** | A2 | Managed sessions do not require external linking |
| `internal/mux/discovery.go` | `GetSessions()`, fs sockets | External discovery | **DELETED** | Phase B | Discovery mechanisms physically removed |
| `mobile/src/lib/client.ts` | external / best-effort branches | UI logic | **DELETED** | A3 | All sessions are managed and actionable |
| `mobile/src/screens/*.tsx` | cmux / best-effort warnings | UI presentation | **DELETED** | A3 | No cmux warnings or read-only fallback states |
| `internal/mux/...` | `tmux:`, `cmux:`, `localpty:` IDs | Legacy adapters | **DELETED** | Phase B | No external adapter code compiled or reachable |

## 2. Frozen Minimum Method Sets

The following interfaces represent the minimum required boundary between managed runtimes, catalog lookup, and terminal byte transport. These are derived from actual call sites and do not extract a generic provider SDK.

### `ManagedRuntimeCatalog`
Owns bounded lookup and listing of POKIT-created runtimes.

```go
type ManagedRuntimeCatalog interface {
	// Add registers a newly created managed runtime.
	Add(runtime ManagedRuntime) error
	
	// Remove removes the runtime from the catalog (e.g., on deletion).
	Remove(id string) error
	
	// List returns all active managed runtimes.
	List() []ManagedRuntime
	
	// Get retrieves a specific managed runtime by its canonical ID.
	Get(id string) (ManagedRuntime, bool)
}
```

### `ManagedRuntime`
Owns launch, identity, incarnation, provider version, and lifecycle.

```go
type ManagedRuntime interface {
	// Identity and provenance
	ID() string
	Generation() int
	Provider() string
	Version() string
	Workspace() string
	ProcessIdentity() (pid int, startTime time.Time)
	
	// Lifecycle operations
	Stop(ctx context.Context) error
	Kill(ctx context.Context) error
	Delete(ctx context.Context) error
}
```

### `TerminalTransport`
Owns raw byte input/output, resize, bounded live replay, and subscriber fan-out. It does not own semantic runtime state or approval authority.

```go
type TerminalTransport interface {
	// I/O operations
	Write(data []byte) (int, error)
	Resize(cols, rows int) error
	
	// Subscriber fan-out
	Subscribe() (ch <-chan []byte, unsubscribe func())
	
	// Bounded replay for late attach
	Replay() [][]byte
}
```

## 3. Exit Criteria for PA0
- This document is independently accepted.
- No production code has been modified.
- PA1 may commence upon ACCEPT.
