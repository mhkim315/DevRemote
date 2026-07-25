package term

import (
	"testing"
	"time"

	"devremote/companion-daemon/internal/transcript"
)

// ── R4: Codex managed-to-transcript projection tests ──

// TestR4_ProjectCodexAssistant proves ManagedEventAssistant → TranscriptSegment.
func TestR4_ProjectCodexAssistant(t *testing.T) {
	segs := projectCodexEvent("codex_app_server:test", ManagedEventAssistant, "hello world", time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	s := segs[0]
	if s.Kind != transcript.KindAgentEvent {
		t.Errorf("kind = %s, want agent_event", s.Kind)
	}
	if s.Source != transcript.SourceAgentEvent {
		t.Errorf("source = %s, want agent_event", s.Source)
	}
	if s.AgentKind != "codex" {
		t.Errorf("agentKind = %s, want codex", s.AgentKind)
	}
	if s.EventType != "assistant_message" {
		t.Errorf("eventType = %s, want assistant_message", s.EventType)
	}
	if s.Text != "hello world" {
		t.Errorf("text = %s, want hello world", s.Text)
	}
	if s.SessionID != "codex_app_server:test" {
		t.Errorf("sessionId = %s", s.SessionID)
	}
}

// TestR4_ProjectCodexWorking proves ManagedEventWorking → TranscriptSegment.
func TestR4_ProjectCodexWorking(t *testing.T) {
	segs := projectCodexEvent("codex_app_server:test", ManagedEventWorking, "", time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].EventType != "working" {
		t.Errorf("eventType = %s, want working", segs[0].EventType)
	}
	if segs[0].Text != "" {
		t.Errorf("text should be empty for working event")
	}
}

// TestR4_ProjectCodexCompleted proves ManagedEventCompleted → TranscriptSegment.
func TestR4_ProjectCodexCompleted(t *testing.T) {
	segs := projectCodexEvent("codex_app_server:test", ManagedEventCompleted, "", time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].EventType != "completed" {
		t.Errorf("eventType = %s, want completed", segs[0].EventType)
	}
}

// TestR4_ProjectCodexExited proves ManagedEventExited → TranscriptSegment.
func TestR4_ProjectCodexExited(t *testing.T) {
	segs := projectCodexEvent("codex_app_server:test", ManagedEventExited, "", time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].EventType != "exited" {
		t.Errorf("eventType = %s, want exited", segs[0].EventType)
	}
}

// TestR4_ProjectCodexGap proves ManagedEventGap → explicit gap segment (BF-1B).
// Gaps must be visible in the Transcript so the UI can show gap markers.
func TestR4_ProjectCodexGap(t *testing.T) {
	segs := projectCodexEvent("codex_app_server:test", ManagedEventGap, "events dropped", time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment for gap kind, got %d", len(segs))
	}
	if segs[0].EventType != "gap" {
		t.Errorf("expected event type gap, got %q", segs[0].EventType)
	}
	if segs[0].DegradedReason != "events dropped" {
		t.Errorf("expected degraded reason 'events dropped', got %q", segs[0].DegradedReason)
	}
}

// TestR4_ProjectCodexUnknownKind proves unknown kind → no segment.
func TestR4_ProjectCodexUnknownKind(t *testing.T) {
	segs := projectCodexEvent("codex_app_server:test", ManagedEventKind("unknown"), "", time.Now())
	if len(segs) != 0 {
		t.Errorf("expected 0 segments for unknown kind, got %d", len(segs))
	}
}

// TestR4_TranscriptService_FedFromProjector proves the transcript service
// accepts segments from the Codex projector and makes them readable.
func TestR4_TranscriptService_FedFromProjector(t *testing.T) {
	svc := transcript.NewService(transcript.StoreConfig{MaxSegments: 100})
	sessionID := "codex_app_server:test-fed"

	// Set correlation so agent events are primary.
	svc.SetCorrelation(sessionID, transcript.CorrelationState{
		SessionID:   sessionID,
		Correlation: "managed_launch",
		Provider:    "codex",
	})

	// Simulate what appendEvent does: project + feed.
	segs := projectCodexEvent(sessionID, ManagedEventAssistant, "hello from codex", time.Now())
	svc.FeedAgentSegments(sessionID, segs)

	segs = projectCodexEvent(sessionID, ManagedEventWorking, "", time.Now())
	svc.FeedAgentSegments(sessionID, segs)

	segs = projectCodexEvent(sessionID, ManagedEventCompleted, "", time.Now())
	svc.FeedAgentSegments(sessionID, segs)

	// Read back via the transcript API.
	all := svc.ListTranscript(sessionID)
	if len(all) < 3 {
		t.Fatalf("expected at least 3 segments, got %d", len(all))
	}

	// First should be assistant.
	if all[0].Kind != transcript.KindAgentEvent {
		t.Errorf("seg[0].kind = %s, want agent_event", all[0].Kind)
	}
	if all[0].EventType != "assistant_message" {
		t.Errorf("seg[0].eventType = %s, want assistant_message", all[0].EventType)
	}
	if all[0].Text != "hello from codex" {
		t.Errorf("seg[0].text = %s", all[0].Text)
	}

	// Verify availability reflects healthy state.
	resp := svc.BuildResponse(sessionID, all)
	if resp.Availability != transcript.AvailabilityHealthy {
		t.Errorf("availability = %s, want healthy", resp.Availability)
	}
	if resp.PrimarySource != transcript.SourceAgentEvent {
		t.Errorf("primarySource = %s, want agent_event", resp.PrimarySource)
	}

	// Verify agent events go to semantic channel.
	semanticAgentCount := 0
	for _, s := range resp.Semantic {
		if s.Kind == transcript.KindAgentEvent {
			semanticAgentCount++
		}
	}
	if semanticAgentCount < 3 {
		t.Errorf("semantic agent events = %d, want >= 3", semanticAgentCount)
	}
}

// TestR4_TranscriptService_NoCorrelation_AgentEventsStillVisible proves that
// agent event segments appear in the response regardless of correlation.
// Correlation gates ProjectAgentEvents (legacy adapter path), not the managed
// projector path which uses FeedAgentSegments directly.
func TestR4_TranscriptService_NoCorrelation_AgentEventsStillVisible(t *testing.T) {
	svc := transcript.NewService(transcript.StoreConfig{MaxSegments: 100})
	sessionID := "codex_app_server:test-no-correlation"

	// Feed bytes without setting correlation.
	svc.FeedBytes(sessionID, []byte("raw bytes"), time.Now(), 1)

	// Project agent events via the managed projector path without correlation.
	segs := projectCodexEvent(sessionID, ManagedEventAssistant, "hello", time.Now())
	svc.FeedAgentSegments(sessionID, segs)

	all := svc.ListTranscript(sessionID)
	resp := svc.BuildResponse(sessionID, all)

	// Agent events are still visible even without correlation.
	// They appear in the semantic channel.
	agentCount := 0
	for _, s := range resp.Semantic {
		if s.Kind == transcript.KindAgentEvent {
			agentCount++
		}
	}
	if agentCount == 0 {
		t.Error("expected agent events in semantic channel even without correlation")
	}
}

// TestR4_BoundedText proves text is bounded to MaxTextBytes.
func TestR4_BoundedText(t *testing.T) {
	short := "hello"
	if got := transcript.BoundedText(short); got != short {
		t.Errorf("short text: got %q, want %q", got, short)
	}

	long := make([]byte, transcript.MaxTextBytes+100)
	for i := range long {
		long[i] = 'x'
	}
	bounded := transcript.BoundedText(string(long))
	if len(bounded) > transcript.MaxTextBytes {
		t.Errorf("bounded text length = %d, want <= %d", len(bounded), transcript.MaxTextBytes)
	}
	if len(bounded) < transcript.MaxTextBytes-10 {
		t.Errorf("bounded text too short: %d", len(bounded))
	}
}

// TestR4_ManagedCodexService_SetTranscriptService proves nil is safe.
func TestR4_ManagedCodexService_SetTranscriptService(t *testing.T) {
	svc := NewManagedCodexServiceForTest(nil, func() error { return nil }, testMutationAuthorizer{})
	// Nil transcript service should be safe (projection disabled).
	svc.SetTranscriptService(nil)
	// This should not panic.
	if svc.transcriptSvc != nil {
		t.Error("transcriptSvc should be nil")
	}
}
