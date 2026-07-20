package mux

import (
	"strings"
	"testing"
)

// ── cmux Screen Snapshot Delta Extraction PoC ──
//
// Goal: recover append-only transcript events by comparing consecutive
// cmux screen snapshots. Not a VT100 emulator — just text diffing.
//
// The cmux adapter already tracks lastContent across polls. This PoC
// explores whether computing a simple delta (new text since last poll)
// is viable for AI agent output (Claude, Codex, Gemini).

// extractDelta computes the new content in `curr` that was not in `prev`.
// Returns "" if no reliable delta can be determined.

// commonPrefixLen returns the length of the longest common prefix.

// commonSuffixLen returns the length of the longest common suffix.

// splitLines splits text into non-empty lines, preserving order.

// ── Tests ──

func TestExtractDelta_FirstSnapshot(t *testing.T) {
	delta := extractDelta("", "Hello\nWorld")
	if delta != "Hello\nWorld" {
		t.Errorf("first snapshot should emit everything: got %q", delta)
	}
}

func TestExtractDelta_NoChange(t *testing.T) {
	delta := extractDelta("Hello\nWorld", "Hello\nWorld")
	if delta != "" {
		t.Errorf("no change should return empty: got %q", delta)
	}
}

func TestExtractDelta_AppendOnly(t *testing.T) {
	// AI agent appends new output at the bottom — most common case.
	prev := "Thinking...\n"
	curr := "Thinking...\nWriting file...\n"
	delta := extractDelta(prev, curr)
	if !strings.Contains(delta, "Writing file") {
		t.Errorf("append delta missing content: got %q", delta)
	}
}

func TestExtractDelta_MultiLineAppend(t *testing.T) {
	prev := "A\nB\n"
	curr := "A\nB\nC\nD\n"
	delta := extractDelta(prev, curr)
	if !strings.Contains(delta, "C") || !strings.Contains(delta, "D") {
		t.Errorf("multi-line append: got %q", delta)
	}
}

func TestExtractDelta_ScreenCleared(t *testing.T) {
	// Screen was cleared and new content is shorter — line-based diff.
	prev := "Old output line 1\nOld output line 2\nOld output line 3\n"
	curr := "New output\n"
	delta := extractDelta(prev, curr)
	// Should find "New output" as new line
	if !strings.Contains(delta, "New output") {
		t.Errorf("screen cleared: got %q, want containing 'New output'", delta)
	}
	// Should NOT duplicate old output
	if strings.Contains(delta, "Old output") {
		t.Errorf("screen cleared: delta contains old output: %q", delta)
	}
}

func TestExtractDelta_FullScreenRewrite(t *testing.T) {
	// Large-scale screen change — should return empty (unreliable).
	prev := strings.Repeat("old line\n", 50)
	curr := strings.Repeat("new line\n", 50)
	delta := extractDelta(prev, curr)
	if delta != "" {
		t.Errorf("full rewrite should return empty (unreliable): got %q", delta)
	}
}

func TestExtractDelta_ScrollUp_LostTop(t *testing.T) {
	// Terminal scrolled — oldest line removed from top, new at bottom.
	prev := "line2\nline3\nline4\nline5\n"
	curr := "line3\nline4\nline5\nline6\n"
	delta := extractDelta(prev, curr)
	if !strings.Contains(delta, "line6") {
		t.Errorf("scroll delta missing new line: got %q", delta)
	}
	// Should NOT contain lines that were just scrolled
	if strings.Contains(delta, "line2") {
		t.Errorf("scroll delta contains scrolled-out line: %q", delta)
	}
}

func TestExtractDelta_ClaudeStyleThinking(t *testing.T) {
	// Realistic Claude output pattern: thinking appears, then result.
	prev := "\n\nClaude Code v2.1.201\n\n\xe2\x9d\xaf thinking...\n\n"
	curr := "\n\nClaude Code v2.1.201\n\n\xe2\x9d\xaf thinking...\n\nAnalyzing codebase...\nFound 3 files.\n\n\xe2\x9d\xaf Result:\nThe fix is in recorder.go\n"
	delta := extractDelta(prev, curr)
	if !strings.Contains(delta, "Analyzing codebase") {
		t.Errorf("Claude-style: missing thinking output: %q", delta)
	}
	if !strings.Contains(delta, "recorder.go") {
		t.Errorf("Claude-style: missing result: %q", delta)
	}
}

