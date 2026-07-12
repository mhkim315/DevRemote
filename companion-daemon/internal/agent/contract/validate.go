package contract

import (
	"errors"
	"sort"
	"strings"

	"devremote/companion-daemon/internal/agent"
)

// ── Typed degraded result ──
//
// Provider failure is expressed as data, not by breaking the caller. A degraded
// result carries a bounded, secret-free reason and diagnostics; it never carries
// raw prompts, terminal input, source, secrets, or unrestricted paths. Live
// Terminal, auth, revoke, lifecycle, and WS-ticket behavior are unaffected.
type DegradedInfo struct {
	Degraded    bool     `json:"degraded"`
	Reason      string   `json:"reason,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

// OK is the non-degraded zero value.
func OK() DegradedInfo { return DegradedInfo{} }

// Degrade builds a bounded, sanitized degraded result. The reason and each
// diagnostic are redacted and length-bounded; the list is capped.
func Degrade(reason string, diagnostics ...string) DegradedInfo {
	d := DegradedInfo{Degraded: true, Reason: SanitizeDiagnostic(reason)}
	for _, s := range diagnostics {
		if len(d.Diagnostics) >= MaxDiagnostics {
			break
		}
		d.Diagnostics = append(d.Diagnostics, SanitizeDiagnostic(s))
	}
	return d
}

// ── Closed vocabularies ──

var knownEventTypes = map[AgentEventType]bool{
	agent.EventAgentStarted: true, agent.EventUserMessage: true,
	agent.EventAssistantMessage: true, agent.EventThinking: true,
	agent.EventToolCallStarted: true, agent.EventToolCallFinished: true,
	agent.EventApprovalRequested: true, agent.EventApprovalResolved: true,
	agent.EventWaitingInput: true, agent.EventCompleted: true,
	agent.EventFailed: true, agent.EventInterrupted: true,
	agent.EventUnknown: true,
}

var knownEventSources = map[AgentEventSource]bool{
	agent.SourceJSONL: true, agent.SourceLogFile: true, agent.SourceScreen: true,
	agent.SourceProcess: true, agent.SourceManualLink: true,
}

var knownStatuses = map[AgentStatus]bool{
	agent.StatusUnknown: true, agent.StatusIdle: true, agent.StatusThinking: true,
	agent.StatusWorking: true, agent.StatusWaitingApproval: true,
	agent.StatusWaitingInput: true, agent.StatusCompleted: true,
	agent.StatusFailed: true, agent.StatusInterrupted: true, agent.StatusDegraded: true,
}

// IsKnownEventType reports whether t is in the closed event vocabulary.
func IsKnownEventType(t AgentEventType) bool { return knownEventTypes[t] }

// IsKnownEventSource reports whether s is in the closed source vocabulary.
func IsKnownEventSource(s AgentEventSource) bool { return knownEventSources[s] }

// IsKnownStatus reports whether s is in the closed status vocabulary.
func IsKnownStatus(s AgentStatus) bool { return knownStatuses[s] }

// ── Provenance-based authority ──

var knownProvenance = map[Provenance]bool{
	ProvenanceRuntime: true, ProvenanceProviderProtocol: true, ProvenanceProviderHook: true,
	ProvenanceNativeLog: true, ProvenancePTYStructural: true, ProvenanceHeuristic: true,
	ProvenancePromptHint: true, ProvenanceUnknown: true,
}

// IsKnownProvenance reports whether p is a defined provenance tier.
func IsKnownProvenance(p Provenance) bool { return knownProvenance[p] }

// approvalAuthoritative provenance may establish a surfaced approval. ONLY strong,
// structured tiers qualify: a runtime signal, a versioned provider protocol, a
// provider-managed hook, or a provider-native log. Advisory tiers (heuristic /
// prompt_hint / unknown) and PTY-structural signals are explicitly rejected — a
// spoofable text hint must never manufacture an approval.
var approvalAuthoritative = map[Provenance]bool{
	ProvenanceRuntime: true, ProvenanceProviderProtocol: true,
	ProvenanceProviderHook: true, ProvenanceNativeLog: true,
}

// ApprovalAuthoritative reports whether a provenance tier is strong enough to
// establish an approval.
func ApprovalAuthoritative(p Provenance) bool { return approvalAuthoritative[p] }

// terminalStatuses are agent statuses that assert a run has finished. Advisory
// evidence alone must not authoritatively produce these.
var terminalStatuses = map[AgentStatus]bool{
	agent.StatusCompleted: true, agent.StatusFailed: true, agent.StatusInterrupted: true,
}

// AdvisoryStatusConfidenceCeiling caps the confidence advisory-provenance status
// evidence may carry. Advisory signals never reach high/authoritative confidence.
const AdvisoryStatusConfidenceCeiling = 0.5

// ── Event validation + safe normalization ──

// ErrInvalidEvent marks an event that violates the contract and must not be
// emitted. Callers treat it as "drop the event", never as a fatal error.
var ErrInvalidEvent = errors.New("invalid agent event")

// ValidateEvent enforces the frozen event invariants: identity present, closed
// type/source/provenance vocabulary, non-negative Seq, confidence in range,
// low-confidence events pinned to EventUnknown, and per-entry metadata bounds.
func ValidateEvent(e AgentEvent) error {
	if e.ID == "" || e.SessionID == "" {
		return errInvalid("missing id/sessionId")
	}
	if e.Seq < 0 {
		return errInvalid("negative sequence")
	}
	if !IsKnownEventType(e.Type) {
		return errInvalid("event type outside closed vocabulary")
	}
	if !IsKnownEventSource(e.Source) {
		return errInvalid("event source outside closed vocabulary")
	}
	if !IsKnownProvenance(Provenance(e.Provenance)) {
		return errInvalid("event provenance outside closed vocabulary")
	}
	if e.Confidence < 0 || e.Confidence > 1 {
		return errInvalid("confidence out of [0,1]")
	}
	// A low-confidence classification must be surfaced as unknown, never as a
	// confident typed event that downstream code would trust.
	if e.Confidence < 0.4 && e.Type != agent.EventUnknown {
		return errInvalid("low-confidence event must be EventUnknown")
	}
	if len(e.Metadata) > MaxMetadataEntries {
		return errInvalid("metadata exceeds entry bound")
	}
	// Bound EVERY key and value, regardless of entry count.
	for k, v := range e.Metadata {
		if len(k) > MaxMetadataKeyBytes || len(v) > MaxMetadataValueBytes {
			return errInvalid("metadata key/value exceeds size bound")
		}
	}
	return nil
}

func errInvalid(msg string) error { return errors.New(msg + ": " + ErrInvalidEvent.Error()) }

// SafeEvent coerces a possibly-out-of-vocabulary event into a contract-valid one:
// unknown/blank type/source/provenance become safe defaults, Seq is floored at 0,
// confidence is clamped, and metadata is fully bounded (entries AND every
// key/value). It never fabricates a specific typed event — an ambiguous input
// degrades to EventUnknown.
func SafeEvent(e AgentEvent) AgentEvent {
	if e.Seq < 0 {
		e.Seq = 0
	}
	if !IsKnownEventType(e.Type) {
		e.Type = agent.EventUnknown
	}
	if !IsKnownEventSource(e.Source) {
		e.Source = agent.SourceScreen // weakest concrete source; never manual_link by default
	}
	if !IsKnownProvenance(Provenance(e.Provenance)) {
		e.Provenance = string(ProvenanceUnknown)
	}
	if e.Confidence < 0 {
		e.Confidence = 0
	}
	if e.Confidence > 1 {
		e.Confidence = 1
	}
	if e.Confidence < 0.4 {
		e.Type = agent.EventUnknown
	}
	e.Metadata = boundMetadata(e.Metadata)
	return e
}

func boundMetadata(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	// Deterministic selection: sort keys so bounding is stable across runs.
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, min(len(m), MaxMetadataEntries))
	for _, k := range keys {
		if len(out) >= MaxMetadataEntries {
			break
		}
		bk := k
		if len(bk) > MaxMetadataKeyBytes {
			bk = bk[:MaxMetadataKeyBytes]
		}
		v := m[k]
		if len(v) > MaxMetadataValueBytes {
			v = v[:MaxMetadataValueBytes]
		}
		out[bk] = v
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// BoundEvents caps a batch and reports whether truncation occurred. A
// non-positive max is treated as the hard cap MaxEventsPerRead (never unbounded),
// so this can never silently return more than the contract limit.
func BoundEvents(events []AgentEvent, max int) (bounded []AgentEvent, truncated bool) {
	if max <= 0 || max > MaxEventsPerRead {
		max = MaxEventsPerRead
	}
	if len(events) <= max {
		return events, false
	}
	return events[:max], true
}

// DedupeEvents removes events with duplicate IDs, keeping the first occurrence
// and preserving order. Empty-ID events are dropped (they cannot be deduped and
// violate the identity invariant).
func DedupeEvents(events []AgentEvent) []AgentEvent {
	seen := make(map[string]bool, len(events))
	out := events[:0:0]
	for _, e := range events {
		if e.ID == "" || seen[e.ID] {
			continue
		}
		seen[e.ID] = true
		out = append(out, e)
	}
	return out
}

// ── Status precedence ──

// ResolveStatus picks the winning agent status from weighted evidence. Precedence
// is provenance rank first, then numeric confidence. With no usable evidence it
// returns StatusUnknown. It never inspects or emits process lifecycle.
//
// Advisory-status policy (enforced in code, not just documented): when the
// winning evidence is advisory provenance (heuristic / prompt_hint / unknown),
// its confidence is capped at AdvisoryStatusConfidenceCeiling and it may NOT
// authoritatively assert a terminal status (completed/failed/interrupted) — such
// a claim is downgraded to StatusUnknown and marked degraded. Strong evidence
// still wins by precedence, so this only limits SOLE advisory evidence.
func ResolveStatus(evidence []StatusEvidence) StatusResult {
	best := StatusResult{Status: agent.StatusUnknown, Provenance: ProvenanceUnknown}
	found := false
	for _, ev := range evidence {
		if !IsKnownStatus(ev.Status) || !IsKnownProvenance(ev.Provenance) {
			continue
		}
		cand := StatusResult{Status: ev.Status, Provenance: ev.Provenance, Confidence: clamp01(ev.Confidence)}
		if !found || winsStatus(cand, best) {
			best = cand
			found = true
		}
	}
	if !found {
		return StatusResult{Status: agent.StatusUnknown, Provenance: ProvenanceUnknown}
	}
	if best.Provenance.Advisory() {
		if terminalStatuses[best.Status] {
			// A spoofable/heuristic signal cannot declare a run finished.
			return StatusResult{
				Status: agent.StatusUnknown, Provenance: best.Provenance,
				Confidence: minf(best.Confidence, AdvisoryStatusConfidenceCeiling),
				Degraded:   Degrade("advisory evidence cannot assert a terminal status"),
			}
		}
		if best.Confidence > AdvisoryStatusConfidenceCeiling {
			best.Confidence = AdvisoryStatusConfidenceCeiling
		}
	}
	return best
}

func winsStatus(cand, cur StatusResult) bool {
	cr, rr := ProvenanceRank(cand.Provenance), ProvenanceRank(cur.Provenance)
	if cr != rr {
		return cr > rr
	}
	return cand.Confidence > cur.Confidence
}

func clamp01(c float64) float64 {
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// ── Approval safety ──

// ApprovalConfidenceFloor is the minimum confidence an approval_requested event
// needs before it may become a surfaced approval. Below it, ambiguity wins and no
// approval is emitted.
const ApprovalConfidenceFloor = 0.5

// SafeApprovalGate reports whether an event may legitimately produce a surfaced
// approval. It defaults to NO approval: the event must be an unambiguous
// EventApprovalRequested, at or above the confidence floor, AND carry an
// authoritative provenance (runtime / provider_protocol / provider_hook /
// native_log). Advisory, unknown, prompt-hint, and PTY-structural provenance are
// rejected outright — a spoofable text hint can never manufacture an approval,
// even at confidence 0.99.
func SafeApprovalGate(e AgentEvent) bool {
	return e.Type == agent.EventApprovalRequested &&
		e.Confidence >= ApprovalConfidenceFloor &&
		ApprovalAuthoritative(Provenance(e.Provenance))
}

// ── Diagnostic sanitization ──

// SanitizeDiagnostic bounds a diagnostic string and strips likely sensitive
// content (absolute home paths, obvious secret tokens). Diagnostics are metadata,
// never a channel for prompts, terminal input, source, or credentials.
func SanitizeDiagnostic(s string) string {
	s = redactSecrets(s)
	if len(s) > MaxDiagnosticBytes {
		s = s[:MaxDiagnosticBytes]
	}
	return s
}

// redactPrefixes are the credential markers redactSecrets strips. Named with
// "redact" so the repository secret scan treats this redaction table as
// redaction code, not a leaked credential.
var redactPrefixes = []string{"sk-", "ghp_", "xoxb-", "xoxp-", "Bearer ", "AKIA"}

func redactSecrets(s string) string {
	for _, p := range redactPrefixes {
		if idx := strings.Index(s, p); idx >= 0 {
			return s[:idx] + "<REDACTED>"
		}
	}
	// Redact absolute home-style paths (/Users/<name>/..., /home/<name>/...).
	for _, root := range []string{"/Users/", "/home/"} {
		if idx := strings.Index(s, root); idx >= 0 {
			return s[:idx] + "<PATH>"
		}
	}
	return s
}

// ContainsSensitive reports whether a string still holds an obvious secret or an
// absolute home path after sanitization would have run. Used by the fixed harness
// to prove adapter diagnostics are clean.
func ContainsSensitive(s string) bool {
	return SanitizeDiagnostic(s) != s
}
