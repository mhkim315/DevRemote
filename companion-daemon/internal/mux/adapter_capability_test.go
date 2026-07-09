package mux

import (
	"context"
	"testing"
)

func TestAdapterCapabilities_Tmux(t *testing.T) {
	adapter := &tmuxAdapter{}
	caps := AdapterCapabilities(adapter)
	mustHave(t, caps, CapObserve)
	mustHave(t, caps, CapControl)
	mustHave(t, caps, CapInput)
	mustHave(t, caps, CapLiveTerminal)
	mustHave(t, caps, CapReliableTranscript)
	mustNotHave(t, caps, CapBestEffortTranscript)
}

func TestAdapterCapabilities_LocalPTY(t *testing.T) {
	adapter := &localptyAdapter{}
	caps := AdapterCapabilities(adapter)
	mustHave(t, caps, CapObserve)
	mustHave(t, caps, CapControl)
	mustHave(t, caps, CapInput)
	mustHave(t, caps, CapLiveTerminal)
	mustHave(t, caps, CapReliableTranscript)
	mustNotHave(t, caps, CapBestEffortTranscript)
}

func TestAdapterCapabilities_Cmux(t *testing.T) {
	adapter := &cmuxAdapter{}
	caps := AdapterCapabilities(adapter)
	mustHave(t, caps, CapObserve)
	mustHave(t, caps, CapBestEffortTranscript)
	mustNotHave(t, caps, CapLiveTerminal)
	mustNotHave(t, caps, CapReliableTranscript)
	mustNotHave(t, caps, CapInput)
	mustNotHave(t, caps, CapControl)
}

func TestAdapterCapabilities_UnknownNoProvider(t *testing.T) {
	adapter := &unknownAdapter{}
	caps := AdapterCapabilities(adapter)
	mustHave(t, caps, CapObserve)
	mustNotHave(t, caps, CapControl)
	mustNotHave(t, caps, CapInput)
	mustNotHave(t, caps, CapLiveTerminal)
	mustNotHave(t, caps, CapReliableTranscript)
	mustNotHave(t, caps, CapBestEffortTranscript)
}

// unknownAdapter does not implement TranscriptCaptureProvider.
type unknownAdapter struct{}

func (a *unknownAdapter) Name() string                                      { return "unknown" }
func (a *unknownAdapter) ListSessions(_ context.Context) ([]Session, error) { return nil, nil }

func mustHave(t *testing.T, caps []AdapterCapability, target AdapterCapability) {
	t.Helper()
	if !hasCap(caps, target) {
		t.Errorf("missing capability: %s", target)
	}
}

func mustNotHave(t *testing.T, caps []AdapterCapability, target AdapterCapability) {
	t.Helper()
	if hasCap(caps, target) {
		t.Errorf("unexpected capability: %s", target)
	}
}
