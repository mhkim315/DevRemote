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
	store      *Store
	agentProj  *AgentEventProjector
	byteProjs  map[string]*ByteStreamProjector
	arbiters   map[string]*SourceArbiter
	queues     map[string]*chunkQueue
	currentGen map[string]int64
	mu         sync.Mutex
}

func NewService(cfg StoreConfig) *Service {
	return &Service{
		store:      NewStore(cfg),
		agentProj:  NewAgentEventProjector(),
		byteProjs:  make(map[string]*ByteStreamProjector),
		arbiters:   make(map[string]*SourceArbiter),
		queues:     make(map[string]*chunkQueue),
		currentGen: make(map[string]int64),
	}
}

func (s *Service) ProjectAgentEvents(sessionID string, events []agent.AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	arb := s.ensureArbiter(sessionID)
	if !arb.CanBePrimarySource() {
		return
	}
	segments := s.agentProj.ProjectBatch(events, sessionID)
	if len(segments) > 0 {
		arb.RecordAgentEvent()
		s.store.Append(sessionID, segments)
	}
}

func (s *Service) SetCorrelation(sessionID string, cs CorrelationState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	arb := s.ensureArbiter(sessionID)
	arb.SetCorrelation(cs)
}

func (s *Service) FeedBytes(sessionID string, chunk []byte, observedAt time.Time, generation int64) {
	s.mu.Lock()
	if !s.matchGeneration(sessionID, generation) {
		s.mu.Unlock()
		return
	}
	q, hasQ := s.queues[sessionID]
	if !hasQ {
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

func (s *Service) EnableQueue(sessionID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.currentGen[sessionID]; !ok {
		s.currentGen[sessionID] = 1
	}
	gen := s.currentGen[sessionID]
	if _, ok := s.queues[sessionID]; !ok {
		q := newChunkQueue(sessionID, s, gen)
		s.queues[sessionID] = q
	}
	return gen
}

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

func (s *Service) EmitDegraded(sessionID string, reason string, observedAt time.Time) {
	s.emitDegraded(sessionID, reason, observedAt)
}

func (s *Service) emitDegraded(sessionID string, reason string, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	arb := s.ensureArbiter(sessionID)
	arb.RecordDegraded()
	s.store.Append(sessionID, []TranscriptSegment{
		NewDegradedSegment(sessionID, reason, observedAt),
	})
}

func (s *Service) emitDegradedGuarded(sessionID string, reason string, observedAt time.Time, generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.matchGeneration(sessionID, generation) {
		return
	}
	arb := s.ensureArbiter(sessionID)
	arb.RecordDegraded()
	s.store.Append(sessionID, []TranscriptSegment{
		NewDegradedSegment(sessionID, reason, observedAt),
	})
}

func (s *Service) flushBytesGuarded(sessionID string, observedAt time.Time, generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.matchGeneration(sessionID, generation) {
		return
	}
	bp := s.ensureByteProj(sessionID)
	arb := s.ensureArbiter(sessionID)
	segments := bp.Flush(sessionID, observedAt)
	if len(segments) > 0 {
		arb.RecordByteStream()
		s.store.Append(sessionID, segments)
	}
}

func (s *Service) processChunk(sessionID string, data []byte, observedAt time.Time, generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.matchGeneration(sessionID, generation) {
		return
	}
	bp := s.ensureByteProj(sessionID)
	arb := s.ensureArbiter(sessionID)
	segments := bp.Feed(sessionID, data, observedAt)
	if len(segments) > 0 {
		arb.RecordByteStream()
		s.store.Append(sessionID, segments)
	}
}

func (s *Service) BeginInput(sessionID string, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	arb := s.ensureArbiter(sessionID)
	if arb.IsByteStreamSuppressed() {
		return
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

func (s *Service) EndInput(sessionID string, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bp := s.ensureByteProj(sessionID)
	bp.EndInput(sessionID, observedAt)
}

func (s *Service) BeginTUIBurst(sessionID string, generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.matchGeneration(sessionID, generation) {
		return
	}
	bp := s.ensureByteProj(sessionID)
	bp.BeginTUIBurst()
}

func (s *Service) EndTUIBurst(sessionID string, observedAt time.Time, generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.matchGeneration(sessionID, generation) {
		return
	}
	bp := s.ensureByteProj(sessionID)
	arb := s.ensureArbiter(sessionID)
	seg := bp.EndTUIBurst(sessionID, observedAt)
	if seg != nil {
		s.store.Append(sessionID, []TranscriptSegment{*seg})
	}
	arb.RecordByteStream()
}

func (s *Service) ListTranscript(sessionID string) []TranscriptSegment {
	return s.store.List(sessionID)
}

func (s *Service) ListTranscriptAfter(sessionID string, cursor int64) []TranscriptSegment {
	return s.store.ListAfter(sessionID, cursor)
}

func (s *Service) TranscriptStats(sessionID string) StoreStats {
	return s.store.Stats(sessionID)
}

func (s *Service) ClearTranscript(sessionID string) {
	s.mu.Lock()
	q, hasQ := s.queues[sessionID]
	if hasQ {
		delete(s.queues, sessionID)
	}
	s.mu.Unlock()
	if hasQ {
		q.close()
	}
	s.mu.Lock()
	s.store.Clear(sessionID)
	delete(s.byteProjs, sessionID)
	delete(s.arbiters, sessionID)
	s.mu.Unlock()
	RemoveLaunch(sessionID)
}

func (s *Service) PrimarySource(sessionID string) SegmentSource {
	s.mu.Lock()
	defer s.mu.Unlock()
	arb := s.arbiters[sessionID]
	if arb == nil {
		return SourceUnknown
	}
	return arb.PrimarySource()
}

func (s *Service) HasAgentEvents(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	arb := s.arbiters[sessionID]
	if arb == nil {
		return false
	}
	return arb.HasAgentEvents()
}

func (s *Service) AddSnapshotSegment(sessionID string, text string, byteCount int, observedAt time.Time, generation int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.matchGeneration(sessionID, generation) {
		return
	}
	arb := s.ensureArbiter(sessionID)
	if arb.IsByteStreamSuppressed() {
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
}

func (s *Service) FeedBytesBatch(sessionID string, segments []TranscriptSegment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	arb := s.ensureArbiter(sessionID)
	arb.RecordByteStream()
	s.store.Append(sessionID, segments)
}

func (s *Service) BuildResponse(sessionID string, segments []TranscriptSegment) TranscriptResponse {
	s.mu.Lock()
	arb := s.arbiters[sessionID]
	s.mu.Unlock()
	resp := NewTranscriptResponse(sessionID, segments, arb)
	resp.Generation = s.GetGeneration(sessionID)
	return resp
}

func (s *Service) CloseSessionQueue(sessionID string, generation int64) {
	s.mu.Lock()
	if !s.matchGeneration(sessionID, generation) {
		s.mu.Unlock()
		return
	}
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

// ── PA3 Closeout B: generation-bound lease ──

func (s *Service) matchGeneration(sessionID string, generation int64) bool {
	cur := s.currentGen[sessionID]
	if cur == 0 {
		return true // no generation established yet (test-only path)
	}
	return cur == generation
}

func (s *Service) GetGeneration(sessionID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.currentGen[sessionID]
}

func (s *Service) ReplaceTranscript(sessionID string) int64 {
	s.mu.Lock()
	oldQueue := s.queues[sessionID]
	delete(s.queues, sessionID)
	s.store.Clear(sessionID)
	delete(s.byteProjs, sessionID)
	delete(s.arbiters, sessionID)
	if _, ok := s.currentGen[sessionID]; !ok {
		s.currentGen[sessionID] = 1
	} else {
		s.currentGen[sessionID]++
	}
	gen := s.currentGen[sessionID]
	q := newChunkQueue(sessionID, s, gen)
	s.queues[sessionID] = q
	s.mu.Unlock()
	if oldQueue != nil {
		oldQueue.close()
	}
	RemoveLaunch(sessionID)
	log.Printf("TRANSCRIPT replace session=%s gen=%d", sessionID, gen)
	return gen
}
