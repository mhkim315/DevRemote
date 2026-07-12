package transcript

import (
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
)

// Service is the per-process Transcript integration service.
// It owns the bounded store, projectors, and per-session arbitration state.
// One Service instance is shared across all sessions.
type Service struct {
	store     *Store
	agentProj *AgentEventProjector
	byteProjs map[string]*ByteStreamProjector // sessionID → projector
	arbiters  map[string]*SourceArbiter       // sessionID → arbiter
	mu        sync.Mutex
}

// NewService creates a Transcript integration service.
func NewService(cfg StoreConfig) *Service {
	return &Service{
		store:     NewStore(cfg),
		agentProj: NewAgentEventProjector(),
		byteProjs: make(map[string]*ByteStreamProjector),
		arbiters:  make(map[string]*SourceArbiter),
	}
}

// ── AgentEvent projection path ──

// ProjectAgentEvents projects accepted AgentEvents into the Transcript store.
// Only events bound to the requested session are projected. Cross-session
// events are silently skipped.
func (s *Service) ProjectAgentEvents(sessionID string, events []agent.AgentEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	arb := s.ensureArbiter(sessionID)

	segments := s.agentProj.ProjectBatch(events, sessionID)
	if len(segments) > 0 {
		arb.RecordAgentEvent()
		s.store.Append(sessionID, segments)
	}
}

// ── Byte-stream fallback path ──

// FeedBytes processes a Recorder byte chunk through the byte-stream projector
// and appends any resulting segments to the store.
func (s *Service) FeedBytes(sessionID string, chunk []byte, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bp := s.ensureByteProj(sessionID)
	arb := s.ensureArbiter(sessionID)

	segments := bp.Feed(sessionID, chunk, observedAt)
	if len(segments) > 0 {
		arb.RecordByteStream()
		s.store.Append(sessionID, segments)
	}
}

// BeginInput marks the start of terminal input for echo suppression.
func (s *Service) BeginInput(sessionID string, observedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bp := s.ensureByteProj(sessionID)
	arb := s.ensureArbiter(sessionID)

	seg := bp.BeginInput(sessionID, observedAt)
	if seg != nil {
		s.store.Append(sessionID, []TranscriptSegment{*seg})
	}
	arb.RecordByteStream()
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

// ClearTranscript removes all segments for a session.
func (s *Service) ClearTranscript(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.store.Clear(sessionID)
	delete(s.byteProjs, sessionID)
	delete(s.arbiters, sessionID)
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

// FeedBytesBatch directly appends pre-built terminal output segments.
// Used when the caller already has structured terminal output (e.g., from
// ActivityBuffer replay) and doesn't need byte-stream character processing.
func (s *Service) FeedBytesBatch(sessionID string, segments []TranscriptSegment) {
	s.mu.Lock()
	defer s.mu.Unlock()

	arb := s.ensureArbiter(sessionID)
	arb.RecordByteStream()
	s.store.Append(sessionID, segments)
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
