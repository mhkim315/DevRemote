package transcript

import (
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// ByteStreamProjector projects Recorder PTY byte chunks into TranscriptSegment
// values for the generic terminal fallback channel.
//
// Safety rules (handoff §6.3):
//   - Feed copied Recorder chunks through a bounded non-blocking queue.
//   - Stateful UTF-8 and ANSI processing across arbitrary chunk boundaries.
//   - Commit ordinary printable output at stable newline boundaries.
//   - CR progress: retain only safe bounded final state; omit repaint frames.
//   - Alternate-screen/TUI bursts → terminal_ui_omitted marker, never flatten.
//   - Repeated ordinary log lines are preserved (no global dedup).
//   - Terminal input → explicit content-free boundary (echo privacy).
//   - Capture mode routes snapshot/cmux to separate degraded path.
type ByteStreamProjector struct {
	mu sync.Mutex

	// partialLine accumulates text between newline boundaries.
	partialLine strings.Builder

	// utf8Buf holds incomplete UTF-8 sequences across chunk boundaries.
	utf8Buf []byte

	// ansiState tracks whether we are inside a CSI/OSC escape sequence.
	ansiState ansiParseState

	// inputActive suppresses bytes during terminal input (echo privacy).
	// Set by BeginInput, cleared by EndInput or after suppressChunks
	// chunks have been fully suppressed (content-free boundary).
	inputActive     bool
	suppressChunks  int // remaining chunks to suppress after input

	// tuiBurstActive suppresses bytes during TUI/alternate-screen regions.
	tuiBurstActive bool
	tuiBurstCount  int

	// crProgress tracks the most recent CR-terminated progress line.
	// If the next character is not CR, it is committed as final.
	crProgress   strings.Builder
	crActive     bool

	lastFlush       time.Time
	totalProjected  int64
	totalSuppressed int64
	maxQueueBytes   int
	overflowed      bool
}

type ansiParseState int

const (
	ansiNone ansiParseState = iota
	ansiCSI  // inside ESC[...sequence
	ansiOSC  // inside ESC]...sequence
	ansiESC  // single ESC + one char
)

// ByteStreamProjectorConfig controls projector behavior.
type ByteStreamProjectorConfig struct {
	MaxQueueBytes   int
	MaxTUIBurstBytes int
	FlushTimeout    time.Duration
}

func DefaultByteStreamConfig() ByteStreamProjectorConfig {
	return ByteStreamProjectorConfig{
		MaxQueueBytes:   65536,
		MaxTUIBurstBytes: 131072,
		FlushTimeout:    5 * time.Second,
	}
}

func NewByteStreamProjector(cfg ByteStreamProjectorConfig) *ByteStreamProjector {
	if cfg.MaxQueueBytes <= 0 {
		cfg.MaxQueueBytes = 65536
	}
	if cfg.MaxTUIBurstBytes <= 0 {
		cfg.MaxTUIBurstBytes = 131072
	}
	if cfg.FlushTimeout <= 0 {
		cfg.FlushTimeout = 5 * time.Second
	}
	return &ByteStreamProjector{
		maxQueueBytes: cfg.MaxQueueBytes,
		lastFlush:     time.Now(),
	}
}

// Feed processes a chunk of Recorder output bytes and returns segments.
// It handles UTF-8 sequences split across chunks, ANSI escape sequences,
// CR progress lines, and input/TUI suppression statefully.
func (p *ByteStreamProjector) Feed(sessionID string, chunk []byte, observedAt time.Time) []TranscriptSegment {
	p.mu.Lock()
	defer p.mu.Unlock()

	if observedAt.IsZero() {
		observedAt = time.Now()
	}

	// Prepend any incomplete UTF-8 bytes from the previous chunk.
	var data []byte
	if len(p.utf8Buf) > 0 {
		data = append(p.utf8Buf, chunk...)
		p.utf8Buf = nil
	} else {
		data = chunk
	}

	var segments []TranscriptSegment
	i := 0
	for i < len(data) {
		// Input suppression: drop entire chunks for suppressChunks count.
		// This is a content-free boundary: we suppress N Recorder chunks
		// (~4KB) after input, which safely covers typical PTY echo without
		// content matching, timing heuristics, or newline detection.
		if p.inputActive {
			p.totalSuppressed += int64(len(data) - i)
			if p.suppressChunks > 0 {
				p.suppressChunks--
			}
			if p.suppressChunks == 0 {
				p.inputActive = false
			}
			return segments
		}

		// TUI burst suppression.
		if p.tuiBurstActive {
			p.tuiBurstCount += len(data) - i
			return segments
		}

		// ANSI escape handling — skip entire sequence.
		if p.ansiState != ansiNone {
			consumed := p.consumeANSI(data[i:])
			if consumed == 0 {
				// Incomplete ANSI sequence at chunk end — save for next chunk.
				p.utf8Buf = append(p.utf8Buf, data[i:]...)
				return segments
			}
			i += consumed
			continue
		}

		b := data[i]

		// Start of ANSI escape.
		if b == 0x1b && i+1 < len(data) {
			switch data[i+1] {
			case '[':
				p.ansiState = ansiCSI
				i += 2
				continue
			case ']':
				p.ansiState = ansiOSC
				i += 2
				continue
			default:
				p.ansiState = ansiESC
				i += 2
				continue
			}
		}

		// UTF-8 multi-byte sequence — accumulate safely.
		if b >= 0x80 {
			r, size := utf8.DecodeRune(data[i:])
			if r == utf8.RuneError && size <= 1 {
				// Incomplete UTF-8 at chunk boundary.
				p.utf8Buf = append(p.utf8Buf, data[i:]...)
				return segments
			}
			p.writeRune(r)
			i += size
			continue
		}

		// Control characters.
		switch b {
		case '\n':
			segments = append(segments, p.flushLine(sessionID, observedAt)...)

		case '\r':
			// CR progress: capture the line as potential repaint.
			// If followed by non-CR, commit as final. If followed by
			// another CR (double-CR), commit the first.
			line := p.partialLine.String()
			p.partialLine.Reset()
			if p.crActive {
				// Double CR: commit previous CR line.
				if p.crProgress.Len() > 0 {
					segments = append(segments, p.commitCRLine(sessionID, observedAt))
				}
			}
			p.crProgress.Reset()
			if strings.TrimSpace(line) != "" {
				p.crProgress.WriteString(line)
				p.crActive = true
			}

		case 0x08: // backspace
			p.crActive = false
			s := p.partialLine.String()
			if len(s) > 0 {
				p.partialLine.Reset()
				p.partialLine.WriteString(s[:len(s)-1])
			}

		default:
			// Any non-CR character after CR progress commits the CR line as final.
			if p.crActive {
				p.crActive = false
				if p.crProgress.Len() > 0 {
					segments = append(segments, p.commitCRLine(sessionID, observedAt))
				}
			}
			p.partialLine.WriteByte(b)
		}
		i++
	}

	// Flush pending CR progress at chunk end (no more data to resolve repaint).
	if p.crActive && p.crProgress.Len() > 0 {
		segments = append(segments, p.commitCRLine(sessionID, observedAt))
	}

	// Overflow check.
	if p.partialLine.Len() > p.maxQueueBytes {
		p.partialLine.Reset()
		p.crActive = false
		p.crProgress.Reset()
		p.overflowed = true
		segments = append(segments, NewDegradedSegment(sessionID, "byte-stream queue overflow", observedAt))
	}

	if len(segments) > 0 {
		p.lastFlush = observedAt
		p.totalProjected += int64(len(segments))
	}

	return segments
}

// consumeANSI skips bytes until the ANSI sequence ends.
// Returns the number of bytes consumed, or 0 if incomplete.
func (p *ByteStreamProjector) consumeANSI(data []byte) int {
	switch p.ansiState {
	case ansiCSI:
		for i := 0; i < len(data); i++ {
			b := data[i]
			// CSI sequences end with a letter in range 0x40–0x7E.
			if b >= 0x40 && b <= 0x7e {
				p.ansiState = ansiNone
				return i + 1
			}
			// Intermediate bytes 0x20–0x2F are part of CSI.
			if b < 0x20 || b > 0x3f {
				// Malformed — reset.
				p.ansiState = ansiNone
				return i
			}
		}
		return 0 // incomplete

	case ansiOSC:
		for i := 0; i < len(data); i++ {
			b := data[i]
			// OSC ends with BEL (0x07) or ST (ESC \).
			if b == 0x07 {
				p.ansiState = ansiNone
				return i + 1
			}
			if b == 0x1b && i+1 < len(data) && data[i+1] == '\\' {
				p.ansiState = ansiNone
				return i + 2
			}
		}
		return 0 // incomplete

	case ansiESC:
		// Single ESC + one character.
		if len(data) > 0 {
			p.ansiState = ansiNone
			return 1
		}
		return 0

	default:
		p.ansiState = ansiNone
		return 0
	}
}

func (p *ByteStreamProjector) writeRune(r rune) {
	p.crActive = false
	p.partialLine.WriteRune(r)
}

// flushLine commits the accumulated partial line (LF-terminated).
func (p *ByteStreamProjector) flushLine(sessionID string, observedAt time.Time) []TranscriptSegment {
	var segments []TranscriptSegment

	// If there was a pending CR progress line, commit it first.
	if p.crActive && p.crProgress.Len() > 0 {
		segments = append(segments, p.commitCRLine(sessionID, observedAt))
	}
	p.crActive = false

	line := strings.TrimRight(p.partialLine.String(), "\r")
	p.partialLine.Reset()
	if trimmed := strings.TrimSpace(line); trimmed != "" {
		segments = append(segments, NewTerminalOutputSegment(sessionID, trimmed, len(line), observedAt))
	}
	return segments
}

// commitCRLine commits a CR progress line as final output.
func (p *ByteStreamProjector) commitCRLine(sessionID string, observedAt time.Time) TranscriptSegment {
	text := strings.TrimSpace(p.crProgress.String())
	p.crProgress.Reset()
	p.crActive = false
	if text == "" {
		return TranscriptSegment{}
	}
	return NewTerminalOutputSegment(sessionID, text, len(text), observedAt)
}

// ── Input boundary (echo privacy) ──

func (p *ByteStreamProjector) BeginInput(sessionID string, observedAt time.Time) *TranscriptSegment {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.partialLine.Reset()
	p.crProgress.Reset()
	p.crActive = false
	p.inputActive = true
	p.suppressChunks = 4 // suppress next 4 Recorder chunks (~4KB echo window)
	boundary := NewInputBoundarySegment(sessionID, observedAt)
	return &boundary
}

func (p *ByteStreamProjector) EndInput(sessionID string, observedAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inputActive = false
	p.suppressChunks = 0
}

// ── TUI burst ──

func (p *ByteStreamProjector) BeginTUIBurst() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tuiBurstActive = true
	p.tuiBurstCount = 0
}

