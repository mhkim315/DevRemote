package transcript

import (
	"log"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
)

// Service is the per-process Transcript integration service.
// It owns the bounded store, projectors, per-session arbitration state,
// and per-session bounded chunk queues for byte-stream projection.
type Service struct {
	store     *Store
	agentProj *AgentEventProjector
	byteProjs map[string]*ByteStreamProjector // sessionID → projector
	arbiters  map[string]*SourceArbiter       // sessionID → arbiter
	queues    map[string]*chunkQueue          // sessionID → bounded byte-stream queue
	mu        sync.Mutex
}

// NewService creates a Transcript integration service.
func NewService(cfg StoreConfig) *Service {
	return &Service{
		store:     NewStore(cfg),
		agentProj: NewAgentEventProjector(),
		byteProjs: make(map[string]*ByteStreamProjector),
		arbiters:  make(map[string]*SourceArbiter),
		queues:    make(map[string]*chunkQueue),
	}
}

// ── AgentEvent projection path ──

// ProjectAgentEvents projects accepted AgentEvents into the Transcript store.
// Only events with PROVEN session correlation may enter the semantic Transcript.
// Uncorrelated events are silently dropped — byte-stream remains primary.
func (s *Service) ProjectAgentEvents(sessionID string, events []agent.AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	arb := s.ensureArbiter(sessionID)

	// Gate: only project when correlation is explicitly established.
	// Until correlation is proven, byte-stream is the sole semantic source.
	if !arb.CanBePrimarySource() {
		return
	}

	segments := s.agentProj.ProjectBatch(events, sessionID)
	if len(segments) > 0 {
		arb.RecordAgentEvent()
		s.store.Append(sessionID, segments)
	}
}

// SetCorrelation establishes the correlation state for a session.
// Only CorrelationProven or CorrelationManagedLaunch enable AgentEvent
// as the primary semantic source. Must be called before events arrive.
func (s *Service) SetCorrelation(sessionID string, cs CorrelationState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	arb := s.ensureArbiter(sessionID)
	arb.SetCorrelation(cs)
}

// ── Byte-stream fallback path (non-blocking enqueue) ──

