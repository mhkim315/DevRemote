package mux

import "strings"

// ── cmux Snapshot Delta Extraction (E8g4) ──
//
// extractDelta recovers append-only output by comparing consecutive
// cmux screen snapshots. Designed for AI agent output (Claude, Codex,
// Gemini) which is primarily append-oriented.
//
// Returns "" when no reliable delta can be determined (full-screen
// repaints, complex TUI rendering, etc.).

func extractDelta(prev, curr string) string {
	if prev == "" {
		return curr
	}
	if curr == prev {
		return ""
	}

	// Strategy A: common-prefix suffix.
	prefixLen := commonPrefixLen(prev, curr)
	if prefixLen > 0 {
		suffix := curr[prefixLen:]
		if len(suffix) > 0 && len(suffix) < (len(curr)*4/5) {
			return suffix
		}
	}

	// Strategy B: line-based diff.
	prevLines := splitLines(prev)
	currLines := splitLines(curr)

	prevSet := make(map[string]bool, len(prevLines))
	for _, l := range prevLines {
		prevSet[l] = true
	}

	var newLines []string
	for _, l := range currLines {
		if l == "" {
			continue
		}
		if !prevSet[l] {
			newLines = append(newLines, l)
		}
	}

	if len(newLines) > 0 {
		if len(newLines) <= 5 {
			return strings.Join(newLines, "\n")
		}
		if len(newLines) <= len(currLines)/2 {
			return strings.Join(newLines, "\n")
		}
		if len(currLines) <= 15 && len(newLines) <= len(currLines)*3/4 {
			return strings.Join(newLines, "\n")
		}
	}

	// Strategy C: common-suffix prefix (scrolled output).
	suffixLen := commonSuffixLen(prev, curr)
	if suffixLen > 0 {
		prefix := curr[:len(curr)-suffixLen]
		if len(prefix) > 0 && len(prefix) < (len(curr)*4/5) {
			return prefix
		}
	}

	return ""
}

func commonPrefixLen(a, b string) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return minLen
}

func commonSuffixLen(a, b string) int {
	i, j := len(a)-1, len(b)-1
	count := 0
	for i >= 0 && j >= 0 {
		if a[i] != b[j] {
			break
		}
		i--
		j--
		count++
	}
	return count
}

