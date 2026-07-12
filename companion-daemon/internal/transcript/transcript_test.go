package transcript

import (
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
)

// ── Store tests ──

func TestStoreAppendAndList(t *testing.T) {
	s := NewStore(DefaultStoreConfig())
	sessionID := "test:session1"

	seg := NewTerminalOutputSegment(sessionID, "hello world", 11, time.Now())
	s.Append(sessionID, []TranscriptSegment{seg})

	list := s.List(sessionID)
	if len(list) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(list))
	}
	if list[0].Text != "hello world" {
		t.Errorf("expected 'hello world', got %q", list[0].Text)
	}
	if list[0].Seq != 1 {
		t.Errorf("expected Seq=1, got %d", list[0].Seq)
	}
	if list[0].Kind != KindTerminalOutput {
		t.Errorf("expected KindTerminalOutput, got %s", list[0].Kind)
	}
	if list[0].Source != SourceByteStream {
		t.Errorf("expected SourceByteStream, got %s", list[0].Source)
	}
	if list[0].ID == "" {
		t.Error("expected non-empty ID")
	}
	if list[0].ContractVersion != ContractVersion {
		t.Errorf("expected ContractVersion %s, got %s", ContractVersion, list[0].ContractVersion)
	}
}

func TestStoreListEmpty(t *testing.T) {
	s := NewStore(DefaultStoreConfig())
	list := s.List("nonexistent")
	if list != nil {
		t.Errorf("expected nil for empty session, got %v", list)
	}
}

func TestStoreListAfter(t *testing.T) {
	s := NewStore(DefaultStoreConfig())
	sid := "test:session2"

	for i := 0; i < 5; i++ {
		seg := NewTerminalOutputSegment(sid, "line", 4, time.Now())
		s.Append(sid, []TranscriptSegment{seg})
	}

	// ListAfter with cursor=0 returns all.
	all := s.ListAfter(sid, 0)
	if len(all) != 5 {
		t.Fatalf("expected 5, got %d", len(all))
	}

	// ListAfter with cursor=3 returns last 2.
	after := s.ListAfter(sid, 3)
	if len(after) != 2 {
		t.Fatalf("expected 2, got %d", len(after))
	}
	if after[0].Seq != 4 {
		t.Errorf("expected Seq=4, got %d", after[0].Seq)
	}

	// ListAfter with cursor beyond last returns nil.
	beyond := s.ListAfter(sid, 100)
	if beyond != nil {
		t.Errorf("expected nil, got %v", beyond)
	}
}

func TestStoreClear(t *testing.T) {
	s := NewStore(DefaultStoreConfig())
	sid := "test:session3"

	s.Append(sid, []TranscriptSegment{NewTerminalOutputSegment(sid, "data", 4, time.Now())})
	if s.List(sid) == nil {
		t.Fatal("expected segments after append")
	}

	s.Clear(sid)
	if s.List(sid) != nil {
		t.Error("expected nil after clear")
	}

	// Idempotent.
	s.Clear(sid)
	if s.List(sid) != nil {
		t.Error("expected nil after second clear")
	}
}

func TestStoreCrossSessionIsolation(t *testing.T) {
	s := NewStore(DefaultStoreConfig())

	s.Append("sessionA", []TranscriptSegment{NewTerminalOutputSegment("sessionA", "A data", 6, time.Now())})
	s.Append("sessionB", []TranscriptSegment{NewTerminalOutputSegment("sessionB", "B data", 6, time.Now())})

	if len(s.List("sessionA")) != 1 {
		t.Error("sessionA should have 1 segment")
	}
	if len(s.List("sessionB")) != 1 {
		t.Error("sessionB should have 1 segment")
	}

	// Clear one session doesn't affect the other.
	s.Clear("sessionA")
	if s.List("sessionA") != nil {
		t.Error("sessionA should be empty")
	}
	if len(s.List("sessionB")) != 1 {
		t.Error("sessionB should still have 1 segment")
	}
}

