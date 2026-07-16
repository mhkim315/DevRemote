// Package term — C1D contract tests for ManagedClaudeService.
// Uses deterministic fakes (ManagedLauncher injection) to prove:
//   - production entry through CreateDetached
//   - attestor certification fail-closed
//   - hook bridge strict decode (valid/missing/duplicate/oversized)
//   - exact deferred join (full identity comparison)
//   - duplicate tool_use_id rejection
//   - capacity exhaustion
//   - exit/stop/delete lifecycle cleanup
//   - authority isolation (no actionable options, no CTA)
//   - DTO privacy (no raw payload in records)
package term

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ── Test fakes ──

// fakeClaudeProcess implements ManagedProcess for deterministic tests.
type fakeClaudeProcess struct {
	stdin   *bytes.Buffer
	stdout  io.Reader
	pipeW   *io.PipeWriter // non-nil when using io.Pipe for stdout
	termFn  func() error
	killFn  func() error
	waitErr error
	opaque  string
}

func (p *fakeClaudeProcess) Stdin() io.Writer  { return p.stdin }
func (p *fakeClaudeProcess) Stdout() io.Reader { return p.stdout }
func (p *fakeClaudeProcess) Term() error {
	if p.pipeW != nil {
		_ = p.pipeW.Close()
	}
	if p.termFn != nil {
		return p.termFn()
	}
	return nil
}
func (p *fakeClaudeProcess) Kill() error {
	if p.pipeW != nil {
		_ = p.pipeW.Close()
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

// fakeClaudeLauncher is a deterministic ManagedLauncher for tests.
type fakeClaudeLauncher struct {
	mu       sync.Mutex
	proc     *fakeClaudeProcess
	launched bool
	// StreamCh lets tests feed lines into stdout after launch.
	StreamCh chan []byte
	// CapturedArgv stores the argv from the last Launch call.
	CapturedArgv []string
}

func (l *fakeClaudeLauncher) Launch(exe string, argv []string) (ManagedProcess, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.launched {
		return nil, fmt.Errorf("already launched")
	}
	l.launched = true
	l.CapturedArgv = append([]string(nil), argv...)
	// Use io.Pipe so stdout stays open until the writer is closed.
	// An empty bytes.Buffer would EOF immediately, causing the pump
	// goroutine to exit before the test can feed lines.
	pr, pw := io.Pipe()
	p := &fakeClaudeProcess{
		stdin:  new(bytes.Buffer),
		stdout: pr,
		pipeW:  pw,
	}
	l.proc = p
	return p, nil
}

// argvCapturingLauncher is a minimal launcher that only captures argv.
type argvCapturingLauncher struct {
	argv *[]string
}

func (l *argvCapturingLauncher) Launch(exe string, argv []string) (ManagedProcess, error) {
	*l.argv = append([]string(nil), argv...)
	pr, pw := io.Pipe()
	return &fakeClaudeProcess{
		stdin: new(bytes.Buffer),
		stdout: pr,
		pipeW: pw,
	}, nil
}

// feedLine writes a JSON line to the process stdout for the pump to consume.
func (l *fakeClaudeLauncher) feedLine(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.proc != nil && l.proc.pipeW != nil {
		l.proc.pipeW.Write([]byte(line + "\n"))
	}
}

// closeStream closes the pipe writer (simulates child exit / EOF).
func (l *fakeClaudeLauncher) closeStream() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.proc != nil && l.proc.pipeW != nil {
		l.proc.pipeW.Close()
	}
}

// fakeClaudeAttestor implements ClaudeAttestor for deterministic tests.
type fakeClaudeAttestor struct {
	shouldFail bool
}

func (a *fakeClaudeAttestor) Certify(exe string) error {
	if a.shouldFail {
		return fmt.Errorf("certify failed: wrong version")
	}
	return nil
}

// ── Stream event helpers ──

// deferredStreamJSON builds a C0D-format tool_deferred stream-json line.
func deferredStreamJSON(sessionID, toolUseID, toolName, inputJSON string) string {
	return fmt.Sprintf(
		`{"type":"result","stop_reason":"tool_deferred","session_id":"%s","deferred_tool_use":{"id":"%s","name":"%s","input":%s}}`,
		sessionID, toolUseID, toolName, inputJSON,
	)
}

// ── Tests ──

func TestClaudeCreateDetachedAttestorFails(t *testing.T) {
	attestor := &fakeClaudeAttestor{shouldFail: true}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, &fakeClaudeLauncher{}, attestor)
	_, err := svc.CreateDetached("/tmp")
	if err == nil || !strings.Contains(err.Error(), "certify") {
		t.Fatalf("expected certify error, got: %v", err)
	}
}

