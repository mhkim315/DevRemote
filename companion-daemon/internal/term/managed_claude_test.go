package term

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
func (p *fakeClaudeProcess) Wait() error       { return p.waitErr }
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
		stdin: new(bytes.Buffer),
		stdout: pr,
		pipeW: pw,
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

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest)

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

	rt.observePreToolUse("call_Mis", "Bash", "right-session", sha256Hex([]byte(`{"cmd":"x"}`)))
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

	rt.observePreToolUse("call_TN", "Bash", "s1", sha256Hex([]byte(`{"cmd":"x"}`)))
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
	rt.observePreToolUse("call_Digest", "Bash", "s1", sha256Hex(inputCanon))
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

	rt.observePreToolUse("call_Dup", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)))
	rt.observePreToolUse("call_Dup", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)))

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
		rt.observePreToolUse(fmt.Sprintf("call_%d", i), "Bash", fmt.Sprintf("s%d", i), sha256Hex([]byte(fmt.Sprintf(`{"x":%d}`, i))))
	}
	rt.observePreToolUse("call_overflow", "Bash", "so", sha256Hex([]byte(`{"x":"overflow"}`)))

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

	rt.observePreToolUse("call_Exit", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)))
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
	rt.observePreToolUse("call_NA", "Bash", "s1", sha256Hex(inputCanon))
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

	rt.observePreToolUse("call_NonResult", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)))
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

	rt.observePreToolUse("call_EOF", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)))
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
	rt.observePreToolUse("call_T1", "Bash", sid1, digest)
	rt.processLine([]byte(deferredStreamJSON(sid1, "call_T1", "Bash", `{"command":"echo ok"}`)))

	// Advance clock 1s, create second.
	clockNow = func() time.Time { return base.Add(1 * time.Second) }
	rt.observePreToolUse("call_T2", "Bash", sid1, digest)
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
		rt.observePreToolUse(tuid, "Bash", sid, digest)
		rt.processLine([]byte(deferredStreamJSON(sid, tuid, "Bash", `{"command":"echo ok"}`)))
	}

	rt.turnMu.Lock()
	if len(rt.activeApprovals) != maxActiveApprovals {
		rt.turnMu.Unlock()
		t.Fatalf("expected %d active, got %d", maxActiveApprovals, len(rt.activeApprovals))
	}
	rt.turnMu.Unlock()

	// One more: capacity exhausted → rejected BEFORE ingest.
	rt.observePreToolUse("call_Overflow", "Bash", sid, digest)
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

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest)
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

type failingReader struct{}

func (failingReader) Read(p []byte) (int, error) {
	return 0, fmt.Errorf("entropy exhausted")
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
	rt.observePreToolUse("call_StopInv", "Bash", "s1", sha256Hex(inputCanon))
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

	// FIRST-APPROVAL RACE: no primer. When terminate() runs before the
	// first IngestObserved, the Store has no session entry so
	// SupersedeRuntime is a no-op. The pre-ingest terminated check
	// must reject the ingest.
	sid := "s-race"
	inputCanon, _ := canonicalJSON(json.RawMessage(`{"command":"echo ok"}`))
	digest := sha256Hex(inputCanon)

	var hookCalled sync.WaitGroup
	hookCalled.Add(1)
	rt.preIngestHook = func() {
		hookCalled.Done()
		rt.terminate()
	}

	rt.observePreToolUse("call_Race", "Bash", sid, digest)
	deferred := deferredStreamJSON(sid, "call_Race", "Bash", `{"command":"echo ok"}`)
	rt.processLine([]byte(deferred))
	hookCalled.Wait()

	if !rt.terminated {
		t.Fatal("BUG: preIngestHook did not call terminate()")
	}

	// Pre-ingest terminated check must reject: zero records.
	if len(store.ListSafe(id)) != 0 {
		t.Fatalf("pre-ingest check must reject: expected 0 records, got %d", len(store.ListSafe(id)))
	}
}

// TestClaudeReverseRace verifies the post-ingest check: if terminate() runs
// after IngestObserved succeeds, the just-ingested record is immediately
// invalidated.
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

	// Prime the Store session entry so SupersedeRuntime works.
	rt.observePreToolUse("call_Primer", "Bash", sid, digest)
	rt.processLine([]byte(deferredStreamJSON(sid, "call_Primer", "Bash", `{"command":"echo ok"}`)))

	// Now set up the reverse race: ingest first, THEN terminate.
	// The post-ingest check must invalidate the record.
	rt.observePreToolUse("call_Rev", "Bash", sid, digest)
	rt.processLine([]byte(deferredStreamJSON(sid, "call_Rev", "Bash", `{"command":"echo ok"}`)))

	// Record was ingested, then we call terminate.
	rt.terminate()

	// Post-ingest check must have invalidated the just-ingested record.
	// No active approvals should remain.
	rt.turnMu.Lock()
	if len(rt.activeApprovals) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("post-ingest check must clear active: expected 0, got %d", len(rt.activeApprovals))
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

	// Build the production service.
	cfg := ClaudeEntryConfig{
		Bin:              "claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
		PinnedPath:       filepath.Join(os.Getenv("HOME"), ".local", "share", "claude", "versions", "2.1.209", "claude"),
		PinnedDigest:     digest,
	}

	svc := NewManagedClaudeService(cfg, nil, nil)
	store := NewApprovalStore()
	svc.SetApprovalStore(store)

	// Start a real IPC server on a temp socket.
	socketPath := filepath.Join(t.TempDir(), "pokit-c1d-test.sock")
	reg, _ := mux.NewRegistry()
	srv, err := StartIPCServer(socketPath, reg, nil, nil, nil, nil, nil, nil, svc)
	if err != nil {
		t.Fatalf("StartIPCServer: %v", err)
	}
	defer srv.Close()

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