func TestStoreCapacityEviction(t *testing.T) {
	cfg := StoreConfig{MaxSegments: 5, MaxTotalBytes: 1024 * 1024}
	s := NewStore(cfg)
	sid := "test:evict"

	for i := 0; i < 10; i++ {
		seg := NewTerminalOutputSegment(sid, "data data data data", 20, time.Now())
		s.Append(sid, []TranscriptSegment{seg})
	}

	list := s.List(sid)
	if len(list) > cfg.MaxSegments+1 { // +1 for gap marker
		t.Errorf("expected at most %d segments (+gap), got %d", cfg.MaxSegments+1, len(list))
	}

	// First segment should be a degraded gap marker.
	if list[0].Kind != KindDegraded {
		t.Errorf("expected gap marker after eviction, got Kind=%s", list[0].Kind)
	}

	// Latest segments should be the most recent.
	if list[len(list)-1].Text != "data data data data" {
		t.Errorf("expected last segment to have original text")
	}
}

func TestStoreStats(t *testing.T) {
	s := NewStore(DefaultStoreConfig())
	sid := "test:stats"

	stats := s.Stats(sid)
	if stats.SegmentCount != 0 {
		t.Error("expected 0 segments initially")
	}

	s.Append(sid, []TranscriptSegment{
		NewTerminalOutputSegment(sid, "hello", 5, time.Now()),
		NewTerminalOutputSegment(sid, "world", 5, time.Now()),
	})

	stats = s.Stats(sid)
	if stats.SegmentCount != 2 {
		t.Errorf("expected 2 segments, got %d", stats.SegmentCount)
	}
	if stats.LatestSeq != 2 {
		t.Errorf("expected LatestSeq=2, got %d", stats.LatestSeq)
	}
}

// ── AgentEvent projector tests ──

func TestAgentEventProjectorBasic(t *testing.T) {
	proj := NewAgentEventProjector()
	event := agent.AgentEvent{
		ID:         "evt-001",
		SessionID:  "test:session",
		AgentKind:  "claude",
		Type:       agent.EventAssistantMessage,
		Text:       "Here is the result of your query.",
		Confidence: 0.95,
		Timestamp:  time.Now(),
		Provenance: "native_log",
	}

	seg := proj.Project(event, "test:session")
	if seg == nil {
		t.Fatal("expected non-nil segment")
	}
	if seg.Kind != KindAgentEvent {
		t.Errorf("expected KindAgentEvent, got %s", seg.Kind)
	}
	if seg.Source != SourceAgentEvent {
		t.Errorf("expected SourceAgentEvent, got %s", seg.Source)
	}
	if seg.Text != "Here is the result of your query." {
		t.Errorf("unexpected text: %q", seg.Text)
	}
	if seg.AgentEventRef != "evt-001" {
		t.Errorf("expected AgentEventRef='evt-001', got %q", seg.AgentEventRef)
	}
}

func TestAgentEventProjectorThinkingRedacted(t *testing.T) {
	proj := NewAgentEventProjector()
	event := agent.AgentEvent{
		ID:        "evt-002",
		SessionID: "test:session",
		AgentKind: "claude",
		Type:      agent.EventThinking,
		Text:      "Let me think about this problem step by step...",
	}

	seg := proj.Project(event, "test:session")
	if seg == nil {
		t.Fatal("expected non-nil segment (event is projected, but text is empty)")
	}
	if seg.Text != "" {
		t.Errorf("thinking text must be redacted, got %q", seg.Text)
	}
}

func TestAgentEventProjectorToolCallNameOnly(t *testing.T) {
	proj := NewAgentEventProjector()
	event := agent.AgentEvent{
		ID:        "evt-003",
		SessionID: "test:session",
		AgentKind: "claude",
		Type:      agent.EventToolCallStarted,
		Text:      `{"command": "rm -rf /", "path": "/secret"}`,
		ToolName:  "bash",
	}

	seg := proj.Project(event, "test:session")
	if seg == nil {
		t.Fatal("expected non-nil segment")
	}
	// Text must NOT contain the tool input.
	if seg.Text != "" {
		t.Errorf("tool call text must be empty (input redacted), got %q", seg.Text)
	}
	// ToolName is identity metadata, safe to expose.
	if seg.ToolName != "bash" {
		t.Errorf("expected ToolName='bash', got %q", seg.ToolName)
	}
}

