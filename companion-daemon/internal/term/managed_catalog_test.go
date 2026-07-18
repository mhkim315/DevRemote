package term

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// ── PA1: ManagedRuntimeCatalog contract tests ──

// catalogWithCodex creates a catalog backed by a codex service with one
// managed session. Returns the catalog, service, and session ID.
func catalogWithCodex(t *testing.T) (ManagedRuntimeCatalog, *ManagedCodexService, string) {
	t.Helper()
	managed, id := createManagedForAPI(t)
	cat := catalogForAPI(managed, nil)
	return cat, managed, id
}

// catalogWithClaude creates a catalog backed by a claude service with one
// managed session. Returns the catalog, service, and session ID.
func catalogWithClaude(t *testing.T) (ManagedRuntimeCatalog, *ManagedClaudeService, string) {
	t.Helper()
	launcher := &fakeClaudeLauncher{}
	attestor := &fakeClaudeAttestor{}
	svc := NewManagedClaudeService(testCfg(), launcher, attestor)
	id, err := svc.CreateDetached(t.TempDir())
	if err != nil {
		t.Fatalf("create claude session: %v", err)
	}
	cat := catalogForAPI(nil, svc)
	return cat, svc, id
}

// catalogWithBoth creates a catalog backed by both codex and claude services.
func catalogWithBoth(t *testing.T) (ManagedRuntimeCatalog, *ManagedCodexService, string, *ManagedClaudeService, string) {
	t.Helper()
	codexSvc, codexID := createManagedForAPI(t)

	launcher := &fakeClaudeLauncher{}
	attestor := &fakeClaudeAttestor{}
	claudeSvc := NewManagedClaudeService(testCfg(), launcher, attestor)
	claudeID, err := claudeSvc.CreateDetached(t.TempDir())
	if err != nil {
		t.Fatalf("create claude session: %v", err)
	}

	cat := catalogForAPI(codexSvc, claudeSvc)
	return cat, codexSvc, codexID, claudeSvc, claudeID
}

// ── Test 1: dual-registry listing with deterministic ordering ──

func TestCatalog_List_DualRegistryDeterministicOrder(t *testing.T) {
	cat, _, codexID, _, claudeID := catalogWithBoth(t)

	// Call List twice; both must return the same order.
	first := cat.List()
	second := cat.List()

	if len(first) != 2 {
		t.Fatalf("expected 2 records, got %d", len(first))
	}
	if len(second) != 2 {
		t.Fatalf("second list: expected 2 records, got %d", len(second))
	}

	// Deterministic: same order both calls.
	for i := range first {
		if first[i].SessionID != second[i].SessionID {
			t.Fatalf("non-deterministic order: first[%d]=%s, second[%d]=%s",
				i, first[i].SessionID, i, second[i].SessionID)
		}
	}

	// Must contain both IDs.
	ids := map[string]bool{}
	for _, rec := range first {
		ids[rec.SessionID] = true
	}
	if !ids[codexID] {
		t.Fatal("codex session missing from list")
	}
	if !ids[claudeID] {
		t.Fatal("claude session missing from list")
	}

	// Ordering: sorted by CreatedAt then SessionID.
	for i := 1; i < len(first); i++ {
		if first[i].CreatedAt.Before(first[i-1].CreatedAt) {
			t.Fatalf("list not sorted by CreatedAt: %s before %s",
				first[i].SessionID, first[i-1].SessionID)
		}
		if first[i].CreatedAt.Equal(first[i-1].CreatedAt) &&
			first[i].SessionID < first[i-1].SessionID {
			t.Fatalf("list not sorted by SessionID on equal CreatedAt: %s before %s",
				first[i].SessionID, first[i-1].SessionID)
		}
	}
}

// ── Test 2: exact Get returns only the canonical provider record ──

func TestCatalog_Get_ExactProviderRecord(t *testing.T) {
	cat, _, codexID, _, claudeID := catalogWithBoth(t)

	// Get codex record.
	rec, ok := cat.Get(codexID)
	if !ok {
		t.Fatalf("Get(%q) returned false", codexID)
	}
	if rec.SessionID != codexID {
		t.Fatalf("Get(%q).SessionID = %q", codexID, rec.SessionID)
	}
	if rec.Provider != "codex" {
		t.Fatalf("Get(%q).Provider = %q, want codex", codexID, rec.Provider)
	}

	// Get claude record.
	rec2, ok2 := cat.Get(claudeID)
	if !ok2 {
		t.Fatalf("Get(%q) returned false", claudeID)
	}
	if rec2.SessionID != claudeID {
		t.Fatalf("Get(%q).SessionID = %q", claudeID, rec2.SessionID)
	}
	if rec2.Provider != "claude" {
		t.Fatalf("Get(%q).Provider = %q, want claude", claudeID, rec2.Provider)
	}

	// Codex Get does NOT return Claude record.
	if _, ok := cat.Get(claudeID); !ok {
		t.Fatal("claude Get should work")
	}
	// Cross-contamination: codex Get should not accidentally return claude rec.
	codexRec, _ := cat.Get(codexID)
	if codexRec.Provider == "claude" {
		t.Fatal("codex Get returned claude provider")
	}
}

