// Package notification implements the non-authoritative N1 locator protocol.
package notification

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"devremote/companion-daemon/internal/timeline/contract"
)

const Window = 4096

type Locator struct {
	EventID    string             `json:"eventId"`
	SessionID  string             `json:"sessionId"`
	RuntimeID  string             `json:"runtimeId"`
	Generation int64              `json:"generation"`
	Kind       contract.EventKind `json:"kind"`
	Timestamp  time.Time          `json:"timestamp"`
	N1Token    string             `json:"n1Token"`
}

func Token(eventID string, generation int64) string {
	h := sha256.Sum256([]byte(eventID + ":" + itoa(generation)))
	return hex.EncodeToString(h[:])
}
func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	b := make([]byte, 0, 20)
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
func Build(e contract.Envelope, currentGeneration int64) (Locator, bool) {
	if e.LaunchGeneration != currentGeneration || !allowed(e.EventKind) {
		return Locator{}, false
	}
	return Locator{EventID: e.EventID, SessionID: e.SessionID, RuntimeID: e.RuntimeID, Generation: e.LaunchGeneration, Kind: e.EventKind, Timestamp: e.OccurredAt, N1Token: Token(e.EventID, e.LaunchGeneration)}, true
}
func allowed(k contract.EventKind) bool {
	switch k {
	case contract.EventProviderInvocationFinished, contract.EventApprovalRequested, contract.EventApprovalResolved, contract.EventToolCallFinished:
		return true
	}
	return false
}

// Dedup is bounded and deliberately persistent through process restart when
// callers retain it; fire-and-forget delivery is at-most-once, never ACK-exactly-once.
type Dedup struct {
	mu sync.Mutex
	l  *list.List
	m  map[string]*list.Element
}

func NewDedup() *Dedup { return &Dedup{l: list.New(), m: map[string]*list.Element{}} }
func (d *Dedup) Claim(eventID string, gen int64) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := Token(eventID, gen)
	if e := d.m[k]; e != nil {
		d.l.MoveToFront(e)
		return false
	}
	d.m[k] = d.l.PushFront(k)
	if d.l.Len() > Window {
		e := d.l.Back()
		delete(d.m, e.Value.(string))
		d.l.Remove(e)
	}
	return true
}

type Cursor struct {
	DeviceID, LastEventID string
	LastGeneration        int64
}

// SelectSince scans ReadRecent's bounded order. A missing prior event is a
// ring-wrap recovery: return all retained events rather than claiming a cursor.
func SelectSince(events []contract.Envelope, c Cursor) ([]contract.Envelope, bool) {
	if c.LastEventID == "" {
		return events, false
	}
	for i, e := range events {
		if e.EventID == c.LastEventID && e.LaunchGeneration == c.LastGeneration {
			return events[i+1:], false
		}
	}
	return events, true
}