func TestAgentEventProjectorCrossSessionRejected(t *testing.T) {
	proj := NewAgentEventProjector()
	event := agent.AgentEvent{
		ID:        "evt-004",
		SessionID: "other:session",
		AgentKind: "codex",
		Type:      agent.EventAssistantMessage,
		Text:      "This should not appear.",
	}

	// Project for a different session.
	seg := proj.Project(event, "test:session")
	if seg != nil {
		t.Error("cross-session event must not be projected")
	}
}

func TestAgentEventProjectorBatch(t *testing.T) {
	proj := NewAgentEventProjector()
	events := []agent.AgentEvent{
		{ID: "e1", SessionID: "s", AgentKind: "claude", Type: agent.EventAssistantMessage, Text: "one"},
		{ID: "e2", SessionID: "other", AgentKind: "codex", Type: agent.EventAssistantMessage, Text: "skip"},
		{ID: "e3", SessionID: "s", AgentKind: "claude", Type: agent.EventAssistantMessage, Text: "three"},
	}

	segs := proj.ProjectBatch(events, "s")
	if len(segs) != 2 {
		t.Fatalf("expected 2 segments (one cross-session skipped), got %d", len(segs))
	}
	if segs[0].Text != "one" {
		t.Errorf("expected 'one', got %q", segs[0].Text)
	}
	if segs[1].Text != "three" {
		t.Errorf("expected 'three', got %q", segs[1].Text)
	}
}

func TestAgentEventProjectorUnknownType(t *testing.T) {
	proj := NewAgentEventProjector()
	event := agent.AgentEvent{
		ID:        "evt-005",
		SessionID: "test:session",
		AgentKind: "unknown",
		Type:      agent.EventUnknown,
	}

	seg := proj.Project(event, "test:session")
	if seg == nil {
		t.Fatal("unknown events should still be projected (ordering preserved)")
	}
	if seg.Kind != KindAgentEvent {
		t.Errorf("expected KindAgentEvent for unknown, got %s", seg.Kind)
	}
	if seg.Text != "" {
		t.Errorf("unknown event text must be empty, got %q", seg.Text)
	}
}

func TestAgentEventProjectorUserMessageRedacted(t *testing.T) {
	proj := NewAgentEventProjector()
	// User message that looks like code (3+ code indicators).
	event := agent.AgentEvent{
		ID:        "evt-006",
		SessionID: "test:session",
		AgentKind: "claude",
		Type:      agent.EventUserMessage,
		Text:      "func main() { fmt.Println(\"hello\") } // package main import \"fmt\"",
	}

	seg := proj.Project(event, "test:session")
	if seg == nil {
		t.Fatal("expected non-nil segment")
	}
	if seg.Text == event.Text {
		t.Error("code-like user message must be redacted")
	}
}

// ── Byte-stream projector tests ──

func TestByteStreamProjectorBasic(t *testing.T) {
	bp := NewByteStreamProjector(DefaultByteStreamConfig())
	sid := "test:bs"

	// Feed a complete line.
	segs := bp.Feed(sid, []byte("hello world\n"), time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].Text != "hello world" {
		t.Errorf("expected 'hello world', got %q", segs[0].Text)
	}
	if segs[0].Kind != KindTerminalOutput {
		t.Errorf("expected KindTerminalOutput, got %s", segs[0].Kind)
	}
}

func TestByteStreamProjectorMultiline(t *testing.T) {
	bp := NewByteStreamProjector(DefaultByteStreamConfig())
	sid := "test:bs"

	segs := bp.Feed(sid, []byte("line1\nline2\nline3\n"), time.Now())
	if len(segs) != 3 {
		t.Fatalf("expected 3 segments, got %d", len(segs))
	}
	if segs[0].Text != "line1" {
		t.Errorf("expected 'line1', got %q", segs[0].Text)
	}
	if segs[2].Text != "line3" {
		t.Errorf("expected 'line3', got %q", segs[2].Text)
	}
}

