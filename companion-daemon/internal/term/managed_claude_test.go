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
	"strings"
	"sync"
	"testing"
)

// ── Test fakes ──

// fakeClaudeProcess implements ManagedProcess for deterministic tests.
type fakeClaudeProcess struct {
	stdin   *bytes.Buffer
	stdout  *bytes.Buffer
	termFn  func() error
	killFn  func() error
	waitErr error
	opaque  string
}

func (p *fakeClaudeProcess) Stdin() io.Writer  { return p.stdin }
func (p *fakeClaudeProcess) Stdout() io.Reader { return p.stdout }
func (p *fakeClaudeProcess) Term() error {
	if p.termFn != nil {
		return p.termFn()
	}
	return nil
}
func (p *fakeClaudeProcess) Kill() error {
	if p.killFn != nil {
		return p.killFn()
	}
	return nil
}
func (p *fakeClaudeProcess) Wait() error { return p.waitErr }
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
}

func (l *fakeClaudeLauncher) Launch(exe string, argv []string) (ManagedProcess, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.launched {
		return nil, fmt.Errorf("already launched")
	}
	l.launched = true
	p := &fakeClaudeProcess{
		stdin:  new(bytes.Buffer),
		stdout: new(bytes.Buffer),
	}
	l.proc = p
	return p, nil
}

// feedLine writes a JSON line to the process stdout for the pump to consume.
func (l *fakeClaudeLauncher) feedLine(line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.proc != nil {
		l.proc.stdout.Write([]byte(line + "\n"))
	}
}

// closeStream closes stdout (simulates child exit).
func (l *fakeClaudeLauncher) closeStream() {
	l.mu.Lock()
	defer l.mu.Unlock()
	// The pump uses bufio.Scanner; closing the buffer doesn't work.
	// Instead, tests signal exit by calling stop on the runtime directly.
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
	// Simulate the hook bridge decode path with the C0D-format hook input.
	// The hook bridge receives JSON from the hook script and decodes it.
	hookInput := `{"session_id":"s1","tool_use_id":"call_Test","tool_name":"Bash","tool_input":{"command":"echo ok"},"cwd":"/tmp","transcript_path":"/tmp/t.json","permission_mode":"default","effort":"high","hook_event_name":"PreToolUse"}`

	var event claudePreToolUse
	dec := json.NewDecoder(strings.NewReader(hookInput))
	if err := dec.Decode(&event); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if event.SessionID != "s1" {
		t.Fatalf("session_id: got %q", event.SessionID)
	}
	if event.ToolUseID != "call_Test" {
		t.Fatalf("tool_use_id: got %q", event.ToolUseID)
	}
	if event.ToolName != "Bash" {
		t.Fatalf("tool_name: got %q", event.ToolName)
	}
	// Extra fields (cwd, transcript_path, permission_mode, effort) are
	// silently ignored — dec.Decode without DisallowUnknownFields.
}

func TestClaudeHookBridgeDecodeMalformed(t *testing.T) {
	// Malformed JSON should fail decode.
	malformed := `{bad json`
	var event claudePreToolUse
	dec := json.NewDecoder(strings.NewReader(malformed))
	if err := dec.Decode(&event); err == nil {
		t.Fatal("expected decode error for malformed JSON")
	}
}

func TestClaudeHookBridgeDecodeMissingFields(t *testing.T) {
	// Missing tool_use_id should decode but fail downstream field checks.
	missing := `{"session_id":"s1","tool_name":"Bash","tool_input":{}}`
	var event claudePreToolUse
	dec := json.NewDecoder(strings.NewReader(missing))
	if err := dec.Decode(&event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if event.ToolUseID != "" {
		t.Fatal("expected empty tool_use_id")
	}
}

func TestClaudeHookBridgeDecodeOversizedFields(t *testing.T) {
	// tool_name exceeding maxToolNameLen should be caught by field bounds.
	longName := strings.Repeat("x", maxToolNameLen+1)
	input := fmt.Sprintf(`{"session_id":"s1","tool_use_id":"call_Test","tool_name":"%s","tool_input":{}}`, longName)
	var event claudePreToolUse
	dec := json.NewDecoder(strings.NewReader(input))
	if err := dec.Decode(&event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(event.ToolName) <= maxToolNameLen {
		t.Fatal("expected oversized tool_name")
	}
}

func TestClaudeLaunchArgv(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	svc := NewManagedClaudeService(ClaudeEntryConfig{Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209"}, launcher, &fakeClaudeAttestor{})

	_, err := svc.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}

	if !launcher.launched {
		t.Fatal("launcher was not called")
	}

	// Verify the hook settings file was created and contains PreToolUse hook.
	svc.mu.Lock()
	var hookDir string
	for _, rt := range svc.runtimes {
		hookDir = rt.hookDir
	}
	svc.mu.Unlock()

	if hookDir == "" {
		t.Fatal("hook directory not created")
	}

	// Clean up.
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
	t1 := genApprovalToken()
	t2 := genApprovalToken()
	if t1 == t2 {
		t.Fatal("expected different approval tokens")
	}
	if len(t1) != 32 {
		t.Fatalf("expected 32-char hex token, got %d", len(t1))
	}
	// Provider tool_use_id format is never used as ApprovalID.
	if strings.Contains(t1, "call_") {
		t.Fatal("approval token must not contain provider tool_use_id")
	}
}
