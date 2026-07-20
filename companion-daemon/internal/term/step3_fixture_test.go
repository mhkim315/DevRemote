package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// PA3 Step 3 R2: production-path fixture tests.
// Negative: legacy ?activity=/ ?history= → 410 Gone.
// Positive: supported Transcript API proves history available.

// step3Adapter is a deterministic in-memory adapter for Step 3 tests.
// Never depends on host cmux/cmux state or socket permissions.
type step3Adapter struct {
	sessions []mux.Session
}

func (a *step3Adapter) Name() string { return "step3" }
func (a *step3Adapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return a.sessions, nil
}

type step3Session struct{ id, title string }

func (s *step3Session) ID() string          { return s.id }
func (s *step3Session) Title() string       { return s.title }
func (s *step3Session) AdapterName() string { return "step3" }

func newStep3Handlers(t *testing.T) *Handlers {
	t.Helper()
	adapter := &step3Adapter{sessions: []mux.Session{
		&step3Session{id: "test-session", title: "Step 3 Test Session"},
	}}
	reg := mux.MustNewRegistry(adapter)
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	h := &Handlers{
		Registry:   reg,
		Transcript: svc,
	}
	t.Cleanup(func() { SetTranscriptService(nil) })
	return h
}

// ── Negative controls ──

func TestStep3_ActivityEndpoint_Returns410(t *testing.T) {
	h := newStep3Handlers(t)
	req := httptest.NewRequest("GET", "/api/sessions?activity=test", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)
	if rec.Code != http.StatusGone {
		t.Errorf("?activity= returned %d, want %d (Gone)", rec.Code, http.StatusGone)
	}
}

func TestStep3_HistoryEndpoint_Returns410(t *testing.T) {
	h := newStep3Handlers(t)
	req := httptest.NewRequest("GET", "/api/sessions?history=cmux:test", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)
	if rec.Code != http.StatusGone {
		t.Errorf("?history= returned %d, want %d (Gone)", rec.Code, http.StatusGone)
	}
}

func TestStep3_NormalList_Returns200(t *testing.T) {
	h := newStep3Handlers(t)

	// Deterministic fixture adapter (step3Adapter) provides a session row
	// independent of host cmux state or socket permissions.
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions returned %d, want 200", rec.Code)
	}

	// PA3 Step 4 R2: nonempty id/adapter row PRESENT.
	raw := rec.Body.String()
	if !strings.Contains(raw, `"id":"step3:test-session"`) {
		t.Error("deterministic session id not found in JSON")
	}
	for _, k := range []string{`"id"`, `"adapter"`} {
		if !strings.Contains(raw, k) {
			t.Errorf("required key %s must be present in JSON", k)
		}
	}
	// All five legacy keys ABSENT.
	for _, k := range []string{`"state"`, `"load"`, `"runner"`, `"runnerColor"`, `"events"`} {
		if strings.Contains(raw, k) {
			t.Errorf("legacy key %s must be absent from session list JSON", k)
		}
	}
}

// ── Positive control: Transcript API proves history available ──

func TestStep3_TranscriptAPI_HistoryAvailable(t *testing.T) {
	h := newStep3Handlers(t)

	// Feed bytes into the Transcript store via the byte-stream projector.
	sid := "cmux:step3-history"
	gen := h.Transcript.EnableQueue(sid)
	h.Transcript.FeedBytes(sid, []byte("line one\n"), time.Now(), gen)
	h.Transcript.CloseSessionQueue(sid, gen)

	// Query the supported Transcript API via direct handler call.
	req := httptest.NewRequest("GET", "/api/sessions/"+sid+"/transcript", nil)
	req.SetPathValue("id", sid)
	rec := httptest.NewRecorder()
	transcript.HandleTranscript(h.Transcript).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Transcript API returned %d, want 200", rec.Code)
	}

	var resp transcript.TranscriptResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid Transcript JSON: %v", err)
	}
	if len(resp.Semantic) == 0 {
		t.Fatal("Transcript semantic list is empty — history not available")
	}
	if resp.PrimarySource != transcript.SourceByteStream {
		t.Errorf("primarySource = %q, want byte_stream", resp.PrimarySource)
	}

	// Cleanup.
	h.Transcript.ClearTranscript(sid)
}

func TestStep3_TranscriptAPI_HistoryAvailable_AfterOutput(t *testing.T) {
	h := newStep3Handlers(t)
	sid := "controlled_pty:step3-output"

	// Simulate Recorder feeding bytes (the production byte-stream path).
	gen := h.Transcript.EnableQueue(sid)
	h.Transcript.FeedBytes(sid, []byte("first output\n"), time.Now(), gen)
	h.Transcript.FeedBytes(sid, []byte("second output\n"), time.Now(), gen)
	h.Transcript.CloseSessionQueue(sid, gen)

	req := httptest.NewRequest("GET", "/api/sessions/"+sid+"/transcript?limit=50", nil)
	req.SetPathValue("id", sid)
	rec := httptest.NewRecorder()
	transcript.HandleTranscript(h.Transcript).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Transcript API: %d", rec.Code)
	}
	var resp transcript.TranscriptResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Semantic) < 2 {
		t.Fatalf("semantic segments = %d, want >= 2", len(resp.Semantic))
	}

	h.Transcript.ClearTranscript(sid)
}
