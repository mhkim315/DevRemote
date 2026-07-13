package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/agent/contract"
)

// A1-C — accepted-adapter ingestion + action-boundary tests over the
// generation-bound AuthoritativeApprovalStore. The legacy parser-fed store and
// terminal-derived options are gone; these tests exercise the production path.

// ── ingestion helpers ──

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

// ── ingestion: capability gate + exact binding ──

func TestIngest_CodexPositivePreservesExactIDAndBinding(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	svc := newIngestService(store)
	events := []agent.AgentEvent{approvalEvent("codex:s1", "AP-123", contract.ProvenanceNativeLog, 0.9)}
	svc.ingestApprovals("codex:s1", 7, 3, "codex", "0.144.1", events)

	snap, ok := store.LookupRecord("codex:s1", "AP-123")
	if !ok {
		t.Fatal("codex approval not ingested")
	}
	if snap.ApprovalID != "AP-123" || snap.SessionID != "codex:s1" {
		t.Errorf("id/session binding wrong: %+v", snap)
	}
	if snap.LaunchGen != 7 || snap.StreamGen != 3 || snap.Provider != "codex" || snap.Version != "0.144.1" {
		t.Errorf("generation/provider binding wrong: %+v", snap)
	}
	if snap.Provenance != contract.ProvenanceNativeLog {
		t.Errorf("provenance not bound from source event: %q", snap.Provenance)
	}
	// Frozen Codex mapping: approve + reject, no synthesized payload.
	if len(snap.Options) != 2 {
		t.Fatalf("want 2 frozen options, got %d", len(snap.Options))
	}
	for _, o := range snap.Options {
		if o.Payload != "" {
			t.Errorf("frozen option %q carries a synthesized payload %q (blind y/n forbidden)", o.ID, o.Payload)
		}
	}
}

func TestIngest_ClaudeProducesZeroApprovals(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	svc := newIngestService(store)
	// Even a well-formed approval-shaped event: Claude declares no CapApprovalDetection.
	events := []agent.AgentEvent{approvalEvent("claude:s1", "AP-1", contract.ProvenanceNativeLog, 0.9)}
	svc.ingestApprovals("claude:s1", 1, 0, "claude", "2.1.202", events)
	if l := store.List("claude:s1"); len(l) != 0 {
		t.Errorf("Claude (no approval capability) produced %d approvals, want 0", len(l))
	}
}

func TestIngest_NonAuthoritativeSourceEventDropped(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	svc := newIngestService(store)
	// A heuristic/prompt-hint provenance event cannot establish an approval even if
	// (hypothetically) DetectApproval returned it. Codex's SafeApprovalGate already
	// filters these, so the store also sees no authoritative source → nothing.
	events := []agent.AgentEvent{approvalEvent("codex:s1", "AP-1", contract.ProvenanceHeuristic, 0.99)}
	svc.ingestApprovals("codex:s1", 1, 0, "codex", "0.144.1", events)
	if l := store.List("codex:s1"); len(l) != 0 {
		t.Errorf("non-authoritative provenance produced %d approvals, want 0", len(l))
	}
}

func TestIngest_LowConfidenceAndIdlessDropped(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	svc := newIngestService(store)
	// Below the approval confidence floor.
	svc.ingestApprovals("codex:s1", 1, 0, "codex", "0.144.1",
		[]agent.AgentEvent{approvalEvent("codex:s1", "AP-low", contract.ProvenanceNativeLog, 0.4)})
	// Id-less approval event → EventUnknown inside the adapter, no approval.
	svc.ingestApprovals("codex:s1", 1, 0, "codex", "0.144.1",
		[]agent.AgentEvent{approvalEvent("codex:s1", "", contract.ProvenanceNativeLog, 0.9)})
	if l := store.List("codex:s1"); len(l) != 0 {
		t.Errorf("low-confidence/id-less produced %d approvals, want 0", len(l))
	}
}

func TestIngest_UnknownProviderNoFrozenMapping(t *testing.T) {
	// A provider without an accepted adapter yields no adapter → no approvals.
	store := NewAuthoritativeApprovalStore()
	svc := newIngestService(store)
	svc.ingestApprovals("gemini:s1", 1, 0, "gemini", "x",
		[]agent.AgentEvent{approvalEvent("gemini:s1", "AP-1", contract.ProvenanceNativeLog, 0.9)})
	if l := store.List("gemini:s1"); len(l) != 0 {
		t.Errorf("unknown provider produced %d approvals, want 0", len(l))
	}
}

