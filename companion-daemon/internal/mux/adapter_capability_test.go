package mux

import (
	"context"
	"testing"
)

func TestAdapterCapabilities_ControlledPTY(t *testing.T) {
	adapter := &controlledPTYAdapter{}
	caps := AdapterCapabilities(adapter)
	mustHave(t, caps, CapObserve)
	mustHave(t, caps, CapControl)
	mustHave(t, caps, CapInput)
	mustHave(t, caps, CapLiveTerminal)
	mustHave(t, caps, CapReliableTranscript)
	// controlled_pty is the Pokit-managed runtime — the only MVP adapter that
	// owns its lifecycle.
	mustHave(t, caps, CapManagedLifecycle)
	mustNotHave(t, caps, CapBestEffortTranscript)
}

func TestAdapterCapabilities_Tmux(t *testing.T) {
	adapter := &tmuxAdapter{}
	caps := AdapterCapabilities(adapter)
	mustHave(t, caps, CapObserve)
	mustHave(t, caps, CapControl)
	mustHave(t, caps, CapInput)
	mustHave(t, caps, CapLiveTerminal)
	mustHave(t, caps, CapReliableTranscript)
	mustNotHave(t, caps, CapBestEffortTranscript)
	// External attachable: accepts control/input but Pokit does NOT own its
	// lifecycle — control/input must not imply managedLifecycle.
	mustNotHave(t, caps, CapManagedLifecycle)
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
	// Internal/experimental: not advertised as managed for product MVP.
	mustNotHave(t, caps, CapManagedLifecycle)
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
	// External observer: never managed.
	mustNotHave(t, caps, CapManagedLifecycle)
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
	// Safe external default: no managed lifecycle.
	mustNotHave(t, caps, CapManagedLifecycle)
}

// TestAdapterCapabilities_ManagedLifecycleSafeDefault proves the flag is
// opt-in: an adapter that declares the provider but returns false is NOT
// managed (safe default), independent of any input/control capability.
func TestAdapterCapabilities_ManagedLifecycleSafeDefault(t *testing.T) {
	mustNotHave(t, AdapterCapabilities(&declaredFalseAdapter{}), CapManagedLifecycle)
	mustHave(t, AdapterCapabilities(&declaredTrueByteStreamAdapter{}), CapManagedLifecycle)
}

// unknownAdapter does not implement TranscriptCaptureProvider.
type unknownAdapter struct{}

func (a *unknownAdapter) Name() string                                      { return "unknown" }
func (a *unknownAdapter) ListSessions(_ context.Context) ([]Session, error) { return nil, nil }

// declaredFalseAdapter is byte-stream (has control/input) but explicitly not
// managed — proves control/input do not imply managedLifecycle.
type declaredFalseAdapter struct{}

func (a *declaredFalseAdapter) Name() string                                      { return "df" }
func (a *declaredFalseAdapter) ListSessions(_ context.Context) ([]Session, error) { return nil, nil }
func (a *declaredFalseAdapter) TranscriptCaptureMode() TranscriptCaptureMode {
	return CaptureModeByteStream
}
func (a *declaredFalseAdapter) ManagedLifecycle() bool { return false }

type declaredTrueByteStreamAdapter struct{}

func (a *declaredTrueByteStreamAdapter) Name() string { return "dt" }
func (a *declaredTrueByteStreamAdapter) ListSessions(_ context.Context) ([]Session, error) {
	return nil, nil
}
func (a *declaredTrueByteStreamAdapter) TranscriptCaptureMode() TranscriptCaptureMode {
	return CaptureModeByteStream
}
func (a *declaredTrueByteStreamAdapter) ManagedLifecycle() bool { return true }

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
