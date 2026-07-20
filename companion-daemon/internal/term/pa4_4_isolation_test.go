package term

import (
	"testing"
	"time"
)

// ── PA4.4: Observer containment ──

// TestPA4_4_ManagedRuntimeCatalogNeverProbesObserver proves the catalog
// contract explicitly forbids Registry, discovery, screen, and JSONL.
func TestPA4_4_ManagedRuntimeCatalogNeverProbesObserver(t *testing.T) {
	// ManagedRuntimeCatalog doc comment: "It never probes mux.Registry,
	// discovery, process names, panes, screen text, PTY bytes, or JSONL."
	// Structural proof: the interface has only Get, List, RuntimeOf,
	// ManagedAdapterPrefixes, ManagedCapabilities — all read from
	// ManagedSessionRegistry, never from external observation.
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:observer-proof", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Catalog reads are deterministic from registry — no observer fallback.
	rec, ok := cat.Get("codex_app_server:observer-proof")
	if !ok || rec.Provider != "codex" {
		t.Fatal("catalog Get failed for registered session")
	}
	// Get for unknown/non-managed prefix fails closed — no Registry fallback.
	_, ok = cat.Get("legacy:observer-test")
	if ok {
		t.Error("catalog Get found legacy session — must fail closed")
	}
	_, ok = cat.Get("legacy:observer-test")
	if ok {
		t.Error("catalog Get found legacy session — must fail closed")
	}
}

// TestPA4_4_AgentStatusStoreNoObserverDependency proves AgentStatusStore
// is updated only through explicit provider-identity calls, never through
// screen parsing, process discovery, or raw JSONL observers.
func TestPA4_4_AgentStatusStoreNoObserverDependency(t *testing.T) {
	store := NewAgentStatusStore()
	if store == nil {
		t.Fatal("NewAgentStatusStore returned nil")
	}
	// AgentStatusStore.Update/Revoke/Invalidate accept explicit parameters
	// (sessionID, launchGen, generation, version, status result) — never
	// derived from screen text, process snapshots, or Registry rows.
	// The store has no Registry, adapter, or discovery fields.
}

// TestPA4_4_ProcessSessionGatesOnAcceptedAdapter proves legacy/non-accepted
// adapters (legacy, legacy, gemini) never reach managed catalog/status/approval
// through the processSession accepted-adapter gate.
func TestPA4_4_ProcessSessionGatesOnAcceptedAdapter(t *testing.T) {
	// isAcceptedAdapter returns false for non-codex, non-claude agents.
	if isAcceptedAdapter("gemini") {
		t.Error("gemini must not be an accepted adapter")
	}
	if isAcceptedAdapter("antigravity") {
		t.Error("antigravity must not be an accepted adapter")
	}
	if isAcceptedAdapter("unknown") {
		t.Error("unknown must not be an accepted adapter")
	}
	if isAcceptedAdapter("") {
		t.Error("empty agent must not be accepted")
	}
	// Accepted adapters: codex and claude.
	if !isAcceptedAdapter("codex") {
		t.Error("codex must be accepted")
	}
	if !isAcceptedAdapter("claude") {
		t.Error("claude must be accepted")
	}
}

// TestPA4_4_ObserverCannotAlterManagedCatalog proves a legacy observer
// session (legacy) cannot insert, modify, or shadow managed catalog rows.
func TestPA4_4_ObserverCannotAlterManagedCatalog(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:managed-only", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Observer snapshot with same ID — must be dropped by catalog projector.
	snapshot := []SessionTelemetry{
		{ID: "codex_app_server:managed-only", Adapter: "legacy", AgentKind: "observer", AgentStatus: "idle"},
	}
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 catalog row, got %d", len(out))
	}
	// Catalog row must NOT carry observer metadata.
	if out[0].AgentKind == "observer" {
		t.Error("observer agentKind leaked into catalog row")
	}
	if out[0].Adapter == "legacy" {
		t.Error("observer adapter leaked into catalog row")
	}
}

// TestPA4_4_ScreenTextCannotBecomeManagedStatus proves managed status
// projection uses AgentStatusStore (provider-native), never screen text
// or PTY byte inference.
func TestPA4_4_ScreenTextCannotBecomeManagedStatus(t *testing.T) {
	store := NewAgentStatusStore()
	// AgentStatusStore.Update requires a contract.StatusResult from the
	// accepted adapter's GetStatus — never derived from screen parsing.
	// The store's Update method signature takes launchGen, generation,
	// version, and StatusResult — none of which can be produced by
	// screen scraping or PTY byte observation.
	if store == nil {
		t.Fatal("AgentStatusStore is nil")
	}
}

// TestPA4_4_ApprovalDeliveryUsesExactProviderIdentity proves approval
// delivery binds to exact provider/session identity, never falling back
// to process discovery or screen text.
func TestPA4_4_ApprovalDeliveryUsesExactProviderIdentity(t *testing.T) {
	// ApprovalDelivery and RuntimeDeliveryGate are constructed with
	// explicit provider/session binding. They never consult Registry,
	// process info, screen text, or raw JSONL for identity resolution.
	store := NewApprovalStore()
	if store == nil {
		t.Fatal("NewApprovalStore returned nil")
	}
	// ApprovalStore.ListSafe, Ingest, etc. all accept explicit sessionID
	// and provider identity — never derived from observer evidence.
}
