package writer

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/timeline/contract"
)

// ── testFile (rich fake for failure injection) ──

type testFile struct {
	mu       sync.Mutex
	contents string
	writeErr error
	syncErr  error
	shortN   *int
	syncs    int
	closed   bool
}

func (f *testFile) Write(record []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return 0, osErrClosed
	}
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	n := len(record)
	if f.shortN != nil {
		n = *f.shortN
	}
	f.contents += string(record[:n])
	return n, nil
}

func (f *testFile) Sync() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.syncs++
	return f.syncErr
}

func (f *testFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

type blockingFile struct {
	started   chan struct{}
	release   chan struct{}
	startOnce sync.Once
	mu        sync.Mutex
	writes    int
	closes    int
}

func newBlockingFile() *blockingFile {
	return &blockingFile{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (f *blockingFile) Write(record []byte) (int, error) {
	f.mu.Lock()
	f.writes++
	f.mu.Unlock()
	f.startOnce.Do(func() { close(f.started) })
	<-f.release
	return len(record), nil
}

func (f *blockingFile) Sync() error { return nil }

func (f *blockingFile) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	return nil
}

func (f *blockingFile) counts() (writes, closes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes, f.closes
}

var osErrClosed = errors.New("file already closed")

func makeEnv() contract.Envelope {
	scope := contract.Scope{SessionID: "controlled_pty:one", RuntimeID: "runtime-1", LaunchGeneration: 3}
	e, _ := contract.NewEnvelope(contract.Envelope{
		SchemaVersion: contract.SchemaV1, PayloadVersion: contract.PayloadV1,
		EventKind: contract.EventToolCallStarted,
		SessionID: scope.SessionID, RuntimeID: scope.RuntimeID, LaunchGeneration: scope.LaunchGeneration,
		Provider: "codex", SourceIncarnation: "run-1",
		SourceIdentity: contract.SourceIdentity{Kind: "provider", ID: "session-1"},
		SourcePosition: "position-1",
		OccurredAt:     time.Unix(1, 0).UTC(), ObservedAt: time.Unix(2, 0).UTC(),
		RedactionPolicyVersion: "redaction-v1",
		Payload:                contract.Payload{Redacted: &contract.RedactedPayload{Summary: "safe summary"}},
		EvidenceSources:        contract.EvidenceSources{Provider: &contract.ProviderEvidenceRef{ID: "provider-evidence-1", Scope: scope}},
		References:             contract.References{ToolCall: &contract.TypedReference{Kind: contract.ReferenceToolCall, ID: "tool-1", Scope: scope}},
		T0Event:                agent.AgentEvent{ID: "t0-1", SessionID: scope.SessionID, AgentKind: "codex", Type: agent.EventToolCallStarted},
	})
	return e
}

func boundEnv(t *testing.T, provider, sessionID, runtimeID string, generation int64, kind contract.EventKind) contract.Envelope {
	t.Helper()
	e := makeEnv()
	e.EventID = ""
	e.Provider, e.SessionID, e.RuntimeID, e.LaunchGeneration, e.EventKind = provider, sessionID, runtimeID, generation, kind
	e.T0Event.AgentKind = provider
	if kind == contract.EventToolCallFinished {
		e.T0Event.Type = agent.EventToolCallFinished
	}
	e.T0Event.SessionID = sessionID
	e.EvidenceSources.Provider.Scope = contract.Scope{SessionID: sessionID, RuntimeID: runtimeID, LaunchGeneration: generation}
	e.References.ToolCall.Scope = contract.Scope{SessionID: sessionID, RuntimeID: runtimeID, LaunchGeneration: generation}
	bound, err := contract.NewEnvelope(e)
	if err != nil {
		t.Fatal(err)
	}
	return bound
}

func waitForAppended(t *testing.T, w *Writer, want uint64) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for w.Stats().Appended < want && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if got := w.Stats().Appended; got != want {
		t.Fatalf("appended = %d, want %d", got, want)
	}
}

// ── Original Append/write tests (restored from pre-observer baseline) ──

func TestAppendSuccessFramesOneCanonicalEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline-shadow.jsonl")
	w, err := Open(Config{Path: path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := makeEnv()
	if !w.Append(e) {
		t.Fatal("Append returned false")
	}
	w.Close()
	record, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(record), `"eventId":"`+e.EventID+`"`) || !strings.HasSuffix(string(record), "\n") {
		t.Fatalf("record = %q", record)
	}
	if got := w.Stats(); got != (Stats{Appended: 1}) {
		t.Fatalf("stats = %+v", got)
	}
}

func TestAppendFailureIsIsolated(t *testing.T) {
	for name, writeErr := range map[string]error{
		"disk_full":         syscall.ENOSPC,
		"permission_denied": syscall.EACCES,
	} {
		t.Run(name, func(t *testing.T) {
			w := newWriter(&testFile{writeErr: writeErr}, Config{}, nil)
			if w.Append(makeEnv()) {
				t.Fatal("Append succeeded after write failure")
			}
			if got := w.Stats(); got != (Stats{Dropped: 1, Failures: 1}) {
				t.Fatalf("stats = %+v", got)
			}
		})
	}
}

func TestShortWriteIsDroppedBeforeSync(t *testing.T) {
	shortN := 1
	f := &testFile{shortN: &shortN}
	w := newWriter(f, Config{}, nil)
	if w.Append(makeEnv()) {
		t.Fatal("Append accepted a short write")
	}
	if got := w.Stats(); got != (Stats{Dropped: 1, Failures: 1}) {
		t.Fatalf("stats = %+v", got)
	}
	if f.syncs != 0 {
		t.Fatalf("Sync calls after short write = %d, want 0", f.syncs)
	}
}

func TestAppendAfterAbruptWriterDeathDropsWithoutPanic(t *testing.T) {
	f := &testFile{}
	w := newWriter(f, Config{}, nil)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if w.Append(makeEnv()) {
		t.Fatal("Append succeeded after abrupt file loss")
	}
	if got := w.Stats(); got != (Stats{Dropped: 1, Failures: 1}) {
		t.Fatalf("stats = %+v", got)
	}
}

func TestConcurrentAppendIsRaceFree(t *testing.T) {
	f := &testFile{}
	w := newWriter(f, Config{}, nil)
	e := makeEnv()
	const writers = 64
	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !w.Append(e) {
				t.Error("concurrent Append returned false")
			}
		}()
	}
	wg.Wait()
	if got := w.Stats(); got != (Stats{Appended: writers}) {
		t.Fatalf("stats = %+v", got)
	}
}

func TestWriterStartsZeroGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	w := newWriter(&testFile{}, Config{}, nil)
	if after := runtime.NumGoroutine(); after != before+1 {
		t.Fatalf("goroutines: before=%d after=%d", before, after)
	}
	w.Close()
}

func TestOpenRejectsImplicitOrRelativePath(t *testing.T) {
	for _, path := range []string{"", "timeline.jsonl"} {
		if _, err := Open(Config{Path: path}, nil); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("Open(%q) error = %v", path, err)
		}
	}
}

// ── Ring-buffer tests ──

func TestReadRecentEmptyReturnsNil(t *testing.T) {
	w := newWriter(&testFile{}, Config{}, nil)
	defer w.Close()
	if got := w.ReadRecent(10); got != nil {
		t.Fatalf("expected nil, got %d", len(got))
	}
	if got := w.ReadRecent(0); got != nil {
		t.Fatal("expected nil for n=0")
	}
}

func TestReadRecentReturnsMostRecent(t *testing.T) {
	w := newWriter(&testFile{}, Config{}, nil)
	defer w.Close()
	for range 5 {
		w.Append(makeEnv())
	}
	recent := w.ReadRecent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3, got %d", len(recent))
	}
}

func TestReadRecentClampedToBuffer(t *testing.T) {
	w := newWriter(&testFile{}, Config{}, nil)
	defer w.Close()
	for range 200 {
		w.Append(makeEnv())
	}
	recent := w.ReadRecent(500)
	if len(recent) != recentEnvelopes {
		t.Fatalf("expected %d, got %d", recentEnvelopes, len(recent))
	}
}

func TestCloseIdempotent(t *testing.T) {
	w := newWriter(&testFile{}, Config{}, nil)
	w.Close()
	w.Close()
}

