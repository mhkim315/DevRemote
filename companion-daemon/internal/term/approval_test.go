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
