package term

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
)

// Recorder owns the PTY read loop for a session.
// One recorder per session. Single source of ActivityBuffer appends.
type Recorder struct {
	sessionID string
	stream    mux.TerminalStream
	activity  *ActivityBuffer
	ctx       context.Context
	cancel    context.CancelFunc

	mu          sync.Mutex
	subscribers []chan []byte
	done        chan struct{}
	readErr     error
	captureMode mux.TranscriptCaptureMode // source of truth for capture behavior

	// T3 Transcript: optional non-blocking byte-stream feed.
	// When non-nil, readLoop feeds copied chunks to the Transcript service
	// byte-stream projector. nil means Transcript integration is disabled.
	transcriptSvc *transcript.Service

	// Terminal bootstrap: ring buffer of recent raw PTY bytes.
	// Late-attaching subscribers receive this before live stream.
	bootstrapBuf  []byte
	bootstrapPos  int
	bootstrapFull bool
}

// transcriptSvcSingleton is the optional T3 Transcript service, set once
// during app initialization. Recorders check this to feed byte-stream data
// into the Transcript byte-stream projector without requiring signature
// changes to StartRecorder/EnsureRecorder.
var transcriptSvcSingleton *transcript.Service

// SetTranscriptService sets the process-wide Transcript service for Recorder feeding.
func SetTranscriptService(svc *transcript.Service) {
	transcriptSvcSingleton = svc
}

// recorderRegistry tracks active recorders.
var recorderRegistry = struct {
	mu         sync.Mutex
	recorders  map[string]*Recorder
	terminated map[string]bool // sessions whose PTY process exited
}{recorders: make(map[string]*Recorder), terminated: make(map[string]bool)}

// StartRecorder creates a recorder for a session. Returns existing if alive.
// Caller must provide an active stream — recorder takes ownership of the read loop.
// The returned subscriber channel receives live PTY output immediately.
func StartRecorder(sessionID string, stream mux.TerminalStream, activity *ActivityBuffer) (*Recorder, chan []byte) {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()

	if recorderRegistry.terminated[sessionID] {
		return nil, nil
	}

	if r, ok := recorderRegistry.recorders[sessionID]; ok {
		if !r.IsAlive() || r.Err() != nil {
			delete(recorderRegistry.recorders, sessionID)
		} else {
			return r, r.Subscribe()
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &Recorder{
		sessionID:   sessionID,
		stream:      stream,
		activity:    activity,
		ctx:         ctx,
		cancel:      cancel,
		subscribers: nil,
		done:        make(chan struct{}),
	}

	// Add initial subscriber BEFORE starting readLoop to avoid race.
	ch := r.Subscribe()
	recorderRegistry.recorders[sessionID] = r

	r.captureMode = resolveCaptureMode(sessionID)
	r.transcriptSvc = transcriptSvcSingleton
	go r.readLoop()
	log.Printf("RECORDER start session=%s", sessionID)
	return r, ch
}

// Subscribe returns a channel that receives live PTY output.
func (r *Recorder) Subscribe() chan []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan []byte, 64)
	r.subscribers = append(r.subscribers, ch)
	return ch
}

// SubscribeWithBootstrap atomically registers a subscriber and snapshots
// the terminal bootstrap buffer. The returned bootstrap bytes are a
// point-in-time snapshot taken while the subscriber is registered.
// No output between bootstrap and the first subscriber read is lost.
func (r *Recorder) SubscribeWithBootstrap() (bootstrap []byte, subCh chan []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Snapshot bootstrap buffer under lock.
	bootstrap = r.bootstrapLocked()

	// Register subscriber under same lock — no gap.
	subCh = make(chan []byte, 64)
	r.subscribers = append(r.subscribers, subCh)
	return bootstrap, subCh
}

// bootstrapLocked returns a copy of the bootstrap buffer.
// Must be called with r.mu held.
func (r *Recorder) bootstrapLocked() []byte {
	if r.bootstrapBuf == nil || (!r.bootstrapFull && r.bootstrapPos == 0) {
		return nil
	}
	if !r.bootstrapFull {
		out := make([]byte, r.bootstrapPos)
		copy(out, r.bootstrapBuf[:r.bootstrapPos])
		return out
	}
	out := make([]byte, len(r.bootstrapBuf))
	n := copy(out, r.bootstrapBuf[r.bootstrapPos:])
	copy(out[n:], r.bootstrapBuf[:r.bootstrapPos])
	return out
}

// Unsubscribe removes a subscriber.
func (r *Recorder) Unsubscribe(ch chan []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, sub := range r.subscribers {
		if sub == ch {
			r.subscribers = append(r.subscribers[:i], r.subscribers[i+1:]...)
			close(ch)
			return
		}
	}
}

