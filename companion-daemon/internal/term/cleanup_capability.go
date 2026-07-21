package term

import (
	"context"
	"sync"
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

func (c *GenerationCompletion) Complete()             { c.once.Do(func() { close(c.done) }) }
func (c *GenerationCompletion) Done() <-chan struct{} { return c.done }

type GenerationCleanupCapability struct {
	Generation  int64
	Identity    LaunchIdentity
	Recorder    *Recorder
	Transport   *TerminalTransport
	Cleanup     ProcessCleanup
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
	if cap.Cleanup != nil {
		cap.Cleanup.Execute(ctx)
	}
}
