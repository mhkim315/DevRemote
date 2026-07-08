package term

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// testOpener is a StreamOpener that returns a fake pipe stream.
type testOpener struct {
	writeContent string
}

func (o *testOpener) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte(o.writeContent))
		pw.Close()
	}()
	return &testStream{pr: pr, pw: pw}, nil
}

type testStream struct {
	pr *io.PipeReader
	pw *io.PipeWriter
}

func (s *testStream) Read(p []byte) (int, error)   { return s.pr.Read(p) }
func (s *testStream) Write(p []byte) (int, error)   { return s.pw.Write(p) }
func (s *testStream) Close() error                  { s.pr.Close(); return nil }
func (s *testStream) Resize(rows, cols int) error   { return nil }

// testSession implements mux.Session with StreamOpener.
type testRecorderSession struct {
	opener mux.StreamOpener
}

func (s *testRecorderSession) ID() string          { return "recorder-test" }
func (s *testRecorderSession) Title() string       { return "Recorder Test" }
func (s *testRecorderSession) AdapterName() string { return "test" }

// --- Tests ---

func TestRecorder_NoWebSocketCapture(t *testing.T) {
	activity := NewActivityBuffer(100)
	opener := &testOpener{writeContent: "hello from PTY"}

	// Simulate lifecycle start without WebSocket.
	rec, ch := EnsureRecorder("test:recorder-test", opener, activity)
	if rec == nil {
		t.Fatal("EnsureRecorder returned nil")
	}
	defer DeleteRecorder("test:recorder-test")

	// Read subscriber output to drain channel.
	go func() {
		for range ch {
		}
	}()
	time.Sleep(500 * time.Millisecond)

	// ActivityBuffer should have captured output.
	events := activity.List("test:recorder-test")
	if len(events) == 0 {
		t.Fatal("no activity captured without WebSocket")
	}
	if !strings.Contains(events[0].Text, "hello from PTY") {
		t.Errorf("captured text=%q, want 'hello from PTY'", events[0].Text)
	}
	t.Logf("captured %d events without WebSocket: seq=%d text=%q", len(events), events[0].Seq, events[0].Text)
}

func TestRecorder_MultipleSubscribers(t *testing.T) {
	activity := NewActivityBuffer(100)

	// Pipe that writes once and keeps pipe alive long enough for both subs.
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("shared output"))
		pw.Close()
	}()

	// Subscribe ch2 BEFORE starting recorder.
	rec := &Recorder{
		sessionID: "test:multi-sub",
		stream:    &testStream{pr: pr, pw: pw},
		activity:  activity,
		done:      make(chan struct{}),
	}
	rec.ctx, rec.cancel = context.WithCancel(context.Background())
	ch1 := rec.Subscribe()
	ch2 := rec.Subscribe()
	recorderRegistry.mu.Lock()
	recorderRegistry.recorders["test:multi-sub"] = rec
	recorderRegistry.mu.Unlock()
	go rec.readLoop()
	defer DeleteRecorder("test:multi-sub")
	defer rec.Unsubscribe(ch2)

	// Drain both channels.
	got1 := string(<-ch1)
	got2 := string(<-ch2)

	if got1 != got2 {
		t.Errorf("subscribers got different data: %q vs %q", got1, got2)
	}

	events := activity.List("test:multi-sub")
	// Should have exactly one terminal_output (from merge).
	outputCount := 0
	for _, e := range events {
		if e.Type == ActivityTerminalOutput {
			outputCount++
		}
	}
	if outputCount != 1 {
		t.Errorf("ActivityBuffer has %d output events, want 1 (no double append)", outputCount)
	}
	t.Logf("outputCount=%d, subscribers got same data", outputCount)
}

func TestRecorder_DeleteCleanup(t *testing.T) {
	activity := NewActivityBuffer(100)
	opener := &testOpener{writeContent: "before delete"}

	_, ch := EnsureRecorder("test:delete-test", opener, activity)
	// Drain.
	time.Sleep(200 * time.Millisecond)
	for len(ch) > 0 {
		<-ch
	}

	// Verify activity captured.
	if len(activity.List("test:delete-test")) == 0 {
		t.Fatal("no activity before delete")
	}

	// Delete.
	DeleteRecorder("test:delete-test")

	// Recorder removed.
	if GetRecorder("test:delete-test") != nil {
		t.Error("recorder still exists after delete")
	}
}

func TestRecorder_TerminalInput_NoRawText(t *testing.T) {
	activity := NewActivityBuffer(100)

	// Append input via buffer (simulating WebSocket path).
	activity.Append(ActivityEvent{
		SessionID: "test:input-test",
		Type:      ActivityTerminalInput,
		Text:      "",
		Bytes:     5,
	})
	activity.Append(ActivityEvent{
		SessionID: "test:input-test",
		Type:      ActivityTerminalOutput,
		Text:      "output",
		Bytes:     6,
	})

	events := activity.List("test:input-test")
	if len(events) != 2 {
		t.Fatalf("len=%d, want 2", len(events))
	}
	if events[0].Text != "" {
		t.Errorf("terminal_input Text=%q, want empty", events[0].Text)
	}
	if events[1].Text != "output" {
		t.Errorf("terminal_output Text=%q, want 'output'", events[1].Text)
	}
}