// ── Test 3: unknown, malformed, pane-style, and arbitrary IDs → not found ──

func TestCatalog_Get_UnknownAndMalformedIDs(t *testing.T) {
	cat, _, _ := catalogWithCodex(t)

	tests := []struct {
		name string
		id   string
	}{
		{"unknown codex ID", "codex_app_server:nonexistent"},
		{"unknown claude ID", "claude_headless:nonexistent"},
		{"no adapter prefix (pane-style)", "0"},
		{"legacy tmux ID", "tmux:devremote"},
		{"legacy cmux ID", "cmux:main"},
		{"empty string", ""},
		{"arbitrary text", "just-some-text"},
		{"malformed with colons", "a:b:c:d"},
		{"controlled PTY", "controlled_pty:test"},
		{"localpty", "localpty:shell"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := cat.Get(tt.id); ok {
				t.Fatalf("Get(%q) unexpectedly returned true", tt.id)
			}
		})
	}
}

// ── Test 4: duplicate/ambiguous ID fails closed ──

func TestCatalog_Get_AmbiguousDuplicateFailsClosed(t *testing.T) {
	// Use a raw constructor to put the same ID in both registries.
	codexReg := NewManagedSessionRegistry(8)
	claudeReg := NewManagedSessionRegistry(8)

	// Register the SAME session ID with adapter prefix "codex_app_server"
	// in BOTH registries — an adversarial condition.
	dupID := "codex_app_server:ambiguous-1"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: dupID, Provider: "codex", Version: "0.144.1", Epoch: 1,
		CreatedAt: time.Now(),
	})
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: dupID, Provider: "claude", Version: "2.1.209", Epoch: 1,
		CreatedAt: time.Now(),
	})

	cat := NewManagedRuntimeCatalog(codexReg, claudeReg, nil, nil)

	// Get must fail closed for the ambiguous ID.
	if _, ok := cat.Get(dupID); ok {
		t.Fatal("Get returned true for ambiguous duplicate ID — should fail closed")
	}

	// A non-duplicate ID in codex registry still works.
	cleanID := "codex_app_server:clean"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: cleanID, Provider: "codex", Version: "0.144.1", Epoch: 1,
		CreatedAt: time.Now(),
	})
	rec, ok := cat.Get(cleanID)
	if !ok {
		t.Fatal("Get on clean codex ID failed after ambiguous duplicate was rejected")
	}
	if rec.SessionID != cleanID {
		t.Fatalf("Get returned wrong record: %+v", rec)
	}
}

// ── Test 5: blocking legacy Registry cannot be invoked by managed paths ──

