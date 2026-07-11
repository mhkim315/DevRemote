package term

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"github.com/gorilla/websocket"
)

// Phase 6 E2E: LocalPTYAdapter with real child PTY process.
// Tests create → discover → WS live output/input → terminate.

func TestLocalPTY_E2E_Lifecycle(t *testing.T) {
	adapter := mux.NewLocalPTYAdapter()
	reg := mux.MustNewRegistry(adapter)
	h := &Handlers{Registry: reg, Events: NewMemoryEventStore()}

	// 1. Create session (real bash process).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// cat echoes input immediately — no prompt, no banner, deterministic.
	createID, err := reg.CreateSession(ctx, adapter.Name(), mux.CreateOptions{
		Name:    "e2e-test",
		Command: "cat",
	})
	if err != nil {
		if strings.Contains(err.Error(), "SpawnPTY") || strings.Contains(err.Error(), "failed to start") {
			t.Skipf("PTY not available in this environment: %v", err)
		}
		t.Fatalf("CreateSession: %v", err)
	}
	if createID == "" {
		t.Fatal("CreateSession returned empty ID")
	}
	t.Cleanup(func() {
		reg.TerminateSession(context.Background(), adapter.Name(), createID)
	})

	// 2. Discover via API.
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	rec := httptest.NewRecorder()
	h.HandleSessionsAPI(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/sessions: status %d", rec.Code)
	}

	var sessions []SessionTelemetry
	if err := json.Unmarshal(rec.Body.Bytes(), &sessions); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	found := false
	for _, s := range sessions {
		if s.Adapter == "localpty" && s.ID == "localpty:"+createID {
			found = true
		}
	}
	if !found {
		t.Error("created localpty session not found in API response")
	}

	// 3. WebSocket connect.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.RawQuery = "session=localpty:" + createID
		h.HandleWS(w, r)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/term/ws?session=localpty:" + createID
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("WS dial: %v", err)
	}
	defer conn.Close()

	// 4. Write input — cat echoes it back.
	// M3-auth-4A framing contract: raw terminal input is a BINARY frame.
	testInput := []byte("LOCALPTY_E2E_OK\n")
	if err := conn.WriteMessage(websocket.BinaryMessage, testInput); err != nil {
		t.Fatalf("WS write: %v", err)
	}

	// 5. Read echoed output (cat echoes input to stdout via PTY).
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("WS read (echo): %v", err)
	}
	if !strings.Contains(string(msg), "LOCALPTY_E2E_OK") {
		t.Errorf("cat echo not received: %q", string(msg))
	}

	// 7. Terminate via API.
	delReq := httptest.NewRequest("DELETE", "/api/sessions?id=localpty:"+createID, nil)
	delRec := httptest.NewRecorder()
	h.HandleSessionsAPI(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Errorf("DELETE: status %d, want 200", delRec.Code)
	}

	// 8. Verify gone.
	req2 := httptest.NewRequest("GET", "/api/sessions", nil)
	rec2 := httptest.NewRecorder()
	h.HandleSessionsAPI(rec2, req2)
	var after []SessionTelemetry
	json.Unmarshal(rec2.Body.Bytes(), &after)
	for _, s := range after {
		if s.Adapter == "localpty" && s.ID == "localpty:"+createID {
			t.Errorf("session %s still present after terminate", s.ID)
		}
	}
}
