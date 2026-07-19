package term

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/transcript"
)

var _ = strings.TrimSpace

// ── B4.1 ──

func TestProduction_AcceptedOnlyRecordReachesTranscript(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "codex.jsonl")
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"assistant","payload":{"message":{"content":[{"type":"text","text":"hello"}]}}}`,
	})

	ts := transcript.NewService(transcript.DefaultStoreConfig())
	sid := "controlled_pty:t1"
	defer transcript.RemoveLaunch(sid)

	a := newAdapterState()
	a.resetForGeneration(logPath)
	cursor := &LogCursor{Path: logPath}
	rr, err := ReadRawLines(cursor, 500)
	if err != nil {
		t.Fatalf("ReadRawLines: %v", err)
	}
	a.appendRecords(rr.Lines)
	records, acursor, overflowed := a.buildAdapterInput()
	if overflowed {
		t.Fatal("unexpected overflow")
	}

	events, version, nextCursor, _ := callAcceptedAdapter("codex", records, sid, acursor)
	a.setCursor(nextCursor)
	if version != "0.144.1" {
		t.Errorf("version: got %q, want 0.144.1", version)
	}
	if len(events) == 0 {
		t.Fatal("expected events, got 0")
	}

	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})
	if !a.updateVersion(version, "codex") {
		t.Fatal("updateVersion rejected valid version")
	}
	binding := transcript.LookupLaunch(sid)
	corr := a.launchCorrelation(binding, "controlled_pty", "codex", 0, time.Time{})
	ts.SetCorrelation(sid, transcript.CorrelationState{SessionID: sid, Correlation: corr, Provider: "codex"})
	if corr != contract.CorrelationManagedLaunch {
		t.Fatalf("correlation: got %v, want ManagedLaunch", corr)
	}

	ts.ProjectAgentEvents(sid, events)
	segments := ts.ListTranscript(sid)
	if len(segments) == 0 {
		t.Fatal("no Transcript segments")
	}
	for _, seg := range segments {
		if seg.Source != transcript.SourceAgentEvent {
			t.Errorf("source: got %q, want agent_event", seg.Source)
		}
	}
	t.Logf("%d Transcript segments", len(segments))
}

// ── B4.2 ──

func TestProduction_CursorProgressionAcrossThreePolls(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "codex.jsonl")
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:36.000Z","type":"task_started","payload":{"task_id":"t1"}}`,
	})

	a := newAdapterState()
	a.resetForGeneration(logPath)
	cursor := &LogCursor{Path: logPath}

	rr1, _ := ReadRawLines(cursor, 500)
	a.appendRecords(rr1.Lines)
	recs, ac, _ := a.buildAdapterInput()
	ev1, _, nc1, _ := callAcceptedAdapter("codex", recs, "s", ac)
	a.setCursor(nc1)
	t.Logf("poll1: %d events, cursor=%q", len(ev1), nc1)

	appendLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"assistant","payload":{"message":{"content":[{"type":"text","text":"m1"}]}}}`,
	})
	rr2, _ := ReadRawLines(cursor, 500)
	a.appendRecords(rr2.Lines)
	recs, ac, _ = a.buildAdapterInput()
	ev2, _, nc2, _ := callAcceptedAdapter("codex", recs, "s", ac)
	a.setCursor(nc2)
	t.Logf("poll2: %d events, cursor=%q", len(ev2), nc2)

	appendLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:38.000Z","type":"assistant","payload":{"message":{"content":[{"type":"text","text":"m2"}]}}}`,
	})
	rr3, _ := ReadRawLines(cursor, 500)
	a.appendRecords(rr3.Lines)
	recs, ac, _ = a.buildAdapterInput()
	ev3, _, nc3, _ := callAcceptedAdapter("codex", recs, "s", ac)
	a.setCursor(nc3)
	t.Logf("poll3: %d events, cursor=%q", len(ev3), nc3)

	if nc1 == "" || nc1 == nc2 || nc2 == nc3 {
		t.Error("cursor did not advance")
	}
	ids := make(map[string]bool)
	for _, evs := range [][]agent.AgentEvent{ev1, ev2, ev3} {
		for _, ev := range evs {
			if ids[ev.ID] {
				t.Errorf("duplicate: %q", ev.ID)
			}
			ids[ev.ID] = true
		}
	}
	total := len(ev1) + len(ev2) + len(ev3)
	if total == 0 {
		t.Error("zero events")
	}
	t.Logf("total: %d events", total)
}

// ── B4.3 ──

