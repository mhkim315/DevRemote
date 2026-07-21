//go:build legacy

package term

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

func newPA4Handlers(t *testing.T, cat ManagedRuntimeCatalog) *Handlers {
	t.Helper()
	reg, err := mux.NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return &Handlers{
		Registry:   reg,
		Catalog:    cat,
		Transcript: transcript.NewService(transcript.DefaultStoreConfig()),
	}
}

// ── R2 ghost-prevention: managed-prefix Registry rows excluded ──

func TestPA4_1_RegistryCodexPrefixGhostExcluded(t *testing.T) {
	// Registry-only Codex-looking row with no catalog record.
	snapshot := []SessionTelemetry{
		{ID: "codex_app_server:ghost-codex", Adapter: "legacy", AgentKind: "observer"},
		{ID: "legacy:real-legacy", Adapter: "legacy"},
	}
	cat := NewManagedRuntimeCatalog(nil, nil, nil, nil, "", "")
	out := appendCatalogRows(snapshot, cat, nil, nil)
	// Ghost must be excluded; only legacy legacy remains.
	if len(out) != 1 || out[0].ID != "legacy:real-legacy" {
		t.Fatalf("expected 1 legacy row, got %d: %+v", len(out), out)
	}
}

func TestPA4_1_RegistryClaudePrefixGhostExcluded(t *testing.T) {
	snapshot := []SessionTelemetry{
		{ID: "claude_headless:ghost-claude", Adapter: "controlled_pty", AgentKind: "observer"},
	}
	claudeReg := NewManagedSessionRegistry(10)
	cat := NewManagedRuntimeCatalog(nil, claudeReg, nil, nil, "", "2.1.202")
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 0 {
		t.Fatalf("expected 0 rows (ghost excluded), got %d", len(out))
	}
}

func TestPA4_1_RegistryControlledPTYPrefixGhostExcluded(t *testing.T) {
	// controlled_pty: prefix with managed-looking ID but no catalog record.
	// Catalog has no codex/claude registries, so ManagedAdapterPrefixes is empty.
	// Legacy controlled_pty rows without managed prefix should survive.
	snapshot := []SessionTelemetry{
		{ID: "controlled_pty:real-ctl", Adapter: "controlled_pty"},
	}
	cat := NewManagedRuntimeCatalog(nil, nil, nil, nil, "", "")
	out := appendCatalogRows(snapshot, cat, nil, nil)
	// controlled_pty is NOT a managed prefix (only codex_app_server/claude_headless are).
	// So the row survives as a normal legacy row.
	if len(out) != 1 || out[0].ID != "controlled_pty:real-ctl" {
		t.Fatalf("expected 1 legacy controlled_pty row, got %d", len(out))
	}
}

// ── R2: catalog-carried capabilities and lifecycle ──

func TestPA4_1_CatalogRowCarriesCapabilitiesAndLifecycle(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:carry", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Capabilities come from catalog contract, not Registry.
	sessCaps, adapterCaps := cat.ManagedCapabilities("codex_app_server")
	if len(sessCaps) != 2 || len(adapterCaps) != 3 {
		t.Errorf("capabilities: session=%v adapter=%v", sessCaps, adapterCaps)
	}

	// Lifecycle integration: nil lifecycle → empty LifecycleState.
	outNoLC := appendCatalogRows(nil, cat, nil, nil)
	if outNoLC[0].LifecycleState != "" {
		t.Errorf("LifecycleState=%q with nil lifecycle, want empty", outNoLC[0].LifecycleState)
	}

	// Lifecycle integration: non-nil lifecycle without entry → empty.
	adapter := mux.NewControlledPTYAdapter()
	owned := NewOwnedPTYRuntime(launcherWrapper(adapter), nil)
	lifecycle := NewLifecycleService(owned, nil)
	outNoEntry := appendCatalogRows(nil, cat, lifecycle, nil)
	if outNoEntry[0].LifecycleState != "" {
		t.Errorf("LifecycleState=%q without entry, want empty", outNoEntry[0].LifecycleState)
	}

	// POSITIVE: lifecycle WITH entry → LifecycleState populated.
	// Use same-package access to insert a CatalogEntry directly.
	owned.entries["codex_app_server:carry"] = &CatalogEntry{
		State: LifecycleRunning,
	}
	outWithEntry := appendCatalogRows(nil, cat, lifecycle, nil)
	if outWithEntry[0].LifecycleState != string(LifecycleRunning) {
		t.Errorf("LifecycleState=%q with entry, want %q",
			outWithEntry[0].LifecycleState, LifecycleRunning)
	}

	// Catalog capabilities populated regardless of lifecycle state.
	if len(outWithEntry[0].AdapterCapabilities) == 0 || len(outWithEntry[0].Capabilities) == 0 {
		t.Error("catalog row missing capabilities")
	}
}