// Stop terminates the recorder. Idempotent.
func (r *Recorder) Stop() {
	recorderRegistry.mu.Lock()
	delete(recorderRegistry.recorders, r.sessionID)
	recorderRegistry.mu.Unlock()

	r.cancel()
	r.stream.Close()
	<-r.done

	r.mu.Lock()
	for _, ch := range r.subscribers {
		close(ch)
	}
	r.subscribers = nil
	r.mu.Unlock()
	log.Printf("RECORDER stop session=%s", r.sessionID)
}

// Err returns the read error, if any.
func (r *Recorder) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.readErr
}

// SubscriberCount returns the number of active subscriber channels. Read-only
// test hook used by the M2.5-4 production-boundary tests to prove that a
// disconnected viewer's recorder subscription is released while the
// session-owned recorder itself persists (E8f2 single-reader invariant).
func (r *Recorder) SubscriberCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subscribers)
}

// IsAlive returns true if the recorder's readLoop is still running.
func (r *Recorder) IsAlive() bool {
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}

// Done returns a channel closed when the recorder's readLoop has ended (PTY
// EOF/error). The M2 lifecycle service waits on this to observe process exit —
// natural exit and Stop/Kill all end here, converging on one cleanup path.
func (r *Recorder) Done() <-chan struct{} { return r.done }

// unregisterSelf removes this recorder from the registry, but only if
// the registry still points to this exact instance (not a newer replacement).
func (r *Recorder) unregisterSelf() {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()
	if existing, ok := recorderRegistry.recorders[r.sessionID]; ok && existing == r {
		delete(recorderRegistry.recorders, r.sessionID)
	}
}

// feedTranscript feeds copied payload bytes to the T3 Transcript byte-stream
// projector asynchronously so Transcript projection never blocks raw PTY delivery.
func (r *Recorder) feedTranscript(payload []byte) {
	if r.transcriptSvc == nil {
		return
	}
	chunk := make([]byte, len(payload))
	copy(chunk, payload)
	go r.transcriptSvc.FeedBytes(r.sessionID, chunk, time.Now())
}

// readLoop reads PTY output, broadcasts to subscribers, appends to ActivityBuffer.
// On exit (EOF, error, or cancel), the recorder unregisters itself — but only
// if no newer recorder for the same sessionID has been created.
func (r *Recorder) readLoop() {
	defer close(r.done)
	defer r.unregisterSelf()
	buf := make([]byte, 1024)
	for {
		select {
		case <-r.ctx.Done():
			return
		default:
		}

		n, err := r.stream.Read(buf)
		if err != nil {
			r.mu.Lock()
			r.readErr = err
			r.mu.Unlock()
			log.Printf("RECORDER read err session=%s: %v", r.sessionID, err)
			// Mark session terminated so telemetry does not restart recorder.
			recorderRegistry.mu.Lock()
			recorderRegistry.terminated[r.sessionID] = true
			recorderRegistry.mu.Unlock()
			// Close all subscribers so WebSocket handlers detect EOF.
			r.mu.Lock()
			for _, ch := range r.subscribers {
				close(ch)
			}
			r.subscribers = nil
			r.mu.Unlock()
			return
		}
		if n == 0 {
			continue
		}

		payload := make([]byte, n)
		copy(payload, buf[:n])

		// E8g4: detect cmux delta frames (prefixed with ESC[9998m).
		// Strip marker. Append to ActivityBuffer (Transcript)
		// but do NOT broadcast to live terminal subscribers.
		if isDeltaMarker(payload) {
			payload = payload[len(deltaMarker):]
			if len(payload) == 0 {
				continue
			}
			if r.activity != nil {
				text := stripANSI(string(payload))
				// Replace CR with LF so TUI status repaints don't merge.
				text = strings.ReplaceAll(text, "\r", "\n")
				if !isANSIControlOnly(text) && len(text) > 3 {
					if len(text) > 32768 {
						text = text[:32768]
					}
					r.activity.Append(ActivityEvent{
						SessionID: r.sessionID,
						Type:      ActivityTerminalOutput,
						Text:      text,
						Bytes:     len(payload),
					})
				}
			}
			// T3: feed cmux delta to Transcript byte-stream projector (non-blocking).
			r.feedTranscript(payload)
			continue // do NOT broadcast delta to subscribers
		}

		// E8i: detect cmux screen snapshots. These are full-screen redraws
		// (ESC[2J ESC[H + screen content) sent as one pipe Write(). The pipe
		// delivers them in chunks; only the first chunk starts with ESC[2J.
		// We must drain ALL chunks from the same Write() without appending
		// any to ActivityBuffer. Live terminal subscribers still receive them.
		if isClearScreenSnapshot(payload) {
			r.broadcast(payload)
			r.drainSnapshot(buf)
			continue
		}

		// Append to ActivityBuffer FIRST — recorder is the SINGLE append source.
		// Subscriber broadcast follows so that receiving data implies capture is done.
		if r.activity != nil {
			text := stripANSI(string(payload))
			// Replace CR with LF so TUI status repaints don't merge.
			text = strings.ReplaceAll(text, "\r", "\n")
			if !isANSIControlOnly(text) && len(text) > 3 {
				if len(text) > 32768 {
					text = text[:32768]
				}
				r.activity.Append(ActivityEvent{
					SessionID: r.sessionID,
					Type:      ActivityTerminalOutput,
					Text:      text,
					Bytes:     n,
				})
			}
		}

		// T3: feed raw bytes to Transcript byte-stream projector (non-blocking).
		r.feedTranscript(payload)

		// Broadcast to subscribers after append — eliminates race between
		// subscriber receive and ActivityBuffer.List.
		r.broadcast(payload)
	}
}

