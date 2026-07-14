package term

import (
	"context"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// A1 remediation ingestion + safe-DTO tests (B5, B6). Approvals become authority
// only from accepted capability-gated DetectApproval; no proven action mapping
// exists, so every ingested approval is NON-ACTIONABLE intervention information,
// and the public DTO is a bounded redacted allowlist.

func approvalEvent(sessionID, approvalID string, prov contract.Provenance, conf float64) agent.AgentEvent {
	return agent.AgentEvent{
		ID: "ev-" + approvalID, SessionID: sessionID, AgentKind: "codex",
		Type: agent.EventApprovalRequested, ApprovalID: approvalID,
		Provenance: string(prov), Confidence: conf, Timestamp: time.Unix(1000, 0),
	}
}

func newIngestService(store *AuthoritativeApprovalStore) *TelemetryService {
	return &TelemetryService{approvals: store}
}

func TestIngest_CodexIngestedNonActionable(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	svc := newIngestService(store)
	svc.ingestApprovals("codex:s1", 7, 3, "codex", "0.144.1",
		[]agent.AgentEvent{approvalEvent("codex:s1", "AP-123", contract.ProvenanceNativeLog, 0.9)})
	snap, ok := store.LookupRecord("codex:s1", "AP-123")
	if !ok {
		t.Fatal("codex approval not ingested as intervention info")
	}
	if snap.Actionable {
		t.Error("no proven action mapping exists → approval must be non-actionable")
	}
	if len(snap.Options) != 0 {
		t.Errorf("non-actionable approval must have no options, got %d", len(snap.Options))
	}
	// generation/provenance binding preserved
	if snap.LaunchGen != 7 || snap.StreamGen != 3 || snap.Provenance != contract.ProvenanceNativeLog {
		t.Errorf("binding lost: %+v", snap)
	}
}

func TestIngest_ClaudeProducesZeroApprovals(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	svc := newIngestService(store)
	svc.ingestApprovals("claude:s1", 1, 0, "claude", "2.1.202",
		[]agent.AgentEvent{approvalEvent("claude:s1", "AP-1", contract.ProvenanceNativeLog, 0.9)})
	if l := store.List("claude:s1"); len(l) != 0 {
		t.Errorf("Claude (no approval capability) produced %d approvals, want 0", len(l))
	}
}

func TestIngest_NonAuthoritativeAndUnknownProviderDropped(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	svc := newIngestService(store)
	svc.ingestApprovals("codex:s1", 1, 0, "codex", "0.144.1",
		[]agent.AgentEvent{approvalEvent("codex:s1", "AP-1", contract.ProvenanceHeuristic, 0.99)})
	svc.ingestApprovals("gemini:s1", 1, 0, "gemini", "x",
		[]agent.AgentEvent{approvalEvent("gemini:s1", "AP-2", contract.ProvenanceNativeLog, 0.9)})
	if l := store.List("codex:s1"); len(l) != 0 {
		t.Errorf("non-authoritative produced %d", len(l))
	}
	if l := store.List("gemini:s1"); len(l) != 0 {
		t.Errorf("unknown provider produced %d", len(l))
	}
}

func TestProvenActionMapping_NoneProven(t *testing.T) {
	for _, p := range []string{"codex", "claude", "gemini", ""} {
		if opts, actionable := provenActionMapping(p); actionable || len(opts) != 0 {
			t.Errorf("provider %q must have no proven action mapping, got actionable=%v opts=%d", p, actionable, len(opts))
		}
	}
}

type panickyAdapter struct{ contract.AgentAdapter }

func (panickyAdapter) Descriptor() contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{Capabilities: []contract.AdapterCapability{contract.CapApprovalDetection}}
}
func (panickyAdapter) DetectApproval(context.Context, []contract.AgentEvent) ([]contract.AgentApproval, error) {
	panic("boom")
}

