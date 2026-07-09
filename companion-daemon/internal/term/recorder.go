package term

import (
	"context"
	"log"
	"sync"

	"devremote/companion-daemon/internal/mux"
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
}

// recorderRegistry tracks active recorders.
var recorderRegistry = struct {
	mu       sync.Mutex
	recorders map[string]*Recorder
}{recorders: make(map[string]*Recorder)}

// StartRecorder creates a recorder for a session. Returns existing if alive.
// Caller must provide an active stream — recorder takes ownership of the read loop.
// The returned subscriber channel receives live PTY output immediately.
func StartRecorder(sessionID string, stream mux.TerminalStream, activity *ActivityBuffer) (*Recorder, chan []byte) {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()

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

// IsAlive returns true if the recorder's readLoop is still running.
func (r *Recorder) IsAlive() bool {
	select {
	case <-r.done:
		return false
	default:
		return true
	}
}

// unregisterSelf removes this recorder from the registry, but only if
// the registry still points to this exact instance (not a newer replacement).
func (r *Recorder) unregisterSelf() {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()
	if existing, ok := recorderRegistry.recorders[r.sessionID]; ok && existing == r {
		delete(recorderRegistry.recorders, r.sessionID)
	}
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

		// Append to ActivityBuffer FIRST — recorder is the SINGLE append source.
		// Subscriber broadcast follows so that receiving data implies capture is done.
		//
		// E8i: skip full-screen snapshots (cmux adapter polls screen every 500ms).
		// Snapshots start with clear-screen + cursor-home and contain cumulative
		// content, not deltas. They are broadcast to live terminal subscribers
		// but must not be stored as terminal_output in ActivityBuffer.
		isScreenSnapshot := isClearScreenSnapshot(payload)
		if r.activity != nil && !isScreenSnapshot {
			text := string(payload)
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

		// Broadcast to subscribers after append — eliminates race between
		// subscriber receive and ActivityBuffer.List.
		// Screen snapshots ARE broadcast (live terminal needs them).
		r.mu.Lock()
		for _, ch := range r.subscribers {
			select {
			case ch <- payload:
			default:
			}
		}
		r.mu.Unlock()
	}
}

// EnsureRecorder returns or creates a recorder for a session.
// HandleWS calls this to subscribe — does NOT open its own stream.
// If no recorder exists and no opener is provided, returns nil.
func EnsureRecorder(sessionID string, opener mux.StreamOpener, activity *ActivityBuffer) (*Recorder, chan []byte) {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()

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
	recorderRegistry.mu.Unlock()
	if ok {
		r.Stop()
	}
}

// WriteInput sends input to the PTY stream. Used for tmux stream-only input fallback.
func (r *Recorder) WriteInput(data []byte) (int, error) {
	return r.stream.Write(data)
}

// isClearScreenSnapshot reports whether payload starts with a clear-screen
// escape sequence (ESC[2J or ESC[H), indicating a full-screen redraw rather
// than incremental terminal output. Used to filter cmux screen polls from
// ActivityBuffer while still broadcasting them to live terminal subscribers.
func isClearScreenSnapshot(payload []byte) bool {
	if len(payload) < 4 {
		return false
	}
	// ESC [ 2 J (clear screen) or ESC [ H (cursor home)
	return (payload[0] == 0x1b && payload[1] == '[' &&
		payload[2] == '2' && payload[3] == 'J') ||
		(payload[0] == 0x1b && payload[1] == '[' && payload[2] == 'H')
}

// GetRecorder returns the recorder for a session, or nil.
func GetRecorder(sessionID string) *Recorder {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()
	return recorderRegistry.recorders[sessionID]
}
