// Package contract freezes the provider-neutral T0 Common AgentEvent Contract:
// the small, operating-system-neutral surface that version-specific agent
// adapters (T1 Codex, T2 Claude, and future providers) implement WITHOUT
// changing mobile, lifecycle, authentication, Terminal, approval, or Transcript
// contracts.
//
// Ownership boundary: this package (and its fixed conformance harness) is owned
// by Pokit, not by any version-specific adapter. Adapters IMPORT this package;
// they must not edit it to make themselves pass. A change here is a deliberate
// contract revision, reviewed as such.
//
// Relationship to the existing agent model (migration map): the canonical event,
// status, identity, approval, and evidence types are REUSED from the sibling
// `agent` package (internal/agent) — this contract does not fork a parallel
// model. It ADDS the stable six-operation adapter interface, an opaque bounded
// cursor, provenance/confidence/correlation tiers (from the R1 signal matrix),
// a version/capability descriptor for the later Adapter Doctor/Repair, and
// validation/safe-default behavior. The legacy production wire event
// (internal/models.AgentEvent, carried in SessionTelemetry.Events) and the
// term-layer parsers are intentionally UNCHANGED by T0; their convergence onto
// this contract is T3 work (see docs/T0_COMMON_AGENT_EVENT_IMPLEMENTATION_REPORT.md).
//
// Layer distinctions this contract must never blur:
//
//	process lifecycle: starting/running/stopping/exited/killed/failed  (daemon-authoritative)
//	agent status:      thinking/working/waiting/etc.                   (this contract)
//	terminal stream:   raw PTY bytes                                   (Recorder-owned, untouched)
//	AgentEvent:        provider-neutral structured evidence            (this contract)
//	Transcript:        later T3 readable projection                   (not T0)
//
// AgentEvent/status evidence is NEVER process-lifecycle authority.
package contract

import (
	"context"

	"devremote/companion-daemon/internal/agent"
)

// ContractVersion identifies this frozen contract revision. Adapters declare the
// contract version they were built against so the later Adapter Doctor/Repair can
// detect drift. Bump only on a deliberate, reviewed contract change.
const ContractVersion = "t0.1"

// ── Re-exported canonical model (single source of truth = internal/agent) ──
//
// These aliases let adapters and the harness refer to the frozen model through
// the contract package without importing two packages, while keeping ONE
// definition (no parallel model). JSON shapes are identical to internal/agent.
type (
	AgentIdentity     = agent.AgentIdentity
	AgentStatus       = agent.AgentStatus
	AgentEvent        = agent.AgentEvent
	AgentEventType    = agent.AgentEventType
	AgentEventSource  = agent.AgentEventSource
	AgentApproval     = agent.AgentApproval
	InteractionOption = agent.InteractionOption
	InputSchema       = agent.InputSchema
	AgentCapability   = agent.AgentCapability
	DetectionEvidence = agent.DetectionEvidence
	LogRef            = agent.LogRef
)

// ── Provenance, confidence, and correlation tiers (R1 signal matrix) ──
//
// Provenance is the STRENGTH of the signal behind a piece of evidence. It is a
// separate axis from AgentEventSource (which records the mechanical origin —
// jsonl/log_file/screen/process/manual_link). Precedence between conflicting
// evidence uses ProvenanceRank; a weaker signal can never override a stronger one.
type Provenance string

const (
	// ProvenanceRuntime: daemon-owned runtime / lifecycle signal. Strongest.
	ProvenanceRuntime Provenance = "runtime"
	// ProvenanceProviderProtocol: a versioned provider protocol (e.g. app-server JSON-RPC).
	ProvenanceProviderProtocol Provenance = "provider_protocol"
	// ProvenanceProviderHook: a provider-managed hook lifecycle event.
	ProvenanceProviderHook Provenance = "provider_hook"
	// ProvenanceNativeLog: a provider-native log/JSONL record.
	ProvenanceNativeLog Provenance = "native_log"
	// ProvenancePTYStructural: terminal-structure signal (ANSI/CSI). Never logical state.
	ProvenancePTYStructural Provenance = "pty_structural"
	// ProvenanceHeuristic: derived from terminal text behavior only. Advisory.
	ProvenanceHeuristic Provenance = "heuristic"
	// ProvenancePromptHint: model-emitted marker or OSC. Spoofable; lowest. Advisory only.
	ProvenancePromptHint Provenance = "prompt_hint"
	// ProvenanceUnknown: provenance could not be established. Treated as weakest.
	ProvenanceUnknown Provenance = "unknown"
)

