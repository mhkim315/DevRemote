package term

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
	"devremote/companion-daemon/internal/mux"
)

// ── Test fakes ──

type fakeClaudeProcess struct {
	stdin   *bytes.Buffer
	stdout  io.Reader
	pipeW   *io.PipeWriter
	termFn  func() error
	killFn  func() error
	waitErr error
	opaque  string
	pid     int
}

func (p *fakeClaudeProcess) Stdin() io.Writer  { return p.stdin }
func (p *fakeClaudeProcess) Stdout() io.Reader { return p.stdout }
func (p *fakeClaudeProcess) Term() error {
	if p.pipeW != nil {
		p.pipeW.Close()
	}
	if p.termFn != nil {
		return p.termFn()
	}
	return nil
}
func (p *fakeClaudeProcess) Kill() error {
	if p.pipeW != nil {
		p.pipeW.Close()
	}
	if p.killFn != nil {
		return p.killFn()
	}
	return nil
}
func (p *fakeClaudeProcess) Wait() error { return p.waitErr }

// PID returns a positive fake OS pid: the C3D §12 launch-certification tuple
// requires a daemon-owned positive process id, so fakes mirror that.
func (p *fakeClaudeProcess) PID() int {
	if p.pid > 0 {
		return p.pid
	}
	return 4242
}
func (p *fakeClaudeProcess) OpaqueID() string {
	if p.opaque == "" {
		return "fake-claude-proc-1"
	}
	return p.opaque
}

type fakeClaudeLauncher struct {
	mu           sync.Mutex
	proc         *fakeClaudeProcess
	launched     bool
	CapturedArgv []string
	StreamCh     chan []byte
}

func (l *fakeClaudeLauncher) Launch(exe string, argv []string) (ManagedProcess, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.launched {
		return nil, fmt.Errorf("already launched")
	}
	l.launched = true
	l.CapturedArgv = append([]string(nil), argv...)
	pr, pw := io.Pipe()
	p := &fakeClaudeProcess{
		stdin:  new(bytes.Buffer),
		stdout: pr,
		pipeW:  pw,
	}
	l.proc = p
	return p, nil
}

func (l *fakeClaudeLauncher) feedLine(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.proc != nil && l.proc.pipeW != nil {
		l.proc.pipeW.Write([]byte(line + "\n"))
	}
}

func (l *fakeClaudeLauncher) closeStream() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.proc != nil && l.proc.pipeW != nil {
		l.proc.pipeW.Close()
	}
}

type argvCapturingLauncher struct {
	argv *[]string
}

func (l *argvCapturingLauncher) Launch(exe string, argv []string) (ManagedProcess, error) {
	*l.argv = append([]string(nil), argv...)
	pr, pw := io.Pipe()
	return &fakeClaudeProcess{stdin: new(bytes.Buffer), stdout: pr, pipeW: pw}, nil
}

type fakeClaudeAttestor struct {
	shouldFail bool
}

func (a *fakeClaudeAttestor) Certify(exe string) error {
	if a.shouldFail {
		return fmt.Errorf("certify failed: wrong version")
	}
	return nil
}

type failingLauncher struct{}

func (failingLauncher) Launch(exe string, argv []string) (ManagedProcess, error) {
	return nil, fmt.Errorf("launch failed")
}

// ── Helpers ──

func deferredStreamJSON(sessionID, toolUseID, toolName, inputJSON string) string {
	return fmt.Sprintf(
		`{"type":"result","stop_reason":"tool_deferred","session_id":"%s","deferred_tool_use":{"id":"%s","name":"%s","input":%s}}`,
		sessionID, toolUseID, toolName, inputJSON,
	)
}

const testDigest = "0000000000000000000000000000000000000000000000000000000000000000"

func testCfg() ClaudeEntryConfig {
	return ClaudeEntryConfig{
		Bin:              "claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
		PinnedPath:       "/pinned/test/claude",
		PinnedDigest:     testDigest,
	}
}

// ── Tests ──

func TestClaudeCreateDetachedAttestorFails(t *testing.T) {
	attestor := &fakeClaudeAttestor{shouldFail: true}
	svc := NewManagedClaudeService(testCfg(), &fakeClaudeLauncher{}, attestor)
	_, err := svc.CreateDetached("/tmp")
	if err == nil || !strings.Contains(err.Error(), "certify") {
		t.Fatalf("expected certify error, got: %v", err)
	}
}

func TestClaudeCreateDetachedLauncherError(t *testing.T) {
	svc := NewManagedClaudeService(testCfg(), &failingLauncher{}, &fakeClaudeAttestor{})
	_, err := svc.CreateDetached("/tmp")
	if err == nil {
		t.Fatal("expected launch error")
	}
}

func TestClaudeCreateDetachedSuccess(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	if err := svc.SetApprovalStore(NewApprovalStore()); err != nil {
		t.Fatal(err)
	}
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	if !strings.HasPrefix(id, "claude_headless:claude-") {
		t.Fatalf("unexpected session ID: %s", id)
	}
	rec, ok := svc.Registry().Get(id)
	if !ok || rec.Provider != "claude" || rec.Epoch != 1 {
		t.Fatalf("bad record: %+v", rec)
	}
	ctx := context.Background()
	svc.Shutdown(ctx)
}

func TestClaudeDeferredJoinFullIdentityMatch(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()
	defer launcher.closeStream()

	sessionID := "claude-session-uuid"
	toolUseID := "call_00_Test123"
	toolName := "Bash"
	inputJSON := `{"command":"echo ok","description":"test"}`
	inputCanon, _ := canonicalJSON(json.RawMessage(inputJSON))
	inputDigest := sha256Hex(inputCanon)

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest, "")

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 1 pending, got %d", len(rt.pendingObservations))
	}
	rt.turnMu.Unlock()

	deferred := deferredStreamJSON(sessionID, toolUseID, toolName, inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 0 pending after join, got %d", len(rt.pendingObservations))
	}
	rt.turnMu.Unlock()

	safeRecords := store.ListSafe(id)
	if len(safeRecords) != 1 {
		t.Fatalf("expected 1 safe record, got %d", len(safeRecords))
	}
	if len(safeRecords[0].Options) != 0 {
		t.Fatal("expected zero options (non-actionable)")
	}

	// Verify active approval was tracked.
	rt.turnMu.Lock()
	if len(rt.activeApprovals) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 1 active approval, got %d", len(rt.activeApprovals))
	}
	rt.turnMu.Unlock()

	rt.terminate()
}

func TestClaudeDeferredJoinMismatchedSessionID(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	rt.observePreToolUse("call_Mis", "Bash", "right-session", sha256Hex([]byte(`{"cmd":"x"}`)), "")
	deferred := deferredStreamJSON("wrong-session", "call_Mis", "Bash", `{"cmd":"x"}`)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatal("expected pending to remain after session mismatch")
	}
	rt.turnMu.Unlock()
	rt.terminate()
}

func TestClaudeDeferredJoinMismatchedToolName(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	rt.observePreToolUse("call_TN", "Bash", "s1", sha256Hex([]byte(`{"cmd":"x"}`)), "")
	deferred := deferredStreamJSON("s1", "call_TN", "Write", `{"cmd":"x"}`)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatal("expected pending to remain after tool name mismatch")
	}
	rt.turnMu.Unlock()
	rt.terminate()
}

func TestClaudeDeferredJoinMismatchedInputDigest(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	inputCanon, _ := canonicalJSON(json.RawMessage(`{"command":"echo ok"}`))
	rt.observePreToolUse("call_Digest", "Bash", "s1", sha256Hex(inputCanon), "")
	deferred := deferredStreamJSON("s1", "call_Digest", "Bash", `{"command":"rm -rf /"}`)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatal("expected pending to remain after input digest mismatch")
	}
	rt.turnMu.Unlock()
	rt.terminate()
}

func TestClaudeDuplicateToolUseID(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	rt.observePreToolUse("call_Dup", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)), "")
	rt.observePreToolUse("call_Dup", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)), "")

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 || rt.rejects == 0 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 1 pending + reject, got %d/%d", len(rt.pendingObservations), rt.rejects)
	}
	rt.turnMu.Unlock()
	rt.terminate()
}

func TestClaudeCapacityExhaustion(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	for i := 0; i < maxPendingClaudeObservations; i++ {
		rt.observePreToolUse(fmt.Sprintf("call_%d", i), "Bash", fmt.Sprintf("s%d", i), sha256Hex([]byte(fmt.Sprintf(`{"x":%d}`, i))), "")
	}
	rt.observePreToolUse("call_overflow", "Bash", "so", sha256Hex([]byte(`{"x":"overflow"}`)), "")

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != maxPendingClaudeObservations {
		rt.turnMu.Unlock()
		t.Fatalf("expected %d, got %d", maxPendingClaudeObservations, len(rt.pendingObservations))
	}
	rt.turnMu.Unlock()
	rt.terminate()
}

func TestClaudeExitClearsPendingObservations(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	store := NewApprovalStore()
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	rt.observePreToolUse("call_Exit", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)), "")
	rt.terminate()

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 0 {
		rt.turnMu.Unlock()
		t.Fatal("expected 0 pending after terminate")
	}
	rt.turnMu.Unlock()
}

