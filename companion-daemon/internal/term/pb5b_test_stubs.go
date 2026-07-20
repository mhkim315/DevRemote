package term

import (
	"sync"
)

// PB.5b: stub types for tests

type fakeProviderOwner struct {
	currentEpoch int
	mu           sync.Mutex
	calls        []string
}

func (f *fakeProviderOwner) Stop(_ string, _ int64) LifecycleOutcome   { return "" }
func (f *fakeProviderOwner) Kill(_ string, _ int64) LifecycleOutcome   { return "" }
func (f *fakeProviderOwner) Delete(_ string, _ int64) LifecycleOutcome { return "" }
func (f *fakeProviderOwner) CurrentEpoch(_ string) int                 { return f.currentEpoch }