// provenanceRank orders provenance strongest→weakest. Higher = stronger.
var provenanceRank = map[Provenance]int{
	ProvenanceRuntime:          70,
	ProvenanceProviderProtocol: 60,
	ProvenanceProviderHook:     50,
	ProvenanceNativeLog:        40,
	ProvenancePTYStructural:    30,
	ProvenanceHeuristic:        20,
	ProvenancePromptHint:       10,
	ProvenanceUnknown:          0,
}

// ProvenanceRank returns the precedence weight of a provenance (higher = stronger).
// An unrecognised provenance ranks as weakest, so unknown inputs never win.
func ProvenanceRank(p Provenance) int { return provenanceRank[p] }

// Stronger reports whether provenance a should win over b in a precedence conflict.
func (p Provenance) Stronger(other Provenance) bool {
	return ProvenanceRank(p) > ProvenanceRank(other)
}

// Advisory reports whether a provenance may only ENRICH and can never establish
// approval, input, or lifecycle authority (heuristic / prompt hint / unknown).
func (p Provenance) Advisory() bool {
	switch p {
	case ProvenanceHeuristic, ProvenancePromptHint, ProvenanceUnknown:
		return true
	}
	return false
}

// ConfidenceLevel is the coarse, closed confidence bucket used for precedence and
// display. Derived from a numeric [0,1] confidence via ConfidenceLevelFor.
type ConfidenceLevel string

const (
	ConfidenceAuthoritative ConfidenceLevel = "authoritative"
	ConfidenceHigh          ConfidenceLevel = "high"
	ConfidenceMedium        ConfidenceLevel = "medium"
	ConfidenceLow           ConfidenceLevel = "low"
)