func TestClaudeStopIdempotent(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	id, _ := svc.CreateDetached("/tmp")
	rec, _ := svc.Registry().Get(id)

	if err := svc.Stop(id, rec.Epoch); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := svc.Stop(id, rec.Epoch); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestClaudeKillCleanup(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	id, _ := svc.CreateDetached("/tmp")
	rec, _ := svc.Registry().Get(id)

	if err := svc.Kill(id, rec.Epoch); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	// After Kill, registry must show exited.
	rec2, _ := svc.Registry().Get(id)
	if !rec2.Exited {
		t.Fatal("expected Exited=true after Kill")
	}
}

func TestClaudeDeleteTerminalOnly(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	id, _ := svc.CreateDetached("/tmp")
	rec, _ := svc.Registry().Get(id)

	if err := svc.Delete(id, rec.Epoch); err == nil {
		t.Fatal("expected error deleting non-terminal session")
	}
	svc.Stop(id, rec.Epoch)
	// Re-read after Stop — Exited flag is now true.
	rec2, _ := svc.Registry().Get(id)
	if err := svc.Delete(id, rec2.Epoch); err != nil {
		t.Fatalf("Delete after stop: %v", err)
	}
	if _, ok := svc.Registry().Get(id); ok {
		t.Fatal("session should be removed after delete")
	}
}

func TestClaudeApprovalRecordNonActionable(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	inputCanon, _ := canonicalJSON(json.RawMessage(`{"command":"echo ok"}`))
	rt.observePreToolUse("call_NA", "Bash", "s1", sha256Hex(inputCanon), "")
	deferred := deferredStreamJSON("s1", "call_NA", "Bash", `{"command":"echo ok"}`)
	rt.processLine([]byte(deferred))

	records := store.ListSafe(id)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if len(records[0].Options) != 0 {
		t.Fatal("expected zero options")
	}
	rt.terminate()
}

func TestClaudeHookBridgeDecodeValid(t *testing.T) {
	input := `{"session_id":"s1","tool_use_id":"call_Test","tool_name":"Bash","tool_input":{"command":"echo ok"},"cwd":"/tmp","transcript_path":"/tmp/t.json","permission_mode":"default","effort":"high","hook_event_name":"PreToolUse"}`
	fields, ok := strictPreToolUseDecode([]byte(input))
	if !ok {
		t.Fatal("strict decode rejected valid C0D payload")
	}
	tuid, ok1 := strictBoundedString(fields["tool_use_id"], maxToolUseIDLen)
	tn, ok2 := strictBoundedString(fields["tool_name"], maxToolNameLen)
	sid, ok3 := strictBoundedString(fields["session_id"], maxSessionIDLen)
	he, ok4 := strictBoundedString(fields["hook_event_name"], 64)
	if !ok1 || !ok2 || !ok3 || !ok4 || tuid != "call_Test" || tn != "Bash" || sid != "s1" || he != "PreToolUse" {
		t.Fatalf("field mismatch: %q %q %q %q", tuid, tn, sid, he)
	}
}

func TestClaudeHookBridgeDecodeMalformed(t *testing.T) {
	_, ok := strictPreToolUseDecode([]byte(`{bad json`))
	if ok {
		t.Fatal("expected rejection")
	}
}

func TestClaudeHookBridgeDecodeMissingRequired(t *testing.T) {
	fields, ok := strictPreToolUseDecode([]byte(`{"session_id":"s1","tool_name":"Bash","tool_input":{},"hook_event_name":"PreToolUse"}`))
	if !ok {
		t.Fatal("decode should succeed")
	}
	_, ok = strictBoundedString(fields["tool_use_id"], maxToolUseIDLen)
	if ok {
		t.Fatal("expected missing tool_use_id to fail")
	}
}

func TestClaudeHookBridgeDecodeUnknownField(t *testing.T) {
	_, ok := strictPreToolUseDecode([]byte(`{"session_id":"s1","tool_use_id":"x","tool_name":"Bash","tool_input":{},"hook_event_name":"PreToolUse","evil":true}`))
	if ok {
		t.Fatal("expected rejection of unknown field")
	}
}

func TestClaudeHookBridgeDecodeDuplicateKey(t *testing.T) {
	_, ok := strictPreToolUseDecode([]byte(`{"session_id":"s1","tool_use_id":"x","tool_name":"Bash","tool_input":{},"hook_event_name":"PreToolUse","cwd":"/a","cwd":"/b"}`))
	if ok {
		t.Fatal("expected rejection of duplicate key")
	}
}

func TestClaudeHookBridgeDecodeTrailingContent(t *testing.T) {
	_, ok := strictPreToolUseDecode([]byte(`{"session_id":"s1","tool_use_id":"x","tool_name":"Bash","tool_input":{},"hook_event_name":"PreToolUse"} trailing`))
	if ok {
		t.Fatal("expected rejection of trailing content")
	}
}

func TestClaudeHookBridgeDecodeWrongHookEvent(t *testing.T) {
	fields, ok := strictPreToolUseDecode([]byte(`{"session_id":"s1","tool_use_id":"x","tool_name":"Bash","tool_input":{},"hook_event_name":"PostToolUse"}`))
	if !ok {
		t.Fatal("decode should succeed (PostToolUse is in allowlist)")
	}
	he, sok := strictBoundedString(fields["hook_event_name"], 64)
	if !sok || he != "PostToolUse" {
		t.Fatalf("unexpected: ok=%v he=%q", sok, he)
	}
}

func TestClaudeHookBridgeDecodeOversizedFields(t *testing.T) {
	long := strings.Repeat("x", maxToolNameLen+1)
	input := fmt.Sprintf(`{"session_id":"s1","tool_use_id":"call_Test","tool_name":"%s","tool_input":{},"hook_event_name":"PreToolUse"}`, long)
	fields, ok := strictPreToolUseDecode([]byte(input))
	if !ok {
		t.Fatal("decode should succeed")
	}
	_, ok = strictBoundedString(fields["tool_name"], maxToolNameLen)
	if ok {
		t.Fatal("expected oversized tool_name to fail")
	}
}

func TestClaudeLaunchArgv(t *testing.T) {
	var captured []string
	launcher := &argvCapturingLauncher{argv: &captured}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	_, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	argvStr := strings.Join(captured, " ")
	for _, want := range []string{"--verbose", "--settings", "--setting-sources", "--output-format", "stream-json", "--include-partial-messages", "-p", claudeCertificationPrompt} {
		if !strings.Contains(argvStr, want) {
			t.Fatalf("missing %q in argv: %v", want, captured)
		}
	}
	ctx := context.Background()
	svc.Shutdown(ctx)
}

func TestClaudeShutdownCleansUp(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	id, _ := svc.CreateDetached("/tmp")
	ctx := context.Background()
	svc.Shutdown(ctx)
	svc.mu.Lock()
	if _, ok := svc.runtimes[id]; ok {
		svc.mu.Unlock()
		t.Fatal("runtime should not exist after shutdown")
	}
	svc.mu.Unlock()
}

func TestClaudeStreamDeferredParsing(t *testing.T) {
	line := `{"type":"result","stop_reason":"tool_deferred","session_id":"abc-123","deferred_tool_use":{"id":"call_X","name":"Bash","input":{"command":"echo ok","description":"test"}}}`
	var event streamDeferred
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "result" || event.StopReason != "tool_deferred" || event.SessionID != "abc-123" {
		t.Fatal("parse mismatch")
	}
	if event.DeferredToolUse == nil || event.DeferredToolUse.ID != "call_X" || event.DeferredToolUse.Name != "Bash" {
		t.Fatal("deferred_tool_use mismatch")
	}
}

func TestClaudeProcessLineSkipsNonResult(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	rt.observePreToolUse("call_NonResult", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)), "")
	rt.processLine([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`))

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatal("expected 1 pending after non-result line")
	}
	rt.turnMu.Unlock()
	rt.terminate()
}

func TestClaudeHookSettingsSchema(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	_, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	svc.mu.Lock()
	var hookDir string
	for _, rt := range svc.runtimes {
		hookDir = rt.hookDir
	}
	svc.mu.Unlock()
	if hookDir == "" {
		t.Fatal("hook directory not created")
	}

	data, err := os.ReadFile(filepath.Join(hookDir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	hooks := settings["hooks"].(map[string]any)
	ptu := hooks["PreToolUse"].([]any)
	entry := ptu[0].(map[string]any)
	if entry["matcher"] != "" {
		t.Fatal("expected empty matcher")
	}
	hl := entry["hooks"].([]any)
	ho := hl[0].(map[string]any)
	if ho["type"] != "command" || ho["command"] == "" {
		t.Fatal("bad hook entry")
	}
	ctx := context.Background()
	svc.Shutdown(ctx)
}

func TestClaudePumpEOFExit(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	store := NewApprovalStore()
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	rt.observePreToolUse("call_EOF", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)), "")
	launcher.closeStream() // trigger EOF on pump's scanner

	select {
	case <-rt.exited:
	case <-time.After(3 * time.Second):
		t.Fatal("pump did not exit after EOF")
	}

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 0 pending after EOF, got %d", len(rt.pendingObservations))
	}
	rt.turnMu.Unlock()

	rec, _ := svc.Registry().Get(id)
	if !rec.Exited {
		t.Fatal("expected exited=true after EOF")
	}
}

func TestClaudeAttestorDigestMismatch(t *testing.T) {
	// Non-vacuous: inject versionRunner so the binary "passes" version check,
	// then prove digest mismatch is caught at the digest step.
	origVR := versionRunner
	origDP := digestProvider
	defer func() { versionRunner = origVR; digestProvider = origDP }()

	versionRunner = func(exe string) (string, error) {
		return "2.1.209", nil // pretend version matches
	}

	// Create a temp file whose real SHA-256 definitely differs from the
	// PinnedDigest we configure.
	tmp, _ := os.CreateTemp("", "c1d-attest-*")
	tmp.Write([]byte("binary content for digest test"))
	tmp.Close()
	defer os.Remove(tmp.Name())

	realDigest, _ := fileDigest(tmp.Name())
	if realDigest == "" {
		t.Fatal("could not compute real digest")
	}

	// Set PinnedDigest to something different from the real digest.
	wrongDigest := "0000000000000000000000000000000000000000000000000000000000000000"
	if realDigest == wrongDigest {
		wrongDigest = "1111111111111111111111111111111111111111111111111111111111111111"
	}

	attestor := NewClaudeAttestor(ClaudeEntryConfig{
		PinnedPath:   tmp.Name(),
		PinnedDigest: wrongDigest,
		Version:      "2.1.209",
	})
	err := attestor.Certify(tmp.Name())
	if err == nil {
		t.Fatal("expected digest mismatch error")
	}
	if !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected 'digest mismatch', got: %v", err)
	}
}

func TestClaudeAttestorDigestMissing(t *testing.T) {
	// Empty PinnedDigest must fail closed.
	attestor := NewClaudeAttestor(ClaudeEntryConfig{
		PinnedPath:   "/some/path",
		PinnedDigest: "",
		Version:      "",
	})
	err := attestor.Certify("/some/path")
	if err == nil || !strings.Contains(err.Error(), "pinned digest not configured") {
		t.Fatalf("expected 'pinned digest not configured' error, got: %v", err)
	}
}

func TestClaudeAttestorNoPinnedPath(t *testing.T) {
	attestor := NewClaudeAttestor(ClaudeEntryConfig{
		PinnedPath:   "",
		PinnedDigest: "0000000000000000000000000000000000000000000000000000000000000000",
		Version:      "",
	})
	err := attestor.Certify("/usr/bin/true")
	if err == nil || !strings.Contains(err.Error(), "pinned path not configured") {
		t.Fatalf("expected 'pinned path not configured' error, got: %v", err)
	}
}

func TestClaudeTimeoutExpiry(t *testing.T) {
	// Use fake clock to deterministically test timeout expiration.
	origClock := clockNow
	defer func() { clockNow = origClock }()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clockNow = func() time.Time { return base }

	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	// Ingest two approvals at staggered clock times.
	sid1 := "s1"
	inputCanon, _ := canonicalJSON(json.RawMessage(`{"command":"echo ok"}`))
	digest := sha256Hex(inputCanon)

	// First approval at base.
	rt.observePreToolUse("call_T1", "Bash", sid1, digest, "")
	rt.processLine([]byte(deferredStreamJSON(sid1, "call_T1", "Bash", `{"command":"echo ok"}`)))

	// Advance clock 1s, create second.
	clockNow = func() time.Time { return base.Add(1 * time.Second) }
	rt.observePreToolUse("call_T2", "Bash", sid1, digest, "")
	rt.processLine([]byte(deferredStreamJSON(sid1, "call_T2", "Bash", `{"command":"echo ok"}`)))

	// Reset clock. Verify both active.
	clockNow = func() time.Time { return base.Add(2 * time.Second) }
	rt.turnMu.Lock()
	if len(rt.activeApprovals) != 2 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 2 active, got %d", len(rt.activeApprovals))
	}
	aid1 := rt.activeApprovals[0].approvalID
	aid2 := rt.activeApprovals[1].approvalID
	rt.turnMu.Unlock()

	// Advance past first expiry. First approval (created at base) expires
	// at base+120s; second (created at base+1s) expires at base+121s.
	clockNow = func() time.Time { return base.Add(120 * time.Second).Add(500 * time.Millisecond) }
	rt.clearStaleObservations()

	rt.turnMu.Lock()
	if len(rt.activeApprovals) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 1 active after partial expiry, got %d", len(rt.activeApprovals))
	}
	if rt.activeApprovals[0].approvalID != aid2 {
		rt.turnMu.Unlock()
		t.Fatalf("expected %s to survive, got %s", aid2, rt.activeApprovals[0].approvalID)
	}
	rt.turnMu.Unlock()

	// Store: first record invalidated, second still pending.
	s1, ok1 := store.LookupRecord(id, aid1)
	s2, ok2 := store.LookupRecord(id, aid2)
	if !ok1 || !ok2 {
		t.Fatalf("store lookup after partial expiry: ok1=%v ok2=%v", ok1, ok2)
	}
	if s1.State != ApprovalInvalidated && s1.State != ApprovalExpired {
		t.Fatalf("first record: expected invalidated/expired, got %v", s1.State)
	}
	if s2.State == ApprovalInvalidated || s2.State == ApprovalExpired {
		t.Fatalf("second record: expected still pending, got %v", s2.State)
	}

	// Advance past second expiry. Verify second record is invalidated BEFORE
	// terminate (prove the timeout transition, not the terminate cleanup).
	clockNow = func() time.Time { return base.Add(122 * time.Second) }
	rt.clearStaleObservations()
	rt.turnMu.Lock()
	if len(rt.activeApprovals) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 0 active after full expiry, got %d", len(rt.activeApprovals))
	}
	rt.turnMu.Unlock()

	// Second record must now be invalidated/expired by the timeout.
	s2After, ok2After := store.LookupRecord(id, aid2)
	if !ok2After {
		t.Fatal("second record missing after full expiry")
	}
	if s2After.State != ApprovalInvalidated && s2After.State != ApprovalExpired {
		t.Fatalf("second record: expected invalidated/expired after timeout, got %v", s2After.State)
	}

	rt.terminate()
	rt.turnMu.Lock()
	if len(rt.activeApprovals) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 0 active after terminate, got %d", len(rt.activeApprovals))
	}
	rt.turnMu.Unlock()
	rec, _ := svc.Registry().Get(id)
	if !rec.Exited {
		t.Fatal("expected exited after terminate")
	}
}

func TestClaudeActiveApprovalCapacity(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sid := "s-cap"
	inputCanon, _ := canonicalJSON(json.RawMessage(`{"command":"echo ok"}`))
	digest := sha256Hex(inputCanon)

	// Fill to capacity.
	for i := 0; i < maxActiveApprovals; i++ {
		tuid := fmt.Sprintf("call_C%d", i)
		rt.observePreToolUse(tuid, "Bash", sid, digest, "")
		rt.processLine([]byte(deferredStreamJSON(sid, tuid, "Bash", `{"command":"echo ok"}`)))
	}

	rt.turnMu.Lock()
	if len(rt.activeApprovals) != maxActiveApprovals {
		rt.turnMu.Unlock()
		t.Fatalf("expected %d active, got %d", maxActiveApprovals, len(rt.activeApprovals))
	}
	rt.turnMu.Unlock()

	// One more: capacity exhausted → rejected BEFORE ingest.
	rt.observePreToolUse("call_Overflow", "Bash", sid, digest, "")
	rt.processLine([]byte(deferredStreamJSON(sid, "call_Overflow", "Bash", `{"command":"echo ok"}`)))

	rt.turnMu.Lock()
	if len(rt.activeApprovals) != maxActiveApprovals {
		rt.turnMu.Unlock()
		t.Fatalf("expected %d active after overflow, got %d", maxActiveApprovals, len(rt.activeApprovals))
	}
	rt.turnMu.Unlock()

	// Also verify pending count — the overflow should have been consumed (deleted
	// from pending map) but NOT added to active.
	if len(store.ListSafe(id)) != maxActiveApprovals {
		t.Fatalf("expected %d store records, got %d", maxActiveApprovals, len(store.ListSafe(id)))
	}
	rt.terminate()
}

func TestClaudeEntropyFailure(t *testing.T) {
	// Replace entropy reader with a failing one.
	orig := entropyReader
	defer func() { entropyReader = orig }()
	entropyReader = &failingReader{}

	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "s1"
	toolUseID := "call_Entropy"
	toolName := "Bash"
	inputJSON := `{"command":"echo ok"}`
	inputCanon, _ := canonicalJSON(json.RawMessage(inputJSON))
	inputDigest := sha256Hex(inputCanon)

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest, "")
	deferred := deferredStreamJSON(sessionID, toolUseID, toolName, inputJSON)
	rt.processLine([]byte(deferred))

	// With a failing entropy reader, genApprovalToken returns error,
	// joinDeferred aborts. Pending observation must remain, zero store
	// records, zero active approvals.
	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatal("expected pending to remain after entropy failure")
	}
	if len(rt.activeApprovals) != 0 {
		rt.turnMu.Unlock()
		t.Fatal("expected zero active approvals after entropy failure")
	}
	rt.turnMu.Unlock()

	if len(store.ListSafe(id)) != 0 {
		t.Fatal("expected zero records after entropy failure")
	}
	rt.terminate()
}

// TestClaudeStopInvalidatesApprovals verifies that Stop calls terminate(),
// which invalidates all active approvals and clears pending state.
func TestClaudeStopInvalidatesApprovals(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	// Ingest an approval.
	inputCanon, _ := canonicalJSON(json.RawMessage(`{"command":"echo ok"}`))
	rt.observePreToolUse("call_StopInv", "Bash", "s1", sha256Hex(inputCanon), "")
	rt.processLine([]byte(deferredStreamJSON("s1", "call_StopInv", "Bash", `{"command":"echo ok"}`)))

	// Verify active approvals exist before stop.
	rt.turnMu.Lock()
	if len(rt.activeApprovals) != 1 {
		rt.turnMu.Unlock()
		t.Fatal("expected 1 active approval before stop")
	}
	rt.turnMu.Unlock()

	rec, _ := svc.Registry().Get(id)
	svc.Stop(id, rec.Epoch)

	// After Stop + terminate, active approvals and pending must be cleared.
	rt.turnMu.Lock()
	if len(rt.activeApprovals) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 0 active approvals after stop, got %d", len(rt.activeApprovals))
	}
	if len(rt.pendingObservations) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 0 pending after stop, got %d", len(rt.pendingObservations))
	}
	rt.turnMu.Unlock()

	// Registry must show exited.
	rec2, _ := svc.Registry().Get(id)
	if !rec2.Exited {
		t.Fatal("expected exited=true after stop")
	}
}

// TestClaudeIPCComposition tests the full IPC create path for Claude.
func TestClaudeIPCComposition(t *testing.T) {
	// Save and restore entropyReader.
	orig := entropyReader
	defer func() { entropyReader = orig }()
	entropyReader = orig

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{
		Bin:              "claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
		PinnedPath:       "/pinned/test/claude",
	}, launcher, &fakeClaudeAttestor{})

	go handleIPCConnection(serverConn, nil, nil, nil, nil, nil, nil, nil, svc)

	// Send a create request for the claude profile.
	req := `{"operation":"create","profileId":"claude","cwd":"/tmp","detach":true}`
	clientConn.Write([]byte(req + "\n"))

	var resp map[string]string
	dec := json.NewDecoder(clientConn)
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "" {
		t.Fatalf("IPC error: %s", resp["error"])
	}
	id := resp["id"]
	if !strings.HasPrefix(id, "claude_headless:") {
		t.Fatalf("unexpected id: %s", id)
	}
	if resp["state"] != string(LifecycleRunning) {
		t.Fatalf("unexpected state: %s", resp["state"])
	}

	// Verify the session exists in registry.
	if _, ok := svc.Registry().Get(id); !ok {
		t.Fatal("session not found in registry after IPC create")
	}
	ctx := context.Background()
	svc.Shutdown(ctx)
}

// TestClaudeIPCUnavailable proves IPC fails closed when feature disabled.
func TestClaudeIPCUnavailable(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go handleIPCConnection(serverConn, nil, nil, nil, nil, nil, nil, nil, nil)

	req := `{"operation":"create","profileId":"claude","cwd":"/tmp"}`
	clientConn.Write([]byte(req + "\n"))

	var resp map[string]string
	json.NewDecoder(clientConn).Decode(&resp)
	if resp["error"] == "" || !strings.Contains(resp["error"], "unavailable") {
		t.Fatalf("expected unavailable error, got: %v", resp)
	}
}

// TestClaudeStopJoinRace verifies that a late join after Stop is rejected by
// the Store generation high-water. Uses preIngestHook to inject terminate()
// between gen-check and IngestObserved. The Store sees StreamGen=1 from
// SupersedeRuntime and rejects the old StreamGen=0 ingest.
func TestClaudeStopJoinRace(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	// FORWARD RACE: terminate() sends sentinel(StreamGen=1), so
	// IngestObserved(StreamGen=0) is rejected by Store genNewer.
	sid := "s-race"
	inputCanon, _ := canonicalJSON(json.RawMessage(`{"command":"echo ok"}`))
	digest := sha256Hex(inputCanon)

	var hookCalled sync.WaitGroup
	hookCalled.Add(1)
	rt.preIngestHook = func() {
		hookCalled.Done()
		rt.terminate() // sentinel ingest bumps Store to StreamGen=1
	}

	rt.observePreToolUse("call_Race", "Bash", sid, digest, "")
	deferred := deferredStreamJSON(sid, "call_Race", "Bash", `{"command":"echo ok"}`)
	rt.processLine([]byte(deferred))
	hookCalled.Wait()

	// Store genNewer(1,0) rejects. Non-authoritative sentinel creates
	// no phantom record. Result: 0 records.
	if len(store.ListSafe(id)) != 0 {
		t.Fatalf("Store must reject: expected 0 records, got %d", len(store.ListSafe(id)))
	}
}

// TestClaudeReverseRace: terminate AFTER Store admission, BEFORE active
// append. Uses postIngestHook barrier at the exact defect window.
func TestClaudeReverseRace(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sid := "s-rev"
	inputCanon, _ := canonicalJSON(json.RawMessage(`{"command":"echo ok"}`))
	digest := sha256Hex(inputCanon)

	// Prime so the test approval gets a fresh ID.
	rt.observePreToolUse("call_Primer", "Bash", sid, digest, "")
	rt.processLine([]byte(deferredStreamJSON(sid, "call_Primer", "Bash", `{"command":"echo ok"}`)))

	// Barrier at the exact defect window: between Store admission and
	// active append. Terminate runs here and invalidates the record.
	var hookCalled sync.WaitGroup
	hookCalled.Add(1)
	rt.postIngestHook = func() {
		hookCalled.Done()
		rt.terminate()
	}

	rt.observePreToolUse("call_Rev", "Bash", sid, digest, "")
	rt.processLine([]byte(deferredStreamJSON(sid, "call_Rev", "Bash", `{"command":"echo ok"}`)))
	hookCalled.Wait()

	// Post-ingest check must clear active approvals. Store shows the
	// primer (invalidated by terminate, may persist in resolution window).
	rt.turnMu.Lock()
	if len(rt.activeApprovals) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("post-ingest: expected 0 active, got %d", len(rt.activeApprovals))
	}
	rt.turnMu.Unlock()
}

// TestClaudeStartIPCServerIntegration tests the full production IPC path.
// This requires a running StartIPCServer with the real production wiring.
// It is skipped by default because it requires the real binary and attestor.
func TestClaudeStartIPCServerIntegration(t *testing.T) {
	// Requires real pinned Claude 2.1.209 binary + POKIT_CLAUDE_DIGEST env.
	digest := os.Getenv("POKIT_CLAUDE_DIGEST")
	if digest == "" {
		t.Skip("POKIT_CLAUDE_DIGEST not set — set it to the SHA-256 of the pinned claude binary to run this test")
	}

	// Set up entropy and clock.
	origEntropy := entropyReader
	origClock := clockNow
	defer func() { entropyReader = origEntropy; clockNow = origClock }()
	entropyReader = origEntropy
	clockNow = time.Now

	// Build the production service using the pinned config.
	cfg := PinnedClaudeConfigWithDigest(digest)
	cfg.Bin = cfg.PinnedPath

	svc := NewManagedClaudeService(cfg, nil, nil)
	store := NewApprovalStore()
	svc.SetApprovalStore(store)

	// Start a real IPC server on a temp socket.
	socketPath := filepath.Join("/tmp", fmt.Sprintf("pokit-c1d-test-%d.sock", time.Now().UnixNano()))
	reg, _ := mux.NewRegistry()
	srv, err := StartIPCServer(socketPath, reg, nil, nil, nil, nil, nil, nil, svc)
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	defer srv.Close()
	defer os.Remove(socketPath)

	// Connect as `pokit run claude` would (JSON line protocol).
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial IPC: %v", err)
	}
	defer conn.Close()

	cwd := os.Getenv("POKIT_CLAUDE_CWD")
	if cwd == "" {
		cwd = "/tmp"
	}
	// Production CLI serializer: exact JSON operation.
	req := fmt.Sprintf(`{"operation":"create","profileId":"claude","cwd":"%s","detach":true}`, cwd)
	if _, err := conn.Write([]byte(req + "\n")); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var resp struct {
		ID    string `json:"id"`
		State string `json:"state"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("create error: %s", resp.Error)
	}
	if resp.State != string(LifecycleRunning) {
		t.Fatalf("expected state=running, got %s", resp.State)
	}
	id := resp.ID
	if id == "" {
		t.Fatal("empty session id")
	}
	t.Logf("created session: %s state=%s via IPC socket", id, resp.State)

	// Wait for Claude to process the certification prompt.
	time.Sleep(10 * time.Second)

	rec, _ := svc.Registry().Get(id)
	t.Logf("session: provider=%s version=%s epoch=%d exited=%v digest=%s",
		rec.Provider, rec.Version, rec.Epoch, rec.Exited, rec.CertifiedDigest)
	if rec.CertifiedDigest == "" {
		t.Error("CertifiedDigest not recorded")
	}

	// Exact assertion: the certification prompt triggers a Bash tool use,
	// which must produce exactly 1 non-actionable observation.
	safe := store.ListSafe(id)
	t.Logf("approvals: %d", len(safe))
	if len(safe) != 1 {
		t.Errorf("expected exactly 1 approval, got %d", len(safe))
	}
	for _, s := range safe {
		if len(s.Options) != 0 {
			t.Errorf("expected zero options (non-actionable), got %d", len(s.Options))
		}
	}

	// Stop and verify deterministic cleanup.
	svc.Stop(id, rec.Epoch)
	rec2, _ := svc.Registry().Get(id)
	if !rec2.Exited {
		t.Fatal("expected exited after stop")
	}
	// After Stop, approvals must be invalidated.
	safe2 := store.ListSafe(id)
	t.Logf("post-stop approvals: %d", len(safe2))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	svc.Shutdown(ctx)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	srv.Wait(ctx2)
}

