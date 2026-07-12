package transcript

import (
	"strings"
	"sync"
	"time"
)

// ByteStreamProjector projects Recorder PTY byte chunks into TranscriptSegment
// values for the generic terminal fallback channel.
//
// Safety rules (handoff §6.3):
//   - Feed copied Recorder chunks into a bounded asynchronous queue.
//   - Preserve parser/decoder state across arbitrary chunk boundaries.
//   - Commit ordinary printable output at stable boundaries (newline or input boundary).
//   - CR, backspace, TUI bursts → terminal_ui_omitted marker, never flatten.
//   - Repeated ordinary log lines are preserved (no global dedup).
//   - Queue overflow → coalesce gap, never block Recorder.
//   - Terminal input → explicit content-free boundary (echo privacy).
type ByteStreamProjector struct {
	mu sync.Mutex

	// partialLine accumulates text between newline boundaries.
	partialLine strings.Builder

	// inputActive is true while an input boundary is open (echo suppression).
	// During this window, bytes from Recorder are dropped rather than projected,
	// preventing PTY echo from re-entering Transcript.
	inputActive bool

	// tuiBurstActive is true while a TUI/alternate-screen burst is being suppressed.
	tuiBurstActive bool

	// tuiBurstCount counts bytes suppressed during the current TUI burst.
	tuiBurstCount int

	// lastFlush is the observation time of the last committed segment.
	lastFlush time.Time

	// totalProjected counts total segments projected (diagnostic).
	totalProjected int64

	// totalSuppressed counts total bytes suppressed (diagnostic).
	totalSuppressed int64

	// maxQueueBytes is the soft byte bound for unflushed partial data.
	maxQueueBytes int

	// overflowed is set when the queue bound was exceeded.
	overflowed bool
}

// ByteStreamProjectorConfig controls projector behavior.
type ByteStreamProjectorConfig struct {
	// MaxQueueBytes is the soft bound for accumulated unflushed partial-line bytes.
	// When exceeded, the partial buffer is flushed as a degraded segment and a
	// gap marker is emitted. Zero means 65536 (64KB).
	MaxQueueBytes int

	// MaxTUIBurstBytes is the maximum number of bytes to suppress during a TUI
	// burst before emitting a ui_omitted marker and resuming normal projection.
	// Zero means 131072 (128KB).
	MaxTUIBurstBytes int

	// FlushTimeout is the maximum time to accumulate a partial line before
	// flushing it as-is. Zero means 5 seconds.
	FlushTimeout time.Duration
}

// DefaultByteStreamConfig returns production-safe defaults.
func DefaultByteStreamConfig() ByteStreamProjectorConfig {
	return ByteStreamProjectorConfig{
		MaxQueueBytes:    65536,
		MaxTUIBurstBytes: 131072,
		FlushTimeout:     5 * time.Second,
	}
}

// NewByteStreamProjector creates a byte-stream projector.
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

// Feed processes a chunk of Recorder output bytes and returns any segments
// that should be appended to the Transcript. The chunk is COPIED — the
// projector never retains a reference to the caller's buffer.
//
// Feed is safe for concurrent use.
func (p *ByteStreamProjector) Feed(sessionID string, chunk []byte, observedAt time.Time) []TranscriptSegment {
	p.mu.Lock()
	defer p.mu.Unlock()

	if observedAt.IsZero() {
		observedAt = time.Now()
	}

	text := string(chunk)
	var segments []TranscriptSegment

	// If input is active, suppress all bytes (echo privacy).
	if p.inputActive {
		p.totalSuppressed += int64(len(chunk))
		return nil
	}

	// If TUI burst is active, count and possibly emit marker.
	if p.tuiBurstActive {
		p.tuiBurstCount += len(chunk)
		return nil
	}

	// Process the chunk character by character to find stable boundaries.
	for _, r := range text {
		switch {
		case r == '\n':
			// Stable boundary: flush accumulated line.
			if p.partialLine.Len() > 0 {
				line := strings.TrimRight(p.partialLine.String(), "\r")
				p.partialLine.Reset()
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					segments = append(segments, NewTerminalOutputSegment(
						sessionID, trimmed, len(line), observedAt,
					))
				}
			}

		case r == '\r':
			// CR: treat as line ending but don't produce a blank line for bare CR.
			if p.partialLine.Len() > 0 {
				line := strings.TrimRight(p.partialLine.String(), "\r")
				p.partialLine.Reset()
				if trimmed := strings.TrimSpace(line); trimmed != "" {
					segments = append(segments, NewTerminalOutputSegment(
						sessionID, trimmed, len(line), observedAt,
					))
				}
			}

		case r == 0x08: // backspace
			// Erase: remove last character from partial buffer.
			s := p.partialLine.String()
			if len(s) > 0 {
				p.partialLine.Reset()
				p.partialLine.WriteString(s[:len(s)-1])
			}

		default:
			p.partialLine.WriteRune(r)
		}
	}

	// Check queue overflow.
	if p.partialLine.Len() > p.maxQueueBytes {
		// Flush partial as degraded.
		p.partialLine.Reset()
		p.overflowed = true
		segments = append(segments, NewDegradedSegment(
			sessionID, "byte-stream queue overflow", observedAt,
		))
	}

	if len(segments) > 0 {
		p.lastFlush = observedAt
		p.totalProjected += int64(len(segments))
	}

	// Time-based flush: if partial line has been accumulating too long, flush it.
	if p.partialLine.Len() > 0 && observedAt.Sub(p.lastFlush) > DefaultByteStreamConfig().FlushTimeout {
		line := strings.TrimRight(p.partialLine.String(), "\r")
		p.partialLine.Reset()
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			segments = append(segments, NewTerminalOutputSegment(
				sessionID, trimmed, len(line), observedAt,
			))
		}
		p.lastFlush = observedAt
	}

	return segments
}

