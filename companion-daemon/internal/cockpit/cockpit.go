// Package cockpit defines a read-only operational projection. It has no
// authority callbacks and cannot approve, deliver input, or mutate sessions.
package cockpit

import "sync"

type Origin struct{ Provider, SessionID, RuntimeID, Generation, Model, SnapshotID, EvidenceReference string }
type Item struct {
	Kind, State, Summary string
	Origin               Origin
	Stale                bool
}
type Projection struct{ Items []Item }

func (p Projection) ReadOnly() bool { return true }

// CockpitState is restart-volatile, read-only aggregated operational state.
type CockpitState struct {
	Sessions      []Item
	Approvals     []Item
	Findings      []Item
	Notifications []Item
}

// CockpitStore owns only a projection copy; it has no authority callbacks.
type CockpitStore struct {
	mu            sync.RWMutex
	sessions      []Item
	approvals     []Item
	findings      []Item
	notifications []Item
}

func NewCockpitStore() *CockpitStore                 { return &CockpitStore{} }
func (s *CockpitStore) AppendSession(session Item)   { s.append(&s.sessions, session) }
func (s *CockpitStore) AppendApproval(approval Item) { s.append(&s.approvals, approval) }
func (s *CockpitStore) AppendFinding(finding Item)   { s.append(&s.findings, finding) }
func (s *CockpitStore) AppendNotification(notification Item) {
	s.append(&s.notifications, notification)
}
func (s *CockpitStore) append(dst *[]Item, item Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	*dst = append(*dst, item)
}
func (s *CockpitStore) ReadAll() CockpitState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return CockpitState{Sessions: append([]Item(nil), s.sessions...), Approvals: append([]Item(nil), s.approvals...), Findings: append([]Item(nil), s.findings...), Notifications: append([]Item(nil), s.notifications...)}
}
func (s *CockpitStore) ReadOnly() bool { return true }
