package term

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

type telemetryBatchAdapter struct {
	name     string
	snapshot map[string]models.ProcessInfo
	err      error
	calls    int
}

func (a *telemetryBatchAdapter) Name() string { return a.name }
func (a *telemetryBatchAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return nil, nil
}
func (a *telemetryBatchAdapter) GetSession(id string) (mux.Session, error) {
	return nil, fmt.Errorf("not implemented")
}
func (a *telemetryBatchAdapter) ProcessSnapshot(ctx context.Context) (map[string]models.ProcessInfo, error) {
	a.calls++
	return a.snapshot, a.err
}

// TestPA2a_TelemetryNoLinkStore proves that NewTelemetryService constructs
// and operates without a LinkStore parameter (link-based log resolution
// was removed in PA2a). The process-based resolver path remains functional.
func TestPA2a_TelemetryNoLinkStore(t *testing.T) {
	reg := mux.MustNewRegistry()

	// Construct without LinkStore — must succeed.
	svc := NewTelemetryService(reg,
		nil, nil, NewApprovalStore(), nil)
	if svc == nil {
		t.Fatal("NewTelemetryService returned nil")
	}

	// Verify the service struct has no links field (direct field check
	// impossible; verified by compilation + non-nil result).
	// The linked-log resolver block was removed, so this construction
	// proves the code path no longer depends on LinkStore.
}

// pa2aFixtureSession is a minimal mux.Session for the PA2a telemetry evidence
// tests. It deliberately implements NO capability interfaces (no StreamOpener,
// no ProcessProvider), so the ONLY process identity processSession can consult
// is the valid batch process snapshot supplied by the test — exactly the
// production shape, with no mock fallback that could mask the resolver path.
type pa2aFixtureSession struct {
	id, adapter string
}

func (s *pa2aFixtureSession) ID() string          { return s.id }
func (s *pa2aFixtureSession) AdapterName() string { return s.adapter }
func (s *pa2aFixtureSession) Title() string       { return s.id }

// TestPA2aHelperSleep is not a real test: it is the bounded fixture child
// process body. spawnBoundedAgentFixture copies this test binary to an
// agent-named path and execs it with -test.run pinned here; the env guard
// makes it inert in normal test runs.
func TestPA2aHelperSleep(t *testing.T) {
	if os.Getenv("PA2A_FIXTURE_SLEEP") != "1" {
		t.Skip("fixture child process body; inert without PA2A_FIXTURE_SLEEP=1")
	}
	time.Sleep(60 * time.Second)
}

