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
	"devremote/companion-daemon/internal/models"
	"devremote/companion-daemon/internal/mux"
)

// --- ApprovalStore Tests ---

func TestApprovalStore_Resolve_Pending(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{ID: "a1", SessionID: "s1", Status: "pending", CreatedAt: time.Now()},
	})
	if !s.Resolve("s1", "a1", "approved") {
		t.Error("Resolve returned false for pending approval")
	}
	list := s.List("s1")
	if len(list) != 1 || list[0].Status != "approved" || list[0].ResolvedAt == nil {
		t.Error("approval not marked as approved")
	}
}

func TestApprovalStore_Resolve_Rejected(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{ID: "a1", SessionID: "s1", Status: "pending", CreatedAt: time.Now()},
	})
	s.Resolve("s1", "a1", "rejected")
	list := s.List("s1")
	if list[0].Status != "rejected" {
		t.Errorf("status=%s, want rejected", list[0].Status)
	}
}

func TestApprovalStore_Resolve_Duplicate(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{ID: "a1", SessionID: "s1", Status: "pending", CreatedAt: time.Now()},
	})
	if !s.Resolve("s1", "a1", "approved") {
		t.Error("first resolve should succeed")
	}
	if s.Resolve("s1", "a1", "approved") {
		t.Error("duplicate resolve should return false")
	}
}

func TestApprovalStore_Resolve_Expired(t *testing.T) {
	s := NewApprovalStore()
	expired := time.Now().Add(-10 * time.Minute)
	s.Upsert("s1", []agent.AgentApproval{
		{ID: "a1", SessionID: "s1", Status: "pending", CreatedAt: expired},
	})
	if s.Resolve("s1", "a1", "approved") {
		t.Error("expired approval resolve should return false")
	}
}

func TestApprovalStore_Upsert_Merge(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{ID: "a1", SessionID: "s1", Status: "pending", CreatedAt: time.Now()},
	})
	s.Resolve("s1", "a1", "approved")
	s.Upsert("s1", []agent.AgentApproval{
		{ID: "a1", SessionID: "s1", Status: "pending", CreatedAt: time.Now()},
	})
	list := s.List("s1")
	if list[0].Status != "approved" {
		t.Errorf("upsert overwrote resolved status: %s", list[0].Status)
	}
}

func TestApprovalStore_List_Expiry(t *testing.T) {
	s := NewApprovalStore()
	fresh := time.Now()
	expired := time.Now().Add(-10 * time.Minute)
	s.Upsert("s1", []agent.AgentApproval{
		{ID: "a1", SessionID: "s1", Status: "pending", CreatedAt: fresh},
		{ID: "a2", SessionID: "s1", Status: "pending", CreatedAt: expired},
	})
	list := s.List("s1")
	if len(list) != 1 {
		t.Errorf("List returned %d approvals, want 1 (expired pruned)", len(list))
	}
}

func TestApprovalStore_Resolve_NotFound(t *testing.T) {
	s := NewApprovalStore()
	if s.Resolve("nonexistent", "a1", "approved") {
		t.Error("resolve nonexistent should return false")
	}
}

// --- Approval Handler Tests ---

func TestHandleApprovalAction_Success(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			AgentKind: "claude", Prompt: "Approve?",
			Options:   []agent.InteractionOption{{ID: "approve", Label: "Approve", Kind: "approve"}, {ID: "reject", Label: "Reject", Kind: "reject"}},
			CreatedAt: time.Now(),
		},
	})
	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}
	body := `{"action":"approve"}`
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(body))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", rec.Code)
	}
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["status"] != "ok" {
		t.Errorf("response status=%q", resp["status"])
	}
}

func TestHandleApprovalAction_Duplicate(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			Options:   []agent.InteractionOption{{ID: "approve", Label: "Approve", Kind: "approve"}},
			CreatedAt: time.Now(),
		},
	})
	s.Resolve("s1", "a1", "approved")

	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"approve"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("duplicate: status=%d, want 409", rec.Code)
	}
}

func TestHandleApprovalAction_Expired(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			Options:   []agent.InteractionOption{{ID: "approve", Label: "Approve", Kind: "approve"}},
			CreatedAt: time.Now().Add(-10 * time.Minute),
		},
	})
	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"approve"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)

	if rec.Code != http.StatusGone {
		t.Errorf("expired: status=%d, want 410", rec.Code)
	}
}

