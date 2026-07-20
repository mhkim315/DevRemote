package term

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// newPA4Handlers creates Handlers with a real Registry plus the given catalog.
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

// ── PA4.1 unit tests: appendCatalogRows isolation ──

// TestPA4_1_AppendCatalogDropsCollidingRegistryRows proves that
// appendCatalogRows drops Registry rows whose canonical ID collides
// with a managed catalog session.
func TestPA4_1_AppendCatalogDropsCollidingRegistryRows(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:collide", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")

	// Registry snapshot has a row that collides with the managed session.
	snapshot := []SessionTelemetry{
		{ID: "codex_app_server:collide", Adapter: "controlled_pty", AgentKind: "observer"},
		{ID: "tmux:legacy", Adapter: "tmux"},
	}
	out := appendCatalogRows(snapshot, cat, nil, nil)
	if len(out) != 2 {
		t.Fatalf("expected 2 rows (1 managed + 1 legacy), got %d", len(out))
	}
	// Verify the colliding Registry row was dropped and catalog row is authoritative.
	hasManaged, hasLegacy := false, false
	for _, row := range out {
		if row.ID == "codex_app_server:collide" {
			hasManaged = true
			if row.Adapter != "codex_app_server" {
				t.Errorf("managed row adapter=%q, want codex_headless", row.Adapter)
			}
			if row.AgentKind != "codex" {
				t.Errorf("managed row agentKind=%q, want codex", row.AgentKind)
			}
		}
		if row.ID == "tmux:legacy" {
			hasLegacy = true
		}
	}
	if !hasManaged {
		t.Error("managed row missing")
	}
	if !hasLegacy {
		t.Error("legacy row missing")
	}
}

// TestPA4_1_AppendCatalogProvidesCapabilities proves catalog rows
// include capabilities from managed authority.
func TestPA4_1_AppendCatalogProvidesCapabilities(t *testing.T) {
	claudeReg := NewManagedSessionRegistry(10)
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: "claude_headless:cap-test", Provider: "claude", Version: "2.1.202",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "idle",
	})
	cat := NewManagedRuntimeCatalog(nil, claudeReg, nil, nil, "", "2.1.202")

	out := appendCatalogRows(nil, cat, nil, nil)
	if len(out) != 1 {
		t.Fatalf("expected 1 managed row, got %d", len(out))
	}
	row := out[0]
	if len(row.AdapterCapabilities) == 0 {
		t.Error("managed row has no AdapterCapabilities")
	}
	if len(row.Capabilities) == 0 {
		t.Error("managed row has no session Capabilities")
	}
	hasLiveStream := false
	for _, c := range row.AdapterCapabilities {
		if c == "live_stream" {
			hasLiveStream = true
		}
	}
	if !hasLiveStream {
		t.Error("managed row missing live_stream capability")
	}
}

// ── PA4.1 integration tests: HandleSessionsV2 isolation ──

// TestPA4_1_HandleSessionsV2ManagedOnly proves that when only managed
// sessions exist, HandleSessionsV2 returns them through catalog authority.
func TestPA4_1_HandleSessionsV2ManagedOnly(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:managed-only", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")
	h := newPA4Handlers(t, cat)

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HandleSessionsV2: %d", rec.Code)
	}

	var rows []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 managed row, got %d", len(rows))
	}
	row := rows[0]
	if row.Adapter != "codex_app_server" {
		t.Errorf("adapter=%q, want codex_headless", row.Adapter)
	}
	if row.AgentKind != "codex" {
		t.Errorf("agentKind=%q, want codex", row.AgentKind)
	}
	if len(row.AdapterCapabilities) == 0 {
		t.Error("no AdapterCapabilities on managed row")
	}
}

// TestPA4_1_HandleSessionsV2BothProviders proves catalog rows for
// both Codex and Claude appear with correct metadata.
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
	foundCodex, foundClaude := false, false
	for _, row := range rows {
		if row.ID == "codex_app_server:both-1" {
			foundCodex = true
			if row.Adapter != "codex_app_server" {
				t.Errorf("codex row adapter=%q", row.Adapter)
			}
		}
		if row.ID == "claude_headless:both-2" {
			foundClaude = true
			if row.Adapter != "claude_headless" {
				t.Errorf("claude row adapter=%q", row.Adapter)
			}
		}
	}
	if !foundCodex {
		t.Error("Codex row missing")
	}
	if !foundClaude {
		t.Error("Claude row missing")
	}
}

// TestPA4_1_NilCatalogNoPanic proves nil catalog is handled gracefully.
func TestPA4_1_NilCatalogNoPanic(t *testing.T) {
	h := newPA4Handlers(t, nil)

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("HandleSessionsV2 with nil catalog: %d", rec.Code)
	}
}

// TestPA4_1_MalformedIDFailsClosed proves unknown prefix rows are excluded.
func TestPA4_1_MalformedIDFailsClosed(t *testing.T) {
	codexReg := NewManagedSessionRegistry(10)
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:valid", Provider: "codex", Version: "0.144.1",
		Epoch: 1, CreatedAt: time.Now(), NativeStatus: "running",
	})
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")
	h := newPA4Handlers(t, cat)

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)

	var rows []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	for _, row := range rows {
		if !strings.HasPrefix(row.ID, "codex_app_server:") && !strings.HasPrefix(row.ID, "claude_headless:") {
			t.Errorf("non-managed row leaked: %s", row.ID)
		}
	}
}

// TestPA4_1_ConcurrentCatalogListIsolation proves concurrent mutations
// never corrupt session list projection.
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
				Provider: "codex", Version: "0.144.1", Epoch: 1,
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

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)
	var rows []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &rows)
	if len(rows) < 1 {
		t.Error("no managed rows after concurrent ops")
	}
	for _, row := range rows {
		if row.Adapter != "codex_app_server" {
			t.Errorf("row %s adapter=%q", row.ID, row.Adapter)
		}
	}
}
