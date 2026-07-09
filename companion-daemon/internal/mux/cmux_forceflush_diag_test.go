package mux

import (
	"strings"
	"testing"
)

// TestForceFlush_Overcommit reproduces the observed 95% duplication bug.
// Consecutive identical snapshots cause force-flush to recommit the entire
// non-volatile screen content repeatedly.
func TestForceFlush_Overcommit(t *testing.T) {
	st := newScreenTracker()

	// Simulate a screen with semantic + volatile content.
	// This matches the real cmux screen structure.
	stable := strings.Repeat("semantic line\n", 5)
	volatile := "timer Working (1s • esc to interrupt)\nfooter gpt-5.5 medium\n"
	snapshot := stable + volatile

	// Step 1: Bootstrap — commits initial semantic content.
	r1 := st.processScreen(snapshot)
	t.Logf("bootstrap: committed=%d chars", len(r1))
	initialCommitCount := len(splitLines(r1))

	// Step 2: Repeated identical snapshots (screen is idle).
	// Force-flush triggers after 6 polls and may overcommit.
	var totalLines int
	var allLines []string
	for i := 0; i < 20; i++ {
		r := st.processScreen(snapshot)
		if r != "" {
			lines := splitLines(r)
			totalLines += len(lines)
			allLines = append(allLines, lines...)
		}
	}

	unique := make(map[string]bool)
	for _, l := range allLines {
		unique[l] = true
	}
	dupPct := float64(totalLines-len(unique)) / float64(max(totalLines, 1)) * 100

	t.Logf("After 20 identical polls:")
	t.Logf("  initial commit: %d lines", initialCommitCount)
	t.Logf("  total committed: %d lines", totalLines)
	t.Logf("  unique lines: %d", len(unique))
	t.Logf("  duplication: %.1f%%", dupPct)

	// ACCEPTANCE: duplication must be under 50%.
	// >50% means the same lines are being recommitted.
	if dupPct > 50 {
		t.Errorf("force-flush overcommit: %.1f%% duplication (want < 50%%)", dupPct)
	}
}

func max(a, b int) int { if a > b { return a }; return b }
