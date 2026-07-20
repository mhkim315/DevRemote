package term

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
	"devremote/companion-daemon/internal/transcript"
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
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	opener := &testOpener{writeContent: "hello from PTY"}

	// Simulate lifecycle start without WebSocket.
	rec, ch := EnsureRecorder("test:recorder-test", func() (ptyStream, error) { return opener.OpenStream(context.Background()) })
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

	// Transcript should have captured Recorder output.
	segs := svc.ListTranscript("test:recorder-test")
	if len(segs) == 0 {
		t.Error("no transcript segments captured without WebSocket")
	}
	hasOutput := false
	for _, seg := range segs {
		if strings.Contains(seg.Text, "hello from PTY") {
			hasOutput = true
			break
		}
	}
	if !hasOutput {
		t.Error("expected 'hello from PTY' in transcript segments")
	}
}

func TestRecorder_MultipleSubscribers(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	sessionID := "test:multi-sub-old"

	// Pipe that writes once and keeps pipe alive long enough for both subs.
	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("shared output\n"))
		pw.Close()
	}()

	// Subscribe ch2 BEFORE starting recorder.
	rec := &Recorder{
		sessionID: sessionID,
		stream:    &testStream{pr: pr, pw: pw},
		done:      make(chan struct{}),
	}
	rec.ctx, rec.cancel = context.WithCancel(context.Background())
	rec.transcriptSvc = svc
	rec.queueGen = svc.EnableQueue(sessionID)
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
	if !strings.Contains(got1, "shared output") {
		t.Errorf("subscriber data missing expected content: %q", got1)
	}

	// Ensure queue is drained before checking transcript.
	svc.CloseSessionQueue(sessionID, rec.queueGen)

	// Transcript should have the output (no double append).
	segs := svc.ListTranscript(sessionID)
	outputCount := 0
	for _, seg := range segs {
		if seg.Kind == transcript.KindTerminalOutput {
			outputCount++
		}
	}
	if outputCount == 0 {
		t.Error("no transcript segments after subscriber fan-out")
	}
}

func TestRecorder_DeleteCleanup(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	opener := &testOpener{writeContent: "before delete"}

	_, ch := EnsureRecorder("test:delete-test", func() (ptyStream, error) { return opener.OpenStream(context.Background()) })
	// Drain.
	time.Sleep(200 * time.Millisecond)
	for len(ch) > 0 {
		<-ch
	}

	// Verify transcript captured output before delete.
	segs := svc.ListTranscript("test:delete-test")
	if len(segs) == 0 {
		t.Error("no transcript segments before delete")
	}

	// Delete.
	DeleteRecorder("test:delete-test")

	// Recorder removed.
	if GetRecorder("test:delete-test") != nil {
		t.Error("recorder still exists after delete")
	}
}

func TestRecorder_TerminalInput_NoRawText(t *testing.T) {
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	sid := "test:input-test"

	gen := ts.EnableQueue(sid)

	// Simulate terminal input via BeginInput (echo privacy).
	ts.BeginInput(sid, time.Now())

	// Feed bytes through byte-stream — must be suppressed.
	// Drain queue immediately so the worker processes the suppressed
	// chunk before EndInput clears suppression.
	ts.FeedBytes(sid, []byte("secret inputn"), time.Now(), gen)
	ts.CloseSessionQueue(sid, gen)

	// End input — output resumes.
	ts.EndInput(sid, time.Now())

	// Re-enable queue for visible output with same generation.
	gen2 := ts.EnableQueue(sid)
	ts.FeedBytes(sid, []byte("visible outputn"), time.Now(), gen2)
	ts.CloseSessionQueue(sid, gen2)

	segs := ts.ListTranscript(sid)
	if len(segs) == 0 {
		t.Fatal("no transcript segments after input/output sequence")
	}

	// Verify input boundary is present and content-free.
	hasBoundary := false
	for _, seg := range segs {
		if seg.Kind == transcript.KindInputBoundary {
			hasBoundary = true
			if seg.Text != "" {
				t.Errorf("input boundary carries text: %q", seg.Text)
			}
		}
	}
	if !hasBoundary {
		t.Error("input boundary marker missing")
	}

	// Verify that the suppressed input text ("secret input") did NOT leak.
	for _, seg := range segs {
		if strings.Contains(seg.Text, "secret input") {
			t.Errorf("suppressed input text leaked into transcript: %q", seg.Text)
		}
	}

	// Verify visible output is present.
	hasOutput := false
	for _, seg := range segs {
		if strings.Contains(seg.Text, "visible output") {
			hasOutput = true
		}
	}
	if !hasOutput {
		t.Error("visible output missing from transcript")
	}
}

