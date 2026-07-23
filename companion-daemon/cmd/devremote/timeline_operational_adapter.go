package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
)

const operationalRedactionPolicy = "operational-redaction-v1"

type timelineOperationalAdapter struct {
	mu           sync.Mutex
	producers    *writer.ProducerStore
	capabilities map[string]writer.Capability
}

func newTimelineOperationalAdapter(producers *writer.ProducerStore) *timelineOperationalAdapter {
	return &timelineOperationalAdapter{
		producers: producers, capabilities: make(map[string]writer.Capability),
	}
}

func operationalKey(event term.OperationalEvent) string {
	return fmt.Sprintf("%s:%s:%d", event.Provider, event.SessionID, event.LaunchGeneration)
}

func (a *timelineOperationalAdapter) SubmitAfterCommit(event term.OperationalEvent) {
	if a == nil || a.producers == nil {
		return
	}
	envelope, _, validEnvelope := operationalEnvelope(event)
	key := operationalKey(event)
	a.mu.Lock()
	defer a.mu.Unlock()

	// Exit always revokes the capability even if the optional shadow envelope
	// cannot be constructed. A malformed observation may be dropped; runtime
	// authority must never remain live after the post-exit callback.
	if event.Kind == term.OperationalProviderInvocationFinished {
		if capability, bound := a.capabilities[key]; bound && validEnvelope {
			_ = capability.SubmitAfterCommit(envelope)
		}
		a.producers.Revoke(event.Provider, event.SessionID, event.LaunchGeneration)
		delete(a.capabilities, key)
		return
	}
	if !validEnvelope {
		return
	}
	if event.Kind == term.OperationalProviderInvocationStarted {
		capability, err := a.producers.Bind(
			event.Provider, event.RuntimeID, event.SessionID, event.LaunchGeneration,
			contract.EventProviderInvocationStarted,
			contract.EventProviderInvocationFinished,
			contract.EventToolCallStarted,
			contract.EventToolCallFinished,
			contract.EventApprovalRequested,
			contract.EventApprovalResolved,
			contract.EventStreamObserved,
		)
		if err != nil {
			return
		}
		a.capabilities[key] = capability
	}

	capability, bound := a.capabilities[key]
	if bound {
		_ = capability.SubmitAfterCommit(envelope)
	}
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
