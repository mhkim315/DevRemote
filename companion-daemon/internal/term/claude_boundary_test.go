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

// Phase A5b: detection bridge (AgentKind/Status/Confidence) +
// existing Events path (EventStore → Snapshot.Events → /api/sessions).

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
			t.Logf("detection: AgentKind=%s Status=%s Confidence=%.2f Events=%d",
				s.AgentKind, s.AgentStatus, s.AgentConfidence, len(s.Events))
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

// TestEventsPath proves the existing production event pipeline:
// EventStore.Append → Snapshot.Events → /api/sessions.
func TestEventsPath_ProductionPipeline(t *testing.T) {
	reg := mux.MustNewRegistry(&claudeProcessAdapter{})
	events := NewMemoryEventStore()

	// Simulate what processSession does: emit Claude events into EventStore.
	events.Append("tmux:claude-session", []models.AgentEvent{
		{ID: "1", Session: "tmux:claude-session", Type: "user", Summary: "user message", Timestamp: "2026-07-07T00:00:00Z"},
		{ID: "2", Session: "tmux:claude-session", Type: "tool_use", Summary: "tool call", Timestamp: "2026-07-07T00:00:01Z"},
		{ID: "3", Session: "tmux:claude-session", Type: "message", Summary: "Thinking", Timestamp: "2026-07-07T00:00:02Z"},
	})

	h := &Handlers{Registry: reg, Events: events}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	var sessions []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	for _, s := range sessions {
		if s.ID == "tmux:claude-session" {
			t.Logf("events path: Events=%d", len(s.Events))
			if len(s.Events) != 3 {
				t.Errorf("Events=%d, want 3", len(s.Events))
			}
			foundTypes := map[string]int{}
			for _, e := range s.Events {
				foundTypes[e.Type]++
			}
			if foundTypes["user"] == 0 {
				t.Error("missing user event type")
			}
			if foundTypes["tool_use"] == 0 {
				t.Error("missing tool_use event type")
			}
		}
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
