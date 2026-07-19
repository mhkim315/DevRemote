package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

func TestCodexLog_ProductionEventsPath(t *testing.T) {
 t.Skip("PA3 Step 2: legacy parser removed; accepted-adapter feeds Transcript, not EventStore")
	tmpDir := t.TempDir()
	logPath := tmpDir + "/session.jsonl"
	codexLog := `{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"<UUID>"}}{"timestamp":"2026-07-06T13:29:35.399Z","type":"event_msg","payload":{"type":"task_started","turn_id":"<UUID>"}}
{"timestamp":"2026-07-06T13:29:38.000Z","type":"event_msg","payload":{"type":"user_message","message":"<PROMPT>"}}
{"timestamp":"2026-07-06T13:30:00.000Z","type":"event_msg","payload":{"type":"waiting_for_approval","turn_id":"<UUID>"}}
`
	os.WriteFile(logPath, []byte(codexLog), 0644)

	reg := mux.MustNewRegistry(&codexTestAdapter{})
	detector := agent.NewTermAgentDetector()
	events := NewMemoryEventStore()
	svc := NewTelemetryService(reg, events, NoopNotifier{}, detector, nil, nil, nil)
	svc.SetLogResolver(func(p models.ProcessInfo) (LogRef, error) {
		return LogRef{Path: logPath, Agent: "codex"}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go svc.Run(ctx)
	time.Sleep(3 * time.Second)
	cancel()
	<-svc.Done()

	h := &Handlers{Registry: reg, Events: events, Telemetry: svc, AgentDetector: detector}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	var sessions []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	found := false
	for _, s := range sessions {
		if s.ID == "tmux:codex-session" {
			found = true
			t.Logf("codex: events=%d, agentKind=%s, state=%s", len(s.Events), s.AgentKind, s.State)
			if len(s.Events) == 0 {
				t.Error("Events is empty")
			}
			gotTypes := map[string]bool{}
			for _, e := range s.Events {
				gotTypes[e.Type] = true
			}
			for _, want := range []string{"agent_started", "user_message", "approval_requested"} {
				if !gotTypes[want] {
					t.Errorf("missing event type %q", want)
				}
			}
			if s.AgentKind != "codex" {
				t.Errorf("AgentKind=%q, want codex", s.AgentKind)
			}
			if s.AgentStatus != "waiting_approval" {
				t.Errorf("AgentStatus=%q, want waiting_approval", s.AgentStatus)
			}
			if s.AgentConfidence < 0.5 {
				t.Errorf("AgentConfidence=%.2f < 0.5", s.AgentConfidence)
			}
		}
	}
	if !found {
		t.Fatal("codex session not found in API response")
	}
}

// --- adapters ---

type codexTestAdapter struct{}

func (a *codexTestAdapter) Name() string { return "tmux" }
func (a *codexTestAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&codexTestSession{}}, nil
}
func (a *codexTestAdapter) ProcessSnapshot(_ context.Context) (map[string]models.ProcessInfo, error) {
	return map[string]models.ProcessInfo{
		"codex-session": {Command: "codex", CWD: "/Users/test/project"},
	}, nil
}

type codexTestSession struct{}

func (s *codexTestSession) ID() string          { return "codex-session" }
func (s *codexTestSession) Title() string       { return "Codex" }
func (s *codexTestSession) AdapterName() string { return "tmux" }
func (s *codexTestSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{Command: "codex", CWD: "/Users/test/project"}, nil
}
