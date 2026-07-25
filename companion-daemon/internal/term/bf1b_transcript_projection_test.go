package term

import (
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/transcript"
)

// BF-1B: Codex managed event → Transcript projection.
func TestBF1B_CodexProjection_BasicAssistantEvent(t *testing.T) {
	sid := "codex_app_server:test-001"
	segs := projectCodexEvent(sid, ManagedEventAssistant, "Hello from Codex", time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	s := segs[0]
	if s.SessionID != sid || s.Kind != transcript.KindAgentEvent || s.Source != transcript.SourceAgentEvent || s.Text != "Hello from Codex" || s.AgentKind != "codex" || s.EventType != "assistant_message" {
		t.Errorf("segment mismatch: %+v", s)
	}
}

func TestBF1B_CodexProjection_AllKinds(t *testing.T) {
	sid := "codex_app_server:test-kinds"
	for _, tc := range []struct {
		kind      ManagedEventKind
		eventType string
		text      string
	}{
		{ManagedEventWorking, "working", ""},
		{ManagedEventCompleted, "completed", ""},
		{ManagedEventExited, "exited", ""},
		{ManagedEventGap, "gap", "dropped output"},
	} {
		segs := projectCodexEvent(sid, tc.kind, tc.text, time.Now())
		if len(segs) != 1 {
			t.Errorf("%s: expected 1 segment, got %d", tc.kind, len(segs))
			continue
		}
		if segs[0].EventType != tc.eventType {
			t.Errorf("%s: EventType=%q want %q", tc.kind, segs[0].EventType, tc.eventType)
		}
	}
}

func TestBF1B_CodexProjection_UnknownKindProducesZero(t *testing.T) {
	segs := projectCodexEvent("x", "fantasy_event", "text", time.Now())
	if len(segs) != 0 {
		t.Errorf("unknown kind: expected 0, got %d", len(segs))
	}
}

func TestBF1B_CodexProjection_BoundedText(t *testing.T) {
	long := strings.Repeat("x", 50000)
	segs := projectCodexEvent("s", ManagedEventAssistant, long, time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if len(segs[0].Text) >= 50000 {
		t.Errorf("text not bounded: len=%d", len(segs[0].Text))
	}
}

func TestBF1B_CodexProjection_NoCrossSession(t *testing.T) {
	a := projectCodexEvent("codex_app_server:a", ManagedEventAssistant, "A", time.Now())
	b := projectCodexEvent("codex_app_server:b", ManagedEventAssistant, "B", time.Now())
	if a[0].SessionID == b[0].SessionID || a[0].SessionID != "codex_app_server:a" || b[0].SessionID != "codex_app_server:b" {
		t.Error("cross-session contamination in Codex projection")
	}
}

// BF-1B: Claude JSONL → Transcript projection via normalizer.
func TestBF1B_ClaudeNormalizer_BasicAssistant(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	n := &claudeJSONLNormalizer{
		sessionID:       "claude_headless:t1",
		claudeSessionID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		transcriptSvc:   svc,
	}
	n.projectLine([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello Claude"}]},"sessionId":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`))
	segs := svc.ListTranscript("claude_headless:t1")
	found := false
	for _, s := range segs {
		if s.Text == "Hello Claude" {
			found = true
			if s.Kind != transcript.KindAgentEvent || s.AgentKind != "claude" || s.EventType != "assistant_message" {
				t.Errorf("field mismatch: %+v", s)
			}
		}
	}
	if !found {
		t.Error("Claude assistant text not found in Transcript")
	}
}

func TestBF1B_ClaudeNormalizer_MultiBlockOnlyProjectsText(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	n := &claudeJSONLNormalizer{
		sessionID:       "claude_headless:t2",
		claudeSessionID: "bbbbbbbb-cccc-dddd-eeee-ffffffffffff",
		transcriptSvc:   svc,
	}
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Running command"},{"type":"tool_use","id":"tu_01","name":"Bash","input":{"command":"rm -rf /"}}]},"sessionId":"bbbbbbbb-cccc-dddd-eeee-ffffffffffff"}`
	n.projectLine([]byte(line))
	segs := svc.ListTranscript("claude_headless:t2")
	for _, s := range segs {
		if strings.Contains(s.Text, "tool_use") || strings.Contains(s.Text, "rm -rf") {
			t.Error("tool_use payload must not appear in Transcript text")
		}
	}
}

func TestBF1B_ClaudeNormalizer_SkipsNonAssistant(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	n := &claudeJSONLNormalizer{transcriptSvc: svc, sessionID: "claude_headless:t3"}
	n.projectLine([]byte(`{"type":"system","subtype":"init","sessionId":"cccccccc-dddd-eeee-ffff-000000000000"}`))
	segs := svc.ListTranscript("claude_headless:t3")
	if len(segs) != 0 {
		t.Errorf("system events must not be projected: got %d", len(segs))
	}
}

func TestBF1B_ClaudeNormalizer_EmptyAndMalformed(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	n := &claudeJSONLNormalizer{transcriptSvc: svc}
	n.projectLine(nil)
	n.projectLine([]byte{})
	n.projectLine([]byte(`{not json`))
	n.projectLine([]byte(`{"type":"assistant","message":`))
	if len(svc.ListTranscript("claude_headless:t4")) != 0 {
		t.Error("malformed/empty must produce no segments")
	}
}

func TestBF1B_ClaudeNormalizer_NilTranscriptSvc(t *testing.T) {
	n := &claudeJSONLNormalizer{}
	n.projectLine([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"test"}]}}`))
	// Must not panic
}

func TestBF1B_ClaudeNormalizer_NoCrossSession(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	nA := &claudeJSONLNormalizer{sessionID: "claude_headless:a", transcriptSvc: svc}
	nB := &claudeJSONLNormalizer{sessionID: "claude_headless:b", transcriptSvc: svc}
	nA.projectLine([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"A-data"}]},"sessionId":"a"}`))
	nB.projectLine([]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"B-data"}]},"sessionId":"b"}`))
	for _, s := range svc.ListTranscript("claude_headless:a") {
		if s.Text == "B-data" {
			t.Error("cross-session: A sees B data")
		}
	}
	for _, s := range svc.ListTranscript("claude_headless:b") {
		if s.Text == "A-data" {
			t.Error("cross-session: B sees A data")
		}
	}
}

func TestBF1B_ClaudeNormalizer_BoundedText(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	n := &claudeJSONLNormalizer{sessionID: "claude_headless:tb", transcriptSvc: svc}
	long := strings.Repeat("x", 50000)
	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"` + long + `"}]},"sessionId":"x"}`
	n.projectLine([]byte(line))
	for _, s := range svc.ListTranscript("claude_headless:tb") {
		if len(s.Text) >= 50000 {
			t.Errorf("text not bounded: len=%d", len(s.Text))
		}
	}
}

