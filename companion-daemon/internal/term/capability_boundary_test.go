package term

import (
	"slices"
	"testing"

	"devremote/companion-daemon/internal/mux"
)

// TestAPICapabilities_ManagedLifecycleBoundary proves the /api/sessions
// adapterCapabilities boundary exposes managedLifecycle for the Pokit-managed
// runtime (controlled_pty) and NOT for an external attachable adapter (tmux),
// while tmux keeps its input/control/liveTerminal capabilities. No adapter-name
// branching — the value comes from the adapter's capability declaration.
func TestAPICapabilities_ManagedLifecycleBoundary(t *testing.T) {
	reg := mux.MustNewRegistry(mux.NewControlledPTYAdapter(), mux.NewTmuxAdapter())

	cp := adapterCapabilityStrings(reg, "controlled_pty")
	if !slices.Contains(cp, "managedLifecycle") {
		t.Fatalf("controlled_pty adapterCapabilities missing managedLifecycle: %v", cp)
	}

	tm := adapterCapabilityStrings(reg, "tmux")
	if slices.Contains(tm, "managedLifecycle") {
		t.Fatalf("tmux must not expose managedLifecycle: %v", tm)
	}
	for _, want := range []string{"control", "input", "liveTerminal", "reliableTranscript"} {
		if !slices.Contains(tm, want) {
			t.Fatalf("tmux missing expected capability %q: %v", want, tm)
		}
	}
}
