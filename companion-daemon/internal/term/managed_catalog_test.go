package term

import (
	"context"
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

// ── Test 3b: malformed stored records are filtered from Get/List ──

func TestCatalog_MalformedRecordsFiltered(t *testing.T) {
	// Register records that violate the canonical identity contract
	// directly into raw registries, then verify the catalog filters them.
	codexReg := NewManagedSessionRegistry(16)
	claudeReg := NewManagedSessionRegistry(16)

	now := time.Now()

	malformed := []ManagedSessionRecord{
		// Empty local ID (adapter with no local part = canonical parse gives empty LocalID).
		{SessionID: "codex_app_server:", Provider: "codex", Version: "0.144.1", Epoch: 1, CreatedAt: now},
		// Wrong adapter prefix in Codex registry.
		{SessionID: "claude_headless:wrong-origin", Provider: "codex", Version: "0.144.1", Epoch: 1, CreatedAt: now},
		// Empty provider.
		{SessionID: "codex_app_server:no-provider", Provider: "", Version: "0.144.1", Epoch: 1, CreatedAt: now},
		// Empty version.
		{SessionID: "codex_app_server:no-version", Provider: "codex", Version: "", Epoch: 1, CreatedAt: now},
	}
	// Also register one valid record as a positive control.
	validID := "codex_app_server:valid"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: validID, Provider: "codex", Version: "0.144.1", Epoch: 1, CreatedAt: now,
	})

	// Register malformed records in codex registry.
	for _, rec := range malformed {
		_ = codexReg.Register(rec)
	}

	cat := NewManagedRuntimeCatalog(codexReg, claudeReg, nil, nil, "", "")

	// Get: malformed IDs return not found.
	for _, rec := range malformed {
		if _, ok := cat.Get(rec.SessionID); ok {
			t.Errorf("Get(%q) returned true for malformed record", rec.SessionID)
		}
	}
	// Get: valid ID works.
	if _, ok := cat.Get(validID); !ok {
		t.Error("Get on valid ID failed")
	}

	// List: only the valid record appears.
	records := cat.List()
	if len(records) != 1 {
		t.Fatalf("List: expected 1 valid record, got %d: %v", len(records), records)
	}
	if records[0].SessionID != validID {
		t.Fatalf("List: expected %q, got %q", validID, records[0].SessionID)
	}
}

// ── Test 4: duplicate/ambiguous ID fails closed (Get + List) ──

func TestCatalog_Get_AmbiguousDuplicateFailsClosed(t *testing.T) {
	// Use a raw constructor to put the same ID in both registries.
	codexReg := NewManagedSessionRegistry(8)
	claudeReg := NewManagedSessionRegistry(8)

	now := time.Now()
	dupID := "codex_app_server:ambiguous-1"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: dupID, Provider: "codex", Version: "0.144.1", Epoch: 1,
		CreatedAt: now,
	})
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: dupID, Provider: "claude", Version: "2.1.209", Epoch: 1,
		CreatedAt: now,
	})

	// Also register a clean non-duplicate ID in each registry.
	cleanCodex := "codex_app_server:clean"
	cleanClaude := "claude_headless:clean"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: cleanCodex, Provider: "codex", Version: "0.144.1", Epoch: 1,
		CreatedAt: now,
	})
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: cleanClaude, Provider: "claude", Version: "2.1.209", Epoch: 1,
		CreatedAt: now,
	})

	cat := NewManagedRuntimeCatalog(codexReg, claudeReg, nil, nil, "", "")

	// Get must fail closed for the ambiguous ID.
	if _, ok := cat.Get(dupID); ok {
		t.Fatal("Get returned true for ambiguous duplicate ID — should fail closed")
	}

	// Get must succeed for clean IDs.
	if rec, ok := cat.Get(cleanCodex); !ok || rec.SessionID != cleanCodex {
		t.Fatalf("Get on clean codex ID failed: ok=%v rec=%+v", ok, rec)
	}
	if rec, ok := cat.Get(cleanClaude); !ok || rec.SessionID != cleanClaude {
		t.Fatalf("Get on clean claude ID failed: ok=%v rec=%+v", ok, rec)
	}

	// List: ambiguous ID must be ENTIRELY EXCLUDED — neither copy appears.
	records := cat.List()
	for _, rec := range records {
		if rec.SessionID == dupID {
			t.Fatalf("List included ambiguous ID %q — should be excluded entirely", dupID)
		}
	}

	// List: clean IDs must appear.
	found := map[string]bool{}
	for _, rec := range records {
		found[rec.SessionID] = true
	}
	if !found[cleanCodex] {
		t.Fatal("List missing clean codex ID")
	}
	if !found[cleanClaude] {
		t.Fatal("List missing clean claude ID")
	}
	if len(records) != 2 {
		t.Fatalf("List: expected 2 clean records, got %d: %v", len(records), records)
	}
}

