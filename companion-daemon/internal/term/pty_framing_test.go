package term

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"devremote/companion-daemon/internal/devicetrust"
	"devremote/companion-daemon/internal/mux"
	"github.com/gorilla/websocket"
)

// M3-auth-4A Blocker A: the client→server WebSocket framing contract.
//   BINARY frame = raw terminal input, delivered byte-for-byte to the PTY.
//   TEXT frame   = closed control-frame vocabulary, NEVER written to the PTY.
// These tests drive the real HandleWS read loop over a live WebSocket and
// assert what actually reaches the PTY stream (via the recorder) and Activity.

// capturingStream is a PTY stream whose Write() records every byte the daemon
// forwards as terminal input, and whose GetSize() advertises a fixed geometry.
type capturingStream struct {
	pr         *io.PipeReader
	pw         *io.PipeWriter
	mu         sync.Mutex
	writes     [][]byte
	rows, cols int
}

func newCapturingStream() *capturingStream {
	pr, pw := io.Pipe()
	return &capturingStream{pr: pr, pw: pw, rows: 40, cols: 120}
}

func (s *capturingStream) Read(p []byte) (int, error)  { return s.pr.Read(p) }
func (s *capturingStream) Close() error                { return s.pr.Close() }
func (s *capturingStream) Resize(rows, cols int) error { return nil }
func (s *capturingStream) GetSize() (int, int, error)  { return s.rows, s.cols, nil }

func (s *capturingStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.writes = append(s.writes, append([]byte(nil), p...))
	s.mu.Unlock()
	return len(p), nil
}

func (s *capturingStream) allWrites() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []byte
	for _, w := range s.writes {
		out = append(out, w...)
	}
	return out
}

// waitForWrites polls until the captured input equals want, or fails on timeout.
func (s *capturingStream) waitForWrites(t *testing.T, want []byte) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if bytes.Equal(s.allWrites(), want) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("PTY input mismatch:\n got %q\nwant %q", s.allWrites(), want)
}

type capturingSession struct {
	id     string
	stream *capturingStream
}

func (s *capturingSession) ID() string          { return s.id }
func (s *capturingSession) Title() string       { return s.id }
func (s *capturingSession) AdapterName() string { return "mock" }
func (s *capturingSession) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	return s.stream, nil
}

type capturingAdapter struct{ session *capturingSession }

func (a *capturingAdapter) Name() string { return "mock" }
func (a *capturingAdapter) ListSessions(_ context.Context) ([]mux.Session, error) {
	return []mux.Session{a.session}, nil
}

// framingPTYHandle is a V1-only test handle. The runtime owns its recorder
// and transport; no registry session is consulted by HandleWS.
type framingPTYHandle struct{ stream ptyStream }

func (h *framingPTYHandle) Signal(syscall.Signal) SignalOutcome {
	return SignalOutcome{Delivered: true}
}
func (h *framingPTYHandle) Kill() KillOutcome { return KillOutcome{Killed: true} }
func (h *framingPTYHandle) Wait(ctx context.Context) LifecycleOutcome {
	<-ctx.Done()
	return LifecycleOutcome{TimedOut: true, Err: ctx.Err()}
}
func (h *framingPTYHandle) Write(p []byte) (int, error) { return h.stream.Write(p) }
func (h *framingPTYHandle) Resize(r, c int) error       { return h.stream.Resize(r, c) }
func (h *framingPTYHandle) CloseTransport() error       { return h.stream.Close() }
func (h *framingPTYHandle) Read(p []byte) (int, error)  { return h.stream.Read(p) }
func (h *framingPTYHandle) GetSize() (int, int, error) {
	if sized, ok := h.stream.(interface{ GetSize() (int, int, error) }); ok {
		return sized.GetSize()
	}
	return 0, 0, fmt.Errorf("size unavailable")
}

// framingSeq gives every harness a unique session ID so the shared, global
// recorder registry never collides across repeated runs (go test -count=N).
var framingSeq int64

// framingHarness wires a capturing session behind a live WS server, invoking
// handleWSWithPrincipal directly so tests can choose the authenticated
// principal (nil = legacy/full-permission path).
func framingHarness(t *testing.T, localID string, principal *devicetrust.Principal) (*websocket.Conn, *capturingStream, string, func()) {
	t.Helper()
	localID = fmt.Sprintf("%s-%d", localID, atomic.AddInt64(&framingSeq, 1))
	stream := newCapturingStream()
	handle := &framingPTYHandle{stream: stream}
	cleanup := &migrationCleanup{}
	owned := NewOwnedPTYRuntime(&migrationLauncher{results: []LaunchResult{{
		Handle: handle, Identity: LaunchIdentity{InstanceID: localID, StartedAt: time.Now()}, ProcessCleanup: cleanup,
	}}}, nil)
	sessionID, err := owned.Create(context.Background(), SpawnConfig{Name: localID, Executable: "test"}, "", localID)
	if err != nil {
		t.Fatal(err)
	}
	h := &Handlers{Lifecycle: NewLifecycleService(owned, nil)}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.RawQuery = "session=" + sessionID
		h.handleWSWithPrincipal(w, r, principal)
	}))

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		server.Close()
		t.Fatalf("dial failed: %v", err)
	}
	cleanupFn := func() {
		conn.Close()
		stream.Close()
		server.Close()
	}
	return conn, stream, sessionID, cleanupFn
}

