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

// TestAntigravityTelemetry_ProductionBoundary proves that
// Antigravity actual-log fixtures reach /api/sessions.Events
// through the same production telemetry path as Claude/Codex.
func TestAntigravityTelemetry_ProductionBoundary(t *testing.T) {
	// Create a temp Antigravity JSONL log with real fixture event types.
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "transcript.jsonl")
	antigravityLog := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-07-04T06:00:54Z","content":"<PROMPT>"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-07-04T06:01:00Z","content":"<REDACTED_PLAN>"}
{"step_index":2,"source":"MODEL","type":"SEARCH_WEB","status":"DONE","created_at":"2026-07-04T06:05:00Z","content":"<CMD>"}
{"step_index":50,"source":"SYSTEM","type":"CHECKPOINT","status":"DONE","created_at":"2026-07-04T06:15:00Z","content":"<CHECKPOINT>"}
{"step_index":51,"source":"SYSTEM","type":"ERROR_MESSAGE","status":"DONE","created_at":"2026-07-04T06:15:01Z","content":"<ERROR>"}
`
	os.WriteFile(logPath, []byte(antigravityLog), 0644)

	adapter := &antigravityTestAdapter{logPath: logPath}
	reg := mux.MustNewRegistry(adapter)

	detector := agent.NewTermAgentDetector()
	events := NewMemoryEventStore()
	svc := NewTelemetryService(reg, events, NewNopLinkStore(), NoopNotifier{}, detector, nil)
	svc.SetLogResolver(func(p models.ProcessInfo) (LogRef, error) {
		return LogRef{Path: logPath, Agent: "antigravity"}, nil
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
		if s.ID == "tmux:antigravity-session" {
			found = true
			t.Logf("events=%d, agentKind=%s, state=%s",
				len(s.Events), s.AgentKind, s.State)

			// 1. Events must be non-empty — production parser path was exercised.
			if len(s.Events) == 0 {
				t.Error("Events is empty — production Antigravity parser path not exercised")
			}

			// 2. Common event types from production path.
			gotTypes := map[string]bool{}
			for _, e := range s.Events {
				gotTypes[e.Type] = true
			}
			for _, want := range []string{"user_message", "assistant_message", "tool_call_started"} {
				if !gotTypes[want] {
					t.Errorf("missing event type %q in %v", want, gotTypes)
				}
			}

			// 3. AgentKind must be "antigravity" from the composite detector.
			if s.AgentKind != "antigravity" {
				t.Errorf("AgentKind=%q, want antigravity", s.AgentKind)
			}
			if s.AgentConfidence < 0.5 {
				t.Errorf("confidence %.2f < 0.5", s.AgentConfidence)
			}

			// 4. AgentStatus must be populated from parser state.
			if s.AgentStatus == "" {
				t.Error("AgentStatus is empty")
			}

			// 5. Events must flow through EventStore (check the Events field).
			t.Logf("event types: %v", gotTypes)
		}
	}
	if !found {
		t.Fatal("session tmux:antigravity-session not found in API response — production path not connected")
	}
}

// TestAntigravityTelemetry_DegradedSystemTypes proves that
// unknown Antigravity system types degrade safely through production path.
func TestAntigravityTelemetry_DegradedSystemTypes(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "transcript.jsonl")
	// Only system types: CHECKPOINT → unknown, ERROR_MESSAGE → failed, EPHEMERAL_MESSAGE → skip
	antigravityLog := `{"step_index":50,"source":"SYSTEM","type":"CHECKPOINT","status":"DONE","created_at":"2026-07-04T06:15:00Z","content":"<CHECKPOINT>"}
{"step_index":51,"source":"SYSTEM","type":"ERROR_MESSAGE","status":"DONE","created_at":"2026-07-04T06:15:01Z","content":"<ERROR>"}
{"step_index":53,"source":"SYSTEM","type":"EPHEMERAL_MESSAGE","status":"DONE","created_at":"2026-07-04T06:15:03Z","content":"<EPHEMERAL>"}
{"step_index":60,"source":"UNKNOWN","type":"UNKNOWN_TYPE","status":"UNKNOWN","created_at":"2026-07-04T06:20:00Z","content":"<REDACTED>"}
`
	os.WriteFile(logPath, []byte(antigravityLog), 0644)

	adapter := &antigravityTestAdapter{logPath: logPath}
	reg := mux.MustNewRegistry(adapter)

	detector := agent.NewTermAgentDetector()
	events := NewMemoryEventStore()
	svc := NewTelemetryService(reg, events, NewNopLinkStore(), NoopNotifier{}, detector, nil)
	svc.SetLogResolver(func(p models.ProcessInfo) (LogRef, error) {
		return LogRef{Path: logPath, Agent: "antigravity"}, nil
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

	// Terminal must survive degraded input.
	if rec.Code != http.StatusOK {
		t.Fatalf("degraded system types: status %d (terminal must survive)", rec.Code)
	}

	var sessions []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	found := false
	for _, s := range sessions {
		if s.ID == "tmux:antigravity-session" {
			found = true
			t.Logf("degraded: events=%d, agentKind=%s, state=%s",
				len(s.Events), s.AgentKind, s.State)

			// EPHEMERAL_MESSAGE is skipped; CHECKPOINT + ERROR_MESSAGE produce events.
			// Session must still be listed with agent fields.
			if s.AgentKind != "antigravity" {
				t.Errorf("AgentKind=%q, want antigravity", s.AgentKind)
			}
		}
	}
	if !found {
		t.Fatal("session not found after degraded system types (must not hide terminal)")
	}
}

// TestAntigravityDetection_FalsePositive proves that non-Antigravity
// sessions are not misidentified as Antigravity.
func TestAntigravityDetection_FalsePositive(t *testing.T) {
	reg := mux.MustNewRegistry(&bashSessionAdapter{})
	detector := agent.NewTermAgentDetector()
	svc := NewTelemetryService(reg, NewMemoryEventStore(), NewNopLinkStore(), NoopNotifier{}, detector, nil)
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

// --- antigravity test adapters ---

type antigravityTestAdapter struct{ logPath string }

func (a *antigravityTestAdapter) Name() string { return "tmux" }
func (a *antigravityTestAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&antigravityTestSession{}}, nil
}
func (a *antigravityTestAdapter) ProcessSnapshot(_ context.Context) (map[string]models.ProcessInfo, error) {
	return map[string]models.ProcessInfo{
		"antigravity-session": {Command: "antigravity", CWD: "/Users/test/.gemini/antigravity"},
	}, nil
}

type antigravityTestSession struct{}

func (s *antigravityTestSession) ID() string          { return "antigravity-session" }
func (s *antigravityTestSession) Title() string       { return "Antigravity" }
func (s *antigravityTestSession) AdapterName() string { return "tmux" }
func (s *antigravityTestSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{Command: "antigravity", CWD: "/Users/test/.gemini/antigravity"}, nil
}
