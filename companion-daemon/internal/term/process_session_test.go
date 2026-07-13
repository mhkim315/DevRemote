package term

import (
	"context"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// procSessMock implements mux.Session + mux.StreamOpener for processSession tests.
type procSessMock struct {
	id, adapter string
}

func (m *procSessMock) ID() string          { return m.id }
func (m *procSessMock) AdapterName() string { return m.adapter }
func (m *procSessMock) Title() string       { return m.id }
func (m *procSessMock) ProcessInfo(ctx context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{PID: 1234, Command: "codex"}, nil
}
func (m *procSessMock) ProcessSnapshot(ctx context.Context) (map[string]models.ProcessInfo, error) {
	return nil, nil
}
type stubRegAdapter struct {
	name     string
	sessions []mux.Session
}

func (s *stubRegAdapter) Name() string { return s.name }
func (s *stubRegAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return s.sessions, nil
}

// ── B2.1: processSession with accepted adapter independent of legacy parser ──

func TestProcessSession_AcceptedAdapterIndependent(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"assistant","payload":{"message":{"content":[{"type":"text","text":"hello"}]}}}`,
	})

	ts := transcript.NewService(transcript.DefaultStoreConfig())
	adapter := &stubRegAdapter{name: "controlled_pty"}
	reg := mux.MustNewRegistry(adapter)
	events := NewMemoryEventStore()
	links := NewNopLinkStore()
	approvals := NewApprovalStore()
	activity := NewActivityBuffer(100)

	sid := "controlled_pty:mock1"
	sess := &procSessMock{id: "mock1", adapter: "controlled_pty"}
	adapter.sessions = []mux.Session{sess}
	defer transcript.RemoveLaunch(sid)

	transcript.RegisterLaunch(sid, "codex", "controlled_pty", "0.144.1", 0, 1)

	svc := NewTelemetryService(
		reg, events, links, nil, nil, approvals, activity, ts,
	)
	svc.SetLogResolver(func(p models.ProcessInfo) (LogRef, error) {
		return LogRef{Path: logPath, Agent: "codex", Session: sid}, nil
	})

	// Seed session state as the Run loop would.
	svc.mu.Lock()
	svc.sessions[sid] = &sessionStateData{LastActivity: time.Now(), State: "idle", Load: 0}
	svc.mu.Unlock()

	svc.processSession(context.Background(), sess,
		map[string]models.ProcessInfo{sid: {PID: 1234, Command: "codex"}},
		map[string]bool{"controlled_pty": false}, map[string]bool{})

	segments := ts.ListTranscript(sid)
	if len(segments) == 0 {
		t.Fatal("processSession: no Transcript segments produced")
	}

	// Verify exact segment properties, not just KindAgentEvent presence.
	// Accepted adapter must produce correctly-typed events with proper source.
	hasTypedAgentEvent := false
	hasUnknown := false
	for _, seg := range segments {
		t.Logf("segment: kind=%s source=%s agentKind=%s eventType=%s text=%q",
			seg.Kind, seg.Source, seg.AgentKind, seg.EventType, seg.Text)
		if seg.Kind != transcript.KindAgentEvent {
			t.Errorf("unexpected segment kind: %s", seg.Kind)
		}
		if seg.Source != transcript.SourceAgentEvent {
			t.Errorf("unexpected segment source: %s (want agent_event)", seg.Source)
		}
		if seg.AgentKind != "codex" {
			t.Errorf("unexpected agent kind: %s (want codex)", seg.AgentKind)
		}
		if seg.SessionID != sid {
			t.Errorf("wrong session: %s", seg.SessionID)
		}
		// At least one segment must be a typed event (not EventUnknown).
		if seg.EventType != "" && seg.EventType != "unknown" {
			hasTypedAgentEvent = true
		}
		if seg.EventType == "unknown" {
			hasUnknown = true
		}
	}
	if !hasTypedAgentEvent {
		t.Error("expected at least one typed (non-unknown) accepted-adapter event — got only EventUnknown")
	}
	t.Logf("%d Transcript segments via processSession (typed=%v unknown_present=%v)",
		len(segments), hasTypedAgentEvent, hasUnknown)
}

// ── B2.2: version conflict → no AgentEvent segments ──

func TestProcessSession_VersionConflictNoSegments(t *testing.T) {
	dir := t.TempDir()
	logPath := dir + "/codex_bad.jsonl"
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"9.9.9"}}`,
	})

	ts := transcript.NewService(transcript.DefaultStoreConfig())
	reg := mux.MustNewRegistry(&stubRegAdapter{name: "controlled_pty"})

	sid := "controlled_pty:mock2"
	sess := &procSessMock{id: "mock2", adapter: "controlled_pty"}
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterLaunch(sid, "codex", "controlled_pty", "0.144.1", 0, 1)

	svc := NewTelemetryService(
		reg, NewMemoryEventStore(), NewNopLinkStore(), nil, nil,
		NewApprovalStore(), NewActivityBuffer(100), ts,
	)
	svc.SetLogResolver(func(p models.ProcessInfo) (LogRef, error) {
		return LogRef{Path: logPath, Agent: "codex", Session: sid}, nil
	})

	svc.mu.Lock()
	svc.sessions[sid] = &sessionStateData{LastActivity: time.Now(), State: "idle", Load: 0}
	svc.mu.Unlock()

	svc.processSession(context.Background(), sess,
		map[string]models.ProcessInfo{sid: {PID: 1234, Command: "codex"}},
		map[string]bool{"controlled_pty": false}, map[string]bool{})

	segments := ts.ListTranscript(sid)
	agentCount := 0
	for _, seg := range segments {
		if seg.Source == transcript.SourceAgentEvent {
			agentCount++
		}
	}
	if agentCount > 0 {
		t.Errorf("version conflict: expected 0 AgentEvent segments, got %d", agentCount)
	}
	t.Logf("version conflict: %d segments, %d agent_event", len(segments), agentCount)
}

// ── B2.3: snapshot suppressed after terminal input ──

func TestProcessSession_SnapshotSuppressedAfterInput(t *testing.T) {
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	sid := "controlled_pty:snaptest"

	ts.BeginInput(sid, time.Now())
	ts.AddSnapshotSegment(sid, "echoed text from terminal", 20, time.Now())

	segments := ts.ListTranscript(sid)
	for _, seg := range segments {
		if seg.Source == transcript.SourceSnapshot && seg.Kind == transcript.KindTerminalOutput {
			t.Errorf("snapshot text leaked after input: %q", seg.Text)
		}
	}
	t.Logf("snapshot after input: %d segments", len(segments))
}

var _ = agent.EventUnknown
