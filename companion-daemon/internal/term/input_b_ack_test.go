package term

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"github.com/gorilla/websocket"
)

type inputBWriter struct {
	err   error
	short int
	wrote []byte
}

func (w *inputBWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n := len(p)
	if w.short > 0 && n > w.short {
		n = w.short
	}
	w.wrote = append(w.wrote, p[:n]...)
	return n, nil
}

type inputBWSFixture struct {
	server     *httptest.Server
	tickets    *devicetrust.WSTicketStore
	principal  *devicetrust.Principal
	hostID     string
	session    string
	generation int64
}

// newInputBWSFixture builds the real ticket-authenticated HandleWS path with
// a captured generation-7 transport. Tests exercise production reader/writer
// goroutines and may reconnect against the same server.
func newInputBWSFixture(t *testing.T, writer *inputBWriter) *inputBWSFixture {
	t.Helper()
	const session = "controlled_pty:input-b-ack"
	const generation = int64(7)
	pr, pw := io.Pipe()
	stream := &mockStream{pr: pr, pw: pw}
	recorder, _ := StartRecorder(session, stream)
	t.Cleanup(func() { _ = pw.Close(); recorder.Stop() })

	owned := NewOwnedPTYRuntime(nil, nil)
	owned.RegisterForTest(session, "", "test", recorder)
	owned.mu.Lock()
	owned.entries[session].transport = newTerminalTransport(session, generation, writer, stream, recorder)
	owned.mu.Unlock()

	identity, err := devicetrust.LoadOrCreateHostIdentity(&devicetrust.FileKeyStore{Path: filepath.Join(t.TempDir(), "host.json")})
	if err != nil {
		t.Fatal(err)
	}
	sessions := devicetrust.NewDeviceSessionManager("input-b-boot", time.Minute)
	bearer, _, _, err := sessions.CreateAfterVerifiedChallenge("device-owner", identity.HostID, sessions.BootID(), []string{devicetrust.PermSessionsRead, devicetrust.PermTerminalInput})
	if err != nil {
		t.Fatal(err)
	}
	principal := sessions.AuthenticateBearer(bearer)
	if principal == nil {
		t.Fatal("device session did not authenticate")
	}
	tickets := devicetrust.NewWSTicketStore()
	h := &Handlers{Lifecycle: NewLifecycleService(owned, nil), WSTickets: tickets, SessionMgr: sessions, HostIdentity: identity}
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	t.Cleanup(srv.Close)
	return &inputBWSFixture{server: srv, tickets: tickets, principal: principal, hostID: identity.HostID, session: session, generation: generation}
}

func (f *inputBWSFixture) dial(t *testing.T) *websocket.Conn {
	t.Helper()
	ticket, _, err := f.tickets.Issue(f.principal, f.hostID, f.session)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(f.server.URL)
	u.Scheme = "ws"
	u.Path = "/term/ws"
	u.RawQuery = "session=" + url.QueryEscape(f.session) + "&ticket=" + url.QueryEscape(ticket)
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func openInputBWS(t *testing.T, writer *inputBWriter) (*websocket.Conn, int64) {
	f := newInputBWSFixture(t, writer)
	conn := f.dial(t)
	t.Cleanup(func() { _ = conn.Close() })
	return conn, f.generation
}

// controlRequest builds a versioned terminal_input TextMessage.
func controlRequest(sessionID string, generation int64, inputID string, payload []byte) []byte {
	req := map[string]interface{}{
		"type":       "terminal_input",
		"version":    1,
		"sessionId":  sessionID,
		"generation": generation,
		"inputId":    inputID,
		"payload":    base64.StdEncoding.EncodeToString(payload),
	}
	b, _ := json.Marshal(req)
	return b
}

// testInputID returns a 64-char lowercase hex inputId for testing.
func testInputID() string {
	return "0000111122223333444455556666777788889999aaaabbbbccccddddeeeeffff"
}

func TestInputB_HandleWSAcknowledgesExactGenerationAfterWrite(t *testing.T) {
	writer := &inputBWriter{}
	conn, generation := openInputBWS(t, writer)

	// Read hello.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, helloPayload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var hello struct {
		Type       string `json:"type"`
		Generation int64  `json:"generation"`
	}
	if err := json.Unmarshal(helloPayload, &hello); err != nil || hello.Type != "hello" || hello.Generation != generation {
		t.Fatalf("hello=%s decoded=%+v err=%v", helloPayload, hello, err)
	}

	// Send versioned control request (TextMessage).
	input := []byte("accepted input")
	req := controlRequest("controlled_pty:input-b-ack", generation, testInputID(), input)
	if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
		t.Fatal(err)
	}

	// Read result.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, ackPayload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var ack struct {
		Type     string `json:"type"`
		InputID  string `json:"inputId"`
		Outcome  string `json:"outcome"`
		Sequence uint64 `json:"sequence"`
	}
	if mt != websocket.TextMessage || json.Unmarshal(ackPayload, &ack) != nil || ack.Type != "input_result" || ack.Outcome != "accepted" || ack.Sequence != 1 {
		t.Fatalf("result type=%d payload=%s decoded=%+v", mt, ackPayload, ack)
	}
	if string(writer.wrote) != string(input) {
		t.Fatalf("WriteInput data=%q, want %q", writer.wrote, input)
	}
}