// --- E8f2 production-path tests (Blocker resolution) ---

// TestRecorder_TelemetryNoWebSocketCapture verifies that the production
// TelemetryService.processSession → EnsureRecorder → Transcript path
// captures output without a WebSocket connection.
func TestRecorder_TelemetryNoWebSocketCapture(t *testing.T) {
	ts := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(ts)
	defer SetTranscriptService(nil)

	// Create a mock session that implements StreamOpener.
	sess := &mockStreamSession{
		id:      "telemetry-test",
		adapter: "test",
		opener:  &testOpener{writeContent: "hello from PTY via telemetry"},
	}

	// Create TelemetryService with Transcript Service.
	svc := NewTelemetryService(nil, nil, nil, nil, ts)

	// Call processSession — the production path that E8f2 added.
	// This is the session-discovery trigger: no WebSocket, just daemon lifecycle.
	svc.processSession(context.Background(), sess, nil, nil, nil)

	// Wait for recorder readLoop to consume stream.
	time.Sleep(500 * time.Millisecond)

	// Clean up.
	defer DeleteRecorder("test:telemetry-test")

	// Transcript should have captured output through production path.
	segs := ts.ListTranscript("test:telemetry-test")
	if len(segs) == 0 {
		t.Error("no transcript segments captured through TelemetryService.processSession production path")
	}
	hasOutput := false
	for _, seg := range segs {
		if strings.Contains(seg.Text, "hello from PTY via telemetry") {
			hasOutput = true
			break
		}
	}
	if !hasOutput {
		t.Error("expected telemetry output in transcript segments")
	}
}

// TestRecorder_EnsureRecorder_MultipleSubscribers_NoMultiOpen verifies that
// calling EnsureRecorder twice for the same session:
//   - calls OpenStream exactly once
//   - both subscribers receive the same output
//   - Transcript contains the output (no double append)
func TestRecorder_EnsureRecorder_MultipleSubscribers_NoMultiOpen(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	// Use a counting opener with a delay so content arrives after both subs attach.
	opener := &countingOpener{
		content: "shared output via EnsureRecorder",
		delay:   200 * time.Millisecond,
	}

	sessionID := "test:ensure-multi-sub"

	// First call: should open stream.
	rec1, ch1 := EnsureRecorder(sessionID, func() (ptyStream, error) { return opener.OpenStream(context.Background()) })
	if rec1 == nil {
		t.Fatal("first EnsureRecorder returned nil")
	}
	defer DeleteRecorder(sessionID)

	// Second call: should return existing recorder, NOT open a new stream.
	rec2, ch2 := EnsureRecorder(sessionID, func() (ptyStream, error) { return opener.OpenStream(context.Background()) })
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

	// Transcript should have the output (no double append).
	segs := svc.ListTranscript(sessionID)
	outputCount := 0
	for _, seg := range segs {
		if seg.Kind == transcript.KindTerminalOutput {
			outputCount++
		}
	}
	if outputCount == 0 {
		t.Error("no transcript segments after multi-subscriber EnsureRecorder")
	}
}

// TestRecorder_DeleteCleanup_ClearsActivity verifies the production DELETE path:
//
//	DeleteRecorder(id) then recreate
//	→ recorder removed
//	→ transcript persists across recreation
//	→ new output appended to existing transcript
func TestRecorder_DeleteCleanup_ClearsActivity(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	opener := &testOpener{writeContent: "before delete cleanup"}
	sessionID := "test:delete-cleanup"

	// Start recorder and wait for capture.
	_, ch := EnsureRecorder(sessionID, func() (ptyStream, error) { return opener.OpenStream(context.Background()) })
	time.Sleep(200 * time.Millisecond)
	for len(ch) > 0 {
		<-ch
	}

	// Verify transcript captured output.
	segsBefore := svc.ListTranscript(sessionID)
	if len(segsBefore) == 0 {
		t.Error("no transcript segments before delete")
	}

	// Production DELETE path (matching HandleSessionCRUD DELETE).
	DeleteRecorder(sessionID)

	// Recorder removed.
	if GetRecorder(sessionID) != nil {
		t.Error("recorder still exists after delete")
	}

	// Transcript should survive deletion (store is independent of recorder).
	segsAfter := svc.ListTranscript(sessionID)
	if len(segsAfter) == 0 {
		t.Error("transcript segments lost after recorder deletion")
	}

	// Re-create: if same session ID is reused, transcript continues.
	opener2 := &testOpener{writeContent: "after recreate"}
	_, ch2 := EnsureRecorder(sessionID, func() (ptyStream, error) { return opener2.OpenStream(context.Background()) })
	defer DeleteRecorder(sessionID)
	time.Sleep(300 * time.Millisecond)
	for len(ch2) > 0 {
		<-ch2
	}

	segsAfterRecreate := svc.ListTranscript(sessionID)
	if len(segsAfterRecreate) == 0 {
		t.Error("no transcript segments after recreate")
	}
	hasBefore := false
	hasAfter := false
	for _, seg := range segsAfterRecreate {
		if strings.Contains(seg.Text, "before delete cleanup") {
			hasBefore = true
		}
		if strings.Contains(seg.Text, "after recreate") {
			hasAfter = true
		}
	}
	if !hasBefore {
		t.Error("transcript lost original segments after recreate")
	}
	if !hasAfter {
		t.Error("transcript missing new segments after recreate")
	}
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
	rec, subCh := EnsureRecorder("test:stream-only-input", func() (ptyStream, error) { return sess.opener.OpenStream(context.Background()) })
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
}

