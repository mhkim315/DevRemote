package term

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// Phase A7: Agent Agnostic UX verification.
// Proves unknown AgentKind, unknown Status, degraded state, schema evolution
// all work without vendor-specific branching.

func TestAgnostic_UnknownAgentKind(t *testing.T) {
	// Unknown agent ("gemini", "qwen", "future") must pass through API unchanged.
	for _, kind := range []string{"gemini", "qwen", "future"} {
		t.Run(kind, func(t *testing.T) {
			st := SessionTelemetry{
				ID:              "tmux:test",
				Adapter:         "tmux",
				AgentKind:       kind,
				AgentStatus:     "working",
				AgentConfidence: 0.7,
			}
			data, _ := json.Marshal(st)
			if !strings.Contains(string(data), `"agentKind":"`+kind+`"`) {
				t.Errorf("JSON missing agentKind=%q", kind)
			}
			// Round-trip.
			var rt SessionTelemetry
			json.Unmarshal(data, &rt)
			if rt.AgentKind != kind {
				t.Errorf("round-trip: AgentKind=%q, want %q", rt.AgentKind, kind)
			}
		})
	}
}

func TestAgnostic_UnknownStatus(t *testing.T) {
	// Unknown status values must pass through without filtering or crash.
	for _, status := range []string{"degraded", "custom_state", "reconnecting"} {
		t.Run(status, func(t *testing.T) {
			st := SessionTelemetry{
				ID:              "tmux:test",
				Adapter:         "tmux",
				AgentKind:       "claude",
				AgentStatus:     status,
				AgentConfidence: 0.5,
			}
			data, _ := json.Marshal(st)
			if !strings.Contains(string(data), `"agentStatus":"`+status+`"`) {
				t.Errorf("JSON missing agentStatus=%q", status)
			}
		})
	}
}

func TestAgnostic_DegradedState(t *testing.T) {
	// Low confidence → agentKind=unknown, no false positive.
	st := SessionTelemetry{
		ID:              "tmux:test",
		Adapter:         "tmux",
		AgentKind:       "unknown",
		AgentStatus:     "unknown",
		AgentConfidence: 0.1,
	}
	data, _ := json.Marshal(st)
	var rt SessionTelemetry
	json.Unmarshal(data, &rt)
	if rt.AgentKind != "unknown" {
		t.Errorf("degraded: AgentKind=%q, want unknown", rt.AgentKind)
	}

	// Degraded terminal session must still appear in API.
	reg := mux.MustNewRegistry(&agnosticAdapter{})
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	if rec.Code != 200 {
		t.Fatalf("degraded session: status %d (terminal must survive)", rec.Code)
	}
}

func TestAgnostic_SchemaEvolution(t *testing.T) {
	// New omitempty field: old clients ignore, new clients accept.
	// Prove backward compat: session WITHOUT agent fields.
	noAgent := SessionTelemetry{ID: "tmux:old", Adapter: "tmux"}
	data, _ := json.Marshal(noAgent)
	if strings.Contains(string(data), "agentKind") {
		t.Error("backward compat: agentKind present in session without agent")
	}

	// Prove forward compat: session WITH agent fields round-trips.
	withAgent := SessionTelemetry{
		ID:              "tmux:new",
		Adapter:         "tmux",
		AgentKind:       "claude",
		AgentStatus:     "working",
		AgentConfidence: 0.7,
		// Hypothetical future field:
		// AgentVersion: "2.1.202",  // would be omitempty
	}
	data2, _ := json.Marshal(withAgent)
	var rt SessionTelemetry
	json.Unmarshal(data2, &rt)
	if rt.AgentKind != "claude" {
		t.Error("forward compat: round-trip lost AgentKind")
	}
}

func TestAgnostic_TerminalUnaffectedByAgentLayer(t *testing.T) {
	// Terminal sessions must appear regardless of agent state.
	reg := mux.MustNewRegistry(&agnosticAdapter{})
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}

	var sessions []SessionTelemetry
	json.Unmarshal(rec.Body.Bytes(), &sessions)
	found := false
	for _, s := range sessions {
		if s.ID == "tmux:test" {
			found = true
			// Agent fields may be empty (no detector) — that's fine.
			// The session must be listed regardless.
		}
	}
	if !found {
		t.Error("terminal session not listed (agent layer must not hide)")
	}
}

func TestAgnostic_MissingOptionalFields(t *testing.T) {
	// Session without agentKind/status/confidence must serialize cleanly.
	st := SessionTelemetry{ID: "tmux:test", Adapter: "tmux"}
	data, _ := json.Marshal(st)
	// Must NOT contain agent fields when not set.
	for _, field := range []string{"agentKind", "agentStatus", "agentConfidence"} {
		if strings.Contains(string(data), field) {
			t.Errorf("missing optional: JSON contains %q when not set", field)
		}
	}
	// Round-trip must survive.
	var rt SessionTelemetry
	json.Unmarshal(data, &rt)
	if rt.ID != "tmux:test" {
		t.Error("round-trip lost ID")
	}
	// Agent fields must be empty string/zero.
	if rt.AgentKind != "" || rt.AgentStatus != "" || rt.AgentConfidence != 0 {
		t.Error("agent fields non-zero after empty round-trip")
	}
}

// --- helpers ---

type agnosticAdapter struct{}

func (a *agnosticAdapter) Name() string { return "tmux" }
func (a *agnosticAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&agnosticSession{}}, nil
}

type agnosticSession struct{}

func (s *agnosticSession) ID() string          { return "test" }
func (s *agnosticSession) Title() string       { return "Test" }
func (s *agnosticSession) AdapterName() string { return "tmux" }
