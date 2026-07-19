package term

import (
	"context"
	"fmt"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/sessionid"
	"devremote/companion-daemon/internal/transcript"
)

func TestStep6a_ReplacementCapturesOldRecorder(t *testing.T) {
	ctl := mux.NewControlledPTYAdapter()
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	ownedPTY := NewOwnedPTYRuntime(ctl, nil, transcriptSvc)

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
	ownedPTY := NewOwnedPTYRuntime(ctl, nil, transcriptSvc)

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

// PA3 Step 6a Rollback Proof: prove production cap.Execute() rollback runs
// when createWithCapture fails after CreateSessionAndCapture but before publication.
//
// The test injects a deterministic failure via testCreateWithCaptureFailAfterCapture
// (set in owned_pty_runtime.go) between newCleanup and register(). At that point:
//   - CreateSessionAndCapture has succeeded (session B is in the adapter map)
//   - StartRecorderUnconditional has created B's Recorder
//   - newTerminalTransport has created B's Transport
//   - All cap fields are populated (Session, Recorder, Transport, Terminator)
//
// The ONLY cleanup path for B's Recorder and session is the deferred cap.Execute().
// There is no manual DeleteRecorderIfSame/CompareAndTerminate on this code path.
//
// PROHIBITED in test code: DeleteRecorderIfSame, DeleteRecorder,
// terminateAdapterSession, CompareAndTerminate, or any equivalent manual cleanup.
func TestStep6a_RollbackProof(t *testing.T) {
	ctl := mux.NewControlledPTYAdapter()
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	ownedPTY := NewOwnedPTYRuntime(ctl, nil, transcriptSvc)

	opts := mux.CreateOptions{Name: "step6a-rbproof", Executable: "sleep", Args: []string{"30"}}

	// Step a: Create session A normally — captures exact Session + Recorder.
	idA, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-rbproof")
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	entryA, ok := ownedPTY.Get(idA)
	if !ok || entryA.recorder == nil || entryA.session == nil {
		t.Fatal("A: missing Recorder or Session")
	}
	oldRec := entryA.recorder
	oldSess := entryA.session

	if !oldRec.IsAlive() {
		t.Fatal("A's Recorder should be alive after creation")
	}
	canonicalID := idA

	// Step b: Enable the failure injector — triggers AFTER CreateSessionAndCapture
	// succeeds but BEFORE publication (register). Produces a deterministic path
	// where ONLY the deferred cap.Execute() cleans up the replacement.
	testCreateWithCaptureFailAfterCapture = func() error {
		return fmt.Errorf("injected rollback failure before publication")
	}

	// Step c: Call Create for same-ID replacement — MUST return error.
	_, err = ownedPTY.Create(context.Background(), opts, "shell", "step6a-rbproof")
	if err == nil {
		t.Fatal("expected error from failed replacement, got nil")
	}
	t.Logf("replacement error (expected): %v", err)

	// Step d: PROVE the deferred cap.Execute() ran.
	//
	// d.1: A's Recorder is stopped. This proves oldCap.Execute ran in the
	//      pre-install barrier (DeleteRecorderIfSame for A's Recorder).
	if oldRec.IsAlive() {
		t.Error("A's Recorder should be stopped by pre-install oldCap.Execute")
	}

	// d.2: GetRecorder returns nil. This proves the deferred cap.Execute() ran
	//      for the FAILED replacement B. The replacement created B's Recorder
	//      (StartRecorderUnconditional) and the ONLY cleanup of B's Recorder on
	//      this code path is the deferred cap.Execute(). If cap.Execute didn't
	//      run, B's Recorder would still be registered and alive.
	if r := GetRecorder(canonicalID); r != nil {
		t.Errorf("GetRecorder should return nil after rollback, got %v — "+
			"deferred cap.Execute may not have run", r)
	}

	// d.3: The adapter has no session for the local ID. Both A (cleaned by
	//      oldCap.Execute) and B (cleaned by deferred cap.Execute) were
	//      terminated via CompareAndTerminate.
	sessions, err := ctl.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions after rollback: %v", err)
	}
	localID := sessionid.ParseSessionID(canonicalID).LocalID
	for _, s := range sessions {
		if s.ID() == localID {
			t.Errorf("session %s still in adapter after rollback — "+
				"deferred cap.Execute may not have terminated it", localID)
		}
	}

	// Step e: Create session B (same ID) normally — MUST succeed.
	// Disable the failure injector so this creation succeeds.
	testCreateWithCaptureFailAfterCapture = nil
	idB, err := ownedPTY.Create(context.Background(), opts, "shell", "step6a-rbproof")
	if err != nil {
		t.Fatalf("create B after rollback: %v", err)
	}
	if idA != idB {
		t.Fatalf("canonical ID changed after rollback: %s → %s", idA, idB)
	}

	// Step f: Assert B has fresh Recorder and Session, distinct from A's.
	entryB, ok := ownedPTY.Get(idB)
	if !ok || entryB.recorder == nil || entryB.session == nil {
		t.Fatal("B: missing Recorder or Session after recovery")
	}
	if entryB.recorder == oldRec {
		t.Error("B reused A's old Recorder — want fresh instance")
	}
	if entryB.session == oldSess {
		t.Error("B reused A's old Session — want fresh identity")
	}
	if !entryB.recorder.IsAlive() {
		t.Error("B's Recorder should be alive after recovery creation")
	}

	// Step g: Assert A's old capability cannot affect B.
	// B's Recorder stays alive and B's session remains in the adapter —
	// if A's old cleanup capability could affect B, either would be gone.
	time.Sleep(50 * time.Millisecond)
	if !entryB.recorder.IsAlive() {
		t.Error("B's Recorder stopped unexpectedly — old capability may have affected B")
	}
	sessions, err = ctl.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("list sessions after B: %v", err)
	}
	found := false
	for _, s := range sessions {
		if s.ID() == localID {
			found = true
			break
		}
	}
	if !found {
		t.Error("B's session not found in adapter — old capability may have affected B")
	}

	// Cleanup — NO manual DeleteRecorder/terminateAdapterSession calls.
	ownedPTY.Stop(context.Background(), idB)
	time.Sleep(200 * time.Millisecond)
}
