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
		if r.Err() != nil {
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

// readLoop reads PTY output, broadcasts to subscribers, appends to ActivityBuffer.
func (r *Recorder) readLoop() {
	defer close(r.done)
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

		// Broadcast to subscribers.
		r.mu.Lock()
		for _, ch := range r.subscribers {
			select {
			case ch <- payload:
			default:
			}
		}
		r.mu.Unlock()

		// Append to ActivityBuffer — recorder is the SINGLE append source.
		if r.activity != nil {
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
	}
}

// GetRecorder returns the recorder for a session, or nil.
func GetRecorder(sessionID string) *Recorder {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()
	return recorderRegistry.recorders[sessionID]
}