func TestByteStreamProjectorPartialLine(t *testing.T) {
	bp := NewByteStreamProjector(DefaultByteStreamConfig())
	sid := "test:bs"

	// Feed partial line — no segment yet.
	segs := bp.Feed(sid, []byte("partial"), time.Now())
	if len(segs) != 0 {
		t.Errorf("expected 0 segments for partial line, got %d", len(segs))
	}

	// Complete the line.
	segs = bp.Feed(sid, []byte(" line\n"), time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment after newline, got %d", len(segs))
	}
	if segs[0].Text != "partial line" {
		t.Errorf("expected 'partial line', got %q", segs[0].Text)
	}
}

func TestByteStreamProjectorCRHandling(t *testing.T) {
	bp := NewByteStreamProjector(DefaultByteStreamConfig())
	sid := "test:bs"

	// CR ends the line.
	segs := bp.Feed(sid, []byte("progress\r"), time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].Text != "progress" {
		t.Errorf("expected 'progress', got %q", segs[0].Text)
	}
}

func TestByteStreamProjectorBackspace(t *testing.T) {
	bp := NewByteStreamProjector(DefaultByteStreamConfig())
	sid := "test:bs"

	// Feed "helx\x08lo\n" — backspace erases the 'x'.
	segs := bp.Feed(sid, []byte("helx\x08lo\n"), time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].Text != "hello" {
		t.Errorf("expected 'hello' (backspace erased 'x'), got %q", segs[0].Text)
	}
}

func TestByteStreamProjectorInputBoundarySuppression(t *testing.T) {
	bp := NewByteStreamProjector(DefaultByteStreamConfig())
	sid := "test:bs"
	now := time.Now()

	// Start input — feed should return the boundary marker.
	boundary := bp.BeginInput(sid, now)
	if boundary == nil {
		t.Fatal("expected input boundary segment")
	}
	if boundary.Kind != KindInputBoundary {
		t.Errorf("expected KindInputBoundary, got %s", boundary.Kind)
	}
	if boundary.Text != "" {
		t.Errorf("input boundary must be content-free, got text: %q", boundary.Text)
	}

	// While input is active, bytes are suppressed.
	segs := bp.Feed(sid, []byte("rm -rf / --no-preserve-root\n"), now)
	if len(segs) != 0 {
		t.Errorf("input bytes must be suppressed, got %d segments: %q", len(segs), segs[0].Text)
	}

	// End input — normal projection resumes.
	bp.EndInput(sid, now)
	segs = bp.Feed(sid, []byte("output after input\n"), now)
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment after input ends, got %d", len(segs))
	}
	if segs[0].Text != "output after input" {
		t.Errorf("expected 'output after input', got %q", segs[0].Text)
	}

	// Verify diagnostics show suppression.
	diag := bp.Diagnostics()
	if diag.TotalSuppressed == 0 {
		t.Error("expected non-zero TotalSuppressed")
	}
}

func TestByteStreamOutputPreserved(t *testing.T) {
	// Repeated log lines are preserved — not globally deduplicated.
	bp := NewByteStreamProjector(DefaultByteStreamConfig())
	sid := "test:bs"

	segs := bp.Feed(sid, []byte("ERROR: something\nERROR: something\nERROR: something\n"), time.Now())
	if len(segs) != 3 {
		t.Fatalf("repeated lines must be preserved, expected 3, got %d", len(segs))
	}
	for i, seg := range segs {
		if seg.Text != "ERROR: something" {
			t.Errorf("segment %d: expected 'ERROR: something', got %q", i, seg.Text)
		}
	}
}

func TestByteStreamProjectorFlush(t *testing.T) {
	bp := NewByteStreamProjector(DefaultByteStreamConfig())
	sid := "test:bs"

	// Feed partial line without newline.
	bp.Feed(sid, []byte("unfinished"), time.Now())

	// Flush should emit the partial line.
	segs := bp.Flush(sid, time.Now())
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment from flush, got %d", len(segs))
	}
	if segs[0].Text != "unfinished" {
		t.Errorf("expected 'unfinished', got %q", segs[0].Text)
	}
}