func TestProduction_VersionConflictRevokesCorrelation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "codex_bad.jsonl")
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"9.9.9"}}`,
	})

	a := newAdapterState()
	a.resetForGeneration(logPath)
	cursor := &LogCursor{Path: logPath}
	rr, _ := ReadRawLines(cursor, 500)
	a.appendRecords(rr.Lines)
	recs, ac, _ := a.buildAdapterInput()
	_, version, _, _ := callAcceptedAdapter("codex", recs, "s", ac)

	if a.updateVersion(version, "codex") {
		t.Error("updateVersion accepted unsupported version 9.9.9")
	}
	if a.versionValid {
		t.Error("versionValid should be false")
	}

	sid := "controlled_pty:vctest"
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})
	corr := a.launchCorrelation(transcript.LookupLaunch(sid), "controlled_pty", "codex", 0, time.Time{})
	if corr != contract.CorrelationUnavailable {
		t.Errorf("got %v, want Unavailable", corr)
	}
}

// ── B4.4 ──

func TestProduction_OverflowMarkerExactlyOnce(t *testing.T) {
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	a := newAdapterState()
	a.resetForGeneration("/p/test.jsonl")
	lines := make([][]byte, 2001)
	for i := range lines {
		lines[i] = []byte(`{"r":` + itoa(i) + `}`)
	}
	a.appendRecords(lines)
	_, _, overflowed := a.buildAdapterInput()
	if !overflowed {
		t.Fatal("expected overflow")
	}
	for poll := 0; poll < 3; poll++ {
		if a.shouldEmitOverflowMarker() {
			ts.EmitDegraded("s", "overflow", time.Now())
		}
	}
	count := 0
	for _, seg := range ts.ListTranscript("s") {
		if seg.Kind == transcript.KindDegraded {
			count++
		}
	}
	if count != 1 {
		t.Errorf("degraded count: got %d, want 1", count)
	}
}

// ── B4.5 ──

func TestProduction_PIDMismatchZeroSegments(t *testing.T) {
	sid := "controlled_pty:pidtest"
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1", PID: 12345})

	a := newAdapterState()
	a.resetForGeneration("/p/test.jsonl")
	a.updateVersion("0.144.1", "codex")

	corr := a.launchCorrelation(transcript.LookupLaunch(sid), "controlled_pty", "codex", 99999, time.Time{})
	if corr != contract.CorrelationUnavailable {
		t.Errorf("PID mismatch: got %v, want Unavailable", corr)
	}
}

// ── B4.6 ──

func TestProduction_ProviderMismatchZeroSegments(t *testing.T) {
	sid := "controlled_pty:provtest"
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})

	a := newAdapterState()
	a.resetForGeneration("/p/test.jsonl")
	a.updateVersion("0.144.1", "codex")

	corr := a.launchCorrelation(transcript.LookupLaunch(sid), "controlled_pty", "claude", 0, time.Time{})
	if corr != contract.CorrelationUnavailable {
		t.Errorf("provider mismatch: got %v, want Unavailable", corr)
	}
}

// ── B4.7 ──

func TestProduction_GenerationResetClearsAllState(t *testing.T) {
	a := newAdapterState()
	a.resetForGeneration("/p/gen1.jsonl")
	a.appendRecords([][]byte{[]byte(`{"a":1}`)})
	a.setCursor("10:hash10")
	a.updateVersion("0.144.1", "codex")

	big := make([]byte, 5000)
	for i := 0; i < 900; i++ {
		a.appendRecords([][]byte{big})
	}
	if !a.overflowed {
		t.Fatal("should overflow")
	}
	if !a.shouldEmitOverflowMarker() {
		t.Fatal("first marker")
	}

	a.resetForGeneration("/p/gen2.jsonl")
	if len(a.records) != 0 || a.adapterCursor != "" || a.versionValid ||
		a.versionConflict || a.overflowed || a.markerEmitted {
		t.Error("state not fully cleared")
	}
}

// ── B4.8 ──

func TestProduction_TranscriptAPIResponseAfterPolls(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "codex.jsonl")
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:36.000Z","type":"task_started","payload":{"task_id":"t1"}}`,
	})

	ts := transcript.NewService(transcript.DefaultStoreConfig())
	sid := "controlled_pty:apitest"
	defer transcript.RemoveLaunch(sid)
	transcript.RegisterFirstLaunch(transcript.LaunchSpec{SessionID: sid, Provider: "codex", Adapter: "controlled_pty", Version: "0.144.1"})

	a := newAdapterState()
	a.resetForGeneration(logPath)
	cursor := &LogCursor{Path: logPath}
	rr, _ := ReadRawLines(cursor, 500)
	a.appendRecords(rr.Lines)
	recs, ac, _ := a.buildAdapterInput()
	events, version, nc, _ := callAcceptedAdapter("codex", recs, sid, ac)
	a.setCursor(nc)
	a.updateVersion(version, "codex")

	corr := a.launchCorrelation(transcript.LookupLaunch(sid), "controlled_pty", "codex", 0, time.Time{})
	ts.SetCorrelation(sid, transcript.CorrelationState{SessionID: sid, Correlation: corr, Provider: "codex"})
	ts.ProjectAgentEvents(sid, events)

	resp := ts.BuildResponse(sid, ts.ListTranscript(sid))
	if resp.SessionID != sid {
		t.Errorf("session: %q", resp.SessionID)
	}
	if resp.PrimarySource != transcript.SourceAgentEvent {
		t.Errorf("source: %q", resp.PrimarySource)
	}
	if len(resp.Semantic) == 0 {
		t.Error("no semantic segments")
	}
	t.Logf("API: %d semantic, %d fallback", len(resp.Semantic), len(resp.Fallback))
}

// ── B4.9 ──

func TestProduction_AcceptedAdapterIndependentOfLegacyParser(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "codex.jsonl")
	writeLines(t, logPath, []string{
		`{"timestamp":"2026-07-06T13:29:35.399Z","type":"session_meta","payload":{"session_id":"s1","cli_version":"0.144.1"}}`,
		`{"timestamp":"2026-07-06T13:29:37.000Z","type":"user","payload":{"message":{"role":"user","content":[{"type":"text","text":"prompt"}]}}}`,
	})

	a := newAdapterState()
	a.resetForGeneration(logPath)
	cursor := &LogCursor{Path: logPath}
	rr, _ := ReadRawLines(cursor, 500)
	a.appendRecords(rr.Lines)
	recs, ac, _ := a.buildAdapterInput()

	ev, _, _, _ := callAcceptedAdapter("codex", recs, "s", ac)
	t.Logf("%d events from accepted adapter (independent of legacy parser)", len(ev))
	if len(ev) == 0 {
		t.Error("expected events")
	}
}

// ── Helpers ──

func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	for _, line := range lines {
		f.WriteString(line + "\n")
	}
}

func appendLines(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	defer f.Close()
	for _, line := range lines {
		f.WriteString(line + "\n")
	}
}
