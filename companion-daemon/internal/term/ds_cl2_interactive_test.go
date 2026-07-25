package term

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/transcript"
)

// DS-CL2: Claude interactive host and exact evidence tests.

// TestDS_CL2_SessionUUIDGeneration proves UUIDs are valid v4 format.
func TestDS_CL2_SessionUUIDGeneration(t *testing.T) {
	uuid, err := newClaudeSessionUUID()
	if err != nil {
		t.Fatal(err)
	}
	if len(uuid) != 36 {
		t.Errorf("uuid length = %d, want 36", len(uuid))
	}
	// Version 4 UUID has '4' at position 14 (after second hyphen).
	if uuid[14] != '4' {
		t.Errorf("uuid[14] = %c, want '4'", uuid[14])
	}
	// Variant 1 has '8','9','a','b' at position 19.
	if uuid[19] != '8' && uuid[19] != '9' && uuid[19] != 'a' && uuid[19] != 'b' {
		t.Errorf("uuid[19] = %c, want variant 1", uuid[19])
	}
	t.Logf("UUID: %s", uuid)

	// Two UUIDs should differ.
	uuid2, _ := newClaudeSessionUUID()
	if uuid == uuid2 {
		t.Error("two UUIDs are identical")
	}
}

// TestDS_CL2_HookSettingsGeneration proves hook settings are valid JSON
// and contain the expected structure.
func TestDS_CL2_HookSettingsGeneration(t *testing.T) {
	dir := t.TempDir()
	token := "test-token-abc123"
	port := 12345

	if err := writeInteractiveHookSettings(dir, token, port, "test-nonce-abc"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("missing hooks key")
	}
	if _, ok := hooks["PreToolUse"]; !ok {
		t.Error("missing PreToolUse hook")
	}
	if _, ok := hooks["PostToolUse"]; !ok {
		t.Error("missing PostToolUse hook")
	}

	// Verify the hook command includes the token and port.
	content := string(data)
	if !strings.Contains(content, token) {
		t.Error("hook settings missing token")
	}
	if !strings.Contains(content, "12345") {
		t.Error("hook settings missing port")
	}
}

// TestDS_CL2_JSONLProjector_AssistantMessage proves that assistant JSONL
// lines are projected into Transcript segments.
func TestDS_CL2_JSONLProjector_AssistantMessage(t *testing.T) {
	svc := transcript.NewService(transcript.StoreConfig{MaxSegments: 100})
	sessionID := "controlled_pty:claude-test"

	// Start normalizer manually (no file tailing — we project lines directly).
	n := &claudeJSONLNormalizer{
		sessionID:       sessionID,
		claudeSessionID: "test-uuid-1234",
		transcriptSvc:   svc,
		done:            make(chan struct{}),
	}

	// Set correlation.
	svc.SetCorrelation(sessionID, transcript.CorrelationState{
		SessionID:   sessionID,
		Correlation: "managed_launch",
		Provider:    "claude",
	})

	// Project an assistant message.
	assistantLine := `{"type":"assistant","message":{"id":"msg_1","role":"assistant","content":[{"type":"text","text":"Hello from Claude"}],"model":"claude-5","stop_reason":null}}`
	n.projectLine([]byte(assistantLine))

	segments := svc.ListTranscript(sessionID)
	if len(segments) == 0 {
		t.Fatal("no segments projected")
	}
	s := segments[0]
	if s.Kind != transcript.KindAgentEvent {
		t.Errorf("kind = %s, want agent_event", s.Kind)
	}
	if s.AgentKind != "claude" {
		t.Errorf("agentKind = %s, want claude", s.AgentKind)
	}
	if s.EventType != "assistant_message" {
		t.Errorf("eventType = %s, want assistant_message", s.EventType)
	}
	if s.Text != "Hello from Claude" {
		t.Errorf("text = %q, want 'Hello from Claude'", s.Text)
	}
	if s.Source != transcript.SourceAgentEvent {
		t.Errorf("source = %s, want agent_event", s.Source)
	}

	// Verify primary source is agent_event (correlation set).
	resp := svc.BuildResponse(sessionID, segments)
	if resp.PrimarySource != transcript.SourceAgentEvent {
		t.Errorf("primarySource = %s, want agent_event", resp.PrimarySource)
	}
}

// TestDS_CL2_JSONLProjector_NonAssistantSkipped proves non-assistant lines
// are silently skipped.
func TestDS_CL2_JSONLProjector_NonAssistantSkipped(t *testing.T) {
	svc := transcript.NewService(transcript.StoreConfig{MaxSegments: 100})
	sessionID := "controlled_pty:claude-skip"

	n := &claudeJSONLNormalizer{
		sessionID:     sessionID,
		transcriptSvc: svc,
		done:          make(chan struct{}),
	}

	// User message — should be skipped.
	n.projectLine([]byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"my prompt"}]}}`))
	// System message — should be skipped.
	n.projectLine([]byte(`{"type":"system","message":{"role":"system","content":"system prompt"}}`))
	// Malformed line — should be skipped.
	n.projectLine([]byte(`not json`))
	// Empty line — should be skipped.
	n.projectLine([]byte(``))
	// Tool use assistant — should be skipped (no text content).
	n.projectLine([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{}}]}}`))

	if len(svc.ListTranscript(sessionID)) != 0 {
		t.Errorf("expected 0 segments from non-assistant lines, got %d", len(svc.ListTranscript(sessionID)))
	}
}