// broadcast sends payload to all subscriber channels (non-blocking).
func (r *Recorder) broadcast(payload []byte) {
	r.mu.Lock()
	// Write to terminal bootstrap ring buffer.
	r.writeBootstrapLocked(payload)
	for _, ch := range r.subscribers {
		select {
		case ch <- payload:
		default:
		}
	}
	r.mu.Unlock()
}

// writeBootstrapLocked appends payload to the terminal bootstrap ring buffer.
// Must be called with r.mu held.
func (r *Recorder) writeBootstrapLocked(payload []byte) {
	if r.bootstrapBuf == nil {
		r.bootstrapBuf = make([]byte, 65536) // 64KB ring buffer
	}
	for _, b := range payload {
		r.bootstrapBuf[r.bootstrapPos] = b
		r.bootstrapPos++
		if r.bootstrapPos >= len(r.bootstrapBuf) {
			r.bootstrapPos = 0
			r.bootstrapFull = true
		}
	}
}

// Bootstrap returns a copy of the terminal bootstrap buffer content
// (most recent raw PTY bytes) for late-attaching subscribers.
func (r *Recorder) Bootstrap() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bootstrapBuf == nil || (!r.bootstrapFull && r.bootstrapPos == 0) {
		return nil
	}
	if !r.bootstrapFull {
		out := make([]byte, r.bootstrapPos)
		copy(out, r.bootstrapBuf[:r.bootstrapPos])
		return out
	}
	// Ring buffer full — return from write pos to end, then start to write pos.
	out := make([]byte, len(r.bootstrapBuf))
	n := copy(out, r.bootstrapBuf[r.bootstrapPos:])
	copy(out[n:], r.bootstrapBuf[:r.bootstrapPos])
	return out
}

// snapshotEndMarker is a sentinel appended by cmux adapter after each
// full-screen snapshot. It is an invalid SGR sequence that no real
// terminal output would contain. xterm.js ignores unknown SGR codes.
var snapshotEndMarker = []byte("\x1b[9999m")

// deltaMarker prefixes cmux delta frames. ESC[9998m is an invalid SGR
// code — xterm.js ignores it. Recorder strips it before ActivityBuffer.
var deltaMarker = []byte("\x1b[9998m")

// resolveCaptureMode is a future hook for per-adapter capture behavior.
// Currently returns CaptureModeByteStream (legacy default).
// Actual cmux behavior is enforced by sentinel detection in readLoop.
func resolveCaptureMode(sessionID string) mux.TranscriptCaptureMode {
	return mux.CaptureModeByteStream
}

// isDeltaMarker reports whether payload starts with the delta prefix.
func isDeltaMarker(payload []byte) bool {
	if len(payload) < len(deltaMarker) {
		return false
	}
	for i := 0; i < len(deltaMarker); i++ {
		if payload[i] != deltaMarker[i] {
			return false
		}
	}
	return true
}

