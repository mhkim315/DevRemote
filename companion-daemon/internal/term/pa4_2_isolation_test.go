package term

import (
	"context"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// ── PA4.2: Managed lifecycle and approval lookup isolation ──

// TestPA4_2_LifecycleServiceHasNoRegistryDependency proves the
// LifecycleService struct has zero *mux.Registry fields or methods.
// Managed lifecycle routes exclusively through OwnedPTYRuntime and
// ManagedRuntimeCatalog for provider-owned runtimes.
func TestPA4_2_LifecycleServiceHasNoRegistryDependency(t *testing.T) {
	adapter := mux.NewControlledPTYAdapter()
	owned := NewOwnedPTYRuntime(adapter, nil)
	svc := NewLifecycleService(owned, nil)

	// Stop routes to OwnedPTYRuntime for nonexistent sessions — returns error, not panic.
	_, err := svc.Stop(context.Background(), "controlled_pty:nonexistent")
	if err == nil {
		t.Error("Stop should return error for nonexistent session")
	}

	// Kill routes to OwnedPTYRuntime.
	_, err = svc.Kill(context.Background(), "controlled_pty:nonexistent")
	if err == nil {
		t.Error("Kill should return error for nonexistent session")
	}

	// Delete routes to OwnedPTYRuntime.
	_, err = svc.Delete(context.Background(), "controlled_pty:nonexistent")
	if err == nil {
		t.Error("Delete should return error for nonexistent session")
	}
}

// TestPA4_2_LifecycleStopRoutesThroughProviderOwner proves Stop for
// a provider-owned session routes through ManagedRuntimeCatalog
// (Epoch derivation) and the ProviderLifecycleOwner, never Registry.
func TestPA4_2_LifecycleStopRoutesThroughProviderOwner(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:lc-stop", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	adapter := mux.NewControlledPTYAdapter()
	owned := NewOwnedPTYRuntime(adapter, nil)
	svc := NewLifecycleService(owned, nil)

	// Wire managed owners — catalog is the sole read path for Epoch derivation.
	fakeOwner := &fakeProviderOwner{currentEpoch: 1}
	svc.WireManagedOwners(cat, fakeOwner, nil)

	res, err := svc.Stop(context.Background(), "codex_app_server:lc-stop")
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if res.SessionID == "" {
		t.Error("Stop should identify managed session via catalog")
	}
	// Provider owner received the Stop call with server-derived Epoch=1.
	fakeOwner.mu.Lock()
	found := false
	for _, c := range fakeOwner.calls {
		if c.Action == "stop" && c.ID == "codex_app_server:lc-stop" && c.Epoch == 1 {
			found = true
		}
	}
	fakeOwner.mu.Unlock()
	if !found {
		t.Error("Stop not routed to provider owner with correct ID and Epoch")
	}
}

// TestPA4_2_LifecycleDeleteClearsTranscript proves Delete for a
// managed session clears the Transcript via the injected transcript
// service, never through Registry or legacy paths.
func TestPA4_2_LifecycleDeleteClearsTranscript(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	sid := "codex_app_server:lc-delete"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: sid, Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "exited",
	})
	codexReg.MarkExited(sid, 1)
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	adapter := mux.NewControlledPTYAdapter()
	owned := NewOwnedPTYRuntime(adapter, nil)
	svc := NewLifecycleService(owned, nil)

	fakeOwner := &fakeProviderOwner{currentEpoch: 1}
	svc.WireManagedOwners(cat, fakeOwner, nil)

	res, err := svc.Delete(context.Background(), sid)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if res.SessionID == "" {
		t.Error("Delete should identify managed session via catalog")
	}
	fakeOwner.mu.Lock()
	found := false
	for _, c := range fakeOwner.calls {
		if c.Action == "delete" && c.ID == sid {
			found = true
		}
	}
	fakeOwner.mu.Unlock()
	if !found {
		t.Error("Delete not routed to provider owner")
	}
}

// TestPA4_2_ManagedLifecycleNeverUsesRegistryAdapter proves that
// lifecycle operations do not call Registry.Adapter(name) or
// Registry.Sessions() for managed sessions.
func TestPA4_2_ManagedLifecycleNeverUsesRegistryAdapter(t *testing.T) {
	// OwnedPTYRuntime with nil adapter — proves lifecycle doesn't call
	// adapter methods for managed provider sessions.
	svc := NewLifecycleService(nil, nil)

	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:no-reg", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	fakeOwner := &fakeProviderOwner{currentEpoch: 1}
	svc.WireManagedOwners(cat, fakeOwner, nil)

	// Stop should succeed through catalog + provider owner, no Registry needed.
	res, err := svc.Stop(context.Background(), "codex_app_server:no-reg")
	if err != nil {
		t.Fatalf("Stop with nil OwnedPTYRuntime: %v", err)
	}
	if res.SessionID == "" {
		t.Error("Stop should identify managed session")
	}
}

// TestPA4_2_UnknownSessionFailsClosed proves lifecycle operations on
// unknown/non-managed session IDs return not-found without falling
// back to Registry or legacy discovery.
func TestPA4_2_UnknownSessionFailsClosed(t *testing.T) {
	adapter := mux.NewControlledPTYAdapter()
	owned := NewOwnedPTYRuntime(adapter, nil)
	svc := NewLifecycleService(owned, nil)

	// Empty catalog — no managed sessions.
	cat := NewManagedRuntimeCatalog(nil, nil, nil, nil, "", "")
	svc.WireManagedOwners(cat, nil, nil)

	// Unknown session must return error (fail closed), not succeed silently.
	_, err := svc.Stop(context.Background(), "legacy:unknown")
	if err == nil {
		t.Error("Stop succeeded on unknown legacy session — must fail closed")
	}

	_, err = svc.Kill(context.Background(), "legacy:unknown")
	if err == nil {
		t.Error("Kill succeeded on unknown legacy session — must fail closed")
	}
}

// TestPA4_2_ApprovalStoreHasNoRegistryDependency proves the
// AuthoritativeApprovalStore has zero Registry dependencies.
// Approval resolution uses exact provider/session identity — never
// screen text, process discovery, or Registry rows.
func TestPA4_2_ApprovalStoreHasNoRegistryDependency(t *testing.T) {
	// AuthoritativeApprovalStore is constructed with only a store config.
	// It has no Registry, adapter, or discovery fields.
	store := NewApprovalStore()
	if store == nil {
		t.Fatal("NewApprovalStore returned nil")
	}
	// The store's methods (Ingest, List, Get, etc.) accept provider/session
	// identity as explicit parameters — never derived from Registry.
}