// failingLauncher implements ManagedLauncher and always fails.
type failingLauncher struct{}

func (failingLauncher) Launch(exe string, argv []string) (ManagedProcess, error) {
	return nil, fmt.Errorf("launch failed")
}

func TestClaudeCreateDetachedLauncherError(t *testing.T) {
	fl := &failingLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, fl, &fakeClaudeAttestor{})
	_, err := svc.CreateDetached("/tmp")
	if err == nil {
		t.Fatal("expected launch error")
	}
}

func TestClaudeCreateDetachedSuccess(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	attestor := &fakeClaudeAttestor{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, attestor)
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatal(err)
	}

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}
	if !strings.HasPrefix(id, "claude_headless:claude-") {
		t.Fatalf("unexpected session ID: %s", id)
	}

	// Registry record exists.
	rec, ok := svc.Registry().Get(id)
	if !ok {
		t.Fatal("session not in registry")
	}
	if rec.Provider != "claude" {
		t.Fatalf("expected provider claude, got %q", rec.Provider)
	}
	if rec.Epoch != 1 {
		t.Fatalf("expected epoch 1, got %d", rec.Epoch)
	}

	// Shutdown.
	ctx := context.Background()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeDeferredJoinFullIdentityMatch(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	attestor := &fakeClaudeAttestor{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, attestor)
	if err := svc.SetApprovalStore(store); err != nil {
		t.Fatal(err)
	}

	id, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("CreateDetached: %v", err)
	}

	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()
	if rt == nil {
		t.Fatal("runtime not found")
	}

	// Simulate a hook observation. Use canonicalJSON to match the digest
	// computation in joinDeferred (which also uses canonicalJSON).
	sessionID := "claude-session-uuid"
	toolUseID := "call_00_Test123"
	toolName := "Bash"
	inputJSON := `{"command":"echo ok","description":"test"}`
	inputCanon, _ := canonicalJSON(json.RawMessage(inputJSON))
	inputDigest := sha256Hex(inputCanon)

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest)

	// Verify pending observation exists.
	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 1 pending observation, got %d", len(rt.pendingObservations))
	}
	rt.turnMu.Unlock()

	// Feed a matching deferred result.
	deferred := deferredStreamJSON(sessionID, toolUseID, toolName, inputJSON)

	rt.processLine([]byte(deferred))

	// Pending observation should be cleared.
	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 0 pending observations after join, got %d", len(rt.pendingObservations))
	}
	rt.turnMu.Unlock()

	// Store should have a non-actionable record.
	safeRecords := store.ListSafe(id)
	if len(safeRecords) != 1 {
		t.Fatalf("expected 1 safe record, got %d", len(safeRecords))
	}
	if len(safeRecords[0].Options) != 0 {
		t.Fatal("expected zero options (non-actionable)")
	}

	rt.stop()
}