// ── Source arbitration tests ──

func TestSourceArbiterInitial(t *testing.T) {
	arb := NewSourceArbiter()
	// Without AgentEvent, primary source is byte-stream (the fallback).
	if arb.PrimarySource() != SourceByteStream {
		t.Errorf("expected SourceByteStream initially, got %s", arb.PrimarySource())
	}
	if arb.HasAgentEvents() {
		t.Error("expected no agent events initially")
	}
	if arb.SuppressByteStream() {
		t.Error("byte-stream should NOT be suppressed without AgentEvent primary")
	}
}

func TestSourceArbiterAgentEventPrimary(t *testing.T) {
	arb := NewSourceArbiter()
	arb.RecordAgentEvent()
	if arb.PrimarySource() != SourceAgentEvent {
		t.Errorf("expected SourceAgentEvent, got %s", arb.PrimarySource())
	}
	if !arb.HasAgentEvents() {
		t.Error("expected HasAgentEvents=true")
	}
}

func TestSourceArbiterByteStreamFallback(t *testing.T) {
	arb := NewSourceArbiter()
	arb.RecordByteStream()
	if arb.PrimarySource() != SourceByteStream {
		t.Errorf("expected SourceByteStream, got %s", arb.PrimarySource())
	}
	if arb.HasAgentEvents() {
		t.Error("expected HasAgentEvents=false (only byte stream)")
	}
}

func TestSourceArbiterAgentWinsOverByteStream(t *testing.T) {
	arb := NewSourceArbiter()
	arb.RecordByteStream()
	arb.RecordAgentEvent()
	// Agent events take priority.
	if arb.PrimarySource() != SourceAgentEvent {
		t.Errorf("expected SourceAgentEvent (primary), got %s", arb.PrimarySource())
	}
}

func TestSourceArbiterDegraded(t *testing.T) {
	arb := NewSourceArbiter()
	if arb.IsDegraded() {
		t.Error("expected not degraded initially")
	}
	arb.RecordDegraded()
	if !arb.IsDegraded() {
		t.Error("expected degraded after RecordDegraded")
	}
}

// ── Service integration tests ──

func TestServiceProjectAgentEvents(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:svc"

	svc.ProjectAgentEvents(sid, []agent.AgentEvent{
		{ID: "e1", SessionID: sid, AgentKind: "claude", Type: agent.EventAssistantMessage, Text: "hello", Provenance: "native_log"},
	})

	list := svc.ListTranscript(sid)
	if len(list) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(list))
	}
	if list[0].Kind != KindAgentEvent {
		t.Errorf("expected KindAgentEvent, got %s", list[0].Kind)
	}
	if !svc.HasAgentEvents(sid) {
		t.Error("expected HasAgentEvents=true")
	}
}

func TestServiceFeedBytes(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:svc"

	svc.FeedBytes(sid, []byte("terminal output\n"), time.Now())

	list := svc.ListTranscript(sid)
	if len(list) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(list))
	}
	if list[0].Kind != KindTerminalOutput {
		t.Errorf("expected KindTerminalOutput, got %s", list[0].Kind)
	}
}

func TestServiceBothSources(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:svc"

	svc.FeedBytes(sid, []byte("fallback output\n"), time.Now())
	svc.ProjectAgentEvents(sid, []agent.AgentEvent{
		{ID: "e1", SessionID: sid, AgentKind: "claude", Type: agent.EventAssistantMessage, Text: "semantic output", Provenance: "native_log"},
	})

	list := svc.ListTranscript(sid)
	if len(list) != 2 {
		t.Fatalf("expected 2 segments (both sources preserved), got %d", len(list))
	}

	// Both sources present with explicit labels; no merging.
	kinds := map[SegmentKind]bool{}
	for _, seg := range list {
		kinds[seg.Kind] = true
	}
	if !kinds[KindAgentEvent] || !kinds[KindTerminalOutput] {
		t.Error("both AgentEvent and TerminalOutput segments must be present")
	}

	// Primary source should be AgentEvent.
	if svc.PrimarySource(sid) != SourceAgentEvent {
		t.Errorf("expected SourceAgentEvent, got %s", svc.PrimarySource(sid))
	}
}