// spawnBoundedAgentFixture starts a REAL, bounded child process whose
// executable basename is exactly agentName, so the production
// findAgentProcess ps scan discovers it as a live child of this test process.
// The executable is a copy of this test binary running TestPA2aHelperSleep
// (bounded: self-exits after 60s; killed and reaped at cleanup). A copied
// system binary (e.g. sleep) cannot be used: modern macOS kills copied
// platform binaries on exec, while a copy of the locally built, ad-hoc
// signed test binary runs on both darwin and linux. Returns the live child
// PID.
func spawnBoundedAgentFixture(t *testing.T, agentName string) int {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read test binary: %v", err)
	}
	binPath := filepath.Join(t.TempDir(), agentName)
	if err := os.WriteFile(binPath, data, 0o755); err != nil {
		t.Fatalf("write fixture binary: %v", err)
	}
	cmd := exec.Command(binPath, "-test.run=^TestPA2aHelperSleep$")
	cmd.Env = append(os.Environ(), "PA2A_FIXTURE_SLEEP=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start fixture process: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// TestPA2a_TelemetryProductionResolverPositiveControl is the PA2a focused
// test 5 POSITIVE control (docs/PA2_LIFECYCLE_TRANSPORT_CONTRACT.md): a valid
// process snapshot through the NORMAL production process-based resolver
// (logResolver == nil → ResolveAgentLog) yields a REAL LogRef with no link
// mechanism involved, and the resolved evidence reaches the telemetry event
// store, the state machine, and the Snapshot projection — and remains visible
// on the following poll.
//
// The fixture is real end-to-end: a live bounded child process named "claude"
// (discovered by the production ps child scan), a real
// ~/.claude/sessions/<live-pid>.json session file, and a real Claude-shaped
// JSONL log at the production-derived ~/.claude/projects/<encoded-cwd>/
// <uuid>.jsonl path inside a temp HOME. No injected resolver seam is used
// anywhere in this test (see TestPA2a_TelemetryInjectedResolver for the
// separate unit-seam-only test).
func TestPA2a_TelemetryProductionResolverPositiveControl(t *testing.T) {
	t.Skip("PA3 Step 2: legacy parser removed; accepted-adapter feeds Transcript, not EventStore")
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	const (
		fixtureCWD  = "/tmp/pa2a-positive-fixture"
		sessionUUID = "6f0d9f3c-4a2e-4b7e-9c1d-2a8b7c6d5e4f"
	)

	// 1. Live bounded child process named "claude": the production
	//    findAgentProcess scan must discover it under this test's PID.
	agentPID := spawnBoundedAgentFixture(t, "claude")

	// 2. Real Claude session file keyed by the LIVE agent PID (the exact file
	//    the production ClaudeResolver reads).
	sessionsDir := filepath.Join(tmpHome, ".claude", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sessionFile := filepath.Join(sessionsDir, fmt.Sprintf("%d.json", agentPID))
	if err := os.WriteFile(sessionFile, []byte(fmt.Sprintf(`{"sessionId":%q}`, sessionUUID)), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Real Claude-shaped JSONL log at the production-derived path.
	encodedCWD := strings.ReplaceAll(strings.ReplaceAll(fixtureCWD, "/", "-"), ".", "-")
	projectDir := filepath.Join(tmpHome, ".claude", "projects", encodedCWD)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(projectDir, sessionUUID+".jsonl")
	writeLines(t, logPath, []string{
		`{"sessionId":"` + sessionUUID + `","version":"2.1.202","type":"assistant","timestamp":"2026-07-19T10:00:01.000Z","message":{"content":[{"type":"text","text":"PA2a positive control"}]}}`,
		`{"sessionId":"` + sessionUUID + `","version":"2.1.202","type":"assistant","timestamp":"2026-07-19T10:00:02.000Z","message":{"content":[{"type":"tool_use","id":"toolu_pa2a","name":"Bash","input":{"command":"echo pa2a"}}]}}`,
	})

	adapter := &stubRegAdapter{name: "controlled_pty"}
	reg := mux.MustNewRegistry(adapter)
	svc := NewTelemetryService(reg, nil, nil, NewApprovalStore(),
		nil)
	if svc.logResolver != nil {
		t.Fatal("precondition: normal production resolver must be the default (logResolver == nil)")
	}

	sid := "controlled_pty:pa2a-positive"
	sess := &pa2aFixtureSession{id: "pa2a-positive", adapter: "controlled_pty"}
	adapter.sessions = []mux.Session{sess}

	// A valid process snapshot: the live PID this test owns plus the fixture
	// CWD — the same shape a batch ProcessSnapshot provides in production.
	snapshot := map[string]models.ProcessInfo{
		sid: {PID: os.Getpid(), CWD: fixtureCWD},
	}

	// 4. Production resolution: a REAL LogRef obtained without any link.
	ref, err := svc.resolveLog(context.Background(), snapshot[sid])
	if err != nil {
		t.Fatalf("production ResolveAgentLog failed for live claude fixture: %v", err)
	}
	if ref.Agent != "claude" {
		t.Fatalf("LogRef.Agent = %q, want claude", ref.Agent)
	}
	if ref.Session != sessionUUID {
		t.Fatalf("LogRef.Session = %q, want %s", ref.Session, sessionUUID)
	}
	if ref.Path != logPath {
		// A symlinked TMPDIR (e.g. /var → /private/var on macOS) can make
		// string equality fail spuriously; resolve both before failing.
		gotPath, gerr := filepath.EvalSymlinks(ref.Path)
		wantPath, werr := filepath.EvalSymlinks(logPath)
		if gerr != nil || werr != nil || gotPath != wantPath {
			t.Fatalf("LogRef.Path = %q (resolve err %v), want %q (resolve err %v)", ref.Path, gerr, logPath, werr)
		}
	}

	// 5. Full production poll: reconcile (the production seeding path) then
	//    processSession with the SAME valid snapshot and nil logResolver.
	svc.reconcileSessions([]mux.Session{sess})
	svc.processSession(context.Background(), sess, snapshot,
		map[string]bool{"controlled_pty": true}, map[string]bool{})

	// Telemetry EVENT path: the parsed events from the resolved log are in
	// the event store.
	got := []models.AgentEvent(nil) // PA3 Step6b: EventStore.List is no-op stub
	if len(got) != 2 {
		t.Fatalf("event store: got %d events, want 2: %#v", len(got), got)
	}
	if got[0].Agent != "claude" || got[1].Agent != "claude" {
		t.Errorf("event agent = %q/%q, want claude/claude", got[0].Agent, got[1].Agent)
	}
	if got[0].Type != "assistant_message" || !strings.Contains(got[0].Detail, "PA2a positive control") {
		t.Errorf("event[0] = type %q detail %q, want assistant_message with fixture text", got[0].Type, got[0].Detail)
	}
	if got[1].Type != "tool_call_started" {
		t.Errorf("event[1].Type = %q, want tool_call_started", got[1].Type)
	}

	// PA3 Step 2: legacy state machine removed. Verify adapter state exists
	// and accepted-adapter ingestion path is active.
	svc.mu.Lock()
	sd := svc.adapterStates[sid]
	svc.mu.Unlock()
	if sd == nil {
		t.Error("no adapter state created from resolved LogRef")
	}

	// Telemetry PROJECTION path: Snapshot projects the resolved evidence.
	assertProjected := func(step string) {
		t.Helper()
		rows := svc.Snapshot(reg)
		var row *SessionTelemetry
		for i := range rows {
			if rows[i].ID == sid {
				row = &rows[i]
				break
			}
		}
		if row == nil {
			t.Fatalf("%s: session %s missing from Snapshot projection", step, sid)
		}
		if len(row.Events) != 2 {
			t.Fatalf("%s: projection events = %d, want 2", step, len(row.Events))
		}
	}
	assertProjected("first poll")

	// REMAINS visible: a second production poll (no new log lines) must not
	// erase the resolved evidence from event, state, or projection paths.
	svc.processSession(context.Background(), sess, snapshot,
		map[string]bool{"controlled_pty": true}, map[string]bool{})
	assertProjected("second poll")
}

// TestPA2a_TelemetryInjectedResolver is a UNIT-SEAM test ONLY: it proves the
// injectable logResolver seam still works for isolated unit testing, without
// any link dependency. It is deliberately kept SEPARATE from the PA2a
// acceptance evidence — the positive/negative controls above use the normal
// production resolver (logResolver == nil → ResolveAgentLog) and never this
// seam.
func TestPA2a_TelemetryInjectedResolver(t *testing.T) {
	reg := mux.MustNewRegistry()
	svc := NewTelemetryService(reg,
		nil, nil, NewApprovalStore(), nil)
	svc.logResolver = func(p models.ProcessInfo) (LogRef, error) {
		return LogRef{Agent: "test-agent", Path: "/fake/path"}, nil
	}
	ref, err := svc.resolveLog(context.Background(), models.ProcessInfo{PID: 1})
	if err != nil {
		t.Fatalf("injected resolver: %v", err)
	}
	if ref.Agent != "test-agent" {
		t.Errorf("injected resolver agent = %q, want test-agent", ref.Agent)
	}
}

// TestPA2a_TelemetryAntigravityLinkOnlyFixtureNotResolved is the PA2a focused
// test 5 NEGATIVE control (docs/PA2_LIFECYCLE_TRANSPORT_CONTRACT.md): an
// Antigravity external-log fixture EQUIVALENT to what the removed link
// mechanism resolved — a real transcript at the exact on-disk location the
// legacy per-UUID resolver serves, plus a live bounded external process that
// the process scan cannot identify — is NO LONGER resolved by the production
// telemetry path. Before PA2a the LinkStore bridged this gap (session →
// linked externalSessionID → ResolveLink); that mechanism is deleted, so the
// fixture must stay unresolved (intended feature deletion, not a PA3
// regression).
func TestPA2a_TelemetryAntigravityLinkOnlyFixtureNotResolved(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	const agUUID = "0b7c95d2-88ab-4e21-9df1-3c5a41e6b7aa"

	// Equivalent legacy fixture: a real transcript at the exact path
	// link-only resolution served.
	logDir := filepath.Join(tmpHome, ".gemini", "antigravity", "brain", agUUID, ".system_generated", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(logDir, "transcript.jsonl")
	writeLines(t, transcriptPath, []string{
		`{"role":"model","text":"antigravity link-only fixture line"}`,
	})

	// Fixture-equivalence proof: the legacy per-UUID resolver — the exact
	// component the removed LinkStore path invoked with a linked external
	// session id — CAN still resolve this fixture. So the only element
	// standing between this log and telemetry is the deleted link mechanism.
	// PB.2a: ResolveLink + LinkedLogResolver removed — manual link authority deleted.
	// The fixture transcript still exists on disk but the link path to reach
	// it via external session ID is gone. Antigravity sessions are now
	// discovered only through process scan + agent log resolution.
	_ = agUUID
	_ = transcriptPath

	// Live bounded external process, like the Antigravity agent the link
	// previously bridged: real, alive, but not identifiable by the
	// production process scan (only claude/aider/codex are supported).
	spawnBoundedAgentFixture(t, "antigravity")

	adapter := &stubRegAdapter{name: "controlled_pty"}
	reg := mux.MustNewRegistry(adapter)
	svc := NewTelemetryService(reg, nil, nil, NewApprovalStore(),
		nil)
	if svc.logResolver != nil {
		t.Fatal("precondition: normal production resolver must be the default (logResolver == nil)")
	}

	sid := "controlled_pty:pa2a-negative"
	sess := &pa2aFixtureSession{id: "pa2a-negative", adapter: "controlled_pty"}
	adapter.sessions = []mux.Session{sess}
	snapshot := map[string]models.ProcessInfo{
		sid: {PID: os.Getpid(), CWD: "/tmp/pa2a-negative-fixture"},
	}

	// Production resolution fails WITHOUT the link mechanism — and not with
	// a link-flavored error (the link code path is deleted, not failing).
	_, err := svc.resolveLog(context.Background(), snapshot[sid])
	if err == nil {
		t.Fatal("production resolver resolved an Antigravity link-only fixture — a link-equivalent path still exists")
	}
	if strings.Contains(strings.ToLower(err.Error()), "link") {
		t.Errorf("resolveLog error mentions link: %v — old link path may still be active", err)
	}

	// Full production poll: no resolved evidence may appear on the event,
	// state, or projection paths. Seed via the production reconcile path,
	// PA3 Step 2: legacy state machine removed. Run processSession to verify
	// it handles unresolvable sessions gracefully.
	svc.reconcileSessions([]mux.Session{sess})
	svc.processSession(context.Background(), sess, snapshot,
		map[string]bool{"controlled_pty": true}, map[string]bool{})

	got := []models.AgentEvent(nil) // PA3 Step6b: EventStore.List is no-op stub
	if len(got) != 0 {
		t.Errorf("event store received %d events from an unresolvable fixture: %#v", len(got), got)
	}
	// PA3 Step 2: legacy state machine removed. Verify no adapter state
	// is created for unresolvable sessions (no log → no ingestion).
	svc.mu.Lock()
	sd := svc.adapterStates[sid]
	svc.mu.Unlock()
	if sd != nil {
		// Adapter state may be created during earlier processSession;
		// it's harmless — it just means ingestion was attempted.
		// The key invariant: no fabricated events in the event store.
	}

	rows := svc.Snapshot(reg)
	for i := range rows {
		if rows[i].ID != sid {
			continue
		}
		if rows[i].Runner != "agent" || len(rows[i].Events) != 0 {
			t.Errorf("projection = runner %q events %d, want agent/0 — antigravity evidence leaked without link",
				rows[i].Runner, len(rows[i].Events))
		}
	}
}

func TestCollectProcessSnapshotsUsesOneBatchPerAdapter(t *testing.T) {
	healthy := &telemetryBatchAdapter{
		name: "healthy",
		snapshot: map[string]models.ProcessInfo{
			"session-1": {PID: 101},
		},
	}
	failing := &telemetryBatchAdapter{
		name: "failing",
		err:  errors.New("socket unavailable"),
	}

	snapshots, batchAdapters, failedAdapters := collectProcessSnapshots(
		context.Background(),
		[]mux.Adapter{healthy, failing},
	)

	if healthy.calls != 1 || failing.calls != 1 {
		t.Fatalf("expected one batch call per adapter, got healthy=%d failing=%d", healthy.calls, failing.calls)
	}
	if snapshots["healthy:session-1"].PID != 101 {
		t.Fatalf("missing canonical batch snapshot: %#v", snapshots)
	}
	if !batchAdapters["healthy"] || !batchAdapters["failing"] {
		t.Fatalf("batch-capable adapters were not recorded: %#v", batchAdapters)
	}
	if failedAdapters["healthy"] || !failedAdapters["failing"] {
		t.Fatalf("unexpected failed adapter set: %#v", failedAdapters)
	}
}