// ConfidenceLevelFor maps a numeric confidence to a closed bucket. Values are
// clamped, so out-of-range inputs never produce an undefined level.
func ConfidenceLevelFor(c float64) ConfidenceLevel {
	switch {
	case c >= 0.99:
		return ConfidenceAuthoritative
	case c >= 0.75:
		return ConfidenceHigh
	case c >= 0.5:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

// Correlation states how confidently a discovered provider session is bound to a
// Pokit-managed session. Discovery MUST NOT invent terminal ownership: a session
// that is not provably correlated stays CorrelationUnavailable and cannot drive
// managed lifecycle.
type Correlation string

const (
	// CorrelationProven: the provider session is proven to map to this Pokit session.
	CorrelationProven Correlation = "proven"
	// CorrelationManagedLaunch: correlation holds only because Pokit launched it.
	CorrelationManagedLaunch Correlation = "managed_launch"
	// CorrelationUnavailable: no correlation proven. Observe-only; never authoritative.
	CorrelationUnavailable Correlation = "unavailable"
)

// ── Six-operation adapter contract ──

// SessionContext is the read-only, provider-neutral view of a Pokit session that
// every operation receives. It carries no raw terminal input and no secrets.
type SessionContext struct {
	SessionID            string   // canonical Pokit session id (<adapter>:<local>)
	TerminalAdapter      string   // terminal backend name (controlled_pty/...)
	TerminalCapabilities []string // declared terminal capabilities
	CWD                  string   // working directory (may be redacted upstream)
	PID                  int      // process id, 0 if unknown
	CommandLine          []string // argv, if known
	ProcessName          string   // process image name, if known
	ScreenText           string   // recent screen text (may be empty); never stored raw
}

// RawRecord is one opaque provider-native record handed to NormalizeEvent. The
// bytes stay inside the adapter; only a normalized AgentEvent leaves. Provenance
// records the signal strength of the record's origin.
type RawRecord struct {
	Bytes      []byte
	Source     AgentEventSource
	Provenance Provenance
}

// DiscoveryInput asks an adapter to enumerate provider sessions it can see for a
// given Pokit session context. Discovery is bounded by MaxDiscoveredSessions.
type DiscoveryInput struct {
	Session SessionContext
	Limit   int // caller cap; the adapter must also honor MaxDiscoveredSessions
}

// DiscoveredSession is a provider session the adapter believes relates to the
// Pokit session. It never asserts terminal ownership and never cross-links to a
// different Pokit session; Correlation states how strong the binding is.
type DiscoveredSession struct {
	ProviderSessionID string      // provider-native id (opaque to Pokit)
	Provider          string      // provider kind (claude/codex/...)
	ProviderVersion   string      // provider version, if known
	Correlation       Correlation // proven / managed_launch / unavailable
	Confidence        float64     // [0,1]
	LogRefs           []LogRef    // discovered logs (display paths redacted)
	Degraded          DegradedInfo
}

// ReadInput requests a bounded, incremental batch of events after Cursor.
type ReadInput struct {
	Session   SessionContext
	Records   []RawRecord // provider records to normalize this batch
	Cursor    Cursor      // opaque position from the previous read ("" = beginning)
	MaxEvents int         // caller cap; the adapter must also honor MaxEventsPerRead
}

// ReadResult is the bounded output of ReadEvents. NextCursor is opaque and
// monotonic; re-reading with the same cursor and records yields zero new events.
type ReadResult struct {
	Events     []AgentEvent
	NextCursor Cursor
	Degraded   DegradedInfo
}

// StatusInput carries the evidence GetStatus weighs. It never includes process
// lifecycle — agent status must not be derived from (or become) lifecycle.
type StatusInput struct {
	Session      SessionContext
	RecentEvents []AgentEvent
	Evidence     []StatusEvidence
}

// StatusEvidence is one weighted status signal with its provenance and confidence.
type StatusEvidence struct {
	Status     AgentStatus
	Provenance Provenance
	Confidence float64
}

// StatusResult is the resolved agent status plus the provenance/confidence that
// won precedence. It is advisory activity state, never lifecycle authority.
type StatusResult struct {
	Status     AgentStatus
	Provenance Provenance
	Confidence float64
	Degraded   DegradedInfo
}

// AgentAdapterDescriptor declares an adapter's stable identity, the provider
// versions it was built for, and its optional capabilities. The later Adapter
// Doctor/Repair uses ContractVersion + SupportedVersions to detect drift.
type AgentAdapterDescriptor struct {
	Name              string   // stable adapter key (== AgentIdentity.Kind it emits)
	Provider          string   // provider/product name
	ContractVersion   string   // contract revision built against (== ContractVersion)
	SupportedVersions []string // provider versions with retained fixtures
	Capabilities      []AdapterCapability
}

// AdapterCapability is an optional adapter feature the product may gate on.
type AdapterCapability string

const (
	CapEvents            AdapterCapability = "events"
	CapStatus            AdapterCapability = "status"
	CapToolCallDetection AdapterCapability = "tool_call_detection"
	CapApprovalDetection AdapterCapability = "approval_detection"
	CapIncrementalRead   AdapterCapability = "incremental_read"
	CapScreenFallback    AdapterCapability = "screen_fallback"
	CapProcessDetection  AdapterCapability = "process_detection"
	CapLogDetection      AdapterCapability = "log_detection"
)

// AgentAdapter is the frozen six-operation contract (plus a descriptor). Every
// operation must be safe on unknown/malformed input: it returns a typed degraded
// result and never panics, never fabricates an event, and never blocks Live
// Terminal. A failing adapter degrades its own metadata only.
type AgentAdapter interface {
	// Descriptor declares stable identity, provider versions, and capabilities.
	Descriptor() AgentAdapterDescriptor

	// Detect identifies the agent from a session context. Confidence < 0.5 MUST
	// yield AgentIdentity.Kind == "unknown". Never panics on partial evidence.
	Detect(ctx context.Context, session SessionContext) (AgentIdentity, error)

	// DiscoverSessions enumerates provider sessions related to the context. It
	// never invents terminal ownership and never cross-links Pokit sessions; an
	// unproven binding stays CorrelationUnavailable. Bounded by MaxDiscoveredSessions.
	DiscoverSessions(ctx context.Context, in DiscoveryInput) ([]DiscoveredSession, error)

	// ReadEvents returns a bounded, incremental, ordered batch after the cursor.
	// The cursor is opaque; duplicate suppression and read bounds are mandatory.
	ReadEvents(ctx context.Context, in ReadInput) (ReadResult, error)

	// NormalizeEvent maps ONE provider-native record to a provider-neutral event.
	// Provider-native fields must not leak into the public event (only Metadata,
	// which must not drive UX). Unknown/malformed input yields EventUnknown or a
	// degraded result, never a panic or a fabricated typed event.
	NormalizeEvent(ctx context.Context, rec RawRecord) (AgentEvent, DegradedInfo)

	// DetectApproval derives approval evidence from already-normalized events.
	// It defaults to NO approval on ambiguity or low confidence; a positive result
	// requires an unambiguous approval_requested event.
	DetectApproval(ctx context.Context, events []AgentEvent) ([]AgentApproval, error)

	// GetStatus resolves advisory agent status from weighted evidence using
	// provenance precedence. It never derives or asserts process lifecycle.
	GetStatus(ctx context.Context, in StatusInput) (StatusResult, error)
}
