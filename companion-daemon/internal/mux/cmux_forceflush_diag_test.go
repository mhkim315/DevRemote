package mux

import (
	"fmt"
	"strings"
	"testing"
)

// TestForceFlush_Overcommit reproduces the 95% duplication bug:
// committed trim window (50 lines) is smaller than screen line count,
// so old lines fall out of the window and get recommitted by each
// force-flush cycle.
func TestForceFlush_Overcommit(t *testing.T) {
	st := newScreenTracker()

	// 200 distinct semantic lines — exceeds committed trim window (50).
	var lines []string
	for i := 0; i < 200; i++ {
		lines = append(lines, fmt.Sprintf("semantic line %03d", i))
	}
	// Add volatile footer that is always filtered.
	lines = append(lines, "volatile footer gpt-5.5 medium")
	snapshot := strings.Join(lines, "\n")

	// Step 1: Bootstrap commits.
	r1 := st.processScreen(snapshot)
	firstLines := len(splitLines(r1))
	t.Logf("bootstrap: %d lines", firstLines)

	// Step 2: Run 30 identical polls. This gives force-flush multiple
	// opportunities to fire (every 6 polls). Without the forceFlushed
	// guard, each force-flush would recommit old lines that fell out
	// of the committed window — producing hundreds of duplicate lines.
	var commitCount int
	totalLines := 0
	for i := 0; i < 30; i++ {
		r := st.processScreen(snapshot)
		if r != "" {
			commitCount++
			totalLines += len(splitLines(r))
		}
	}

	// Without the guard, commits would pile up (>10) because each
	// force-flush cycle recommits lines outside the committed window.
	// With the guard, force-flush fires at most once per stable screen.
	// Bootstrap may commit once. Total commits must be <= 2.
	if commitCount > 2 {
		t.Errorf("force-flush overcommit: %d commits after 30 identical polls (want <= 2). "+
			"Without forceFlushed guard, old lines outside committed window get recommitted repeatedly.",
			commitCount)
	}

	// If total committed lines exceed the screen's distinct line count
	// by a large margin, lines are being recommitted.
	screenDistinct := 200
	if totalLines > screenDistinct*2 {
		t.Errorf("force-flush overcommit: %d total lines for %d distinct screen lines (want <= %d)",
			totalLines, screenDistinct, screenDistinct*2)
	}

	t.Logf("commits=%d total_lines=%d (screen has %d distinct)", commitCount, totalLines, screenDistinct)
}