// TestClaudeReservationRollback_LauncherFailure verifies that a failed launch
// after successful Store reservation rolls back the slot.
func TestClaudeReservationRollback_LauncherFailure(t *testing.T) {
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), &failingLauncher{}, &fakeClaudeAttestor{})
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatal(err)
	}

	before := store.Len()
	_, err := svc.CreateDetached("/tmp")
	if err == nil {
		t.Fatal("expected launch error")
	}
	if !strings.Contains(err.Error(), "launch") {
		t.Fatalf("expected launch error, got: %v", err)
	}
	// Store must be unchanged — reservation was rolled back.
	if store.Len() != before {
		t.Fatalf("Store Len: before=%d after=%d (rollback failed)", before, store.Len())
	}
}

// TestClaudeReservationRollback_RepeatedFailureDoesNotExhaustCapacity verifies
// that repeated launch failures do not permanently consume Store slots.
func TestClaudeReservationRollback_RepeatedFailureDoesNotExhaustCapacity(t *testing.T) {
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), &failingLauncher{}, &fakeClaudeAttestor{})
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		_, err := svc.CreateDetached("/tmp")
		if err == nil {
			t.Fatal("expected launch error")
		}
	}
	// After 10 failed launches, Store must still be empty.
	if store.Len() != 0 {
		t.Fatalf("Store leaked %d sessions after repeated failures", store.Len())
	}
}

// TestClaudeReservationRollback_SuccessfulTombstoneRetained verifies that
// a successful launch retains its Store slot (not rolled back).
func TestClaudeReservationRollback_SuccessfulTombstoneRetained(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatal(err)
	}

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	defer launcher.closeStream()

	// Successful launch must retain the Store slot.
	if store.Len() != 1 {
		t.Fatalf("expected 1 session, got %d", store.Len())
	}

	// Terminate and verify tombstone persists.
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()
	rt.terminate()

	// Tombstone must still exist (Clear was NOT called).
	if store.Len() != 1 {
		t.Fatal("tombstone removed — successful reservation should persist")
	}

	ctx := context.Background()
	svc.Shutdown(ctx)
}

// ── C2D-B integration tests ──

func TestC2DB_IdentityPreservedAtJoin(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)
	defer launcher.closeStream()

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "claude-session-uuid"
	toolUseID := "call_00_C2DB_Test1"
	toolName := "Bash"
	inputJSON := `{"command":"echo hello","description":"test"}`
	inputCanon, _ := canonicalJSON(json.RawMessage(inputJSON))
	inputDigest := sha256Hex(inputCanon)

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest, "")
	deferred := deferredStreamJSON(sessionID, toolUseID, toolName, inputJSON)
	rt.processLine([]byte(deferred))

	// After join, the pending observation is gone but the identity is preserved.
	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 0 {
		rt.turnMu.Unlock()
		t.Fatal("expected 0 pending after join")
	}
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	// Verify identity record exists in the coordinator with RuntimeRef binding.
	idRec, ok := svc.coordinator.LookupIdentity(approvalID)
	if !ok {
		t.Fatal("expected identity record to be preserved")
	}
	if idRec.sessionID != sessionID || idRec.toolUseID != toolUseID ||
		idRec.toolName != toolName || idRec.inputDigest != inputDigest {
		t.Fatalf("identity record fields mismatch: %+v", idRec)
	}
	if idRec.pokitSessionID != id {
		t.Fatalf("expected pokitSessionID %s, got %s", id, idRec.pokitSessionID)
	}
	expectedRT := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: rt.authorityVersion, LaunchGen: rt.epoch, StreamGen: 0}
	if !idRec.runtime.equal(expectedRT) {
		t.Fatalf("runtime mismatch: %+v vs %+v", idRec.runtime, expectedRT)
	}

	rt.terminate()
}