// --- E8i: screen snapshot filtering ---

func TestRecorder_ScreenSnapshotNotAppended(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	pr, pw := io.Pipe()
	rec := &Recorder{
		sessionID: "test:snapshot-filter",
		stream:    &testStream{pr: pr, pw: pw},
		done:      make(chan struct{}),
	}
	rec.ctx, rec.cancel = context.WithCancel(context.Background())
	rec.transcriptSvc = svc
	rec.queueGen = svc.EnableQueue(rec.sessionID)
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

	// Transcript must NOT contain any snapshot content.
	segs := svc.ListTranscript("test:snapshot-filter")
	snapshotFound := false
	deltaFound := false
	for _, seg := range segs {
		if strings.Contains(seg.Text, "AAAA") || strings.Contains(seg.Text, "BBBB") {
			snapshotFound = true
		}
		if strings.Contains(seg.Text, "real delta") {
			deltaFound = true
		}
	}
	if snapshotFound {
		t.Error("screen snapshot leaked into transcript — should be filtered")
	}
	if !deltaFound {
		t.Error("real delta was NOT stored in transcript — should be stored")
	}
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
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	pr, pw := io.Pipe()
	rec := &Recorder{
		sessionID: "test:delta-marker",
		stream:    &testStream{pr: pr, pw: pw},
		done:      make(chan struct{}),
	}
	rec.ctx, rec.cancel = context.WithCancel(context.Background())
	rec.transcriptSvc = svc
	rec.queueGen = svc.EnableQueue(rec.sessionID)
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

	// Delta frames are NOT broadcast to subscribers (marker stripped, continue).
	// In byte_stream capture mode (default), delta is not stored in transcript.
	// Only screen_snapshot_delta capture mode stores via AddSnapshotSegment.
	for _, r := range received {
		if strings.Contains(r, "9998") {
			t.Error("delta marker leaked to subscriber")
		}
		if strings.Contains(r, "agent output") {
			t.Error("delta content leaked to subscriber")
		}
	}

	// Delta content must NOT appear in transcript (byte_stream mode).
	segs := svc.ListTranscript("test:delta-marker")
	for _, seg := range segs {
		if strings.Contains(seg.Text, "agent output") {
			t.Error("delta content leaked into transcript in byte_stream capture mode")
		}
	}
}

func TestRecorder_DeltaMarkerNotVisible(t *testing.T) {
	payload := []byte("\033[9998mhello")
	if !isDeltaMarker(payload) {
		t.Fatal("isDeltaMarker failed")
	}
	clean := stripANSI(string(payload[len(deltaMarker):]))
	if clean != "hello" {
		t.Errorf("stripANSI after marker removal: got %q, want 'hello'", clean)
	}
}

func TestRecorder_NormalANSINotDelta(t *testing.T) {
	normal := []byte("\033[31mred text\033[0m\r\n")
	if isDeltaMarker(normal) {
		t.Error("normal ANSI color mistaken for delta marker")
	}
	if isClearScreenSnapshot(normal) {
		t.Error("normal ANSI color mistaken for snapshot")
	}
}

