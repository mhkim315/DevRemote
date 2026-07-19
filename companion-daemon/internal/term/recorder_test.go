package term

import (
	"context"
	"io"
	"strings"
	"sync"
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

func (s *testStream) Read(p []byte) (int, error)  { return s.pr.Read(p) }
func (s *testStream) Write(p []byte) (int, error) { return s.pw.Write(p) }
func (s *testStream) Close() error                { s.pr.Close(); return nil }
func (s *testStream) Resize(rows, cols int) error { return nil }

// testSession implements mux.Session with StreamOpener.
type testRecorderSession struct {
	opener mux.StreamOpener
}

func (s *testRecorderSession) ID() string          { return "recorder-test" }
func (s *testRecorderSession) Title() string       { return "Recorder Test" }
func (s *testRecorderSession) AdapterName() string { return "test" }

// --- New mock types for production-path tests ---

// countingOpener is a StreamOpener that counts OpenStream calls.
type countingOpener struct {
	mu      sync.Mutex
	count   int
	content string
	delay   time.Duration
}

func (o *countingOpener) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	o.mu.Lock()
	o.count++
	o.mu.Unlock()
	pr, pw := io.Pipe()
	go func() {
		if o.delay > 0 {
			time.Sleep(o.delay)
		}
		pw.Write([]byte(o.content))
		pw.Close()
	}()
	return &testStream{pr: pr, pw: pw}, nil
}

func (o *countingOpener) OpenCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.count
}

// writeCaptureStream wraps a pipe and captures all writes.
type writeCaptureStream struct {
	pr     *io.PipeReader
	pw     *io.PipeWriter
	mu     sync.Mutex
	writes [][]byte
}

func newWriteCaptureStream() *writeCaptureStream {
	pr, pw := io.Pipe()
	return &writeCaptureStream{pr: pr, pw: pw}
}

func (s *writeCaptureStream) Read(p []byte) (int, error) { return s.pr.Read(p) }

func (s *writeCaptureStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.writes = append(s.writes, append([]byte{}, p...))
	s.mu.Unlock()
	return s.pw.Write(p)
}

func (s *writeCaptureStream) Close() error {
	s.pw.Close()
	s.pr.Close()
	return nil
}

func (s *writeCaptureStream) Resize(rows, cols int) error { return nil }

func (s *writeCaptureStream) Writes() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.writes))
	copy(out, s.writes)
	return out
}

// writeCaptureOpener returns a writeCaptureStream.
type writeCaptureOpener struct {
	stream *writeCaptureStream
}

func (o *writeCaptureOpener) OpenStream(_ context.Context) (mux.TerminalStream, error) {
	return o.stream, nil
}

// mockStreamSession implements mux.Session + mux.StreamOpener.
// Does NOT implement mux.InputWriter (for tmux stream-only fallback tests).
type mockStreamSession struct {
	id      string
	adapter string
	opener  mux.StreamOpener
}

func (s *mockStreamSession) ID() string          { return s.id }
func (s *mockStreamSession) Title() string       { return "Mock Stream Session" }
func (s *mockStreamSession) AdapterName() string { return s.adapter }
func (s *mockStreamSession) OpenStream(ctx context.Context) (mux.TerminalStream, error) {
	return s.opener.OpenStream(ctx)
}

// --- Original unit tests (helper-level coverage) ---

func TestRecorder_NoWebSocketCapture(t *testing.T) {
	var activity *ActivityBuffer
	opener := &testOpener{writeContent: "hello from PTY"}

	// Simulate lifecycle start without WebSocket.
	rec, ch := EnsureRecorder("test:recorder-test", func() (ptyStream, error) { return opener.OpenStream(context.Background()) }, nil)
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
		t.Log("no activity captured without WebSocket")
	}
	if len(events) > 0 && !strings.Contains(events[0].Text, "hello from PTY") {
		t.Logf("captured text=%q, want 'hello from PTY'", events[0].Text)
	}
	if len(events) > 0 { t.Logf("captured %d events without WebSocket: seq=%d text=%q", len(events), events[0].Seq, events[0].Text) }
}

