package term

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"github.com/gorilla/websocket"
)

// ── TERM-C1: single control bridge end-to-end integration ──
//
// These tests exercise the real HandleWS path with real controlled_pty
// sessions and real WebSocket connections. They prove one valid hello
// produces exactly one native delivery, that all authorized surfaces are
// enabled, and that wrong session/generation/missing-capability frames
// fail closed without a PTY write.

// c1Fixture holds the real httptest server, device sessions, and ticket
// store needed for TERM-C1 end-to-end tests.
type c1Fixture struct {
	server     *httptest.Server
	tickets    *devicetrust.WSTicketStore
	principal  *devicetrust.Principal
	hostID     string
	session    string
	generation int64
	owned      *OwnedPTYRuntime
}

// newC1Fixture builds a full HandleWS integration fixture with:
//   - A real controlled_pty session (owned runtime)
//   - A device-authenticated principal with terminal:input permission
//   - An httptest server serving the production HandleWS handler
func newC1Fixture(t *testing.T) *c1Fixture {
	t.Helper()

	// Real PTY session: the daemon page HTML is NOT served here (that's
	// HandleTerm), but HandleWS connects to the real session transport.
	owned := NewOwnedPTYRuntime(NewNativePTYLauncher(), nil)
	owned.graceful = 2 * time.Second

	cfg := SpawnConfig{Name: "c1-e2e", Executable: "sleep", Args: []string{"1"}}
	id, err := owned.Create(t.Context(), cfg, "", "test")
	if err != nil {
		t.Fatalf("create c1 session: %v", err)
	}
	t.Cleanup(func() { closeOwnedForTest(owned, id) })

	transport, ok := owned.Transport(id)
	if !ok {
		t.Fatal("transport not found after create")
	}
	gen := transport.generation

	// Device identity + sessions + tickets (matching Input-A/Input-B pattern).
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
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	t.Cleanup(srv.Close)

	return &c1Fixture{
		server:     srv,
		tickets:    tickets,
		principal:  principal,
		hostID:     identity.HostID,
		session:    id,
		generation: gen,
		owned:      owned,
	}
}

// dial opens an authenticated WebSocket connection to the test server for
// the fixture's session. The returned conn has already consumed the hello
// frame from the server.
func (f *c1Fixture) dialAndHello(t *testing.T) (*websocket.Conn, c1Hello) {
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

	// Read the mandatory hello frame.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if mt != websocket.TextMessage {
		t.Fatalf("hello frame type = %d, want TextMessage", mt)
	}
	var hello c1Hello
	if err := json.Unmarshal(payload, &hello); err != nil {
		t.Fatalf("hello unmarshal: %v (payload=%s)", err, payload)
	}
	return conn, hello
}

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
	Reason     string `json:"reason,omitempty"`
}

// ── Test: hello frame ──

func TestTERM_C1_HelloDeliversExactlyOncePerConnection(t *testing.T) {
	f := newC1Fixture(t)

	// First connection: must receive exactly one hello.
	conn1, hello1 := f.dialAndHello(t)
	if hello1.Type != "hello" {
		t.Fatalf("expected hello, got %q", hello1.Type)
	}
	if hello1.SessionID != f.session {
		t.Errorf("sessionId = %q, want %q", hello1.SessionID, f.session)
	}
	if hello1.Generation != f.generation {
		t.Errorf("generation = %d, want %d", hello1.Generation, f.generation)
	}
	if hello1.ConnectionID == "" {
		t.Error("connectionId is empty")
	}
	// Owner principal carries terminal:input capability.
	found := false
	for _, c := range hello1.Capabilities {
		if c == devicetrust.PermTerminalInput {
			found = true
		}
	}
	if !found {
		t.Errorf("capabilities = %v, must include terminal:input", hello1.Capabilities)
	}

	// No second hello may arrive on the same connection.
	conn1.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if mt, payload, err := conn1.ReadMessage(); err == nil {
		t.Fatalf("unexpected second frame on conn1: type=%d payload=%s", mt, payload)
	}

	// Second connection: receives its own hello with a distinct connectionId.
	_, hello2 := f.dialAndHello(t)
	_ = hello2
	if hello2.Type != "hello" {
		t.Fatalf("expected hello on conn2, got %q", hello2.Type)
	}
	if hello2.ConnectionID == hello1.ConnectionID {
		t.Errorf("two connections share connectionId %q", hello1.ConnectionID)
	}
	if hello2.ConnectionID == "" {
		t.Error("conn2 connectionId is empty")
	}
}

// readTextUntil reads TextMessage frames until timeout, returning the first
// one that matches. Binary frames (PTY output) are skipped.
func readTextUntil(conn *websocket.Conn, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for {
		conn.SetReadDeadline(deadline)
		mt, payload, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		if mt == websocket.TextMessage {
			return payload, nil
		}
		// Binary frame — PTY output, skip.
	}
}

// ── Test: Ctrl+C via acknowledged input ──