// ── R2: Registry metadata cannot override catalog ──

func TestPA4_1_RegistryMetadataCannotOverrideCatalog(t *testing.T) {
	claudeReg := NewManagedSessionRegistry(10)
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: "claude_headless:override", Provider: "claude", Version: "2.1.202",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "thinking",
	})
	cat := NewManagedRuntimeCatalog(nil, claudeReg, nil, nil, "", "2.1.202")

	// Registry has a colliding row with wrong metadata.
	snapshot := []SessionTelemetry{
		{ID: "claude_headless:override", Adapter: "legacy", AgentKind: "observer",
			Capabilities: []string{"screen"}, AdapterCapabilities: []string{"screen"}},
	}
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	// Catalog metadata must win, not Registry.
	if out[0].Adapter != "claude_headless" {
		t.Errorf("adapter=%q, want claude_headless (Registry legacy must not override)", out[0].Adapter)
	}
	if out[0].AgentKind != "claude" {
		t.Errorf("agentKind=%q, want claude", out[0].AgentKind)
	}
	for _, c := range out[0].AdapterCapabilities {
		if c == "screen" {
			t.Error("Registry screen capability leaked into catalog row")
		}
	}
}

// ── R2: stale/replaced generation not resurrected ──

func TestPA4_1_StaleGenerationNotResurrectedThroughRegistry(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:stale-gen", Provider: "codex", Version: "0.144.1",
		Epoch: 2, CreatedAt: time.Now(),
	})
	// ManagedRuntimeCatalog generation gate: MarkExited at correct Epoch 2 succeeds.
	if !codexReg.MarkExited("codex_app_server:stale-gen", 2) {
		t.Fatal("MarkExited at correct Epoch 2 failed")
	}
	// Stale Epoch 0 is rejected — catalog generation check is authoritative.
	if codexReg.MarkExited("codex_app_server:stale-gen", 0) {
		t.Error("MarkExited at stale Epoch 0 succeeded — generation gate broken")
	}

	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Registry row carries wrong metadata — catalog at Epoch 2 is authoritative.
	snapshot := []SessionTelemetry{
		{
			ID: "codex_app_server:stale-gen", Adapter: "controlled_pty",
			AgentKind: "observer", AgentStatus: "running",
			Capabilities: []string{"screen"}, AdapterCapabilities: []string{"screen"},
		},
	}
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 1 || out[0].AgentStatus != "exited" {
		t.Fatalf("expected 1 exited catalog row, got %d, status=%q", len(out), out[0].AgentStatus)
	}
	if out[0].AgentKind != "codex" {
		t.Errorf("agentKind=%q, want codex", out[0].AgentKind)
	}
	for _, c := range out[0].AdapterCapabilities {
		if c == "screen" {
			t.Error("Registry screen capability leaked into catalog row")
		}
	}
}

// ── R2: default production composition proof ──