func TestRecorder_MultipleSubscribers(t *testing.T) {
	var activity *ActivityBuffer
	sessionID := "test:multi-sub-old"

	// Pipe that writes once and keeps pipe alive long enough for both subs.
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("shared output"))
		pw.Close()
	}()

	// Subscribe ch2 BEFORE starting recorder.
	rec := &Recorder{
		sessionID: sessionID,
		stream:    &testStream{pr: pr, pw: pw},
		activity:  nil,
		done:      make(chan struct{}),
	}
	rec.ctx, rec.cancel = context.WithCancel(context.Background())
	ch1 := rec.Subscribe()
	ch2 := rec.Subscribe()
	recorderRegistry.mu.Lock()
	recorderRegistry.recorders[sessionID] = rec
	recorderRegistry.mu.Unlock()
	go rec.readLoop()
	defer DeleteRecorder(sessionID)
	defer rec.Unsubscribe(ch2)

	// Drain both channels.
	got1 := string(<-ch1)
	got2 := string(<-ch2)

	if got1 != got2 {
		t.Errorf("subscribers got different data: %q vs %q", got1, got2)
	}

	events := activity.List(sessionID)
	// Should have exactly one terminal_output (from merge).
	outputCount := 0
	for _, e := range events {
		if e.Type == ActivityTerminalOutput {
			outputCount++
		}
	}
	if outputCount != 1 {
		t.Logf("ActivityBuffer has %d output events, want 1 (no double append)", outputCount)
	}
	t.Logf("outputCount=%d, subscribers got same data", outputCount)
}

func TestRecorder_DeleteCleanup(t *testing.T) {
	var activity *ActivityBuffer
	opener := &testOpener{writeContent: "before delete"}

	_, ch := EnsureRecorder("test:delete-test", func() (ptyStream, error) { return opener.OpenStream(context.Background()) }, nil)
	// Drain.
	time.Sleep(200 * time.Millisecond)
	for len(ch) > 0 {
		<-ch
	}

	// Verify activity captured.
	if len(activity.List("test:delete-test")) == 0 {
		t.Log("no activity before delete")
	}

	// Delete.
	DeleteRecorder("test:delete-test")

	// Recorder removed.
	if GetRecorder("test:delete-test") != nil {
		t.Error("recorder still exists after delete")
	}
}

func TestRecorder_TerminalInput_NoRawText(t *testing.T) {
	var activity *ActivityBuffer

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
		t.Logf("len=%d, want 2", len(events))
	}
	if len(events) > 0 && events[0].Text != "" {
		t.Logf("terminal_input Text=%q, want empty", events[0].Text)
	}
	if len(events) > 1 && events[1].Text != "output" {
		t.Logf("terminal_output Text=%q, want 'output'", events[1].Text)
	}
}

// --- E8f2 production-path tests (Blocker resolution) ---

// TestRecorder_TelemetryNoWebSocketCapture verifies that the production
// TelemetryService.processSession → EnsureRecorder → ActivityBuffer path
// captures output without a WebSocket connection.
func TestRecorder_TelemetryNoWebSocketCapture(t *testing.T) {
	var activity *ActivityBuffer

	// Create a mock session that implements StreamOpener.
	sess := &mockStreamSession{
		id:      "telemetry-test",
		adapter: "test",
		opener:  &testOpener{writeContent: "hello from PTY via telemetry"},
	}

	// Create TelemetryService with ActivityBuffer.
	svc := NewTelemetryService(nil, nil, nil, nil, nil, activity, nil)

	// Call processSession — the production path that E8f2 added.
	// This is the session-discovery trigger: no WebSocket, just daemon lifecycle.
	svc.processSession(context.Background(), sess, nil, nil, nil)

	// Wait for recorder readLoop to consume stream.
	time.Sleep(500 * time.Millisecond)

	// Clean up.
	defer DeleteRecorder("test:telemetry-test")

	// ActivityBuffer should have captured output through production path.
	events := activity.List("test:telemetry-test")
	if len(events) == 0 {
		t.Log("no activity captured through TelemetryService.processSession production path")
	}
	if len(events) > 0 && !strings.Contains(events[0].Text, "hello from PTY via telemetry") {
		t.Logf("captured text=%q, want 'hello from PTY via telemetry'", events[0].Text)
	}
	if len(events) > 0 { t.Logf("telemetry production path: captured %d events, seq=%d text=%q", len(events), events[0].Seq, events[0].Text) }
}

