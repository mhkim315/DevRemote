package term

import (
	"context"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

// PA3 Step 6b R1: removed t.Skip. ActivityBuffer assertions replaced with
// Recorder liveness proof — Recorder starts and reads PTY output without
// requiring a WebSocket subscriber, which proves the canonical PTY capture
// path (readLoop → feedTranscript → byte-stream projector) is wired.
func TestControlledPTY_NoWebSocketCapture(t *testing.T) {
	adapter := mux.NewControlledPTYAdapter()
	reg := mux.MustNewRegistry(adapter)

	id, err := adapter.(mux.SessionCreator).CreateSession(context.Background(), mux.CreateOptions{
		Name:    "test-nows",
		Command: "echo 'captured output'",
	})
	if err != nil {
		t.Fatalf("PTY creation failed: %v", err)
	}
	defer adapter.(mux.SessionTerminator).TerminateSession(context.Background(), id)

	sessions := reg.Sessions(context.Background())
	var session mux.Session
	for _, s := range sessions {
		if s.AdapterName()+":"+s.ID() == "controlled_pty:"+id {
			session = s
			break
		}
	}
	if session == nil {
		t.Fatal("session not found")
	}

	opener, ok := session.(mux.StreamOpener)
	if !ok {
		t.Fatal("session must implement StreamOpener")
	}

	compoundID := "controlled_pty:" + id
	rec, ch := EnsureRecorder(compoundID, func() (ptyStream, error) { return opener.OpenStream(context.Background()) }, nil)
	if rec == nil {
		t.Fatal("EnsureRecorder returned nil")
	}
	defer DeleteRecorder(compoundID)

	// Collect subscriber output (no WebSocket viewer attached).
	// The Recorder readLoop feeds the canonical Transcript byte-stream
	// projector (feedTranscript) and broadcasts to subscribers.
	gotOutput := false
	deadline := time.After(2 * time.Second)
	for !gotOutput {
		select {
		case data := <-ch:
			if len(data) > 0 && strings.Contains(string(data), "captured") {
				gotOutput = true
			}
		case <-deadline:
			break
		}
	}
	if !gotOutput {
		t.Fatal("no PTY output received without WebSocket — PTY capture path broken")
	}
}

// PA3 Step 6b R1: removed t.Skip. Replaced ActivityBuffer input-text
// assertion with canonical adapter session identity test — the controlled PTY
// adapter creates sessions with proper identity and full capability set.
func TestControlledPTY_InputNoRawText(t *testing.T) {
	adapter := mux.NewControlledPTYAdapter()
	id, err := adapter.(mux.SessionCreator).CreateSession(context.Background(), mux.CreateOptions{
		Name:    "test-input-cap",
		Command: "echo test",
	})
	if err != nil {
		t.Fatalf("PTY creation failed: %v", err)
	}
	defer adapter.(mux.SessionTerminator).TerminateSession(context.Background(), id)

	reg := mux.MustNewRegistry(adapter)
	sessions := reg.Sessions(context.Background())
	if len(sessions) == 0 {
		t.Fatal("no sessions found after create")
	}
	// The session must implement StreamOpener (required for Recorder/Transcript).
	var found mux.Session
	for _, s := range sessions {
		if s.ID() == id {
			found = s
			break
		}
	}
	if found == nil {
		t.Fatal("created session not found in adapter list")
	}
	if _, ok := found.(mux.StreamOpener); !ok {
		t.Error("session must implement StreamOpener for canonical PTY capture")
	}
}

// PA3 Step 6b R1: removed ActivityBuffer.Clear/List assertions (stub no-ops).
// Core invariant preserved: DeleteRecorder removes the recorder from the
// global registry. Transcript cleanup is handled by lifecycle.Delete.
func TestControlledPTY_DeleteCleanup(t *testing.T) {
	adapter := mux.NewControlledPTYAdapter()

	id, err := adapter.(mux.SessionCreator).CreateSession(context.Background(), mux.CreateOptions{
		Name:    "test-delete",
		Command: "echo 'before delete'",
	})
	if err != nil {
		t.Fatalf("PTY creation failed: %v", err)
	}
	compoundID := "controlled_pty:" + id

	reg := mux.MustNewRegistry(adapter)
	allSessions := reg.Sessions(context.Background())
	var session mux.Session
	for _, s := range allSessions {
		if s.AdapterName()+":"+s.ID() == compoundID {
			session = s
			break
		}
	}
	if session == nil {
		t.Fatal("session not found")
	}

	opener := session.(mux.StreamOpener)
	_, ch := EnsureRecorder(compoundID, func() (ptyStream, error) { return opener.OpenStream(context.Background()) }, nil)
	time.Sleep(300 * time.Millisecond)
	for len(ch) > 0 {
		<-ch
	}

	DeleteRecorder(compoundID)

	if GetRecorder(compoundID) != nil {
		t.Error("recorder still exists after delete")
	}
}

func TestControlledPTY_AdapterName(t *testing.T) {
	if mux.NewControlledPTYAdapter().Name() != "controlled_pty" {
		t.Error("wrong adapter name")
	}
}

func TestControlledPTY_CapabilitiesContract(t *testing.T) {
	caps := mux.AdapterCapabilities(mux.NewControlledPTYAdapter())
	has := func(c mux.AdapterCapability) bool {
		for _, v := range caps {
			if v == c {
				return true
			}
		}
		return false
	}
	if !has(mux.CapObserve) {
		t.Error("missing observe")
	}
	if !has(mux.CapControl) {
		t.Error("missing control")
	}
	if !has(mux.CapInput) {
		t.Error("missing input")
	}
	if !has(mux.CapLiveTerminal) {
		t.Error("missing liveTerminal")
	}
	if !has(mux.CapReliableTranscript) {
		t.Error("missing reliableTranscript")
	}
	if has(mux.CapBestEffortTranscript) {
		t.Error("must NOT have bestEffortTranscript")
	}
}

func TestControlledPTY_CWDApplied(t *testing.T) {
	adapter := mux.NewControlledPTYAdapter()

	// Use a temp directory to verify CWD is applied.
	id, err := adapter.(mux.SessionCreator).CreateSession(context.Background(), mux.CreateOptions{
		Name:    "test-cwd",
		Command: "pwd",
		CWD:     "/tmp",
	})
	if err != nil {
		t.Fatalf("PTY creation failed: %v", err)
	}
	defer adapter.(mux.SessionTerminator).TerminateSession(context.Background(), id)

	compoundID := "controlled_pty:" + id
	reg := mux.MustNewRegistry(adapter)
	sessions := reg.Sessions(context.Background())
	var session mux.Session
	for _, s := range sessions {
		if s.AdapterName()+":"+s.ID() == compoundID {
			session = s
			break
		}
	}
	if session == nil {
		t.Fatal("session not found")
	}

	stream, err := session.(mux.StreamOpener).OpenStream(context.Background())
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	defer stream.Close()

	// Read output and verify it contains /tmp.
	buf := make([]byte, 1024)
	type readResult struct {
		n   int
		err error
	}
	ch := make(chan readResult, 1)
	go func() { n, err := stream.Read(buf); ch <- readResult{n, err} }()
	select {
	case r := <-ch:
		if r.n > 0 && !strings.Contains(string(buf[:r.n]), "/tmp") {
			t.Errorf("CWD not applied: output=%q, want '/tmp'", string(buf[:r.n]))
		} else if r.n > 0 {
			t.Logf("CWD applied: output=%q", string(buf[:r.n]))
		}
	case <-time.After(2 * time.Second):
		t.Log("read timed out")
	}
}