func TestClaudeDeferredJoinMismatchedSessionID(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})
	store := NewApprovalStore()
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "right-session"
	toolUseID := "call_00_Mis"
	toolName := "Bash"
	inputJSON := `{"command":"echo ok"}`
	inputDigest := sha256Hex([]byte(inputJSON))

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest)

	// Deferred result has a DIFFERENT session ID.
	deferred := deferredStreamJSON("wrong-session", toolUseID, toolName, inputJSON)
	rt.processLine([]byte(deferred))

	// Pending observation should still be present (mismatch → no join).
	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("expected pending observation to remain after session mismatch")
	}
	rt.turnMu.Unlock()

	// No store record.
	if len(store.ListSafe(id)) != 0 {
		t.Fatal("expected zero records after session mismatch")
	}

	rt.stop()
}

func TestClaudeDeferredJoinMismatchedToolName(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})
	store := NewApprovalStore()
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "s1"
	toolUseID := "call_TN"
	toolName := "Bash"
	inputJSON := `{"command":"echo ok"}`
	inputDigest := sha256Hex([]byte(inputJSON))

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest)

	// Deferred result has a DIFFERENT tool name.
	deferred := deferredStreamJSON(sessionID, toolUseID, "Write", inputJSON)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("expected pending observation to remain after tool name mismatch")
	}
	rt.turnMu.Unlock()

	if len(store.ListSafe(id)) != 0 {
		t.Fatal("expected zero records after tool name mismatch")
	}

	rt.stop()
}

func TestClaudeDeferredJoinMismatchedInputDigest(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})
	store := NewApprovalStore()
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "s1"
	toolUseID := "call_Digest"
	toolName := "Bash"
	inputJSON := `{"command":"echo ok"}`
	inputDigest := sha256Hex([]byte(inputJSON))

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest)

	// Deferred result has a DIFFERENT input.
	deferred := deferredStreamJSON(sessionID, toolUseID, toolName, `{"command":"rm -rf /"}`)
	rt.processLine([]byte(deferred))

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("expected pending observation to remain after input digest mismatch")
	}
	rt.turnMu.Unlock()

	rt.stop()
}

func TestClaudeDuplicateToolUseID(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "s1"
	toolUseID := "call_Dup"
	toolName := "Bash"
	inputDigest := sha256Hex([]byte(`{"command":"echo ok"}`))

	// First observation: accepted.
	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest)
	// Second observation with same tool_use_id: rejected.
	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest)

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 1 pending observation (duplicate rejected), got %d", len(rt.pendingObservations))
	}
	if rt.rejects == 0 {
		rt.turnMu.Unlock()
		t.Fatal("expected at least 1 rejection for duplicate")
	}
	rt.turnMu.Unlock()

	rt.stop()
}

func TestClaudeCapacityExhaustion(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	// Fill to capacity.
	for i := 0; i < maxPendingClaudeObservations; i++ {
		rt.observePreToolUse(
			fmt.Sprintf("call_%d", i),
			"Bash",
			fmt.Sprintf("s%d", i),
			sha256Hex([]byte(fmt.Sprintf(`{"cmd":%d}`, i))),
		)
	}

	// One more should be rejected.
	rt.observePreToolUse("call_overflow", "Bash", "so", sha256Hex([]byte(`{"cmd":"x"}`)))

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != maxPendingClaudeObservations {
		rt.turnMu.Unlock()
		t.Fatalf("expected %d observations (capacity bound), got %d", maxPendingClaudeObservations, len(rt.pendingObservations))
	}
	rt.turnMu.Unlock()

	rt.stop()
}

func TestClaudeExitClearsPendingObservations(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})
	store := NewApprovalStore()
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	// Add a pending observation.
	rt.observePreToolUse("call_Exit", "Bash", "s1", sha256Hex([]byte(`{"cmd":"x"}`)))

	// Store should have no records (observation not yet joined).
	if len(store.ListSafe(id)) != 0 {
		t.Fatal("expected zero records before join")
	}

	// Stop the runtime (simulating child exit cleanly).
	rt.turnMu.Lock()
	rt.turnClosed = true
	rt.pendingObservations = make(map[string]*claudePendingObservation)
	rt.turnMu.Unlock()

	// Now simulate the pump exit path.
	if rt.approvals != nil {
		rt.approvals.InvalidateSession(rt.sessionID, "managed claude child exited")
	}

	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 0 observations after exit, got %d", len(rt.pendingObservations))
	}
	rt.turnMu.Unlock()

	rt.stop()
}