func TestRecorder_DeltaThenSnapshot(t *testing.T) {
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	pr, pw := io.Pipe()
	rec := &Recorder{
		sessionID: "test:delta-then-snap",
		stream:    &testStream{pr: pr, pw: pw},
		done:      make(chan struct{}),
	}
	rec.ctx, rec.cancel = context.WithCancel(context.Background())
	rec.transcriptSvc = svc
	rec.queueGen = svc.EnableQueue(rec.sessionID)
	ch := rec.Subscribe()
	recorderRegistry.mu.Lock()
	recorderRegistry.recorders["test:delta-then-snap"] = rec
	recorderRegistry.mu.Unlock()
	go rec.readLoop()
	defer DeleteRecorder("test:delta-then-snap")

	// First: delta frame with marker (dropped in byte_stream mode).
	pw.Write([]byte("\033[9998mnew output\r\n"))
	time.Sleep(50 * time.Millisecond)
	// Then: full screen snapshot (drained, not in transcript).
	pw.Write([]byte("\033[2J\033[Hscreen content\r\n\033[9999m"))
	pw.Close()

	// Drain subscriber
	var received []string
	for data := range ch {
		received = append(received, string(data))
	}
	time.Sleep(100 * time.Millisecond)

	// In byte_stream mode: delta is dropped, snapshot is drained.
	// Neither should appear in transcript.
	segs := svc.ListTranscript("test:delta-then-snap")
	for _, seg := range segs {
		if strings.Contains(seg.Text, "new output") {
			t.Error("delta content leaked into transcript (byte_stream mode)")
		}
		if strings.Contains(seg.Text, "screen content") {
			t.Error("snapshot content leaked into transcript")
		}
	}

	// Subscriber must not receive delta content (delta frames are not broadcast).
	for _, r := range received {
		if strings.Contains(r, "new output") {
			t.Error("delta content leaked to subscriber")
		}
	}
}

// PA3 Closeout A — Recorder instance-safe termination.
type barrierStream struct {
	release     chan struct{}
	closeCalled chan struct{}
	releaseOnce sync.Once
	closeOnce   sync.Once
}

func newBarrierStream() *barrierStream {
	return &barrierStream{release: make(chan struct{}), closeCalled: make(chan struct{})}
}

func (s *barrierStream) Read([]byte) (int, error)    { <-s.release; return 0, io.EOF }
func (s *barrierStream) Write(p []byte) (int, error) { return len(p), nil }
func (s *barrierStream) Close() error                { s.closeOnce.Do(func() { close(s.closeCalled) }); return nil }
func (s *barrierStream) Resize(rows, cols int) error { return nil }
func (s *barrierStream) Release()                    { s.releaseOnce.Do(func() { close(s.release) }) }

var closeoutASeq int64 // atomic counter for unique session IDs across -count=N

func TestRecorder_CloseoutA_StaleEOFCannotMarkReplacement(t *testing.T) {
	seq := atomic.AddInt64(&closeoutASeq, 1)
	sid := fmt.Sprintf("test:closeout-stale-eof-%d", seq)

	streamA := newBarrierStream()
	recA, _ := EnsureRecorder(sid, func() (ptyStream, error) { return streamA, nil })
	if recA == nil {
		t.Fatal("EnsureRecorder A returned nil")
	}
	// Ensure the readLoop has been scheduled onto the blocking Read before
	// DeleteRecorder cancels its context. The barrier then keeps it running
	// until the replacement has been installed.
	time.Sleep(time.Millisecond)

	deleteDone := make(chan struct{})
	go func() {
		defer close(deleteDone)
		DeleteRecorder(sid)
	}()

	select {
	case <-streamA.closeCalled:
	case <-time.After(2 * time.Second):
		streamA.Release()
		<-deleteDone
		t.Fatal("DeleteRecorder did not call Close on A")
	}
	if r := GetRecorder(sid); r != nil {
		streamA.Release()
		<-deleteDone
		t.Fatalf("registry has %v after DeleteRecorder removed A, want nil", r)
	}
	select {
	case <-recA.Done():
		streamA.Release()
		<-deleteDone
		t.Fatal("A exited before its read barrier was released")
	default:
	}

	streamB := newBarrierStream()
	recB, _ := EnsureRecorder(sid, func() (ptyStream, error) { return streamB, nil })
	if recB == nil {
		streamA.Release()
		<-deleteDone
		t.Fatal("EnsureRecorder B returned nil")
	}
	defer func() {
		streamB.Release()
		DeleteRecorder(sid)
	}()

	if r := GetRecorder(sid); r != recB {
		t.Fatalf("registry has %v after installing B, want B (%v)", r, recB)
	}

	streamA.Release()
	select {
	case <-deleteDone:
	case <-time.After(2 * time.Second):
		t.Fatal("DeleteRecorder did not finish after releasing A")
	}

	if r := GetRecorder(sid); r != recB {
		t.Fatalf("registry changed to %v after barrier release, want B (%v)", r, recB)
	}
	recorderRegistry.mu.Lock()
	_, termOK := recorderRegistry.terminated[sid]
	recorderRegistry.mu.Unlock()
	if termOK {
		t.Error("terminated flag was set by stale A's EOF")
	}
}