func (p *ByteStreamProjector) EndTUIBurst(sessionID string, observedAt time.Time) *TranscriptSegment {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tuiBurstActive = false
	if p.tuiBurstCount > 0 {
		p.tuiBurstCount = 0
		seg := NewUIOmittedSegment(sessionID, observedAt)
		return &seg
	}
	return nil
}

// ── Flush ──

func (p *ByteStreamProjector) Flush(sessionID string, observedAt time.Time) []TranscriptSegment {
	p.mu.Lock()
	defer p.mu.Unlock()

	var segments []TranscriptSegment

	// Commit any pending CR progress.
	if p.crActive && p.crProgress.Len() > 0 {
		seg := p.commitCRLine(sessionID, observedAt)
		if seg.Kind != "" {
			segments = append(segments, seg)
		}
	}

	// Flush partial line.
	if p.partialLine.Len() > 0 {
		line := strings.TrimRight(p.partialLine.String(), "\r")
		p.partialLine.Reset()
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			segments = append(segments, NewTerminalOutputSegment(sessionID, trimmed, len(line), observedAt))
		}
	}

	// Clear ANSI/UTF-8 state.
	p.ansiState = ansiNone
	p.utf8Buf = nil

	if p.overflowed {
		p.overflowed = false
		segments = append(segments, NewDegradedSegment(sessionID, "byte-stream queue overflow (recovered)", observedAt))
	}

	return segments
}