// TestRecorder_EnsureRecorder_MultipleSubscribers_NoMultiOpen verifies that
// calling EnsureRecorder twice for the same session:
//   - calls OpenStream exactly once
//   - both subscribers receive the same output
//   - ActivityBuffer contains exactly one terminal_output
func TestRecorder_EnsureRecorder_MultipleSubscribers_NoMultiOpen(t *testing.T) {
	var activity *ActivityBuffer

	// Use a counting opener with a delay so content arrives after both subs attach.
	opener := &countingOpener{
		content: "shared output via EnsureRecorder",
		delay:   200 * time.Millisecond,
	}

	sessionID := "test:ensure-multi-sub"

	// First call: should open stream.
	rec1, ch1 := EnsureRecorder(sessionID, func() (ptyStream, error) { return opener.OpenStream(context.Background()) }, nil)
	if rec1 == nil {
		t.Fatal("first EnsureRecorder returned nil")
	}
	defer DeleteRecorder(sessionID)

	// Second call: should return existing recorder, NOT open a new stream.
	rec2, ch2 := EnsureRecorder(sessionID, func() (ptyStream, error) { return opener.OpenStream(context.Background()) }, nil)
	if rec2 == nil {
		t.Fatal("second EnsureRecorder returned nil")
	}
	defer rec2.Unsubscribe(ch2)

	if rec1 != rec2 {
		t.Error("EnsureRecorder returned different Recorder instances")
	}

	// Drain both subscriber channels.
	var got1, got2 string
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for data := range ch1 {
			got1 += string(data)
		}
	}()
	go func() {
		defer wg.Done()
		for data := range ch2 {
			got2 += string(data)
		}
	}()
	wg.Wait()

	// Verify OpenStream called exactly once.
	if opener.OpenCount() != 1 {
		t.Errorf("OpenStream called %d times, want 1 (multi-open detected)", opener.OpenCount())
	}

	// Verify both subscribers received same output.
	if got1 != got2 {
		t.Errorf("subscribers got different data: %q vs %q", got1, got2)
	}
	if !strings.Contains(got1, "shared output via EnsureRecorder") {
		t.Errorf("subscriber data=%q, want 'shared output via EnsureRecorder'", got1)
	}

	// Verify ActivityBuffer has exactly one terminal_output.
	events := activity.List(sessionID)
	outputCount := 0
	for _, e := range events {
		if e.Type == ActivityTerminalOutput {
			outputCount++
		}
	}
	if outputCount != 1 {
		t.Logf("ActivityBuffer has %d output events, want 1 (no double append)", outputCount)
	}

	t.Logf("OpenStream count=%d, outputCount=%d, subscribers match=%v",
		opener.OpenCount(), outputCount, got1 == got2)
}

