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

// Phase A5 product-boundary bridge: production TelemetryService with
// agent.NewTermAgentDetector(), real session ProcessProvider evidence.

func TestClaudeOutput_TruePositive_ProductionBridge(t *testing.T) {
	// Session with Claude process evidence → production bridge detects Claude.
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
	for _, s := range sessions {
		if s.Adapter == "tmux" {
			t.Logf("production: AgentKind=%s Status=%s Confidence=%.2f",
				s.AgentKind, s.AgentStatus, s.AgentConfidence)
			if s.AgentKind != "claude" {
				t.Errorf("Claude process evidence: AgentKind=%q, want claude", s.AgentKind)
			}
			if s.AgentConfidence < 0.5 {
				t.Errorf("Claude process: confidence %.2f < 0.5", s.AgentConfidence)
			}
			if s.AgentStatus == "" {
				t.Error("AgentStatus is empty")
			}
			if len(s.AgentEvents) == 0 {
				t.Error("AgentEvents is empty (parser not wired)")
			}
			for _, e := range s.AgentEvents {
				if e.Type == "" {
					t.Error("agent event has empty Type")
				}
			}
		}
	}
}

func TestClaudeOutput_FalsePositive_ProductionBridge(t *testing.T) {
	// Non-Claude session → production bridge returns unknown.
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
		if s.Adapter == "tmux" {
			if s.AgentKind != "unknown" && s.AgentConfidence >= 0.5 {
				t.Errorf("bash process: false positive Kind=%s Confidence=%.2f", s.AgentKind, s.AgentConfidence)
			}
			t.Logf("bash: AgentKind=%s Confidence=%.2f", s.AgentKind, s.AgentConfidence)
		}
	}
}

func TestClaudeOutput_NilDetector_BackwardCompat(t *testing.T) {
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
			t.Errorf("nil detector: AgentKind=%q, want empty", s.AgentKind)
		}
	}
}

// --- adapters with real ProcessProvider ---

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