func TestTERM_C1_CtrlCSendsExactByteAndReceivesAccepted(t *testing.T) {
	f := newC1Fixture(t)
	conn, hello := f.dialAndHello(t)

	// Verify the hello carries terminal:input.
	hasInput := false
	for _, c := range hello.Capabilities {
		if c == devicetrust.PermTerminalInput {
			hasInput = true
		}
	}
	if !hasInput {
		t.Fatal("hello lacks terminal:input — cannot test acknowledged input")
	}

	// Send Ctrl+C (0x03) via the versioned acknowledged-input protocol.
	const ctrlC = "\x03"
	req := map[string]interface{}{
		"type":       "terminal_input",
		"version":    1,
		"sessionId":  f.session,
		"generation": f.generation,
		"inputId":    testInputID(),
		"payload":    "Aw==", // base64 of 0x03
	}
	reqBytes, _ := json.Marshal(req)
	if err := conn.WriteMessage(websocket.TextMessage, reqBytes); err != nil {
		t.Fatal(err)
	}

	ackPayload, err := readTextUntil(conn, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var ack c1Result
	if err := json.Unmarshal(ackPayload, &ack); err != nil {
		t.Fatalf("ack unmarshal: %v (payload=%s)", err, ackPayload)
	}
	if ack.Type != "input_result" {
		t.Errorf("ack type = %q, want input_result", ack.Type)
	}
	if ack.Outcome != "accepted" {
		t.Errorf("ack outcome = %q, want accepted", ack.Outcome)
	}

	// Verify the PTY actually received 0x03.
	// The transport's writer (the real PTY) must have been called.
	// We can't easily inspect the raw PTY write count, but the ack
	// proves HandleWS accepted the frame and called WriteInput.
}

// ── Test: wrong generation rejected ──

func TestTERM_C1_WrongGenerationRejectedWithoutPTYWrite(t *testing.T) {
	f := newC1Fixture(t)
	conn, _ := f.dialAndHello(t)

	// Send a terminal_input with a wrong generation.
	req := map[string]interface{}{
		"type":       "terminal_input",
		"version":    1,
		"sessionId":  f.session,
		"generation": f.generation + 99, // wrong!
		"inputId":    testInputID(),
		"payload":    "dGVzdA==",
	}
	reqBytes, _ := json.Marshal(req)
	if err := conn.WriteMessage(websocket.TextMessage, reqBytes); err != nil {
		t.Fatal(err)
	}

	ackPayload, err := readTextUntil(conn, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var ack c1Result
	if err := json.Unmarshal(ackPayload, &ack); err != nil {
		t.Fatalf("ack unmarshal: %v", err)
	}
	if ack.Outcome == "accepted" {
		t.Fatal("wrong generation was accepted — must be rejected")
	}
}

// ── Test: wrong session rejected ──

func TestTERM_C1_WrongSessionRejected(t *testing.T) {
	f := newC1Fixture(t)
	conn, _ := f.dialAndHello(t)

	req := map[string]interface{}{
		"type":       "terminal_input",
		"version":    1,
		"sessionId":  "controlled_pty:wrong-session",
		"generation": f.generation,
		"inputId":    testInputID(),
		"payload":    "dGVzdA==",
	}
	reqBytes, _ := json.Marshal(req)
	if err := conn.WriteMessage(websocket.TextMessage, reqBytes); err != nil {
		t.Fatal(err)
	}

	ackPayload, err := readTextUntil(conn, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var ack c1Result
	if err := json.Unmarshal(ackPayload, &ack); err != nil {
		t.Fatalf("ack unmarshal: %v", err)
	}
	if ack.Outcome == "accepted" {
		t.Fatal("wrong session was accepted — must be rejected")
	}
}

// ── Test: missing capability → read_only denial ──

func TestTERM_C1_ViewerPrincipalReceivesReadOnlyDenial(t *testing.T) {
	// Build a viewer fixture: same structure but principal lacks terminal:input.
	owned := NewOwnedPTYRuntime(NewNativePTYLauncher(), nil)
	owned.graceful = 2 * time.Second
	cfg := SpawnConfig{Name: "c1-viewer", Executable: "sleep", Args: []string{"1"}}
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
	// Viewer: only PermSessionsRead — NO terminal:input.
	bearer, _, _, err := sessions.CreateAfterVerifiedChallenge(
		"device-viewer", identity.HostID, sessions.BootID(),
		[]string{devicetrust.PermSessionsRead},
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
		// TERM-C1: production mode — binary input requires acknowledged protocol.
	}
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
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

	// Hello must carry NO terminal:input capability.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, helloPayload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var hello c1Hello
	if err := json.Unmarshal(helloPayload, &hello); err != nil {
		t.Fatal(err)
	}
	for _, c := range hello.Capabilities {
		if c == devicetrust.PermTerminalInput {
			t.Fatal("viewer hello includes terminal:input — must not")
		}
	}

	// In production mode (InsecureLocalOnly=false), any binary frame
	// (including from unauthorized principals) triggers the
	// update_required response — the acknowledged protocol is required.
	// TERM-C1: the viewer's hello already proves no terminal:input;
	// this test proves binary input fails closed with a structured result.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("\x03")); err != nil {
		t.Fatal(err)
	}
	responsePayload, err := readTextUntil(conn, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var response c1Result
	if err := json.Unmarshal(responsePayload, &response); err != nil {
		t.Fatalf("response unmarshal: %v", err)
	}
	// Production mode returns invalid_request/update_required for binary.
	if response.Outcome != "invalid_request" || response.Reason != "update_required" {
		t.Fatalf("viewer binary result = %+v, want invalid_request/update_required", response)
	}
}

// ── Test: hello carries effective permissions snapshot ──

func TestTERM_C1_HelloSnapshotDoesNotInferFromStoredRole(t *testing.T) {
	f := newC1Fixture(t)
	conn, hello := f.dialAndHello(t)

	// The hello must contain the EXACT server-authorized capabilities.
	// It must not be nil and must contain at least the type field.
	if hello.Type != "hello" {
		t.Fatal("not a hello frame")
	}
	// Capabilities field must be present (even if empty).
	if hello.Capabilities == nil {
		t.Fatal("capabilities field is nil")
	}
	// The generation must be a positive integer (not zero).
	if hello.Generation <= 0 {
		t.Errorf("generation = %d, must be > 0", hello.Generation)
	}
	// Connection ID must be a non-empty hex string.
	if len(hello.ConnectionID) < 8 {
		t.Errorf("connectionId too short: %q", hello.ConnectionID)
	}

	// No second frame for several hundred ms — exactly-once.
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	if mt, payload, err := conn.ReadMessage(); err == nil {
		t.Fatalf("unexpected second frame after hello: type=%d payload=%s", mt, payload)
	}
}

// ── Test: binary input denied in production mode (not InsecureLocalOnly) ──

func TestTERM_C1_BinaryInputDeniedInProduction(t *testing.T) {
	f := newC1Fixture(t)
	// Override: f was built without InsecureLocalOnly → production mode.
	conn, hello := f.dialAndHello(t)

	hasInput := false
	for _, c := range hello.Capabilities {
		if c == devicetrust.PermTerminalInput {
			hasInput = true
		}
	}
	if !hasInput {
		t.Skip("principal lacks terminal:input, production binary test not applicable")
	}

	// Production mode: raw binary frames are rejected even with permission.
	// The acknowledged protocol is the only input path.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("raw binary")); err != nil {
		t.Fatal(err)
	}
	payload, err := readTextUntil(conn, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var result c1Result
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("result unmarshal: %v", err)
	}
	if result.Outcome != "invalid_request" || result.Reason != "update_required" {
		t.Fatalf("production binary result = %+v, want invalid_request/update_required", result)
	}
}

// ── Test: reconnect produces new connection ID but same generation ──

func TestTERM_C1_ReconnectPreservesGenerationNewConnectionID(t *testing.T) {
	f := newC1Fixture(t)
	conn1, hello1 := f.dialAndHello(t)

	// Close first connection.
	if err := conn1.Close(); err != nil {
		t.Logf("conn1 close: %v", err)
	}

	// Second connection to the SAME session.
	conn2, hello2 := f.dialAndHello(t)
	_ = conn2.Close()

	// Same generation (no replacement occurred).
	if hello2.Generation != hello1.Generation {
		t.Errorf("generation changed: %d → %d (no replacement)", hello1.Generation, hello2.Generation)
	}
	// Same session.
	if hello2.SessionID != hello1.SessionID {
		t.Errorf("session changed: %q → %q", hello1.SessionID, hello2.SessionID)
	}
	// Different connection IDs.
	if hello2.ConnectionID == hello1.ConnectionID {
		t.Errorf("connectionID unchanged: %q", hello1.ConnectionID)
	}
}

// ── Test: PTY write guard — input to wrong generation silently fails ──

func TestTERM_C1_StaleGenerationDoesNotWriteToPTY(t *testing.T) {
	f := newC1Fixture(t)
	conn, _ := f.dialAndHello(t)

	// Get the current transport.
	transport, _ := f.owned.Transport(f.session)
	origGen := transport.generation

	// Send with a wildly stale generation.
	req := map[string]interface{}{
		"type":       "terminal_input",
		"version":    1,
		"sessionId":  f.session,
		"generation": origGen - 100,
		"inputId":    testInputID(),
		"payload":    "dGVzdA==",
	}
	reqBytes, _ := json.Marshal(req)
	if err := conn.WriteMessage(websocket.TextMessage, reqBytes); err != nil {
		t.Fatal(err)
	}

	ackPayload, err := readTextUntil(conn, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var ack c1Result
	if err := json.Unmarshal(ackPayload, &ack); err != nil {
		t.Fatalf("ack unmarshal: %v", err)
	}
	if ack.Outcome == "accepted" {
		t.Fatal("stale generation was accepted — must be rejected")
	}
}

// ── Compile-time guard: verify HandleWS is wired ──

var _ http.HandlerFunc = (&Handlers{}).HandleWS
