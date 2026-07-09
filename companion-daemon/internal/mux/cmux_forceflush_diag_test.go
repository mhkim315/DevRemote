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

	// Distinct semantic lines + volatile footer.
	lines := []string{
		"semantic line 01",
		"semantic line 02",
		"semantic line 03",
		"semantic line 04",
		"semantic line 05",
		"volatile footer gpt-5.5 medium",
	}
	snapshot := strings.Join(lines, "\n")

	// Bootstrap — may commit initial content.
	r1 := st.processScreen(snapshot)
	t.Logf("bootstrap: %q", r1[:minInt(len(r1), 80)])

	// Repeated identical polls. Force-flush fires after 6 polls.
	// After the first force-flush, subsequent identical polls must
	// NOT produce additional commits.
	var commitCount int
	var allLines []string
	for i := 0; i < 30; i++ {
		r := st.processScreen(snapshot)
		if r != "" {
			commitCount++
			for _, l := range splitLines(r) {
				allLines = append(allLines, l)
			}
		}
	}

	// After 30 identical polls, force-flush should fire at most ONCE
	// (the first time stableCount reaches forceFlushPolls).
	// Bootstrap may also commit once. Total commits <= 2.
	if commitCount > 2 {
		t.Errorf("force-flush overcommit: %d commits after 30 identical polls (want <= 2)", commitCount)
	}

	// The same semantic line must not appear more than twice.
	counts := make(map[string]int)
	for _, l := range allLines {
		counts[l]++
	}
	for l, c := range counts {
		if c > 2 {
			t.Errorf("line committed %d times: %q", c, l[:minInt(len(l), 60)])
		}
	}

	t.Logf("commits=%d total_lines=%d unique=%d", commitCount, len(allLines), len(counts))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
