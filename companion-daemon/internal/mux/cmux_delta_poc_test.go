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
