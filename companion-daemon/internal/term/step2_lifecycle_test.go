package term

import (
	"testing"

	"devremote/companion-daemon/internal/transcript"
)

// PA3 Step 2 R7: Branch A LifecycleState ownership boundary tests.

// TestLifecycleState_ControlledPTY_SeededEntry verifies mergeLifecycleState
// returns the EXACT non-empty LifecycleState from a seeded CatalogEntry.
func TestLifecycleState_ControlledPTY_SeededEntry(t *testing.T) {
	transcriptSvc := transcript.NewService(transcript.DefaultStoreConfig())
	ownedPTY := testOwnedPTYRuntime(nil, transcriptSvc)
	lifecycle := testLifecycleService(ownedPTY, transcriptSvc)

	// Seed a running catalog entry.
	seedCatalog(lifecycle, "controlled_pty:test-ls", "controlled_pty", "my-session", LifecycleRunning)

	snap := []SessionTelemetry{{
		ID: "controlled_pty:test-ls", DisplayID: "test-ls", Adapter: "controlled_pty",
	}}
	result := mergeLifecycleState(snap, lifecycle)

	if len(result) == 0 {
		t.Fatal("mergeLifecycleState returned empty")
	}
	row := result[0]
	if row.LifecycleState != "running" {
		t.Errorf("LifecycleState = %q, want running", row.LifecycleState)
	}
}
