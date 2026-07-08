package term

import (
	"context"
	"log"
	"sync"

	"devremote/companion-daemon/internal/mux"
)

// Recorder owns the PTY read loop for a session and broadcasts to subscribers.
// One recorder per session. ActivityBuffer fed only by the recorder, never by WebSocket handlers.
type Recorder struct {
	sessionID string
	stream    mux.TerminalStream
	activity  *ActivityBuffer
	ctx       context.Context
	cancel    context.CancelFunc

	mu          sync.Mutex
	subscribers []chan []byte // broadcast channels to WebSocket handlers
	done        chan struct{}
}

// recorderRegistry tracks active recorders per session.
var recorderRegistry = struct {
	mu       sync.Mutex
	recorders map[string]*Recorder
}{recorders: make(map[string]*Recorder)}

// StartRecorder creates or returns an existing recorder for a session.
// Only one recorder per session. The recorder owns the PTY read loop.
func StartRecorder(sessionID string, opener mux.StreamOpener, activity *ActivityBuffer) (*Recorder, error) {
	recorderRegistry.mu.Lock()
	defer recorderRegistry.mu.Unlock()

	if r, ok := recorderRegistry.recorders[sessionID]; ok {
		return r, nil // Already recording.
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := opener.OpenStream(ctx)
	if err != nil {
		cancel()
		return nil, err
	}

	r := &Recorder{
		sessionID:   sessionID,
		stream:      stream,
		activity:    activity,
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make([]chan []byte, 0),
		done:        make(chan struct{}),
	}

	recorderRegistry.recorders[sessionID] = r

	go r.readLoop()
	log.Printf("RECORDER start session=%s", sessionID)
	return r, nil
}

// Subscribe returns a channel that receives live PTY output.
func (r *Recorder) Subscribe() chan []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan []byte, 64)
	r.subscribers = append(r.subscribers, ch)
	return ch
}

// Unsubscribe removes a subscriber channel.
func (r *Recorder) Unsubscribe(ch chan []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, sub := range r.subscribers {
		if sub == ch {
			r.subscribers = append(r.subscribers[:i], r.subscribers[i+1:]...)
			close(ch)
			break
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (r *Recorder) SubscriberCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.subscribers)
}

// Stop terminates the recorder and its PTY read loop.
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

// readLoop reads from the PTY stream and broadcasts to subscribers + ActivityBuffer.
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
			log.Printf("RECORDER read err session=%s: %v", r.sessionID, err)
			return
		}
		if n == 0 {
			continue
		}

		// Copy to subscribers.
		payload := make([]byte, n)
		copy(payload, buf[:n])

		r.mu.Lock()
		for _, ch := range r.subscribers {
			select {
			case ch <- payload:
			default:
				// Drop if subscriber is slow.
			}
		}
		r.mu.Unlock()

		// Append to ActivityBuffer — only the recorder does this.
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
