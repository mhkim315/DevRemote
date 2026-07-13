package transcript

import (
	"sync"
	"time"
)

// chunkQueue is a session-owned, bounded, single-worker queue for Recorder
// PTY byte chunks. It guarantees:
//   - Non-blocking enqueue (overflow → coalesced gap)
//   - Single ordered worker (preserves chunk ordering)
//   - Bounded event count and byte capacity
//   - Close/enqueue serialization (no send-on-closed-channel panic)
//   - Worker completion signal for safe drain-before-clear
type chunkQueue struct {
	sessionID string
	svc       *Service

	mu       sync.Mutex
	chunks   chan chunkItem
	closed   bool
	done     chan struct{} // closed when worker exits
	dropped  int64
	overflow bool
}

type chunkItem struct {
	data       []byte
	observedAt time.Time
}

const (
	chunkQueueCapacity      = 256
	chunkQueueMaxChunkBytes = 65536
)

func newChunkQueue(sessionID string, svc *Service) *chunkQueue {
	q := &chunkQueue{
		sessionID: sessionID,
		svc:       svc,
		chunks:    make(chan chunkItem, chunkQueueCapacity),
		done:      make(chan struct{}),
	}
	go q.worker()
	return q
}

// enqueue attempts to deliver a chunk. It never blocks. If the queue is
// closed or full, the chunk is dropped. Send-on-closed-channel panic is
// prevented by checking closed under lock.
func (q *chunkQueue) enqueue(data []byte, observedAt time.Time) {
	if len(data) > chunkQueueMaxChunkBytes {
		data = data[:chunkQueueMaxChunkBytes]
	}
	chunk := make([]byte, len(data))
	copy(chunk, data)

	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	select {
	case q.chunks <- chunkItem{data: chunk, observedAt: observedAt}:
		q.mu.Unlock()
	default:
		q.dropped++
		q.overflow = true
		q.mu.Unlock()
	}
}

// close shuts down the worker and waits for it to drain. Safe to call
// multiple times. After close, enqueue is a no-op.
func (q *chunkQueue) close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.mu.Unlock()
	close(q.chunks)
	<-q.done // wait for worker to drain and exit
}

// isClosed reports whether close has been called.
func (q *chunkQueue) isClosed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed
}

func (q *chunkQueue) worker() {
	defer close(q.done)
	for item := range q.chunks {
		q.mu.Lock()
		overflow := q.overflow
		q.overflow = false
		q.mu.Unlock()

		if overflow {
			q.svc.emitDegraded(q.sessionID, "byte-stream chunks dropped (queue overflow)", item.observedAt)
		}

		q.svc.processChunk(q.sessionID, item.data, item.observedAt)
	}
	// Final flush on close.
	q.svc.FlushBytes(q.sessionID, time.Now())
}