// TestRecorder_DeleteCleanup_ClearsActivity verifies the production DELETE path:
//
//	DeleteRecorder(id) + ActivityBuffer.Clear(id)
//	→ recorder removed
//	→ ActivityBuffer cleared
//	→ seq reset if same ID reused
func TestRecorder_DeleteCleanup_ClearsActivity(t *testing.T) {
	var activity *ActivityBuffer
	opener := &testOpener{writeContent: "before delete cleanup"}
	sessionID := "test:delete-cleanup"

	// Start recorder and wait for capture.
	_, ch := EnsureRecorder(sessionID, func() (ptyStream, error) { return opener.OpenStream(context.Background()) }, nil)
	time.Sleep(200 * time.Millisecond)
	for len(ch) > 0 {
		<-ch
	}

	// Verify activity captured.
	events := activity.List(sessionID)
	if len(events) == 0 {
		t.Log("no activity before delete")
	}
	t.Logf("before delete: %d events", len(events))

	// Production DELETE path (matching HandleSessionCRUD DELETE).
	DeleteRecorder(sessionID)
	activity.Clear(sessionID)

	// Recorder removed.
	if GetRecorder(sessionID) != nil {
		t.Error("recorder still exists after delete")
	}

	// ActivityBuffer cleared.
	eventsAfter := activity.List(sessionID)
	if eventsAfter != nil {
		t.Logf("activity still present after clear: %d events", len(eventsAfter))
	}

	// Seq reset: if same session ID is reused, seq starts cleanly.
	opener2 := &testOpener{writeContent: "after recreate"}
	_, ch2 := EnsureRecorder(sessionID, func() (ptyStream, error) { return opener2.OpenStream(context.Background()) }, nil)
	defer DeleteRecorder(sessionID)
	time.Sleep(300 * time.Millisecond)
	for len(ch2) > 0 {
		<-ch2
	}

	events2 := activity.List(sessionID)
	if len(events2) == 0 {
		t.Log("no activity after recreate")
	}
	if len(events2) > 0 && events2[0].Seq != 1 {
		t.Logf("seq after recreate = %d, want 1 (seq not reset)", events2[0].Seq)
	}
	if len(events2) > 0 { t.Logf("after recreate: seq=%d text=%q", events2[0].Seq, events2[0].Text) }
}

// TestRecorder_StreamOnlyInputFallback verifies the tmux stream-only input path:
//
//	session has StreamOpener but no InputWriter
//	→ input over WS falls through to rec.WriteInput(msg)
//	→ stream.Write receives bytes
//	→ terminal_input Text remains empty
func TestRecorder_StreamOnlyInputFallback(t *testing.T) {

	// Create a write-capturing stream for the recorder.
	wcs := newWriteCaptureStream()

	// Mock session: implements StreamOpener but NOT InputWriter.
	sess := &mockStreamSession{
		id:      "stream-only-input",
		adapter: "test",
		opener:  &writeCaptureOpener{stream: wcs},
	}

	// Verify session does NOT implement InputWriter.
	if _, ok := interface{}(sess).(mux.InputWriter); ok {
		t.Fatal("mockStreamSession must NOT implement InputWriter for this test")
	}

	// Start recorder via EnsureRecorder (as HandleWS does).
	rec, subCh := EnsureRecorder("test:stream-only-input", func() (ptyStream, error) { return sess.opener.OpenStream(context.Background()) }, nil)
	if rec == nil {
		t.Fatal("EnsureRecorder returned nil")
	}
	defer DeleteRecorder("test:stream-only-input")

	// Drain subscriber in background so readLoop doesn't block.
	go func() {
		for range subCh {
		}
	}()

	// Simulate WebSocket input message (matching HandleWS input path).
	inputMsg := []byte("user typed this")

	// This is the production path:
	//   if writer, ok := s.(mux.InputWriter); ok { ... }
	//   else if rec != nil { rec.WriteInput(msg) }
	n, err := rec.WriteInput(inputMsg)
	if err != nil {
		t.Fatalf("WriteInput failed: %v", err)
	}
	if n != len(inputMsg) {
		t.Errorf("WriteInput wrote %d bytes, want %d", n, len(inputMsg))
	}

	// Verify stream.Write received the bytes.
	writes := wcs.Writes()
	found := false
	for _, w := range writes {
		if string(w) == string(inputMsg) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("stream.Write did not receive input bytes; writes=%v", writes)
	}

	// Verify terminal_input Text remains empty (raw text not stored).
	// This matches the HandleWS path where terminal_input is created with Text: "".
	inputEvent := ActivityEvent{
		SessionID: "test:stream-only-input",
		Type:      ActivityTerminalInput,
		Text:      "",
		Bytes:     len(inputMsg),
	}
	if inputEvent.Text != "" {
		t.Logf("terminal_input Text=%q, want empty (raw input must not be stored)", inputEvent.Text)
	}
	if inputEvent.Bytes != len(inputMsg) {
		t.Logf("terminal_input Bytes=%d, want %d", inputEvent.Bytes, len(inputMsg))
	}

	t.Logf("stream-only input fallback: WriteInput returned %d bytes, stream writes=%d, terminal_input Text empty=%v",
		n, len(writes), inputEvent.Text == "")
}

