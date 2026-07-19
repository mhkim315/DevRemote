package term

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// PA3 Step 3 R2: production-path fixture tests.
// Negative: legacy ?activity=/ ?history= → 410 Gone.
// Positive: supported Transcript API proves history available.

func newStep3Handlers(t *testing.T) *Handlers {
	t.Helper()
	reg := mux.MustNewRegistry(mux.NewTmuxAdapter())
	activity := NewActivityBuffer(100)
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	h := &Handlers{
		Registry:   reg,
		Events:     NewMemoryEventStore(),
		Activity:   activity,
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
	req := httptest.NewRequest("GET", "/api/sessions?history=tmux:test", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)
	if rec.Code != http.StatusGone {
		t.Errorf("?history= returned %d, want %d (Gone)", rec.Code, http.StatusGone)
	}
}

func TestStep3_NormalList_Returns200(t *testing.T) {
	h := newStep3Handlers(t)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsV2(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/sessions returned %d, want 200", rec.Code)
	}
}

// ── Positive control: Transcript API proves history available ──

func TestStep3_TranscriptAPI_HistoryAvailable(t *testing.T) {
	h := newStep3Handlers(t)

	// Feed bytes into the Transcript store via the byte-stream projector.
	sid := "tmux:step3-history"
	h.Transcript.FeedBytes(sid, []byte("line one\n"), time.Now())
	h.Transcript.FlushBytes(sid, time.Now())

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
	h.Transcript.EnableQueue(sid)
	h.Transcript.FeedBytes(sid, []byte("first output\n"), time.Now())
	h.Transcript.FeedBytes(sid, []byte("second output\n"), time.Now())
	h.Transcript.CloseSessionQueue(sid)

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
