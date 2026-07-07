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

func TestClaudeOutput_ProductionResolverPath(t *testing.T) {
	// Create a real log file at a path the production resolver can find.
	tmpDir := t.TempDir()
	claudeProj := filepath.Join(tmpDir, ".claude", "projects", "test")
	os.MkdirAll(claudeProj, 0755)
	logPath := filepath.Join(claudeProj, "session.jsonl")
	os.WriteFile(logPath, []byte(`{"type":"user","message":{"role":"user","content":"<PROMPT>"},"sessionId":"<UUID>"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"<REDACTED>"}]},"sessionId":"<UUID>"}
`), 0644)

	// Session that reports CWD within the temp dir → resolver finds .claude/projects/.
	reg := mux.MustNewRegistry(&claudeResolverSessionAdapter{cwd: tmpDir, logPath: logPath})
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
			t.Logf("resolver path: AgentKind=%s Status=%s Events=%d",
				s.AgentKind, s.AgentStatus, len(s.AgentEvents))

			if s.AgentKind != "claude" {
				t.Errorf("AgentKind=%q, want claude", s.AgentKind)
			}
			if len(s.AgentEvents) == 0 {
				t.Error("no events from production resolver path")
			}
			found := map[string]bool{}
			for _, e := range s.AgentEvents {
				found[string(e.Type)] = true
			}
			for _, want := range []string{"user_message", "thinking"} {
				if !found[want] {
					t.Errorf("missing %q", want)
				}
			}
		}
	}
}

func TestClaudeOutput_MissingLog_NoCrash(t *testing.T) {
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
	if rec.Code != http.StatusOK {
		t.Fatalf("missing log: status %d", rec.Code)
	}
	var sessions []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	for _, s := range sessions {
		if s.Adapter == "tmux" {
			if len(s.AgentEvents) > 0 {
				t.Errorf("missing log: got %d events, want 0", len(s.AgentEvents))
			}
		}
	}
}

func TestClaudeOutput_FalsePositive(t *testing.T) {
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
				t.Errorf("bash: false positive Kind=%s", s.AgentKind)
			}
		}
	}
}

func TestClaudeOutput_NilDetector(t *testing.T) {
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

type claudeResolverSessionAdapter struct {
	cwd     string
	logPath string
}

func (a *claudeResolverSessionAdapter) Name() string { return "tmux" }
func (a *claudeResolverSessionAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&claudeResolverSession{cwd: a.cwd, logPath: a.logPath}}, nil
}

type claudeResolverSession struct {
	cwd     string
	logPath string
}

func (s *claudeResolverSession) ID() string          { return "claude-session" }
func (s *claudeResolverSession) Title() string       { return "Claude Code" }
func (s *claudeResolverSession) AdapterName() string { return "tmux" }
func (s *claudeResolverSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{Command: "claude", CWD: s.cwd}, nil
}
func (s *claudeResolverSession) LogPath() string { return s.logPath }

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
