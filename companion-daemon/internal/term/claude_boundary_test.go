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

// Phase A5 product-boundary bridge: prove Claude adapter output
// flows through the actual SessionTelemetry schema.

func TestClaudeOutput_ProductBridge(t *testing.T) {
	lines := loadAgentFixtures(t, "claude")
	if len(lines) == 0 {
		t.Fatal("no Claude fixtures")
	}

	// Step 1: Claude agent pipeline.
	detector := agent.NewClaudeDetector()
	id := detector.Detect(agent.DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
	})
	parser := agent.NewClaudeParser()
	result := parser.ParseBatch(lines, "")

	// Step 2: Build a real SessionTelemetry with agent data.
	// This is what the telemetry pipeline will do in A8-A9.
	st := SessionTelemetry{
		ID:              "tmux:claude-session",
		Adapter:         "tmux",
		State:           string(result.Status),
		Capabilities:    []string{"live_stream", "screen", "history"},
		AgentKind:       id.Kind,
		AgentStatus:     string(result.Status),
		AgentConfidence: id.Confidence,
	}

	// Step 3: Verify JSON round-trip through real product schema.
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("SessionTelemetry marshal: %v", err)
	}
	var roundtrip SessionTelemetry
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("SessionTelemetry unmarshal: %v", err)
	}
	if roundtrip.AgentKind != "claude" {
		t.Errorf("AgentKind=%q, want claude", roundtrip.AgentKind)
	}
	if roundtrip.AgentStatus == "" {
		t.Error("AgentStatus is empty")
	}

	// Step 4: Verify JSON contains all agent fields.
	str := string(data)
	for _, want := range []string{`"agentKind":"claude"`, `"agentStatus"`, `"agentConfidence"`} {
		if !strings.Contains(str, want) {
			t.Errorf("JSON missing %s", want)
		}
	}

	// Step 5: Prove backward compatibility — fields are omitempty.
	noAgent := SessionTelemetry{ID: "tmux:test", Adapter: "tmux"}
	data2, _ := json.Marshal(noAgent)
	if strings.Contains(string(data2), "agentKind") {
		t.Error("backward compat: agentKind present in session without agent")
	}

	// Step 6: Prove the full pipeline output (events) is JSON compatible.
	if len(result.Events) == 0 {
		t.Error("0 events from Claude fixtures")
	}
	for _, e := range result.Events {
		if e.Type == "" {
			t.Error("event has empty Type")
		}
	}
	eventData, _ := json.Marshal(result.Events)
	if len(eventData) == 0 {
		t.Error("events JSON is empty")
	}

	// Step 7: Agent layer failure must not break terminal.
	reg := mux.MustNewRegistry(&emptyAdapter{})
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()}
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/sessions: status %d (agent failure isolated)", rec.Code)
	}

	// Step 8: Resolver produces redacted DisplayPath.
	resolver := agent.NewClaudeLogResolver()
	res, _ := resolver.Resolve(agent.DetectionEvidence{
		ProcessName: "claude",
		CWD:         "/Users/test/project",
	})
	for _, lr := range res.Logs {
		if lr.DisplayPath == "" {
			t.Error("DisplayPath is empty")
		}
		if strings.Contains(lr.DisplayPath, "/Users/") {
			t.Errorf("DisplayPath contains raw path: %s", lr.DisplayPath)
		}
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