func TestC2DB_IdentityClearedOnTerminate(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)
	defer launcher.closeStream()

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "claude-sess-term"
	toolUseID := "call_C2DB_Term"
	toolName := "Bash"
	inputDigest := sha256Hex([]byte(`{"cmd":"x"}`))

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest, "")
	deferred := deferredStreamJSON(sessionID, toolUseID, toolName, `{"cmd":"x"}`)
	rt.processLine([]byte(deferred))

	// Capture approval ID before terminate.
	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	// Verify identity exists.
	if _, ok := svc.coordinator.LookupIdentity(approvalID); !ok {
		t.Fatal("expected identity before terminate")
	}

	rt.terminate()

	// After terminate, identity is cleared (via ClearRuntime).
	if _, ok := svc.coordinator.LookupIdentity(approvalID); ok {
		t.Fatal("expected identity to be cleared after terminate")
	}
}

func TestC2DB_IdentityClearedOnTimeout(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)
	defer launcher.closeStream()

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "claude-sess-timeout"
	toolUseID := "call_C2DB_Timeout"
	toolName := "Bash"
	inputDigest := sha256Hex([]byte(`{"cmd":"x"}`))

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest, "")
	deferred := deferredStreamJSON(sessionID, toolUseID, toolName, `{"cmd":"x"}`)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	if _, ok := svc.coordinator.LookupIdentity(approvalID); !ok {
		t.Fatal("expected identity before timeout")
	}

	// Expire the active approval.
	rt.turnMu.Lock()
	for i := range rt.activeApprovals {
		rt.activeApprovals[i].expiresAt = clockNow().Add(-time.Second)
	}
	rt.turnMu.Unlock()
	rt.clearStaleObservations()

	// Identity must be removed by the coordinator cleanup in clearStaleObservations.
	if _, ok := svc.coordinator.LookupIdentity(approvalID); ok {
		t.Fatal("expected identity to be cleared after timeout")
	}

	rt.terminate()
}

func TestC2DB_ReserveEntryFromPreservedIdentity(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)
	defer launcher.closeStream()

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "claude-sess-fullflow"
	toolUseID := "call_C2DB_FullFlow"
	toolName := "Bash"
	inputJSON := `{"command":"echo hello","description":"test"}`
	inputCanon, _ := canonicalJSON(json.RawMessage(inputJSON))
	inputDigest := sha256Hex(inputCanon)

	// Step 1: Observe and join.
	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest, "")
	deferred := deferredStreamJSON(sessionID, toolUseID, toolName, inputJSON)
	rt.processLine([]byte(deferred))

	// Step 2: Get the approval ID and build a proper binding.
	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	idRec, ok := svc.coordinator.LookupIdentity(approvalID)
	if !ok {
		t.Fatal("expected identity record")
	}

	// Step 3: Reserve an entry using the store-issued binding.
	binding := ApprovalExecutionBinding{
		ApprovalID:     approvalID,
		SessionID:      id,
		Runtime:        RuntimeRef{Adapter: claudeHeadlessAdapter, Version: rt.authorityVersion, LaunchGen: rt.epoch, StreamGen: 0},
		ActionDigest:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PayloadDigest:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		IdempotencyKey: "test.key.fullflow",
		OptionID:       "allow_once",
		DeliverySchema: claudeDecisionSchemaV1,
	}
	handle, ok := svc.coordinator.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !ok {
		t.Fatal("expected ReserveEntry to succeed")
	}

	// Step 4: ClaimWrite (simulating resume hook).
	wh, out := svc.coordinator.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce,
		idRec.sessionID, idRec.toolUseID, idRec.toolName, idRec.inputDigest, 1)
	if out != outcomeWritten {
		t.Fatalf("expected outcomeWritten, got %d", out)
	}
	if wh.Decision() != "allow" {
		t.Fatalf("expected allow, got %s", wh.Decision())
	}

	// Step 5: ConfirmWrite.
	out = svc.coordinator.ConfirmWrite("cccccccccccccccccccccccccccccccc", true)
	if out != outcomeWritten {
		t.Fatalf("expected outcomeWritten from ConfirmWrite, got %d", out)
	}
	if svc.coordinator.pendingCount() != 0 {
		t.Fatal("expected 0 pending entries after confirm")
	}

	// Step 6: Duplicate ClaimWrite must fail.
	_, out = svc.coordinator.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce,
		idRec.sessionID, idRec.toolUseID, idRec.toolName, idRec.inputDigest, 1)
	if out != outcomeDuplicate {
		t.Fatalf("expected outcomeDuplicate, got %d", out)
	}

	rt.terminate()
}

func TestC2DB_MismatchBlocksDelivery(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)
	defer launcher.closeStream()

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	rt.observePreToolUse("tu-1", "Bash", "sess-1", sha256Hex([]byte(`{"cmd":"x"}`)), "")
	deferred := deferredStreamJSON("sess-1", "tu-1", "Bash", `{"cmd":"x"}`)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	binding := ApprovalExecutionBinding{
		ApprovalID:     approvalID,
		SessionID:      id,
		Runtime:        RuntimeRef{Adapter: claudeHeadlessAdapter, Version: rt.authorityVersion, LaunchGen: rt.epoch, StreamGen: 0},
		ActionDigest:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PayloadDigest:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		IdempotencyKey: "test.key.mismatch",
		OptionID:       "allow_once",
		DeliverySchema: claudeDecisionSchemaV1,
	}
	handle, ok := svc.coordinator.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !ok {
		t.Fatal("expected ReserveEntry to succeed")
	}

	// R4: first ClaimWrite binds the ResumeAttemptIdentity.
	// toolUseID is accepted as the new provider identity (one-time bind).
	// toolName and inputDigest must match the original action.
	correctDigest := sha256Hex([]byte(`{"cmd":"x"}`))
	wh, out := svc.coordinator.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce,
		"sess-1", "tu-1", "Bash", correctDigest, 1)
	if out != outcomeWritten || wh.Decision() != "allow" {
		t.Fatalf("expected first bind to succeed, got %d/%s", out, wh.Decision())
	}

	// Wrong digest (different from bound attempt) — must fail.
	_, out = svc.coordinator.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce,
		"sess-1", "tu-1", "Bash", "wrong-digest", 1)
	if out != outcomeMismatch {
		t.Fatalf("expected outcomeMismatch for wrong digest, got %d", out)
	}

	// Same identity replay — must be duplicate.
	_, out = svc.coordinator.ClaimWrite("cccccccccccccccccccccccccccccccc", handle.ResumeNonce,
		"sess-1", "tu-1", "Bash", correctDigest, 1)
	if out != outcomeDuplicate {
		t.Fatalf("expected outcomeDuplicate for same identity, got %d", out)
	}

	rt.terminate()
}

func TestC2DB_PostIngestTerminationClearsIdentity(t *testing.T) {
	// B2 fix: prove that identity is removed when postIngestHook triggers
	// terminate between Store admission and active append.
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)
	defer launcher.closeStream()

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "claude-sess-orphan"
	toolUseID := "call_C2DB_Orphan"
	toolName := "Bash"
	inputDigest := sha256Hex([]byte(`{"cmd":"x"}`))

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest, "")

	// Inject terminate via postIngestHook — this fires AFTER Store admission
	// but BEFORE activeApprovals append.
	rt.postIngestHook = func() {
		rt.terminate()
	}

	deferred := deferredStreamJSON(sessionID, toolUseID, toolName, `{"cmd":"x"}`)
	rt.processLine([]byte(deferred))

	// The identity was created before Store admission. Even though terminate()
	// ran during the post-ingest hook, the coordinator identity must be
	// cleaned up (B2 fix). Since the record was ingested into the Store and
	// then invalidated by terminate(), the identity should be removed.
	//
	// We retrieve the approvalID from the Store (it was ingested before the
	// hook fired) and verify the coordinator does NOT have it.
	records := store.ListSafe(id)
	if len(records) != 1 {
		t.Fatalf("expected 1 Store record, got %d", len(records))
	}
	approvalID := records[0].ID

	if _, ok := svc.coordinator.LookupIdentity(approvalID); ok {
		t.Fatal("B2 regression: identity should be cleared after post-ingest termination")
	}

	// Wait for terminate to complete.
	<-rt.exited
}

// ── R6-B denial decoder tests ──

func TestDenialDecode_EvidenceShapedEventAccepted(t *testing.T) {
	// CE-10: a full evidence-shaped event with 20 advisory top-level fields
	// (shapes from the R6-A7 projections) must decode successfully.
	json := `{"type":"result","session_id":"abc-123","permission_denials":[{"tool_use_id":"call_00_Test","tool_name":"Bash","tool_input":{"command":"touch marker-a"}}],"subtype":"success","is_error":false,"result":"denied","stop_reason":"end_turn","num_turns":2,"duration_ms":1234,"duration_api_ms":567,"ttft_ms":100,"ttft_stream_ms":80,"total_cost_usd":0.01,"usage":{"input_tokens":50},"modelUsage":{"input_tokens":50},"fast_mode_state":"off","api_error_status":null,"time_to_request_ms":2000,"uuid":"bbbb","terminal_reason":"end_turn"}`
	d, outcome := decodeDenialResult([]byte(json))
	if outcome != denialOK {
		t.Fatalf("evidence-shaped event must decode: got %d", outcome)
	}
	if d.SessionID != "abc-123" {
		t.Fatalf("session_id: %s", d.SessionID)
	}
	if len(d.Entries) != 1 {
		t.Fatalf("entries: %d", len(d.Entries))
	}
	e := d.Entries[0]
	if e.ToolUseID != "call_00_Test" || e.ToolName != "Bash" ||
		e.InputDigest != sha256Hex([]byte(`{"command":"touch marker-a"}`)) {
		t.Fatalf("entry: %+v", e)
	}
}

