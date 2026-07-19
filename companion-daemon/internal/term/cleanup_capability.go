package term

import (
	"context"
	"sync"

	"devremote/companion-daemon/internal/mux"
)

// PA3 Step 6: GenerationCleanupCapability — immutable generation-bound cleanup.
// Captures exact adapter session, Recorder, transport, and terminator identities.
// Execute() is nil-safe: each step guards its field.

type GenerationCompletion struct {
	once sync.Once
	done chan struct{}
}

func NewGenerationCompletion() *GenerationCompletion {
	return &GenerationCompletion{done: make(chan struct{})}
}

func (c *GenerationCompletion) Complete() { c.once.Do(func() { close(c.done) }) }
func (c *GenerationCompletion) Done() <-chan struct{} { return c.done }

type GenerationCleanupCapability struct {
	Generation  int64
	Session     mux.Session
	Recorder    *Recorder
	Transport   *TerminalTransport
	Terminator  mux.SessionIdentityTerminator
	CanonicalID string
	LocalID     string
	Completion  *GenerationCompletion
}

func (cap *GenerationCleanupCapability) Execute(ctx context.Context) {
	defer cap.Completion.Complete()
	if cap.Transport != nil {
		cap.Transport.RetireIfGeneration(cap.Generation)
	}
	if cap.Recorder != nil {
		DeleteRecorderIfSame(cap.CanonicalID, cap.Recorder)
	}
	if cap.Terminator != nil && cap.Session != nil {
		_ = cap.Terminator.CompareAndTerminate(ctx, cap.LocalID, cap.Session)
	}
}
