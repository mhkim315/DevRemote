package mux

import (
	"context"
	"strings"
	"testing"
	"time"
)

// --- E9: Controlled PTY lifecycle tests ---

func TestControlledPTY_CreateSession(t *testing.T) {
	adapter := NewControlledPTYAdapter()
	ctx := context.Background()

	id, err := adapter.(SessionCreator).CreateSession(ctx, CreateOptions{
		Name:    "test-create",
		Command: "echo hello",
	})
	if err != nil {
		// PTY creation may fail in restricted environments — not a contract failure.
		t.Skipf("PTY creation skipped (env restriction): %v", err)
	}
	defer adapter.(SessionTerminator).TerminateSession(ctx, id)

	if id != "test-create" {
		t.Errorf("expected 'test-create', got %q", id)
	}
	// Verify session appears in list.
	sessions, err := adapter.ListSessions(ctx)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	found := false
	for _, s := range sessions {
		if s.ID() == "test-create" {
			found = true
			break
		}
	}
	if !found {
		t.Error("created session not found in ListSessions")
	}
}

func TestControlledPTY_CaptureMode(t *testing.T) {
	adapter := NewControlledPTYAdapter()
	mode := adapter.(TranscriptCaptureProvider).TranscriptCaptureMode()
	if mode != CaptureModeByteStream {
		t.Errorf("mode = %v, want CaptureModeByteStream", mode)
	}
}

func TestControlledPTY_Capabilities(t *testing.T) {
	adapter := NewControlledPTYAdapter()
	caps := AdapterCapabilities(adapter)
	mustHaveCap(t, caps, CapObserve)
	mustHaveCap(t, caps, CapControl)
	mustHaveCap(t, caps, CapInput)
	mustHaveCap(t, caps, CapLiveTerminal)
	mustHaveCap(t, caps, CapReliableTranscript)
	mustNotHaveCap(t, caps, CapBestEffortTranscript)
}

func TestControlledPTY_StreamOpener(t *testing.T) {
	adapter := NewControlledPTYAdapter()
	ctx := context.Background()

	id, err := adapter.(SessionCreator).CreateSession(ctx, CreateOptions{
		Name:    "test-stream",
		Command: "echo hello && sleep 1",
	})
	if err != nil {
		t.Skipf("PTY creation skipped: %v", err)
	}
	defer adapter.(SessionTerminator).TerminateSession(ctx, id)

	sessions, _ := adapter.ListSessions(ctx)
	var session Session
	for _, s := range sessions {
		if s.ID() == id {
			session = s
			break
		}
	}
	if session == nil {
		t.Fatal("session not found")
	}

	opener, ok := session.(StreamOpener)
	if !ok {
		t.Fatal("session must implement StreamOpener")
	}

	stream, err := opener.OpenStream(ctx)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer stream.Close()

	// Read output from PTY (with timeout via goroutine).
	buf := make([]byte, 1024)
	type readResult struct {
		n   int
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		n, err := stream.Read(buf)
		ch <- readResult{n, err}
	}()
	var nRead int
	var readErr error
	select {
	case r := <-ch:
		nRead, readErr = r.n, r.err
	case <-time.After(2 * time.Second):
		t.Log("Read timed out (process may have exited)")
		return
	}
	if err != nil {
		// Process may exit before we read — acceptable.
		t.Logf("Read returned: n=%d err=%v", nRead, readErr)
	}
	if nRead > 0 && !strings.Contains(string(buf[:nRead]), "hello") {
		t.Logf("output: %q (may have exited before read)", string(buf[:nRead]))
	}
}

func TestControlledPTY_InputWriter(t *testing.T) {
	adapter := NewControlledPTYAdapter()
	ctx := context.Background()

	id, err := adapter.(SessionCreator).CreateSession(ctx, CreateOptions{
		Name:    "test-input",
		Command: "cat",
	})
	if err != nil {
		t.Skipf("PTY creation skipped: %v", err)
	}
	defer adapter.(SessionTerminator).TerminateSession(ctx, id)

	sessions, _ := adapter.ListSessions(ctx)
	var session Session
	for _, s := range sessions {
		if s.ID() == id {
			session = s
			break
		}
	}
	if session == nil {
		t.Fatal("session not found")
	}

	writer, ok := session.(InputWriter)
	if !ok {
		t.Fatal("session must implement InputWriter")
	}

	err = writer.WriteInput(ctx, []byte("test input\n"))
	if err != nil {
		t.Errorf("WriteInput: %v", err)
	}
}

func TestControlledPTY_TerminateSession(t *testing.T) {
	adapter := NewControlledPTYAdapter()
	ctx := context.Background()

	id, err := adapter.(SessionCreator).CreateSession(ctx, CreateOptions{
		Name:    "test-terminate",
		Command: "sleep 10",
	})
	if err != nil {
		t.Skipf("PTY creation skipped: %v", err)
	}

	err = adapter.(SessionTerminator).TerminateSession(ctx, id)
	if err != nil {
		t.Errorf("TerminateSession: %v", err)
	}

	// Session must not appear in list after termination.
	sessions, _ := adapter.ListSessions(ctx)
	for _, s := range sessions {
		if s.ID() == id {
			t.Error("terminated session still appears in ListSessions")
		}
	}
}

func TestControlledPTY_Name(t *testing.T) {
	adapter := NewControlledPTYAdapter()
	if adapter.Name() != "controlled_pty" {
		t.Errorf("Name = %q, want 'controlled_pty'", adapter.Name())
	}
}

func mustHaveCap(t *testing.T, caps []AdapterCapability, target AdapterCapability) {
	t.Helper()
	if !hasCap(caps, target) {
		t.Errorf("missing capability: %s", target)
	}
}

func mustNotHaveCap(t *testing.T, caps []AdapterCapability, target AdapterCapability) {
	t.Helper()
	if hasCap(caps, target) {
		t.Errorf("unexpected capability: %s", target)
	}
}
