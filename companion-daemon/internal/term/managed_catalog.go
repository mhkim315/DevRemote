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
	"fmt"
	"log"
	"sort"
	"strings"

	"devremote/companion-daemon/internal/sessionid"
)

// ManagedRuntimeCatalog is the read-only federated view over managed
// runtime registries. It never probes legacy discovery, process
// names, panes, screen text, PTY bytes, or JSONL.
type ManagedRuntimeCatalog interface {
	// Get returns the exact provider-owned record for sessionID.
	// The adapter prefix selects the registry; an ambiguous ID (present
	// in both registries) fails closed. Unknown/malformed/pane-style
	// prefixes return (zero, false).
	Get(sessionID string) (ManagedSessionRecord, bool)

	// List returns defensive copies of all records from both registries,
	// sorted by CreatedAt ascending then SessionID ascending.
	// Ambiguous IDs (present in both registries) are excluded entirely.
	List() []ManagedSessionRecord

	// RuntimeOf resolves the current runtime identity for sessionID by
	// delegating to the accepted provider-specific RuntimeOf method.
	// Uninstalled services, unknown adapters, ambiguous IDs, and stale
	// generations return (zero, false).
	RuntimeOf(sessionID string) (RuntimeRef, bool)

	// ManagedAdapterPrefixes returns the canonical adapter prefixes that
	// identify managed sessions. Any Registry row whose ID begins with one
	// of these prefixes must be excluded — the catalog is the sole
	// authority for managed session list rows.
	ManagedAdapterPrefixes() []string

	// ManagedCapabilities returns the static capability sets for a managed
	// adapter. The returned capabilities are authoritative and never depend
	// on legacy discovery or observer evidence.
	ManagedCapabilities(adapter string) (sessionCaps, adapterCaps []string)
}

// managedRuntimeCatalog implements ManagedRuntimeCatalog.
type managedRuntimeCatalog struct {
	codexReg           *ManagedSessionRegistry
	claudeReg          *ManagedSessionRegistry
	codexRuntimeOf     func(string) (RuntimeRef, bool)
	claudeRuntimeOf    func(string) (RuntimeRef, bool)
	codexAuthorityVer  string
	claudeAuthorityVer string
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
	codexAuthorityVer string,
	claudeAuthorityVer string,
) ManagedRuntimeCatalog {
	return &managedRuntimeCatalog{
		codexReg:           codexReg,
		claudeReg:          claudeReg,
		codexRuntimeOf:     codexRuntimeOf,
		claudeRuntimeOf:    claudeRuntimeOf,
		codexAuthorityVer:  codexAuthorityVer,
		claudeAuthorityVer: claudeAuthorityVer,
	}
}

// validateManagedRecord checks that a stored record conforms to the
// canonical identity contract. A record that fails validation is
// malformed — it should never have been registered, and the catalog
// must not expose it.
func validateManagedRecord(rec *ManagedSessionRecord, expectAdapter string) error {
	ref := sessionid.ParseSessionID(rec.SessionID)
	if err := ref.Validate(); err != nil {
		return fmt.Errorf("invalid canonical session ID %q: %w", rec.SessionID, err)
	}
	if ref.Adapter != expectAdapter {
		return fmt.Errorf("session ID adapter %q does not match registry origin %q", ref.Adapter, expectAdapter)
	}
	// Exact provider-origin binding: Codex registry must have Provider=="codex";
	// Claude registry must have Provider=="claude". Case-mutated, whitespace-mutated,
	// cross-provider, empty, and unknown values are rejected.
	expectProvider := "codex"
	if expectAdapter == claudeHeadlessAdapter {
		expectProvider = "claude"
	}
	if rec.Provider != expectProvider {
		return fmt.Errorf("provider %q does not match registry origin %q for session %q", rec.Provider, expectProvider, rec.SessionID)
	}
	if rec.Version == "" {
		return fmt.Errorf("empty version for session %q", rec.SessionID)
	}
	return nil
}

// Get selects the exact provider-owned registry from the canonical
// session ID adapter prefix. It cross-checks the other registry to
// detect ambiguous duplicates (fail closed). Returned records are
// validated; malformed stored records are not exposed.
func (c *managedRuntimeCatalog) Get(sessionID string) (ManagedSessionRecord, bool) {
	ref := sessionid.ParseSessionID(sessionID)
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
		if err := validateManagedRecord(&rec, codexAppServerAdapter); err != nil {
			log.Printf("managed catalog: Get skipping invalid codex record: %v", err)
			return ManagedSessionRecord{}, false
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
		if err := validateManagedRecord(&rec, claudeHeadlessAdapter); err != nil {
			log.Printf("managed catalog: Get skipping invalid claude record: %v", err)
			return ManagedSessionRecord{}, false
		}
		return rec, true
	default:
		return ManagedSessionRecord{}, false
	}
}