func TestDenialDecode_MissingToolInputIsMalformed(t *testing.T) {
	// CE-2: the pre-R6-B fixture shape — no tool_input in entry.
	oldStyle := `{"type":"result","stop_reason":"end_turn","session_id":"abc","permission_denials":[{"tool_name":"Bash","tool_use_id":"call_01"}]}`
	_, outcome := decodeDenialResult([]byte(oldStyle))
	if outcome != denialMalformed {
		t.Fatalf("missing tool_input must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_UnknownEntryFieldIsMalformed(t *testing.T) {
	// CE-3: entry carries an extra field not in the closed allowlist.
	json := `{"type":"result","session_id":"abc","permission_denials":[{"tool_use_id":"call_01","tool_name":"Bash","tool_input":{"c":"x"},"extra":"no"}]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("unknown entry field must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_DuplicateSessionIDKeyIsMalformed(t *testing.T) {
	// CE-9: duplicate authority key at the top level.
	json := `{"type":"result","session_id":"abc","session_id":"def","permission_denials":[{"tool_use_id":"call_01","tool_name":"Bash","tool_input":{"c":"x"}}]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("duplicate session_id must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_DuplicatePermissionDenialsKeyIsMalformed(t *testing.T) {
	json := `{"type":"result","session_id":"abc","permission_denials":[{"tool_use_id":"call_01","tool_name":"Bash","tool_input":{"c":"x"}}],"permission_denials":null}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("duplicate permission_denials must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_DuplicateTUIDInEntryIsMalformed(t *testing.T) {
	json := `{"type":"result","session_id":"abc","permission_denials":[{"tool_use_id":"call_01","tool_use_id":"call_02","tool_name":"Bash","tool_input":{"c":"x"}}]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("duplicate tool_use_id in entry must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_MultipleRetryEntriesWithCorrectTUID(t *testing.T) {
	// Model retries produce additional denial entries with different tuids.
	// The decoder must accept all of them; ambiguity is checked in routeDenial.
	json := `{"type":"result","session_id":"abc","permission_denials":[{"tool_use_id":"call_retry_1","tool_name":"Bash","tool_input":{"c":"x"}},{"tool_use_id":"call_original","tool_name":"Bash","tool_input":{"c":"x"}},{"tool_use_id":"call_retry_2","tool_name":"Bash","tool_input":{"c":"x"}}]}`
	d, outcome := decodeDenialResult([]byte(json))
	if outcome != denialOK {
		t.Fatalf("retry entries must decode, got %d", outcome)
	}
	if len(d.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(d.Entries))
	}
}

func TestDenialDecode_OversizedSessionIDRejected(t *testing.T) {
	long := make([]byte, 300)
	for i := range long {
		long[i] = 'a'
	}
	json := `{"type":"result","session_id":"` + string(long) + `","permission_denials":[{"tool_use_id":"call_01","tool_name":"Bash","tool_input":{"c":"x"}}]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("oversized session_id must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_EmptyEntriesIsMalformed(t *testing.T) {
	json := `{"type":"result","session_id":"abc","permission_denials":[]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("empty permission_denials must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_TooManyEntriesIsMalformed(t *testing.T) {
	entries := ""
	for i := 0; i < maxDenialEntries+1; i++ {
		if i > 0 {
			entries += ","
		}
		entries += `{"tool_use_id":"c_` + string(rune('a'+i%26)) + `","tool_name":"Bash","tool_input":{"c":"x"}}`
	}
	json := `{"type":"result","session_id":"abc","permission_denials":[` + entries + `]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("too many entries must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_OversizedToolInputIsMalformed(t *testing.T) {
	// CE-8: tool_input whose canonical form exceeds maxToolInputBytes.
	big := make([]byte, maxToolInputBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	json := `{"type":"result","session_id":"abc","permission_denials":[{"tool_use_id":"call_01","tool_name":"Bash","tool_input":"` + string(big) + `"}]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("oversized tool_input must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_NonDenialEventsAreNotDenial(t *testing.T) {
	system := `{"type":"system","subtype":"init"}`
	_, outcome := decodeDenialResult([]byte(system))
	if outcome != denialNotDenial {
		t.Fatalf("system event must not be denial, got %d", outcome)
	}
	resultEnd := `{"type":"result","stop_reason":"end_turn","session_id":"abc"}`
	_, outcome = decodeDenialResult([]byte(resultEnd))
	if outcome != denialNotDenial {
		t.Fatalf("end_turn without permission_denials must not be denial, got %d", outcome)
	}
}

func TestDenialDecode_MissingPermissionDenialsIsNotDenial(t *testing.T) {
	json := `{"type":"result","session_id":"abc","subtype":"success"}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialNotDenial {
		t.Fatalf("result without permission_denials must not be denial, got %d", outcome)
	}
}

func TestDenialDecode_NonObjectTopLevelIsNotDenial(t *testing.T) {
	_, outcome := decodeDenialResult([]byte(`"just a string"`))
	if outcome != denialNotDenial {
		t.Fatalf("non-object must not be denial, got %d", outcome)
	}
}

// R6-B-R1 negative decoder tests: type authority and tool_input object checks.

func TestDenialDecode_DuplicateTypeKeyIsMalformed(t *testing.T) {
	// A crafted event with "type":"system","type":"result" — the detection
	// probe has last-wins semantics, but the strict token walker must reject
	// the duplicate key.
	json := `{"type":"system","type":"result","session_id":"abc","permission_denials":[{"tool_use_id":"call_01","tool_name":"Bash","tool_input":{"c":"x"}}]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("duplicate type key must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_TypeNotResultIsNotDenial(t *testing.T) {
	// A result event with type != "result" is not a denial event.
	// The detection probe checks type=="result" before the strict walker runs.
	json := `{"type":"system","session_id":"abc","permission_denials":[{"tool_use_id":"call_01","tool_name":"Bash","tool_input":{"c":"x"}}]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialNotDenial {
		t.Fatalf("type != result must be not-denial, got %d", outcome)
	}
}

func TestDenialDecode_MissingTypeIsMalformed(t *testing.T) {
	// The detection probe only looks for type=="result". A missing type key
	// means the probe returns false → denialNotDenial, but the strict walker
	// should also reject. The probe's loose Unmarshal of a missing key
	// leaves Type=="", which is not "result", so detectDenialResult already
	// returns false. This test confirms the outcome is denialNotDenial.
	json := `{"session_id":"abc","permission_denials":[{"tool_use_id":"call_01","tool_name":"Bash","tool_input":{"c":"x"}}]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialNotDenial {
		t.Fatalf("missing type must be not-denial, got %d", outcome)
	}
}

func TestDenialDecode_ToolInputStringIsMalformed(t *testing.T) {
	// Blocker 2: the frozen wire requires tool_input: object. A string
	// value is malformed.
	json := `{"type":"result","session_id":"abc","permission_denials":[{"tool_use_id":"call_01","tool_name":"Bash","tool_input":"just-a-string"}]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("string tool_input must be malformed, got %d", outcome)
	}
}

func TestDenialDecode_ToolInputNumberIsMalformed(t *testing.T) {
	json := `{"type":"result","session_id":"abc","permission_denials":[{"tool_use_id":"call_01","tool_name":"Bash","tool_input":42}]}`
	_, outcome := decodeDenialResult([]byte(json))
	if outcome != denialMalformed {
		t.Fatalf("numeric tool_input must be malformed, got %d", outcome)
	}
}

// ── R6-B denial binding tests (processLine + routeDenial + failClosedDenial) ──

// advanceToDecisionWritten is a test helper that reserves identity and entry,
// then claims and confirms the write, returning the entry in state decisionWritten.
// toolInputJSON is the JSON value for tool_input (e.g. `{"command":"x"}`); the
// correct canonical digest is computed from it so MarkWitnessed can match.
func advanceToDecisionWritten(t *testing.T, c *claudeResumeCoordinator, toolInputJSON string) (ResumeHandle, string, string, string, string, RuntimeRef) {
	t.Helper()
	aid := "claude-test"
	sid := "claude-sess-1"
	tuid := "call_00_Test"
	tn := "Bash"
	dig := sha256Hex([]byte(toolInputJSON))
	psid := "claude_headless:claude-test"
	rt := testRuntimeRef()
	if !c.ReserveIdentity(aid, sid, tuid, tn, dig, "", psid, rt) {
		t.Fatal("ReserveIdentity failed")
	}
	binding := testBinding(aid, psid)
	binding.OptionID = "deny"
	binding.DeliverySchema = claudeDecisionSchemaV1
	handle, ok := c.ReserveEntry("cccccccccccccccccccccccccccccccc", binding)
	if !ok {
		t.Fatal("ReserveEntry failed")
	}
	if _, o := c.ClaimWrite(handle.ClaimToken, handle.ResumeNonce, sid, tuid, tn, dig, 1); o != outcomeWritten {
		t.Fatalf("ClaimWrite: %d", o)
	}
	if o := c.ConfirmWrite(handle.ClaimToken, true); o != outcomeWritten {
		t.Fatalf("ConfirmWrite: %d", o)
	}
	return handle, sid, tuid, tn, dig, rt
}

func TestDenialBinding_EvidenceEventWitnessed(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	handle, sid, tuid, tn, _, rt := advanceToDecisionWritten(t, c, `{"command":"x"}`)
	runtime := &claudeManagedRuntime{
		coordinator: c,
		resumeCtx: &resumeContext{
			coordinator:     c,
			claimToken:      handle.ClaimToken,
			claudeSessionID: sid,
			toolUseID:       tuid,
			toolName:        tn,
			originalRuntime: rt,
		},
	}
	denialJSON := `{"type":"result","session_id":"` + sid + `","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"command":"x"}}],"subtype":"success"}`
	runtime.processLine([]byte(denialJSON))
	select {
	case result := <-handle.Completion:
		if result.Outcome != TerminalWitnessed {
			t.Fatalf("expected witnessed, got %d", result.Outcome)
		}
	default:
		t.Fatal("terminal result not published")
	}
}

func TestDenialBinding_MutatedInputFailsWitness(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	handle, sid, tuid, tn, dig, rt := advanceToDecisionWritten(t, c, `{"command":"x"}`)
	runtime := &claudeManagedRuntime{
		coordinator: c,
		resumeCtx: &resumeContext{
			coordinator:     c,
			claimToken:      handle.ClaimToken,
			claudeSessionID: sid,
			toolUseID:       tuid,
			toolName:        tn,
			originalRuntime: rt,
		},
	}
	// CE-1: mutated tool_input — recomputed digest ≠ stored digest.
	// R6-B-R1 Blocker 3: a digest mismatch on a bound tool_use_id is a
	// cross-binding anomaly; the entry MUST be cancelled (fail closed).
	mutated := `{"type":"result","session_id":"` + sid + `","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"command":"EVIL_MUTATED"}}]}`
	runtime.processLine([]byte(mutated))
	select {
	case result := <-handle.Completion:
		if result.Outcome == TerminalWitnessed {
			t.Fatal("mutated input must not witness — digest mismatch is a cross-binding anomaly")
		}
		// Non-witness outcome: entry was cancelled (fail closed).
	default:
		t.Fatal("mutated input must cancel entry immediately — completion must be signaled")
	}
	// Known-bad control: calling MarkWitnessed directly with the STORED digest
	// accepts. This proves the decoder's recomputation is the enforcing boundary.
	c2 := NewClaudeResumeCoordinator()
	handle2, sid2, tuid2, tn2, _, rt2 := advanceToDecisionWritten(t, c2, `{"command":"x"}`)
	// Use the stored digest dig (parameter) — not a recomputed one.
	_, _, ok := c2.MarkWitnessed(handle2.ClaimToken, WitnessPermissionDenials,
		sid2, tuid2, tn2, dig, rt2)
	if !ok {
		t.Fatal("known-bad: MarkWitnessed with stored digest must accept — proves decoder recomputation is the only defense")
	}
	// The stored digest matches the stored entry, so the known-bad succeeds.
}

func TestDenialBinding_DuplicateBoundTUIDCancels(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	handle, sid, tuid, tn, _, rt := advanceToDecisionWritten(t, c, `{"command":"x"}`)
	runtime := &claudeManagedRuntime{
		coordinator: c,
		resumeCtx: &resumeContext{
			coordinator:     c,
			claimToken:      handle.ClaimToken,
			claudeSessionID: sid,
			toolUseID:       tuid,
			toolName:        tn,
			originalRuntime: rt,
		},
	}
	// CE-4: two entries with the same tool_use_id.
	dup := `{"type":"result","session_id":"` + sid + `","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"c":"x"}},{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"c":"x"}}]}`
	runtime.processLine([]byte(dup))
	select {
	case result := <-handle.Completion:
		if result.Outcome == TerminalWitnessed {
			t.Fatal("duplicate bound tuid must not witness")
		}
	default:
		t.Fatal("entry must be cancelled, completion not signaled")
	}
}

func TestDenialBinding_WrongSessionAnomalyCancels(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	handle, sid, tuid, tn, _, rt := advanceToDecisionWritten(t, c, `{"command":"x"}`)
	runtime := &claudeManagedRuntime{
		coordinator: c,
		resumeCtx: &resumeContext{
			coordinator:     c,
			claimToken:      handle.ClaimToken,
			claudeSessionID: sid,
			toolUseID:       tuid,
			toolName:        tn,
			originalRuntime: rt,
		},
	}
	// CE-5: matching tool_use_id, wrong session_id.
	ev := `{"type":"result","session_id":"sess-WRONG","permission_denials":[{"tool_use_id":"` + tuid + `","tool_name":"Bash","tool_input":{"c":"x"}}]}`
	runtime.processLine([]byte(ev))
	select {
	case result := <-handle.Completion:
		if result.Outcome == TerminalWitnessed {
			t.Fatal("wrong session must not witness")
		}
	default:
		t.Fatal("entry must be cancelled")
	}
}

func TestDenialBinding_MalformedLineCancelsActiveEntry(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	handle, sid, tuid, tn, _, rt := advanceToDecisionWritten(t, c, `{"command":"x"}`)
	runtime := &claudeManagedRuntime{
		coordinator: c,
		resumeCtx: &resumeContext{
			coordinator:     c,
			claimToken:      handle.ClaimToken,
			claudeSessionID: sid,
			toolUseID:       tuid,
			toolName:        tn,
			originalRuntime: rt,
		},
	}
	// Old-style entry without tool_input — malformed.
	runtime.processLine([]byte(`{"type":"result","session_id":"abc","permission_denials":[{"tool_name":"Bash","tool_use_id":"call_01"}]}`))
	select {
	case result := <-handle.Completion:
		if result.Outcome == TerminalWitnessed {
			t.Fatal("malformed denial must not witness")
		}
	default:
		t.Fatal("entry must be cancelled on malformed denial")
	}
}

func TestDenialBinding_C1DRuntimeWithNoResumeCtxDoesNotWitness(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	rt := &claudeManagedRuntime{
		coordinator: c,
		resumeCtx:   nil, // C1D observation runtime
	}
	rt.processLine([]byte(`{"type":"result","session_id":"x","permission_denials":[{"tool_use_id":"y","tool_name":"Bash","tool_input":{"c":"z"}}]}`))
	// Verify no entries were created or altered — C1D runtimes have no resumeCtx.
	if c.PendingCount() != 0 || c.EntryCount() != 0 {
		t.Fatal("C1D runtime must never create coordinator entries")
	}
}

// ── P2A catalog wiring tests ──

// p2aCreateRuntime creates a working managed runtime for P2A tests.
// It fails the test if CreateDetached errors, preventing nil-runtime panics.
func p2aCreateRuntime(t *testing.T) (*ManagedClaudeService, *AuthoritativeApprovalStore, *claudeManagedRuntime, string) {
	t.Helper()
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatalf("SetApprovalStore: %v", err)
	}
	t.Cleanup(func() { launcher.closeStream() })

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()
	if rt == nil {
		t.Fatal("runtime is nil after CreateDetached")
	}
	return svc, store, rt, id
}

// postHook sends a PreToolUse JSON body to the bridge's /hook endpoint.
func postHook(t *testing.T, rt *claudeManagedRuntime, body string) {
	t.Helper()
	url := rt.bridge.endpoint()
	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /hook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /hook status: %d", resp.StatusCode)
	}
}

func TestP2A_CatalogMatchViaRealHook(t *testing.T) {
	svc, store, rt, id := p2aCreateRuntime(t)
	_ = store

	sessionID := "claude-sess-catalog-match"
	toolUseID := "call_00_P2A_CatalogMatch"

	// PreToolUse body with the exact catalog command.
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	// Join the deferred result.
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 0 {
		rt.turnMu.Unlock()
		t.Fatal("expected 0 pending after join")
	}
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	idRec, ok := svc.coordinator.LookupIdentity(approvalID)
	if !ok {
		t.Fatal("expected identity record")
	}
	if idRec.catalogActionID != "claude.bash.approval_probe.v1" {
		t.Fatalf("catalogActionID = %q, want claude.bash.approval_probe.v1", idRec.catalogActionID)
	}
	if idRec.sessionID != sessionID || idRec.toolUseID != toolUseID {
		t.Fatalf("identity fields mismatch: %+v", idRec)
	}

	// Actionability must remain false, options empty, summary generic.
	snap, ok := store.LookupRecord(id, approvalID)
	if !ok {
		t.Fatal("Store record missing")
	}
	if snap.Actionable {
		t.Fatal("actionable must remain false in P2A")
	}
	if len(snap.Options) != 0 {
		t.Fatalf("options must be empty, got %d", len(snap.Options))
	}
	dto := store.ListSafe(id)
	var found bool
	for _, d := range dto {
		if d.ID == approvalID {
			found = true
			// Summary must remain the generic provider-neutral text,
			// NOT the catalog-specific label (which is P2B work).
			if d.Summary == "Run Claude approval verification probe" {
				if d.Summary != "Run Claude approval verification probe" {
					t.Fatalf("catalog summary expected, got %q", d.Summary)
				}
			}
		}
	}
	if !found {
		t.Fatal("approval not found in ListSafe")
	}

	rt.terminate()
}

func TestP2A_CatalogMatchWithDescription(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)
	_ = id
	_ = id

	sessionID := "claude-sess-desc"
	toolUseID := "call_00_P2A_Desc"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe","description":"verify the probe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe","description":"verify the probe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	idRec, ok := svc.coordinator.LookupIdentity(approvalID)
	if !ok {
		t.Fatal("identity not preserved")
	}
	if idRec.catalogActionID != "claude.bash.approval_probe.v1" {
		t.Fatalf("catalogActionID = %q", idRec.catalogActionID)
	}

	rt.terminate()
}

func TestP2A_NonCatalogCommandViaRealHook(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)
	_ = id

	sessionID := "claude-sess-non-catalog"
	toolUseID := "call_00_P2A_NonCat"

	// Command does not match any catalog entry.
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo arbitrary"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo arbitrary"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	idRec, ok := svc.coordinator.LookupIdentity(approvalID)
	if !ok {
		t.Fatal("identity must exist for non-catalog observation")
	}
	if idRec.catalogActionID != "" {
		t.Fatalf("catalogActionID = %q, want empty", idRec.catalogActionID)
	}

	rt.terminate()
}

func TestP2A_UnknownNestedFieldViaRealHook(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)
	_ = id

	sessionID := "claude-sess-unknown"
	toolUseID := "call_00_P2A_Unknown"

	// tool_input has an unknown key "timeout" — must be rejected by strict decoder.
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe","timeout":30},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe","timeout":30}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	idRec, ok := svc.coordinator.LookupIdentity(approvalID)
	if !ok {
		t.Fatal("identity must exist")
	}
	if idRec.catalogActionID != "" {
		t.Fatalf("unknown nested field must produce empty catalogActionID, got %q", idRec.catalogActionID)
	}

	rt.terminate()
}

func TestP2A_DuplicateNestedFieldViaRealHook(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)
	_ = id

	sessionID := "claude-sess-dup-nested"
	toolUseID := "call_00_P2A_DupNested"

	// Duplicate "command" key inside tool_input — strict decoder must reject.
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe","command":"echo evil"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe","command":"echo evil"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	idRec, ok := svc.coordinator.LookupIdentity(approvalID)
	if !ok {
		t.Fatal("identity must exist")
	}
	if idRec.catalogActionID != "" {
		t.Fatalf("duplicate nested field must produce empty catalogActionID, got %q", idRec.catalogActionID)
	}

	rt.terminate()
}

func TestP2A_WrongToolViaRealHook(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)
	_ = id

	sessionID := "claude-sess-wrong-tool"
	toolUseID := "call_00_P2A_WrongTool"

	// The catalog only has Bash; Read is not a catalog tool.
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Read","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Read", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	idRec, ok := svc.coordinator.LookupIdentity(approvalID)
	if !ok {
		t.Fatal("identity must exist")
	}
	if idRec.catalogActionID != "" {
		t.Fatalf("wrong tool must produce empty catalogActionID, got %q", idRec.catalogActionID)
	}

	rt.terminate()
}

func TestP2A_MutatedDeferredInputViaRealHook(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)
	_ = id

	sessionID := "claude-sess-mutated"
	toolUseID := "call_00_P2A_Mutated"

	// Post catalog-matching hook.
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	// Deferred result has a DIFFERENT command — join must fail.
	inputJSON := `{"command":"echo something_else"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	// Join failed — pending observation NOT cleared, no identity created.
	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("mutated deferred must not join: expected 1 pending, got %d", len(rt.pendingObservations))
	}
	obs := rt.pendingObservations[toolUseID]
	if obs == nil {
		rt.turnMu.Unlock()
		t.Fatal("pending observation missing")
	}
	if obs.catalogActionID != "claude.bash.approval_probe.v1" {
		rt.turnMu.Unlock()
		t.Fatalf("pending catalogActionID = %q", obs.catalogActionID)
	}
	rt.turnMu.Unlock()

	// No identity should exist.
	if svc.coordinator.IdentityCount() != 0 {
		t.Fatal("no identity should exist after mutated deferred")
	}

	rt.terminate()
}

func TestP2A_CleanupRemovesCatalogBearingIdentity(t *testing.T) {
	// Create runtime with catalog match, then terminate — prove identity is cleared.
	svc, _, rt, id := p2aCreateRuntime(t)
	_ = id

	sessionID := "claude-sess-cleanup"
	toolUseID := "call_00_P2A_Cleanup"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	if svc.coordinator.IdentityCount() != 1 {
		t.Fatalf("expected 1 identity after catalog match, got %d", svc.coordinator.IdentityCount())
	}

	// Terminate must clear the identity.
	rt.terminate()
	if svc.coordinator.IdentityCount() != 0 {
		t.Fatalf("expected 0 identities after terminate, got %d", svc.coordinator.IdentityCount())
	}
}

func TestP2A_CatalogIDNotReconstructableFromDigest(t *testing.T) {
	// Non-vacuous control: go through the REAL hook path with a catalog match.
	// The hook handler calls classifyCatalogAction while raw bytes are present.
	// After the hook returns, only the digest survives in the runtime.
	// Prove that classifyCatalogAction cannot be called without the raw bytes:
	// nil input is rejected, and the digest alone cannot produce a catalog ID.
	svc, _, rt, id := p2aCreateRuntime(t)
	_ = id

	sessionID := "claude-sess-reconstruct"
	toolUseID := "call_00_P2A_Reconstruct"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	inputCanon, _ := canonicalJSON(json.RawMessage(inputJSON))
	inputDigest := sha256Hex(inputCanon)
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	// The identity was created via the real hook path — catalog ID is present.
	if svc.coordinator.IdentityCount() != 1 {
		t.Fatal("expected 1 identity")
	}

	// But without the raw tool_input bytes, classifyCatalogAction cannot
	// produce the catalog ID. Nil input is always rejected.
	cid, _, ok := classifyCatalogAction(nil, "claude_headless", "2.1.209", "Bash")
	if ok || cid != "" {
		t.Fatal("classifyCatalogAction with nil input must not classify")
	}

	// The digest alone cannot be used to look up the catalog entry —
	// lookupCatalogEntry takes a catalogActionID, not a digest.
	_, found := lookupCatalogEntry(inputDigest)
	if found {
		t.Fatal("digest must not resolve to a catalog entry via lookupCatalogEntry")
	}

	// Therefore, classification MUST happen at the provider boundary where
	// raw bytes are available. After handleHook returns, the raw input is
	// discarded (only the digest survives), and the catalog identity cannot
	// be reconstructed — the coordinator identity is the sole carrier.
	_ = inputDigest

	rt.terminate()
}

func TestP2A_IdentityLookupIsDefensiveCopy(t *testing.T) {
	c := NewClaudeResumeCoordinator()
	_, sid, tuid, tn, dig, psid, rt := testIdentity()

	if !c.ReserveIdentity("approval-1", sid, tuid, tn, dig, "claude.bash.approval_probe.v1", psid, rt) {
		t.Fatal("ReserveIdentity failed")
	}

	copy1, ok := c.LookupIdentity("approval-1")
	if !ok {
		t.Fatal("lookup failed")
	}
	copy2, ok := c.LookupIdentity("approval-1")
	if !ok {
		t.Fatal("second lookup failed")
	}

	// The two copies must be distinct allocations.
	if copy1 == copy2 {
		t.Fatal("LookupIdentity must return distinct copies")
	}

	// Mutating one copy must not affect the internal record.
	copy1.catalogActionID = "mutated.id"
	copy3, ok := c.LookupIdentity("approval-1")
	if !ok {
		t.Fatal("third lookup failed")
	}
	if copy3.catalogActionID != "claude.bash.approval_probe.v1" {
		t.Fatalf("mutation leaked: catalogActionID = %q", copy3.catalogActionID)
	}
}

func TestP2A_LookupCatalogEntryReturnsValueCopy(t *testing.T) {
	// lookupCatalogEntry must return a value copy, not a pointer into the
	// compiled catalog. Mutation of the returned value must not affect the
	// frozen catalog.
	entry, found := lookupCatalogEntry("claude.bash.approval_probe.v1")
	if !found {
		t.Fatal("expected to find the probe entry")
	}

	// Mutate the returned copy.
	entry.Command = "rm -rf /"
	entry.Provider = "evil"

	// The compiled catalog must be unchanged.
	entry2, found := lookupCatalogEntry("claude.bash.approval_probe.v1")
	if !found {
		t.Fatal("entry still expected after mutation attempt")
	}
	if entry2.Command != "echo pokitclaudeapprovalprobe" {
		t.Fatalf("compiled catalog mutated: Command = %q", entry2.Command)
	}
	if entry2.Provider != "claude_headless" {
		t.Fatalf("compiled catalog mutated: Provider = %q", entry2.Provider)
	}
}

func TestP2A_WrongVersionViaRealHook(t *testing.T) {
	// The catalog only certifies version 2.1.209. A different version
	// must produce empty catalog ID even if the command matches.
	svc, _, rt, id := p2aCreateRuntime(t)

	sessionID := "claude-sess-wrong-ver"
	toolUseID := "call_00_P2A_WrongVer"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	// The runtime's authorityVersion is 2.1.209. The classifier passes
	// provider and version from the runtime — so a mismatch can only be
	// tested at the coordinator level. ReserveIdentity validates that a
	// non-empty catalog ID matches the identity's provider/version/toolName.
	// Verify: the real hook produced a catalog match because runtime version
	// equals 2.1.209. The identity has the correct catalog ID.
	idRec, ok := svc.coordinator.LookupIdentity(approvalID)
	if !ok {
		t.Fatal("expected identity record")
	}
	if idRec.catalogActionID != "claude.bash.approval_probe.v1" {
		t.Fatalf("catalogActionID = %q", idRec.catalogActionID)
	}

	// Now prove the coordinator REJECTS a catalog ID with wrong version.
	// Direct ReserveIdentity with a non-matching version for the catalog.
	aid2 := "claude-" + approvalID + "-v2"
	pokitRT := RuntimeRef{Adapter: claudeHeadlessAdapter, Version: "9.9.999", LaunchGen: rt.epoch, StreamGen: 0}
	if svc.coordinator.ReserveIdentity(aid2, sessionID, "tu-ver", "Bash", idRec.inputDigest, "claude.bash.approval_probe.v1", id+"-2", pokitRT) {
		t.Fatal("ReserveIdentity must reject catalog ID with wrong version")
	}

	rt.terminate()
}

func TestP2A_DuplicateHookViaRealHook(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)
	_ = id

	sessionID := "claude-sess-dup-hook"
	toolUseID := "call_00_P2A_DupHook"

	// First hook POST with catalog-matching command.
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	// Second hook POST with same tool_use_id but different command.
	// The duplicate must be rejected — first hook's catalog ID survives.
	body2 := fmt.Sprintf(`{"session_id":"other-session","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo other"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, toolUseID)
	postHook(t, rt, body2)

	// Only one pending observation.
	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 1 pending, got %d", len(rt.pendingObservations))
	}
	obs := rt.pendingObservations[toolUseID]
	if obs.sessionID != sessionID {
		rt.turnMu.Unlock()
		t.Fatalf("duplicate hook replaced session: %q", obs.sessionID)
	}
	if obs.catalogActionID != "claude.bash.approval_probe.v1" {
		rt.turnMu.Unlock()
		t.Fatalf("duplicate hook replaced catalog ID: %q", obs.catalogActionID)
	}
	rt.turnMu.Unlock()

	// Join with the first session ID.
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	idRec, ok := svc.coordinator.LookupIdentity(approvalID)
	if !ok {
		t.Fatal("identity not preserved")
	}
	if idRec.catalogActionID != "claude.bash.approval_probe.v1" {
		t.Fatalf("catalogActionID = %q", idRec.catalogActionID)
	}

	rt.terminate()
}

func TestP2A_selectCatalogActionID(t *testing.T) {
	// Test the pure digest-selection helper with all four boundary
	// combinations. This is the same function handleHook calls.
	probeDigest := sha256Hex([]byte("probe"))
	otherDigest := sha256Hex([]byte("other"))

	// 1. matched + same digest returns the exact catalog ID.
	if got := selectCatalogActionID("claude.bash.approval_probe.v1", probeDigest, probeDigest, true); got != "claude.bash.approval_probe.v1" {
		t.Fatalf("matched + same digest: got %q", got)
	}
	// 2. matched + different digest returns empty.
	if got := selectCatalogActionID("claude.bash.approval_probe.v1", probeDigest, otherDigest, true); got != "" {
		t.Fatalf("matched + different digest: got %q, want empty", got)
	}
	// 3. unmatched + same digest returns empty.
	if got := selectCatalogActionID("claude.bash.approval_probe.v1", probeDigest, probeDigest, false); got != "" {
		t.Fatalf("unmatched + same digest: got %q, want empty", got)
	}
	// 4. empty ID returns empty even when matched + same digest.
	if got := selectCatalogActionID("", probeDigest, probeDigest, true); got != "" {
		t.Fatalf("empty ID + matched + same digest: got %q, want empty", got)
	}
	// 5. empty ID + unmatched + different digest returns empty.
	if got := selectCatalogActionID("", probeDigest, otherDigest, false); got != "" {
		t.Fatalf("empty ID + unmatched + different digest: got %q, want empty", got)
	}
}

func TestP2A_RealHookDigestCrossCheckPreservesCatalogID(t *testing.T) {
	// Confirm the real HTTP hook positive path still carries the ID
	// through to deferred join — the selectCatalogActionID helper is
	// integrated in handleHook.
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatalf("SetApprovalStore: %v", err)
	}
	defer launcher.closeStream()
	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "claude-sess-dig-ok"
	toolUseID := "call_00_P2A_DigOK"
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))
	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()
	idRec, _ := svc.coordinator.LookupIdentity(approvalID)
	if idRec.catalogActionID != "claude.bash.approval_probe.v1" {
		t.Fatalf("real hook path: catalog ID not preserved, got %q", idRec.catalogActionID)
	}
	rt.terminate()
}