// List ambiguous drop-both: when the same SessionID exists in both
// registries, List must drop both copies — not silently choose one.
func TestCatalog_List_AmbiguousDropBoth(t *testing.T) {
	codexReg := NewManagedSessionRegistry(8)
	claudeReg := NewManagedSessionRegistry(8)
	now := time.Now()

	// Register the SAME canonical ID in both registries — an adversarial
	// condition that should never happen in production (different adapter
	// prefixes) but the catalog must handle safely.
	dupID := "codex_app_server:dup"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: dupID, Provider: "codex", Version: "0.144.1", Epoch: 1, CreatedAt: now,
	})
	// Register in Claude registry too — this record has a mismatched adapter
	// prefix (codex_app_server in claude registry), but the catalog must still
	// detect the cross-registry duplicate and drop BOTH copies.
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: dupID, Provider: "codex", Version: "0.144.1", Epoch: 1, CreatedAt: now,
	})

	// One clean record in each registry with correct adapter prefixes.
	clean1 := "codex_app_server:c1"
	clean2 := "claude_headless:c2"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: clean1, Provider: "codex", Version: "0.144.1", Epoch: 1, CreatedAt: now,
	})
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: clean2, Provider: "claude", Version: "2.1.209", Epoch: 1, CreatedAt: now,
	})

	cat := NewManagedRuntimeCatalog(codexReg, claudeReg, nil, nil, "", "")
	records := cat.List()

	// Ambiguous must be absent — both copies dropped.
	for _, rec := range records {
		if rec.SessionID == dupID {
			t.Fatalf("ambiguous ID %q leaked into List", dupID)
		}
	}
	// Clean records present.
	found := map[string]bool{}
	for _, rec := range records {
		found[rec.SessionID] = true
	}
	if !found[clean1] || !found[clean2] {
		t.Fatalf("clean records missing: found=%v", found)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 clean records, got %d: %v", len(records), records)
	}
}

// ── Test 5: blocking/failing legacy Registry cannot be invoked by managed paths ──

// blockingAdapter blocks forever on ListSessions — if any managed path
// accidentally calls it, the test hangs and fails via the go test timeout.
type blockingAdapter struct{ name string }