// List merges both registries and returns defensive copies sorted by
// CreatedAt then SessionID. Records that fail validation are skipped.
// Ambiguous SessionIDs (present in both registries) are excluded
// entirely — neither copy appears in the output, even if one copy is
// malformed in its own registry.
func (c *managedRuntimeCatalog) List() []ManagedSessionRecord {
	type sourced struct {
		rec    ManagedSessionRecord
		origin string // "codex" or "claude"
	}
	var all []sourced

	// First pass: collect every record with its origin. Do NOT filter
	// malformed records yet — a malformed record in one registry may
	// still be evidence of an ambiguous ID when the same ID appears
	// (validly) in the other registry.
	if c.codexReg != nil {
		for _, rec := range c.codexReg.List() {
			all = append(all, sourced{rec, "codex"})
		}
	}
	if c.claudeReg != nil {
		for _, rec := range c.claudeReg.List() {
			all = append(all, sourced{rec, "claude"})
		}
	}
	if len(all) == 0 {
		return []ManagedSessionRecord{}
	}

	// Second pass: detect ambiguous SessionIDs (present in both origins).
	codexIDs := make(map[string]struct{})
	claudeIDs := make(map[string]struct{})
	for _, s := range all {
		if s.origin == "codex" {
			codexIDs[s.rec.SessionID] = struct{}{}
		} else {
			claudeIDs[s.rec.SessionID] = struct{}{}
		}
	}

	// Third pass: exclude ambiguous records, validate the rest.
	deduped := make([]ManagedSessionRecord, 0, len(all))
	for _, s := range all {
		_, inCodex := codexIDs[s.rec.SessionID]
		_, inClaude := claudeIDs[s.rec.SessionID]
		if inCodex && inClaude {
			log.Printf("managed catalog: ambiguous session %q dropped from list (found in both registries)", s.rec.SessionID)
			continue
		}
		expectAdapter := codexAppServerAdapter
		if s.origin == "claude" {
			expectAdapter = claudeHeadlessAdapter
		}
		if err := validateManagedRecord(&s.rec, expectAdapter); err != nil {
			log.Printf("managed catalog: List skipping invalid %s record: %v", s.origin, err)
			continue
		}
		deduped = append(deduped, s.rec)
	}
	if len(deduped) == 0 {
		return []ManagedSessionRecord{}
	}

	sort.Slice(deduped, func(i, j int) bool {
		if !deduped[i].CreatedAt.Equal(deduped[j].CreatedAt) {
			return deduped[i].CreatedAt.Before(deduped[j].CreatedAt)
		}
		return deduped[i].SessionID < deduped[j].SessionID
	})
	return deduped
}

// RuntimeOf resolves the current runtime identity for sessionID by
// first verifying the ID is valid and non-ambiguous (same path as
// Get), then delegating to the accepted provider-specific RuntimeOf.
// The returned RuntimeRef is cross-validated against the catalog
// record so the read boundary and execution-authority boundary agree.
func (c *managedRuntimeCatalog) RuntimeOf(sessionID string) (RuntimeRef, bool) {
	// Verify the ID is valid and non-ambiguous via the same path as Get.
	rec, ok := c.Get(sessionID)
	if !ok {
		return RuntimeRef{}, false
	}

	ref := sessionid.ParseSessionID(sessionID)
	var resolver func(string) (RuntimeRef, bool)
	switch ref.Adapter {
	case codexAppServerAdapter:
		resolver = c.codexRuntimeOf
	case claudeHeadlessAdapter:
		resolver = c.claudeRuntimeOf
	default:
		return RuntimeRef{}, false
	}
	if resolver == nil {
		return RuntimeRef{}, false
	}

	rt, rtOk := resolver(sessionID)
	if !rtOk {
		return RuntimeRef{}, false
	}

	// Cross-validate: the RuntimeRef must agree with the catalog record on
	// provider, version, adapter, and generation. Provider-specific version
	// policy: the RuntimeRef version must equal the certified authority version
	// configured at activation time. No generic string normalization.
	expectedVer := c.codexAuthorityVer
	if ref.Adapter == claudeHeadlessAdapter {
		expectedVer = c.claudeAuthorityVer
	}
	if rt.Adapter != ref.Adapter || rt.LaunchGen != rec.Epoch || rt.Version != expectedVer {
		log.Printf("managed catalog: RuntimeOf binding mismatch for %q: ref={Adapter=%s Version=%s LaunchGen=%d} rec={adapter=%s epoch=%d} wantVersion=%s",
			sessionID, rt.Adapter, rt.Version, rt.LaunchGen, ref.Adapter, rec.Epoch, expectedVer)
		return RuntimeRef{}, false
	}
	return rt, true
}

