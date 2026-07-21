package term

import (
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
	wrote []byte
}

func (w *inputBWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	w.wrote = append(w.wrote, p...)
	return len(p), nil
}

// openInputBWS builds the real ticket-authenticated HandleWS path with a
// captured generation-7 transport. Tests exercise the production reader and
// writer goroutines rather than calling WriteInput or ACK helpers directly.
func openInputBWS(t *testing.T, writer *inputBWriter) (*websocket.Conn, int64) {
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
	ticket, _, err := tickets.Issue(principal, identity.HostID, session)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{Lifecycle: NewLifecycleService(owned, nil), WSTickets: tickets, SessionMgr: sessions, HostIdentity: identity}
	srv := httptest.NewServer(http.HandlerFunc(h.HandleWS))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	u.Scheme = "ws"
	u.Path = "/term/ws"
	u.RawQuery = "session=" + url.QueryEscape(session) + "&ticket=" + url.QueryEscape(ticket)
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, generation
}

func TestInputB_HandleWSAcknowledgesExactGenerationAfterWrite(t *testing.T) {
	writer := &inputBWriter{}
	conn, generation := openInputBWS(t, writer)

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

	input := []byte("accepted input")
	if err := conn.WriteMessage(websocket.BinaryMessage, input); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, ackPayload, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var ack struct {
		Type       string `json:"type"`
		Generation int64  `json:"generation"`
		Sequence   uint64 `json:"sequence"`
	}
	if mt != websocket.TextMessage || json.Unmarshal(ackPayload, &ack) != nil || ack.Type != "input_ack" || ack.Generation != generation || ack.Sequence != 1 {
		t.Fatalf("ack type=%d payload=%s decoded=%+v", mt, ackPayload, ack)
	}
	if string(writer.wrote) != string(input) {
		t.Fatalf("WriteInput data=%q, want %q", writer.wrote, input)
	}
}

func TestInputB_HandleWSWriteFailureDoesNotAcknowledge(t *testing.T) {
	writer := &inputBWriter{err: errors.New("write failed")}
	conn, _ := openInputBWS(t, writer)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil { // hello
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("must not ack")); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	for {
		mt, payload, err := conn.ReadMessage()
		if err != nil {
			return // close or deadline: both prove no acceptance ACK was emitted.
		}
		var control struct {
			Type string `json:"type"`
		}
		if mt == websocket.TextMessage && json.Unmarshal(payload, &control) == nil && control.Type == "input_ack" {
			t.Fatalf("write failure emitted acceptance ACK: %s", payload)
		}
	}
}
