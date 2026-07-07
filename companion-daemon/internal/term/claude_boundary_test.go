package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/mux"
)

// Phase A5 product-boundary bridge: Claude adapter output flows
// through production telemetry path (TelemetryService + HandleSessionsV2).

func TestClaudeOutput_TelemetryPath(t *testing.T) {
	reg := mux.MustNewRegistry(&claudeSessionAdapter{})

	// Production bridge: evidence-based detection (no hardcoded process names).
	detector := agent.NewTermAgentDetector()

	svc := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), NoopNotifier{}, detector)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go svc.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-svc.Done()

	h := &Handlers{
		Registry:      reg,
		Events:        NewMemoryEventStore(),
		Telemetry:     svc,
		AgentDetector: detector,
	}

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions: status %d", rec.Code)
	}

	var sessions []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	found := false
	for _, s := range sessions {
		if s.Adapter == "tmux" {
			found = true
			// Evidence-based detection: terminal adapter alone is insufficient.
			// The bridge returns unknown with low confidence — no false positive.
			t.Logf("production bridge: AgentKind=%s AgentStatus=%s Confidence=%.2f",
				s.AgentKind, s.AgentStatus, s.AgentConfidence)
			if s.AgentKind == "" {
				t.Error("AgentKind is empty (should at least be 'unknown')")
			}
			if s.AgentConfidence > 0.5 && s.AgentKind != "unknown" {
				t.Errorf("high confidence without evidence: Kind=%s Confidence=%.2f", s.AgentKind, s.AgentConfidence)
			}
		}
	}
	if !found {
		t.Error("session not found in telemetry API response")
	}

	// Nil detector path: fields must be empty.
	svc2 := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), NoopNotifier{}, nil)
	h2 := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Telemetry: svc2}
	req2 := httptest.NewRequest("GET", "/api/sessions", nil)
	rec2 := httptest.NewRecorder()
	h2.HandleSessionsAPI(rec2, req2)
	var sessions2 []SessionTelemetry
	json.Unmarshal(rec2.Body.Bytes(), &sessions2)
	for _, s := range sessions2 {
		if s.AgentKind != "" {
			t.Errorf("nil detector: AgentKind=%q, want empty", s.AgentKind)
		}
	}
}

func TestClaudeOutput_FalsePositivePrevention(t *testing.T) {
	// Non-Claude session must NOT be detected as Claude.
	// Evidence-based detection returns unknown for insufficient evidence.
	det := agent.NewClaudeDetector()
	id := det.Detect(agent.DetectionEvidence{
		ProcessName: "bash", // plain shell, not an AI agent
		TermAdapter: "tmux",
	})
	if id.Kind == "claude" && id.Confidence >= 0.5 {
		t.Errorf("false positive: bash process detected as claude (confidence=%.2f)", id.Confidence)
	}
	if id.Kind != "unknown" {
		t.Logf("bash process: Kind=%q Confidence=%.2f", id.Kind, id.Confidence)
	}

	// Empty evidence must return unknown.
	id2 := det.Detect(agent.DetectionEvidence{})
	if id2.Kind != "unknown" {
		t.Errorf("empty evidence: Kind=%q, want unknown", id2.Kind)
	}
	if id2.Confidence >= 0.5 {
		t.Errorf("empty evidence: confidence=%.2f >= 0.5", id2.Confidence)
	}
}

// --- helpers ---

type claudeSessionAdapter struct{}

func (a *claudeSessionAdapter) Name() string { return "tmux" }
func (a *claudeSessionAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&claudeStubSession{}}, nil
}

type claudeStubSession struct{}

func (s *claudeStubSession) ID() string          { return "claude-session" }
func (s *claudeStubSession) Title() string       { return "Claude Code" }
func (s *claudeStubSession) AdapterName() string { return "tmux" }