// BF-1B: Codex transcriptHook integration.
func TestBF1B_CodexTranscriptHook_Integration(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	sid := "codex_app_server:hook-int"
	svc.SetCorrelation(sid, transcript.CorrelationState{SessionID: sid, Correlation: "managed_launch", Provider: "codex"})
	hook := func(kind ManagedEventKind, text string, observedAt time.Time) {
		segs := projectCodexEvent(sid, kind, text, observedAt)
		if len(segs) > 0 {
			svc.FeedAgentSegments(sid, segs)
		}
	}
	hook(ManagedEventWorking, "", time.Now())
	hook(ManagedEventAssistant, "Codex working", time.Now())
	hook(ManagedEventCompleted, "", time.Now())
	segs := svc.ListTranscript(sid)
	if len(segs) < 3 {
		t.Fatalf("expected >=3 segments, got %d", len(segs))
	}
	for _, s := range segs {
		if s.Text == "Codex working" && s.AgentKind != "codex" {
			t.Error("assistant event missing agent kind")
		}
	}
}

// BF-1B: Redaction.
func TestBF1B_Redaction_SecretPatternsBlocked(t *testing.T) {
	secrets := []string{
		"Bearer sk-ant-api-1234567890abcdef",     // redact
		"ANTHROPIC_API_KEY=sk-ant-api-secret",     // redact
		"OPENAI_API_KEY=sk-orca-1234567890abcdef", // redact
		"ghp_1234567890abcdef1234567890abcdef",    // redact
	}
	for _, text := range secrets {
		segs := projectCodexEvent("s", ManagedEventAssistant, text, time.Now())
		if len(segs) > 0 {
			t.Errorf("secret-bearing text must produce zero segments: %q", text[:min2(20, len(text))])
		}
	}
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
