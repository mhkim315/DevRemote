// Package term — PA1: read-only managed runtime catalog. Provides a
// federated view over the two accepted provider-owned registries (Codex
// and Claude). The catalog owns no store, cache, or mutation authority.
//
// Every lookup is dispatched by canonical adapter prefix. Unknown,
// malformed, ambiguous, and pane-style IDs fail closed. Listing merges
// both registries with deterministic ordering and returns defensive
// copies. RuntimeOf delegates to the accepted provider-specific methods
// without changing their generation or certification validation.
package term

import (
	"log"
	"sort"
	"strings"

	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

// ManagedRuntimeCatalog is the read-only federated view over managed
// runtime registries. It never probes mux.Registry, discovery, process
// names, panes, screen text, PTY bytes, or JSONL.
type ManagedRuntimeCatalog interface {
	// Get returns the exact provider-owned record for sessionID.
	// The adapter prefix selects the registry; an ambiguous ID (present
	// in both registries) fails closed. Unknown/malformed/pane-style
	// prefixes return (zero, false).
	Get(sessionID string) (ManagedSessionRecord, bool)

	// List returns defensive copies of all records from both registries,
	// sorted by CreatedAt ascending then SessionID ascending.
	List() []ManagedSessionRecord

	// RuntimeOf resolves the current runtime identity for sessionID by
	// delegating to the accepted provider-specific RuntimeOf method.
	// Uninstalled services, unknown adapters, and stale generations
	// return (zero, false).
	RuntimeOf(sessionID string) (RuntimeRef, bool)
}

// managedRuntimeCatalog implements ManagedRuntimeCatalog.
type managedRuntimeCatalog struct {
	codexReg        *ManagedSessionRegistry
	claudeReg       *ManagedSessionRegistry
	codexRuntimeOf  func(string) (RuntimeRef, bool)
	claudeRuntimeOf func(string) (RuntimeRef, bool)
}

// NewManagedRuntimeCatalog creates a read-only catalog over the two
// provider-owned registries and RuntimeOf resolvers. nil registries
// and resolvers are handled gracefully: a nil registry contributes
// zero records; a nil resolver returns (zero, false).
func NewManagedRuntimeCatalog(
	codexReg *ManagedSessionRegistry,
	claudeReg *ManagedSessionRegistry,
	codexRuntimeOf func(string) (RuntimeRef, bool),
	claudeRuntimeOf func(string) (RuntimeRef, bool),
) ManagedRuntimeCatalog {
	return &managedRuntimeCatalog{
		codexReg:        codexReg,
		claudeReg:       claudeReg,
		codexRuntimeOf:  codexRuntimeOf,
		claudeRuntimeOf: claudeRuntimeOf,
	}
}

// Get selects the exact provider-owned registry from the canonical
// session ID adapter prefix. It cross-checks the other registry to
// detect ambiguous duplicates (fail closed).
func (c *managedRuntimeCatalog) Get(sessionID string) (ManagedSessionRecord, bool) {
	ref := mux.ParseSessionID(sessionID)
	switch ref.Adapter {
	case codexAppServerAdapter:
		if c.codexReg == nil {
			return ManagedSessionRecord{}, false
		}
		rec, ok := c.codexReg.Get(sessionID)
		if !ok {
			return ManagedSessionRecord{}, false
		}
		if c.claudeReg != nil {
			if _, dup := c.claudeReg.Get(sessionID); dup {
				log.Printf("managed catalog: ambiguous session %q found in both registries", sessionID)
				return ManagedSessionRecord{}, false
			}
		}
		return rec, true
	case claudeHeadlessAdapter:
		if c.claudeReg == nil {
			return ManagedSessionRecord{}, false
		}
		rec, ok := c.claudeReg.Get(sessionID)
		if !ok {
			return ManagedSessionRecord{}, false
		}
		if c.codexReg != nil {
			if _, dup := c.codexReg.Get(sessionID); dup {
				log.Printf("managed catalog: ambiguous session %q found in both registries", sessionID)
				return ManagedSessionRecord{}, false
			}
		}
		return rec, true
	default:
		return ManagedSessionRecord{}, false
	}
}

// List merges both registries and returns defensive copies sorted by
// CreatedAt then SessionID. Duplicate SessionIDs (should be impossible
// due to distinct adapter prefixes) are logged and dropped; the first
// occurrence wins.
func (c *managedRuntimeCatalog) List() []ManagedSessionRecord {
	var all []ManagedSessionRecord
	if c.codexReg != nil {
		all = append(all, c.codexReg.List()...)
	}
	if c.claudeReg != nil {
		all = append(all, c.claudeReg.List()...)
	}
	if len(all) <= 1 {
		if all == nil {
			return []ManagedSessionRecord{}
		}
		return all
	}
	// Deduplicate: same SessionID in both registries → keep first, log.
	seen := make(map[string]struct{}, len(all))
	deduped := make([]ManagedSessionRecord, 0, len(all))
	for _, rec := range all {
		if _, exists := seen[rec.SessionID]; exists {
			log.Printf("managed catalog: duplicate session %q dropped from list merge", rec.SessionID)
			continue
		}
		seen[rec.SessionID] = struct{}{}
		deduped = append(deduped, rec)
	}
	sort.Slice(deduped, func(i, j int) bool {
		if !deduped[i].CreatedAt.Equal(deduped[j].CreatedAt) {
			return deduped[i].CreatedAt.Before(deduped[j].CreatedAt)
		}
		return deduped[i].SessionID < deduped[j].SessionID
	})
	return deduped
}

// RuntimeOf delegates to the accepted provider-specific RuntimeOf based
// on the canonical session ID adapter prefix. Unknown adapters and nil
// resolvers return (zero, false).
func (c *managedRuntimeCatalog) RuntimeOf(sessionID string) (RuntimeRef, bool) {
	ref := mux.ParseSessionID(sessionID)
	switch ref.Adapter {
	case codexAppServerAdapter:
		if c.codexRuntimeOf == nil {
			return RuntimeRef{}, false
		}
		return c.codexRuntimeOf(sessionID)
	case claudeHeadlessAdapter:
		if c.claudeRuntimeOf == nil {
			return RuntimeRef{}, false
		}
		return c.claudeRuntimeOf(sessionID)
	default:
		return RuntimeRef{}, false
	}
}

// appendCatalogRows appends managed-session rows built from the catalog
// to the /api/sessions response. It replaces appendManagedRows and
// appendClaudeManagedRows with a single catalog-driven projector.
// Managed identity is authoritative: any snapshot row that collides with
// a managed canonical ID is dropped so contradictory observer evidence
// can never overwrite or shadow the managed status.
func appendCatalogRows(snapshot []SessionTelemetry, catalog ManagedRuntimeCatalog, approvals *AuthoritativeApprovalStore) []SessionTelemetry {
	if catalog == nil {
		return snapshot
	}
	recs := catalog.List()
	if len(recs) == 0 {
		return snapshot
	}
	managedIDs := make(map[string]struct{}, len(recs))
	for _, rec := range recs {
		managedIDs[rec.SessionID] = struct{}{}
	}
	out := make([]SessionTelemetry, 0, len(snapshot)+len(recs))
	for _, row := range snapshot {
		if _, collides := managedIDs[row.ID]; collides {
			continue
		}
		out = append(out, row)
	}
	for _, rec := range recs {
		adapter := codexAppServerAdapter
		runnerColor := "#58a6ff"
		if strings.HasPrefix(rec.SessionID, claudeHeadlessAdapter+":") {
			adapter = claudeHeadlessAdapter
			runnerColor = "#f97316"
		}
		var safe []SafeApprovalDTO
		if approvals != nil {
			safe = approvals.ListSafe(rec.SessionID)
		}
		out = append(out, SessionTelemetry{
			ID:          rec.SessionID,
			DisplayID:   strings.TrimPrefix(rec.SessionID, adapter+":"),
			State:       string(rec.NativeStatus),
			Adapter:     adapter,
			Runner:      rec.Provider,
			RunnerColor: runnerColor,
			AgentKind:   rec.Provider,
			AgentStatus: string(rec.NativeStatus),
			Events:      []models.AgentEvent{},
			Approvals:   safe,
		})
	}
	return out
}