// FeedBytes enqueues a Recorder byte chunk for ordered, non-blocking
// projection. It never blocks the caller. If no queue is active for the
// session, it processes synchronously (used in tests and simple paths).
func (s *Service) FeedBytes(sessionID string, chunk []byte, observedAt time.Time) {
	s.mu.Lock()
	q, hasQ := s.queues[sessionID]
	if !hasQ {
		// Synchronous path: no queue attached.
		bp := s.ensureByteProj(sessionID)
		arb := s.ensureArbiter(sessionID)
		segments := bp.Feed(sessionID, chunk, observedAt)
		if len(segments) > 0 {
			arb.RecordByteStream()
			s.store.Append(sessionID, segments)
		}
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	q.enqueue(chunk, observedAt)
}

// EnableQueue attaches a bounded chunk queue to a session, making FeedBytes
// non-blocking. Called from the Recorder production path.
func (s *Service) EnableQueue(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.queues[sessionID]; !ok {
		q := newChunkQueue(sessionID, s)
		s.queues[sessionID] = q
	}
}

// FlushBytes flushes any accumulated partial state in the byte-stream
// projector for the session.
func (s *Service) FlushBytes(sessionID string, observedAt time.Time) {
	s.mu.Lock()
	bp := s.ensureByteProj(sessionID)
	arb := s.ensureArbiter(sessionID)
	segments := bp.Flush(sessionID, observedAt)
	if len(segments) > 0 {
		arb.RecordByteStream()
		s.store.Append(sessionID, segments)
	}
	s.mu.Unlock()
}

// EmitDegraded appends a degraded marker directly (called from queue worker
// and from TelemetryService for overflow markers).
func (s *Service) EmitDegraded(sessionID string, reason string, observedAt time.Time) {
	s.emitDegraded(sessionID, reason, observedAt)
}

// emitDegraded appends a degraded marker directly (called from queue worker).
func (s *Service) emitDegraded(sessionID string, reason string, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	arb := s.ensureArbiter(sessionID)
	arb.RecordDegraded()
	s.store.Append(sessionID, []TranscriptSegment{
		NewDegradedSegment(sessionID, reason, observedAt),
	})
}

// processChunk is called by the chunk queue worker to project a single chunk.
func (s *Service) processChunk(sessionID string, data []byte, observedAt time.Time) {
	s.mu.Lock()
	bp := s.ensureByteProj(sessionID)
	arb := s.ensureArbiter(sessionID)
	segments := bp.Feed(sessionID, data, observedAt)
	if len(segments) > 0 {
		arb.RecordByteStream()
		s.store.Append(sessionID, segments)
	}
	s.mu.Unlock()
}

// BeginInput marks the start of terminal input for echo privacy.
// Idempotent: if already suppressed, does not emit duplicate markers.
func (s *Service) BeginInput(sessionID string, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	arb := s.ensureArbiter(sessionID)
	if arb.IsByteStreamSuppressed() {
		return // already suppressed, no duplicate markers
	}

	bp := s.ensureByteProj(sessionID)
	seg := bp.BeginInput(sessionID, observedAt)
	if seg != nil {
		s.store.Append(sessionID, []TranscriptSegment{*seg})
	}
	arb.MarkByteStreamSuppressed()
	s.store.Append(sessionID, []TranscriptSegment{
		NewDegradedSegment(sessionID, "Byte-stream projection suppressed after terminal input", observedAt),
	})
}

// EndInput marks the end of terminal input echo suppression.
func (s *Service) EndInput(sessionID string, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bp := s.ensureByteProj(sessionID)
	bp.EndInput(sessionID, observedAt)
}

// BeginTUIBurst marks the start of an unprojectable TUI region.
func (s *Service) BeginTUIBurst(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bp := s.ensureByteProj(sessionID)
	bp.BeginTUIBurst()
}

// EndTUIBurst marks the end of a TUI burst and appends a ui_omitted marker.
func (s *Service) EndTUIBurst(sessionID string, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bp := s.ensureByteProj(sessionID)
	arb := s.ensureArbiter(sessionID)

	seg := bp.EndTUIBurst(sessionID, observedAt)
	if seg != nil {
		s.store.Append(sessionID, []TranscriptSegment{*seg})
	}
	arb.RecordByteStream()
}

// ── Read API ──

// ListTranscript returns all segments for a session, oldest-first.
func (s *Service) ListTranscript(sessionID string) []TranscriptSegment {
	return s.store.List(sessionID)
}

// ListTranscriptAfter returns segments with Seq > cursor, oldest-first.
func (s *Service) ListTranscriptAfter(sessionID string, cursor int64) []TranscriptSegment {
	return s.store.ListAfter(sessionID, cursor)
}

// TranscriptStats returns diagnostic counters for a session.
func (s *Service) TranscriptStats(sessionID string) StoreStats {
	return s.store.Stats(sessionID)
}

// ClearTranscript removes all segments and shuts down the queue for a session.
// Queue is drained first to prevent the worker from resurrecting deleted state.
func (s *Service) ClearTranscript(sessionID string) {
	// Close queue first — wait for drain before clearing store.
	s.mu.Lock()
	q, hasQ := s.queues[sessionID]
	if hasQ {
		delete(s.queues, sessionID)
	}
	s.mu.Unlock()
	if hasQ {
		q.close()
	}

	// Now safe to clear store: no worker can append.
	s.mu.Lock()
	s.store.Clear(sessionID)
	delete(s.byteProjs, sessionID)
	delete(s.arbiters, sessionID)
	s.mu.Unlock()

	// Clean up managed launch binding.
	RemoveLaunch(sessionID)
}

// ── Arbitration queries ──

// PrimarySource returns the current primary Transcript source for a session.
func (s *Service) PrimarySource(sessionID string) SegmentSource {
	s.mu.Lock()
	defer s.mu.Unlock()

	arb := s.arbiters[sessionID]
	if arb == nil {
		return SourceUnknown
	}
	return arb.PrimarySource()
}

// HasAgentEvents returns true if the session has AgentEvent-sourced segments.
func (s *Service) HasAgentEvents(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	arb := s.arbiters[sessionID]
	if arb == nil {
		return false
	}
	return arb.HasAgentEvents()
}

// AddSnapshotSegment appends a cmux/snapshot-sourced segment with degraded
// SourceSnapshot provenance. Separate from both AgentEvent and byte-stream.
// If byte-stream is suppressed (terminal input occurred), snapshot text is
// NOT stored — snapshots may contain echoed input bytes.
func (s *Service) AddSnapshotSegment(sessionID string, text string, byteCount int, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	arb := s.ensureArbiter(sessionID)
	if arb.IsByteStreamSuppressed() {
		// Snapshot may contain echoed terminal input — suppress permanently.
		s.store.Append(sessionID, []TranscriptSegment{
			NewDegradedSegment(sessionID, "snapshot suppressed after terminal input", observedAt),
		})
		return
	}
	seg := TranscriptSegment{
		SessionID:       sessionID,
		Kind:            KindTerminalOutput,
		Source:          SourceSnapshot,
		Text:            boundedText(text, MaxTextBytes),
		ByteCount:       byteCount,
		ObservedAt:      observedAt,
		ContractVersion: ContractVersion,
	}
	s.store.Append(sessionID, []TranscriptSegment{seg})
	_ = arb
}

// FeedBytesBatch directly appends pre-built terminal output segments.
func (s *Service) FeedBytesBatch(sessionID string, segments []TranscriptSegment) {
	s.mu.Lock()
	defer s.mu.Unlock()

	arb := s.ensureArbiter(sessionID)
	arb.RecordByteStream()
	s.store.Append(sessionID, segments)
}

// BuildResponse constructs the separated TranscriptResponse envelope.
func (s *Service) BuildResponse(sessionID string, segments []TranscriptSegment) TranscriptResponse {
	s.mu.Lock()
	arb := s.arbiters[sessionID]
	s.mu.Unlock()
	return NewTranscriptResponse(sessionID, segments, arb)
}

// ── Queue shutdown (called on Recorder stop) ──

// CloseSessionQueue gracefully shuts down the chunk queue for a session.
// The worker drains remaining chunks and flushes the projector.
func (s *Service) CloseSessionQueue(sessionID string) {
	s.mu.Lock()
	q, ok := s.queues[sessionID]
	if ok {
		delete(s.queues, sessionID)
	}
	s.mu.Unlock()
	if ok {
		q.close()
		log.Printf("TRANSCRIPT queue close session=%s", sessionID)
	}
}

// ── Internal helpers ──

func (s *Service) ensureByteProj(sessionID string) *ByteStreamProjector {
	if bp, ok := s.byteProjs[sessionID]; ok {
		return bp
	}
	bp := NewByteStreamProjector(DefaultByteStreamConfig())
	s.byteProjs[sessionID] = bp
	return bp
}

func (s *Service) ensureArbiter(sessionID string) *SourceArbiter {
	if arb, ok := s.arbiters[sessionID]; ok {
		return arb
	}
	arb := NewSourceArbiter()
	s.arbiters[sessionID] = arb
	return arb
}

func (s *Service) ensureQueue(sessionID string) *chunkQueue {
	if q, ok := s.queues[sessionID]; ok {
		return q
	}
	q := newChunkQueue(sessionID, s)
	s.queues[sessionID] = q
	return q
}
