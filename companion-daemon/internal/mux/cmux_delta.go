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

// ── Volatile UI Suppression ──
//
// Agent terminals repaint volatile UI elements (timers, spinners,
// status bars, model banners) on every screen refresh. These are not
// semantic output — they are transient repaint artifacts.
//
// suppressVolatileLines removes lines that appear to be volatile UI:
//   - lines that repeat across consecutive deltas (within a window)
//   - lines that differ only by an incrementing number (timer)
//   - lines that are single characters (spinner artifacts)
//
// Returns the filtered delta, or "" if all lines are volatile.

type volatileFilter struct {
	recentLines map[string]int // line → times seen in recent window
	maxRecent   int
}

func newVolatileFilter() *volatileFilter {
	return &volatileFilter{
		recentLines: make(map[string]int),
		maxRecent:   3,
	}
}

// filter removes volatile lines from delta. Returns "" if no semantic
// content remains after filtering.
func (f *volatileFilter) filter(delta string) string {
	lines := splitLines(delta)
	if len(lines) == 0 {
		return ""
	}

	var semantic []string
	volatileCount := 0
	for _, line := range lines {
		if f.isVolatile(line) {
			volatileCount++
			continue
		}
		semantic = append(semantic, line)
	}

	// Update recent lines with new semantic content.
	for _, line := range semantic {
		f.recentLines[line]++
	}
	// Decay old entries.
	for line, count := range f.recentLines {
		if count > 1 {
			f.recentLines[line] = count - 1
		} else {
			delete(f.recentLines, line)
		}
	}

	// If all lines were volatile, suppress the entire delta.
	if len(semantic) == 0 {
		return ""
	}
	return strings.Join(semantic, "\n")
}

// isVolatile returns true if a line appears to be volatile UI rather
// than semantic agent output.
func (f *volatileFilter) isVolatile(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return true // blank lines are not meaningful
	}

	// Single character (spinner artifact): "/", "-", "\", "|"
	if len(line) == 1 {
		return true
	}

	// Lines that repeat across deltas (seen ≥ maxRecent times).
	if f.recentLines[line] >= f.maxRecent {
		return true
	}

	// Timer pattern: "Working (Ns • esc to interrupt)"
	// Normalize by stripping incrementing numbers, then check for repeats.
	normalized := normalizeVolatilePattern(line)
	if normalized != line && f.recentLines[normalized] >= f.maxRecent {
		return true
	}
	// Also store the normalized form for future detection.
	if normalized != line {
		f.recentLines[normalized]++
	}

	return false
}

// normalizeVolatilePattern replaces incrementing numeric patterns with
// a placeholder so that "Working (1s...)", "Working (2s...)" all map
// to the same normalized form.
func normalizeVolatilePattern(line string) string {
	// Replace isolated numbers and timers: "1s", "2s", "3s", "1m", etc.
	// Pattern: digit(s) followed by 's' or 'm' in a timer context.
	result := line
	// Simple: replace runs of digits followed by 's' with "Ns"
	for i := 0; i < len(result); i++ {
		if result[i] >= '0' && result[i] <= '9' {
			j := i
			for j < len(result) && result[j] >= '0' && result[j] <= '9' {
				j++
			}
			if j < len(result) && (result[j] == 's' || result[j] == 'm') {
				result = result[:i] + "N" + result[j:]
				i = i + 1 // skip past "Ns" or "Nm"
			} else {
				i = j
			}
		}
	}
	return result
}

// ── Pending-to-Committed Transcript Model ──
//
// Deltas are not immediately committed to ActivityBuffer. Instead they
// enter a pending state. Only when the output stabilizes (no new delta
// for N consecutive polls) is it committed. Volatile repaint (timers,
// spinners, status bars) never reaches committed transcript.
//
// Two layers:
//  1. Committed: stable semantic text → delta marker → ActivityBuffer
//  2. Pending: transient text, held in memory, replaced on each poll

type transcriptManager struct {
	pending         string // accumulated pending output
	stableCount     int    // consecutive polls with no new semantic delta
	stableThreshold int    // polls before commit (e.g., 2 = 1 second at 500ms)
}

func newTranscriptManager() *transcriptManager {
	return &transcriptManager{stableThreshold: 2}
}

// processDelta decides whether to commit accumulated pending output.
// delta is the new text extracted from the latest screen comparison.
// Returns the text to commit (for ActivityBuffer), or "" if nothing
// should be committed yet.
func (m *transcriptManager) processDelta(delta string) (commitText string) {
	// Filter volatile patterns from the delta.
	semantic := removeVolatileLines(delta)

	if semantic != "" {
		// New semantic output — append to pending, reset stability.
		if m.pending != "" {
			m.pending += "\n" + semantic
		} else {
			m.pending = semantic
		}
		m.stableCount = 0
		return ""
	}

	// No new semantic output. If we have pending content, count
	// stability. Commit when the screen has been stable long enough.
	if m.pending != "" {
		m.stableCount++
		if m.stableCount >= m.stableThreshold {
			commitText = m.pending
			m.pending = ""
			m.stableCount = 0
		}
	}
	return ""
}

// removeVolatileLines strips volatile UI patterns from delta text.
// Returns only lines that appear to be semantic content (messages,
// tool output, logs) rather than transient repaint artifacts
// (timers, spinners, status bars).
func removeVolatileLines(delta string) string {
	lines := splitLines(delta)
	var semantic []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Single character = spinner fragment.
		if len(line) == 1 {
			continue
		}
		// Timer pattern: contains "s •" or ") •" (e.g. "Working (3s • esc...)").
		if isTimerLine(line) {
			continue
		}
		semantic = append(semantic, line)
	}
	if len(semantic) == 0 {
		return ""
	}
	return strings.Join(semantic, "\n")
}

// isTimerLine detects volatile timer/progress indicators.
// Patterns: "Working (Ns • ...)", "Building... Ns", etc.
// Generic: line containing "(digit+s" or "digit+s •" or "digit+s remaining".
func isTimerLine(line string) bool {
	return strings.Contains(line, "s •") ||
		strings.Contains(line, ") •") ||
		strings.Contains(line, "• esc") ||
		strings.Contains(line, "esc to interrupt")
}