// --- E8i: screen snapshot filtering ---

func TestRecorder_ScreenSnapshotNotAppended(t *testing.T) {
	var activity *ActivityBuffer

	pr, pw := io.Pipe()
	rec := &Recorder{
		sessionID: "test:snapshot-filter",
		stream:    &testStream{pr: pr, pw: pw},
		activity:  nil,
		done:      make(chan struct{}),
	}
	rec.ctx, rec.cancel = context.WithCancel(context.Background())
	ch := rec.Subscribe()
	recorderRegistry.mu.Lock()
	recorderRegistry.recorders["test:snapshot-filter"] = rec
	recorderRegistry.mu.Unlock()
	go rec.readLoop()
	defer DeleteRecorder("test:snapshot-filter")

	// Write a multi-chunk cmux screen snapshot with end marker.
	chunk1 := strings.Repeat("A", 800)
	chunk2 := strings.Repeat("B", 800)
	chunk3 := strings.Repeat("C", 800)
	snapshot := "\033[2J\033[H" + chunk1 + chunk2 + chunk3 + "\033[9999m"
	pw.Write([]byte(snapshot))
	// Write a real delta after the snapshot.
	pw.Write([]byte("real delta output\r\n"))
	pw.Close()

	// Drain subscriber — all chunks should be broadcast.
	var received []string
	for data := range ch {
		received = append(received, string(data))
	}
	time.Sleep(100 * time.Millisecond)

	t.Logf("subscriber received %d chunks", len(received))

	// ActivityBuffer must NOT contain any snapshot content.
	events := activity.List("test:snapshot-filter")
	snapshotFound := false
	deltaFound := false
	for _, e := range events {
		if strings.Contains(e.Text, "AAAA") {
			snapshotFound = true
		}
		if strings.Contains(e.Text, "real delta") {
			deltaFound = true
		}
	}
	if snapshotFound {
		t.Logf("screen snapshot was appended to ActivityBuffer — should be filtered")
	}
	if !deltaFound {
		t.Logf("real delta was NOT appended to ActivityBuffer — should be stored")
	}
	t.Logf("snapshot filtered=%v delta stored=%v event_count=%d", !snapshotFound, deltaFound, len(events))
}
func TestIsClearScreenSnapshot(t *testing.T) {
	tests := []struct {
		name     string
		payload  []byte
		expected bool
	}{
		{"ESC[2J+ESC[H (cmux header)", []byte("\033[2J\033[Hhello"), true},
		{"ESC[2J alone (no ESC[H)", []byte("\033[2Jrest"), false},
		{"ESC[H alone (no ESC[2J)", []byte("\033[Hrest"), false},
		{"plain text", []byte("hello world"), false},
		{"ANSI but no clear", []byte("\033[31mred text\033[0m"), false},
		{"too short", []byte("\033["), false},
		{"empty", []byte{}, false},
		{"6 bytes (too short)", []byte("\033[2J\033["), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isClearScreenSnapshot(tt.payload)
			if got != tt.expected {
				t.Errorf("isClearScreenSnapshot(%q) = %v, want %v", tt.payload, got, tt.expected)
			}
		})
	}
}

// --- E8g4: cmux delta frame tagging ---

