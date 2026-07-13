package transcript

import (
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent/contract"
)

// LaunchBinding is an immutable session-owned launch authority.
// Only the session creation controller (pokit run) may create one.
//
// S1.1-B: Generation is a REAL monotonic launch-instance identity assigned by the
// registry (never supplied by the caller). PID+StartedAt are optional supporting
// process identity — when present they STRENGTHEN correlation (PID reuse with a
// different start time fails); when absent correlation is weaker/unavailable, never
// a wildcard. Process identity is NEVER agent-status authority.
type LaunchBinding struct {
	SessionID       string
	Provider        string    // "codex" or "claude"
	Adapter         string    // "controlled_pty"
	AcceptedVersion string    // "0.144.1" or "2.1.202"
	ProcessName     string    // executable name (codex/claude)
	PID             int       // process ID at launch time (0 = unknown)
	StartedAt       time.Time // process start time at launch (zero = unknown)
	Generation      int64     // monotonic launch instance identity (registry-assigned)
}

// LaunchSpec is the caller-supplied description of a managed launch. The registry
// assigns Generation; callers never set it. PID/StartedAt are optional and should
// be sourced from the session's own ProcessInfo so a later discovery of the same
// process reports identical values.
type LaunchSpec struct {
	SessionID   string
	Provider    string
	Adapter     string
	Version     string
	ProcessName string // defaults to Provider when empty
	PID         int
	StartedAt   time.Time
}

// LaunchRegistry stores active launch bindings and a monotonic launch counter.
// Only the session creation path may register bindings. TelemetryService reads
// them to establish correlation.
//
// Concurrency model (S1.1-R3):
//   - `mu` guards the binding map and the monotonic counter. It is a LEAF lock:
//     no method holds `mu` while calling into the term/status layer.
//   - `gates` is a FIXED-SIZE striped set of transition mutexes selected by a
//     stable hash of the canonical session ID (S1.1-R3 cleanup C1). A transition
//     holds its session's stripe for the WHOLE reserve→invalidate→publish, so
//     same-session replacements are fully serialized while different sessions run
//     concurrently (unless their IDs collide onto the same stripe, which only
//     serializes them — it never weakens correctness). The synchronization state
//     is constant-bounded: it never grows with the historical session count, so no
//     gate-retirement / ABA protocol is needed. The invalidation callback runs
//     while the stripe is held but `mu` is NOT — the audited lock order is
//     stripe → (mu released) → term s.mu / store.mu (each taken and released) → (mu
//     released) — with no inverse acquisition, so there is no deadlock.
type LaunchRegistry struct {
	mu       sync.Mutex
	bindings map[string]LaunchBinding // sessionID → binding
	// nextGen is a process-wide strictly-increasing launch counter. Using one
	// counter (rather than a per-session map) keeps the registry bounded to the
	// active bindings while guaranteeing that any later launch — including a
	// same-ID replacement or a delete→recreate — receives a strictly higher
	// generation than any earlier launch. Generations are opaque monotonic
	// tokens; they need not be dense per session.
	nextGen int64

	// gates is a fixed-size stripe array; a session maps to exactly one stripe by
	// launchGateStripes-modulo of its FNV-1a hash. Never resized, never deleted.
	gates [launchGateStripes]sync.Mutex
}

// launchGateStripes bounds the transition-synchronization state. 256 stripes make
// collisions rare in practice while keeping the memory constant regardless of how
// many unique sessions are created and deleted over the daemon's lifetime.
const launchGateStripes = 256

var globalLaunchRegistry = &LaunchRegistry{
	bindings: make(map[string]LaunchBinding),
}

// launchGateWaitHook is a test-only seam (nil in production); see its use in
// RegisterOrReplace.
var launchGateWaitHook func(sessionID string)

// sessionGate returns the FIXED striped transition mutex for a session, chosen by
// a stable FNV-1a hash modulo the stripe count. The returned pointer is stable for
// the process lifetime (the array never resizes), so a waiter can hold it safely
// with no retirement/ABA concern, and the synchronization state never grows with
// the number of historical sessions. A given session ID always maps to the same
// stripe, so its transitions and its removal are mutually serialized.
func (r *LaunchRegistry) sessionGate(sessionID string) *sync.Mutex {
	return &r.gates[launchGateStripe(sessionID)]
}