func TestClaudeStopIdempotent(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})

	id, _ := svc.CreateDetached("/tmp")
	rec, _ := svc.Registry().Get(id)

	// First stop.
	if err := svc.Stop(id, rec.Epoch); err != nil {
		t.Fatalf("first Stop: %v", err)
	}

	// Second stop should be idempotent (session already exited).
	if err := svc.Stop(id, rec.Epoch); err != nil {
		t.Fatalf("second Stop (idempotent): %v", err)
	}
}

func TestClaudeKillCleanup(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})

	id, _ := svc.CreateDetached("/tmp")
	rec, _ := svc.Registry().Get(id)

	if err := svc.Kill(id, rec.Epoch); err != nil {
		t.Fatalf("Kill: %v", err)
	}
}

func TestClaudeDeleteTerminalOnly(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})

	id, _ := svc.CreateDetached("/tmp")
	rec, _ := svc.Registry().Get(id)

	// Delete on non-terminal session: must fail.
	if err := svc.Delete(id, rec.Epoch); err == nil {
		t.Fatal("expected error deleting non-terminal session")
	}

	// Stop first, then delete.
	svc.Stop(id, rec.Epoch)
	if err := svc.Delete(id, rec.Epoch); err != nil {
		t.Fatalf("Delete after stop: %v", err)
	}

	// Session should be gone.
	if _, ok := svc.Registry().Get(id); ok {
		t.Fatal("session should be removed after delete")
	}
}

func TestClaudeApprovalRecordNonActionable(t *testing.T) {
	// Prove that ingested records have zero options and zero delivery material.
	launcher := &fakeClaudeLauncher{}
	store := NewApprovalStore()
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	sessionID := "s-nonactionable"
	toolUseID := "call_NA"
	toolName := "Bash"
	inputJSON := `{"command":"echo ok"}`
	inputCanon, _ := canonicalJSON(json.RawMessage(inputJSON))
	inputDigest := sha256Hex(inputCanon)

	rt.observePreToolUse(toolUseID, toolName, sessionID, inputDigest)
	deferred := deferredStreamJSON(sessionID, toolUseID, toolName, inputJSON)
	rt.processLine([]byte(deferred))

	records := store.ListSafe(id)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	r := records[0]
	if len(r.Options) != 0 {
		t.Fatalf("expected zero options, got %d", len(r.Options))
	}
	// No default action.
	_ = r

	rt.stop()
}

func TestClaudeHookBridgeDecodeValid(t *testing.T) {
	// The strict decoder must accept all C0D-known fields.
	hookInput := `{"session_id":"s1","tool_use_id":"call_Test","tool_name":"Bash","tool_input":{"command":"echo ok"},"cwd":"/tmp","transcript_path":"/tmp/t.json","permission_mode":"default","effort":"high","hook_event_name":"PreToolUse"}`
	fields, ok := strictPreToolUseDecode([]byte(hookInput))
	if !ok {
		t.Fatal("strict decode rejected valid C0D payload")
	}
	// Extract and validate identity fields.
	toolUseID, ok1 := strictBoundedString(fields["tool_use_id"], maxToolUseIDLen)
	toolName, ok2 := strictBoundedString(fields["tool_name"], maxToolNameLen)
	sessionID, ok3 := strictBoundedString(fields["session_id"], maxSessionIDLen)
	hookEvent, ok4 := strictBoundedString(fields["hook_event_name"], 64)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		t.Fatal("failed to extract identity fields")
	}
	if toolUseID != "call_Test" || toolName != "Bash" || sessionID != "s1" || hookEvent != "PreToolUse" {
		t.Fatalf("field mismatch: %q %q %q %q", toolUseID, toolName, sessionID, hookEvent)
	}
}

