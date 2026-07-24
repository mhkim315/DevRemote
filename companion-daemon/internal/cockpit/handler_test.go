package cockpit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
)

func TestCockpitHandlerReturnsEmptyJSON(t *testing.T) {
	s := NewCockpitStore()
	s.AppendSession(Item{Kind: "runtime", State: "idle", Summary: "session", Origin: Origin{Provider: "codex", SessionID: "codex:1", RuntimeID: "runtime", Generation: "2"}})
	m := http.NewServeMux()
	sessions := devicetrust.NewPermissiveSessionManager("boot", time.Minute)
	RegisterCockpitHandler(m, s, sessions)
	rr := httptest.NewRecorder()
	m.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/cockpit", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatal(rr.Code)
	}
	token, _, _, err := sessions.CreateAfterVerifiedChallenge("device", "host", "boot", []string{devicetrust.PermSessionsRead}, 0)
	if err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/cockpit", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	m.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatal(rr.Code)
	}
	var payload map[string]any
	if json.Unmarshal(rr.Body.Bytes(), &payload) != nil {
		t.Fatal("invalid empty state")
	}
	if _, hasPascal := payload["Sessions"]; hasPascal || payload["sessions"] == nil {
		t.Fatalf("schema is not mobile camelCase: %s", rr.Body.String())
	}
	sessionsPayload, ok := payload["sessions"].([]any)
	if !ok || len(sessionsPayload) != 1 {
		t.Fatalf("sessions = %#v", payload["sessions"])
	}
	origin := sessionsPayload[0].(map[string]any)["origin"].(map[string]any)
	if origin["sessionId"] != "codex:1" || origin["runtimeId"] != "runtime" {
		t.Fatalf("mobile origin schema = %#v", origin)
	}
}
