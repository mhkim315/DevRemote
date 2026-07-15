package term

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func testRecord(id string, epoch int64) ManagedSessionRecord {
	return ManagedSessionRecord{
		SessionID: id,
		Provider:  "codex",
		Version:   "codex-cli 0.144.1",
		Epoch:     epoch,
		ProcessID: "proc-opaque",
		OS:        "darwin",
		Arch:      "arm64",
		CreatedAt: time.Now(),
	}
}

// TestManagedRegistry_RegisterGetList_DefensiveCopies: reads return copies —
// mutating a returned record never changes stored state.
func TestManagedRegistry_RegisterGetList_DefensiveCopies(t *testing.T) {
	g := NewManagedSessionRegistry(4)
	if err := g.Register(testRecord("codex_app_server:a", 1)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := g.Register(testRecord("codex_app_server:b", 2)); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, ok := g.Get("codex_app_server:a")
	if !ok || got.NativeStatus != ManagedStatusIdle || got.Epoch != 1 {
		t.Fatalf("get = %+v ok=%v", got, ok)
	}
	// Mutate the returned copy; the stored record must be unchanged.
	got.NativeStatus = ManagedStatusWorking
	got.Provider = "tampered"
	again, _ := g.Get("codex_app_server:a")
	if again.NativeStatus != ManagedStatusIdle || again.Provider != "codex" {
		t.Fatalf("stored record mutated through a returned copy: %+v", again)
	}

	list := g.List()
	if len(list) != 2 {
		t.Fatalf("list len = %d", len(list))
	}
	list[0].NativeStatus = ManagedStatusExited
	fresh := g.List()
	if fresh[0].NativeStatus != ManagedStatusIdle {
		t.Fatalf("stored record mutated through List copy: %+v", fresh[0])
	}
}

// TestManagedRegistry_DuplicateAndCapacity_FailWithoutReplacing: a duplicate
// ID and the capacity+1 registration both fail closed, and every prior record
// is byte-identical afterwards.
func TestManagedRegistry_DuplicateAndCapacity_FailWithoutReplacing(t *testing.T) {
	g := NewManagedSessionRegistry(2)
	if err := g.Register(testRecord("codex_app_server:a", 1)); err != nil {
		t.Fatalf("register a: %v", err)
	}
	before := g.List()

	dup := testRecord("codex_app_server:a", 99)
	dup.Provider = "attacker"
	if err := g.Register(dup); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate register err = %v", err)
	}
	if err := g.Register(testRecord("codex_app_server:b", 2)); err != nil {
		t.Fatalf("register b: %v", err)
	}
	if err := g.Register(testRecord("codex_app_server:c", 3)); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("capacity register err = %v", err)
	}

	after := g.List()
	if len(after) != 2 {
		t.Fatalf("records = %d, want 2", len(after))
	}
	// The pre-existing record is fully unchanged (exact comparison).
	if after[0] != before[0] {
		t.Fatalf("record replaced by failed register:\n before %+v\n after  %+v", before[0], after[0])
	}
	if after[0].Epoch != 1 || after[0].Provider != "codex" {
		t.Fatalf("identity mutated: %+v", after[0])
	}
}