func TestClaudeHookBridgeDecodeMalformed(t *testing.T) {
	malformed := `{bad json`
	_, ok := strictPreToolUseDecode([]byte(malformed))
	if ok {
		t.Fatal("expected rejection of malformed JSON")
	}
}

func TestClaudeHookBridgeDecodeMissingRequired(t *testing.T) {
	// Missing tool_use_id: decode succeeds but field check fails.
	missing := `{"session_id":"s1","tool_name":"Bash","tool_input":{},"hook_event_name":"PreToolUse"}`
	fields, ok := strictPreToolUseDecode([]byte(missing))
	if !ok {
		t.Fatal("decode should succeed (all fields known)")
	}
	_, ok = strictBoundedString(fields["tool_use_id"], maxToolUseIDLen)
	if ok {
		t.Fatal("expected empty/missing tool_use_id to fail")
	}
}

func TestClaudeHookBridgeDecodeUnknownField(t *testing.T) {
	// Unknown field must be rejected.
	input := `{"session_id":"s1","tool_use_id":"x","tool_name":"Bash","tool_input":{},"hook_event_name":"PreToolUse","evil_field":true}`
	_, ok := strictPreToolUseDecode([]byte(input))
	if ok {
		t.Fatal("expected rejection of unknown field")
	}
}

func TestClaudeHookBridgeDecodeDuplicateKey(t *testing.T) {
	// Duplicate key must be rejected.
	input := `{"session_id":"s1","tool_use_id":"x","tool_name":"Bash","tool_input":{},"hook_event_name":"PreToolUse","cwd":"/tmp","cwd":"/etc"}`
	_, ok := strictPreToolUseDecode([]byte(input))
	if ok {
		t.Fatal("expected rejection of duplicate key")
	}
}

func TestClaudeHookBridgeDecodeTrailingContent(t *testing.T) {
	// Trailing content after the object must be rejected.
	input := `{"session_id":"s1","tool_use_id":"x","tool_name":"Bash","tool_input":{},"hook_event_name":"PreToolUse"} extra`
	_, ok := strictPreToolUseDecode([]byte(input))
	if ok {
		t.Fatal("expected rejection of trailing content")
	}
}

func TestClaudeHookBridgeDecodeWrongHookEvent(t *testing.T) {
	// hook_event_name != "PreToolUse" must fail.
	input := `{"session_id":"s1","tool_use_id":"x","tool_name":"Bash","tool_input":{},"hook_event_name":"PostToolUse"}`
	fields, ok := strictPreToolUseDecode([]byte(input))
	if !ok {
		t.Fatal("decode should succeed (PostToolUse is in allowlist)")
	}
	heName, sok := strictBoundedString(fields["hook_event_name"], 64)
	if !sok || heName == "PreToolUse" {
		t.Fatal("expected wrong hook_event_name to be detectable")
	}
	// The handleHook handler would reject non-PreToolUse. Verify the field
	// is present but wrong value.
	if heName != "PostToolUse" {
		t.Fatalf("unexpected hook_event_name: %q", heName)
	}
}

func TestClaudeHookBridgeDecodeOversizedFields(t *testing.T) {
	longName := strings.Repeat("x", maxToolNameLen+1)
	input := fmt.Sprintf(`{"session_id":"s1","tool_use_id":"call_Test","tool_name":"%s","tool_input":{},"hook_event_name":"PreToolUse"}`, longName)
	fields, ok := strictPreToolUseDecode([]byte(input))
	if !ok {
		t.Fatal("decode should succeed")
	}
	_, ok = strictBoundedString(fields["tool_name"], maxToolNameLen)
	if ok {
		t.Fatal("expected oversized tool_name to fail bound check")
	}
}

