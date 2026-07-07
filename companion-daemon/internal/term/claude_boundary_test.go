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

type claudeDetectorAdapter struct{}

func (d *claudeDetectorAdapter) DetectAgent(sessionID, adapterName, localID string) (string, string, float64) {
	det := agent.NewClaudeDetector()
	id := det.Detect(agent.DetectionEvidence{ProcessName: "claude", CWD: "/Users/test/project"})
	return id.Kind, string(agent.StatusWorking), id.Confidence
}

func TestClaudeOutput_TelemetryPath(t *testing.T) {
	reg := mux.MustNewRegistry(&claudeSessionAdapter{})
	detector := &claudeDetectorAdapter{}

	// Production path: TelemetryService with detector, routed through HandleSessionsV2.
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
			if s.AgentKind != "claude" {
				t.Errorf("telemetry path: AgentKind=%q, want claude", s.AgentKind)
			}
			if s.AgentStatus == "" {
				t.Error("telemetry path: AgentStatus is empty")
			}
			t.Logf("telemetry path: AgentKind=%s AgentStatus=%s Confidence=%.2f",
				s.AgentKind, s.AgentStatus, s.AgentConfidence)
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