func TestCapabilityBindsWriterAndCompleteIdentity(t *testing.T) {
	store := NewProducerStore()
	w := newWriter(&testFile{}, Config{}, store)
	capability, err := store.Bind("codex", "runtime-a", "codex_app_server:one", 7, contract.EventToolCallStarted)
	if err != nil {
		t.Fatal(err)
	}
	e := boundEnv(t, "codex", "codex_app_server:one", "runtime-a", 7, contract.EventToolCallStarted)
	if !capability.SubmitAfterCommit(e) {
		t.Fatal("bound submit rejected")
	}
	waitForAppended(t, w, 1)
	wrongRuntime := boundEnv(t, "codex", "codex_app_server:one", "runtime-b", 7, contract.EventToolCallStarted)
	if capability.SubmitAfterCommit(wrongRuntime) {
		t.Fatal("runtime mismatch accepted")
	}
	wrongKind := boundEnv(t, "codex", "codex_app_server:one", "runtime-a", 7, contract.EventToolCallFinished)
	if capability.SubmitAfterCommit(wrongKind) {
		t.Fatal("unbound event kind accepted")
	}
	store.Revoke("codex", "codex_app_server:one", 7)
	if capability.SubmitAfterCommit(e) {
		t.Fatal("revoked capability accepted")
	}
	if result := w.Close(); !result.WorkerExited {
		t.Fatalf("close = %+v", result)
	}
	if got := w.Stats(); got.Appended != 1 || got.Dropped < 3 {
		t.Fatalf("stats = %+v", got)
	}
}

func TestCapabilityRejectsArbitraryProviderAndConcurrentSubmitClose(t *testing.T) {
	store := NewProducerStore()
	w := newWriter(&testFile{}, Config{}, store)
	if _, err := store.Bind("generic", "r", "generic:x", 1, contract.EventToolCallStarted); err == nil {
		t.Fatal("arbitrary provider bound")
	}
	capability, err := store.Bind("claude", "runtime-a", "claude_headless:one", 1, contract.EventToolCallStarted)
	if err != nil {
		t.Fatal(err)
	}
	e := boundEnv(t, "claude", "claude_headless:one", "runtime-a", 1, contract.EventToolCallStarted)
	var wg sync.WaitGroup
	for range 64 {
		wg.Add(1)
		go func() { defer wg.Done(); capability.SubmitAfterCommit(e) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); w.Close() }()
	wg.Wait()
	_ = w.Close()
}

func TestCloseAtomicallyDropsPendingAndLetsOnlyInFlightFinish(t *testing.T) {
	store := NewProducerStore()
	file := newBlockingFile()
	w := newWriter(file, Config{}, store)
	w.closeWait = 20 * time.Millisecond
	capability, err := store.Bind("codex", "runtime-a", "codex_app_server:one", 7, contract.EventToolCallStarted)
	if err != nil {
		t.Fatal(err)
	}
	e := boundEnv(t, "codex", "codex_app_server:one", "runtime-a", 7, contract.EventToolCallStarted)
	if !capability.SubmitAfterCommit(e) {
		t.Fatal("in-flight submit rejected")
	}
	select {
	case <-file.started:
	case <-time.After(time.Second):
		t.Fatal("worker did not enter Write")
	}
	for range 3 {
		if !capability.SubmitAfterCommit(e) {
			t.Fatal("pending submit rejected")
		}
	}

	result := w.Close()
	if result.WorkerExited || result.InFlight != 1 || result.PendingDropped != 3 {
		t.Fatalf("close = %+v", result)
	}
	if got := w.Stats().Dropped; got != 3 {
		t.Fatalf("dropped before release = %d, want 3", got)
	}
	if capability.SubmitAfterCommit(e) {
		t.Fatal("post-close submit accepted")
	}

	close(file.release)
	select {
	case <-w.workerDone:
	case <-time.After(time.Second):
		t.Fatal("worker did not exit after blocked write completed")
	}
	writes, closes := file.counts()
	if writes != 1 || closes != 1 {
		t.Fatalf("file writes=%d closes=%d, want 1/1", writes, closes)
	}
	if got := w.Stats(); got.Appended != 1 || got.Dropped != 4 {
		t.Fatalf("final stats = %+v", got)
	}
	if repeat := w.Close(); repeat != result {
		t.Fatalf("idempotent close = %+v, want %+v", repeat, result)
	}
}
