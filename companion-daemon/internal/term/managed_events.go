// Package term — SP0.5: bounded per-session managed event store. One
// monotonic sequence per managed session; ring-bounded with an explicit gap
// marker on overflow (a cursor below the ring floor can never observe a
// silently complete history). Reads are snapshot+cursor; wrong cursor fails
// closed. The store holds ONLY projected, bounded, provider-neutral events —
// never raw JSON-RPC, prompts, or provider payloads.
package term

import (
	"fmt"
	"sync"
	"time"
)

const (
	// managedEventContractVersion is validated exactly by consumers.
	managedEventContractVersion = "pokit.managed.v1"
	managedEventRingCap         = 256
	managedEventTextMax         = 4096
	managedPromptMaxBytes       = 4096
)

// ManagedEventKind is the closed managed event vocabulary.
type ManagedEventKind string

const (
	ManagedEventAssistant ManagedEventKind = "assistant"
	ManagedEventWorking   ManagedEventKind = "working"
	ManagedEventCompleted ManagedEventKind = "completed"
	ManagedEventExited    ManagedEventKind = "exited"
	ManagedEventGap       ManagedEventKind = "gap"
)

// ManagedEvent is the versioned provider-neutral managed event DTO shared by
// the local attach stream and the authenticated mobile read surface. Field
// set is closed; Text is byte-bounded and present only for assistant events.
type ManagedEvent struct {
	ContractVersion string           `json:"contractVersion"`
	SessionID       string           `json:"sessionId"`
	Epoch           int64            `json:"epoch"`
	Seq             uint64           `json:"seq"`
	Kind            ManagedEventKind `json:"kind"`
	Text            string           `json:"text,omitempty"`
	ObservedAt      string           `json:"observedAt"`
}

// managedEventStore is the bounded per-session event ring. One mutex guards
// seq/floor/ring/closed/subscribers; no lock is held across I/O.
type managedEventStore struct {
	mu        sync.Mutex
	closed    bool
	sessionID string
	epoch     int64
	seq       uint64 // last assigned seq (0 = none yet)
	floor     uint64 // seq of the oldest retained event (0 = none yet)
	ring      []ManagedEvent
	subs      map[chan struct{}]struct{}
}

func newManagedEventStore(sessionID string, epoch int64) *managedEventStore {
	return &managedEventStore{
		sessionID: sessionID,
		epoch:     epoch,
		subs:      make(map[chan struct{}]struct{}),
	}
}

// append projects one bounded event into the ring and wakes subscribers.
// Appends on a closed store are inert.
func (st *managedEventStore) append(kind ManagedEventKind, text string) {
	if len(text) > managedEventTextMax {
		text = text[:managedEventTextMax]
	}
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return
	}
	st.seq++
	ev := ManagedEvent{
		ContractVersion: managedEventContractVersion,
		SessionID:       st.sessionID,
		Epoch:           st.epoch,
		Seq:             st.seq,
		Kind:            kind,
		Text:            text,
		ObservedAt:      time.Now().UTC().Format(time.RFC3339),
	}
	st.ring = append(st.ring, ev)
	if len(st.ring) > managedEventRingCap {
		st.ring = st.ring[1:]
	}
	st.floor = st.ring[0].Seq
	for ch := range st.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	st.mu.Unlock()
}

// readAfter returns events with Seq > cursor (bounded by max). A cursor ahead
// of the newest seq, or a read on a closed store, fails closed. When the
// cursor has fallen below the ring floor, a synthetic gap event precedes the
// retained events — the dropped range is never silently skipped. Re-reading
// the same cursor is idempotent.
func (st *managedEventStore) readAfter(cursor uint64, max int) ([]ManagedEvent, error) {
	if max <= 0 {
		max = managedEventRingCap
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.closed {
		return nil, fmt.Errorf("managed event store closed")
	}
	if cursor > st.seq {
		return nil, fmt.Errorf("cursor %d ahead of newest event %d", cursor, st.seq)
	}
	out := make([]ManagedEvent, 0, 8)
	if st.floor > 0 && cursor+1 < st.floor {
		// Events (cursor, floor) were evicted: mark the hole explicitly.
		out = append(out, ManagedEvent{
			ContractVersion: managedEventContractVersion,
			SessionID:       st.sessionID,
			Epoch:           st.epoch,
			Seq:             st.floor - 1,
			Kind:            ManagedEventGap,
			ObservedAt:      time.Now().UTC().Format(time.RFC3339),
		})
	}
	for _, ev := range st.ring {
		if ev.Seq <= cursor {
			continue
		}
		out = append(out, ev)
		if len(out) >= max {
			break
		}
	}
	return out, nil
}

// newest returns the last assigned sequence number (the read cursor ceiling).
func (st *managedEventStore) newest() uint64 {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.seq
}

// subscribe registers a wakeup channel signalled on every append.
func (st *managedEventStore) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	st.mu.Lock()
	st.subs[ch] = struct{}{}
	st.mu.Unlock()
	return ch
}

func (st *managedEventStore) unsubscribe(ch chan struct{}) {
	st.mu.Lock()
	delete(st.subs, ch)
	st.mu.Unlock()
}

// close permanently rejects appends and reads (session deleted or daemon
// shutdown) and wakes subscribers so streamers observe the closure.
func (st *managedEventStore) close() {
	st.mu.Lock()
	st.closed = true
	for ch := range st.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	st.mu.Unlock()
}