// TestManagedRegistry_OrderedNativeEventsUpdateStatus: idle → working →
// completed transitions apply in order for the current epoch.
func TestManagedRegistry_OrderedNativeEventsUpdateStatus(t *testing.T) {
	g := NewManagedSessionRegistry(2)
	if err := g.Register(testRecord("codex_app_server:a", 7)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if !g.UpdateNativeStatus("codex_app_server:a", 7, ManagedStatusWorking) {
		t.Fatal("working update rejected for current epoch")
	}
	if rec, _ := g.Get("codex_app_server:a"); rec.NativeStatus != ManagedStatusWorking {
		t.Fatalf("status = %s, want working", rec.NativeStatus)
	}
	if !g.UpdateNativeStatus("codex_app_server:a", 7, ManagedStatusCompleted) {
		t.Fatal("completed update rejected for current epoch")
	}
	if rec, _ := g.Get("codex_app_server:a"); rec.NativeStatus != ManagedStatusCompleted {
		t.Fatalf("status = %s, want completed", rec.NativeStatus)
	}
}

// TestManagedRegistry_StaleClosedEpochRejected: old-epoch updates, updates
// after exit, updates after Close, unknown status values, and direct "exited"
// injection are all rejected without mutating state.
func TestManagedRegistry_StaleClosedEpochRejected(t *testing.T) {
	g := NewManagedSessionRegistry(2)
	if err := g.Register(testRecord("codex_app_server:a", 7)); err != nil {
		t.Fatalf("register: %v", err)
	}

	if g.UpdateNativeStatus("codex_app_server:a", 6, ManagedStatusWorking) {
		t.Fatal("stale-epoch update accepted")
	}
	if g.UpdateNativeStatus("codex_app_server:a", 8, ManagedStatusWorking) {
		t.Fatal("future-epoch update accepted")
	}
	if g.UpdateNativeStatus("codex_app_server:a", 7, ManagedNativeStatus("root")) {
		t.Fatal("unknown status value accepted")
	}
	if g.UpdateNativeStatus("codex_app_server:a", 7, ManagedStatusExited) {
		t.Fatal("exited injected through UpdateNativeStatus")
	}
	if g.UpdateNativeStatus("codex_app_server:missing", 7, ManagedStatusWorking) {
		t.Fatal("update accepted for unknown session")
	}
	if rec, _ := g.Get("codex_app_server:a"); rec.NativeStatus != ManagedStatusIdle {
		t.Fatalf("status mutated by rejected updates: %s", rec.NativeStatus)
	}

	// After exit: same-epoch events cannot restore a current status.
	if !g.MarkExited("codex_app_server:a", 7) {
		t.Fatal("mark exited rejected")
	}
	if g.UpdateNativeStatus("codex_app_server:a", 7, ManagedStatusWorking) {
		t.Fatal("post-exit update restored status")
	}
	if g.MarkExited("codex_app_server:a", 7) {
		t.Fatal("second MarkExited reported success")
	}
	rec, _ := g.Get("codex_app_server:a")
	if !rec.Exited || rec.NativeStatus != ManagedStatusExited {
		t.Fatalf("exit state = %+v", rec)
	}

	// After Close: nothing is writable, nothing is registrable.
	g.Close()
	if err := g.Register(testRecord("codex_app_server:z", 9)); err == nil {
		t.Fatal("register accepted on a closed registry")
	}
	if g.UpdateNativeStatus("codex_app_server:a", 7, ManagedStatusIdle) {
		t.Fatal("update accepted on a closed registry")
	}
	if g.MarkExited("codex_app_server:a", 7) {
		t.Fatal("mark-exited accepted on a closed registry")
	}
}

// TestManagedRegistry_ChildExitMarksNonCurrent: MarkExited flips the record
// to exited/non-current exactly once for the current epoch, and a stale-epoch
// exit is rejected.
func TestManagedRegistry_ChildExitMarksNonCurrent(t *testing.T) {
	g := NewManagedSessionRegistry(2)
	if err := g.Register(testRecord("codex_app_server:a", 3)); err != nil {
		t.Fatalf("register: %v", err)
	}
	if g.MarkExited("codex_app_server:a", 2) {
		t.Fatal("stale-epoch exit accepted")
	}
	if !g.MarkExited("codex_app_server:a", 3) {
		t.Fatal("current-epoch exit rejected")
	}
	rec, _ := g.Get("codex_app_server:a")
	if !rec.Exited || rec.NativeStatus != ManagedStatusExited {
		t.Fatalf("record after exit = %+v", rec)
	}
}

// TestManagedRegistry_ConcurrentAccess: register/update/exit/snapshot/remove/
// close race under -race. Afterwards every surviving record must hold a value
// from the closed vocabulary and exited records must never regress.
func TestManagedRegistry_ConcurrentAccess(t *testing.T) {
	g := NewManagedSessionRegistry(64)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		id := fmt.Sprintf("codex_app_server:s%d", i)
		epoch := int64(i + 1)
		if err := g.Register(testRecord(id, epoch)); err != nil {
			t.Fatalf("seed register: %v", err)
		}
		wg.Add(4)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				g.UpdateNativeStatus(id, epoch, ManagedStatusWorking)
				g.UpdateNativeStatus(id, epoch, ManagedStatusCompleted)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				g.Get(id)
				g.List()
			}
		}()
		go func() {
			defer wg.Done()
			g.MarkExited(id, epoch)
			for j := 0; j < 50; j++ {
				g.UpdateNativeStatus(id, epoch, ManagedStatusWorking) // must stay rejected
			}
		}()
		go func() {
			defer wg.Done()
			g.Register(testRecord(id, 999)) // duplicate — must keep failing
		}()
	}
	wg.Wait()

	for _, rec := range g.List() {
		if !rec.Exited || rec.NativeStatus != ManagedStatusExited {
			t.Fatalf("exited record regressed: %+v", rec)
		}
		if rec.Epoch == 999 {
			t.Fatalf("duplicate register replaced identity: %+v", rec)
		}
	}
	g.Close()
}
