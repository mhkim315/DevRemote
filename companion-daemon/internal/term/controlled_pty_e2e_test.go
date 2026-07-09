package term

import (
	"context"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/mux"
)

func TestControlledPTY_NoWebSocketCapture(t *testing.T) {
	activity := NewActivityBuffer(100)
	adapter := mux.NewControlledPTYAdapter()
	reg := mux.MustNewRegistry(adapter)

	id, err := adapter.(mux.SessionCreator).CreateSession(context.Background(), mux.CreateOptions{
		Name:    "test-nows",
		Command: "echo 'captured output'",
	})
	if err != nil {
		t.Skipf("PTY creation skipped: %v", err)
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
	rec, ch := EnsureRecorder(compoundID, opener, activity)
	if rec == nil {
		t.Fatal("EnsureRecorder returned nil")
	}
	defer DeleteRecorder(compoundID)

	go func() { for range ch {} }()
	time.Sleep(500 * time.Millisecond)

	events := activity.List(compoundID)
	if len(events) == 0 {
		t.Fatal("no activity captured without WebSocket")
	}
	if !strings.Contains(events[0].Text, "captured output") {
		t.Errorf("captured text=%q", events[0].Text)
	}
}

func TestControlledPTY_InputNoRawText(t *testing.T) {
	activity := NewActivityBuffer(100)
	activity.Append(ActivityEvent{SessionID: "controlled_pty:test-input", Type: ActivityTerminalInput, Text: "", Bytes: 5})
	events := activity.List("controlled_pty:test-input")
	if len(events) == 0 || events[0].Text != "" {
		t.Error("terminal_input Text must be empty")
	}
}

func TestControlledPTY_DeleteCleanup(t *testing.T) {
	activity := NewActivityBuffer(100)
	adapter := mux.NewControlledPTYAdapter()

	id, err := adapter.(mux.SessionCreator).CreateSession(context.Background(), mux.CreateOptions{
		Name:    "test-delete",
		Command: "echo 'before delete'",
	})
	if err != nil {
		t.Skipf("PTY creation skipped: %v", err)
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
	_, ch := EnsureRecorder(compoundID, opener, activity)
	time.Sleep(300 * time.Millisecond)
	for len(ch) > 0 {
		<-ch
	}

	DeleteRecorder(compoundID)
	activity.Clear(compoundID)

	if GetRecorder(compoundID) != nil {
		t.Error("recorder still exists after delete")
	}
	if len(activity.List(compoundID)) != 0 {
		t.Error("activity not cleared after delete")
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
			if v == c { return true }
		}
		return false
	}
	if !has(mux.CapObserve) { t.Error("missing observe") }
	if !has(mux.CapControl) { t.Error("missing control") }
	if !has(mux.CapInput) { t.Error("missing input") }
	if !has(mux.CapLiveTerminal) { t.Error("missing liveTerminal") }
	if !has(mux.CapReliableTranscript) { t.Error("missing reliableTranscript") }
	if has(mux.CapBestEffortTranscript) { t.Error("must NOT have bestEffortTranscript") }
}
