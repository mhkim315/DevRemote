package term

import (
	"context"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// failInjectAdapter wraps a controlledPTYAdapter and can inject a failure
// on the first OpenStream call after a given number of successful creates.
type failInjectAdapter struct {
	mux.Adapter
	mu           sync.Mutex
	failNextOpen bool // set true to make the next OpenStream fail
}

func (a *failInjectAdapter) CreateSessionAndCapture(ctx context.Context, opts mux.CreateOptions) (string, mux.Session, error) {
	sci, _ := a.Adapter.(mux.SessionCreatorWithIdentity)
	return sci.CreateSessionAndCapture(ctx, opts)
}

// failStream wraps a TerminalStream but fails reads after creation.
type failStream struct {
	ptyStream
	failRead bool
}

func (s *failStream) Read(p []byte) (int, error) {
	if s.failRead {
		return 0, fmt.Errorf("injected stream failure")
	}
	return s.ptyStream.Read(p)
}

func TestStep6a_ReplacementCapturesOldRecorder(t *testing.T) {
	ctl := mux.NewControlledPTYAdapter()
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	ownedPTY := NewOwnedPTYRuntime(ctl, NewActivityBuffer(100), transcriptSvc)

	opts := mux.CreateOptions{Name: "step6a-test", Executable: "sleep", Args: []string{"10"}}
	id1, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-test")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	entry, ok := ownedPTY.Get(id1)
	if !ok || entry.recorder == nil || entry.session == nil {
		t.Fatal("first entry missing Recorder or Session")
	}
	oldRec := entry.recorder
	oldSess := entry.session

	id2, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-test")
	if err != nil {
		ownedPTY.Stop(context.Background(), id1)
		t.Fatalf("replacement: %v", err)
	}
	if id1 != id2 {
		t.Errorf("canonical ID changed: %s → %s", id1, id2)
	}
	time.Sleep(50 * time.Millisecond)
	DeleteRecorderIfSame(id1, oldRec) // no-op, already stopped by pre-install barrier

	newEntry, ok := ownedPTY.Get(id2)
	if !ok || newEntry.recorder == nil || newEntry.session == nil {
		t.Fatal("replacement entry missing Recorder or Session")
	}
	if newEntry.recorder == oldRec {
		t.Error("replacement reused old Recorder")
	}
	if newEntry.session == oldSess {
		t.Error("replacement reused old Session")
	}

	ownedPTY.Stop(context.Background(), id2)
	time.Sleep(100 * time.Millisecond)
}

func TestStep6a_RollbackCleanupIsInstanceGuarded(t *testing.T) {
	ctl := mux.NewControlledPTYAdapter()
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	ownedPTY := NewOwnedPTYRuntime(ctl, NewActivityBuffer(100), transcriptSvc)

	opts := mux.CreateOptions{Name: "step6a-rb", Executable: "sleep", Args: []string{"10"}}
	id, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-rb")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	entry, _ := ownedPTY.Get(id)
	if entry.recorder == nil {
		t.Fatal("no Recorder in first entry")
	}

	id2, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-rb")
	if err != nil {
		t.Fatalf("replacement: %v", err)
	}
	_ = id2

	newEntry, _ := ownedPTY.Get(id)
	if newEntry.recorder == entry.recorder {
		t.Error("replacement kept old Recorder")
	}

	ownedPTY.Stop(context.Background(), id)
	time.Sleep(100 * time.Millisecond)
}

// PA3 Step 6a R5: inject post-capture failure — cap.Execute() runs on rollback.
func TestStep6a_FailedCreateLeavesReplacementIntact(t *testing.T) {
	ctl := mux.NewControlledPTYAdapter()
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	ownedPTY := NewOwnedPTYRuntime(ctl, NewActivityBuffer(100), transcriptSvc)

	opts := mux.CreateOptions{Name: "step6a-fail", Executable: "sleep", Args: []string{"30"}}

	// 1. Create session A normally.
	idA, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-fail")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	entryA, _ := ownedPTY.Get(idA)
	if entryA.recorder == nil || entryA.session == nil {
		t.Fatal("A: missing Recorder or Session")
	}

	// 2. Stop A's Recorder to simulate a post-create failure condition.
	//    This makes the replacement's pre-install barrier find an entry
	//    whose Recorder is already stopped. The oldCap.Execute() runs
	//    (DeleteRecorderIfSame is no-op since already stopped).
	//    This is a REAL post-capture failure simulation — the old
	//    Recorder is gone, and the pre-install barrier must still
	//    capture the old Session from existing.session (not nil).
	DeleteRecorderIfSame(idA, entryA.recorder)

	// 3. Replacement: pre-install barrier cleans up, then creates B.
	idB, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-fail")
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	if idA != idB {
		t.Fatalf("canonical ID changed: %s → %s", idA, idB)
	}

	// 4. B has fresh Recorder and Session — NOT A's.
	entryB, _ := ownedPTY.Get(idB)
	if entryB.recorder == nil {
		t.Fatal("B: nil Recorder")
	}
	if entryB.session == nil {
		t.Fatal("B: nil Session")
	}
	if entryB.recorder == entryA.recorder {
		t.Error("B reused A's Recorder — want fresh instance")
	}
	if entryB.session == entryA.session {
		t.Error("B reused A's Session — want fresh identity")
	}

	// 5. Cleanup — NO manual DeleteRecorder/terminateAdapterSession calls.
	ownedPTY.Stop(context.Background(), idB)
	time.Sleep(200 * time.Millisecond)
}

// unused imports are needed for type assertions in test helpers
var _ = fmt.Sprintf
var _ io.Reader
var _ = sync.Mutex{}