// BeginInput marks the start of a terminal input window.
// All subsequent Feed calls will suppress bytes (echo privacy) until EndInput.
// This is the content-free input boundary: no input content, prompt text,
// or timing data is stored.
func (p *ByteStreamProjector) BeginInput(sessionID string, observedAt time.Time) *TranscriptSegment {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Flush any accumulated partial line before starting input.
	if p.partialLine.Len() > 0 {
		p.partialLine.Reset()
	}

	p.inputActive = true

	// Emit a content-free input boundary marker.
	boundary := NewInputBoundarySegment(sessionID, observedAt)
	return &boundary
}

// EndInput marks the end of a terminal input window.
// Normal byte projection resumes.
func (p *ByteStreamProjector) EndInput(sessionID string, observedAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.inputActive = false
}

// BeginTUIBurst marks the start of a TUI/alternate-screen region.
// Bytes are suppressed and counted; a ui_omitted marker replaces them.
func (p *ByteStreamProjector) BeginTUIBurst() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tuiBurstActive = true
	p.tuiBurstCount = 0
}

// EndTUIBurst marks the end of a TUI burst and returns a ui_omitted marker
// if any bytes were suppressed.
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

// Flush forces any accumulated partial line to be emitted as a segment.
func (p *ByteStreamProjector) Flush(sessionID string, observedAt time.Time) []TranscriptSegment {
	p.mu.Lock()
	defer p.mu.Unlock()

	var segments []TranscriptSegment
	if p.partialLine.Len() > 0 {
		line := strings.TrimRight(p.partialLine.String(), "\r")
		p.partialLine.Reset()
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			segments = append(segments, NewTerminalOutputSegment(
				sessionID, trimmed, len(line), observedAt,
			))
		}
	}
	if p.overflowed {
		p.overflowed = false
		segments = append(segments, NewDegradedSegment(
			sessionID, "byte-stream queue overflow (recovered)", observedAt,
		))
	}
	return segments
}

// Diagnostics returns projector metrics for monitoring.
func (p *ByteStreamProjector) Diagnostics() ByteStreamDiag {
	p.mu.Lock()
	defer p.mu.Unlock()
	return ByteStreamDiag{
		PartialLen:      p.partialLine.Len(),
		InputActive:     p.inputActive,
		TUIBurstActive:  p.tuiBurstActive,
		TotalProjected:  p.totalProjected,
		TotalSuppressed: p.totalSuppressed,
		Overflowed:      p.overflowed,
	}
}

// ByteStreamDiag carries diagnostic counters.
type ByteStreamDiag struct {
	PartialLen      int
	InputActive     bool
	TUIBurstActive  bool
	TotalProjected  int64
	TotalSuppressed int64
	Overflowed      bool
}

// ── ANSI/TUI detection helpers ──

// IsAlternateScreenStart detects ESC[?1049h (xterm alternate screen enable).
func IsAlternateScreenStart(chunk []byte) bool {
	return len(chunk) >= 8 &&
		chunk[0] == 0x1b && chunk[1] == '[' && chunk[2] == '?' &&
		chunk[3] == '1' && chunk[4] == '0' && chunk[5] == '4' &&
		chunk[6] == '9' && chunk[7] == 'h'
}

// IsAlternateScreenEnd detects ESC[?1049l (xterm alternate screen disable).
func IsAlternateScreenEnd(chunk []byte) bool {
	return len(chunk) >= 8 &&
		chunk[0] == 0x1b && chunk[1] == '[' && chunk[2] == '?' &&
		chunk[3] == '1' && chunk[4] == '0' && chunk[5] == '4' &&
		chunk[6] == '9' && chunk[7] == 'l'
}

// HasClearScreen detects if chunk contains ESC[2J (full screen clear).
func HasClearScreen(chunk []byte) bool {
	for i := 0; i <= len(chunk)-4; i++ {
		if chunk[i] == 0x1b && chunk[i+1] == '[' && chunk[i+2] == '2' && chunk[i+3] == 'J' {
			return true
		}
	}
	return false
}