func TestPA4_1_DefaultConfigEnforcesIsolation(t *testing.T) {
	// Production default: catalog + lifecycle + Registry all wired through
	// HandleSessionsV2, matching app.go's handler assembly. No special flag.
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:default-cfg", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")
	adapter := mux.NewControlledPTYAdapter()
	owned := NewOwnedPTYRuntime(launcherWrapper(adapter), nil)
	lifecycle := NewLifecycleService(owned, nil)
	reg, _ := mux.NewRegistry()
	h := &Handlers{
		Registry: reg, Catalog: cat, Lifecycle: lifecycle,
		Transcript: transcript.NewService(transcript.DefaultStoreConfig()),
	}

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HandleSessionsV2: %d", rec.Code)
	}

	var rows []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 || rows[0].Adapter != "codex_app_server" || len(rows[0].AdapterCapabilities) == 0 {
		t.Fatalf("default config: got %d rows, adapter=%q caps=%v", len(rows), rows[0].Adapter, rows[0].AdapterCapabilities)
	}
}

// ── R2: legacy non-managed Registry behavior unchanged ──

func TestPA4_1_LegacyNonManagedRegistryBehaviorUnchanged(t *testing.T) {
	// No catalog — legacy rows pass through unmodified.
	snapshot := []SessionTelemetry{
		{ID: "legacy:legacy-a", Adapter: "legacy", Capabilities: []string{"live_stream"}},
		{ID: "legacy:legacy-b", Adapter: "legacy", Capabilities: []string{"screen"}},
	}
	cat := NewManagedRuntimeCatalog(nil, nil, nil, nil, "", "")
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 2 {
		t.Fatalf("expected 2 legacy rows, got %d", len(out))
	}
	if out[0].ID != "legacy:legacy-a" || out[1].ID != "legacy:legacy-b" {
		t.Error("legacy rows modified")
	}
}

// ── Existing positive tests ──

func TestPA4_1_AppendCatalogDropsCollidingRegistryRows(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:collide", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")
	snapshot := []SessionTelemetry{
		{ID: "codex_app_server:collide", Adapter: "controlled_pty", AgentKind: "observer"},
		{ID: "legacy:legacy", Adapter: "legacy"},
	}
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 2 {
		t.Fatalf("expected 2 rows (1 managed + 1 legacy), got %d", len(out))
	}
	hasManaged, hasLegacy := false, false
	for _, row := range out {
		if row.ID == "codex_app_server:collide" {
			hasManaged = true
			if row.Adapter != "codex_app_server" {
				t.Errorf("adapter=%q, want codex_app_server", row.Adapter)
			}
		}
		if row.ID == "legacy:legacy" {
			hasLegacy = true
		}
	}
	if !hasManaged || !hasLegacy {
		t.Error("missing expected rows")
	}
}

func TestPA4_1_HandleSessionsV2BothProviders(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:both-1", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	claudeReg := NewManagedSessionRegistry(10)
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: "claude_headless:both-2", Provider: "claude", Version: "2.1.202",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "thinking",
	})
	cat := NewManagedRuntimeCatalog(codexReg, claudeReg, nil, nil, "0.144.1", "2.1.202")
	h := newPA4Handlers(t, cat)

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)

	var rows []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
}

func TestPA4_1_NilCatalogNoPanic(t *testing.T) {
	h := newPA4Handlers(t, nil)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HandleSessionsV2 with nil catalog: %d", rec.Code)
	}
}

func TestPA4_1_ConcurrentCatalogListIsolation(t *testing.T) {
	codexReg := NewManagedSessionRegistry(100)
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")
	h := newPA4Handlers(t, cat)

	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:base", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = codexReg.Register(ManagedSessionRecord{
				SessionID: fmt.Sprintf("codex_app_server:c-%d", n),
				Provider:  "codex", Version: "0.144.1", Epoch: 1,
				CreatedAt: time.Now(), NativeStatus: "running",
			})
		}(i)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/sessions", nil)
			rec := httptest.NewRecorder()
			h.HandleSessionsV2(rec, req)
			if rec.Code != http.StatusOK {
				errs <- fmt.Errorf("HandleSessionsV2: %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
}
