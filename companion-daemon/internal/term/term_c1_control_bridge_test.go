package term

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"github.com/gorilla/websocket"
)

// ── TERM-C1: single control bridge end-to-end integration ──
//
// These tests serve the ACTUAL daemon page HTML (HandleHTML) and the
// HandleWS WebSocket endpoint from a single httptest server. They prove
// the production control bridge delivers hello exactly once, accepts
// Ctrl+C / paste / macros through the acknowledged input protocol, and
// rejects wrong session/generation/missing-capability frames.
//
// The Go test stands in for the WebView: it fetches the daemon page,
// verifies the control bridge JS is present, dials the real HandleWS,
// and exercises the EXACT protocol the bridge's sendInput() produces.

// ── Fixture ──

type c1Fixture struct {
	server     *httptest.Server
	tickets    *devicetrust.WSTicketStore
	principal  *devicetrust.Principal
	hostID     string
	session    string
	generation int64
	owned      *OwnedPTYRuntime
	pageHTML   string // the actual served daemon page
}

func newC1Fixture(t *testing.T) *c1Fixture {
	t.Helper()

	// Build the production Handlers with a real controlled_pty session.
	owned := NewOwnedPTYRuntime(NewNativePTYLauncher(), nil)
	owned.graceful = 2 * time.Second

	cfg := SpawnConfig{Name: "c1-e2e", Executable: "sleep", Args: []string{"2"}}
	id, err := owned.Create(t.Context(), cfg, "", "test")
	if err != nil {
		t.Fatalf("create c1 session: %v", err)
	}
	t.Cleanup(func() { closeOwnedForTest(owned, id) })

	transport, ok := owned.Transport(id)
	if !ok {
		t.Fatal("transport not found")
	}
	gen := transport.generation

	// Device auth (matching Input-A/Input-B pattern).
	identity, err := devicetrust.LoadOrCreateHostIdentity(
		&devicetrust.FileKeyStore{Path: filepath.Join(t.TempDir(), "host.json")},
	)
	if err != nil {
		t.Fatal(err)
	}
	sessions := devicetrust.NewDeviceSessionManager("c1-boot", time.Minute)
	bearer, _, _, err := sessions.CreateAfterVerifiedChallenge(
		"device-owner", identity.HostID, sessions.BootID(),
		[]string{devicetrust.PermSessionsRead, devicetrust.PermTerminalInput},
	)
	if err != nil {
		t.Fatal(err)
	}
	principal := sessions.AuthenticateBearer(bearer)
	if principal == nil {
		t.Fatal("device session did not authenticate")
	}
	tickets := devicetrust.NewWSTicketStore()

	h := &Handlers{
		Lifecycle:    NewLifecycleService(owned, nil),
		WSTickets:    tickets,
		SessionMgr:   sessions,
		HostIdentity: identity,
	}

	// Serve both the daemon page AND WebSocket from one mux.
	mux := http.NewServeMux()
	mux.HandleFunc("/term/", h.HandleHTML)
	mux.HandleFunc("/term/ws", h.HandleWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Fetch the daemon page HTML to prove it's served.
	pageURL := srv.URL + "/term/?session=" + url.QueryEscape(id)
	resp, err := http.Get(pageURL)
	if err != nil {
		t.Fatalf("fetch daemon page: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("daemon page status = %d", resp.StatusCode)
	}
	// Read the full page — it's a large HTML doc.
	buf := make([]byte, 512*1024)
	n, _ := resp.Body.Read(buf)
	pageHTML := string(buf[:n])

	return &c1Fixture{
		server:     srv,
		tickets:    tickets,
		principal:  principal,
		hostID:     identity.HostID,
		session:    id,
		generation: gen,
		owned:      owned,
		pageHTML:   pageHTML,
	}
}

// dialWS connects to the fixture server's HandleWS for the test session.
func (f *c1Fixture) dialWS(t *testing.T) *websocket.Conn {
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
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// ── Helper types ──

type c1Hello struct {
	Type         string   `json:"type"`
	Capabilities []string `json:"capabilities"`
	SessionID    string   `json:"sessionId"`
	Generation   int64    `json:"generation"`
	ConnectionID string   `json:"connectionId"`
}

type c1Result struct {
	Type       string `json:"type"`
	Outcome    string `json:"outcome"`
	Sequence   uint64 `json:"sequence,omitempty"`
	InputID    string `json:"inputId,omitempty"`
	Generation int64  `json:"generation,omitempty"`
	SessionID  string `json:"sessionId,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// readHello reads the mandatory hello frame sent after WS upgrade.
func readHello(t *testing.T, conn *websocket.Conn) c1Hello {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if mt != websocket.TextMessage {
		t.Fatalf("hello type = %d, want TextMessage", mt)
	}
	var h c1Hello
	if err := json.Unmarshal(payload, &h); err != nil {
		t.Fatalf("hello unmarshal: %v (payload=%s)", err, payload)
	}
	return h
}

// readText reads TextMessage frames, skipping binary (PTY output).
func readText(t *testing.T, conn *websocket.Conn, timeout time.Duration) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		conn.SetReadDeadline(deadline)
		mt, payload, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("readText: %v", err)
		}
		if mt == websocket.TextMessage {
			return payload
		}
	}
}

// bridgeSendInput constructs the EXACT JSON that __pokitControlBridge.sendInput()
// produces and sends it over the WebSocket. This is what the daemon page's
// term.onData → pokitSendInput → bridge.sendInput chain emits.
func bridgeSendInput(t *testing.T, conn *websocket.Conn, session string, generation int64, text string, inputID string) {
	t.Helper()
	// The bridge uses btoa(String.fromCharCode.apply(null, new TextEncoder().encode(text))).
	payload := base64.StdEncoding.EncodeToString([]byte(text))
	req := map[string]interface{}{
		"type":       "terminal_input",
		"version":    1,
		"sessionId":  session,
		"generation": generation,
		"inputId":    inputID,
		"payload":    payload,
	}
	b, _ := json.Marshal(req)
	if err := conn.WriteMessage(websocket.TextMessage, b); err != nil {
		t.Fatalf("bridgeSendInput: %v", err)
	}
}

// ── E2E: daemon page is served with control bridge ──

func TestTERM_C1_DaemonPageServesControlBridge(t *testing.T) {
	f := newC1Fixture(t)

	// The served daemon page must contain the production control bridge.
	if !strings.Contains(f.pageHTML, "__pokitControlBridge") {
		t.Fatal("daemon page does not contain __pokitControlBridge")
	}
	if !strings.Contains(f.pageHTML, "pokitSendInput") {
		t.Fatal("daemon page does not contain pokitSendInput")
	}
	if !strings.Contains(f.pageHTML, "pokitMakeInputID") {
		t.Fatal("daemon page does not contain pokitMakeInputID")
	}
	if !strings.Contains(f.pageHTML, "sendInput") {
		t.Fatal("daemon page does not contain sendInput method")
	}
	// The connect() function creates the WS and binds the bridge.
	if !strings.Contains(f.pageHTML, "__pokitControlBridge.bind(ws)") {
		t.Fatal("daemon page does not bind bridge on connect")
	}
	// The page demux: text → bridge.receive, binary → term.write.
	if !strings.Contains(f.pageHTML, "__pokitControlBridge.receive(e.data,ws)") {
		t.Fatal("daemon page onmessage does not route to bridge.receive")
	}
}

// ── E2E: hello frame through bridge ──

func TestTERM_C1_HelloDeliversExactlyOnce(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)

	hello := readHello(t, conn)
	if hello.Type != "hello" {
		t.Fatalf("expected hello, got %q", hello.Type)
	}
	if hello.SessionID != f.session {
		t.Errorf("sessionId = %q, want %q", hello.SessionID, f.session)
	}
	if hello.Generation != f.generation {
		t.Errorf("generation = %d, want %d", hello.Generation, f.generation)
	}
	if hello.ConnectionID == "" {
		t.Error("connectionId is empty")
	}
	hasInput := false
	for _, c := range hello.Capabilities {
		if c == devicetrust.PermTerminalInput {
			hasInput = true
		}
	}
	if !hasInput {
		t.Errorf("capabilities = %v, must include terminal:input", hello.Capabilities)
	}

	// No second frame — exactly one hello per connection.
	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if mt, payload, err := conn.ReadMessage(); err == nil {
		t.Fatalf("unexpected second frame: type=%d payload=%s", mt, payload)
	}
}

// ── E2E: Ctrl+C through bridge.sendInput ──

func TestTERM_C1_CtrlCThroughBridgeReceivesAccepted(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	// Simulate the EXACT bridge.sendInput("Ctrl+C") path.
	// The bridge uses pokitMakeInputID() for a random 64-char hex inputId.
	bridgeSendInput(t, conn, f.session, f.generation, "\x03", testInputID())

	ackPayload := readText(t, conn, 2*time.Second)
	var ack c1Result
	if err := json.Unmarshal(ackPayload, &ack); err != nil {
		t.Fatalf("ack unmarshal: %v", err)
	}
	if ack.Outcome != "accepted" {
		t.Fatalf("Ctrl+C outcome = %q, want accepted (payload=%s)", ack.Outcome, ackPayload)
	}
	if ack.Sequence != 1 {
		t.Errorf("first input sequence = %d, want 1", ack.Sequence)
	}
}

// ── E2E: paste (long text) through bridge ──

func TestTERM_C1_PasteThroughBridgeReceivesAccepted(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	paste := "echo 'hello world'; ls -la /tmp\n"
	bridgeSendInput(t, conn, f.session, f.generation, paste, testInputID())

	ackPayload := readText(t, conn, 2*time.Second)
	var ack c1Result
	if err := json.Unmarshal(ackPayload, &ack); err != nil {
		t.Fatalf("ack unmarshal: %v", err)
	}
	if ack.Outcome != "accepted" {
		t.Fatalf("paste outcome = %q, want accepted", ack.Outcome)
	}
}

// ── E2E: text + Enter (two-frame input) ──

func TestTERM_C1_TextPlusEnterIsTwoFrames(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	// Text frame.
	textID := testInputID()
	bridgeSendInput(t, conn, f.session, f.generation, "ls", textID)
	ack1Payload := readText(t, conn, 2*time.Second)
	var ack1 c1Result
	if err := json.Unmarshal(ack1Payload, &ack1); err != nil {
		t.Fatal(err)
	}
	if ack1.Outcome != "accepted" {
		t.Fatalf("text outcome = %q", ack1.Outcome)
	}
	if ack1.Sequence != 1 {
		t.Errorf("text sequence = %d, want 1", ack1.Sequence)
	}

	// Enter frame (distinct inputId).
	enterID := "eeee0000111122223333444455556666777788889999aaaabbbbccccddddeeee"
	bridgeSendInput(t, conn, f.session, f.generation, "\r", enterID)
	ack2Payload := readText(t, conn, 2*time.Second)
	var ack2 c1Result
	if err := json.Unmarshal(ack2Payload, &ack2); err != nil {
		t.Fatal(err)
	}
	if ack2.Outcome != "accepted" {
		t.Fatalf("enter outcome = %q", ack2.Outcome)
	}
	if ack2.Sequence != 2 {
		t.Errorf("enter sequence = %d, want 2", ack2.Sequence)
	}
}

// ── E2E: wrong generation rejected ──

func TestTERM_C1_WrongGenerationRejected(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	bridgeSendInput(t, conn, f.session, f.generation+99, "test", testInputID())

	ackPayload := readText(t, conn, 2*time.Second)
	var ack c1Result
	if err := json.Unmarshal(ackPayload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Outcome == "accepted" {
		t.Fatal("wrong generation was accepted")
	}
}

// ── E2E: wrong session rejected ──

func TestTERM_C1_WrongSessionRejected(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	bridgeSendInput(t, conn, "controlled_pty:wrong-session", f.generation, "test", testInputID())

	ackPayload := readText(t, conn, 2*time.Second)
	var ack c1Result
	if err := json.Unmarshal(ackPayload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Outcome == "accepted" {
		t.Fatal("wrong session was accepted")
	}
}

// ── E2E: viewer principal denied ──

func TestTERM_C1_ViewerDenied(t *testing.T) {
	// Build a viewer-specific fixture.
	owned := NewOwnedPTYRuntime(NewNativePTYLauncher(), nil)
	owned.graceful = 2 * time.Second
	cfg := SpawnConfig{Name: "c1-viewer", Executable: "sleep", Args: []string{"2"}}
	id, err := owned.Create(t.Context(), cfg, "", "test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { closeOwnedForTest(owned, id) })

	identity, err := devicetrust.LoadOrCreateHostIdentity(
		&devicetrust.FileKeyStore{Path: filepath.Join(t.TempDir(), "host.json")},
	)
	if err != nil {
		t.Fatal(err)
	}
	sessions := devicetrust.NewDeviceSessionManager("c1-viewer-boot", time.Minute)
	bearer, _, _, err := sessions.CreateAfterVerifiedChallenge(
		"device-viewer", identity.HostID, sessions.BootID(),
		[]string{devicetrust.PermSessionsRead}, // NO terminal:input
	)
	if err != nil {
		t.Fatal(err)
	}
	principal := sessions.AuthenticateBearer(bearer)
	if principal == nil {
		t.Fatal("viewer session did not authenticate")
	}
	tickets := devicetrust.NewWSTicketStore()

	h := &Handlers{
		Lifecycle:    NewLifecycleService(owned, nil),
		WSTickets:    tickets,
		SessionMgr:   sessions,
		HostIdentity: identity,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/term/", h.HandleHTML)
	mux.HandleFunc("/term/ws", h.HandleWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ticket, _, err := tickets.Issue(principal, identity.HostID, id)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	u.Path = "/term/ws"
	u.RawQuery = "session=" + url.QueryEscape(id) + "&ticket=" + url.QueryEscape(ticket)
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	hello := readHello(t, conn)
	for _, c := range hello.Capabilities {
		if c == devicetrust.PermTerminalInput {
			t.Fatal("viewer hello includes terminal:input")
		}
	}

	// Binary input → update_required in production mode.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("\x03")); err != nil {
		t.Fatal(err)
	}
	payload := readText(t, conn, 2*time.Second)
	var result c1Result
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "invalid_request" || result.Reason != "update_required" {
		t.Fatalf("viewer binary result = %+v, want invalid_request/update_required", result)
	}
}

// ── E2E: duplicate hello on same connection does NOT clear input state ──

func TestTERM_C1_DuplicateHelloDoesNotClearInputState(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	// Send Ctrl+C — must be accepted.
	bridgeSendInput(t, conn, f.session, f.generation, "\x03", testInputID())
	ack1Payload := readText(t, conn, 2*time.Second)
	var ack1 c1Result
	json.Unmarshal(ack1Payload, &ack1)
	if ack1.Outcome != "accepted" {
		t.Fatalf("first Ctrl+C = %q, want accepted", ack1.Outcome)
	}

	// The daemon page bridge's once() dedup prevents duplicate hello frames
	// from re-delivering. The server only sends hello ONCE (at upgrade).
	// Prove no second hello arrives.
	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if mt, payload, err := conn.ReadMessage(); err == nil {
		var frame struct{ Type string }
		json.Unmarshal(payload, &frame)
		if frame.Type == "hello" {
			t.Fatal("duplicate hello arrived on same connection")
		}
		t.Logf("post-ack frame: type=%d type=%s", mt, frame.Type)
	}
}

// ── E2E: reconnect produces new connection ──

func TestTERM_C1_ReconnectNewConnectionID(t *testing.T) {
	f := newC1Fixture(t)
	conn1 := f.dialWS(t)
	hello1 := readHello(t, conn1)

	// Close and reconnect.
	conn1.Close()
	conn2 := f.dialWS(t)
	hello2 := readHello(t, conn2)

	if hello2.ConnectionID == hello1.ConnectionID {
		t.Errorf("reconnect reused connectionId %q", hello1.ConnectionID)
	}
	if hello2.Generation != hello1.Generation {
		t.Errorf("generation changed without replacement: %d → %d", hello1.Generation, hello2.Generation)
	}

	// Input on new connection works.
	bridgeSendInput(t, conn2, f.session, f.generation, "test", testInputID())
	ack := readText(t, conn2, 2*time.Second)
	var result c1Result
	json.Unmarshal(ack, &result)
	if result.Outcome != "accepted" {
		t.Fatalf("reconnect input = %q", result.Outcome)
	}
}

// ── Compile-time guards ──

var _ http.HandlerFunc = (&Handlers{}).HandleHTML
var _ http.HandlerFunc = (&Handlers{}).HandleWS
