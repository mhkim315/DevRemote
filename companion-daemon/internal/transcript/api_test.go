package transcript

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

func TestHandleTranscript_ReturnsResponse(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	projectStubEvents(svc, "controlled_pty:test", []agentEventStub{
		{id: "e1", sessionID: "controlled_pty:test", agentKind: "codex", eventType: "assistant_message", text: "hello"},
	})

	resp := doGet(t, svc, "/api/sessions/controlled_pty:test/transcript", "")
	if resp == nil {
		t.Fatal("nil response")
	}
	if resp.SessionID != "controlled_pty:test" {
		t.Errorf("sessionId: got %q", resp.SessionID)
	}
	if len(resp.Semantic) == 0 {
		t.Error("no semantic segments")
	}
	if resp.PrimarySource != SourceAgentEvent {
		t.Errorf("primary: got %q", resp.PrimarySource)
	}
}

func TestHandleTranscript_EmptySession(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	resp := doGet(t, svc, "/api/sessions/nonexistent/transcript", "")
	if resp == nil {
		t.Fatal("nil response")
	}
	if len(resp.Semantic) != 0 {
		t.Errorf("expected empty, got %d segments", len(resp.Semantic))
	}
}

func TestHandleTranscript_WrongSession(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	projectStubEvents(svc, "controlled_pty:sessionA", []agentEventStub{
		{id: "e1", sessionID: "controlled_pty:sessionA", agentKind: "codex", eventType: "assistant_message", text: "secret"},
	})

	resp := doGet(t, svc, "/api/sessions/controlled_pty:sessionB/transcript", "")
	if resp == nil {
		t.Fatal("nil response")
	}
	// Session B has no events — no cross-session leakage.
	if len(resp.Semantic) != 0 {
		t.Errorf("cross-session leak: got %d segments for session B", len(resp.Semantic))
	}
}

func TestHandleTranscript_Pagination(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	// Add multiple segments.
	for i := 0; i < 5; i++ {
		projectStubEvents(svc, "controlled_pty:test", []agentEventStub{
			{id: "e" + string(rune('0'+i)), sessionID: "controlled_pty:test", agentKind: "codex", eventType: "assistant_message", text: "msg"},
		})
	}

	// First page: all segments.
	resp := doGet(t, svc, "/api/sessions/controlled_pty:test/transcript", "")
	if resp == nil || len(resp.Semantic) < 5 {
		t.Fatalf("expected >=5, got %d", len(resp.Semantic))
	}
	afterSeq := resp.Semantic[2].Seq

	// Second page: after cursor.
	resp2 := doGet(t, svc, "/api/sessions/controlled_pty:test/transcript?after="+itoa64(afterSeq), "")
	if resp2 == nil {
		t.Fatal("nil response for paginated")
	}
	// Should only have segments with seq > afterSeq.
	for _, seg := range resp2.Semantic {
		if seg.Seq <= afterSeq {
			t.Errorf("paginated segment seq=%d not > after=%d", seg.Seq, afterSeq)
		}
	}
	t.Logf("page 1: %d, page 2 (after %d): %d", len(resp.Semantic), afterSeq, len(resp2.Semantic))
}

func TestHandleTranscript_MethodNotAllowed(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	h := HandleTranscript(svc)
	req := httptest.NewRequest("POST", "/api/sessions/x/transcript", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: got %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleTranscript_InvalidCursor(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	h := HandleTranscript(svc)
	req := httptest.NewRequest("GET", "/api/sessions/x/transcript?after=notanumber", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid cursor: got %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// ── Helpers ──

type agentEventStub struct {
	id, sessionID, agentKind, eventType, text string
}

func projectStubEvents(s *Service, sessionID string, stubs []agentEventStub) {
	// Set correlation so projection gate passes.
	s.mu.Lock()
	arb := s.ensureArbiter(sessionID)
	arb.SetCorrelation(CorrelationState{
		SessionID:   sessionID,
		Correlation: "managed_launch",
		Provider:    "codex",
	})
	s.mu.Unlock()

	for _, st := range stubs {
		seg := NewAgentEventSegment(st.id, st.sessionID, st.agentKind, st.eventType, st.text, "", 1.0, time.Now())
		s.store.Append(sessionID, []TranscriptSegment{seg})
		s.mu.Lock()
		arb := s.ensureArbiter(sessionID)
		arb.RecordAgentEvent()
		s.mu.Unlock()
	}
}

func doGet(t *testing.T, svc *Service, url, authHeader string) *TranscriptResponse {
	t.Helper()
	h := HandleTranscript(svc)

	// Extract path and query from URL.
	parts := strings.SplitN(url, "?", 2)
	path := parts[0]
	req := httptest.NewRequest("GET", path, nil)
	if len(parts) > 1 {
		req.URL.RawQuery = parts[1]
	}
	// Set the path value for Go 1.22+ routing.
	req.SetPathValue("id", extractSessionID(path))

	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Logf("non-200 response: %d body=%s", rec.Code, rec.Body.String())
		return nil
	}

	var resp TranscriptResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Logf("decode error: %v", err)
		return nil
	}
	return &resp
}

func extractSessionID(path string) string {
	// /api/sessions/{id}/transcript → {id}
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if p == "sessions" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}

// ── Authentication tests (RequirePrincipal wrapper) ──

func TestHandleTranscript_Auth_MissingToken(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	h := devicetrust.RequirePrincipal(
		devicetrust.NewPermissiveSessionManager("b", 20*time.Minute),
		HandleTranscript(svc),
		devicetrust.PermSessionsRead,
	)
	req := httptest.NewRequest("GET", "/api/sessions/x/transcript", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleTranscript_Auth_InvalidToken(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	h := devicetrust.RequirePrincipal(
		devicetrust.NewPermissiveSessionManager("b", 20*time.Minute),
		HandleTranscript(svc),
		devicetrust.PermSessionsRead,
	)
	req := httptest.NewRequest("GET", "/api/sessions/x/transcript", nil)
	req.Header.Set("Authorization", "Bearer deadbeef")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("invalid token: got %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestHandleTranscript_Auth_ValidToken(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	m := devicetrust.NewPermissiveSessionManager("b", 20*time.Minute)
	raw, _, _, err := m.CreateAfterVerifiedChallenge("d1", "h", "b", devicetrust.PermissionsForRole(devicetrust.RoleOwner), 0)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	h := devicetrust.RequirePrincipal(m, HandleTranscript(svc), devicetrust.PermSessionsRead)
	req := httptest.NewRequest("GET", "/api/sessions/x/transcript", nil)
	req.SetPathValue("id", "x")
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("valid token: got %d, want 200", rec.Code)
	}
}

func TestHandleTranscript_Auth_WrongPermission(t *testing.T) {
	svc := NewService(DefaultStoreConfig())
	m := devicetrust.NewPermissiveSessionManager("b", 20*time.Minute)
	raw, _, _, err := m.CreateAfterVerifiedChallenge("d1", "h", "b", devicetrust.PermissionsForRole(devicetrust.RoleMember), 0)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	// RoleMember has sessions:read but NOT sessions:kill.
	h := devicetrust.RequirePrincipal(m, HandleTranscript(svc), devicetrust.PermSessionsKill)
	req := httptest.NewRequest("GET", "/api/sessions/x/transcript", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("wrong permission: got %d, want %d", rec.Code, http.StatusForbidden)
	}
}