func TestServiceClearTranscript(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:svc"

	svc.FeedBytes(sid, []byte("data\n"), time.Now())
	svc.ProjectAgentEvents(sid, []agent.AgentEvent{
		{ID: "e1", SessionID: sid, AgentKind: "claude", Type: agent.EventAssistantMessage, Text: "data", Provenance: "native_log"},
	})

	if len(svc.ListTranscript(sid)) == 0 {
		t.Fatal("expected segments before clear")
	}

	svc.ClearTranscript(sid)
	if len(svc.ListTranscript(sid)) != 0 {
		t.Error("expected empty after clear")
	}

	// After clear, no primary source.
	if svc.PrimarySource(sid) != SourceUnknown {
		t.Errorf("expected SourceUnknown after clear, got %s", svc.PrimarySource(sid))
	}
}

func TestServiceTranscriptStats(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	sid := "test:svc"

	svc.FeedBytes(sid, []byte("line1\nline2\nline3\n"), time.Now())

	stats := svc.TranscriptStats(sid)
	if stats.SegmentCount != 3 {
		t.Errorf("expected 3 segments, got %d", stats.SegmentCount)
	}
}

func TestServiceCrossSessionIsolation(t *testing.T) {
	svc := NewService(DefaultStoreConfig())

	svc.ProjectAgentEvents("sessionA", []agent.AgentEvent{
		{ID: "ea", SessionID: "sessionA", AgentKind: "claude", Type: agent.EventAssistantMessage, Text: "A only", Provenance: "native_log"},
	})
	svc.ProjectAgentEvents("sessionB", []agent.AgentEvent{
		{ID: "eb", SessionID: "sessionB", AgentKind: "codex", Type: agent.EventAssistantMessage, Text: "B only", Provenance: "native_log"},
	})

	if len(svc.ListTranscript("sessionA")) != 1 {
		t.Error("sessionA should have 1 segment")
	}
	if len(svc.ListTranscript("sessionB")) != 1 {
		t.Error("sessionB should have 1 segment")
	}

	svc.ClearTranscript("sessionA")
	if len(svc.ListTranscript("sessionA")) != 0 {
		t.Error("sessionA should be empty")
	}
	if len(svc.ListTranscript("sessionB")) != 1 {
		t.Error("sessionB should still have 1 segment")
	}
}

// ── Echo privacy proof test ──

// TestEchoPrivacy proves that typed command content cannot enter Transcript
// through the Recorder output path (PTY echo). The production-path mechanism
// is BeginInput → echo suppression → EndInput, which is content-free.
func TestEchoPrivacy(t *testing.T) {
	bp := NewByteStreamProjector(DefaultByteStreamConfig())
	sid := "test:echo"

	now := time.Now()
	// Simulate normal output before input.
	bp.Feed(sid, []byte("$ "), now) // prompt (partial line)

	// Begin input — content-free boundary.
	boundary := bp.BeginInput(sid, now)
	if boundary == nil {
		t.Fatal("BeginInput must return an input boundary marker")
	}
	if boundary.Kind != KindInputBoundary {
		t.Errorf("expected KindInputBoundary, got %s", boundary.Kind)
	}
	// Content-free verification: boundary Text is always empty.
	if boundary.Text != "" {
		t.Errorf("input boundary text must be empty (content-free), got %q", boundary.Text)
	}

	// Simulate PTY echo of a typed command. These bytes would normally come
	// through Recorder output as the PTY echoes the typed characters.
	typedCommand := []byte("echo 'sensitive data'\n")
	segs := bp.Feed(sid, typedCommand, now)
	if len(segs) != 0 {
		t.Errorf("PTY echo of typed command must NOT enter Transcript, got: %q", segs[0].Text)
	}

	// End input.
	bp.EndInput(sid, now)

	// Subsequent output is projected normally.
	segs = bp.Feed(sid, []byte("sensitive data\n"), now)
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment for output after input, got %d", len(segs))
	}
	if segs[0].Text != "sensitive data" {
		t.Errorf("expected 'sensitive data', got %q", segs[0].Text)
	}

	// Verify diagnostics: the typed command bytes were suppressed.
	diag := bp.Diagnostics()
	if diag.TotalSuppressed == 0 {
		t.Error("Expected non-zero suppressed bytes for echo privacy")
	}
}

