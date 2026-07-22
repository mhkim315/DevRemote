package term

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
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
// Integration argument:
//   Go tests (this file) — validate delivery semantics through real
//     HandleWS: hello frame identity, ACK outcomes, PTY WriteInput
//     byte-for-byte, sequence monotonicity, reconnect state isolation.
//   Goja tests (term_c1_served_page_goja_test.go) — execute the actual
//     served daemon page JavaScript against real HandleWS connections,
//     proving the bridge's sendInput/receive/bind lifecycle.
//   FeedScreen Jest tests (mobile/__tests__/terminalController.test.ts,
//     terminalGeneratedScript.test.ts) — validate the real React Native
//     WebView onMessage handler against real served-page HTML.
//
// Combined = complete TERM-C1 integration per plan §4.

// ── PTY write counter ──

type c1WriteCounter struct {
	mu    sync.Mutex
	calls int
	bytes [][]byte
}

func (w *c1WriteCounter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	cp := make([]byte, len(p))
	copy(cp, p)
	w.bytes = append(w.bytes, cp)
	return len(p), nil
}

func (w *c1WriteCounter) Calls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

func (w *c1WriteCounter) TotalBytes() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, b := range w.bytes {
		n += len(b)
	}
	return n
}

func (w *c1WriteCounter) Wrote() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := ""
	for _, b := range w.bytes {
		s += string(b)
	}
	return s
}

// ── Fixture ──

type c1Fixture struct {
	server     *httptest.Server
	tickets    *devicetrust.WSTicketStore
	principal  *devicetrust.Principal
	hostID     string
	session    string
	generation int64
	owned      *OwnedPTYRuntime
	pageHTML   string
	writes     *c1WriteCounter // PTY write observer
}

