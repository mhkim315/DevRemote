package term

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// ── SP0-P3: REST reads from the owned registry + observer isolation ──

// failingAdapter simulates a broken tmux/cmux backend: discovery always errors.
type failingAdapter struct{ name string }

func (a failingAdapter) Name() string { return a.name }
func (a failingAdapter) ListSessions(context.Context) ([]mux.Session, error) {
	return nil, fmt.Errorf("%s: refresh failed", a.name)
}

// spoof rows are crafted inline in tests below to collide with a managed
// canonical ID.

func createManagedForAPI(t *testing.T) (*ManagedCodexService, string) {
	t.Helper()
	fl := &fakeLauncher{handler: happyAppServer("thread-API")}
	managed := newTestManagedService(fl)
	id, err := managed.CreateDetached("")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	return managed, id
}

// TestManagedREST_ListAndGet_FromOwnedRegistry: the authenticated list
// endpoint carries the managed row sourced from the owned registry, and the
// native-status endpoint retrieves the same session by canonical ID.
func TestManagedREST_ListAndGet_FromOwnedRegistry(t *testing.T) {
	managed, id := createManagedForAPI(t)
	h := &Handlers{Registry: mux.MustNewRegistry(), Events: NewMemoryEventStore(), Managed: managed}

	// List (production handler).
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, httptest.NewRequest("GET", "/api/sessions", nil))
	var rows []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	var found *SessionTelemetry
	for i := range rows {
		if rows[i].ID == id {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatalf("managed session %s missing from list: %s", id, rec.Body.String())
	}
	if found.Adapter != "codex_app_server" || found.State != string(ManagedStatusIdle) {
		t.Fatalf("managed row = %+v", found)
	}

	// Get status (production handler, path value).
	req := httptest.NewRequest("GET", "/api/sessions/x/native-status", nil)
	req.SetPathValue("id", id)
	rec2 := httptest.NewRecorder()
	h.HandleManagedNativeStatus(rec2, req)
	if rec2.Code != 200 {
		t.Fatalf("native-status code = %d body=%s", rec2.Code, rec2.Body.String())
	}
	var dto ManagedNativeStatusDTO
	if err := json.Unmarshal(rec2.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode dto: %v", err)
	}
	if dto.ID != id || dto.Provider != "codex" || dto.NativeStatus != "idle" || dto.LaunchGen != 1 {
		t.Fatalf("dto = %+v", dto)
	}

	// Unknown ID → 404; disabled service → 404.
	req404 := httptest.NewRequest("GET", "/api/sessions/x/native-status", nil)
	req404.SetPathValue("id", "codex_app_server:nope")
	rec3 := httptest.NewRecorder()
	h.HandleManagedNativeStatus(rec3, req404)
	if rec3.Code != 404 {
		t.Fatalf("unknown id code = %d", rec3.Code)
	}
	hOff := &Handlers{Registry: mux.MustNewRegistry(), Events: NewMemoryEventStore()}
	rec4 := httptest.NewRecorder()
	hOff.HandleManagedNativeStatus(rec4, req)
	if rec4.Code != 404 {
		t.Fatalf("disabled service code = %d", rec4.Code)
	}
}

// TestManagedREST_DTOBounded: the native-status response carries EXACTLY the
// bounded field set — no prompts, command text, payloads, paths, thread/turn
// identities, or process details.
func TestManagedREST_DTOBounded(t *testing.T) {
	managed, id := createManagedForAPI(t)
	h := &Handlers{Registry: mux.MustNewRegistry(), Events: NewMemoryEventStore(), Managed: managed}

	req := httptest.NewRequest("GET", "/api/sessions/x/native-status", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	h.HandleManagedNativeStatus(rec, req)

	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]bool{
		"id": true, "provider": true, "version": true, "nativeStatus": true,
		"launchGen": true, "createdAt": true, "statusChangedAt": true, "exited": true,
	}
	if len(raw) != len(want) {
		t.Fatalf("field count = %d, want %d: %v", len(raw), len(want), raw)
	}
	for k := range raw {
		if !want[k] {
			t.Fatalf("unexpected DTO field %q", k)
		}
	}
	body := rec.Body.String()
	for _, leak := range []string{"proc-", "threadId", "thread-API", "turnId", "/Users", certificationPrompt[:20], "processId", "pid"} {
		if strings.Contains(body, leak) {
			t.Fatalf("DTO leaks %q: %s", leak, body)
		}
	}
}

// TestManagedREST_SpoofedDiscoveryCannotOverwrite: a discovery adapter that
// fabricates a session with the SAME canonical ID cannot shadow or overwrite
// the managed row — the owned-registry status stays authoritative and only
// one row is served.
func TestManagedREST_SpoofedDiscoveryCannotOverwrite(t *testing.T) {
	managed, id := createManagedForAPI(t)
	// Adversarial: appendManagedRows receives a snapshot row colliding with
	// the managed canonical ID and claiming a contradictory state.
	spoofed := []SessionTelemetry{{ID: id, State: "exited", Adapter: "codex_app_server"}}
	out := appendManagedRows(spoofed, managed)

	count := 0
	for _, row := range out {
		if row.ID == id {
			count++
			if row.State != string(ManagedStatusIdle) {
				t.Fatalf("spoofed row overwrote managed status: %+v", row)
			}
		}
	}
	if count != 1 {
		t.Fatalf("managed ID served %d times, want exactly 1", count)
	}

	// Known-bad control: with NO managed authority the spoofed row passes
	// through untouched (the dedup is managed-authority, not blanket filtering).
	pass := appendManagedRows(spoofed, nil)
	if len(pass) != 1 || pass[0].State != "exited" {
		t.Fatalf("control failed: %+v", pass)
	}
}

// TestManagedREST_FailingDiscoveryIsolation: failing tmux/cmux-style adapters
// (and a spoofing one) do not affect managed create/list/get/status.
func TestManagedREST_FailingDiscoveryIsolation(t *testing.T) {
	managed, id := createManagedForAPI(t)
	reg := mux.MustNewRegistry(
		failingAdapter{name: "tmux"},
		failingAdapter{name: "cmux"},
	)
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Managed: managed}

	// List still serves the managed row despite failing discovery.
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, httptest.NewRequest("GET", "/api/sessions", nil))
	if !strings.Contains(rec.Body.String(), id) {
		t.Fatalf("managed row missing when discovery fails: %s", rec.Body.String())
	}

	// Status still 200 with the owned-registry value.
	req := httptest.NewRequest("GET", "/api/sessions/x/native-status", nil)
	req.SetPathValue("id", id)
	rec2 := httptest.NewRecorder()
	h.HandleManagedNativeStatus(rec2, req)
	if rec2.Code != 200 || !strings.Contains(rec2.Body.String(), `"nativeStatus":"idle"`) {
		t.Fatalf("native-status with failing discovery: code=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// A second managed create is untouched by the broken registry (the
	// managed path never consults mux.Registry).
	if _, err := managed.CreateDetached(""); err != nil {
		t.Fatalf("managed create affected by failing discovery: %v", err)
	}
}