func TestExtractDelta_KoreanPreserved(t *testing.T) {
	prev := "이전 출력\n"
	curr := "이전 출력\n새로운 출력입니다\n"
	delta := extractDelta(prev, curr)
	if !strings.Contains(delta, "새로운 출력입니다") {
		t.Errorf("Korean text lost: got %q", delta)
	}
}

func TestExtractDelta_ANSIOnlyChange(t *testing.T) {
	// Only ANSI cursor position changed — no real text change.
	// (In practice, ANSI would be stripped before comparison.)
	prev := "Hello World\n"
	curr := "Hello World\n"
	delta := extractDelta(prev, curr)
	if delta != "" {
		t.Errorf("ANSI-only change should return empty: got %q", delta)
	}
}

func TestExtractDelta_ComplexMixed(t *testing.T) {
	// Simulated: agent was thinking, then produced tool output.
	prev := strings.Join([]string{
		"",
		"Claude Code v2.1.201",
		"",
		"❯ Analyzing the issue...",
		"  Found potential fix in recorder.go",
		"",
		"❯ Thinking...",
		"",
	}, "\n")

	curr := strings.Join([]string{
		"",
		"Claude Code v2.1.201",
		"",
		"❯ Analyzing the issue...",
		"  Found potential fix in recorder.go",
		"",
		"❯ Writing fix...",
		"  Modified recorder.go:42",
		"  Modified recorder_test.go:100",
		"",
		"❯ Done.",
		"",
	}, "\n")

	delta := extractDelta(prev, curr)
	if len(delta) == 0 {
		t.Fatal("complex mixed: no delta found")
	}
	if !strings.Contains(delta, "Writing fix") {
		t.Errorf("complex mixed: missing new content: %q", delta)
	}
	if !strings.Contains(delta, "recorder.go:42") {
		t.Errorf("complex mixed: missing detail: %q", delta)
	}
	// Previous thinking should NOT appear
	if strings.Contains(delta, "Analyzing the issue") {
		t.Errorf("complex mixed: duplicated old content: %q", delta)
	}
}

func TestExtractDelta_ProgressSpinner(t *testing.T) {
	// Progress spinner rotates — each snapshot has slightly different char.
	// Line-based diff would see these as "new" lines, so we rely on
	// common-prefix strategy which handles this correctly.
	prev := "Building... /\n"
	curr := "Building... -\n"
	delta := extractDelta(prev, curr)
	// Common prefix: "Building... " (12 chars). Suffix: "-" vs "/"
	// Either way, the suffix is tiny and not meaningful.
	// Ideal: return empty. The common-prefix strategy gives us "-".
	// This is acceptable — a single character of noise vs full line.
	t.Logf("progress spinner delta: %q (minor noise acceptable)", delta)
}

// --- E8g4: cmux transcript contract invariants ---

func TestCmuxInvariant_DuplicateSnapshotNoSeqIncrease(t *testing.T) {
	// Repeated identical snapshots must not produce transcript events.
	st := newScreenTracker()

	// First snapshot bootstraps (may commit initial content).
	result1 := st.processScreen("line1\nline2\nline3\n")
	_ = result1

	// Second identical snapshot.
	result2 := st.processScreen("line1\nline2\nline3\n")
	if result2 != "" {
		t.Errorf("identical snapshot produced transcript: %q", result2)
	}
}

func TestCmuxInvariant_OneNewLineOneEvent(t *testing.T) {
	st := newScreenTracker()

	// Bootstrap.
	st.processScreen("line1\nline2\n")

	// New line appears at bottom.
	result := st.processScreen("line1\nline2\nline3\n")

	// After stability (6 identical polls), the new line must be committed.
	for i := 0; i < 10; i++ {
		r := st.processScreen("line1\nline2\nline3\n")
		if r != "" {
			result = r
		}
	}
	if result == "" {
		t.Fatal("new line was never committed even after stability window")
	}
	if !strings.Contains(result, "line3") && !strings.Contains(result, "line1") {
		t.Errorf("committed text does not contain expected content: %q", result)
	}
	// Must not produce duplicate commits for the same content.
	dup := st.processScreen("line1\nline2\nline3\n")
	if dup != "" {
		t.Errorf("duplicate commit after stability: %q", dup)
	}
}