// ManagedAdapterPrefixes returns the adapter prefixes that identify
// managed sessions. Any Registry row matching these prefixes must be
// excluded from the session list — the catalog is the sole authority.
func (c *managedRuntimeCatalog) ManagedAdapterPrefixes() []string {
	// Canonical managed adapter prefixes are always returned, even when
	// neither registry is configured. This prevents Registry-only rows
	// with managed-looking IDs from fabricating managed projections.
	return []string{codexAppServerAdapter, claudeHeadlessAdapter}
}

// ManagedCapabilities returns the authoritative capability sets for a
// managed adapter. These are static per managed runtime and never depend
// on legacy discovery or observer evidence.
func (c *managedRuntimeCatalog) ManagedCapabilities(adapter string) (sessionCaps, adapterCaps []string) {
	switch adapter {
	case codexAppServerAdapter, claudeHeadlessAdapter:
		return []string{"live_stream", "history"},
			[]string{"live_stream", "history", "process"}
	default:
		return nil, nil
	}
}

// appendCatalogRows appends managed-session rows built from the catalog
// to the /api/sessions response. It replaces appendManagedRows and
// appendClaudeManagedRows with a single catalog-driven projector.
//
// PA4.1 R2: Managed identity is authoritative — any snapshot row whose
// canonical ID collides with a managed session or whose adapter prefix
// matches a managed adapter is dropped. Contradictory observer evidence
// can never overwrite, shadow, or fabricate a managed-looking list row.
// Managed row metadata (capabilities, lifecycle) is sourced exclusively
// from ManagedRuntimeCatalog contract methods, never hard-coded or from
// legacy adapter registry.
func appendCatalogRows(snapshot []SessionTelemetry, catalog ManagedRuntimeCatalog, lifecycle *LifecycleService, approvals *AuthoritativeApprovalStore) []SessionTelemetry {
	if catalog == nil {
		return snapshot
	}
	recs := catalog.List()

	// Build set of managed canonical IDs (collision exclusion).
	managedIDs := make(map[string]struct{}, len(recs))
	for _, rec := range recs {
		managedIDs[rec.SessionID] = struct{}{}
	}

	// Managed adapter prefixes — any Registry row with one of these
	// prefixes is excluded even without a catalog collision (ghost).
	prefixes := catalog.ManagedAdapterPrefixes()

	out := make([]SessionTelemetry, 0, len(snapshot)+len(recs))
	for _, row := range snapshot {
		// Drop if collides with a managed catalog ID.
		if _, collides := managedIDs[row.ID]; collides {
			continue
		}
		// Drop if ID matches a managed adapter prefix (ghost prevention).
		ghost := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(row.ID, prefix+":") {
				ghost = true
				break
			}
		}
		if ghost {
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

		// PA4.1: managed lifecycle state from OwnedPTYRuntime (for
		// controlled_pty) or from the catalog's native status.
		lifecycleState := ""
		if lifecycle != nil {
			if e, ok := lifecycle.OwnedPTY().Get(rec.SessionID); ok {
				lifecycleState = string(e.State)
			}
		}

		// PA4.1 R2: capabilities from catalog contract, not hard-coded.
		sessCaps, adapterCaps := catalog.ManagedCapabilities(adapter)

		out = append(out, SessionTelemetry{
			ID:                  rec.SessionID,
			DisplayID:           strings.TrimPrefix(rec.SessionID, adapter+":"),
			State:               string(rec.NativeStatus),
			Adapter:             adapter,
			Runner:              rec.Provider,
			RunnerColor:         runnerColor,
			AgentKind:           rec.Provider,
			AgentStatus:         string(rec.NativeStatus),
			LifecycleState:      lifecycleState,
			Capabilities:        sessCaps,
			AdapterCapabilities: adapterCaps,
			Approvals:           safe,
		})
	}
	return out
}