func splitLines(s string) []string {
	raw := strings.Split(s, "\n")
	var out []string
	for _, l := range raw {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// stripMuxANSI removes ANSI escape sequences from text.
// Duplicated from term.stripANSI to avoid circular import.
func stripMuxANSI(s string) string {
	var out []byte
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) {
			switch s[i+1] {
			case '[': // CSI: ESC[ ... letter
				j := i + 2
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					j++
				}
				i = j
				continue
			case ']': // OSC: ESC] ... BEL/ST
				j := i + 2
				for j < len(s) && s[j] != 0x07 && !(s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\') {
					j++
				}
				if j < len(s) {
					if s[j] == 0x07 {
						j++
					} else {
						j += 2
					}
				}
				i = j
				continue
			default:
				i += 2
				if i < len(s) {
					i++
				}
				continue
			}
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}

// ── Stable Prefix + Mutable Tail Transcript Model ──
//
// cmux screens are polled as full snapshots. Rather than committing
// every diff, we track the screen at line level:
//
//   Stable Prefix  — top lines unchanged across polls (committed when they scroll out)
//   Mutable Tail   — bottom lines that change each poll (projected, not committed)
//
// Lines are committed to ActivityBuffer only when they:
//   1. Scroll out of the current screen (no longer visible), OR
//   2. Remain in the stable prefix for N consecutive polls
//
// Mutable tail lines update in place and are never committed directly.
// This prevents timer/status/spinner spam in Transcript.

type screenTracker struct {
	prevLines    []string // previous poll's screen lines
	committed    []string // lines already committed (prevent duplicates)
	stableCount  int      // consecutive polls with identical stable prefix
	commitThreshold int   // polls before committing stable prefix edges
}

func newScreenTracker() *screenTracker {
	return &screenTracker{commitThreshold: 3} // ~1.5 seconds at 500ms poll
}

// processScreen compares the current screen against the previous one.
// Returns lines to commit to ActivityBuffer (via delta marker).
func (s *screenTracker) processScreen(currentContent string) (commitText string) {
	currLines := splitLines(currentContent)
	if len(currLines) == 0 {
		return ""
	}

	// First poll: bootstrap from the existing screen content.
	// Filter out volatile lines, keep the rest as initial transcript.
	if s.prevLines == nil {
		s.prevLines = currLines
		var initial []string
		const maxBootstrap = 200
		start := 0
		if len(currLines) > maxBootstrap {
			start = len(currLines) - maxBootstrap
		}
		for _, line := range currLines[start:] {
			line = strings.TrimSpace(line)
			if line == "" || isTimerLine(line) || isVolatileLine(line) || isStatusPanelLine(line) {

				continue
			}
			if !s.isCommitted(line) {
				initial = append(initial, line)
				s.committed = append(s.committed, line)
			}
		}
		if len(initial) > 0 {
			return normalizeTranscript(strings.Join(initial, "\n"))
		}
		return ""
	}

	// Find common prefix: lines at the TOP that are identical.
	prefixLen := 0
	for i := 0; i < len(s.prevLines) && i < len(currLines); i++ {
		if s.prevLines[i] != currLines[i] {
			break
		}
		prefixLen = i + 1
	}

	// Track screen stability.
	if prefixLen == len(s.prevLines) && prefixLen == len(currLines) {
		s.stableCount++
	} else {
		s.stableCount = 0
	}

	var toCommit []string

	// Lines that scrolled out of the stable prefix → commit.
	if prefixLen < len(s.prevLines) {
		for _, line := range s.prevLines[prefixLen:] {
			line = strings.TrimSpace(line)
			if line == "" || isTimerLine(line) || isVolatileLine(line) || isStatusPanelLine(line) {

				continue
			}
			if !s.isCommitted(line) {
				toCommit = append(toCommit, line)
				s.committed = append(s.committed, line)
			}
		}
	}

	// Force-flush: screen completely idle for 10 polls (~5s) →
	// commit ALL non-volatile lines from the current screen.
	const forceFlushPolls = 6
	if s.stableCount >= forceFlushPolls {
		s.stableCount = 0
		for _, line := range currLines {
			line = strings.TrimSpace(line)
			if line == "" || isTimerLine(line) || isVolatileLine(line) || isStatusPanelLine(line) {

				continue
			}
			if !s.isCommitted(line) {
				toCommit = append(toCommit, line)
				s.committed = append(s.committed, line)
			}
		}
	}

	s.prevLines = currLines

	// Trim committed buffer.
	if len(s.committed) > 100 {
		s.committed = s.committed[len(s.committed)-50:]
	}

	if len(toCommit) > 0 {
		text := normalizeTranscript(strings.Join(toCommit, "\n"))
		if strings.TrimSpace(text) != "" {
			return text
		}
	}
	return ""
}
// isCommitted prevents duplicate commits within the same snapshot
// polling sequence. Does NOT use global text matching — identical
// messages across different interactions are NOT deduplicated.
// Only suppresses repeated renders from the same screen snapshot.
// looksLikeSemanticContent returns true if the line appears to be
// natural language content rather than UI/status/decoration.
func looksLikeSemanticContent(line string) bool {
	// Korean Unicode range
	for _, r := range line {
		if r >= 0xAC00 && r <= 0xD7AF {
			return true
		}
	}
	// English sentence: starts with capital letter, has spaces, reasonable length
	if len(line) > 10 {
		hasSpace := strings.Contains(line, " ")
		firstChar := rune(line[0])
		if hasSpace && firstChar >= 'A' && firstChar <= 'Z' {
			return true
		}
	}
	return false
}


// joinNonVolatile filters and joins lines, skipping volatile ones.
func joinNonVolatile(lines []string) string {
	var kept []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || isTimerLine(line) || isVolatileLine(line) || isStatusPanelLine(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func (s *screenTracker) isCommitted(line string) bool {
	// Never block semantic markers — these are always new events.
	if strings.HasPrefix(line, "›") || strings.HasPrefix(line, "•") ||
		strings.HasPrefix(line, "⎿") ||
		strings.HasPrefix(line, "❯") || strings.HasPrefix(line, "⏺") {
		return false
	}
	// Allow lines that look like natural language (Korean/English sentences).
	// These are continuation lines without explicit markers.
	if looksLikeSemanticContent(line) {
		return false
	}
	// Small window dedup: only check last 20 committed lines.
	// This catches timer/status repaint within a single polling sequence
	// without blocking identical messages across interactions.
	recentStart := len(s.committed) - 20
	if recentStart < 0 {
		recentStart = 0
	}
	for _, c := range s.committed[recentStart:] {
		if c == line {
			return true
		}
	}
	return false
}

// isVolatileLine returns true if the line is a transient UI element
// (banner, model indicator, status bar hint) — not semantic output.
func isVolatileLine(line string) bool {
	// Lines that are purely decorative separators.
	if strings.Count(line, "─") > 10 || strings.Count(line, "━") > 10 ||
		strings.Count(line, "▔") > 10 || strings.Count(line, "▀") > 10 {
		return true
	}
	// Status bar with key hints: "? for shortcuts · ← for agents"
	if strings.Contains(line, "? for shortcuts") || strings.Contains(line, "← for agents") {
		return true
	}
	// Model/version banners.
	if strings.Contains(line, "Claude Code") || strings.Contains(line, "Opus") ||
		strings.Contains(line, "Brewed") || strings.Contains(line, "Churned") ||
		strings.Contains(line, "Sautéed") || strings.Contains(line, "Thought for") ||
		strings.Contains(line, "gpt-") || strings.Contains(line, "API Usage") {
		return true
	}
	// Prompt indicators.
	if strings.Contains(line, "esc to interrupt") || strings.Contains(line, "Use /skills") {
		return true
	}
	// Status panels with box-drawing and usage stats.
	if isStatusPanelLine(line) {
		return true
	}
	return false
}

// isTimerLine detects volatile timer/progress indicators.
func isTimerLine(line string) bool {
	// Claude Code thinking indicators
	if strings.HasPrefix(strings.TrimSpace(line), "✻") {
		return true
	}
	return strings.Contains(line, "s •") ||
		strings.Contains(line, ") •") ||
		strings.Contains(line, "• esc") ||
		strings.Contains(line, "esc to interrupt")
}

// isStatusPanelLine detects box-drawing status panels common across agents.
func isStatusPanelLine(line string) bool {
	// Box-drawing panel rows: lines containing │ at both ends or
	// bordered by ╭╮╰╯. These are UI panels, not semantic output.
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "│") || strings.HasSuffix(trimmed, "│") {
		return true
	}
	// Panel borders: ╭ ╮ ╰ ╯
	if strings.HasPrefix(trimmed, "╭") || strings.HasPrefix(trimmed, "╰") {
		return true
	}
	// Progress bars
	if strings.Count(line, "█") > 5 || strings.Count(line, "░") > 5 {
		return true
	}
	// Usage/billing lines
	if strings.Contains(line, "API Usage") || strings.Contains(line, "Billing") ||
		strings.Contains(line, "for shortcuts") || strings.Contains(line, "← for agents") {
		return true
	}
	return false
}

// normalizeTranscript applies boundary formatting to committed text.
// Ensures user prompts (›), assistant responses (•), and tool output (⎿)
// each start on their own line. Collapses excessive blank lines.
func normalizeTranscript(text string) string {
	if text == "" {
		return ""
	}

	// Step 1: Ensure message markers start on new lines.
	// › = user prompt, • = assistant bullet, ⎿ = terminal continuation
	text = ensureNewlineBefore(text, "› ")
	text = ensureNewlineBefore(text, "• ")
	text = ensureNewlineBefore(text, "⎿ ")
	text = ensureNewlineBefore(text, "❯ ")
	text = ensureNewlineBefore(text, "⏺ ")

	// Step 2: Clean up each line.
	lines := strings.Split(text, "\n")
	var out []string
	prevBlank := false
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)

		// Skip volatile footer/banner lines that may have leaked through.
		if isVolatileLine(trimmed) || isTimerLine(trimmed) || isStatusPanelLine(trimmed) {
			continue
		}

		// Collapse multiple blank lines into one.
		if trimmed == "" {
			if !prevBlank && len(out) > 0 {
				out = append(out, "")
				prevBlank = true
			}
			continue
		}
		prevBlank = false
		out = append(out, trimmed)
	}

	// Step 3: Remove trailing blank lines.
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}

	return strings.Join(out, "\n")
}

// ensureNewlineBefore ensures marker starts on a new line.
// If marker appears mid-line, insert a newline before it.
func ensureNewlineBefore(text, marker string) string {
	var result []byte
	for i := 0; i < len(text); {
		// Check for marker at this position.
		if i+len(marker) <= len(text) && text[i:i+len(marker)] == marker {
			// If not at start of line (preceded by non-newline char),
			// insert a newline before the marker.
			if i > 0 && text[i-1] != '\n' {
				result = append(result, '\n')
			}
			result = append(result, marker...)
			i += len(marker)
			continue
		}
		result = append(result, text[i])
		i++
	}
	return string(result)
}