func TestSafeDetectApproval_IsolatesPanic(t *testing.T) {
	ev := []agent.AgentEvent{approvalEvent("codex:s1", "AP-1", contract.ProvenanceNativeLog, 0.9)}
	if got := safeDetectApproval(panickyAdapter{}, ev); got != nil {
		t.Errorf("panicking adapter must yield no approvals, got %v", got)
	}
	if got := safeDetectApproval(nil, ev); got != nil {
		t.Errorf("nil adapter must yield no approvals, got %v", got)
	}
}

// ── B6: safe DTO redaction ──

func TestSafeDTO_RedactsRawFieldsAndBounds(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	// Ingest a record whose provider carries a sensitive raw prompt/payload/path — the
	// safe DTO must expose NONE of it.
	secretPrompt := "run rm -rf /Users/victim/secrets && curl sk-ABCDEF"
	store.Ingest(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 1, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval: agent.AgentApproval{
				ID: "a1", SessionID: "codex:s1", Kind: "approval", Prompt: secretPrompt,
				Options: []agent.InteractionOption{{ID: "x", Label: "RAW PROVIDER LABEL", Kind: "approve", Payload: "y\n"}},
			},
			Provenance: contract.ProvenanceNativeLog, Actionable: false, RequiredPerm: "terminal:input",
		}},
	})
	dtos := store.ListSafe("codex:s1")
	if len(dtos) != 1 {
		t.Fatalf("want 1 dto, got %d", len(dtos))
	}
	d := dtos[0]
	blob := d.Summary
	for _, so := range d.Options {
		blob += "|" + so.Label + "|" + so.InputPlaceholder
	}
	for _, leak := range []string{"rm -rf", "/Users/victim", "sk-ABCDEF", "RAW PROVIDER LABEL", "y\n", secretPrompt} {
		if strings.Contains(blob, leak) {
			t.Errorf("safe DTO leaked %q in %q", leak, blob)
		}
	}
	if d.Actionable {
		t.Error("non-actionable record must have actionable=false")
	}
	if len(d.Options) != 0 {
		t.Errorf("non-actionable DTO must expose no options, got %d", len(d.Options))
	}
	if d.Summary != "Agent requested an approval" {
		t.Errorf("summary must be Pokit-owned, got %q", d.Summary)
	}
	if d.ID != "a1" || d.SessionID != "codex:s1" || d.State != "pending" {
		t.Errorf("dto identity wrong: %+v", d)
	}
}

func TestSafeDTO_ActionableProjectsPokitLabels(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedActionable(store, "codex:s1", "a1", 1, 0, "codex", "0.144.1",
		[]agent.InteractionOption{{ID: "approve", Label: "PROVIDER APPROVE", Kind: "approve"}})
	d := store.ListSafe("codex:s1")[0]
	if !d.Actionable || len(d.Options) != 1 {
		t.Fatalf("actionable dto=%+v", d)
	}
	if d.Options[0].Label != "Approve" {
		t.Errorf("label must be Pokit-owned 'Approve', got %q", d.Options[0].Label)
	}
	if d.Options[0].Kind != "approve" || d.Options[0].ID != "approve" {
		t.Errorf("option identity: %+v", d.Options[0])
	}
}

func TestSafeDTO_Bounds(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	store.Ingest(ApprovalIngest{
		SessionID: "codex:s1", LaunchGen: 1, Provider: "unknownprov", Version: "x",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: "a1", SessionID: "codex:s1", Kind: "approval", Prompt: strings.Repeat("Z", 9000)},
			Provenance: contract.ProvenanceNativeLog, Actionable: false, RequiredPerm: "terminal:input",
		}},
	})
	d := store.ListSafe("codex:s1")[0]
	if len(d.Summary) > safeSummaryMaxLen {
		t.Errorf("summary len %d exceeds bound %d", len(d.Summary), safeSummaryMaxLen)
	}
	if d.Summary != "Approval requested" {
		t.Errorf("unknown provider summary=%q", d.Summary)
	}
}