func (a blockingAdapter) Name() string { return a.name }
func (a blockingAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestCatalog_LegacyRegistryIsolation(t *testing.T) {
	// Build a legacy registry with a blocking adapter. Any access to
	// registry.Sessions / registry.FindSession would hang.
	reg := mux.MustNewRegistry(
		blockingAdapter{name: "tmux"},
		failingAdapter{name: "cmux"},
	)
	cat, managed, id := catalogWithCodex(t)

	// Build a handler with BOTH catalog and a blocking legacy registry.
	h := &Handlers{
		Registry: reg,
		Events:   NewMemoryEventStore(),
		Managed:  managed,
		Catalog:  cat,
	}

	// Start a background goroutine that would trigger the blocking
	// registry. If any managed path touches it, this will hang.
	blockingDone := make(chan struct{})
	go func() {
		// This would block forever if called, but we never await it.
		_ = reg.Sessions(context.Background())
		close(blockingDone)
	}()

	// Use a deterministic barrier: managed paths must complete within a
	// bounded time while the blocking goroutine is still pending.
	done := make(chan struct{})
	go func() {
		// managed list via catalog — must not touch legacy registry.
		rec := httptest.NewRecorder()
		h.HandleManagedSessions(rec, httptest.NewRequest("GET", "/api/managed-sessions", nil))
		if rec.Code != 200 {
			t.Errorf("managed list: code=%d body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), id) {
			t.Error("managed list missing catalog row")
		}

		// native-status via catalog — must not touch legacy registry.
		req := httptest.NewRequest("GET", "/api/sessions/x/native-status", nil)
		req.SetPathValue("id", id)
		rec2 := httptest.NewRecorder()
		h.HandleManagedNativeStatus(rec2, req)
		if rec2.Code != 200 {
			t.Errorf("native-status: code=%d", rec2.Code)
		}

		// /api/sessions via catalog append — must not fall through to legacy.
		rec3 := httptest.NewRecorder()
		h.HandleSessionsV2(rec3, httptest.NewRequest("GET", "/api/sessions", nil))
		if rec3.Code != 200 {
			t.Errorf("sessions v2: code=%d", rec3.Code)
		}
		if !strings.Contains(rec3.Body.String(), id) {
			t.Error("sessions v2 missing catalog row")
		}
		close(done)
	}()

	// Managed paths must complete within 2 seconds.
	select {
	case <-done:
		// PASS — managed paths completed without touching blocking registry.
	case <-time.After(2 * time.Second):
		t.Fatal("managed list/get/status blocked — may have touched legacy registry")
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

	cat := NewManagedRuntimeCatalog(managed.Registry(), nil, rtFunc, nil, "0.144.1", "")

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
	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "", "")
	snapshot := []SessionTelemetry{{ID: "tmux:0", State: "idle", Adapter: "tmux"}}
	out := appendCatalogRows(snapshot, cat, nil)
	if len(out) != 1 || out[0].ID != "tmux:0" {
		t.Fatalf("empty catalog should pass through: %+v", out)
	}
}

// ── Test: RuntimeOf fails closed when ID is ambiguous ──

func TestCatalog_RuntimeOf_AmbiguousFailsClosed(t *testing.T) {
	codexReg := NewManagedSessionRegistry(8)
	claudeReg := NewManagedSessionRegistry(8)
	now := time.Now()

	dupID := "codex_app_server:rt-ambiguous"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: dupID, Provider: "codex", Version: "0.144.1", Epoch: 1, CreatedAt: now,
	})
	// Same ID in Claude registry — the adapter prefix is wrong for Claude,
	// but the presence in both registries makes it ambiguous.
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: dupID, Provider: "codex", Version: "0.144.1", Epoch: 1, CreatedAt: now,
	})

	// Stub resolvers: they would succeed if called, but RuntimeOf must
	// reject the ambiguous ID before delegating.
	codexRT := func(sid string) (RuntimeRef, bool) {
		return RuntimeRef{Adapter: "codex_app_server", Version: "0.144.1", LaunchGen: 1}, true
	}
	claudeRT := func(sid string) (RuntimeRef, bool) {
		return RuntimeRef{Adapter: "claude_headless", Version: "2.1.209", LaunchGen: 1}, true
	}

	cat := NewManagedRuntimeCatalog(codexReg, claudeReg, codexRT, claudeRT, "0.144.1", "2.1.209")

	// RuntimeOf must fail for the ambiguous ID (Get rejects it).
	if _, ok := cat.RuntimeOf(dupID); ok {
		t.Fatal("RuntimeOf returned true for ambiguous ID — should fail closed")
	}

	// A clean ID with a valid resolver still works.
	cleanID := "codex_app_server:rt-clean"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: cleanID, Provider: "codex", Version: "0.144.1", Epoch: 1, CreatedAt: now,
	})
	ref, ok := cat.RuntimeOf(cleanID)
	if !ok {
		t.Fatal("RuntimeOf returned false for clean ID with valid resolver")
	}
	if ref.Adapter != "codex_app_server" {
		t.Fatalf("RuntimeOf.Adapter = %q", ref.Adapter)
	}
}

// ── Test: RuntimeOf rejects mismatched RuntimeRef binding ──

func TestCatalog_RuntimeOf_BindingMismatch(t *testing.T) {
	codexReg := NewManagedSessionRegistry(8)
	now := time.Now()
	sid := "codex_app_server:binding-test"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: sid, Provider: "codex", Version: "0.144.1", Epoch: 3, CreatedAt: now,
	})

	// Resolver returns wrong Version (mismatched authority version).
	codexRT := func(s string) (RuntimeRef, bool) {
		return RuntimeRef{Adapter: "codex_app_server", Version: "0.999.0", LaunchGen: 3}, true
	}

	cat := NewManagedRuntimeCatalog(codexReg, nil, codexRT, nil, "0.144.1", "")
	if _, ok := cat.RuntimeOf(sid); ok {
		t.Fatal("RuntimeOf accepted mismatched Version")
	}

	// Resolver returns wrong Adapter.
	codexRT2 := func(s string) (RuntimeRef, bool) {
		return RuntimeRef{Adapter: "claude_headless", Version: "0.144.1", LaunchGen: 3}, true
	}
	cat2 := NewManagedRuntimeCatalog(codexReg, nil, codexRT2, nil, "0.144.1", "")
	if _, ok := cat2.RuntimeOf(sid); ok {
		t.Fatal("RuntimeOf accepted mismatched Adapter")
	}
}

// ── Test: provider identity mutations are rejected ──

