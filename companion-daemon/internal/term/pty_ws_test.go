package term

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"github.com/gorilla/websocket"
)

type mockStream struct {
	pr *io.PipeReader
	pw *io.PipeWriter
}

func (s *mockStream) Read(p []byte) (n int, err error) {
	return s.pr.Read(p)
}

func (s *mockStream) Write(p []byte) (n int, err error) {
	return 0, fmt.Errorf("write not implemented")
}

func (s *mockStream) Close() error {
	return s.pr.Close()
}

func (s *mockStream) Resize(rows, cols int) error {
	return nil
}

type mockSession struct {
	stream *mockStream
}

func (s *mockSession) ID() string                                                 { return "test" }
func (s *mockSession) Title() string                                              { return "test" }
func (s *mockSession) AdapterName() string                                        { return "mock" }
func (s *mockSession) ReadScreen(ctx context.Context) ([]byte, error)             { return nil, nil }
func (s *mockSession) ReadHistory(ctx context.Context, lines int) ([]byte, error) { return nil, nil }
func (s *mockSession) OpenStream(ctx context.Context) (mux.TerminalStream, error) {
	return s.stream, nil
}

type mockAdapter struct {
	session *mockSession
}

func (a *mockAdapter) Name() string                              { return "mock" }
func (a *mockAdapter) GetSession(id string) (mux.Session, error) { return a.session, nil }

func (a *mockAdapter) ListSessions(ctx context.Context) ([]mux.Session, error) {
	return []mux.Session{a.session}, nil
}
func (a *mockAdapter) CreateSession(ctx context.Context, opts mux.CreateOptions) (string, error) {
	return "", nil
}
func (a *mockAdapter) TerminateSession(ctx context.Context, id string) error { return nil }

func TestHandleWS_CloseCode1011(t *testing.T) {
	t.Parallel()

	reg := mux.MustNewRegistry()
	pr, pw := io.Pipe()
	mockSess := &mockSession{
		stream: &mockStream{pr: pr, pw: pw},
	}
	adapter := &mockAdapter{session: mockSess}
	reg.Register(adapter)

	h := &Handlers{Registry: reg}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.RawQuery = "session=mock:test"
		// Skip auth for test
		h.HandleWS(w, r)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	// Simulate stream error by closing the pipe writer with an error
	simulatedErr := fmt.Errorf("stream failed")
	pw.CloseWithError(simulatedErr)

	// Attempt to read from WS, should get a close error
	_, _, err = conn.ReadMessage()
	if err == nil {
		t.Fatalf("expected error reading from ws, got nil")
	}

	closeErr, ok := err.(*websocket.CloseError)
	if !ok {
		t.Fatalf("expected websocket.CloseError, got %T: %v", err, err)
	}

	if closeErr.Code != 1011 {
		t.Errorf("expected close code 1011, got %d", closeErr.Code)
	}
	if !strings.Contains(closeErr.Text, "stream failed") {
		t.Errorf("expected close text to contain 'stream failed', got %q", closeErr.Text)
	}
}

func TestHandleWS_ClientDisconnectWhileProducingOutput(t *testing.T) {
	t.Parallel()

	reg := mux.MustNewRegistry()
	pr, pw := io.Pipe()
	mockSess := &mockSession{
		stream: &mockStream{pr: pr, pw: pw},
	}
	adapter := &mockAdapter{session: mockSess}
	reg.Register(adapter)

	h := &Handlers{Registry: reg}

	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(handlerDone)
		r.URL.RawQuery = "session=mock:test"
		h.HandleWS(w, r)
	}))
	defer server.Close()

	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}

	producerDone := make(chan struct{})
	go func() {
		defer close(producerDone)
		payload := []byte("continuous output")
		for i := 0; i < 1000; i++ {
			if _, err := pw.Write(payload); err != nil {
				return
			}
		}
	}()

	if err := conn.Close(); err != nil {
		t.Fatalf("client close failed: %v", err)
	}

	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("HandleWS did not return after client disconnect")
	}

	pw.Close()
	select {
	case <-producerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("output producer remained blocked after disconnect")
	}
}