// TestDS_CL2_JSONLProjector_MultipleContentBlocks proves the first text
// block is projected from multi-block messages.
func TestDS_CL2_JSONLProjector_MultipleContentBlocks(t *testing.T) {
	svc := transcript.NewService(transcript.StoreConfig{MaxSegments: 100})
	sessionID := "controlled_pty:claude-multi"

	n := &claudeJSONLNormalizer{
		sessionID:     sessionID,
		transcriptSvc: svc,
		done:          make(chan struct{}),
	}

	// Assistant with text + tool_use — text should be projected.
	line := `{"type":"assistant","message":{"id":"msg_2","role":"assistant","content":[{"type":"text","text":"Let me check that"},{"type":"tool_use","id":"tu_1","name":"Bash","input":{"command":"ls"}}]}}`
	n.projectLine([]byte(line))

	segments := svc.ListTranscript(sessionID)
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if segments[0].Text != "Let me check that" {
		t.Errorf("text = %q", segments[0].Text)
	}
}

// TestDS_CL2_JSONLProjector_BoundedText proves text is truncated.
func TestDS_CL2_JSONLProjector_BoundedText(t *testing.T) {
	svc := transcript.NewService(transcript.StoreConfig{MaxSegments: 100})
	sessionID := "controlled_pty:claude-bounded"

	n := &claudeJSONLNormalizer{
		sessionID:     sessionID,
		transcriptSvc: svc,
		done:          make(chan struct{}),
	}

	// Very long text — should be bounded.
	longText := strings.Repeat("x", transcript.MaxTextBytes+1000)
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + longText + `"}]}}`
	n.projectLine([]byte(line))

	segments := svc.ListTranscript(sessionID)
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if len(segments[0].Text) > transcript.MaxTextBytes {
		t.Errorf("text length %d > MaxTextBytes %d", len(segments[0].Text), transcript.MaxTextBytes)
	}
}

// TestDS_CL2_InteractiveHost_NewHost proves construction succeeds.
func TestDS_CL2_InteractiveHost_NewHost(t *testing.T) {
	svc := transcript.NewService(transcript.StoreConfig{MaxSegments: 100})
	authorizer := testMutationAuthorizer{}
	ownedPTY, err := NewOwnedPTYRuntime(authorizer, NewNativePTYLauncher(), svc)
	if err != nil {
		t.Fatal(err)
	}

	host, err := NewClaudeInteractiveHost(
		ClaudeInteractiveConfig{Bin: "claude", Version: "2.1.219", AuthorityVersion: "2.1.219"},
		ownedPTY, svc, authorizer, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if host == nil {
		t.Fatal("host is nil")
	}
}

// TestDS_CL2_FindJSONL tests the JSONL file search.
func TestDS_CL2_FindJSONL(t *testing.T) {
	dir := t.TempDir()
	projDir := filepath.Join(dir, "-TestProject")
	os.MkdirAll(projDir, 0755)
	jsonlPath := filepath.Join(projDir, "test-uuid.jsonl")
	os.WriteFile(jsonlPath, []byte(`{"type":"assistant"}`), 0644)

	found := findClaudeJSONL(dir, "test-uuid")
	if found != jsonlPath {
		t.Errorf("found = %q, want %q", found, jsonlPath)
	}

	// Non-existent UUID.
	notFound := findClaudeJSONL(dir, "nonexistent")
	if notFound != "" {
		t.Errorf("expected empty for nonexistent, got %q", notFound)
	}
}

// TestDS_CL2_InteractiveBridgeStartStop proves the hook bridge can start and stop.
func TestDS_CL2_InteractiveBridgeStartStop(t *testing.T) {
	bridge, err := startClaudeInteractiveBridge(nil)
	if err != nil {
		t.Fatal(err)
	}
	if bridge.token == "" {
		t.Error("token is empty")
	}
	if bridge.port == 0 {
		t.Error("port is 0")
	}
	t.Logf("Bridge started on port %d", bridge.port)

	// Stop should not panic.
	bridge.stop()
}

// TestDS_CL2_CorrelationSet proves correlation is set for Claude sessions.
func TestDS_CL2_CorrelationSet(t *testing.T) {
	svc := transcript.NewService(transcript.StoreConfig{MaxSegments: 100})
	sessionID := "controlled_pty:claude-corr"

	svc.SetCorrelation(sessionID, transcript.CorrelationState{
		SessionID:   sessionID,
		Correlation: "managed_launch",
		Provider:    "claude",
	})

	// Feed agent segments to verify correlation.
	svc.FeedAgentSegments(sessionID, []transcript.TranscriptSegment{{
		SessionID:       sessionID,
		Kind:            transcript.KindAgentEvent,
		Source:          transcript.SourceAgentEvent,
		Text:            "test",
		AgentKind:       "claude",
		EventType:       "assistant_message",
		ObservedAt:      time.Now(),
		ContractVersion: transcript.ContractVersion,
	}})

	segments := svc.ListTranscript(sessionID)
	resp := svc.BuildResponse(sessionID, segments)
	if resp.PrimarySource != transcript.SourceAgentEvent {
		t.Errorf("primarySource = %s, want agent_event", resp.PrimarySource)
	}
	if resp.Availability != transcript.AvailabilityHealthy {
		t.Errorf("availability = %s, want healthy", resp.Availability)
	}
}

// TestDS_CL2_NormalizerStop proves the normalizer can be stopped.
func TestDS_CL2_NormalizerStop(t *testing.T) {
	n := &claudeJSONLNormalizer{
		sessionID: "test",
		done:      make(chan struct{}),
	}
	close(n.done) // Simulate completion.
	n.Stop()
	// Should not block.
}