func TestInputB_HandleWSWriteFailureDoesNotAcknowledge(t *testing.T) {
	writer := &inputBWriter{err: errors.New("write failed")}
	conn, _ := openInputBWS(t, writer)

	// Read hello.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}

	// Send control request — write will fail.
	req := controlRequest("controlled_pty:input-b-ack", 7, testInputID(), []byte("must not ack"))
	if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
		t.Fatal(err)
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Type    string `json:"type"`
		Outcome string `json:"outcome"`
	}
	if mt != websocket.TextMessage || json.Unmarshal(payload, &result) != nil || result.Type != "input_result" {
		t.Fatalf("result type=%d payload=%s", mt, payload)
	}
	if result.Outcome != "write_failed" {
		t.Fatalf("expected write_failed, got %q", result.Outcome)
	}
}

func TestInputB_HandleWSShortWriteIsWriteFailed(t *testing.T) {
	writer := &inputBWriter{short: 1}
	conn, _ := openInputBWS(t, writer)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, controlRequest("controlled_pty:input-b-ack", 7, testInputID(), []byte("short write"))); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(payload, &result); err != nil || result.Outcome != "write_failed" {
		t.Fatalf("short-write result=%s decoded=%+v err=%v", payload, result, err)
	}
}

func TestInputB_DuplicateInputIDReplaysCached(t *testing.T) {
	writer := &inputBWriter{}
	conn, generation := openInputBWS(t, writer)

	// Read hello.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage()

	inputID := testInputID()
	input := []byte("duplicate test")
	req := controlRequest("controlled_pty:input-b-ack", generation, inputID, input)

	// First write — accepted.
	conn.WriteMessage(websocket.TextMessage, req)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, ackPayload, _ := conn.ReadMessage()
	var ack struct {
		Type     string `json:"type"`
		Outcome  string `json:"outcome"`
		Sequence uint64 `json:"sequence"`
	}
	json.Unmarshal(ackPayload, &ack)
	if mt != websocket.TextMessage || ack.Outcome != "accepted" || ack.Sequence != 1 {
		t.Fatalf("first write: outcome=%q seq=%d", ack.Outcome, ack.Sequence)
	}

	// Second write with same inputID and same payload — cached duplicate.
	conn.WriteMessage(websocket.TextMessage, req)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, ackPayload, _ = conn.ReadMessage()
	json.Unmarshal(ackPayload, &ack)
	if ack.Outcome != "accepted" || ack.Sequence != 1 {
		t.Fatalf("duplicate: outcome=%q seq=%d (want accepted seq=1 cached)", ack.Outcome, ack.Sequence)
	}
	// WriteInput must NOT have been called a second time.
	if len(writer.wrote) != len(input) {
		t.Fatalf("WriteInput called twice: wrote %d bytes, want %d", len(writer.wrote), len(input))
	}
}

func TestInputB_QueuedDuplicateArrivalWritesOnlyOnce(t *testing.T) {
	writer := &inputBWriter{}
	conn, generation := openInputBWS(t, writer)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	payload := []byte("queued duplicate")
	req := controlRequest("controlled_pty:input-b-ack", generation, testInputID(), payload)
	// Queue both frames before the reader consumes either result. HandleWS owns
	// one ordered reader, so the second arrival must hit the first result cache.
	if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		var result inputResult
		if err := json.Unmarshal(raw, &result); err != nil || result.Outcome != "accepted" || result.Sequence != 1 {
			t.Fatalf("queued duplicate result=%s decoded=%+v err=%v", raw, result, err)
		}
	}
	if string(writer.wrote) != string(payload) {
		t.Fatalf("queued duplicate wrote %q, want one %q", writer.wrote, payload)
	}
}

