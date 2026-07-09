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