func TestRecorder_CloseoutA_StaleStopCannotAffectReplacement(t *testing.T) {
	seq := atomic.AddInt64(&closeoutASeq, 1)
	sid := fmt.Sprintf("test:closeout-stale-stop-%d", seq)
	streamA := newBarrierStream()
	recA, _ := EnsureRecorder(sid, func() (ptyStream, error) { return streamA, nil })
	if recA == nil {
		t.Fatal("EnsureRecorder A returned nil")
	}

	recA.unregisterSelf()
	if r := GetRecorder(sid); r != nil {
		t.Fatalf("registry has %v after unregistering A, want nil", r)
	}

	streamB := newBarrierStream()
	recB, _ := EnsureRecorder(sid, func() (ptyStream, error) { return streamB, nil })
	if recB == nil {
		streamA.Release()
		t.Fatal("EnsureRecorder B returned nil")
	}
	defer func() {
		streamB.Release()
		DeleteRecorder(sid)
	}()

	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		recA.Stop()
	}()
	select {
	case <-streamA.closeCalled:
	case <-time.After(2 * time.Second):
		streamA.Release()
		<-stopDone
		t.Fatal("stale A.Stop did not call Close")
	}

	if r := GetRecorder(sid); r != recB {
		t.Fatalf("registry has %v during stale A.Stop, want B (%v)", r, recB)
	}
	streamA.Release()
	select {
	case <-stopDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stale A.Stop did not finish after releasing A")
	}

	if r := GetRecorder(sid); r != recB {
		t.Fatalf("registry has %v after stale A.Stop, want B (%v)", r, recB)
	}
	recorderRegistry.mu.Lock()
	_, termOK := recorderRegistry.terminated[sid]
	recorderRegistry.mu.Unlock()
	if termOK {
		t.Error("terminated flag was set by stale A")
	}
}

// TestRecorder_CloseoutA_MatchingRecordsTermination: positive control — the
// CURRENT Recorder must correctly record its own termination.
func TestRecorder_CloseoutA_MatchingRecordsTermination(t *testing.T) {
	seq := atomic.AddInt64(&closeoutASeq, 1)
	sid := fmt.Sprintf("test:closeout-match-%d", seq)
	stream := newBarrierStream()
	close(stream.release)
	rec, _ := EnsureRecorder(sid, func() (ptyStream, error) { return stream, nil })
	if rec == nil {
		t.Fatal("EnsureRecorder returned nil")
	}
	defer DeleteRecorder(sid)

	select {
	case <-rec.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("recorder did not exit")
	}

	recorderRegistry.mu.Lock()
	term := recorderRegistry.terminated[sid]
	recorderRegistry.mu.Unlock()
	if !term {
		t.Error("matching Recorder did not record termination")
	}
}

var closeoutBSeq int64

type pipeStream struct {
	pr *io.PipeReader
	pw *io.PipeWriter
}

func (s *pipeStream) Read(p []byte) (int, error)  { return s.pr.Read(p) }
func (s *pipeStream) Write(p []byte) (int, error) { return s.pw.Write(p) }
func (s *pipeStream) Close() error                { return s.pr.Close() }
func (s *pipeStream) Resize(rows, cols int) error { return nil }