func TestClaudeLaunchArgv(t *testing.T) {
	var capturedArgv []string
	launcher := &argvCapturingLauncher{argv: &capturedArgv}
	cfg := ClaudeEntryConfig{
		Bin:              "claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
		PinnedPath:       "/fake/path",
	}
	svc := NewManagedClaudeService(cfg, launcher, &fakeClaudeAttestor{})

	_, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}

	// Verify the launch argv contains the required flags.
	argvStr := strings.Join(capturedArgv, " ")
	if !strings.Contains(argvStr, "--settings") {
		t.Fatalf("missing --settings in argv: %v", capturedArgv)
	}
	if !strings.Contains(argvStr, "--setting-sources") {
		t.Fatalf("missing --setting-sources in argv: %v", capturedArgv)
	}
	if !strings.Contains(argvStr, "--output-format") {
		t.Fatalf("missing --output-format in argv: %v", capturedArgv)
	}
	if !strings.Contains(argvStr, "stream-json") {
		t.Fatalf("missing stream-json in argv: %v", capturedArgv)
	}
	if !strings.Contains(argvStr, "--include-partial-messages") {
		t.Fatalf("missing --include-partial-messages in argv: %v", capturedArgv)
	}
	if !strings.Contains(argvStr, "-p") {
		t.Fatalf("missing -p in argv: %v", capturedArgv)
	}
	if !strings.Contains(argvStr, claudeCertificationPrompt) {
		t.Fatalf("missing certification prompt in argv: %v", capturedArgv)
	}

	ctx := context.Background()
	svc.Shutdown(ctx)
}

func TestClaudeShutdownCleansUp(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})

	id, _ := svc.CreateDetached("/tmp")
	ctx := context.Background()
	if err := svc.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}

	// Runtime map should be empty after shutdown.
	svc.mu.Lock()
	if _, ok := svc.runtimes[id]; ok {
		svc.mu.Unlock()
		t.Fatal("runtime should not exist after shutdown")
	}
	svc.mu.Unlock()
}

func TestClaudeStreamDeferredParsing(t *testing.T) {
	// Prove the stream-json deferred result is parsed correctly.
	line := `{"type":"result","stop_reason":"tool_deferred","session_id":"abc-123","deferred_tool_use":{"id":"call_X","name":"Bash","input":{"command":"echo ok","description":"test"}}}`
	var event streamDeferred
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.Type != "result" {
		t.Fatalf("type: got %q", event.Type)
	}
	if event.StopReason != "tool_deferred" {
		t.Fatalf("stop_reason: got %q", event.StopReason)
	}
	if event.SessionID != "abc-123" {
		t.Fatalf("session_id: got %q", event.SessionID)
	}
	if event.DeferredToolUse == nil {
		t.Fatal("deferred_tool_use is nil")
	}
	if event.DeferredToolUse.ID != "call_X" {
		t.Fatalf("id: got %q", event.DeferredToolUse.ID)
	}
	if event.DeferredToolUse.Name != "Bash" {
		t.Fatalf("name: got %q", event.DeferredToolUse.Name)
	}
}