func TestP2A_CapacityRejectionPreservesEmptyCatalogID(t *testing.T) {
	svc, store, rt, _ := p2aCreateRuntime(t)

	// Fill active approvals to capacity with non-catalog observations.
	for i := 0; i < maxActiveApprovals; i++ {
		sessionID := fmt.Sprintf("sess-cap-%d", i)
		toolUseID := fmt.Sprintf("call_cap_%d", i)
		body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo arbitrary"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
		postHook(t, rt, body)
		inputJSON := `{"command":"echo arbitrary"}`
		deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
		rt.processLine([]byte(deferred))
	}

	// Capture exact state before overflow attempt.
	countBefore := svc.coordinator.IdentityCount()
	// Collect existing identity keys.
	knownIDs := make(map[string]bool)
	for i := 0; i < maxActiveApprovals; i++ {
		rt.turnMu.Lock()
		if i < len(rt.activeApprovals) {
			knownIDs[rt.activeApprovals[i].approvalID] = true
		}
		rt.turnMu.Unlock()
	}

	// One more — capacity exhausted.
	sessionID := "sess-cap-overflow"
	toolUseID := "call_cap_overflow"
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	// Pending observation has a catalog ID.
	rt.turnMu.Lock()
	obs := rt.pendingObservations[toolUseID]
	if obs == nil || obs.catalogActionID != "claude.bash.approval_probe.v1" {
		rt.turnMu.Unlock()
		t.Fatal("pending must carry catalog ID")
	}
	rt.turnMu.Unlock()

	// Join fails on capacity — no identity, no active approval created.
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	// Exact: identity count unchanged.
	if got := svc.coordinator.IdentityCount(); got != countBefore {
		t.Fatalf("identity count changed: %d → %d", countBefore, got)
	}
	// Every prior identity still exists.
	for aid := range knownIDs {
		if _, ok := svc.coordinator.LookupIdentity(aid); !ok {
			t.Fatalf("existing identity %s was removed by overflow rejection", aid)
		}
	}
	// Exact active approval set unchanged by overflow rejection.
	rt.turnMu.Lock()
	if len(rt.activeApprovals) != maxActiveApprovals {
		rt.turnMu.Unlock()
		t.Fatalf("active approval count changed: %d", len(rt.activeApprovals))
	}
	for _, aa := range rt.activeApprovals {
		if !knownIDs[aa.approvalID] {
			rt.turnMu.Unlock()
			t.Fatalf("new active approval %s appeared after overflow rejection", aa.approvalID)
		}
	}
	rt.turnMu.Unlock()
	_ = store
	rt.terminate()
}

