package transcript

import (
	"sync"
	"time"
)

type chunkQueue struct {
	sessionID  string
	svc        *Service
	generation int64

	mu       sync.Mutex
	chunks   chan chunkItem
	closed   bool
	done     chan struct{}
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

func newChunkQueue(sessionID string, svc *Service, generation int64) *chunkQueue {
	q := &chunkQueue{
		sessionID:  sessionID,
		svc:        svc,
		generation: generation,
		chunks:     make(chan chunkItem, chunkQueueCapacity),
		done:       make(chan struct{}),
	}
	go q.worker()
	return q
}

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

func (q *chunkQueue) close() {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	q.mu.Unlock()
	close(q.chunks)
	<-q.done
}

func (q *chunkQueue) isClosed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed
}

func (q *chunkQueue) worker() {
	defer close(q.done)
	for item := range q.chunks {
		// PA3 Closeout B R2: overflow and chunk processing are each
		// generation-guarded inside the Service lock — no TOCTOU gap
		// between generation check and mutation. ReplaceTranscript
		// cannot interleave.
		q.mu.Lock()
		overflow := q.overflow
		q.overflow = false
		q.mu.Unlock()
		if overflow {
			q.svc.emitDegradedGuarded(q.sessionID, "byte-stream chunks dropped (queue overflow)", item.observedAt, q.generation)
		}
		q.svc.processChunk(q.sessionID, item.data, item.observedAt, q.generation)
	}
	// Final flush is generation-guarded inside the Service lock.
	q.svc.flushBytesGuarded(q.sessionID, time.Now(), q.generation)
}