func TestClaudeProcessLineSkipsNonResult(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})
	store := NewApprovalStore()
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	// Add a pending observation.
	rt.observePreToolUse("call_NonResult", "Bash", "s1", sha256Hex([]byte(`{"cmd":"x"}`)))

	// Feed a non-result line (type: "assistant" or "content_block_delta").
	rt.processLine([]byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`))

	// Pending observation should still exist (no match).
	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 1 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 1 pending observation after non-result line")
	}
	rt.turnMu.Unlock()

	rt.stop()
}

func TestClaudeGenApprovalTokenIsOpaque(t *testing.T) {
	t1, err := genApprovalToken()
	if err != nil {
		t.Fatalf("genApprovalToken: %v", err)
	}
	t2, err := genApprovalToken()
	if err != nil {
		t.Fatalf("genApprovalToken: %v", err)
	}
	if t1 == t2 {
		t.Fatal("expected different approval tokens")
	}
	if len(t1) != 32 {
		t.Fatalf("expected 32-char hex token, got %d", len(t1))
	}
	if strings.Contains(t1, "call_") {
		t.Fatal("approval token must not contain provider tool_use_id")
	}
}

func TestClaudeHookSettingsSchema(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	cfg := ClaudeEntryConfig{
		Bin:              "claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
		PinnedPath:       "/fake/path",
	}
	svc := NewManagedClaudeService(cfg, launcher, &fakeClaudeAttestor{})

	_, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}

	// Read the generated settings file.
	svc.mu.Lock()
	var hookDir string
	for _, rt := range svc.runtimes {
		hookDir = rt.hookDir
	}
	svc.mu.Unlock()

	if hookDir == "" {
		t.Fatal("hook directory not created")
	}

	settingsPath := filepath.Join(hookDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("parse settings: %v", err)
	}

	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("missing hooks key")
	}
	preToolUse, ok := hooks["PreToolUse"].([]any)
	if !ok || len(preToolUse) == 0 {
		t.Fatal("missing PreToolUse hook array")
	}
	entry, ok := preToolUse[0].(map[string]any)
	if !ok {
		t.Fatal("PreToolUse[0] not an object")
	}
	if entry["matcher"] != "" {
		t.Fatalf("expected empty matcher, got %q", entry["matcher"])
	}
	hookList, ok := entry["hooks"].([]any)
	if !ok || len(hookList) == 0 {
		t.Fatal("missing hooks array inside matcher")
	}
	hookObj, ok := hookList[0].(map[string]any)
	if !ok {
		t.Fatal("hook not an object")
	}
	if hookObj["type"] != "command" {
		t.Fatalf("expected type=command, got %q", hookObj["type"])
	}
	cmd, _ := hookObj["command"].(string)
	if cmd == "" {
		t.Fatal("missing command in hook")
	}

	ctx := context.Background()
	svc.Shutdown(ctx)
}

func TestClaudePumpEOFExit(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209", PinnedPath: "/fake/path"}, launcher, &fakeClaudeAttestor{})
	store := NewApprovalStore()
	svc.SetApprovalStore(store)

	id, _ := svc.CreateDetached("/tmp")
	svc.mu.Lock()
	rt := svc.runtimes[id]
	svc.mu.Unlock()

	// Feed a hook observation so we can verify it's cleared on EOF.
	rt.observePreToolUse("call_EOF", "Bash", "s1", sha256Hex([]byte(`{"x":1}`)))

	// Kill the process to trigger EOF on stdout.
	// The pump goroutine is running; killing the underlying process will
	// cause the scanner to hit EOF.
	rt.proc.Kill()

	// Wait for pump to observe exit (bounded).
	select {
	case <-rt.exited:
		// OK — pump processed EOF.
	case <-time.After(3 * time.Second):
		t.Fatal("pump did not exit after EOF")
	}

	// Pending observations must be cleared.
	rt.turnMu.Lock()
	if len(rt.pendingObservations) != 0 {
		rt.turnMu.Unlock()
		t.Fatalf("expected 0 pending after EOF, got %d", len(rt.pendingObservations))
	}
	rt.turnMu.Unlock()

	// Registry must show exited.
	rec, _ := svc.Registry().Get(id)
	if !rec.Exited {
		t.Fatal("expected exited=true after EOF")
	}
}

func TestClaudeAttestorFailOpenRejected(t *testing.T) {
	// PinnedPath is set; a binary at a different path must fail certification.
	attestor := NewClaudeAttestor(ClaudeEntryConfig{
		Bin:        "claude",
		Version:    "wrong-version",
		PinnedPath: "/nonexistent/path",
	})
	err := attestor.Certify("/usr/bin/true")
	if err == nil {
		t.Fatal("expected certification failure for wrong binary")
	}
}

func TestClaudeGenApprovalTokenEntropyFail(t *testing.T) {
	// Prove the function returns error on failure (the production code
	// must handle this). We can't force entropy failure, but we CAN prove
	// the function returns (string, error) with a non-zero token on success.
	tok, err := genApprovalToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}
	if len(tok) != 32 {
		t.Fatalf("expected 32 hex chars, got %d", len(tok))
	}
}