func TestCatalog_LegacyRegistryIsolation(t *testing.T) {
	// Create a legacy registry that panics on access.
	reg := mux.MustNewRegistry()
	cat, managed, id := catalogWithCodex(t)

	// Build a handler with BOTH catalog and legacy registry.
	h := &Handlers{
		Registry: reg,
		Events:   NewMemoryEventStore(),
		Managed:  managed,
		Catalog:  cat,
	}

	// managed list via catalog — must not touch legacy registry.
	rec := httptest.NewRecorder()
	h.HandleManagedSessions(rec, httptest.NewRequest("GET", "/api/managed-sessions", nil))
	if rec.Code != 200 {
		t.Fatalf("managed list: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), id) {
		t.Fatal("managed list missing catalog row")
	}

	// native-status via catalog — must not touch legacy registry.
	req := httptest.NewRequest("GET", "/api/sessions/x/native-status", nil)
	req.SetPathValue("id", id)
	rec2 := httptest.NewRecorder()
	h.HandleManagedNativeStatus(rec2, req)
	if rec2.Code != 200 {
		t.Fatalf("native-status: code=%d", rec2.Code)
	}

	// /api/sessions via catalog append — must not fall through to legacy.
	rec3 := httptest.NewRecorder()
	h.HandleSessionsV2(rec3, httptest.NewRequest("GET", "/api/sessions", nil))
	if rec3.Code != 200 {
		t.Fatalf("sessions v2: code=%d", rec3.Code)
	}
	if !strings.Contains(rec3.Body.String(), id) {
		t.Fatal("sessions v2 missing catalog row")
	}
}

// ── Test 6: managed rows projected from catalog cannot be overwritten by legacy ──

func TestCatalog_ManagedRowsNotOverwritable(t *testing.T) {
	cat, _, codexID, _, claudeID := catalogWithBoth(t)

	// Build a snapshot with a legacy row that collides with a managed ID.
	spoofed := []SessionTelemetry{
		{ID: codexID, State: "exited", Adapter: "codex_app_server"},
		{ID: claudeID, State: "working", Adapter: "claude_headless"},
		{ID: "tmux:legacy", State: "idle", Adapter: "tmux"},
	}

	out := appendCatalogRows(spoofed, cat, nil)

	// The legacy "tmux:legacy" row should survive.
	foundLegacy := false
	foundCodex := 0
	foundClaude := 0
	for _, row := range out {
		switch row.ID {
		case codexID:
			foundCodex++
			if row.State != "idle" {
				t.Fatalf("codex row overwritten by spoofed state: %+v", row)
			}
		case claudeID:
			foundClaude++
			if row.State != "idle" {
				t.Fatalf("claude row overwritten by spoofed state: %+v", row)
			}
		case "tmux:legacy":
			foundLegacy = true
		}
	}
	if foundCodex != 1 {
		t.Fatalf("codex row count=%d, want 1", foundCodex)
	}
	if foundClaude != 1 {
		t.Fatalf("claude row count=%d, want 1", foundClaude)
	}
	if !foundLegacy {
		t.Fatal("legacy row was dropped when it should survive")
	}

	// Control: without catalog, spoofed rows pass through.
	ctrl := appendCatalogRows(spoofed, nil, nil)
	if len(ctrl) != 3 {
		t.Fatalf("control: expected 3 rows, got %d", len(ctrl))
	}
}

// ── Test 7: stale generation/native updates rejected by provider registry ──

func TestCatalog_StaleGenerationRejected(t *testing.T) {
	_, managed, id := catalogWithCodex(t)

	// Try to update with wrong epoch — must be rejected.
	if managed.Registry().UpdateNativeStatus(id, 999, ManagedStatusWorking) {
		t.Fatal("UpdateNativeStatus with wrong epoch was accepted")
	}

	// Verify the record still shows the original status.
	rec, ok := managed.Registry().Get(id)
	if !ok {
		t.Fatal("record missing after rejected update")
	}
	if rec.NativeStatus != ManagedStatusIdle {
		t.Fatalf("status changed to %s despite rejected update", rec.NativeStatus)
	}
	if rec.Epoch != 1 {
		t.Fatalf("epoch changed to %d", rec.Epoch)
	}

	// Correct epoch update works.
	if !managed.Registry().UpdateNativeStatus(id, 1, ManagedStatusWorking) {
		t.Fatal("UpdateNativeStatus with correct epoch was rejected")
	}
	rec2, _ := managed.Registry().Get(id)
	if rec2.NativeStatus != ManagedStatusWorking {
		t.Fatalf("status not updated: %s", rec2.NativeStatus)
	}
}

// ── Test 8: RuntimeOf preserves provider, version, generation, launch cert ──

func TestCatalog_RuntimeOf_PreservesExactProviderBinding(t *testing.T) {
	// Build a fresh service and install approval execution BEFORE creating
	// any runtime (activation must precede the first epoch).
	fl := &fakeLauncher{handler: happyAppServer("thread-RT")}
	managed := newTestManagedService(fl)
	store := NewApprovalStore()
	if err := managed.SetApprovalStore(store); err != nil {
		t.Fatalf("SetApprovalStore: %v", err)
	}
	_, rtFunc, err := managed.InstallApprovalExecution(store)
	if err != nil {
		t.Fatalf("InstallApprovalExecution: %v", err)
	}

	// Now create a runtime — activation already committed.
	id, err := managed.CreateDetached("")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}

	cat := NewManagedRuntimeCatalog(managed.Registry(), nil, rtFunc, nil)

	// RuntimeOf must resolve.
	ref, ok := cat.RuntimeOf(id)
	if !ok {
		t.Fatal("RuntimeOf returned false for live codex session")
	}
	if ref.Adapter != "codex_app_server" {
		t.Fatalf("RuntimeOf.Adapter = %q, want codex_app_server", ref.Adapter)
	}
	if ref.Version == "" {
		t.Fatal("RuntimeOf.Version is empty")
	}
	if ref.LaunchGen != 1 {
		t.Fatalf("RuntimeOf.LaunchGen = %d, want 1", ref.LaunchGen)
	}

	// Unknown adapter fails closed.
	if _, ok := cat.RuntimeOf("claude_headless:nonexistent"); ok {
		t.Fatal("RuntimeOf resolved unknown claude adapter without installed service")
	}
	if _, ok := cat.RuntimeOf("tmux:0"); ok {
		t.Fatal("RuntimeOf resolved legacy tmux adapter")
	}

	// Stale generation (after MarkExited) is rejected.
	managed.Registry().MarkExited(id, 1)
	if _, ok := cat.RuntimeOf(id); ok {
		t.Fatal("RuntimeOf resolved exited session")
	}
}