func TestCatalog_ProviderIdentityMutations(t *testing.T) {
	codexReg := NewManagedSessionRegistry(16)
	claudeReg := NewManagedSessionRegistry(16)
	now := time.Now()

	// Valid records as positive controls.
	validCodex := "codex_app_server:valid-c"
	validClaude := "claude_headless:valid-cl"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: validCodex, Provider: "codex", Version: "0.144.1", Epoch: 1, CreatedAt: now,
	})
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: validClaude, Provider: "claude", Version: "2.1.209", Epoch: 1, CreatedAt: now,
	})

	// Codex registry: cross-provider mutation.
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:cross-prov", Provider: "claude", Version: "0.144.1", Epoch: 1, CreatedAt: now,
	})
	// Codex registry: empty provider.
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: "codex_app_server:empty-prov", Provider: "", Version: "0.144.1", Epoch: 1, CreatedAt: now,
	})
	// Claude registry: cross-provider mutation.
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: "claude_headless:cross-prov", Provider: "codex", Version: "2.1.209", Epoch: 1, CreatedAt: now,
	})
	// Claude registry: case-mutated provider ("Claude" vs "claude").
	_ = claudeReg.Register(ManagedSessionRecord{
		SessionID: "claude_headless:case-mut", Provider: "Claude", Version: "2.1.209", Epoch: 1, CreatedAt: now,
	})

	cat := NewManagedRuntimeCatalog(codexReg, claudeReg, nil, nil, "0.144.1", "2.1.209")

	// Get: all mutated records return not found.
	for _, sid := range []string{"codex_app_server:cross-prov", "codex_app_server:empty-prov", "claude_headless:cross-prov", "claude_headless:case-mut"} {
		if _, ok := cat.Get(sid); ok {
			t.Errorf("Get(%q) returned true for provider-mutated record", sid)
		}
	}

	// Get: valid records work.
	if _, ok := cat.Get(validCodex); !ok {
		t.Error("Get on valid codex record failed")
	}
	if _, ok := cat.Get(validClaude); !ok {
		t.Error("Get on valid claude record failed")
	}

	// List: only valid records appear.
	recs := cat.List()
	if len(recs) != 2 {
		t.Fatalf("List: expected 2 valid records, got %d: %v", len(recs), recs)
	}
	for _, rec := range recs {
		if rec.SessionID != validCodex && rec.SessionID != validClaude {
			t.Errorf("List included mutated record: %s", rec.SessionID)
		}
	}

	// RuntimeOf: mutated records fail (Get path rejects them).
	for _, sid := range []string{"codex_app_server:cross-prov", "codex_app_server:empty-prov"} {
		if _, ok := cat.RuntimeOf(sid); ok {
			t.Errorf("RuntimeOf(%q) returned true for provider-mutated record", sid)
		}
	}
}

// ── Test: version-only mismatch is rejected by RuntimeOf ──

func TestCatalog_RuntimeOf_VersionMismatch(t *testing.T) {
	codexReg := NewManagedSessionRegistry(8)
	now := time.Now()
	sid := "codex_app_server:ver-test"
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: sid, Provider: "codex", Version: "codex-cli 0.144.1", Epoch: 2, CreatedAt: now,
	})

	// Resolver has correct Adapter and LaunchGen but wrong Version.
	codexRT := func(s string) (RuntimeRef, bool) {
		return RuntimeRef{Adapter: "codex_app_server", Version: "0.999.0", LaunchGen: 2}, true
	}

	cat := NewManagedRuntimeCatalog(codexReg, nil, codexRT, nil, "0.144.1", "")
	if _, ok := cat.RuntimeOf(sid); ok {
		t.Fatal("RuntimeOf accepted version mismatch")
	}

	// Resolver with correct version works.
	codexRT2 := func(s string) (RuntimeRef, bool) {
		return RuntimeRef{Adapter: "codex_app_server", Version: "0.144.1", LaunchGen: 2}, true
	}
	cat2 := NewManagedRuntimeCatalog(codexReg, nil, codexRT2, nil, "0.144.1", "")
	if _, ok := cat2.RuntimeOf(sid); !ok {
		t.Fatal("RuntimeOf rejected correct version")
	}
}

// ── Test: empty/malformed versions are rejected ──

func TestCatalog_EmptyVersionRejected(t *testing.T) {
	codexReg := NewManagedSessionRegistry(8)
	now := time.Now()
	sid := "codex_app_server:empty-ver"
	// Empty version in record.
	_ = codexReg.Register(ManagedSessionRecord{
		SessionID: sid, Provider: "codex", Version: "", Epoch: 1, CreatedAt: now,
	})

	cat := NewManagedRuntimeCatalog(codexReg, nil, nil, nil, "0.144.1", "")
	if _, ok := cat.Get(sid); ok {
		t.Error("Get returned true for empty-version record")
	}
	if len(cat.List()) != 0 {
		t.Error("List included empty-version record")
	}
	if _, ok := cat.RuntimeOf(sid); ok {
		t.Error("RuntimeOf returned true for empty-version record")
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