// TestInputBoundaryIsContentFree verifies that the input boundary segment
// contains absolutely no input content, prompt text, or timing data.
func TestInputBoundaryIsContentFree(t *testing.T) {
	seg := NewInputBoundarySegment("test:session", time.Now())

	if seg.Text != "" {
		t.Errorf("input boundary Text must be empty, got %q", seg.Text)
	}
	if seg.Kind != KindInputBoundary {
		t.Errorf("expected KindInputBoundary, got %s", seg.Kind)
	}
	// No content-carrying fields should be set.
	if seg.AgentEventRef != "" {
		t.Error("input boundary must not carry AgentEventRef")
	}
	if seg.AgentKind != "" {
		t.Error("input boundary must not carry AgentKind")
	}
	if seg.ToolName != "" {
		t.Error("input boundary must not carry ToolName")
	}
	if seg.DegradedReason != "" {
		t.Error("input boundary must not carry DegradedReason")
	}
	if seg.ByteCount != 0 {
		t.Errorf("input boundary ByteCount must be 0, got %d", seg.ByteCount)
	}
}

// ── Segment field safety tests ──

func TestSegmentTextTruncation(t *testing.T) {
	longText := ""
	for i := 0; i < MaxTextBytes+100; i++ {
		longText += "x"
	}

	seg := NewTerminalOutputSegment("test", longText, len(longText), time.Now())
	if len(seg.Text) > MaxTextBytes {
		t.Errorf("text must be bounded to MaxTextBytes: %d > %d", len(seg.Text), MaxTextBytes)
	}
}

func TestDegradedSegmentReasonBounded(t *testing.T) {
	longReason := ""
	for i := 0; i < MaxDegradedReasonBytes+100; i++ {
		longReason += "r"
	}

	seg := NewDegradedSegment("test", longReason, time.Now())
	if len(seg.DegradedReason) > MaxDegradedReasonBytes {
		t.Errorf("degraded reason must be bounded: %d > %d", len(seg.DegradedReason), MaxDegradedReasonBytes)
	}
}

// ── ANSI detection tests ──

func TestIsAlternateScreenStart(t *testing.T) {
	if IsAlternateScreenStart([]byte("\x1b[?1049h")) {
		// Valid alternate screen start.
	} else {
		t.Error("expected true for alternate screen start")
	}
	if IsAlternateScreenStart([]byte("normal text")) {
		t.Error("expected false for normal text")
	}
}

func TestIsAlternateScreenEnd(t *testing.T) {
	if IsAlternateScreenEnd([]byte("\x1b[?1049l")) {
		// Valid alternate screen end.
	} else {
		t.Error("expected true for alternate screen end")
	}
	if IsAlternateScreenEnd([]byte("normal text")) {
		t.Error("expected false for normal text")
	}
}

// ── Concurrent safety tests ──

func TestStoreConcurrentAppend(t *testing.T) {
	s := NewStore(DefaultStoreConfig())
	sid := "test:concurrent"

	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				seg := NewTerminalOutputSegment(sid, "data", 4, time.Now())
				s.Append(sid, []TranscriptSegment{seg})
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	list := s.List(sid)
	if len(list) == 0 {
		t.Error("expected segments after concurrent appends")
	}
	// Seq should be monotonic (no duplicates).
	for i := 1; i < len(list); i++ {
		if list[i].Seq <= list[i-1].Seq {
			t.Errorf("Seq not monotonic at index %d: %d <= %d", i, list[i].Seq, list[i-1].Seq)
		}
	}
}
