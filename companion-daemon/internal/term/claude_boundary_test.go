package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

func TestClaudeOutput_RealLogParsing(t *testing.T) {
	// Create a temp log file with Claude-format JSONL (based on A1 fixtures).
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "session.jsonl")
	// Write real Claude-format log entries.
	claudeLog := `{"type":"mode","mode":"normal","sessionId":"<UUID>"}
{"type":"user","message":{"role":"user","content":"<PROMPT>"},"sessionId":"<UUID>"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"<REDACTED>"}]},"sessionId":"<UUID>"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{"command":"<CMD>"}}]},"sessionId":"<UUID>"}
{"type":"permission-mode","permissionMode":"ask","sessionId":"<UUID>"}
`
	if err := os.WriteFile(logFile, []byte(claudeLog), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Session adapter that returns the temp log path via ProcessProvider.
	reg := mux.MustNewRegistry(&claudeLogSessionAdapter{logPath: logFile})
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
			t.Logf("temp log: AgentKind=%s Status=%s Confidence=%.2f Events=%d",
				s.AgentKind, s.AgentStatus, s.AgentConfidence, len(s.AgentEvents))

			if s.AgentKind != "claude" {
				t.Errorf("AgentKind=%q, want claude", s.AgentKind)
			}
			if s.AgentStatus == "" || s.AgentStatus == "idle" {
				t.Errorf("AgentStatus=%q, want non-idle", s.AgentStatus)
			}

			// Verify specific event types from real Claude fixture parsing.
			foundTypes := map[string]bool{}
			for _, e := range s.AgentEvents {
				foundTypes[string(e.Type)] = true
			}
			for _, want := range []string{"user_message", "thinking", "tool_call_started", "approval_requested"} {
				if !foundTypes[want] {
					t.Errorf("missing event type %q in %v", want, foundTypes)
				}
			}
			// Approval fixture should produce waiting_approval status.
			if s.AgentStatus != "waiting_approval" {
				t.Errorf("AgentStatus=%q, want waiting_approval (approval fixture)", s.AgentStatus)
			}
		}
	}
}

func TestClaudeOutput_MissingLog(t *testing.T) {
	// Missing log → no events, no crash, terminal still works.
	reg := mux.MustNewRegistry(&claudeNoLogAdapter{})
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

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions: status %d (terminal must survive)", rec.Code)
	}
	var sessions []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	for _, s := range sessions {
		if s.Adapter == "tmux" {
			if len(s.AgentEvents) > 0 {
				t.Errorf("missing log: got %d AgentEvents, want 0", len(s.AgentEvents))
			}
			t.Logf("missing log: AgentKind=%s Events=%d", s.AgentKind, len(s.AgentEvents))
		}
	}
}

func TestClaudeOutput_FalsePositive_ProductionBridge(t *testing.T) {
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
				t.Errorf("bash: false positive Kind=%s Confidence=%.2f", s.AgentKind, s.AgentConfidence)
			}
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

// --- adapters ---

type claudeLogSessionAdapter struct{ logPath string }

func (a *claudeLogSessionAdapter) Name() string { return "tmux" }
func (a *claudeLogSessionAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&claudeLogSession{logPath: a.logPath}}, nil
}

type claudeLogSession struct{ logPath string }

func (s *claudeLogSession) ID() string          { return "claude-session" }
func (s *claudeLogSession) Title() string       { return "Claude Code" }
func (s *claudeLogSession) AdapterName() string { return "tmux" }
func (s *claudeLogSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{Command: "claude", CWD: "/Users/test/project"}, nil
}
func (s *claudeLogSession) LogPath() string { return s.logPath }

type claudeNoLogAdapter struct{}

func (a *claudeNoLogAdapter) Name() string { return "tmux" }
func (a *claudeNoLogAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&claudeNoLogSession{}}, nil
}

type claudeNoLogSession struct{}

func (s *claudeNoLogSession) ID() string          { return "claude-no-log" }
func (s *claudeNoLogSession) Title() string       { return "Claude Code" }
func (s *claudeNoLogSession) AdapterName() string { return "tmux" }
func (s *claudeNoLogSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{Command: "claude", CWD: "/nonexistent"}, nil
}

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