func TestHandleApprovalAction_InvalidAction(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			Options:   []agent.InteractionOption{{ID: "open_terminal", Label: "Open Terminal", Kind: "open"}},
			CreatedAt: time.Now(),
		},
	})
	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"approve"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid action: status=%d, want 400 (approve not in options)", rec.Code)
	}
}

func TestHandleApprovalAction_EmptyOptions(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			Options:   []agent.InteractionOption{},
			CreatedAt: time.Now(),
		},
	})
	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"approve"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty options: status=%d, want 400 (no options available)", rec.Code)
	}
}
func TestHandleApprovalAction_KindReject(t *testing.T) {
	// Option ID is "deny" but Kind is "reject" — status must come from Kind.
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			AgentKind: "cursor", Prompt: "Deny?",
			Options:   []agent.InteractionOption{{ID: "deny", Label: "Deny", Kind: "reject"}},
			CreatedAt: time.Now(),
		},
	})
	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"deny"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("kind reject: status=%d, want 200", rec.Code)
	}
	// Verify the status in store is "rejected" (from Kind), not "resolved".
	list := s.List("s1")
	if len(list) != 1 || list[0].Status != "rejected" {
		t.Errorf("kind reject: status=%q, want rejected", list[0].Status)
	}
}

func TestHandleApprovalAction_RequiredInput_Missing(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			AgentKind: "codex", Prompt: "Enter reason:",
			Options: []agent.InteractionOption{{
				ID: "reject_with_reason", Label: "Reject with reason", Kind: "reject",
				Input: &agent.InputSchema{Required: true, Placeholder: "Reason"},
			}},
			CreatedAt: time.Now(),
		},
	})
	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"reject_with_reason"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("required input missing: status=%d, want 400", rec.Code)
	}
}

func TestHandleApprovalAction_OptionalInput_Provided(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			AgentKind: "codex", Prompt: "Continue?",
			Options: []agent.InteractionOption{{
				ID: "continue_with_edits", Label: "Continue with edits", Kind: "neutral",
				Input: &agent.InputSchema{Required: false, Placeholder: "Edits"},
			}},
			CreatedAt: time.Now(),
		},
	})
	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}

	// Without input — should succeed since input is optional.
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"continue_with_edits"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("optional input without value: status=%d, want 200", rec.Code)
	}
}


func TestHandleApprovalAction_InputForOptionWithoutSchema(t *testing.T) {
	// Input sent for an option that does NOT declare input → 400.
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			AgentKind: "claude", Prompt: "Approve?",
			Options:   []agent.InteractionOption{{ID: "approve", Label: "Approve", Kind: "approve"}},
			CreatedAt: time.Now(),
		},
	})
	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"approve","input":"extra"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("input for no-schema option: status=%d, want 400", rec.Code)
	}
}

func TestHandleApprovalAction_WithInput(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			AgentKind: "claude", Prompt: "Enter reason:",
			Options: []agent.InteractionOption{{
				ID: "reject_with_reason", Label: "Reject with reason", Kind: "reject",
				Input: &agent.InputSchema{Required: true, Placeholder: "Reason"},
			}},
			CreatedAt: time.Now(),
		},
	})
	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"reject_with_reason","input":"Not needed"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("with input: status=%d, want 200", rec.Code)
	}
	// Status must be "rejected" from Kind.
	list := s.List("s1")
	if list[0].Status != "rejected" {
		t.Errorf("with input: status=%q, want rejected", list[0].Status)
	}
}

// --- Capability-aware option creation ---

func TestCapabilityFilter_InputWritable(t *testing.T) {
	// Session with InputWriter + StreamOpener gets full action options.
	sess := &inputTestSession{}
	opts := buildInteractionOptions(sess)
	if !optionInList(opts, "approve") || !optionInList(opts, "reject") {
		t.Error("input-capable session missing approve/reject")
	}
	if !optionInList(opts, "send_text") || !optionInList(opts, "send_key") {
		t.Error("input-capable session missing send_text/send_key")
	}
	if !optionInList(opts, "open_terminal") {
		t.Error("input-capable session missing open_terminal")
	}
}

