package term

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// PA3 Step 3: legacy query-parameter endpoints return 410 Gone.

func TestStep3_ActivityEndpointReturnsGone(t *testing.T) {
	reg, _ := mux.NewRegistry()
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()}

	req := httptest.NewRequest("GET", "/api/sessions?activity=test-session", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)

	if rec.Code != http.StatusGone {
		t.Errorf("?activity= returned %d, want %d (Gone)", rec.Code, http.StatusGone)
	}
}

func TestStep3_HistoryEndpointReturnsGone(t *testing.T) {
	reg, _ := mux.NewRegistry()
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()}

	req := httptest.NewRequest("GET", "/api/sessions?history=test-session", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)

	if rec.Code != http.StatusGone {
		t.Errorf("?history= returned %d, want %d (Gone)", rec.Code, http.StatusGone)
	}
}

func TestStep3_NormalListUnaffected(t *testing.T) {
	reg, _ := mux.NewRegistry()
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()}

	// Normal GET /api/sessions (no query params) must still work.
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/sessions returned %d, want 200", rec.Code)
	}
}
