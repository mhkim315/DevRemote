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
			Options:   []agent.ApprovalOption{{ID: "approve", Label: "Approve"}, {ID: "reject", Label: "Reject"}},
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
			Options:   []agent.ApprovalOption{{ID: "approve", Label: "Approve"}},
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
			Options:   []agent.ApprovalOption{{ID: "approve", Label: "Approve"}},
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
			Options:   []agent.ApprovalOption{{ID: "open_terminal", Label: "Open Terminal"}},
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

func TestHandleApprovalAction_ViewOnly(t *testing.T) {
	s := NewApprovalStore()
	s.Upsert("s1", []agent.AgentApproval{
		{
			ID: "a1", SessionID: "s1", Status: "pending",
			Options:   []agent.ApprovalOption{{ID: "view_only", Label: "View Only"}},
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
		t.Errorf("view_only: status=%d, want 400 (approve not in options)", rec.Code)
	}
}

// --- Capability-aware option creation ---

func TestCapabilityFilter_InputWritable(t *testing.T) {
	// Session with InputWriter + StreamOpener gets full action options.
	sess := &inputTestSession{}
	opts := buildApprovalOptions(sess)
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
	opts := buildApprovalOptions(sess)
	if !optionInList(opts, "open_terminal") {
		t.Error("stream-only session missing open_terminal")
	}
	if optionInList(opts, "approve") || optionInList(opts, "reject") {
		t.Error("stream-only session should not have approve/reject")
	}
}

func TestCapabilityFilter_ObserveOnly(t *testing.T) {
	// Session with no capabilities gets view_only.
	sess := &observeOnlySession{}
	opts := buildApprovalOptions(sess)
	if optionInList(opts, "approve") || optionInList(opts, "reject") {
		t.Error("observe-only session should not have approve/reject")
	}
	if optionInList(opts, "send_text") || optionInList(opts, "send_key") {
		t.Error("observe-only session should not have send_text/send_key")
	}
	if !optionInList(opts, "view_only") {
		t.Error("observe-only session missing view_only fallback")
	}
}

// --- helpers ---

func optionInList(options []agent.ApprovalOption, id string) bool {
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
