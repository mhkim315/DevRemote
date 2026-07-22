// Package cockpit defines a read-only operational projection. It has no
// authority callbacks and cannot approve, deliver input, or mutate sessions.
package cockpit

type Origin struct{ Provider, SessionID, RuntimeID, Generation, Model, SnapshotID, EvidenceReference string }
type Item struct {
	Kind, State, Summary string
	Origin               Origin
	Stale                bool
}
type Projection struct{ Items []Item }

func (p Projection) ReadOnly() bool { return true }
