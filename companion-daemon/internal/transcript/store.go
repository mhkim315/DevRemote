package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
// stable unique IDs and monotonic Seq values. Oldest segments are evicted when
// bounds are exceeded; a single coalesced KindDegraded gap marker replaces
// evicted segments. The final count never exceeds MaxSegments (gap marker
// included).
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

	for i := range segments {
		if segments[i].ObservedAt.IsZero() {
			segments[i].ObservedAt = time.Now()
		}
		ss.nextSeq++
		segments[i].Seq = ss.nextSeq
		if segments[i].ID == "" {
			segments[i].ID = segmentID(segments[i], ss.nextSeq)
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
		GapCount:     ss.totalEvicted,
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
	segments     []TranscriptSegment
	nextSeq      int64
	totalBytes   int
	totalEvicted int // cumulative evicted count (diagnostic only)
	hasGap       bool
}

// evictLocked removes oldest segments until within bounds, then inserts or
// updates a single coalesced gap marker. The final segment count is always
// ≤ MaxSegments including any gap marker.
func (s *Store) evictLocked(ss *sessionStore, sessionID string) {
	cfg := s.cfg
	evictedThisCall := 0

	// Evict by count. Reserve one slot for a potential gap marker.
	limit := cfg.MaxSegments
	if limit > 1 {
		limit-- // reserve for gap
	}
	for len(ss.segments) > limit {
		evicted := ss.segments[0]
		ss.totalBytes -= len(evicted.Text)
		ss.segments = ss.segments[1:]
		evictedThisCall++
	}

	// Evict by byte total (also reserve one slot for gap).
	for ss.totalBytes > cfg.MaxTotalBytes && len(ss.segments) > 1 {
		evicted := ss.segments[0]
		ss.totalBytes -= len(evicted.Text)
		ss.segments = ss.segments[1:]
		evictedThisCall++
	}

	if evictedThisCall == 0 {
		return
	}

	ss.totalEvicted += evictedThisCall

	// Coalesce: update existing gap marker if the first segment is already a gap.
	if ss.hasGap && len(ss.segments) > 0 && ss.segments[0].Kind == KindDegraded {
		ss.segments[0].DegradedReason = fmt.Sprintf(
			"older transcript segments evicted (capacity bound, %d total evicted)", ss.totalEvicted,
		)
		ss.segments[0].ObservedAt = time.Now()
		return
	}

	// Insert a single coalesced gap marker at the front.
	gap := NewDegradedSegment(sessionID,
		fmt.Sprintf("older transcript segments evicted (capacity bound, %d total evicted)", ss.totalEvicted),
		time.Now(),
	)
	gap.Seq = ss.segments[0].Seq - 1
	if gap.Seq < 1 {
		gap.Seq = 0
	}
	gap.ID = segmentID(gap, gap.Seq)
	gap.ContractVersion = ContractVersion
	ss.segments = append([]TranscriptSegment{gap}, ss.segments...)
	ss.hasGap = true
	ss.totalBytes += len(gap.Text)
}

// segmentID generates a unique segment ID from content + sequence.
func segmentID(seg TranscriptSegment, seq int64) string {
	h := sha256.New()
	h.Write([]byte(seg.SessionID))
	h.Write([]byte(seg.Kind))
	h.Write([]byte(seg.Source))
	h.Write([]byte(seg.Text))
	h.Write([]byte(seg.AgentEventRef))
	// Seq makes identical content have distinct IDs.
	h.Write([]byte(fmt.Sprintf(":%d", seq)))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:])[:16]
}

// ── Atomic cursor for non-blocking reads ──

type Cursor struct {
	seq atomic.Int64
}

func NewCursor(s *Store, sessionID string) *Cursor {
	c := &Cursor{}
	c.seq.Store(s.LatestSeq(sessionID))
	return c
}

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

func (c *Cursor) Seq() int64 {
	return c.seq.Load()
}
