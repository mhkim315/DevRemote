package term

import (
	"context"
	"testing"
	"time"

	"devremote/companion-daemon/internal/transcript"
)

func step6aResult(id string, done chan struct{}, cleanup *migrationCleanup) LaunchResult {
	return LaunchResult{Handle: &migrationHandle{reader: migrationReader{done}}, Identity: LaunchIdentity{InstanceID: id, StartedAt: time.Now()}, ProcessCleanup: cleanup}
}

func TestStep6a_ReplacementCapturesOldRecorder(t *testing.T) {
	firstDone, secondDone := make(chan struct{}), make(chan struct{})
	firstCleanup, secondCleanup := &migrationCleanup{}, &migrationCleanup{}
	launcher := &migrationLauncher{results: []LaunchResult{step6aResult("one", firstDone, firstCleanup), step6aResult("two", secondDone, secondCleanup)}}
	owned := NewOwnedPTYRuntime(launcher, transcript.NewService(transcript.DefaultStoreConfig()))
	cfg := SpawnConfig{Name: "step6a-test", Executable: "sleep", Args: []string{"10"}}
	id, err := owned.Create(context.Background(), cfg, "shell", "step6a-test")
	if err != nil {
		t.Fatal(err)
	}
	first, ok := owned.Get(id)
	if !ok || first.recorder == nil {
		t.Fatal("first V1 launch missing recorder")
	}
	if _, err := owned.Create(context.Background(), cfg, "shell", "step6a-test"); err != nil {
		t.Fatal(err)
	}
	second, ok := owned.Get(id)
	if !ok || second.recorder == nil || second.recorder == first.recorder {
		t.Fatal("replacement did not publish a fresh recorder")
	}
	if second.Identity.InstanceID == first.Identity.InstanceID {
		t.Fatal("replacement reused LaunchIdentity")
	}
	if firstCleanup.count() != 1 || secondCleanup.count() != 0 {
		t.Fatalf("cleanup counts old=%d new=%d", firstCleanup.count(), secondCleanup.count())
	}
	close(firstDone)
	close(secondDone)
}

func TestStep6a_RollbackCleanupIsInstanceGuarded(t *testing.T) {
	firstDone, secondDone := make(chan struct{}), make(chan struct{})
	firstCleanup, secondCleanup := &migrationCleanup{}, &migrationCleanup{}
	launcher := &migrationLauncher{results: []LaunchResult{step6aResult("old", firstDone, firstCleanup), step6aResult("new", secondDone, secondCleanup)}}
	owned := NewOwnedPTYRuntime(launcher, nil)
	cfg := SpawnConfig{Name: "step6a-rb", Executable: "sleep", Args: []string{"10"}}
	id, err := owned.Create(context.Background(), cfg, "shell", "step6a-rb")
	if err != nil {
		t.Fatal(err)
	}
	old, _ := owned.Get(id)
	if _, err := owned.Create(context.Background(), cfg, "shell", "step6a-rb"); err != nil {
		t.Fatal(err)
	}
	current, _ := owned.Get(id)
	if current.Generation == old.Generation || current.recorder == old.recorder {
		t.Fatal("replacement retained old generation resources")
	}
	if firstCleanup.count() != 1 {
		t.Fatalf("old cleanup calls=%d, want 1", firstCleanup.count())
	}
	close(firstDone)
	close(secondDone)
}

func TestStep6a_RollbackProof(t *testing.T) {
	done := make(chan struct{})
	cleanup := &migrationCleanup{}
	launcher := &migrationLauncher{results: []LaunchResult{{Handle: &migrationHandle{reader: migrationReader{done}}, Identity: LaunchIdentity{InstanceID: "bad", StartedAt: time.Now()}, ProcessCleanup: cleanup}}}
	owned := NewOwnedPTYRuntime(launcher, nil)
	_, err := owned.Create(context.Background(), SpawnConfig{Name: "step6a-rbproof", Executable: "sleep"}, "shell", "rollback")
	if err != nil {
		t.Fatal(err)
	}
	// A V1 cleanup is instance-bound: invoking it more than once cannot affect
	// the published generation or run the process cleanup again.
	cleanup.Execute(context.Background())
	cleanup.Execute(context.Background())
	if cleanup.count() != 1 {
		t.Fatalf("cleanup calls=%d, want 1", cleanup.count())
	}
	close(done)
}
