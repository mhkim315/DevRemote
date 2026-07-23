// Package cockpit defines a bounded, read-only operational projection. It has
// no authority callbacks and cannot approve, deliver input, or mutate sessions.
package cockpit

import (
	"strconv"
	"sync"

	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
	"devremote/companion-daemon/internal/validation"
)

const (
	MaxItems        = 256
	MaxFieldBytes   = 512
	recentEnvelopes = 128 // matches writer's ring buffer capacity
	recentResults   = 64  // matches validation store's ring buffer capacity
)

type Origin struct {
	Provider          string `json:"provider"`
	SessionID         string `json:"sessionId"`
	RuntimeID         string `json:"runtimeId"`
	Generation        string `json:"generation"`
	Model             string `json:"model,omitempty"`
	SnapshotID        string `json:"snapshotId,omitempty"`
	EvidenceReference string `json:"evidenceRef,omitempty"`
}

type Item struct {
	Kind    string `json:"kind"`
	State   string `json:"state"`
	Summary string `json:"summary"`
	Origin  Origin `json:"origin"`
	Stale   bool   `json:"stale"`
}

type Projection struct {
	Items []Item `json:"items"`
}

func (p Projection) ReadOnly() bool { return true }

// CockpitState is restart-volatile, read-only aggregated operational state.
type CockpitState struct {
	Sessions      []Item `json:"sessions"`
	Approvals     []Item `json:"approvals"`
	Findings      []Item `json:"findings"`
	Notifications []Item `json:"notifications"`
}

// Sources lists only existing read/observer seams. It deliberately has no
// authority methods and does not invent a producer state machine.
type Sources struct {
	Catalog    term.ManagedRuntimeCatalog
	Approvals  *term.AuthoritativeApprovalStore
	Timeline   *writer.Writer
	Validation *validation.ValidationStore
}

// CockpitStore owns only a bounded projection copy; it has no authority
// callbacks. Sources are polled on demand via Refresh — zero goroutines,
// zero subscriptions. Ring buffers remain readable after Close.
type CockpitStore struct {
	mu            sync.RWMutex
	sessions      []Item
	approvals     []Item
	findings      []Item
	notifications []Item
	sources       Sources
}

// NewCockpitStore constructs the projection from read-only sources.
// Polls sources on demand via Refresh; zero goroutines, no subscriptions.
func NewCockpitStore(sources ...Sources) *CockpitStore {
	s := &CockpitStore{}
	if len(sources) == 0 {
		return s
	}
	s.sources = sources[0]
	s.Refresh()
	return s
}

func (s *CockpitStore) AppendSession(session Item) bool   { return s.append(&s.sessions, session) }
func (s *CockpitStore) AppendApproval(approval Item) bool { return s.append(&s.approvals, approval) }
func (s *CockpitStore) AppendFinding(finding Item) bool   { return s.append(&s.findings, finding) }
func (s *CockpitStore) AppendNotification(notification Item) bool {
	return s.append(&s.notifications, notification)
}

func (s *CockpitStore) append(dst *[]Item, item Item) bool {
	if !validItem(item) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(*dst) >= MaxItems {
		return false
	}
	*dst = append(*dst, item)
	return true
}

// Refresh polls all sources: catalog (sessions+approvals), validation store
// (findings), and timeline writer (notifications). It never holds external
// locks while acquiring the cockpit lock.
func (s *CockpitStore) Refresh() {
	var sessions, approvals, findings, notifications []Item
	refreshRuntime := s.sources.Catalog != nil
	refreshFindings := s.sources.Validation != nil
	refreshNotifications := s.sources.Timeline != nil
	if catalog := s.sources.Catalog; catalog != nil {
		for _, record := range catalog.List() {
			generation := strconv.FormatInt(record.Epoch, 10)
			if runtime, ok := catalog.RuntimeOf(record.SessionID); ok {
				generation = strconv.FormatInt(runtime.LaunchGen, 10)
			}
			sessions = appendBounded(sessions, Item{
				Kind: "runtime", State: string(record.NativeStatus), Summary: record.SessionID,
				Origin: Origin{Provider: record.Provider, SessionID: record.SessionID, Generation: generation},
			})
			if s.sources.Approvals != nil {
				for _, approval := range s.sources.Approvals.ListSafe(record.SessionID) {
					approvals = appendBounded(approvals, Item{
						Kind: "approval", State: approval.State, Summary: approval.Summary,
						Origin: Origin{Provider: record.Provider, SessionID: approval.SessionID, Generation: generation},
					})
				}
			}
		}
	}
	if store := s.sources.Validation; store != nil {
		for _, result := range store.ReadRecent(recentResults) {
			for _, finding := range result.Findings {
				findings = appendBounded(findings, findingItem(result, finding))
			}
		}
	}
	if writer := s.sources.Timeline; writer != nil {
		for _, envelope := range writer.ReadRecent(recentEnvelopes) {
			notifications = appendBounded(notifications, timelineItem(envelope))
		}
	}
	s.mu.Lock()
	if refreshRuntime {
		s.sessions = sessions
		s.approvals = approvals
	}
	if refreshFindings {
		s.findings = findings
	}
	if refreshNotifications {
		s.notifications = notifications
	}
	s.mu.Unlock()
}

func (s *CockpitStore) ReadAll() CockpitState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return CockpitState{
		Sessions: cloneItems(s.sessions), Approvals: cloneItems(s.approvals),
		Findings: cloneItems(s.findings), Notifications: cloneItems(s.notifications),
	}
}

func (s *CockpitStore) ReadOnly() bool { return true }

// Close is a no-op — the ring-buffer model has no subscriptions to clean up.
// Timeline and validation stores remain owned by composition.
func (s *CockpitStore) Close() {}

func timelineItem(envelope contract.Envelope) Item {
	return Item{
		Kind: "notification", State: string(envelope.EventKind), Summary: envelope.EventID,
		Origin: Origin{Provider: envelope.Provider, SessionID: envelope.SessionID, RuntimeID: envelope.RuntimeID,
			Generation: strconv.FormatInt(envelope.LaunchGeneration, 10), EvidenceReference: envelope.EventID},
	}
}

func findingItem(result validation.ValidationResult, finding validation.Finding) Item {
	binding := result.Binding
	return Item{
		Kind: "validation", State: result.ID, Summary: finding.Summary, Stale: finding.Stale,
		Origin: Origin{Provider: binding.ValidatorProvider, SessionID: binding.ValidatorSessionID,
			RuntimeID: binding.ValidatorRuntimeID, Generation: strconv.FormatUint(binding.ValidatorGeneration, 10),
			Model: binding.ValidatorModel, SnapshotID: binding.SnapshotID, EvidenceReference: binding.EvidenceDigest},
	}
}

func appendBounded(items []Item, item Item) []Item {
	if len(items) >= MaxItems || !validItem(item) {
		return items
	}
	return append(items, item)
}

func cloneItems(items []Item) []Item { return append([]Item{}, items...) }

func validItem(item Item) bool {
	// Mandatory origin fields must be non-empty and bounded.
	for _, value := range []string{item.Kind, item.State, item.Summary, item.Origin.Provider, item.Origin.SessionID, item.Origin.Generation} {
		if value == "" || len(value) > MaxFieldBytes {
			return false
		}
	}
	// Optional origin fields are bounded when present. RuntimeID is omitted
	// when the ManagedRuntimeCatalog does not expose a per-runtime string id.
	for _, value := range []string{item.Origin.RuntimeID, item.Origin.Model, item.Origin.SnapshotID, item.Origin.EvidenceReference} {
		if len(value) > MaxFieldBytes {
			return false
		}
	}
	return true
}