func TestCapabilityFilter_StreamOnly(t *testing.T) {
	// Session with StreamOpener but no InputWriter gets open_terminal only.
	sess := &streamOnlySession{}
	opts := buildInteractionOptions(sess)
	if !optionInList(opts, "open_terminal") {
		t.Error("stream-only session missing open_terminal")
	}
	if optionInList(opts, "approve") || optionInList(opts, "reject") {
		t.Error("stream-only session should not have approve/reject")
	}
}

func TestCapabilityFilter_ObserveOnly(t *testing.T) {
	// Session with no capabilities gets empty options (no fake actions).
	sess := &observeOnlySession{}
	opts := buildInteractionOptions(sess)
	if len(opts) != 0 {
		t.Errorf("observe-only session should have empty options, got %d", len(opts))
	}
}


func TestInteractionOption_NChoices(t *testing.T) {
	// N heterogeneous options must preserve semantic kinds end-to-end.
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Kind: "interaction", Status: "pending",
			AgentKind: "cursor", Prompt: "Select mode:",
			Options: []agent.InteractionOption{
				{ID: "fast", Label: "Fast", Kind: "neutral"},
				{ID: "balanced", Label: "Balanced", Kind: "neutral"},
				{ID: "thorough", Label: "Thorough", Kind: "neutral"},
				{ID: "cancel", Label: "Cancel", Kind: "cancel"},
			},
			CreatedAt: time.Now(),
		},
	})
	h := &Handlers{Approvals: s, Cmds: NewCommandBroker()}

	// Valid: choose an option in the list.
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"balanced"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("valid N-option: status=%d, want 200", rec.Code)
	}

	// Invalid: choose an option NOT in the list (approve is not in 4 neutral options).
	req2 := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"approve"}`))
	req2.SetPathValue("id", "s1")
	req2.SetPathValue("approvalId", "a1")
	rec2 := httptest.NewRecorder()
	h.HandleApprovalAction(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("invalid N-option: status=%d, want 400", rec2.Code)
	}
}

// --- helpers ---

func optionInList(options []agent.InteractionOption, id string) bool {
	for _, o := range options {
		if o.ID == id {
			return true
		}
	}
	return false
}

// --- buildPayload tests ---

func TestBuildPayload_RejectWithReason_AfterPayload(t *testing.T) {
	opt := &agent.InteractionOption{
		ID: "reject_with_reason", Kind: "reject",
		Input: &agent.InputSchema{Required: true, Placement: "after_payload"},
	}
	payload := buildPayload(opt, "Not needed")
	expected := "n\nNot needed\n"
	if payload != expected {
		t.Errorf("reject_with_reason: got %q, want %q", payload, expected)
	}
}

func TestBuildPayload_ApproveWithComment_AfterPayload(t *testing.T) {
	opt := &agent.InteractionOption{
		ID: "approve_with_comment", Kind: "approve",
		Input: &agent.InputSchema{Required: false, Placement: "after_payload"},
	}
	payload := buildPayload(opt, "LGTM")
	expected := "y\nLGTM\n"
	if payload != expected {
		t.Errorf("approve_with_comment: got %q, want %q", payload, expected)
	}
}

func TestBuildPayload_AsPayload(t *testing.T) {
	opt := &agent.InteractionOption{
		ID: "custom", Kind: "neutral",
		Input: &agent.InputSchema{Required: true, Placement: "as_payload"},
	}
	payload := buildPayload(opt, "custom command")
	expected := "custom command\n"
	if payload != expected {
		t.Errorf("as_payload: got %q, want %q", payload, expected)
	}
}

func TestBuildPayload_PayloadWithInput_AfterPayload(t *testing.T) {
	opt := &agent.InteractionOption{
		ID: "reject_with_reason", Kind: "reject", Payload: "N",
		Input: &agent.InputSchema{Required: true, Placement: "after_payload"},
	}
	payload := buildPayload(opt, "Not needed")
	expected := "N\nNot needed\n"
	if payload != expected {
		t.Errorf("payload with input: got %q, want %q", payload, expected)
	}
}

func TestBuildPayload_NoInputSchema_DefaultBehavior(t *testing.T) {
	opt := &agent.InteractionOption{ID: "approve", Kind: "approve"}
	payload := buildPayload(opt, "should be ignored")
	// No InputSchema → input rejected at handler level (400).
	// If it reaches buildPayload, use default only.
	if payload != "y\n" {
		t.Errorf("no input schema: got %q, want %q", payload, "y\n")
	}
}


func TestBuildPayload_SendText_AsPayload(t *testing.T) {
	// send_text must declare Placement: "as_payload" so input reaches terminal.
	opts := buildInteractionOptions(&inputTestSession{})
	for _, opt := range opts {
		if opt.ID == "send_text" {
			if opt.Input == nil || opt.Input.Placement != "as_payload" {
				t.Error("send_text missing Placement: as_payload")
			}
		}
		if opt.ID == "send_key" {
			if opt.Input == nil || opt.Input.Placement != "as_payload" {
				t.Error("send_key missing Placement: as_payload")
			}
		}
	}
}

func TestBuildPayload_SendText_ReachesCommandBroker(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			AgentKind: "claude", Prompt: "Send text?",
			Options: []agent.InteractionOption{{
				ID: "send_text", Label: "Send Text", Kind: "neutral",
				Input: &agent.InputSchema{Required: true, Placement: "as_payload"},
			}},
			CreatedAt: time.Now(),
		},
	})
	cmds := NewCommandBroker()
	h := &Handlers{Approvals: s, Cmds: cmds}
	req := httptest.NewRequest("POST", "/api/sessions/s1/approvals/a1", strings.NewReader(`{"action":"send_text","input":"hello world"}`))
	req.SetPathValue("id", "s1")
	req.SetPathValue("approvalId", "a1")
	rec := httptest.NewRecorder()
	h.HandleApprovalAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("send_text: status=%d, want 200", rec.Code)
	}
	payload := cmds.Take("s1")
	if string(payload) != "hello world\n" {
		t.Errorf("send_text payload: got %q, want %q", string(payload), "hello world\n")
	}
}

func TestBuildPayload_MetadataOnly_NoTerminalInjection(t *testing.T) {
	opt := &agent.InteractionOption{
		ID: "comment", Kind: "neutral",
		Input: &agent.InputSchema{Required: false, Placement: "metadata_only"},
	}
	payload := buildPayload(opt, "audit note")
	// metadata_only → input preserved in audit, NOT sent to terminal.
	if payload != "" {
		t.Errorf("metadata_only: got %q, want empty", payload)
	}
}

func TestBuildPayload_EmptyPlacement_SafeDefault(t *testing.T) {
	// Empty/missing placement → safe default: input not sent to terminal.
	opt := &agent.InteractionOption{
		ID: "reject_with_reason", Kind: "reject",
		Input: &agent.InputSchema{Required: true},
	}
	payload := buildPayload(opt, "Not needed")
	// Input is preserved in audit log but NOT injected into terminal.
	// Only default "n\n" payload goes to terminal.
	if payload != "n\n" {
		t.Errorf("empty placement: got %q, want %q (input not sent to terminal)", payload, "n\n")
	}
}


// --- Diagnostic tests ---

func TestHandleDiagnostic_GetOnly(t *testing.T) {
	h := &Handlers{}
	req := httptest.NewRequest("POST", "/debug/diag", nil)
	rec := httptest.NewRecorder()
	h.HandleDiagnostic(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /debug/diag: status=%d, want 405", rec.Code)
	}
}

func TestHandleDiagnostic_JSONShape(t *testing.T) {
	reg := mux.MustNewRegistry(&diagTestAdapter{})
	events := NewMemoryEventStore()
	svc := NewTelemetryService(reg, events, NewNopLinkStore(), NoopNotifier{}, nil, NewApprovalStore())
	h := &Handlers{Registry: reg, Telemetry: svc, Approvals: NewApprovalStore()}

	req := httptest.NewRequest("GET", "/debug/diag", nil)
	rec := httptest.NewRecorder()
	h.HandleDiagnostic(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /debug/diag: status=%d, want 200", rec.Code)
	}

	var snap DiagnosticSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if snap.Daemon.GoVersion == "" {
		t.Error("daemon.goVersion is empty")
	}
	if snap.Daemon.AdapterCount < 1 {
		t.Error("daemon.adapterCount is 0")
	}
	if len(snap.Adapters) == 0 {
		t.Error("adapters is empty")
	}
}

func TestHandleDiagnostic_SessionFields(t *testing.T) {
	reg := mux.MustNewRegistry(&diagTestAdapter{})
	events := NewMemoryEventStore()
	approvals := NewApprovalStore()
	approvals.Upsert("tmux:diag-session", []agent.AgentApproval{
		{ID: "a1", SessionID: "tmux:diag-session", Status: "pending", CreatedAt: time.Now()},
	})
	detector := &alwaysApproveDetector{}
	svc := NewTelemetryService(reg, events, NewNopLinkStore(), NoopNotifier{}, detector, approvals)

	// Run telemetry sampling loop to populate agent detection fields.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go svc.Run(ctx)
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-svc.Done()

	h := &Handlers{Registry: reg, Telemetry: svc, Approvals: approvals}

	req := httptest.NewRequest("GET", "/debug/diag", nil)
	rec := httptest.NewRecorder()
	h.HandleDiagnostic(rec, req)

	var snap DiagnosticSnapshot
	json.Unmarshal(rec.Body.Bytes(), &snap)

	found := false
	for _, sd := range snap.Sessions {
		if sd.ID == "tmux:diag-session" {
			found = true
			if sd.AgentKind != "claude" {
				t.Errorf("AgentKind=%q, want claude", sd.AgentKind)
			}
			if sd.AgentConfidence < 0.5 {
				t.Errorf("AgentConfidence=%.2f, want >=0.5", sd.AgentConfidence)
			}
			if sd.PendingApprovals != 1 {
				t.Errorf("PendingApprovals=%d, want 1", sd.PendingApprovals)
			}
			t.Logf("session diag: kind=%s status=%s confidence=%.2f approvals=%d",
				sd.AgentKind, sd.AgentStatus, sd.AgentConfidence, sd.PendingApprovals)
		}
	}
	if !found {
		t.Error("session tmux:diag-session not found in diagnostic output")
	}
}

func TestRedactStr_HomePath(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/Users/mhk/project/foo", "<HOME>/project/foo"},
		{"/home/mhk/project/foo", "<HOME>/project/foo"},
		{"/Users/otheruser/.claude/logs", "<HOME>/.claude/logs"},
		{"no path here", "no path here"},
		{"", ""},
		{"Bearer secret123", "Bearer <REDACTED>"},
	}
	for _, tc := range tests {
		got := redactStr(tc.in)
		if got != tc.want {
			t.Errorf("redactStr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRedactStr_Truncation(t *testing.T) {
	long := strings.Repeat("x", 250)
	got := redactStr(long)
	if len(got) > 203 { // 200 + "..."
		t.Errorf("redactStr(long) len=%d, want <=203", len(got))
	}
}

// --- test sessions for capability derivation ---

type inputTestSession struct{}

func (s *inputTestSession) ID() string             { return "test" }
func (s *inputTestSession) Title() string          { return "Test" }
func (s *inputTestSession) AdapterName() string    { return "tmux" }
func (s *inputTestSession) WriteInput(_ context.Context, _ []byte) error { return nil }
func (s *inputTestSession) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	return nil, nil
}

type streamOnlySession struct{}

func (s *streamOnlySession) ID() string          { return "stream" }
func (s *streamOnlySession) Title() string       { return "Stream" }
func (s *streamOnlySession) AdapterName() string { return "tmux" }
func (s *streamOnlySession) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	return nil, nil
}

type observeOnlySession struct{}

func (s *observeOnlySession) ID() string          { return "observe" }
func (s *observeOnlySession) Title() string       { return "Observe" }
func (s *observeOnlySession) AdapterName() string { return "tmux" }

// --- diagnostic test adapter ---

type diagTestAdapter struct{}

func (a *diagTestAdapter) Name() string { return "tmux" }
func (a *diagTestAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{&diagTestSession{}}, nil
}
func (a *diagTestAdapter) ProcessSnapshot(_ context.Context) (map[string]models.ProcessInfo, error) {
	return map[string]models.ProcessInfo{
		"diag-session": {Command: "claude", CWD: "/Users/test/project"},
	}, nil
}

type diagTestSession struct{}

func (s *diagTestSession) ID() string          { return "diag-session" }
func (s *diagTestSession) Title() string       { return "Diag" }
func (s *diagTestSession) AdapterName() string { return "tmux" }
func (s *diagTestSession) ProcessInfo(_ context.Context) (models.ProcessInfo, error) {
	return models.ProcessInfo{Command: "claude", CWD: "/Users/test/project"}, nil
}

type alwaysApproveDetector struct{}

func (d *alwaysApproveDetector) DetectAgent(sessionID, adapterName, localID string, evidence ProdDetectionEvidence) (string, string, float64) {
	return "claude", "working", 0.9
}
