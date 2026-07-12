package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"sync/atomic"
	"time"
)

// Store is the bounded, session-isolated Transcript storage.
// It is safe for concurrent append/read/delete across goroutines.
// Each session's segments are independently bounded; overflow on one
// session never affects another.
type Store struct {
	mu       sync.Mutex
	sessions map[string]*sessionStore
	cfg      StoreConfig
}

// StoreConfig controls per-session bounds.
type StoreConfig struct {
	// MaxSegments is the maximum number of segments per session.
	// Zero means use the default MaxSegmentsPerSession.
	MaxSegments int

	// MaxTotalBytes is the soft upper bound on total text bytes per session.
	// Zero means use the default MaxTotalBytesPerSession.
	MaxTotalBytes int
}

// DefaultStoreConfig returns the production config.
func DefaultStoreConfig() StoreConfig {
	return StoreConfig{
		MaxSegments:   MaxSegmentsPerSession,
		MaxTotalBytes: MaxTotalBytesPerSession,
	}
}

// NewStore creates a bounded Transcript store.
func NewStore(cfg StoreConfig) *Store {
	if cfg.MaxSegments <= 0 {
		cfg.MaxSegments = MaxSegmentsPerSession
	}
	if cfg.MaxTotalBytes <= 0 {
		cfg.MaxTotalBytes = MaxTotalBytesPerSession
	}
	return &Store{
		sessions: make(map[string]*sessionStore),
		cfg:      cfg,
	}
}

// Append adds segments to a session's Transcript. Segments are assigned
// stable IDs and monotonic Seq values. Oldest segments are evicted when
// bounds are exceeded; a KindDegraded gap marker is prepended.
func (s *Store) Append(sessionID string, segments []TranscriptSegment) {
	if len(segments) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ss := s.sessions[sessionID]
	if ss == nil {
		ss = &sessionStore{}
		s.sessions[sessionID] = ss
	}

	now := time.Now()
	for i := range segments {
		if segments[i].ObservedAt.IsZero() {
			segments[i].ObservedAt = now
		}
		ss.nextSeq++
		segments[i].Seq = ss.nextSeq
		if segments[i].ID == "" {
			segments[i].ID = segmentID(segments[i])
		}
		segments[i].ContractVersion = ContractVersion
		ss.totalBytes += len(segments[i].Text)
	}

	ss.segments = append(ss.segments, segments...)

	// Evict oldest segments when over bounds.
	s.evictLocked(ss, sessionID)
}

// List returns all segments for a session, oldest-first.
// Returns nil if the session has no segments.
func (s *Store) List(sessionID string) []TranscriptSegment {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss := s.sessions[sessionID]
	if ss == nil || len(ss.segments) == 0 {
		return nil
	}

	out := make([]TranscriptSegment, len(ss.segments))
	copy(out, ss.segments)
	return out
}

// ListAfter returns segments with Seq > cursor, oldest-first.
// Used for incremental reads.
func (s *Store) ListAfter(sessionID string, cursor int64) []TranscriptSegment {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss := s.sessions[sessionID]
	if ss == nil {
		return nil
	}

	for i, seg := range ss.segments {
		if seg.Seq > cursor {
			out := make([]TranscriptSegment, len(ss.segments)-i)
			copy(out, ss.segments[i:])
			return out
		}
	}
	return nil
}

// LatestSeq returns the most recent Seq for a session, or 0 if empty.
func (s *Store) LatestSeq(sessionID string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss := s.sessions[sessionID]
	if ss == nil || len(ss.segments) == 0 {
		return 0
	}
	return ss.segments[len(ss.segments)-1].Seq
}

// Clear removes all segments for a session. Idempotent.
func (s *Store) Clear(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

// Stats returns diagnostic counters for a session.
func (s *Store) Stats(sessionID string) StoreStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	ss := s.sessions[sessionID]
	if ss == nil {
		return StoreStats{}
	}
	return StoreStats{
		SegmentCount: len(ss.segments),
		TotalBytes:   ss.totalBytes,
		LatestSeq:    ss.nextSeq,
		GapCount:     ss.gapCount,
	}
}

// StoreStats is a diagnostic snapshot.
type StoreStats struct {
	SegmentCount int
	TotalBytes   int
	LatestSeq    int64
	GapCount     int
}

// ── Internal per-session store ──

type sessionStore struct {
	segments   []TranscriptSegment
	nextSeq    int64
	totalBytes int
	gapCount   int
}

// evictLocked removes oldest segments until within bounds.
// Must be called with s.mu held.
func (s *Store) evictLocked(ss *sessionStore, sessionID string) {
	cfg := s.cfg

	// Evict by count.
	for len(ss.segments) > cfg.MaxSegments {
		evicted := ss.segments[0]
		ss.totalBytes -= len(evicted.Text)
		ss.segments = ss.segments[1:]
		ss.gapCount++
	}

	// Evict by byte total.
	for ss.totalBytes > cfg.MaxTotalBytes && len(ss.segments) > 1 {
		evicted := ss.segments[0]
		ss.totalBytes -= len(evicted.Text)
		ss.segments = ss.segments[1:]
		ss.gapCount++
	}

	// If we evicted anything, prepend a gap marker.
	if ss.gapCount > 0 && len(ss.segments) > 0 {
		gap := NewDegradedSegment(sessionID, "older transcript segments evicted (capacity bound)", time.Now())
		gap.Seq = ss.segments[0].Seq - 1
		gap.ID = segmentID(gap)
		gap.ContractVersion = ContractVersion
		ss.segments = append([]TranscriptSegment{gap}, ss.segments...)
	}
}

// segmentID generates a stable, unique segment ID from content.
func segmentID(seg TranscriptSegment) string {
	h := sha256.New()
	h.Write([]byte(seg.SessionID))
	h.Write([]byte(seg.Kind))
	h.Write([]byte(seg.Source))
	h.Write([]byte(seg.Text))
	h.Write([]byte(seg.AgentEventRef))
	// Seq and ObservedAt are not included — ID is content-stable.
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:])[:16]
}

// ── Atomic cursor for non-blocking reads ──

// Cursor is a lock-free snapshot of the latest Seq for a session.
// Readers can use it to request incremental updates.
type Cursor struct {
	seq atomic.Int64
}

// NewCursor returns a cursor initialised to the store's current latest seq.
func NewCursor(s *Store, sessionID string) *Cursor {
	c := &Cursor{}
	c.seq.Store(s.LatestSeq(sessionID))
	return c
}

// Advance updates the cursor to at least the given seq.
func (c *Cursor) Advance(seq int64) {
	for {
		cur := c.seq.Load()
		if seq <= cur {
			return
		}
		if c.seq.CompareAndSwap(cur, seq) {
			return
		}
	}
}

// Seq returns the current cursor position.
func (c *Cursor) Seq() int64 {
	return c.seq.Load()
}
