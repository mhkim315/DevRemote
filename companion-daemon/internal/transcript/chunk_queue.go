package transcript

import (
	"sync"
	"time"
)

// chunkQueue is a session-owned, bounded, single-worker queue for Recorder
// PTY byte chunks. It guarantees:
//   - Non-blocking enqueue (overflow → coalesced gap, never blocks Recorder)
//   - Single ordered worker (preserves chunk ordering)
//   - Bounded event count and byte capacity
//   - Deterministic shutdown via Close
type chunkQueue struct {
	sessionID string
	svc       *Service

	mu       sync.Mutex
	chunks   chan chunkItem
	closed   bool
	dropped  int64 // total chunks dropped due to overflow
	overflow bool  // true if overflow has occurred since last gap emission
}

type chunkItem struct {
	data       []byte
	observedAt time.Time
}

const (
	// chunkQueueCapacity is the maximum number of queued chunks before overflow.
	chunkQueueCapacity = 256

	// chunkQueueMaxChunkBytes is the maximum bytes per chunk; larger chunks are truncated.
	chunkQueueMaxChunkBytes = 65536
)

// newChunkQueue creates a bounded queue and starts its worker goroutine.
func newChunkQueue(sessionID string, svc *Service) *chunkQueue {
	q := &chunkQueue{
		sessionID: sessionID,
		svc:       svc,
		chunks:    make(chan chunkItem, chunkQueueCapacity),
	}
	go q.worker()
	return q
}

// enqueue attempts to deliver a chunk to the projector worker.
// It never blocks: if the queue is full, the chunk is dropped and a
// coalesced gap is emitted asynchronously.
func (q *chunkQueue) enqueue(data []byte, observedAt time.Time) {
	if len(data) > chunkQueueMaxChunkBytes {
		data = data[:chunkQueueMaxChunkBytes]
	}
	chunk := make([]byte, len(data))
	copy(chunk, data)

	select {
	case q.chunks <- chunkItem{data: chunk, observedAt: observedAt}:
		// Delivered.
	default:
		// Overflow: drop and mark.
		q.mu.Lock()
		q.dropped++
		q.overflow = true
		q.mu.Unlock()
	}
}

// close shuts down the worker and drains remaining chunks.
func (q *chunkQueue) close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.mu.Unlock()
	close(q.chunks)
}

// worker processes chunks in order on a single goroutine.
func (q *chunkQueue) worker() {
	for item := range q.chunks {
		// Emit any coalesced overflow gap before processing this chunk.
		q.mu.Lock()
		overflow := q.overflow
		q.overflow = false
		dropped := q.dropped
		q.mu.Unlock()

		if overflow {
			q.svc.emitDegraded(q.sessionID, "byte-stream chunks dropped (queue overflow)", item.observedAt)
			_ = dropped
		}

		q.svc.processChunk(q.sessionID, item.data, item.observedAt)
	}
	// Final flush on close.
	q.svc.FlushBytes(q.sessionID, time.Now())
}