func TestFrozenApprovalOptions_OnlyKnownProviders(t *testing.T) {
	if len(frozenApprovalOptions("codex")) == 0 {
		t.Error("codex must have a frozen option mapping")
	}
	for _, p := range []string{"claude", "gemini", "antigravity", ""} {
		if opts := frozenApprovalOptions(p); opts != nil {
			t.Errorf("provider %q must have no frozen mapping, got %v", p, opts)
		}
	}
}

// safeDetectApproval isolation: a panicking/erroring adapter yields no approvals.
type panickyAdapter struct{ contract.AgentAdapter }

func (panickyAdapter) Descriptor() contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{Capabilities: []contract.AdapterCapability{contract.CapApprovalDetection}}
}
func (panickyAdapter) DetectApproval(context.Context, []contract.AgentEvent) ([]contract.AgentApproval, error) {
	panic("boom")
}

type erroringAdapter struct{ contract.AgentAdapter }

func (erroringAdapter) Descriptor() contract.AgentAdapterDescriptor {
	return contract.AgentAdapterDescriptor{Capabilities: []contract.AdapterCapability{contract.CapApprovalDetection}}
}
func (erroringAdapter) DetectApproval(context.Context, []contract.AgentEvent) ([]contract.AgentApproval, error) {
	return nil, context.DeadlineExceeded
}

func TestSafeDetectApproval_IsolatesPanicAndError(t *testing.T) {
	ev := []agent.AgentEvent{approvalEvent("codex:s1", "AP-1", contract.ProvenanceNativeLog, 0.9)}
	if got := safeDetectApproval(panickyAdapter{}, ev); got != nil {
		t.Errorf("panicking adapter must yield no approvals, got %v", got)
	}
	if got := safeDetectApproval(erroringAdapter{}, ev); got != nil {
		t.Errorf("erroring adapter must yield no approvals, got %v", got)
	}
	if got := safeDetectApproval(nil, ev); got != nil {
		t.Errorf("nil adapter must yield no approvals, got %v", got)
	}
}

// ── action boundary ──

func seedApproval(s *AuthoritativeApprovalStore, sessionID, id string, opts []agent.InteractionOption) {
	s.Ingest(ApprovalIngest{
		SessionID: sessionID, LaunchGen: 1, Provider: "codex", Version: "0.144.1",
		Items: []ApprovalIngestItem{{
			Approval:   agent.AgentApproval{ID: id, SessionID: sessionID, Kind: "approval", Options: opts},
			Provenance: contract.ProvenanceNativeLog, RequiredPerm: "terminal:input",
		}},
	})
}

func doAction(h *Handlers, sessionID, approvalID, body string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/sessions/{id}/approvals/{approvalId}", h.HandleApprovalAction)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/approvals/"+approvalID, strings.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	return rr
}

func codexOpts() []agent.InteractionOption {
	return []agent.InteractionOption{
		{ID: "approve", Label: "Approve", Kind: "approve"},
		{ID: "reject", Label: "Reject", Kind: "reject"},
	}
}

func TestAction_RejectSucceeds_NoCommandEmitted(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	cmds := NewCommandBroker()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := &Handlers{Approvals: store, Cmds: cmds, InsecureLocalOnly: true}

	rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("reject code=%d want 200 (%s)", rr.Code, rr.Body.String())
	}
	if cmd := cmds.Take("codex:s1"); cmd != nil {
		t.Errorf("no-command-on-rejection violated: got %q", cmd)
	}
	snap, _ := store.LookupRecord("codex:s1", "a1")
	if snap.State != ApprovalRejected {
		t.Errorf("state=%q want rejected", snap.State)
	}
}

func TestAction_ApproveFailsClosedNoConfirmableChannel(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	cmds := NewCommandBroker()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := &Handlers{Approvals: store, Cmds: cmds, InsecureLocalOnly: true}

	rr := doAction(h, "codex:s1", "a1", `{"action":"approve"}`)
	if rr.Code != http.StatusBadGateway { // 502 delivery_failed
		t.Fatalf("approve code=%d want 502 delivery_failed (%s)", rr.Code, rr.Body.String())
	}
	if cmd := cmds.Take("codex:s1"); cmd != nil {
		t.Errorf("approve must not synthesize a terminal command, got %q", cmd)
	}
	snap, _ := store.LookupRecord("codex:s1", "a1")
	if snap.State != ApprovalDeliveryFailed {
		t.Errorf("state=%q want delivery_failed", snap.State)
	}
}

