package term

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

func TestClaudeLog_ProductionEventsPath(t *testing.T) {
	// Create a temp log at a path matching Claude's pattern.
	tmpHome := t.TempDir()
	projDir := filepath.Join(tmpHome, ".claude", "projects", "test-project")
	os.MkdirAll(projDir, 0755)
	logPath := filepath.Join(projDir, "session.jsonl")
	claudeLog := `{"type":"user","message":{"role":"user","content":"<PROMPT>"},"sessionId":"<UUID>"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"<REDACTED>"}]},"sessionId":"<UUID>"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"<CMD>"}}]},"sessionId":"<UUID>"}
{"type":"permission-mode","permissionMode":"ask","sessionId":"<UUID>"}
`
	os.WriteFile(logPath, []byte(claudeLog), 0644)

	adapter := &claudeSessionAdapter{logPath: logPath}
	reg := mux.MustNewRegistry(adapter)

	detector := agent.NewTermAgentDetector()
	events := NewMemoryEventStore()
	svc := NewTelemetryService(reg, events, NewNopLinkStore(), NoopNotifier{}, detector)
	svc.SetLogResolver(func(p models.ProcessInfo) (LogRef, error) {
		return LogRef{Path: logPath, Agent: "claude"}, nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go svc.Run(ctx)
	time.Sleep(3 * time.Second) // ticker fires at 2s; wait for one full sampling cycle
	cancel()
	<-svc.Done()

	h := &Handlers{Registry: reg, Events: events, Telemetry: svc, AgentDetector: detector}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	var sessions []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	found := false
	for _, s := range sessions {
		if s.ID == "tmux:claude-session" {
			found = true
			t.Logf("events=%d, agentKind=%s, state=%s",
				len(s.Events), s.AgentKind, s.State)

			if len(s.Events) == 0 {
				t.Error("Events is empty — production parser path not exercised")
			}
			// Verify event types from production parser.
			gotTypes := map[string]bool{}
			for _, e := range s.Events {
				gotTypes[e.Type] = true
			}
			for _, want := range []string{"user", "tool_use"} {
				if !gotTypes[want] {
					t.Errorf("missing event type %q in %v", want, gotTypes)
				}
			}
			// Detection fields populated.
			if s.AgentKind != "claude" {
				t.Errorf("AgentKind=%q, want claude", s.AgentKind)
			}
			if s.AgentConfidence < 0.5 {
				t.Errorf("confidence %.2f < 0.5", s.AgentConfidence)
			}
		}
	}
	if !found {
		t.Fatal("session tmux:claude-session not found in API response")
	}
}

func TestClaudeDetection_FalsePositive(t *testing.T) {
	reg := mux.MustNewRegistry(&bashSessionAdapter{})
	detector := agent.NewTermAgentDetector()
	svc := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), NoopNotifier{}, detector)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	reg := mux.MustNewRegistry(&claudeSessionAdapter{})
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

type claudeSessionAdapter struct{ logPath string }

func (a *claudeSessionAdapter) Name() string { return "tmux" }
func (a *claudeSessionAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&claudeSession{}}, nil
}
func (a *claudeSessionAdapter) ProcessSnapshot(_ context.Context) (map[string]models.ProcessInfo, error) {
	return map[string]models.ProcessInfo{
		"claude-session": {Command: "claude", CWD: "/Users/test/project"},
	}, nil
}

type claudeSession struct{}

func (s *claudeSession) ID() string          { return "claude-session" }
func (s *claudeSession) Title() string       { return "Claude Code" }
func (s *claudeSession) AdapterName() string { return "tmux" }
func (s *claudeSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{Command: "claude", CWD: "/Users/test/project"}, nil
}

type bashSessionAdapter struct{}

func (a *bashSessionAdapter) Name() string { return "tmux" }
func (a *bashSessionAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&bashSession{}}, nil
}

type bashSession struct{}

func (s *bashSession) ID() string          { return "bash-session" }
func (s *bashSession) Title() string       { return "Bash" }
func (s *bashSession) AdapterName() string { return "tmux" }
func (s *bashSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{Command: "bash", CWD: "/tmp"}, nil
}
