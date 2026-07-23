package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
)

const operationalRedactionPolicy = "operational-redaction-v1"

// RuntimeVerifier resolves the exact live managed-runtime identity committed
// in the provider-owned registry.
type RuntimeVerifier interface {
	RuntimeOf(sessionID string) (provider, runtimeID string, generation int64, ok bool)
}

type managedOperationalRuntimeVerifier struct {
	codex  *term.ManagedSessionRegistry
	claude *term.ManagedSessionRegistry
}

func newManagedOperationalRuntimeVerifier(
	codex, claude *term.ManagedSessionRegistry,
) *managedOperationalRuntimeVerifier {
	return &managedOperationalRuntimeVerifier{codex: codex, claude: claude}
}

func (v *managedOperationalRuntimeVerifier) RuntimeOf(sessionID string) (string, string, int64, bool) {
	if v == nil {
		return "", "", 0, false
	}
	var codexRecord, claudeRecord term.ManagedSessionRecord
	var inCodex, inClaude bool
	if v.codex != nil {
		codexRecord, inCodex = v.codex.Get(sessionID)
	}
	if v.claude != nil {
		claudeRecord, inClaude = v.claude.Get(sessionID)
	}
	// Unknown and ambiguous identities fail closed.
	if inCodex == inClaude {
		return "", "", 0, false
	}
	record := codexRecord
	expectedProvider, expectedPrefix := "codex", "codex_app_server:"
	if inClaude {
		record = claudeRecord
		expectedProvider, expectedPrefix = "claude", "claude_headless:"
	}
	if record.Exited || record.Provider != expectedProvider ||
		!strings.HasPrefix(record.SessionID, expectedPrefix) ||
		record.SessionID != sessionID || record.ProcessID == "" || record.Epoch <= 0 {
		return "", "", 0, false
	}
	return record.Provider, record.ProcessID, record.Epoch, true
}

type timelineOperationalAdapter struct {
	producers *writer.ProducerStore
	verifier  RuntimeVerifier
}

func newTimelineOperationalAdapter(producers *writer.ProducerStore, verifier RuntimeVerifier) *timelineOperationalAdapter {
	return &timelineOperationalAdapter{producers: producers, verifier: verifier}
}

// BindOperationalRuntime creates the one sender that may submit for an exact
// committed managed-runtime tuple. The shared adapter itself is never a
// submitter: managed runtimes receive only this returned opaque sender.
func (a *timelineOperationalAdapter) BindOperationalRuntime(identity term.OperationalRuntimeIdentity) term.OperationalEventSink {
	if a == nil || a.producers == nil || a.verifier == nil {
		return nil
	}
	provider, runtimeID, generation, verified := a.verifier.RuntimeOf(identity.SessionID)
	if !verified || provider != identity.Provider || runtimeID != identity.RuntimeID ||
		generation != identity.LaunchGeneration {
		return nil
	}
	capability, err := a.producers.Bind(
		identity.Provider, identity.RuntimeID, identity.SessionID, identity.LaunchGeneration,
		contract.EventProviderInvocationStarted,
		contract.EventProviderInvocationFinished,
		contract.EventToolCallStarted,
		contract.EventToolCallFinished,
		contract.EventApprovalRequested,
		contract.EventApprovalResolved,
		contract.EventStreamObserved,
	)
	if err != nil {
		return nil
	}
	return &timelineOperationalRuntimeSender{
		producers: a.producers, capability: capability, identity: identity,
	}
}

// SubmitAfterCommit intentionally rejects direct submissions to the shared
// binder. Only a per-runtime sender returned by BindOperationalRuntime can
// reach Timeline authorization.
func (a *timelineOperationalAdapter) SubmitAfterCommit(event term.OperationalEvent) {
	_ = event
}

// timelineOperationalRuntimeSender binds a capability to one managed runtime
// instance. Candidate identity is checked before envelope construction, and
// finish revokes exactly this sender's capability rather than caller-supplied
// fields from another runtime.
type timelineOperationalRuntimeSender struct {
	mu         sync.Mutex
	producers  *writer.ProducerStore
	capability writer.Capability
	identity   term.OperationalRuntimeIdentity
	revoked    bool
}