func TestRecorder_CloseoutB_StaleFeedAfterReplace(t *testing.T) {
	seq := atomic.AddInt64(&closeoutBSeq, 1)
	sid := fmt.Sprintf("test:closeoutb-stale-feed-%d", seq)
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	prA, pwA := io.Pipe()
	streamA := &pipeStream{pr: prA, pw: pwA}
	recA, subA := EnsureRecorder(sid, func() (ptyStream, error) { return streamA, nil })
	if recA == nil {
		t.Fatal("EnsureRecorder A returned nil")
	}

	go func() { pwA.Write([]byte("gen1 data\n")) }()
	select {
	case <-subA:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for gen1 data")
	}
	time.Sleep(50 * time.Millisecond)

	segs := svc.ListTranscript(sid)
	hasGen1 := false
	for _, seg := range segs {
		if seg.Text == "gen1 data" {
			hasGen1 = true
		}
	}
	if !hasGen1 {
		t.Error("gen1 data missing")
	}

	svc.ReplaceTranscript(sid)

	go func() { pwA.Write([]byte("stale from A\n")) }()
	select {
	case <-subA:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stale data")
	}

	svc.CloseSessionQueue(sid, 2)
	segs = svc.ListTranscript(sid)
	for _, seg := range segs {
		if seg.Text == "stale from A" {
			t.Error("stale Recorder A data leaked into gen2 transcript")
		}
	}

	pwA.Close()
	DeleteRecorder(sid)

	prB, pwB := io.Pipe()
	streamB := &pipeStream{pr: prB, pw: pwB}
	recB, subB := EnsureRecorder(sid, func() (ptyStream, error) { return streamB, nil })
	if recB == nil {
		t.Fatal("EnsureRecorder B returned nil")
	}
	defer func() { pwB.Close(); DeleteRecorder(sid) }()

	go func() { pwB.Write([]byte("gen2 data\n")) }()
	select {
	case <-subB:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for gen2 data")
	}

	svc.CloseSessionQueue(sid, 2)
	segs = svc.ListTranscript(sid)
	hasGen2 := false
	for _, seg := range segs {
		if seg.Text == "gen2 data" {
			hasGen2 = true
		}
	}
	if !hasGen2 {
		t.Error("gen2 data missing from transcript")
	}
}

func TestRecorder_CloseoutB_StaleStopAfterReplace(t *testing.T) {
	seq := atomic.AddInt64(&closeoutBSeq, 1)
	sid := fmt.Sprintf("test:closeoutb-stale-stop-%d", seq)
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	prA, pwA := io.Pipe()
	streamA := &pipeStream{pr: prA, pw: pwA}
	recA, _ := EnsureRecorder(sid, func() (ptyStream, error) { return streamA, nil })
	if recA == nil {
		t.Fatal("EnsureRecorder A returned nil")
	}

	svc.ReplaceTranscript(sid)

	pwA.Close()
	recA.Stop()

	prB, pwB := io.Pipe()
	streamB := &pipeStream{pr: prB, pw: pwB}
	recB, subB := EnsureRecorder(sid, func() (ptyStream, error) { return streamB, nil })
	if recB == nil {
		t.Fatal("EnsureRecorder B returned nil — stale Stop may have corrupted registry")
	}
	defer func() { pwB.Close(); DeleteRecorder(sid) }()

	go func() { pwB.Write([]byte("after stale stop\n")) }()
	select {
	case <-subB:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Recorder B data")
	}

	svc.CloseSessionQueue(sid, 2)
	segs := svc.ListTranscript(sid)
	hasData := false
	for _, seg := range segs {
		if seg.Text == "after stale stop" {
			hasData = true
		}
	}
	if !hasData {
		t.Error("data missing — stale Stop may have closed replacement queue")
	}
}

func TestRecorder_CloseoutB_QueueGenCapture(t *testing.T) {
	seq := atomic.AddInt64(&closeoutBSeq, 1)
	sid := fmt.Sprintf("test:closeoutb-gen-capture-%d", seq)
	svc := transcript.NewService(transcript.DefaultStoreConfig())
	SetTranscriptService(svc)
	defer SetTranscriptService(nil)

	pr, pw := io.Pipe()
	stream := &pipeStream{pr: pr, pw: pw}
	rec, sub := EnsureRecorder(sid, func() (ptyStream, error) { return stream, nil })
	if rec == nil {
		t.Fatal("EnsureRecorder returned nil")
	}
	defer func() { pw.Close(); DeleteRecorder(sid) }()

	if rec.queueGen != 1 {
		t.Errorf("queueGen = %d, want 1", rec.queueGen)
	}
	if rec.queueGen != svc.GetGeneration(sid) {
		t.Errorf("queueGen=%d != svc.GetGeneration=%d", rec.queueGen, svc.GetGeneration(sid))
	}

	go func() { pw.Write([]byte("capture test\n")) }()
	select {
	case <-sub:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for data")
	}

	pw.Close()
	rec.Stop()

	segs := svc.ListTranscript(sid)
	hasData := false
	for _, seg := range segs {
		if seg.Text == "capture test" {
			hasData = true
		}
	}
	if !hasData {
		t.Error("data missing — Recorder may have passed wrong generation")
	}
}