// launchGateStripe hashes a canonical session ID to a stripe index with FNV-1a
// (stable across runs and platforms; no Date/random dependency).
func launchGateStripe(sessionID string) uint32 {
	const (
		offset = 2166136261
		prime  = 16777619
	)
	h := uint32(offset)
	for i := 0; i < len(sessionID); i++ {
		h ^= uint32(sessionID[i])
		h *= prime
	}
	return h % launchGateStripes
}

// RegisterOrReplace is the ONE serialized per-session launch transition. It is the
// only path that may publish a binding. It:
//
//  1. acquires the session transition gate (serializes same-session replacements);
//  2. allocates the next monotonic generation and reads the current binding under
//     the short map mutex, then releases the map mutex;
//  3. if this is a REPLACEMENT (a binding already existed), it REQUIRES a non-nil
//     invalidate callback and runs invalidate(gen) — with NO registry lock held —
//     so the new generation's status/ingestion high-water is installed BEFORE the
//     binding is observable. A replacement with a nil callback FAILS CLOSED: the
//     existing binding is left unchanged and ok=false is returned. A nil callback
//     is never treated as permission to replace (S1.1-R3 cleanup C2);
//  4. publishes the new binding with a STRICT monotonic check: a generation that
//     is not strictly newer than the currently published one is rejected, so a
//     lower reserved generation can never overwrite a higher published one;
//  5. releases the gate.
//
// Returns the published generation, whether a prior binding existed (replaced),
// and ok — false only when a replacement was refused because no invalidation
// callback was supplied (the existing binding is then untouched). A first
// registration (no prior binding) always publishes and returns ok=true regardless
// of the callback.
func (r *LaunchRegistry) RegisterOrReplace(spec LaunchSpec, invalidate func(generation int64)) (published int64, replaced bool, ok bool) {
	gate := r.sessionGate(spec.SessionID)
	if launchGateWaitHook != nil {
		// Test-only seam: fires SYNCHRONOUSLY before blocking on the gate (before
		// generation allocation). Lets a deterministic test observe that a second
		// transition has reached the gate and will block, with no sleep/scheduling
		// race. Nil in production.
		launchGateWaitHook(spec.SessionID)
	}
	gate.Lock()
	defer gate.Unlock()

	// Step 2: allocate + read current under the short map mutex.
	r.mu.Lock()
	r.nextGen++
	gen := r.nextGen
	prev, existed := r.bindings[spec.SessionID]
	r.mu.Unlock()

	// C2: a replacement without an invalidation callback fails closed — the safe
	// pre-publication invalidation must run, so a nil callback may NOT replace.
	if existed && invalidate == nil {
		return prev.Generation, true, false
	}

	// Step 3: pre-publication invalidation for a replacement, outside the map mutex.
	if existed {
		invalidate(gen)
	}

	// Step 4: publish under the map mutex with a strict monotonic check.
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, curExists := r.bindings[spec.SessionID]
	if curExists && gen <= cur.Generation {
		// A concurrent transition already published a newer (or equal) generation
		// while this one was mid-flight. Never regress: keep the winner.
		return cur.Generation, true, true
	}
	name := spec.ProcessName
	if name == "" {
		name = spec.Provider
	}
	r.bindings[spec.SessionID] = LaunchBinding{
		SessionID:       spec.SessionID,
		Provider:        spec.Provider,
		Adapter:         spec.Adapter,
		AcceptedVersion: spec.Version,
		ProcessName:     name,
		PID:             spec.PID,
		StartedAt:       spec.StartedAt,
		Generation:      gen,
	}
	return gen, existed, true
}

