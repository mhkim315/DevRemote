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

var osErrClosed = errors.New("file already closed")

func validEnvelope(t *testing.T) contract.Envelope {
	t.Helper()
	scope := contract.Scope{SessionID: "controlled_pty:one", RuntimeID: "runtime-1", LaunchGeneration: 3}
	e, err := contract.NewEnvelope(contract.Envelope{
		SchemaVersion: contract.SchemaV1, PayloadVersion: contract.PayloadV1,
		EventKind: contract.EventToolCallStarted,
		SessionID: scope.SessionID, RuntimeID: scope.RuntimeID, LaunchGeneration: scope.LaunchGeneration,
		Provider: "codex", SourceIncarnation: "run-1",
		SourceIdentity: contract.SourceIdentity{Kind: "provider", ID: "session-1"}, SourcePosition: "position-1",
		OccurredAt: time.Unix(1, 0).UTC(), ObservedAt: time.Unix(2, 0).UTC(),
		RedactionPolicyVersion: "redaction-v1",
		Payload:                contract.Payload{Redacted: &contract.RedactedPayload{Summary: "safe summary"}},
		EvidenceSources:        contract.EvidenceSources{Provider: &contract.ProviderEvidenceRef{ID: "provider-evidence-1", Scope: scope}},
		References:             contract.References{ToolCall: &contract.TypedReference{Kind: contract.ReferenceToolCall, ID: "tool-1", Scope: scope}},
		T0Event:                agent.AgentEvent{ID: "t0-1", SessionID: scope.SessionID, AgentKind: "codex", Type: agent.EventToolCallStarted},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAppendSuccessFramesOneCanonicalEnvelope(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline-shadow.jsonl")
	w, err := Open(Config{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	e := validEnvelope(t)
	if !w.Append(e) {
		t.Fatal("Append returned false")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
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
			w := newWriter(&testFile{writeErr: writeErr})
			if w.Append(validEnvelope(t)) {
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
	w := newWriter(f)
	if w.Append(validEnvelope(t)) {
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
	w := newWriter(f)
	// This models a kill -9/restart boundary where the owned descriptor becomes
	// unusable without the writer reaching its graceful Close transition. The
	// record is dropped rather than retried through any authority path.
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if w.Append(validEnvelope(t)) {
		t.Fatal("Append succeeded after abrupt file loss")
	}
	if got := w.Stats(); got != (Stats{Dropped: 1, Failures: 1}) {
		t.Fatalf("stats = %+v", got)
	}
}

func TestConcurrentAppendIsRaceFree(t *testing.T) {
	f := &testFile{}
	w := newWriter(f)
	e := validEnvelope(t)
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

func TestWriterStartsSingleBoundedGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()
	w := newWriter(&testFile{})
	// STEP8: one bounded subscriber dispatch goroutine (replaces per-Append goroutine leak).
	if after := runtime.NumGoroutine(); after != before+1 {
		t.Fatalf("writer goroutine count: before=%d after=%d (want +1 for bounded subscriber loop)", before, after)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSubscribeReceivesSuccessfulAppend(t *testing.T) {
	w := newWriter(&testFile{})
	first, second := make(chan contract.Envelope, 1), make(chan contract.Envelope, 1)
	w.Subscribe(func(e contract.Envelope) { first <- e })
	w.Subscribe(func(e contract.Envelope) { second <- e })
	e := validEnvelope(t)
	if !w.Append(e) {
		t.Fatal("append")
	}
	for _, received := range []<-chan contract.Envelope{first, second} {
		select {
		case got := <-received:
			if got.EventID != e.EventID {
				t.Fatal("wrong envelope")
			}
		case <-time.After(time.Second):
			t.Fatal("subscriber not notified")
		}
	}
}

func TestOpenRejectsImplicitOrRelativePath(t *testing.T) {
	for _, path := range []string{"", "timeline.jsonl"} {
		if _, err := Open(Config{Path: path}); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("Open(%q) error = %v", path, err)
		}
	}
}
