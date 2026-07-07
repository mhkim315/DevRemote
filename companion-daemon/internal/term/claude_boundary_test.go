package term

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

// Phase A5b: production agent detection bridge.
// Agent events flow through the existing production telemetry path
// (TelemetryService.processSession → EventStore → Snapshot.Events).
// The A5b bridge adds agentKind/agentStatus/agentConfidence to that path.

func TestClaudeDetection_TruePositive(t *testing.T) {
	reg := mux.MustNewRegistry(&claudeProcessAdapter{})
	detector := agent.NewTermAgentDetector()

	svc := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), NoopNotifier{}, detector)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go svc.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-svc.Done()

	h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Telemetry: svc, AgentDetector: detector}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	var sessions []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	found := false
	for _, s := range sessions {
		if s.Adapter == "tmux" {
			found = true
			t.Logf("detection: AgentKind=%s Status=%s Confidence=%.2f",
				s.AgentKind, s.AgentStatus, s.AgentConfidence)
			if s.AgentKind != "claude" {
				t.Errorf("AgentKind=%q, want claude", s.AgentKind)
			}
			if s.AgentConfidence < 0.5 {
				t.Errorf("confidence %.2f < 0.5", s.AgentConfidence)
			}
		}
	}
	if !found {
		t.Error("Claude session not found")
	}
}

func TestClaudeDetection_FalsePositive(t *testing.T) {
	reg := mux.MustNewRegistry(&bashProcessAdapter{})
	detector := agent.NewTermAgentDetector()
	svc := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), NoopNotifier{}, detector)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go svc.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-svc.Done()
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Telemetry: svc, AgentDetector: detector}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	var sessions []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	for _, s := range sessions {
		if s.AgentKind != "unknown" && s.AgentConfidence >= 0.5 {
			t.Errorf("false positive: Kind=%s Confidence=%.2f", s.AgentKind, s.AgentConfidence)
		}
	}
}

func TestClaudeDetection_NilDetector(t *testing.T) {
	reg := mux.MustNewRegistry(&claudeProcessAdapter{})
	svc := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), NoopNotifier{}, nil)
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore(), Telemetry: svc}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	var sessions []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	for _, s := range sessions {
		if s.AgentKind != "" {
			t.Errorf("nil detector: AgentKind=%q", s.AgentKind)
		}
	}
}

// --- adapters ---

type claudeProcessAdapter struct{}

func (a *claudeProcessAdapter) Name() string { return "tmux" }
func (a *claudeProcessAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&claudeProcessSession{}}, nil
}

type claudeProcessSession struct{}

func (s *claudeProcessSession) ID() string          { return "claude-session" }
func (s *claudeProcessSession) Title() string       { return "Claude Code" }
func (s *claudeProcessSession) AdapterName() string { return "tmux" }
func (s *claudeProcessSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{Command: "claude", CWD: "/Users/test/project"}, nil
}

type bashProcessAdapter struct{}

func (a *bashProcessAdapter) Name() string { return "tmux" }
func (a *bashProcessAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&bashProcessSession{}}, nil
}

type bashProcessSession struct{}

func (s *bashProcessSession) ID() string          { return "bash-session" }
func (s *bashProcessSession) Title() string       { return "Bash" }
func (s *bashProcessSession) AdapterName() string { return "tmux" }
func (s *bashProcessSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{Command: "bash", CWD: "/tmp"}, nil
}
