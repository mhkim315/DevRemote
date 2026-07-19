package term

import (
	"testing"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// PA3 Step 2 R7: Branch A LifecycleState ownership boundary tests.

// TestLifecycleState_ControlledPTY_SeededEntry verifies mergeLifecycleState
// returns the EXACT non-empty LifecycleState from a seeded CatalogEntry.
func TestLifecycleState_ControlledPTY_SeededEntry(t *testing.T) {
	reg, _ := mux.NewRegistry()
	ctl := mux.NewControlledPTYAdapter()
	reg.Register(ctl)

	activity := NewActivityBuffer(100)
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	ownedPTY := NewOwnedPTYRuntime(ctl, activity, transcriptSvc)
	lifecycle := NewLifecycleService(ownedPTY, activity, transcriptSvc)

	// Seed a running catalog entry.
	seedCatalog(lifecycle, "controlled_pty:test-ls", "controlled_pty", "my-session", LifecycleRunning)

	snap := []SessionTelemetry{{
		ID: "controlled_pty:test-ls", DisplayID: "test-ls", Adapter: "controlled_pty",
	}}
	result := mergeLifecycleState(snap, lifecycle, reg)

	if len(result) == 0 {
		t.Fatal("mergeLifecycleState returned empty")
	}
	row := result[0]
	if row.LifecycleState != "running" {
		t.Errorf("LifecycleState = %q, want running", row.LifecycleState)
	}
}