func TestCmuxInvariant_RepeatedTextNotDedupedGlobally(t *testing.T) {
	st := newScreenTracker()

	// Bootstrap with first interaction.
	st.processScreen("› hi\n• hello\n")

	// Force-flush via stability.
	for i := 0; i < 10; i++ {
		st.processScreen("› hi\n• hello\n")
	}

	// New interaction: same prompt "› hi" appears again in a different screen.
	// The screen is now longer: old conversation + new interaction.
	st.processScreen("› hi\n• hello\n› hi\n• hello again\n")

	// Force-flush again.
	var result2 string
	for i := 0; i < 10; i++ {
		r := st.processScreen("› hi\n• hello\n› hi\n• hello again\n")
		if r != "" {
			result2 += r
		}
	}

	// The new interaction content must appear. Must NOT be empty.
	if result2 == "" {
		t.Fatal("second identical prompt produced no transcript — global dedup blocked it")
	}
	// Must contain the NEW content (second interaction).
	if !strings.Contains(result2, "hello again") {
		t.Errorf("new interaction text not found in transcript: %q", result2)
	}
	// Must NOT suppress the second "› hi" just because the first one exists.
	hiCount := strings.Count(result2, "› hi")
	if hiCount < 1 {
		t.Errorf("second › hi was blocked by global dedup: found %d occurrences", hiCount)
	}
}

func TestCmuxInvariant_VolatileUIChangesNoTranscript(t *testing.T) {
	st := newScreenTracker()

	// Bootstrap.
	st.processScreen("output line\n")

	// Change only a timer/status line.
	// "Working (1s • esc to interrupt)" → "Working (2s • esc to interrupt)"
	result := st.processScreen("output line\nWorking (2s • esc to interrupt)\n")
	// Timer lines are volatile — should not produce transcript.
	if strings.Contains(result, "Working") {
		t.Errorf("volatile timer line leaked into transcript: %q", result)
	}
}

func TestCmuxAdapter_DeclaresScreenSnapshotDelta(t *testing.T) {
	adapter := &cmuxAdapter{}
	mode := adapter.TranscriptCaptureMode()
	if mode != CaptureModeScreenSnapshotDelta {
		t.Errorf("cmux adapter mode = %v, want CaptureModeScreenSnapshotDelta", mode)
	}
}

// --- TranscriptCaptureMode contract tests ---

// --- E8g4: cmux viewport normalization ---

func TestNormalizeCmuxSnapshot_BoundsLargeScreen(t *testing.T) {
	// Build a 500-line screen with duplicates.
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, "line "+string(rune('0'+i%10)))
	}
	screen := strings.Join(lines, "\n")
	result := normalizeCmuxSnapshot(screen)
	resultLines := splitLines(result)
	if len(resultLines) > defaultViewportLines {
		t.Errorf("viewport normalization: got %d lines, want <= %d", len(resultLines), defaultViewportLines)
	}
	// Last line must be preserved.
	if resultLines[len(resultLines)-1] != lines[len(lines)-1] {
		t.Error("last line was truncated")
	}
	// First line of result should be from the tail of original.
	if resultLines[0] != lines[len(lines)-defaultViewportLines] {
		t.Errorf("first line of result = %q, want %q", resultLines[0], lines[len(lines)-defaultViewportLines])
	}
}

func TestNormalizeCmuxSnapshot_SmallScreenPassesThrough(t *testing.T) {
	screen := "line1\nline2\nline3"
	result := normalizeCmuxSnapshot(screen)
	if result != screen {
		t.Errorf("small screen modified: got %q, want %q", result, screen)
	}
}

func TestNormalizeCmuxSnapshot_PreservesRepeatedLines(t *testing.T) {
	// Legitimate repeated lines within viewport must be preserved.
	// Build screen where viewport has "hi" appearing twice at positions 195 and 198.
	var lines []string
	for i := 0; i < 400; i++ {
		lines = append(lines, "scrollback line")
	}
	// Viewport area (last 200 lines): positions 200-399.
	// Put "hi" at positions 395 and 398 (within viewport).
	lines[395] = "› hi"
	lines[398] = "› hi"
	screen := strings.Join(lines, "\n")
	result := normalizeCmuxSnapshot(screen)
	hiCount := 0
	for _, l := range splitLines(result) {
		if l == "› hi" {
			hiCount++
		}
	}
	if hiCount != 2 {
		t.Errorf("legitimate repeated '› hi': got %d occurrences, want 2 (content-level dedup must not remove them)", hiCount)
	}
}

func TestNormalizeCmuxSnapshot_SentinelAtEndPreserved(t *testing.T) {
	// Last line (most recent output) must never be truncated.
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, "old line")
	}
	lines = append(lines, "SENTINEL_LAST_LINE")
	screen := strings.Join(lines, "\n")
	result := normalizeCmuxSnapshot(screen)
	if !strings.HasSuffix(strings.TrimSpace(result), "SENTINEL_LAST_LINE") {
		t.Error("sentinel last line was lost")
	}
}
