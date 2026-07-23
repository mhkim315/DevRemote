package writer

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/timeline/contract"
)

type testFile struct{}

func (f *testFile) Write(p []byte) (int, error) { return len(p), nil }
func (f *testFile) Sync() error                 { return nil }
func (f *testFile) Close() error                { return nil }

func makeEnv() contract.Envelope {
	scope := contract.Scope{SessionID: "s", RuntimeID: "r", LaunchGeneration: 1}
	ts := time.Now()
	e, _ := contract.NewEnvelope(contract.Envelope{
		SchemaVersion: 1, PayloadVersion: 1,
		EventKind: contract.EventEvidenceObserved,
		SessionID: scope.SessionID, RuntimeID: scope.RuntimeID, LaunchGeneration: scope.LaunchGeneration,
		Provider: "test", SourceIncarnation: "inc",
		SourceIdentity:         contract.SourceIdentity{Kind: "test", ID: "id"},
		SourcePosition:         "pos",
		RedactionPolicyVersion: "v1",
		Payload:                contract.Payload{Redacted: &contract.RedactedPayload{Summary: "safe"}},
		EvidenceSources:        contract.EvidenceSources{Provider: &contract.ProviderEvidenceRef{ID: "e1", Scope: scope}},
		OccurredAt:             ts, ObservedAt: ts.Add(time.Second),
		References: contract.References{Provenance: &contract.TypedReference{Kind: contract.ReferenceProvenance, ID: "prov", Scope: scope}},
		T0Event:    agent.AgentEvent{ID: "ev", SessionID: scope.SessionID, AgentKind: "test", Type: agent.EventUnknown},
	})
	return e
}

func TestWriterStartsZeroGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	w := newWriter(&testFile{})
	if after := runtime.NumGoroutine(); after != before {
		t.Fatalf("goroutines: before=%d after=%d", before, after)
	}
	w.Close()
}

func TestReadRecentEmptyReturnsNil(t *testing.T) {
	w := newWriter(&testFile{})
	defer w.Close()
	if got := w.ReadRecent(10); got != nil {
		t.Fatalf("expected nil, got %d", len(got))
	}
	if got := w.ReadRecent(0); got != nil {
		t.Fatal("expected nil for n=0")
	}
}

func TestReadRecentReturnsMostRecent(t *testing.T) {
	w := newWriter(&testFile{})
	defer w.Close()
	for range 5 {
		_ = w.Append(makeEnv())
	}
	recent := w.ReadRecent(3)
	if len(recent) != 3 {
		t.Fatalf("expected 3, got %d", len(recent))
	}
}

func TestReadRecentClampedToBuffer(t *testing.T) {
	w := newWriter(&testFile{})
	defer w.Close()
	for range 200 {
		_ = w.Append(makeEnv())
	}
	recent := w.ReadRecent(500)
	if len(recent) != recentEnvelopes {
		t.Fatalf("expected %d, got %d", recentEnvelopes, len(recent))
	}
}

func TestConcurrentAppendAndRead(t *testing.T) {
	w := newWriter(&testFile{})
	defer w.Close()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			_ = w.Append(makeEnv())
		}
	}()
	for range 10 {
		_ = w.ReadRecent(5)
	}
	wg.Wait()
}

func TestCloseIdempotent(t *testing.T) {
	w := newWriter(&testFile{})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}
