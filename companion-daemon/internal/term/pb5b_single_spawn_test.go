package term

import (
	"context"
	"syscall"
	"testing"

	"devremote/companion-daemon/internal/mux"
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

	// Create with nil spawn fails — V1 is not called as fallback.
	_, err := owned.Create(context.Background(), mux.CreateOptions{Name: "test"}, "", "test")
	if err == nil {
		t.Fatal("Create with nil spawn must fail closed")
	}

	// V1 must NOT have been called — avoids double-spawn.
	if cl.calls != 0 {
		t.Fatalf("V1 Spawn was called during Create: %d calls (double-spawn bug)", cl.calls)
	}
}