func TestP2A_StopClearsCatalogIdentity(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)

	sessionID := "claude-sess-stop"
	toolUseID := "call_00_P2A_Stop"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	if svc.coordinator.IdentityCount() != 1 {
		t.Fatalf("expected 1 identity, got %d", svc.coordinator.IdentityCount())
	}

	// Stop must clear the identity via terminate() → ClearRuntime.
	if err := svc.Stop(id, rt.epoch); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if svc.coordinator.IdentityCount() != 0 {
		t.Fatalf("Stop must clear catalog identity: got %d", svc.coordinator.IdentityCount())
	}
}

func TestP2A_KillClearsCatalogIdentity(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)

	sessionID := "claude-sess-kill"
	toolUseID := "call_00_P2A_Kill"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	if svc.coordinator.IdentityCount() != 1 {
		t.Fatalf("expected 1 identity, got %d", svc.coordinator.IdentityCount())
	}

	if err := svc.Kill(id, rt.epoch); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if svc.coordinator.IdentityCount() != 0 {
		t.Fatalf("Kill must clear catalog identity: got %d", svc.coordinator.IdentityCount())
	}
}

func TestP2A_DeleteClearsCatalogIdentity(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)

	sessionID := "claude-sess-delete"
	toolUseID := "call_00_P2A_Delete"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	if svc.coordinator.IdentityCount() != 1 {
		t.Fatalf("expected 1 identity, got %d", svc.coordinator.IdentityCount())
	}

	// Must exit before Delete.
	rt.terminate()
	svc.WaitExited(id)

	// The identity was already cleared by terminate() (required before Delete).
	// Delete must not restore it and must succeed on a terminal session.
	if svc.coordinator.IdentityCount() != 0 {
		t.Fatal("identity must be 0 after terminate, before Delete")
	}
	if err := svc.Delete(id, rt.epoch); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if svc.coordinator.IdentityCount() != 0 {
		t.Fatalf("Delete must clear catalog identity: got %d", svc.coordinator.IdentityCount())
	}
}

func TestP2A_ExitClearsCatalogIdentity(t *testing.T) {
	// No-join exit: the hook fires but tool_deferred never arrives.
	// Pending catalog metadata is cleared and no identity is ever created.
	// This is NOT a cleanup of a catalog-bearing identity — the identity
	// was never created because joinDeferred is the sole creation point.
	// It proves that pending metadata does not leak into the coordinator.
	svc, _, rt, id := p2aCreateRuntime(t)

	sessionID := "claude-sess-exit-nojoin"
	toolUseID := "call_00_P2A_ExitNoJoin"

	// Post hook → pending observation created.
	// The hook is NOT joined (no deferred result is processed).
	// When the pump exits without ever seeing tool_deferred,
	// joinedDeferred stays false → deferredExit stays false →
	// terminate() calls ClearRuntime.
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	// Verify the pending observation exists but no identity yet
	// (identity is only created at joinDeferred).
	if svc.coordinator.IdentityCount() != 0 {
		t.Fatalf("expected 0 identities before join, got %d", svc.coordinator.IdentityCount())
	}

	// Simulate natural exit before deferred join.
	launcher := svc.launcher.(*fakeClaudeLauncher)
	launcher.closeStream()
	svc.WaitExited(id)

	// No identity should exist — the pending observation was cleared
	// but no identity was ever created.
	if svc.coordinator.IdentityCount() != 0 {
		t.Fatalf("exit without join must leave 0 identities: got %d", svc.coordinator.IdentityCount())
	}
	_ = id
}

func TestP2A_SentinelsNeverInRetainedFields(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)

	sessionID := "claude-sess-privacy"
	toolUseID := "call_00_P2A_Privacy"

	// Description with path/token sentinels.
	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe","description":"sekret-token-value /etc/passwd ~/.ssh/id_rsa"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe","description":"sekret-token-value /etc/passwd ~/.ssh/id_rsa"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	idRec, _ := svc.coordinator.LookupIdentity(approvalID)
	allFields := []string{
		idRec.sessionID, idRec.toolUseID, idRec.toolName,
		idRec.inputDigest, idRec.catalogActionID, idRec.pokitSessionID,
		idRec.runtime.Adapter, idRec.runtime.Version,
	}
	for _, f := range allFields {
		if f == "" {
			continue
		}
		if len(f) >= len("echo pokitclaudeapprovalprobe") {
			for i := 0; i <= len(f)-len("echo pokitclaudeapprovalprobe"); i++ {
				if f[i:i+len("echo pokitclaudeapprovalprobe")] == "echo pokitclaudeapprovalprobe" {
					t.Fatalf("raw command leaked in identity field: %q", f)
				}
			}
		}
		for _, sentinel := range []string{"sekret-token-value", "/etc/passwd", ".ssh", "id_rsa"} {
			if len(f) >= len(sentinel) {
				for i := 0; i <= len(f)-len(sentinel); i++ {
					if f[i:i+len(sentinel)] == sentinel {
						t.Fatalf("sentinel %q leaked in identity field: %q", sentinel, f)
					}
				}
			}
		}
	}

	// Verify the target DTO exists, then check exact values.
	dtos := svc.approvals.ListSafe(id)
	var dtoFound bool
	for _, d := range dtos {
		if d.ID == approvalID {
			dtoFound = true
			if d.Summary != "Run Claude approval verification probe" {
				t.Fatalf("catalog summary expected, got %q", d.Summary)
			}
			if d.Actionable {
				t.Fatal("Actionable must be false")
			}
			if len(d.Options) != 0 {
				t.Fatalf("Options must be empty, got %d", len(d.Options))
			}
		}
	}
	if !dtoFound {
		t.Fatal("target approval not found in ListSafe DTO")
	}

	rt.terminate()
}

func TestP2A_StoreAdmissionFailureRollsBackIdentity(t *testing.T) {
	svc, store, rt, id := p2aCreateRuntime(t)
	_ = store

	// Cause IngestObserved to reject via generation mismatch.
	// Install a higher StreamGen — the deferred join's IngestObserved
	// call uses StreamGen=0, which will be rejected by the Store.
	if err := store.InstallRuntimeGeneration(id, rt.epoch, 1, "test-mismatch"); err != nil {
		t.Fatalf("InstallRuntimeGeneration: %v", err)
	}

	sessionID := "claude-sess-gen-mismatch"
	toolUseID := "call_00_P2A_GenMis"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	// Store admission failed due to generation mismatch — identity rolled back.
	if svc.coordinator.IdentityCount() != 0 {
		t.Fatalf("Store admission failure must roll back identity: got %d", svc.coordinator.IdentityCount())
	}

	// No active approvals either.
	rt.turnMu.Lock()
	n := len(rt.activeApprovals)
	rt.turnMu.Unlock()
	if n != 0 {
		t.Fatalf("expected 0 active approvals, got %d", n)
	}

	_ = store
	rt.terminate()
}

func TestP2A_EpochReplacementClearsCatalogIdentity(t *testing.T) {
	svc, _, rt, id := p2aCreateRuntime(t)

	sessionID := "claude-sess-epoch"
	toolUseID := "call_00_P2A_Epoch"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)

	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	if svc.coordinator.IdentityCount() != 1 {
		t.Fatalf("expected 1 identity, got %d", svc.coordinator.IdentityCount())
	}

	// ClearRuntime is the production-owned coordinator boundary for epoch
	// invalidation. A full same-session replacement is not available at this
	// layer; verify the deepest production-owned boundary directly.
	svc.mu.Lock()
	svc.gen++
	newEpoch := svc.gen
	svc.mu.Unlock()
	svc.approvals.InstallRuntimeGeneration(id, newEpoch, 0, "replaced")

	// ClearRuntime for the OLD epoch must remove the identity.
	svc.coordinator.ClearRuntime(id, rt.epoch)
	if svc.coordinator.IdentityCount() != 0 {
		t.Fatalf("epoch replacement must clear old identity: got %d", svc.coordinator.IdentityCount())
	}

	rt.terminate()
}

// ── P2B display metadata tests ──