// drainSnapshot reads chunks until snapshotEndMarker is found, then strips
// it. All chunks (including the marker portion) are broadcast to live
// terminal subscribers but never appended to ActivityBuffer.
//
// A 1-second safety timeout prevents false-positive drain from consuming
// normal PTY output forever (e.g., if isClearScreenSnapshot incorrectly
// matched a non-snapshot escape sequence).
func (r *Recorder) drainSnapshot(buf []byte) {
	const safetyTimeout = 1 * time.Second
	deadline := time.After(safetyTimeout)
	for {
		type readResult struct {
			n   int
			err error
		}
		ch := make(chan readResult, 1)
		go func() {
			n, err := r.stream.Read(buf)
			ch <- readResult{n, err}
		}()
		select {
		case res := <-ch:
			if res.err != nil || res.n == 0 {
				return
			}
			chunk := make([]byte, res.n)
			copy(chunk, buf[:res.n])

			if idx := indexOf(chunk, snapshotEndMarker); idx >= 0 {
				if idx > 0 {
					r.broadcast(chunk[:idx])
				}
				return
			}
			r.broadcast(chunk)
		case <-deadline:
			// Safety: no marker found within timeout — not a real
			// cmux snapshot. Resume normal append behavior.
			log.Printf("RECORDER drainSnapshot timeout session=%s", r.sessionID)
			return
		case <-r.ctx.Done():
			return
		}
	}
}

// indexOf returns the index of needle in haystack, or -1 if not found.
func indexOf(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// EnsureRecorder returns or creates a recorder for a session.
// HandleWS calls this to subscribe — does NOT open its own stream.
// If no recorder exists and no opener is provided, returns nil.
func EnsureRecorder(sessionID string, opener mux.StreamOpener, activity *ActivityBuffer) (*Recorder, chan []byte) {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()

	// E10: do not restart recorder for sessions whose PTY process exited.
	if recorderRegistry.terminated[sessionID] {
		return nil, nil
	}

	if r, ok := recorderRegistry.recorders[sessionID]; ok {
		if !r.IsAlive() || r.Err() != nil {
			delete(recorderRegistry.recorders, sessionID)
		} else {
			return r, r.Subscribe()
		}
	}

	if opener == nil {
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := opener.OpenStream(ctx)
	if err != nil {
		cancel()
		return nil, nil
	}

	r := &Recorder{
		sessionID:   sessionID,
		stream:      stream,
		activity:    activity,
		ctx:         ctx,
		cancel:      cancel,
		subscribers: nil,
		done:        make(chan struct{}),
	}
	ch := r.Subscribe()
	recorderRegistry.recorders[sessionID] = r
	r.captureMode = resolveCaptureMode(sessionID)
	r.transcriptSvc = transcriptSvcSingleton
	go r.readLoop()
	log.Printf("RECORDER start session=%s", sessionID)
	return r, ch
}

// DeleteRecorder stops and removes the recorder for a session.
// Called on session delete/end.
func DeleteRecorder(sessionID string) {
	recorderRegistry.mu.Lock()
	r, ok := recorderRegistry.recorders[sessionID]
	if ok {
		delete(recorderRegistry.recorders, sessionID)
	}
	delete(recorderRegistry.terminated, sessionID)
	recorderRegistry.mu.Unlock()
	if ok {
		r.Stop()
	}
}

// WriteInput sends input to the PTY stream. Used for tmux stream-only input fallback.
func (r *Recorder) WriteInput(data []byte) (int, error) {
	return r.stream.Write(data)
}

// Resize resizes the underlying PTY stream. Used by local terminal attach
// to match the host terminal size. Safe to call while the read loop runs.
func (r *Recorder) Resize(rows, cols int) error {
	return r.stream.Resize(rows, cols)
}

// GetSize returns the underlying PTY's current geometry, if the stream
// supports it (native PTY sessions do; tmux/cmux streams do not). ok is false
// when the size is unavailable.
func (r *Recorder) GetSize() (rows, cols int, ok bool) {
	if s, isSizer := r.stream.(interface {
		GetSize() (int, int, error)
	}); isSizer {
		if rr, cc, err := s.GetSize(); err == nil && rr > 0 && cc > 0 {
			return rr, cc, true
		}
	}
	return 0, 0, false
}

// isClearScreenSnapshot reports whether payload starts with the cmux
// full-screen redraw header (ESC[2J ESC[H). Normal PTY output may contain
// ESC[H (cursor home) alone, which must NOT trigger snapshot drain.
// ESC[2J (clear screen) immediately followed by ESC[H (cursor home) is
// the distinctive cmux screen poll signature.
func isClearScreenSnapshot(payload []byte) bool {
	// cmux header: ESC [ 2 J ESC [ H = 7 bytes
	if len(payload) < 7 {
		return false
	}
	return payload[0] == 0x1b && payload[1] == '[' &&
		payload[2] == '2' && payload[3] == 'J' &&
		payload[4] == 0x1b && payload[5] == '[' &&
		payload[6] == 'H'
}

// GetRecorder returns the recorder for a session, or nil.
func GetRecorder(sessionID string) *Recorder {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()
	return recorderRegistry.recorders[sessionID]
}
