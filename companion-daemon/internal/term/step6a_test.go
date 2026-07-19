package term

import (
	"context"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

func TestStep6a_ReplacementCapturesOldRecorder(t *testing.T) {
	ctl := mux.NewControlledPTYAdapter()
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	ownedPTY := NewOwnedPTYRuntime(ctl, NewActivityBuffer(100), transcriptSvc)

	// Use sleep so the Recorder stays alive long enough for the replacement.
	opts := mux.CreateOptions{
		Name:       "step6a-test",
		Executable: "sleep",
		Args:       []string{"10"},
	}
	id1, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-test")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	entry, ok := ownedPTY.Get(id1)
	if !ok || entry.recorder == nil {
		t.Fatal("first entry has no Recorder")
	}
	oldRec := entry.recorder

	// Replacement should capture oldRec.
	id2, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-test")
	if err != nil {
		// Clean up first session.
		ownedPTY.Stop(context.Background(), id1)
		t.Fatalf("replacement create: %v", err)
	}
	if id1 != id2 {
		t.Errorf("canonical ID changed: %s → %s", id1, id2)
	}

	// Old Recorder was stopped by instance-guarded cleanup.
	time.Sleep(50 * time.Millisecond)
	DeleteRecorderIfSame(id1, oldRec) // no-op, already stopped

	newEntry, ok := ownedPTY.Get(id2)
	if !ok || newEntry.recorder == nil {
		t.Fatal("replacement entry has no Recorder")
	}
	if newEntry.recorder == oldRec {
		t.Error("replacement reused old Recorder")
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

	// Replacement executes old cleanup before publishing.
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

func TestStep6a_FailedCreateLeavesReplacementIntact(t *testing.T) {
	ctl := mux.NewControlledPTYAdapter()
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	ownedPTY := NewOwnedPTYRuntime(ctl, NewActivityBuffer(100), transcriptSvc)

	// Create first session with exact Session captured.
	opts := mux.CreateOptions{Name: "step6a-fail", Executable: "sleep", Args: []string{"30"}}
	id1, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-fail")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	entry1, ok := ownedPTY.Get(id1)
	if !ok || entry1.session == nil {
		t.Fatal("first entry has nil Session — precondition for captured cleanup")
	}
	if entry1.recorder == nil {
		t.Fatal("first entry has nil Recorder")
	}

	// Trigger rollback: delete old Recorder BEFORE replacement.
	// The pre-install barrier captures oldCap with existing.session +
	// existing.recorder. Execute() runs DeleteRecorderIfSame (instance-guarded).
	// The old Recorder is stopped. Replacement proceeds with fresh Recorder.
	DeleteRecorderIfSame(id1, entry1.recorder)

	// Replacement creates new session B. oldCap captures entry1's Session,
	// not nil. Pre-install barrier executes old cleanup (DeleteRecorderIfSame
	// is no-op since already stopped), then creates fresh session.
	id2, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-fail")
	if err != nil {
		t.Fatalf("replacement: %v", err)
	}
	if id1 != id2 {
		t.Errorf("canonical ID changed: %s → %s", id1, id2)
	}

	entry2, _ := ownedPTY.Get(id2)
	if entry2.recorder == nil {
		t.Fatal("replacement has nil Recorder")
	}
	if entry2.recorder == entry1.recorder {
		t.Error("replacement reused old Recorder — want fresh instance")
	}
	if entry2.session == nil {
		t.Error("replacement has nil Session")
	}
	if entry2.session == entry1.session {
		t.Error("replacement reused old Session")
	}

	ownedPTY.Stop(context.Background(), id2)
	time.Sleep(200 * time.Millisecond)
}
