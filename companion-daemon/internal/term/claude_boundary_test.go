package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/mux"
)

// Phase A5 product-boundary bridge: Claude adapter output flows
// through the actual production telemetry path (HandleSessionsV2).

// claudeDetector adapts agent.ClaudeDetector to term.AgentDetector.
type claudeDetectorAdapter struct {
	detector *agent.ClaudeDetector
	parser   *agent.ClaudeParser
}

func (d *claudeDetectorAdapter) DetectAgent(sessionID, adapterName, localID string) (string, string, float64) {
	ev := agent.DetectionEvidence{ProcessName: "claude", CWD: "/Users/test/project"}
	id := d.detector.Detect(ev)
	return id.Kind, string(agent.StatusWorking), id.Confidence
}

func TestClaudeOutput_ProductPath(t *testing.T) {
	// Wire Claude detector into the production telemetry path.
	adapter := &claudeDetectorAdapter{
		detector: agent.NewClaudeDetector(),
		parser:   agent.NewClaudeParser(),
	}

	reg := mux.MustNewRegistry(&claudeSessionAdapter{})
	h := &Handlers{
		Registry:      reg,
		Events:        NewMemoryEventStore(),
		AgentDetector: adapter,
	}

	// Hit the actual API endpoint.
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
	if len(sessions) == 0 {
		t.Fatal("0 sessions in API response")
	}

	// Verify agent fields populated via the production path.
	found := false
	for _, s := range sessions {
		if s.Adapter == "tmux" && s.ID == "tmux:claude-session" {
			found = true
			if s.AgentKind != "claude" {
				t.Errorf("AgentKind=%q, want claude", s.AgentKind)
			}
			if s.AgentStatus == "" {
				t.Error("AgentStatus is empty")
			}
			if s.AgentConfidence == 0 {
				t.Error("AgentConfidence is 0")
			}
			t.Logf("product path: AgentKind=%s AgentStatus=%s Confidence=%.2f",
				s.AgentKind, s.AgentStatus, s.AgentConfidence)
		}
	}
	if !found {
		t.Error("claude session not found in API response")
	}

	// Backward compat: nil detector → no agent fields.
	h2 := &Handlers{Registry: reg, Events: NewMemoryEventStore(), AgentDetector: nil}
	req2 := httptest.NewRequest("GET", "/api/sessions", nil)
	rec2 := httptest.NewRecorder()
	h2.HandleSessionsAPI(rec2, req2)
	var sessions2 []SessionTelemetry
	json.Unmarshal(rec2.Body.Bytes(), &sessions2)
	for _, s := range sessions2 {
		if s.AgentKind != "" {
			t.Errorf("nil detector: AgentKind=%q, want empty (backward compat)", s.AgentKind)
		}
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
