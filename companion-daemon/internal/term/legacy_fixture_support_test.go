package term

import (
	"context"
	"io"
)

// mockStream is a direct V1 recorder stream fixture; it has no adapter or
// registry identity.
type mockStream struct {
	pr *io.PipeReader
	pw *io.PipeWriter
}

func (s *mockStream) Read(p []byte) (int, error)  { return s.pr.Read(p) }
func (s *mockStream) Write(p []byte) (int, error) { return s.pw.Write(p) }
func (s *mockStream) Close() error                { return s.pr.Close() }
func (*mockStream) Resize(int, int) error         { return nil }

func runtimesLen(s *ManagedCodexService) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.runtimes)
}
func leasesLen(s *ManagedCodexService) int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.leases) }

// ownedRecorderForTest follows the production ownership graph: a recorder is
// reachable only from the exact runtime generation which created it.
func ownedRecorderForTest(owned *OwnedPTYRuntime, id string) *Recorder {
	entry, ok := owned.Get(id)
	if !ok {
		return nil
	}
	return entry.recorder
}

func closeOwnedForTest(owned *OwnedPTYRuntime, id string) {
	if recorder := ownedRecorderForTest(owned, id); recorder != nil {
		recorder.Stop()
	}
	_, _ = owned.Kill(context.Background(), id)
}