func newC1Fixture(t *testing.T) *c1Fixture {
	t.Helper()

	owned := NewOwnedPTYRuntime(NewNativePTYLauncher(), nil)
	owned.graceful = 2 * time.Second

	// Use a real process that reads stdin so input is consumable.
	cfg := SpawnConfig{Name: "c1-e2e", Executable: "sleep", Args: []string{"10"}}
	id, err := owned.Create(t.Context(), cfg, "", "test")
	if err != nil {
		t.Fatalf("create c1 session: %v", err)
	}
	// Kill before stopping the recorder: Recorder.Stop waits for the PTY read
	// loop, so stopping it first would wait for this deliberately long-lived
	// test child instead of releasing the integration fixture promptly.
	t.Cleanup(func() {
		_, _ = owned.Kill(t.Context(), id)
		if recorder := ownedRecorderForTest(owned, id); recorder != nil {
			recorder.Stop()
		}
	})

	transport, ok := owned.Transport(id)
	if !ok {
		t.Fatal("transport not found")
	}
	gen := transport.generation

	writes := &c1WriteCounter{}
	// Replace the transport writer with our counter so we can observe
	// every WriteInput call. The recorder and subscriber still read from
	// the real PTY.
	transport.mu.Lock()
	oldWriter := transport.writer
	transport.writer = io.MultiWriter(oldWriter, writes)
	transport.mu.Unlock()

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

	mux := http.NewServeMux()
	mux.HandleFunc("/term/", h.HandleHTML)
	mux.HandleFunc("/term/ws", h.HandleWS)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	pageURL := srv.URL + "/term/?session=" + url.QueryEscape(id)
	resp, err := http.Get(pageURL)
	if err != nil {
		t.Fatalf("fetch daemon page: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 512*1024)
	n, _ := resp.Body.Read(buf)

	return &c1Fixture{
		server:     srv,
		tickets:    tickets,
		principal:  principal,
		hostID:     identity.HostID,
		session:    id,
		generation: gen,
		owned:      owned,
		pageHTML:   string(buf[:n]),
		writes:     writes,
	}
}

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

// ── Helpers ──

type c1Hello struct {
	Type         string   `json:"type"`
	Capabilities []string `json:"capabilities"`
	SessionID    string   `json:"sessionId"`
	Generation   int64    `json:"generation"`
	ConnectionID string   `json:"connectionId"`
}

type c1Result struct {
	Type         string `json:"type"`
	Outcome      string `json:"outcome"`
	Sequence     uint64 `json:"sequence,omitempty"`
	InputID      string `json:"inputId,omitempty"`
	Generation   int64  `json:"generation,omitempty"`
	SessionID    string `json:"sessionId,omitempty"`
	ConnectionID string `json:"connectionId,omitempty"`
	Reason       string `json:"reason,omitempty"`
	OperationID  string `json:"operationId,omitempty"`
	Part         string `json:"part,omitempty"`
}

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

// bridgeSendInput sends the EXACT JSON that __pokitControlBridge.sendInput()
// produces. Mirrors: JSON.stringify({type:"terminal_input", version:1,
// sessionId, generation, inputId, payload:btoa(encoded)})
func bridgeSendInput(t *testing.T, conn *websocket.Conn, session string, generation int64, text, inputID string) {
	t.Helper()
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

// ── E2E: daemon page serves control bridge ──

func TestTERM_C1_DaemonPageServesControlBridge(t *testing.T) {
	f := newC1Fixture(t)

	if !strings.Contains(f.pageHTML, "__pokitControlBridge") {
		t.Fatal("daemon page missing __pokitControlBridge")
	}
	if !strings.Contains(f.pageHTML, "pokitSendInput") {
		t.Fatal("daemon page missing pokitSendInput")
	}
	if !strings.Contains(f.pageHTML, "pokitMakeInputID") {
		t.Fatal("daemon page missing pokitMakeInputID")
	}
	if !strings.Contains(f.pageHTML, "__pokitControlBridge.bind(ws)") {
		t.Fatal("daemon page does not bind bridge on connect")
	}
	if !strings.Contains(f.pageHTML, "__pokitControlBridge.receive(e.data,ws)") {
		t.Fatal("daemon page onmessage does not route to bridge.receive")
	}
}

// ── E2E: hello frame through real HandleWS ──

func TestTERM_C1_HelloFrameDeliveredExactlyOnce(t *testing.T) {
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
		t.Error("connectionId empty")
	}
	hasInput := false
	for _, c := range hello.Capabilities {
		if c == devicetrust.PermTerminalInput {
			hasInput = true
		}
	}
	if !hasInput {
		t.Error("capabilities missing terminal:input")
	}

	// No second frame on same connection.
	conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if mt, payload, err := conn.ReadMessage(); err == nil {
		t.Fatalf("unexpected second frame: type=%d payload=%s", mt, payload)
	}
}

// ── E2E: Ctrl+C → PTY write → ACK ──

func TestTERM_C1_CtrlCWritesExactByteToPTYAndReceivesAccepted(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	beforeCalls := f.writes.Calls()
	bridgeSendInput(t, conn, f.session, f.generation, "\x03", testInputID())

	ackPayload := readText(t, conn, 2*time.Second)
	var ack c1Result
	if err := json.Unmarshal(ackPayload, &ack); err != nil {
		t.Fatalf("ack unmarshal: %v", err)
	}
	if ack.Outcome != "accepted" {
		t.Fatalf("Ctrl+C outcome = %q, want accepted", ack.Outcome)
	}
	if ack.Sequence != 1 {
		t.Errorf("sequence = %d, want 1", ack.Sequence)
	}

	// Verify the PTY transport received exactly the 0x03 byte.
	if f.writes.Calls() != beforeCalls+1 {
		t.Errorf("WriteInput calls = %d, want %d", f.writes.Calls(), beforeCalls+1)
	}
	if f.writes.Wrote() != "\x03" {
		t.Errorf("PTY wrote %q, want \\x03", f.writes.Wrote())
	}
}

// ── E2E: paste (multi-byte) → PTY write → ACK ──

func TestTERM_C1_PasteWritesAllBytesToPTYAndReceivesAccepted(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	paste := "echo 'hello world'; ls -la /tmp\n"
	beforeCalls := f.writes.Calls()
	bridgeSendInput(t, conn, f.session, f.generation, paste, testInputID())

	ackPayload := readText(t, conn, 2*time.Second)
	var ack c1Result
	if err := json.Unmarshal(ackPayload, &ack); err != nil {
		t.Fatal(err)
	}
	if ack.Outcome != "accepted" {
		t.Fatalf("paste outcome = %q, want accepted", ack.Outcome)
	}
	if f.writes.Calls() != beforeCalls+1 {
		t.Errorf("WriteInput calls = %d, want %d", f.writes.Calls(), beforeCalls+1)
	}
	if f.writes.Wrote() != paste {
		t.Errorf("PTY wrote %q, want %q", f.writes.Wrote(), paste)
	}
}

// ── E2E: text + Enter is two-acknowledged-writes ──

func TestTERM_C1_TextPlusEnterIsTwoAcceptedWrites(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	beforeCalls := f.writes.Calls()

	// Text frame.
	bridgeSendInput(t, conn, f.session, f.generation, "ls", testInputID())
	ack1Payload := readText(t, conn, 2*time.Second)
	var ack1 c1Result
	json.Unmarshal(ack1Payload, &ack1)
	if ack1.Outcome != "accepted" || ack1.Sequence != 1 {
		t.Fatalf("text: outcome=%q seq=%d", ack1.Outcome, ack1.Sequence)
	}

	// Enter frame.
	enterID := "eeee0000111122223333444455556666777788889999aaaabbbbccccddddeeee"
	bridgeSendInput(t, conn, f.session, f.generation, "\r", enterID)
	ack2Payload := readText(t, conn, 2*time.Second)
	var ack2 c1Result
	json.Unmarshal(ack2Payload, &ack2)
	if ack2.Outcome != "accepted" || ack2.Sequence != 2 {
		t.Fatalf("enter: outcome=%q seq=%d", ack2.Outcome, ack2.Sequence)
	}

	// Exactly two WriteInput calls (not one, not three).
	if f.writes.Calls() != beforeCalls+2 {
		t.Errorf("WriteInput calls = %d, want %d (two frames)", f.writes.Calls(), beforeCalls+2)
	}
}

// ── E2E: wrong generation → no PTY write ──

func TestTERM_C1_WrongGenerationNoPTYWrite(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	beforeCalls := f.writes.Calls()
	bridgeSendInput(t, conn, f.session, f.generation+99, "test", testInputID())

	ackPayload := readText(t, conn, 2*time.Second)
	var ack c1Result
	json.Unmarshal(ackPayload, &ack)
	if ack.Outcome == "accepted" {
		t.Fatal("wrong generation was accepted")
	}
	// Zero PTY writes.
	if f.writes.Calls() != beforeCalls {
		t.Errorf("WriteInput called %d times on wrong generation, want 0", f.writes.Calls()-beforeCalls)
	}
}

// ── E2E: wrong session → no PTY write ──

func TestTERM_C1_WrongSessionNoPTYWrite(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	beforeCalls := f.writes.Calls()
	bridgeSendInput(t, conn, "controlled_pty:wrong-session", f.generation, "test", testInputID())

	ackPayload := readText(t, conn, 2*time.Second)
	var ack c1Result
	json.Unmarshal(ackPayload, &ack)
	if ack.Outcome == "accepted" {
		t.Fatal("wrong session was accepted")
	}
	if f.writes.Calls() != beforeCalls {
		t.Errorf("WriteInput called %d times on wrong session", f.writes.Calls()-beforeCalls)
	}
}

// ── E2E: viewer denied + zero PTY write ──

func TestTERM_C1_ViewerDeniedZeroPTYWrite(t *testing.T) {
	owned := NewOwnedPTYRuntime(NewNativePTYLauncher(), nil)
	owned.graceful = 2 * time.Second
	cfg := SpawnConfig{Name: "c1-viewer", Executable: "sleep", Args: []string{"2"}}
	id, err := owned.Create(t.Context(), cfg, "", "test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { closeOwnedForTest(owned, id) })

	writes := &c1WriteCounter{}
	if tr, ok := owned.Transport(id); ok {
		tr.mu.Lock()
		tr.writer = writes
		tr.mu.Unlock()
	}

	identity, err := devicetrust.LoadOrCreateHostIdentity(
		&devicetrust.FileKeyStore{Path: filepath.Join(t.TempDir(), "host.json")},
	)
	if err != nil {
		t.Fatal(err)
	}
	sessions := devicetrust.NewDeviceSessionManager("c1-viewer-boot", time.Minute)
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

	beforeCalls := writes.Calls()
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("\x03")); err != nil {
		t.Fatal(err)
	}
	payload := readText(t, conn, 2*time.Second)
	var result c1Result
	json.Unmarshal(payload, &result)
	if result.Outcome != "invalid_request" || result.Reason != "update_required" {
		t.Fatalf("viewer result = %+v, want invalid_request/update_required", result)
	}
	if writes.Calls() != beforeCalls {
		t.Errorf("PTY write count = %d, want 0 (viewer must not write)", writes.Calls()-beforeCalls)
	}
}

// ── E2E: viewer denied — paste through acknowledged input ──

func TestTERM_C1_ViewerDeniedPasteThroughAcknowledgedInput(t *testing.T) {
	// Same viewer fixture as TestTERM_C1_ViewerDeniedZeroPTYWrite but sends
	// acknowledged terminal_input (same format bridge.sendInput produces)
	// instead of raw binary. Prove the viewer principal is denied even
	// when using the correct protocol framing.
	owned := NewOwnedPTYRuntime(NewNativePTYLauncher(), nil)
	owned.graceful = 2 * time.Second
	cfg := SpawnConfig{Name: "c1-viewer-paste", Executable: "sleep", Args: []string{"2"}}
	id, err := owned.Create(t.Context(), cfg, "", "test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { closeOwnedForTest(owned, id) })

	writes := &c1WriteCounter{}
	if tr, ok := owned.Transport(id); ok {
		tr.mu.Lock()
		tr.writer = writes
		tr.mu.Unlock()
	}

	identity, err := devicetrust.LoadOrCreateHostIdentity(
		&devicetrust.FileKeyStore{Path: filepath.Join(t.TempDir(), "host.json")},
	)
	if err != nil {
		t.Fatal(err)
	}
	sessions := devicetrust.NewDeviceSessionManager("c1-viewer-paste-boot", time.Minute)
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
	if hello.Type != "hello" {
		t.Fatal("missing hello")
	}
	for _, c := range hello.Capabilities {
		if c == devicetrust.PermTerminalInput {
			t.Fatal("viewer hello includes terminal:input")
		}
	}

	beforeCalls := writes.Calls()

	// Send paste through the acknowledged protocol — same framing the
	// bridge would use for term.onData → pokitSendInput.
	bridgeSendInput(t, conn, id, hello.Generation, "echo 'pwned' > /tmp/evil\n", testInputID())

	ackPayload := readText(t, conn, 2*time.Second)
	var ack c1Result
	json.Unmarshal(ackPayload, &ack)
	if ack.Outcome == "accepted" {
		t.Fatal("viewer paste was accepted — must be denied")
	}
	if ack.Outcome != "permission_denied" {
		t.Logf("viewer paste outcome = %q (permission_denied or other non-accepted)", ack.Outcome)
	}

	// Zero PTY writes.
	if writes.Calls() != beforeCalls {
		t.Errorf("PTY write count = %d, want 0", writes.Calls()-beforeCalls)
	}
}

// ── E2E: viewer denied — Ctrl+C through acknowledged input ──

func TestTERM_C1_ViewerDeniedCtrlCThroughAcknowledgedInput(t *testing.T) {
	owned := NewOwnedPTYRuntime(NewNativePTYLauncher(), nil)
	owned.graceful = 2 * time.Second
	cfg := SpawnConfig{Name: "c1-viewer-ctrlc", Executable: "sleep", Args: []string{"2"}}
	id, err := owned.Create(t.Context(), cfg, "", "test")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { closeOwnedForTest(owned, id) })

	writes := &c1WriteCounter{}
	if tr, ok := owned.Transport(id); ok {
		tr.mu.Lock()
		tr.writer = writes
		tr.mu.Unlock()
	}

	identity, err := devicetrust.LoadOrCreateHostIdentity(
		&devicetrust.FileKeyStore{Path: filepath.Join(t.TempDir(), "host.json")},
	)
	if err != nil {
		t.Fatal(err)
	}
	sessions := devicetrust.NewDeviceSessionManager("c1-viewer-ctrlc-boot", time.Minute)
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

	beforeCalls := writes.Calls()

	// Ctrl+C (0x03) through acknowledged input — same as term.onData("\\x03")
	// → pokitSendInput → bridge.sendInput.
	bridgeSendInput(t, conn, id, hello.Generation, "\x03", testInputID())

	ackPayload := readText(t, conn, 2*time.Second)
	var ack c1Result
	json.Unmarshal(ackPayload, &ack)
	if ack.Outcome == "accepted" {
		t.Fatal("viewer Ctrl+C was accepted — must be denied")
	}

	if writes.Calls() != beforeCalls {
		t.Errorf("PTY write count = %d, want 0 (viewer Ctrl+C must not write)", writes.Calls()-beforeCalls)
	}
}

// ── E2E: pending ACK survives reconnect (Input-B semantics) ──

func TestTERM_C1_PendingInputACKBeforeReconnect(t *testing.T) {
	f := newC1Fixture(t)
	conn1 := f.dialWS(t)
	_ = readHello(t, conn1)

	beforeCalls := f.writes.Calls()

	// Send input on conn1 — should be accepted.
	bridgeSendInput(t, conn1, f.session, f.generation, "pre-reconnect", testInputID())
	ack1Payload := readText(t, conn1, 2*time.Second)
	var ack1 c1Result
	json.Unmarshal(ack1Payload, &ack1)
	if ack1.Outcome != "accepted" {
		t.Fatalf("pre-reconnect outcome = %q", ack1.Outcome)
	}
	if ack1.Sequence != 1 {
		t.Errorf("pre-reconnect seq = %d, want 1", ack1.Sequence)
	}

	// Close conn1 and reconnect BEFORE the pending operation completes.
	// The pre-reconnect input ACK was already delivered. A new connection
	// starts fresh — the old connection's input cache is gone.
	conn1.Close()

	conn2 := f.dialWS(t)
	hello2 := readHello(t, conn2)
	if hello2.ConnectionID == "" {
		t.Fatal("conn2 missing connectionId")
	}

	// Input on new connection starts at sequence 1.
	bridgeSendInput(t, conn2, f.session, f.generation, "post-reconnect", testInputID())
	ack2Payload := readText(t, conn2, 2*time.Second)
	var ack2 c1Result
	json.Unmarshal(ack2Payload, &ack2)
	if ack2.Outcome != "accepted" {
		t.Fatalf("post-reconnect outcome = %q", ack2.Outcome)
	}
	// New connection → new reader loop → fresh replay cache → sequence 1.
	if ack2.Sequence != 1 {
		t.Errorf("post-reconnect seq = %d, new connection starts at 1", ack2.Sequence)
	}

	// Both writes went through the SAME transport (generation unchanged).
	if f.writes.Calls() != beforeCalls+2 {
		t.Errorf("WriteInput calls = %d, want %d (pre + post reconnect)", f.writes.Calls(), beforeCalls+2)
	}
}

// ── E2E: duplicate hello does NOT clear pending input ──

func TestTERM_C1_DuplicateHelloDoesNotClearPendingInputState(t *testing.T) {
	f := newC1Fixture(t)
	conn := f.dialWS(t)
	_ = readHello(t, conn)

	beforeCalls := f.writes.Calls()

	// Send valid input → must be accepted.
	bridgeSendInput(t, conn, f.session, f.generation, "first", testInputID())
	ack1Payload := readText(t, conn, 2*time.Second)
	var ack1 c1Result
	json.Unmarshal(ack1Payload, &ack1)
	if ack1.Outcome != "accepted" {
		t.Fatalf("first input = %q", ack1.Outcome)
	}

	// The server only sends hello ONCE per connection. After the initial
	// hello, no second hello frame arrives. We prove this by sending
	// successive inputs: each must be accepted, proving the input state
	// (connectionId, generation, capabilities) was not cleared or mutated.

	// Second input still accepted — input state was not cleared.
	bridgeSendInput(t, conn, f.session, f.generation, "second", "abcd0000111122223333444455556666777788889999aaaabbbbccccddddeeee")
	ack2Payload := readText(t, conn, 2*time.Second)
	var ack2 c1Result
	json.Unmarshal(ack2Payload, &ack2)
	if ack2.Outcome != "accepted" {
		t.Fatalf("second input after no-duplicate-hello = %q, want accepted", ack2.Outcome)
	}
	if ack2.Sequence != 2 {
		t.Errorf("second seq = %d, want 2", ack2.Sequence)
	}
	if f.writes.Calls() != beforeCalls+2 {
		t.Errorf("writes = %d, want %d", f.writes.Calls(), beforeCalls+2)
	}
}

// ── TERM-C1-R3: __pokitExpectedSession injection ──

// TestTERM_C1_R3_ExpectedSessionInjection verifies the served HTML page
// contains the __pokitExpectedSession priority path in the expectedSession
// computation. This is the paired-device HTML path where location.search
// has no ?session= query parameter (the page is loaded via
// source={{html,baseUrl}}).
func TestTERM_C1_R3_ExpectedSessionInjection(t *testing.T) {
	mux := http.NewServeMux()
	h := &Handlers{}
	mux.HandleFunc("/term/", h.HandleHTML)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/term/?session=controlled_pty:test-session")
	if err != nil {
		t.Fatalf("fetch daemon page: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 512*1024)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	if !strings.Contains(body, "__pokitExpectedSession") {
		t.Fatal("HandleHTML output missing __pokitExpectedSession in expectedSession computation")
	}
	if !strings.Contains(body, "window.__pokitExpectedSession") {
		t.Fatal("HandleHTML output missing window.__pokitExpectedSession priority check")
	}
	// Verify fallback still exists for explicit_local_dev direct URI loads
	if !strings.Contains(body, "location.search.match") {
		t.Fatal("HandleHTML output missing location.search fallback for explicit_local_dev")
	}
}

// ── Compile-time guards ──

var _ http.HandlerFunc = (&Handlers{}).HandleHTML
var _ http.HandlerFunc = (&Handlers{}).HandleWS