// RegisterFirstLaunch records a FIRST managed launch binding. It is
// first-registration-only: if a binding for the session already exists it FAILS
// CLOSED (ok=false) and leaves the existing binding unchanged — it never replaces
// without invalidation. Use RegisterOrReplaceLaunch (with an invalidation
// callback) for any path that may replace a live binding. Returns the published
// generation and ok.
func RegisterFirstLaunch(spec LaunchSpec) (generation int64, ok bool) {
	gen, replaced, published := globalLaunchRegistry.RegisterOrReplace(spec, nil)
	if replaced {
		// A binding already existed — refuse (no invalidation wiring here).
		return gen, false
	}
	return gen, published
}

// RegisterOrReplaceLaunch is the package entry point for the term-layer
// replacement boundary: it runs the serialized transition with a caller-supplied
// pre-publication invalidation callback. A nil callback is rejected for a
// replacement (fail closed) by the transition. Returns the published generation,
// whether a prior binding existed, and ok (false only when a replacement was
// refused for lack of an invalidation callback).
func RegisterOrReplaceLaunch(spec LaunchSpec, invalidate func(generation int64)) (generation int64, replaced bool, ok bool) {
	return globalLaunchRegistry.RegisterOrReplace(spec, invalidate)
}

// LookupLaunch returns the launch binding for a session, or nil if not
// a managed launch.
func LookupLaunch(sessionID string) *LaunchBinding {
	globalLaunchRegistry.mu.Lock()
	defer globalLaunchRegistry.mu.Unlock()
	b, ok := globalLaunchRegistry.bindings[sessionID]
	if !ok {
		return nil
	}
	return &b
}

// RemoveLaunch clears a launch binding (called on session delete). The monotonic
// counter is NOT reset, so a later same-ID launch still receives a strictly
// higher generation. It is serialized against a same-session transition via the
// session gate so a removal cannot interleave inside a replacement.
func RemoveLaunch(sessionID string) {
	gate := globalLaunchRegistry.sessionGate(sessionID)
	gate.Lock()
	defer gate.Unlock()
	globalLaunchRegistry.mu.Lock()
	defer globalLaunchRegistry.mu.Unlock()
	delete(globalLaunchRegistry.bindings, sessionID)
}

// LaunchCorrelation returns the correlation state for a managed session.
// Validates adapter, provider, version, and process identity. Empty/missing
// values fail closed — they are not wildcards.
//
// S1.1-B process-identity rule: when the binding claims a PID, discovery must
// report the SAME nonzero PID; and when the binding ALSO claims a start time,
// discovery must report the SAME nonzero start time. A matching PID with a
// different (or missing) start time fails closed — this rejects PID reuse by a
// replacement process. A binding that claims no PID does not gate on process
// identity (weaker correlation), but never treats a missing value as a wildcard
// match against a claimed one.
//
// S1.1-B adapter rule (remediation R2): the runtime's actual terminal adapter
// must EXACTLY equal the binding's Adapter, and both must be non-empty. A binding
// recorded for one adapter (e.g. controlled_pty) can never correlate against a
// different runtime adapter even when provider/version/PID/start match.
func LaunchCorrelation(binding *LaunchBinding, discoveredAdapter, discoveredProvider, discoveredVersion string, discoveredPID int, discoveredStart time.Time) contract.Correlation {
	if binding == nil {
		return contract.CorrelationUnavailable
	}
	// Require a non-empty exact adapter match (fail closed on empty/mismatch).
	if binding.Adapter == "" || discoveredAdapter == "" || binding.Adapter != discoveredAdapter {
		return contract.CorrelationUnavailable
	}
	if binding.Provider != discoveredProvider {
		return contract.CorrelationUnavailable
	}
	// Require version match when binding specifies a version.
	if binding.AcceptedVersion != "" {
		if discoveredVersion == "" || binding.AcceptedVersion != discoveredVersion {
			return contract.CorrelationUnavailable
		}
	}
	// Require PID (and, when claimed, start-time) match when binding specifies a PID.
	if binding.PID != 0 {
		if discoveredPID == 0 || binding.PID != discoveredPID {
			return contract.CorrelationUnavailable
		}
		if !binding.StartedAt.IsZero() {
			if discoveredStart.IsZero() || !binding.StartedAt.Equal(discoveredStart) {
				return contract.CorrelationUnavailable // PID reuse with a different start
			}
		}
	}
	return contract.CorrelationManagedLaunch
}
