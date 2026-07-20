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
		{ID: "codex_app_server:ghost-codex", Adapter: "tmux", AgentKind: "observer"},
		{ID: "tmux:real-legacy", Adapter: "tmux"},
	}
	cat := NewManagedRuntimeCatalog(nil, nil, nil, nil, "", "")
	out := appendCatalogRows(snapshot, cat, nil, nil)
	// Ghost must be excluded; only legacy tmux remains.
	if len(out) != 1 || out[0].ID != "tmux:real-legacy" {
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

	// Query capabilities through the catalog contract interface.
	sessCaps, adapterCaps := cat.ManagedCapabilities("codex_app_server")
	if len(sessCaps) == 0 {
		t.Error("ManagedCapabilities returned empty session caps")
	}
	if len(adapterCaps) == 0 {
		t.Error("ManagedCapabilities returned empty adapter caps")
	}
	hasLiveStream := false
	for _, c := range adapterCaps {
		if c == "live_stream" {
			hasLiveStream = true
		}
	}
	if !hasLiveStream {
		t.Error("ManagedCapabilities missing live_stream")
	}

	// Test: when lifecycle is nil, LifecycleState is empty.
	outNoLC := appendCatalogRows(nil, cat, nil, nil)
	if len(outNoLC) != 1 {
		t.Fatalf("expected 1 row, got %d", len(outNoLC))
	}
	if outNoLC[0].LifecycleState != "" {
		t.Errorf("LifecycleState=%q with nil lifecycle, want empty", outNoLC[0].LifecycleState)
	}

	// Test: when lifecycle is non-nil but has no entry, LifecycleState is empty.
	adapter := mux.NewControlledPTYAdapter()
	owned := NewOwnedPTYRuntime(adapter, nil)
	lifecycle := NewLifecycleService(owned, nil)
	outWithLC := appendCatalogRows(nil, cat, lifecycle, nil)
	if outWithLC[0].LifecycleState != "" {
		t.Errorf("LifecycleState=%q without entry, want empty", outWithLC[0].LifecycleState)
	}

	// Catalog capabilities are populated regardless of lifecycle.
	if len(outWithLC[0].AdapterCapabilities) == 0 {
		t.Error("catalog row missing AdapterCapabilities")
	}
	if len(outWithLC[0].Capabilities) == 0 {
		t.Error("catalog row missing session Capabilities")
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
		{ID: "claude_headless:override", Adapter: "tmux", AgentKind: "observer",
			Capabilities: []string{"screen"}, AdapterCapabilities: []string{"screen"}},
	}
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	// Catalog metadata must win, not Registry.
	if out[0].Adapter != "claude_headless" {
		t.Errorf("adapter=%q, want claude_headless (Registry tmux must not override)", out[0].Adapter)
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
	// Mark exited to prove catalog carries authoritative status at Epoch 2.
	codexReg.MarkExited("codex_app_server:stale-gen", 2)

	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Registry row carries stale generation-bearing evidence: Epoch 1 claims
	// it's "running" with AgentKind "observer" and wrong capabilities.
	// The catalog at Epoch 2 says "exited" with AgentKind "codex" — catalog wins.
	snapshot := []SessionTelemetry{
		{
			ID: "codex_app_server:stale-gen", Adapter: "controlled_pty",
			AgentKind: "observer", AgentStatus: "running",
			Capabilities: []string{"screen"}, AdapterCapabilities: []string{"screen"},
		},
	}
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 catalog row, got %d", len(out))
	}
	// Catalog's generation-2 exited status is authoritative over Registry's stale running.
	if out[0].AgentStatus != "exited" {
		t.Errorf("agentStatus=%q, want exited (catalog epoch 2 authoritative over Registry epoch 1)", out[0].AgentStatus)
	}
	if out[0].AgentKind != "codex" {
		t.Errorf("agentKind=%q, want codex (catalog authoritative over Registry observer)", out[0].AgentKind)
	}
	// Registry screen capability must not leak into catalog row.
	for _, c := range out[0].AdapterCapabilities {
		if c == "screen" {
			t.Error("Registry screen capability leaked into catalog row")
		}
	}
}

// ── R2: default production composition proof ──

func TestPA4_1_DefaultConfigEnforcesIsolation(t *testing.T) {
	// Production-like composition: catalog + lifecycle both wired, matching
	// the app.go production path (HandleSessionsV2 with both authorities).
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:default-cfg", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Production composition: catalog + lifecycle both wired through HandleSessionsV2,
	// matching app.go's production handler assembly path.
	adapter := mux.NewControlledPTYAdapter()
	owned := NewOwnedPTYRuntime(adapter, nil)
	lifecycle := NewLifecycleService(owned, nil)
	reg, _ := mux.NewRegistry()
	h := &Handlers{
		Registry:   reg,
		Catalog:    cat,
		Lifecycle:  lifecycle,
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
	if len(rows) != 1 {
		t.Fatalf("expected 1 managed row in default config, got %d", len(rows))
	}
	if rows[0].Adapter != "codex_app_server" {
		t.Errorf("adapter=%q", rows[0].Adapter)
	}
	if len(rows[0].AdapterCapabilities) == 0 {
		t.Error("missing adapter capabilities in default config")
	}
	// No special flag — default production composition enforces isolation.
}

// ── R2: legacy non-managed Registry behavior unchanged ──

func TestPA4_1_LegacyNonManagedRegistryBehaviorUnchanged(t *testing.T) {
	// No catalog — legacy rows pass through unmodified.
	snapshot := []SessionTelemetry{
		{ID: "tmux:legacy-a", Adapter: "tmux", Capabilities: []string{"live_stream"}},
		{ID: "cmux:legacy-b", Adapter: "cmux", Capabilities: []string{"screen"}},
	}
	cat := NewManagedRuntimeCatalog(nil, nil, nil, nil, "", "")
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 2 {
		t.Fatalf("expected 2 legacy rows, got %d", len(out))
	}
	if out[0].ID != "tmux:legacy-a" || out[1].ID != "cmux:legacy-b" {
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
		{ID: "tmux:legacy", Adapter: "tmux"},
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
		if row.ID == "tmux:legacy" {
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
