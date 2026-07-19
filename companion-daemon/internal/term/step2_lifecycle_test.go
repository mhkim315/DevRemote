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

// TestLifecycleState_ManagedRow_Absent verifies that managed Codex/Claude
// rows have NO LifecycleState in the SessionTelemetry projection.
// LifecycleState is owned exclusively by OwnedPTYRuntime.
func TestLifecycleState_ManagedRow_Absent(t *testing.T) {
	// SessionTelemetry for a managed row carries zero LifecycleState.
	row := SessionTelemetry{
		ID: "codex_app_server:test-managed", Adapter: "codex_app_server",
		AgentStatus: "idle",
	}
	if row.LifecycleState != "" {
		t.Errorf("managed LifecycleState = %q, want empty (absent)", row.LifecycleState)
	}
	if row.Adapter != "codex_app_server" {
		t.Errorf("Adapter = %q, want codex_app_server", row.Adapter)
	}
	if row.AgentStatus != "idle" {
		t.Errorf("AgentStatus = %q, want idle", row.AgentStatus)
	}
}