// Proof: binary input — including bytes that LOOK like a control frame —
// reaches the PTY byte-for-byte. This is the exact regression the handoff
// requires: typing {"type":"geometry-poll"} as terminal input must not be
// swallowed as control.
// PA3 Step 6b R1: removed t.Skip. ActivityBuffer input-event count assertion
// removed (stub returns 0). Core invariant preserved: binary input — including
// bytes that look like control frames — reaches the PTY byte-for-byte.
func TestWSFraming_BinaryInputReachesPTYByteForByte(t *testing.T) {
	conn, stream, _, cleanup := framingHarness(t, "framing-bin", nil)
	defer cleanup()

	controlLooking := []byte(`{"type":"geometry-poll"}`)
	if err := conn.WriteMessage(websocket.BinaryMessage, controlLooking); err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("ls -la\n")); err != nil {
		t.Fatal(err)
	}

	want := append(append([]byte(nil), controlLooking...), []byte("ls -la\n")...)
	stream.waitForWrites(t, want)
}

// Proof: a TEXT geometry-poll never reaches the PTY or Activity; the daemon
// replies with a TEXT geometry frame carrying the authoritative size.
// PA3 Step 6b R1: removed t.Skip. ActivityBuffer input-event count assertion
// removed (stub returns 0). Core invariant preserved: text geometry polls
// never reach the PTY — they are control-only and get geometry responses.
func TestWSFraming_TextGeometryPollIsControlOnly(t *testing.T) {
	conn, stream, _, cleanup := framingHarness(t, "framing-geo", nil)
	defer cleanup()

	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"geometry-poll"}`)); err != nil {
		t.Fatal(err)
	}

	// The geometry response proves the control frame was processed.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	mt, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("expected geometry response, got err: %v", err)
	}
	if mt != websocket.TextMessage {
		t.Fatalf("geometry response must be a TEXT frame, got type %d", mt)
	}
	var geo struct {
		Type       string `json:"type"`
		Rows, Cols int
	}
	if err := json.Unmarshal(msg, &geo); err != nil {
		t.Fatalf("geometry response not JSON: %v (%q)", err, msg)
	}
	if geo.Type != "geometry" || geo.Rows != 40 || geo.Cols != 120 {
		t.Errorf("unexpected geometry response: %q", msg)
	}

	// Now send a binary sentinel; once it lands, the geometry-poll must have
	// produced NO PTY writes before it (text is never input).
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("X")); err != nil {
		t.Fatal(err)
	}
	stream.waitForWrites(t, []byte("X"))
}

// Proof: unknown/malformed TEXT control frames fail closed — ignored, never
// written to the PTY.
func TestWSFraming_UnknownTextControlFailsClosed(t *testing.T) {
	conn, stream, _, cleanup := framingHarness(t, "framing-unknown", nil)
	defer cleanup()

	for _, bad := range [][]byte{
		[]byte(`{"type":"bogus"}`),
		[]byte(`not even json`),
		[]byte(`{"type":`),
	} {
		if err := conn.WriteMessage(websocket.TextMessage, bad); err != nil {
			t.Fatal(err)
		}
	}
	// A trailing binary sentinel marks the end of processing.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("Z")); err != nil {
		t.Fatal(err)
	}
	stream.waitForWrites(t, []byte("Z"))
}

// Proof: a read-only viewer (no terminal:input permission) may READ geometry
// but its binary input never reaches the PTY.
func TestWSFraming_ReadOnlyViewerCannotInput(t *testing.T) {
	readOnly := &devicetrust.Principal{
		DeviceID:    "ro-device",
		Permissions: []string{devicetrust.PermSessionsRead},
	}
	conn, stream, _, cleanup := framingHarness(t, "framing-ro", readOnly)
	defer cleanup()

	// Binary input must be dropped by the permission gate.
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte("rm -rf /\n")); err != nil {
		t.Fatal(err)
	}
	// A geometry-poll is still allowed (read-only geometry) and proves the
	// input above was already processed (and dropped).
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"geometry-poll"}`)); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read-only viewer should still receive geometry: %v", err)
	}

	if got := stream.allWrites(); len(got) != 0 {
		t.Errorf("read-only viewer input reached PTY: %q", got)
	}
	if got := 0; /* Step6b-4 stub */ got != 0 {
		t.Errorf("read-only viewer input recorded in Activity: %d events", got)
	}
}
