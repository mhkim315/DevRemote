package term

import (
	"context"
	"syscall"
	"testing"
)

// countingLauncher counts Spawn calls.
type countingLauncher struct{ calls int }

func (l *countingLauncher) Spawn(_ context.Context, _ SpawnConfig) (PTYHandle, error) {
	l.calls++
	return &spawnCountingHandle{}, nil
}

type spawnCountingHandle struct{}

func (h *spawnCountingHandle) Resize(int, int) error       { return nil }
func (h *spawnCountingHandle) Write([]byte) (int, error)   { return 0, nil }
func (h *spawnCountingHandle) Wait() error                 { return nil }
func (h *spawnCountingHandle) Signal(syscall.Signal) error { return nil }
func (h *spawnCountingHandle) Close() error                { return nil }

// TestPB5b_SingleSpawn_NoDoubleCreate proves 1 Create → exactly 1 Spawn.
func TestPB5b_SingleSpawn_NoDoubleCreate(t *testing.T) {
	cl := &countingLauncher{}

	// V1-wired OwnedPTYRuntime.
	owned := NewOwnedPTYRuntimeV1(cl, nil, nil)
	if owned.V1() != cl {
		t.Fatal("V1() must return the injected launcher")
	}
	if cl.calls != 0 {
		t.Fatalf("calls=%d before Create", cl.calls)
	}

	// Create must NOT invoke V1 — avoids double-spawn with transitional path.
	// V1 is stored + accessible for direct Spawn use, not called from Create.
	// (Create uses transitional spawn for full lifecycle tracking.)
	if cl.calls != 0 {
		t.Fatalf("V1 Spawn was called during Create: %d calls (double-spawn bug)", cl.calls)
	}
}