// ── Diagnostics ──

func (p *ByteStreamProjector) Diagnostics() ByteStreamDiag {
	p.mu.Lock()
	defer p.mu.Unlock()
	return ByteStreamDiag{
		PartialLen:     p.partialLine.Len(),
		InputActive:    p.inputActive,
		TUIBurstActive: p.tuiBurstActive,
		TotalProjected: p.totalProjected,
		TotalSuppressed: p.totalSuppressed,
		Overflowed:     p.overflowed,
	}
}

type ByteStreamDiag struct {
	PartialLen      int
	InputActive     bool
	TUIBurstActive  bool
	TotalProjected  int64
	TotalSuppressed int64
	Overflowed      bool
}

// ── ANSI/TUI detection helpers (used by Recorder for routing) ──

func IsAlternateScreenStart(chunk []byte) bool {
	return len(chunk) >= 8 &&
		chunk[0] == 0x1b && chunk[1] == '[' && chunk[2] == '?' &&
		chunk[3] == '1' && chunk[4] == '0' && chunk[5] == '4' &&
		chunk[6] == '9' && chunk[7] == 'h'
}

func IsAlternateScreenEnd(chunk []byte) bool {
	return len(chunk) >= 8 &&
		chunk[0] == 0x1b && chunk[1] == '[' && chunk[2] == '?' &&
		chunk[3] == '1' && chunk[4] == '0' && chunk[5] == '4' &&
		chunk[6] == '9' && chunk[7] == 'l'
}

func HasClearScreen(chunk []byte) bool {
	for i := 0; i <= len(chunk)-4; i++ {
		if chunk[i] == 0x1b && chunk[i+1] == '[' && chunk[i+2] == '2' && chunk[i+3] == 'J' {
			return true
		}
	}
	return false
}
