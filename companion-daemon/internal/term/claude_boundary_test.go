package term

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/mux"
)

// Phase A5: Prove Claude adapter output is compatible with the SessionTelemetry
// product boundary. This test shows that if the telemetry pipeline were wired to
// the agent adapter, the output would flow correctly.

func TestClaudeOutput_TelemetrySchema(t *testing.T) {
	// Load A1 Claude fixtures.
	lines := loadAgentFixtures(t, "claude")
	if len(lines) == 0 {
		t.Fatal("no Claude fixtures")
	}

	// Parse via Claude adapter.
	parser := agent.NewClaudeParser()
	result := parser.ParseBatch(lines, "")

	if result.Degraded {
		t.Fatalf("parse degraded: %v", result.Diagnostics)
	}
	if len(result.Events) == 0 {
		t.Fatal("0 events from Claude fixtures")
	}

	// Prove events are compatible with SessionTelemetry schema:
	// each event has a Type that maps to a capability or state.
	for _, e := range result.Events {
		if e.Type == "" {
			t.Error("event has empty Type")
		}
		if e.AgentKind != "claude" {
			t.Errorf("event AgentKind=%q, want claude", e.AgentKind)
		}
	}

	// Prove the full output (status + events) would fit into telemetry JSON.
	// Create a mock telemetry entry to verify JSON serialization.
	type mockActivity struct {
		AgentKind string             `json:"agentKind"`
		Status    agent.AgentStatus  `json:"status"`
		Events    []agent.AgentEvent `json:"events,omitempty"`
	}
	activity := mockActivity{
		AgentKind: "claude",
		Status:    result.Status,
		Events:    result.Events,
	}
	data, err := json.Marshal(activity)
	if err != nil {
		t.Fatalf("JSON marshal: %v", err)
	}
	if len(data) == 0 {
		t.Error("JSON output is empty")
	}

	// Verify it deserializes.
	var roundtrip mockActivity
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	if roundtrip.AgentKind != "claude" {
		t.Errorf("roundtrip AgentKind=%q", roundtrip.AgentKind)
	}
}

func TestClaudeOutput_DetectBoundary(t *testing.T) {
	detector := agent.NewClaudeDetector()
	resolver := agent.NewClaudeLogResolver()

	// Simulate evidence that would come from a terminal session.
	ev := agent.DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
	}

	id := detector.Detect(ev)
	if id.Kind != "claude" {
		t.Fatalf("detect: Kind=%q, want claude", id.Kind)
	}

	result, _ := resolver.Resolve(ev)
	for _, lr := range result.Logs {
		if lr.DisplayPath == "" {
			t.Error("DisplayPath is empty")
		}
	}

	// Prove the identity is JSON-serializable for API response.
	data, _ := json.Marshal(id)
	if !strings.Contains(string(data), "claude") {
		t.Error("JSON identity missing claude")
	}
}

func TestClaudeOutput_UnsupportedBySession(t *testing.T) {
	// Session without agent capabilities must not break.
	reg := mux.MustNewRegistry(&emptyAdapter{})
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()}

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/sessions: status %d (agent layer must not break terminal)", rec.Code)
	}
}

// --- helpers ---

func loadAgentFixtures(t *testing.T, agentName string) [][]byte {
	t.Helper()
	base := filepath.Join("..", "agent", "testdata", agentName)
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Skipf("fixtures not found: %v", err)
		return nil
	}
	var all [][]byte
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".jsonl" {
			data, err := os.ReadFile(filepath.Join(base, e.Name()))
			if err != nil {
				continue
			}
			for _, line := range splitAgentLines(data) {
				if len(line) > 0 {
					all = append(all, line)
				}
			}
		}
	}
	return all
}

func splitAgentLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