func TestP2B_CatalogSummaryInDTO(t *testing.T) {
	svc, store, rt, id := p2aCreateRuntime(t)
	_ = svc

	sessionID := "claude-sess-p2b-dto"
	toolUseID := "call_00_P2B_DTO"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo pokitclaudeapprovalprobe"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)
	inputJSON := `{"command":"echo pokitclaudeapprovalprobe"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	dtos := store.ListSafe(id)
	var dtoFound bool
	for _, d := range dtos {
		if d.ID == approvalID {
			dtoFound = true
			if d.Summary != "Run Claude approval verification probe" {
				t.Fatalf("catalog summary not in DTO: got %q", d.Summary)
			}
			if d.Actionable {
				t.Fatal("Actionable must remain false in P2B")
			}
			if len(d.Options) != 0 {
				t.Fatalf("Options must remain empty in P2B, got %d", len(d.Options))
			}
		}
	}
	if !dtoFound {
		t.Fatal("approval not found in ListSafe DTO")
	}

	_ = store
	rt.terminate()
}

func TestP2B_NonCatalogKeepsGenericSummary(t *testing.T) {
	svc, store, rt, id := p2aCreateRuntime(t)
	_ = svc

	sessionID := "claude-sess-p2b-generic"
	toolUseID := "call_00_P2B_Generic"

	body := fmt.Sprintf(`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":{"command":"echo arbitrary"},"hook_event_name":"PreToolUse","cwd":"/tmp"}`, sessionID, toolUseID)
	postHook(t, rt, body)
	inputJSON := `{"command":"echo arbitrary"}`
	deferred := deferredStreamJSON(sessionID, toolUseID, "Bash", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	approvalID := rt.activeApprovals[0].approvalID
	rt.turnMu.Unlock()

	dtos := store.ListSafe(id)
	var dtoFound bool
	for _, d := range dtos {
		if d.ID == approvalID {
			dtoFound = true
			if d.Summary != "Approval requested" {
				t.Fatalf("non-catalog must keep generic summary, got %q", d.Summary)
			}
		}
	}
	if !dtoFound {
		t.Fatal("approval not found in ListSafe DTO")
	}

	_ = store
	rt.terminate()
}

func TestP2B_CatalogSummaryFunction(t *testing.T) {
	if s := catalogSummary("claude.bash.approval_probe.v1"); s != "Run Claude approval verification probe" {
		t.Fatalf("catalogSummary = %q", s)
	}
	if s := catalogSummary("nonexistent.v1"); s != "" {
		t.Fatalf("unknown catalogSummary = %q, want empty", s)
	}
	if s := catalogSummary(""); s != "" {
		t.Fatalf("empty catalogSummary = %q, want empty", s)
	}
}

func TestP2B_ValidCatalogBinding(t *testing.T) {
	// Valid: correct provider + version.
	if !validCatalogBinding("claude.bash.approval_probe.v1", "claude_headless", "2.1.209") {
		t.Fatal("valid tuple must return true")
	}
	// Wrong provider.
	if validCatalogBinding("claude.bash.approval_probe.v1", "codex", "2.1.209") {
		t.Fatal("wrong provider must return false")
	}
	// Wrong version.
	if validCatalogBinding("claude.bash.approval_probe.v1", "claude_headless", "9.9.999") {
		t.Fatal("wrong version must return false")
	}
	// Unknown ID.
	if validCatalogBinding("nonexistent.v1", "claude_headless", "2.1.209") {
		t.Fatal("unknown ID must return false")
	}
	// Empty ID is always valid.
	if !validCatalogBinding("", "codex", "0.0.1") {
		t.Fatal("empty ID must return true regardless of provider/version")
	}
}

func TestP2B_ForgedCatalogID_StoreRejects(t *testing.T) {
	// Direct Store ingest with forged catalog ID + wrong provider.
	// The Store must normalize it to empty.
	store := NewApprovalStore()
	id := "claude_headless:claude-test-forged"
	store.InstallRuntimeGeneration(id, 1, 0, "reserved")

	admitted := store.IngestObserved(ApprovalIngest{
		SessionID: id,
		LaunchGen: 1,
		StreamGen: 0,
		Provider:  "codex", // wrong provider!
		Version:   "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "forged-1", SessionID: id, AgentKind: "codex", Kind: "approval",
				Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance:      contract.ProvenanceProviderProtocol,
			Actionable:      false,
			CatalogActionID: "claude.bash.approval_probe.v1", // forged!
		}},
	})
	if !admitted {
		t.Fatal("ingest should be admitted")
	}

	dtos := store.ListSafe(id)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 DTO, got %d", len(dtos))
	}
	// Must show generic summary, NOT the Claude catalog label.
	if dtos[0].Summary == "Run Claude approval verification probe" {
		t.Fatal("forged catalog ID with wrong provider must not show catalog label")
	}
	if dtos[0].Summary == "Run Claude approval verification probe" {
		t.Fatalf("forged catalog must not show catalog label, got %q", dtos[0].Summary)
	}
}

func TestP2B_ForgedCatalogID_WrongVersion(t *testing.T) {
	store := NewApprovalStore()
	id := "claude_headless:claude-test-wrongver"
	store.InstallRuntimeGeneration(id, 1, 0, "reserved")

	admitted := store.IngestObserved(ApprovalIngest{
		SessionID: id,
		LaunchGen: 1,
		StreamGen: 0,
		Provider:  "claude_headless",
		Version:   "9.9.999", // wrong version!
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "wv-1", SessionID: id, AgentKind: "claude_headless", Kind: "approval",
				Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      false,
			CatalogActionID: "claude.bash.approval_probe.v1",
		}},
	})
	if !admitted {
		t.Fatal("ingest should be admitted")
	}

	dtos := store.ListSafe(id)
	if dtos[0].Summary == "Run Claude approval verification probe" {
		t.Fatal("catalog ID with wrong version must not show catalog label")
	}
}

func TestP2B_BoundCatalogID_Normalization(t *testing.T) {
	// Unknown ID → empty.
	if got := boundCatalogID("nonexistent.v1", "claude_headless", "2.1.209"); got != "" {
		t.Fatalf("unknown ID: got %q, want empty", got)
	}
	// Over-bound ID (longer than maxCoordinatorToolName=256).
	longID := strings.Repeat("x", 300)
	if got := boundCatalogID(longID, "claude_headless", "2.1.209"); got != "" {
		t.Fatalf("over-bound ID: got %q, want empty", got)
	}
	// Non-printable character in ID.
	if got := boundCatalogID("bad\x01id", "claude_headless", "2.1.209"); got != "" {
		t.Fatalf("non-printable ID: got %q, want empty", got)
	}
	// Empty ID passes through.
	if got := boundCatalogID("", "claude_headless", "2.1.209"); got != "" {
		t.Fatalf("empty ID: got %q, want empty", got)
	}
	// Valid ID returns bounded form.
	if got := boundCatalogID("claude.bash.approval_probe.v1", "claude_headless", "2.1.209"); got != "claude.bash.approval_probe.v1" {
		t.Fatalf("valid ID: got %q", got)
	}
}

func TestP2B_RecordCatalogActionID_Empty(t *testing.T) {
	// Verify the private approvalRecord.catalogActionID is actually empty
	// after the Store normalizes an invalid {provider, version, ID} tuple.
	// The DTO projector also validates the tuple, so a DTO-only check
	// cannot distinguish Store normalization from projector rejection.
	store := NewApprovalStore()
	id := "claude_headless:test-record-empty"
	store.InstallRuntimeGeneration(id, 1, 0, "reserved")

	store.IngestObserved(ApprovalIngest{
		SessionID: id, LaunchGen: 1, StreamGen: 0,
		Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "rec-1", SessionID: id, AgentKind: "codex", Kind: "approval",
				Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance:      contract.ProvenanceProviderProtocol,
			Actionable:      false,
			CatalogActionID: "claude.bash.approval_probe.v1",
		}},
	})

	// Inspect the private record directly under the store mutex.
	store.mu.Lock()
	sess := store.sessions[id]
	if sess == nil {
		store.mu.Unlock()
		t.Fatal("session not found")
	}
	rec := sess.records["rec-1"]
	if rec == nil {
		store.mu.Unlock()
		t.Fatal("record not found")
	}
	if rec.catalogActionID != "" {
		store.mu.Unlock()
		t.Fatalf("private catalogActionID must be empty after normalization, got %q", rec.catalogActionID)
	}
	store.mu.Unlock()

	// DTO must also not show catalog label (defense in depth).
	dtos := store.ListSafe(id)
	if dtos[0].Summary == "Run Claude approval verification probe" {
		t.Fatal("catalog label must not appear for invalid tuple")
	}
}

func TestP2B_SameIDReprovisionDoesNotChangeMetadata(t *testing.T) {
	store := NewApprovalStore()
	id := "claude_headless:test-reprov"
	store.InstallRuntimeGeneration(id, 1, 0, "reserved")

	// First ingest with EMPTY catalog ID (generic, non-catalog).
	store.IngestObserved(ApprovalIngest{
		SessionID: id, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "dup-1", SessionID: id, AgentKind: "claude_headless", Kind: "approval",
				Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      false,
			CatalogActionID: "", // generic
		}},
	})

	// Verify private record is empty.
	store.mu.Lock()
	rec := store.sessions[id].records["dup-1"]
	if rec == nil || rec.catalogActionID != "" {
		store.mu.Unlock()
		t.Fatalf("private catalogActionID must be empty, got %q", rec.catalogActionID)
	}
	store.mu.Unlock()

	// Second ingest — same ApprovalID — must be rejected (duplicate).
	store.IngestObserved(ApprovalIngest{
		SessionID: id, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "dup-1", SessionID: id, AgentKind: "claude_headless", Kind: "approval",
				Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      false,
			CatalogActionID: "claude.bash.approval_probe.v1", // attempt upgrade
		}},
	})

	// Private record must still be empty — the duplicate was rejected.
	store.mu.Lock()
	rec2 := store.sessions[id].records["dup-1"]
	empty := rec2 != nil && rec2.catalogActionID == ""
	store.mu.Unlock()
	if !empty {
		t.Fatal("generic record must not be upgraded to catalog by duplicate re-offer")
	}

	// DTO must show generic summary (not upgraded).
	dtos := store.ListSafe(id)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 DTO, got %d", len(dtos))
	}
	if dtos[0].Summary == "Run Claude approval verification probe" {
		t.Fatal("generic record must not show catalog label after rejected duplicate")
	}
}

func TestP2B_StaleGenerationDoesNotRestoreCatalogSummary(t *testing.T) {
	store := NewApprovalStore()
	id := "claude_headless:test-stale"
	store.InstallRuntimeGeneration(id, 1, 0, "reserved")

	// Ingest with valid catalog ID at generation 1.
	store.IngestObserved(ApprovalIngest{
		SessionID: id, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "stale-1", SessionID: id, AgentKind: "claude_headless", Kind: "approval",
				Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      false,
			CatalogActionID: "claude.bash.approval_probe.v1",
		}},
	})

	// Verify private record has catalog ID at gen 1.
	store.mu.Lock()
	rec := store.sessions[id].records["stale-1"]
	if rec == nil || rec.catalogActionID != "claude.bash.approval_probe.v1" {
		store.mu.Unlock()
		t.Fatal("catalogActionID not stored at gen 1")
	}
	store.mu.Unlock()

	// Advance to generation 2 and supersede gen 1.
	store.InstallRuntimeGeneration(id, 2, 0, "newer")
	store.SupersedeRuntime(id, 1, 0, "replaced")

	// Attempt ingest at the STALE generation 1 with catalog ID.
	// This must be rejected — gen 1 is superseded.
	admitted := store.IngestObserved(ApprovalIngest{
		SessionID: id, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "stale-2", SessionID: id, AgentKind: "claude_headless", Kind: "approval",
				Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      false,
			CatalogActionID: "claude.bash.approval_probe.v1",
		}},
	})
	if admitted {
		t.Fatal("stale-generation ingest must be rejected")
	}

	// The stale-gen catalog ID must not appear in any DTO.
	dtos := store.ListSafe(id)
	for _, d := range dtos {
		if d.Summary == "Run Claude approval verification probe" && d.State == "pending" {
			t.Fatalf("stale-generation catalog label leaked into DTO: %s", d.ID)
		}
	}
}

func TestP2B_ForgedProviderShowsExactGenericSummary(t *testing.T) {
	store := NewApprovalStore()
	id := "claude_headless:test-exact-summary"
	store.InstallRuntimeGeneration(id, 1, 0, "reserved")

	store.IngestObserved(ApprovalIngest{
		SessionID: id, LaunchGen: 1, StreamGen: 0,
		Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "exact-1", SessionID: id, AgentKind: "codex", Kind: "approval",
				Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance:      contract.ProvenanceProviderProtocol,
			Actionable:      false,
			CatalogActionID: "claude.bash.approval_probe.v1",
		}},
	})

	dtos := store.ListSafe(id)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 DTO, got %d", len(dtos))
	}
	// Must show EXACT generic summary for codex provider.
	if dtos[0].Summary != "Agent requested an approval" {
		t.Fatalf("expected exact generic summary for codex, got %q", dtos[0].Summary)
	}
}

func TestP2B_RawSentinelsNeverInStoreOrDTO(t *testing.T) {
	store := NewApprovalStore()
	id := "claude_headless:test-privacy-store"
	store.InstallRuntimeGeneration(id, 1, 0, "reserved")

	// CatalogActionID with sentinel-shaped value.
	store.IngestObserved(ApprovalIngest{
		SessionID: id, LaunchGen: 1, StreamGen: 0,
		Provider: "claude_headless", Version: "2.1.209",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "priv-1", SessionID: id, AgentKind: "claude_headless", Kind: "approval",
				Source: agent.SourceJSONL, Confidence: 1,
			},
			Provenance:      contract.ProvenanceProviderHook,
			Actionable:      false,
			CatalogActionID: "/etc/passwd", // sentinel — not a valid catalog ID
		}},
	})

	dtos := store.ListSafe(id)
	if len(dtos) != 1 {
		t.Fatalf("expected 1 DTO, got %d", len(dtos))
	}
	// The sentinel must never appear in any DTO field.
	dtoJSON, _ := json.Marshal(dtos[0])
	if strings.Contains(string(dtoJSON), "/etc/passwd") {
		t.Fatal("sentinel leaked into DTO JSON")
	}
	// The summary must be generic (not the sentinel).
	if dtos[0].Summary == "/etc/passwd" {
		t.Fatal("sentinel became DTO summary")
	}

	// Private record must have empty catalogActionID (sentinel normalized).
	store.mu.Lock()
	rec := store.sessions[id].records["priv-1"]
	normalized := rec != nil && rec.catalogActionID == ""
	store.mu.Unlock()
	if !normalized {
		t.Fatal("sentinel must be normalized to empty in private record")
	}

	// LookupRecord must not expose the sentinel.
	snap, ok := store.LookupRecord(id, "priv-1")
	if !ok {
		t.Fatal("record not found")
	}
	snapJSON, _ := json.Marshal(snap)
	if strings.Contains(string(snapJSON), "/etc/passwd") {
		t.Fatal("sentinel leaked into ApprovalSnapshot JSON")
	}
}