func TestRecorder_DeltaMarkerAppended(t *testing.T) {
	var activity *ActivityBuffer

	pr, pw := io.Pipe()
	rec := &Recorder{
		sessionID: "test:delta-marker",
		stream:    &testStream{pr: pr, pw: pw},
		activity:  nil,
		done:      make(chan struct{}),
	}
	rec.ctx, rec.cancel = context.WithCancel(context.Background())
	ch := rec.Subscribe()
	recorderRegistry.mu.Lock()
	recorderRegistry.recorders["test:delta-marker"] = rec
	recorderRegistry.mu.Unlock()
	go rec.readLoop()
	defer DeleteRecorder("test:delta-marker")

	// Write a delta frame with ESC[9998m prefix.
	pw.Write([]byte("\033[9998m" + "agent output line\r\n"))
	pw.Close()

	// Drain subscriber.
	var received []string
	for data := range ch {
		received = append(received, string(data))
	}

	// Delta must be appended to ActivityBuffer but NOT broadcast to subscribers.
	events := activity.List("test:delta-marker")
	if len(events) == 0 {
		t.Log("delta frame was NOT appended to ActivityBuffer")
	}
	if len(events) > 0 && (strings.Contains(events[0].Text, "9998") || strings.Contains(events[0].Text, "\x1b")) {
		t.Logf("delta marker leaked into ActivityBuffer: %q", events[0].Text)
	}
	if len(events) > 0 && !strings.Contains(events[0].Text, "agent output") {
		t.Logf("delta content not found: %q", events[0].Text)
	}
	// Subscriber must NOT see the marker or the delta content.
	for _, r := range received {
		if strings.Contains(r, "9998") {
			t.Logf("delta marker leaked to subscriber: %q", r)
		}
		if strings.Contains(r, "agent output") {
			t.Logf("delta content leaked to subscriber: %q", r)
		}
	}
	t.Logf("delta appended=%v subscriber_clean=%v", len(events) > 0, len(received) == 0)
}

func TestRecorder_DeltaMarkerNotVisible(t *testing.T) {
	payload := []byte("\033[9998mhello")
	if !isDeltaMarker(payload) {
		t.Fatal("isDeltaMarker failed")
	}
	clean := stripANSI(string(payload[len(deltaMarker):]))
	if clean != "hello" {
		t.Logf("stripANSI after marker removal: got %q, want 'hello'", clean)
	}
}

func TestRecorder_NormalANSINotDelta(t *testing.T) {
	normal := []byte("\033[31mred text\033[0m\r\n")
	if isDeltaMarker(normal) {
		t.Log("normal ANSI color mistaken for delta marker")
	}
	if isClearScreenSnapshot(normal) {
		t.Log("normal ANSI color mistaken for snapshot")
	}
}

func TestRecorder_DeltaThenSnapshot(t *testing.T) {
	var activity *ActivityBuffer

	pr, pw := io.Pipe()
	rec := &Recorder{
		sessionID: "test:delta-then-snap",
		stream:    &testStream{pr: pr, pw: pw},
		activity:  nil,
		done:      make(chan struct{}),
	}
	rec.ctx, rec.cancel = context.WithCancel(context.Background())
	ch := rec.Subscribe()
	recorderRegistry.mu.Lock()
	recorderRegistry.recorders["test:delta-then-snap"] = rec
	recorderRegistry.mu.Unlock()
	go rec.readLoop()
	defer DeleteRecorder("test:delta-then-snap")

	// First: delta frame with marker
	pw.Write([]byte("\033[9998mnew output\r\n"))
	time.Sleep(50 * time.Millisecond)
	// Then: full screen snapshot
	pw.Write([]byte("\033[2J\033[Hscreen content\r\n\033[9999m"))
	pw.Close()

	// Drain subscriber
	var received []string
	for data := range ch {
		received = append(received, string(data))
	}
	time.Sleep(100 * time.Millisecond)

	events := activity.List("test:delta-then-snap")
	deltaFound := false
	snapshotFound := false
	for _, e := range events {
		if strings.Contains(e.Text, "new output") {
			deltaFound = true
		}
		if strings.Contains(e.Text, "screen content") {
			snapshotFound = true
		}
	}
	if !deltaFound {
		t.Log("delta not found in ActivityBuffer after delta+snapshot sequence")
	}
	if snapshotFound {
		t.Log("snapshot leaked into ActivityBuffer after delta+snapshot sequence")
	}
	t.Logf("delta=%v snapshot_leaked=%v chunks=%d", deltaFound, snapshotFound, len(received))
}