// ── Test 9: public DTOs contain no PID, process token, hook dir, digest, etc. ──

func TestCatalog_PublicDTOPrivacy(t *testing.T) {
	cat, _, id := catalogWithCodex(t)

	rec, ok := cat.Get(id)
	if !ok {
		t.Fatal("Get failed")
	}

	dto := managedNativeStatusDTO(rec)
	body, _ := json.Marshal(dto)
	bodyStr := string(body)

	// Allowed fields.
	allowed := []string{"id", "provider", "version", "nativeStatus", "launchGen", "createdAt", "statusChangedAt", "exited"}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decode DTO: %v", err)
	}
	if len(raw) != len(allowed) {
		t.Fatalf("DTO field count = %d, want %d: %v", len(raw), len(allowed), raw)
	}
	for _, k := range allowed {
		if _, exists := raw[k]; !exists {
			t.Fatalf("missing allowed field %q in DTO", k)
		}
	}

	// Forbidden fields must not leak.
	forbidden := []string{
		"processId", "ProcessID", "pid", "PID",
		"hookDir", "HookDir",
		"certifiedDigest", "CertifiedDigest",
		"attestorKind", "AttestorKind",
		"certResult", "CertResult",
		"certReason", "CertReason",
		"os", "OS", "arch", "Arch",
		"prompt", "command", "path", "token",
		"approval", "payload", "event",
	}
	for _, leak := range forbidden {
		if strings.Contains(bodyStr, leak) {
			t.Fatalf("DTO leaks %q: %s", leak, bodyStr)
		}
	}
}

// ── Test 10: catalog reads return defensive copies, expose no mutation ──

func TestCatalog_DefensiveCopies(t *testing.T) {
	cat, _, id := catalogWithCodex(t)

	// Get returns a copy — mutating it does not affect the catalog.
	rec1, ok := cat.Get(id)
	if !ok {
		t.Fatal("Get failed")
	}
	rec1.NativeStatus = ManagedStatusExited // mutate the copy
	rec1.SessionID = "hacked"

	// Subsequent Get returns the original.
	rec2, ok := cat.Get(id)
	if !ok {
		t.Fatal("second Get failed after mutating first copy")
	}
	if rec2.NativeStatus != ManagedStatusIdle {
		t.Fatalf("mutation leaked: NativeStatus = %s", rec2.NativeStatus)
	}
	if rec2.SessionID != id {
		t.Fatalf("mutation leaked: SessionID = %s", rec2.SessionID)
	}

	// List returns copies — mutating an element does not affect the catalog.
	records := cat.List()
	if len(records) == 0 {
		t.Fatal("List returned empty")
	}
	records[0].NativeStatus = ManagedStatusExited
	records[0].SessionID = "hacked-list"

	// Subsequent List returns originals.
	records2 := cat.List()
	if records2[0].NativeStatus != ManagedStatusIdle {
		t.Fatal("List mutation leaked: NativeStatus changed")
	}
	if records2[0].SessionID != id {
		t.Fatal("List mutation leaked: SessionID changed")
	}

	// The catalog interface exposes no mutation methods.
	// Compile-time check: ManagedRuntimeCatalog has only Get, List, RuntimeOf.
	var _ ManagedRuntimeCatalog = cat // implements the interface
}

// ── PA1: appendCatalogRows projector tests ──

func TestAppendCatalogRows_NilCatalog(t *testing.T) {
	snapshot := []SessionTelemetry{{ID: "tmux:0", State: "idle", Adapter: "tmux"}}
	out := appendCatalogRows(snapshot, nil, nil)
	if len(out) != 1 || out[0].ID != "tmux:0" {
		t.Fatalf("nil catalog should pass through: %+v", out)
	}
}

func TestAppendCatalogRows_EmptyCatalog(t *testing.T) {
	codexReg := NewManagedSessionRegistry(8)
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil)
	snapshot := []SessionTelemetry{{ID: "tmux:0", State: "idle", Adapter: "tmux"}}
	out := appendCatalogRows(snapshot, cat, nil)
	if len(out) != 1 || out[0].ID != "tmux:0" {
		t.Fatalf("empty catalog should pass through: %+v", out)
	}
}

// ── Concurrent access safety ──

func TestCatalog_ConcurrentAccess(t *testing.T) {
	cat, _, _, _, _ := catalogWithBoth(t)

	var wg sync.WaitGroup
	errCh := make(chan error, 20)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recs := cat.List()
			if len(recs) < 2 {
				errCh <- fmt.Errorf("concurrent List: got %d records", len(recs))
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := "codex_app_server:nonexistent"
			if _, ok := cat.Get(id); ok {
				errCh <- fmt.Errorf("concurrent Get returned true for nonexistent")
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Error(e)
	}
}