func (s *timelineOperationalRuntimeSender) SubmitAfterCommit(event term.OperationalEvent) {
	if s == nil || event.Provider != s.identity.Provider ||
		event.SessionID != s.identity.SessionID || event.RuntimeID != s.identity.RuntimeID ||
		event.LaunchGeneration != s.identity.LaunchGeneration {
		return
	}
	envelope, _, validEnvelope := operationalEnvelope(event)
	if event.Kind == term.OperationalProviderInvocationFinished {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.revoked {
			return
		}
		// Exit always revokes the installed capability even when the optional
		// shadow envelope is malformed. The bound identity, not event fields,
		// selects what is revoked.
		if validEnvelope {
			_ = s.capability.SubmitAfterCommit(envelope)
		}
		s.producers.Revoke(
			s.identity.Provider, s.identity.RuntimeID,
			s.identity.SessionID, s.identity.LaunchGeneration,
		)
		s.revoked = true
		return
	}
	if !validEnvelope {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revoked {
		return
	}
	_ = s.capability.SubmitAfterCommit(envelope)
}

func operationalEnvelope(event term.OperationalEvent) (contract.Envelope, contract.EventKind, bool) {
	kind, eventType, summary, referenceKind, ok := operationalKind(event.Kind)
	if !ok || event.Provider == "" || event.SessionID == "" || event.RuntimeID == "" ||
		event.LaunchGeneration <= 0 || event.SourceID == "" || event.SourcePosition == "" ||
		event.ReferenceID == "" || event.OccurredAt.IsZero() {
		return contract.Envelope{}, "", false
	}
	scope := contract.Scope{
		SessionID: event.SessionID, RuntimeID: event.RuntimeID,
		LaunchGeneration: event.LaunchGeneration,
	}
	sourceID := operationalOpaqueID("source", event.SourceID)
	position := operationalOpaqueID("position", event.SourcePosition)
	referenceID := operationalOpaqueID("reference", event.ReferenceID)
	evidenceID := operationalOpaqueID("evidence", event.SourceID)
	ref := &contract.TypedReference{Kind: referenceKind, ID: referenceID, Scope: scope}
	references := contract.References{}
	switch referenceKind {
	case contract.ReferenceProviderInvocation:
		references.ProviderInvocation = ref
	case contract.ReferenceToolCall:
		references.ToolCall = ref
	case contract.ReferenceApprovalRequest:
		references.ApprovalRequest = ref
	case contract.ReferenceTransport:
		references.Transport = ref
	default:
		return contract.Envelope{}, "", false
	}
	observedAt := time.Now().UTC()
	envelope, err := contract.NewEnvelope(contract.Envelope{
		SchemaVersion: contract.SchemaV1, PayloadVersion: contract.PayloadV1,
		EventKind: kind, SessionID: event.SessionID, RuntimeID: event.RuntimeID,
		LaunchGeneration: event.LaunchGeneration, Provider: event.Provider,
		SourceIncarnation: operationalOpaqueID("incarnation", event.RuntimeID),
		SourceIdentity:    contract.SourceIdentity{Kind: "provider", ID: sourceID},
		SourcePosition:    position,
		OccurredAt:        event.OccurredAt.UTC(), ObservedAt: observedAt,
		RedactionPolicyVersion: operationalRedactionPolicy,
		Payload: contract.Payload{
			Redacted: &contract.RedactedPayload{Summary: summary},
		},
		EvidenceSources: contract.EvidenceSources{
			Provider: &contract.ProviderEvidenceRef{ID: evidenceID, Scope: scope},
		},
		References: references,
		T0Event: agent.AgentEvent{
			ID:        operationalOpaqueID("t0", event.Provider, event.SourceID, event.SourcePosition),
			SessionID: event.SessionID, AgentKind: event.Provider, Type: eventType,
			Timestamp: event.OccurredAt.UTC(), Source: agent.SourceJSONL,
		},
	})
	if err != nil {
		return contract.Envelope{}, "", false
	}
	return envelope, kind, true
}

func operationalKind(kind term.OperationalEventKind) (contract.EventKind, agent.AgentEventType, string, contract.ReferenceKind, bool) {
	switch kind {
	case term.OperationalProviderInvocationStarted:
		return contract.EventProviderInvocationStarted, agent.EventAgentStarted,
			"provider invocation started", contract.ReferenceProviderInvocation, true
	case term.OperationalProviderInvocationFinished:
		return contract.EventProviderInvocationFinished, agent.EventCompleted,
			"provider invocation finished", contract.ReferenceProviderInvocation, true
	case term.OperationalToolCallStarted:
		return contract.EventToolCallStarted, agent.EventToolCallStarted,
			"tool call started", contract.ReferenceToolCall, true
	case term.OperationalToolCallFinished:
		return contract.EventToolCallFinished, agent.EventToolCallFinished,
			"tool call finished", contract.ReferenceToolCall, true
	case term.OperationalApprovalRequested:
		return contract.EventApprovalRequested, agent.EventApprovalRequested,
			"approval requested", contract.ReferenceApprovalRequest, true
	case term.OperationalApprovalResolved:
		return contract.EventApprovalResolved, agent.EventApprovalResolved,
			"approval resolved", contract.ReferenceApprovalRequest, true
	case term.OperationalStreamObserved:
		return contract.EventStreamObserved, agent.EventThinking,
			"provider stream observed", contract.ReferenceTransport, true
	default:
		return "", "", "", "", false
	}
}

func operationalOpaqueID(domain string, values ...string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(domain))
	for _, value := range values {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(value))
	}
	return hex.EncodeToString(h.Sum(nil))
}