func TestInputB_HandleWSCloseDropsConnectionReplayState(t *testing.T) {
	writer := &inputBWriter{}
	f := newInputBWSFixture(t, writer)
	conn := f.dial(t)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	req := controlRequest(f.session, f.generation, testInputID(), []byte("per connection"))
	if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	// A new authenticated HandleWS connection owns a fresh replay cache. This
	// exercises the production close/defer path rather than calling clear() in
	// isolation; the reused input ID is a new per-connection request.
	conn = f.dial(t)
	t.Cleanup(func() { _ = conn.Close() })
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var result inputResult
	if err := json.Unmarshal(raw, &result); err != nil || result.Outcome != "accepted" {
		t.Fatalf("reconnected result=%s decoded=%+v err=%v", raw, result, err)
	}
	if string(writer.wrote) != "per connectionper connection" {
		t.Fatalf("writes=%q", writer.wrote)
	}
}

func TestInputB_InputIDConflictDifferentPayload(t *testing.T) {
	writer := &inputBWriter{}
	conn, generation := openInputBWS(t, writer)

	// Read hello.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage()

	inputID := testInputID()
	// First write.
	req1 := controlRequest("controlled_pty:input-b-ack", generation, inputID, []byte("first"))
	conn.WriteMessage(websocket.TextMessage, req1)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage() // accepted

	// Same inputID, DIFFERENT payload — must be input_id_conflict (not
	// supported by the protocol yet — we just check it doesn't replay).
	req2 := controlRequest("controlled_pty:input-b-ack", generation, inputID, []byte("second"))
	conn.WriteMessage(websocket.TextMessage, req2)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, ackPayload, _ := conn.ReadMessage()
	var ack struct {
		Outcome string `json:"outcome"`
	}
	json.Unmarshal(ackPayload, &ack)
	if ack.Outcome == "accepted" {
		t.Fatal("conflicting inputID was accepted — must be rejected")
	}
}

func TestInputB_ProductionRejectsLegacyBinaryBeforeWrite(t *testing.T) {
	writer := &inputBWriter{}
	conn, _ := openInputBWS(t, writer) // InsecureLocalOnly is false: paired production.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("legacy binary")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Outcome      string `json:"outcome"`
		Reason       string `json:"reason"`
		ConnectionID string `json:"connectionId"`
		SessionID    string `json:"sessionId"`
		Generation   int64  `json:"generation"`
	}
	if err := json.Unmarshal(payload, &result); err != nil || result.Outcome != "invalid_request" || result.Reason != "update_required" || result.ConnectionID == "" || result.SessionID != "controlled_pty:input-b-ack" || result.Generation != 7 {
		t.Fatalf("legacy binary result=%s decoded=%+v err=%v", payload, result, err)
	}
	if len(writer.wrote) != 0 {
		t.Fatalf("production legacy binary wrote %q", writer.wrote)
	}
}

func TestInputB_TrailingTerminalInputIsRejected(t *testing.T) {
	writer := &inputBWriter{}
	conn, generation := openInputBWS(t, writer)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	req := append(controlRequest("controlled_pty:input-b-ack", generation, testInputID(), []byte("no write")), []byte(` {}`)...)
	if err := conn.WriteMessage(websocket.TextMessage, req); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(payload, &result); err != nil || result.Outcome != "invalid_request" {
		t.Fatalf("trailing result=%s decoded=%+v err=%v", payload, result, err)
	}
	if len(writer.wrote) != 0 {
		t.Fatalf("trailing request wrote %q", writer.wrote)
	}
}

func TestInputB_ConnectionIDEntropyFailureIsFailClosed(t *testing.T) {
	old := connectionIDEntropy
	connectionIDEntropy = func([]byte) (int, error) { return 0, errors.New("entropy unavailable") }
	t.Cleanup(func() { connectionIDEntropy = old })
	if id, err := newConnectionID(); err == nil || id != "" {
		t.Fatalf("newConnectionID = %q, %v; want fail closed", id, err)
	}
}