func TestAction_UnknownActionAndNotFound(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker(), InsecureLocalOnly: true}

	if rr := doAction(h, "codex:s1", "a1", `{"action":"nope"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown action code=%d want 400", rr.Code)
	}
	// Unknown action must not consume the reservation.
	if snap, _ := store.LookupRecord("codex:s1", "a1"); snap.State != ApprovalPending {
		t.Errorf("unknown action consumed reservation: state=%q", snap.State)
	}
	if rr := doAction(h, "codex:s1", "missing", `{"action":"reject"}`); rr.Code != http.StatusNotFound {
		t.Errorf("missing approval code=%d want 404", rr.Code)
	}
}

func TestAction_DuplicateResolveConflict(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker(), InsecureLocalOnly: true}

	if rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusOK {
		t.Fatalf("first reject code=%d", rr.Code)
	}
	// Second submit on a terminal request → 409.
	if rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusConflict {
		t.Errorf("duplicate code=%d want 409", rr.Code)
	}
}

func TestAction_ExpiredGone(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	base := time.Unix(1000, 0)
	store.now = func() time.Time { return base }
	seedApproval(store, "codex:s1", "a1", codexOpts())
	store.now = func() time.Time { return base.Add(authApprovalExpiry + time.Second) }
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker(), InsecureLocalOnly: true}
	if rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusGone {
		t.Errorf("expired code=%d want 410", rr.Code)
	}
}

func TestAction_InvalidatedRequestConflict(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	store.InvalidateSession("codex:s1", "correlation lost")
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker(), InsecureLocalOnly: true}
	if rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}`); rr.Code != http.StatusConflict {
		t.Errorf("invalidated code=%d want 409", rr.Code)
	}
}

func TestAction_InputContractValidation(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	// A neutral fire-and-forget option with required input.
	opts := []agent.InteractionOption{
		{ID: "send", Label: "Send", Kind: "neutral", Input: &agent.InputSchema{Required: true, Placement: "as_payload"}},
		{ID: "reject", Label: "Reject", Kind: "reject"},
	}
	seedApproval(store, "codex:s1", "a1", opts)
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker(), InsecureLocalOnly: true}

	// Required input missing → 400, reservation not consumed.
	if rr := doAction(h, "codex:s1", "a1", `{"action":"send"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("missing required input code=%d want 400", rr.Code)
	}
	// Input for an option without a schema → 400.
	if rr := doAction(h, "codex:s1", "a1", `{"action":"reject","input":"x"}`); rr.Code != http.StatusBadRequest {
		t.Errorf("input for schemaless option code=%d want 400", rr.Code)
	}
}

func TestAction_FireAndForgetInputDelivered(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	cmds := NewCommandBroker()
	opts := []agent.InteractionOption{
		{ID: "send", Label: "Send", Kind: "neutral", Input: &agent.InputSchema{Required: true, Placement: "as_payload"}},
	}
	seedApproval(store, "codex:s1", "a1", opts)
	h := &Handlers{Approvals: store, Cmds: cmds, InsecureLocalOnly: true}

	rr := doAction(h, "codex:s1", "a1", `{"action":"send","input":"ls -la"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("send code=%d want 200 (%s)", rr.Code, rr.Body.String())
	}
	if got := string(cmds.Take("codex:s1")); got != "ls -la\n" {
		t.Errorf("delivered payload=%q want %q", got, "ls -la\n")
	}
	snap, _ := store.LookupRecord("codex:s1", "a1")
	if snap.State != ApprovalResolved {
		t.Errorf("state=%q want resolved", snap.State)
	}
}

func TestAction_MethodAndBodyGuards(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker(), InsecureLocalOnly: true}
	// Empty action.
	if rr := doAction(h, "codex:s1", "a1", `{}`); rr.Code != http.StatusBadRequest {
		t.Errorf("empty action code=%d want 400", rr.Code)
	}
	// Malformed JSON.
	if rr := doAction(h, "codex:s1", "a1", `{not json`); rr.Code != http.StatusBadRequest {
		t.Errorf("bad json code=%d want 400", rr.Code)
	}
}

func TestAction_ResponseOutcomeShape(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	h := &Handlers{Approvals: store, Cmds: NewCommandBroker(), InsecureLocalOnly: true}
	rr := doAction(h, "codex:s1", "a1", `{"action":"reject"}`)
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	if body["status"] != "ok" || body["outcome"] != string(OutcomeOK) || body["action"] != "reject" {
		t.Errorf("unexpected response body: %v", body)
	}
}

// diagnostic still projects approvals via List (public projection).
func TestApprovals_ListPublicProjection(t *testing.T) {
	store := NewAuthoritativeApprovalStore()
	seedApproval(store, "codex:s1", "a1", codexOpts())
	l := store.List("codex:s1")
	if len(l) != 1 || l[0].Status != string(ApprovalPending) || l[0].ID != "a1" {
		t.Fatalf("unexpected list projection: %+v", l)
	}
}
