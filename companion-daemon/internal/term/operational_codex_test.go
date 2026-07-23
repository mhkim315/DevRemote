package term

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

type captureOperationalSink struct {
	mu         sync.Mutex
	events     []OperationalEvent
	panicAfter bool
}

func (s *captureOperationalSink) SubmitAfterCommit(event OperationalEvent) {
	s.mu.Lock()
	s.events = append(s.events, event)
	s.mu.Unlock()
	if s.panicAfter {
		panic("timeline sink panic")
	}
}

func (s *captureOperationalSink) snapshot() []OperationalEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]OperationalEvent(nil), s.events...)
}

func waitOperationalKinds(t *testing.T, sink *captureOperationalSink, want ...OperationalEventKind) []OperationalEvent {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events := sink.snapshot()
		seen := make(map[OperationalEventKind]bool, len(events))
		for _, event := range events {
			seen[event.Kind] = true
		}
		complete := true
		for _, kind := range want {
			if !seen[kind] {
				complete = false
				break
			}
		}
		if complete {
			return events
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("operational kinds not observed: %v (events=%+v)", want, sink.snapshot())
	return nil
}

func injectCodexOperationalRaw(t *testing.T, launcher *fakeLauncher, value map[string]any) {
	t.Helper()
	launcher.mu.Lock()
	proc := launcher.procs[len(launcher.procs)-1]
	launcher.mu.Unlock()
	line, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proc.stdoutW.Write(append(line, '\n')); err != nil {
		t.Fatalf("inject provider event: %v", err)
	}
}

func TestManagedCodexOperationalHooksPostCommitAndRedacted(t *testing.T) {
	const sentinel = "CODEX-SECRET-SENTINEL-91f4"
	const threadID = apprTestThread
	const turnID = apprTestTurn
	launcher := &fakeLauncher{handler: func(method string, id float64, params map[string]any, out func(map[string]any)) {
		switch method {
		case "initialize":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		case "thread/start":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"threadId": threadID}})
		case "turn/start":
			out(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"turn": map[string]any{"id": turnID}}})
			out(map[string]any{"jsonrpc": "2.0", "method": "turn/started", "params": map[string]any{
				"threadId": threadID, "turn": map[string]any{"id": turnID},
			}})
		}
	}}
	managed := newTestManagedService(launcher)
	store := NewAuthoritativeApprovalStore()
	if err := managed.SetApprovalStore(store); err != nil {
		t.Fatal(err)
	}
	sink := &captureOperationalSink{panicAfter: true}
	if err := managed.SetOperationalEventSink(sink); err != nil {
		t.Fatal(err)
	}
	sessionID, err := managed.CreateAttached("")
	if err != nil {
		t.Fatal(err)
	}
	if err := managed.SubmitPrompt(sessionID, 1, "prompt-"+sentinel); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, managed.Registry(), sessionID, ManagedStatusWorking)

	injectCodexOperationalRaw(t, launcher, map[string]any{
		"jsonrpc": "2.0", "method": "item/started", "params": map[string]any{
			"threadId": threadID, "turnId": turnID,
			"item": map[string]any{"id": "tool-1", "type": "commandExecution", "command": sentinel},
		},
	})
	launcher.mu.Lock()
	proc := launcher.procs[len(launcher.procs)-1]
	launcher.mu.Unlock()
	if _, err := proc.stdoutW.Write([]byte(certifiedApprovalRaw("7", turnID, "tool-1") + "\n")); err != nil {
		t.Fatal(err)
	}
	injectCodexOperationalRaw(t, launcher, map[string]any{
		"jsonrpc": "2.0", "method": codexResolvedMethod, "params": map[string]any{
			"threadId": threadID, "requestId": 7,
		},
	})
	injectCodexOperationalRaw(t, launcher, map[string]any{
		"jsonrpc": "2.0", "method": "item/completed", "params": map[string]any{
			"threadId": threadID, "turnId": turnID,
			"item": map[string]any{"id": "tool-1", "type": "commandExecution", "output": sentinel},
		},
	})
	injectCodexOperationalRaw(t, launcher, map[string]any{
		"jsonrpc": "2.0", "method": "item/completed", "params": map[string]any{
			"threadId": threadID, "turnId": turnID,
			"item": map[string]any{"id": "message-1", "type": "agentMessage", "text": sentinel},
		},
	})

	waitOperationalKinds(t, sink,
		OperationalProviderInvocationStarted,
		OperationalToolCallStarted,
		OperationalApprovalRequested,
		OperationalApprovalResolved,
		OperationalToolCallFinished,
		OperationalStreamObserved,
	)
	if err := managed.Kill(sessionID, 1); err != nil {
		t.Fatal(err)
	}
	events := waitOperationalKinds(t, sink, OperationalProviderInvocationFinished)
	for _, event := range events {
		if event.Provider != "codex" || event.SessionID != sessionID ||
			event.RuntimeID != "fake-proc" || event.LaunchGeneration != 1 ||
			event.SourceID == "" || event.SourcePosition == "" ||
			event.ReferenceID == "" || event.OccurredAt.IsZero() {
			t.Fatalf("invalid operational identity: %+v", event)
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), sentinel) ||
			strings.Contains(string(encoded), apprSecretCmd) ||
			strings.Contains(string(encoded), apprSecretCWD) ||
			strings.Contains(string(encoded), apprSecretAmendment) {
			t.Fatalf("provider content crossed operational seam: %s", encoded)
		}
	}
}

type panicOperationalSink struct{}

func (panicOperationalSink) SubmitAfterCommit(OperationalEvent) { panic("timeline sink panic") }

func TestManagedCodexOperationalSinkPanicCannotChangeLifecycle(t *testing.T) {
	launcher := &fakeLauncher{handler: happyAppServer("thread-panic")}
	managed := newTestManagedService(launcher)
	if err := managed.SetOperationalEventSink(panicOperationalSink{}); err != nil {
		t.Fatal(err)
	}
	sessionID, err := managed.CreateAttached("")
	if err != nil {
		t.Fatalf("create changed by sink panic: %v", err)
	}
	if _, ok := managed.Registry().Get(sessionID); !ok {
		t.Fatal("registered session missing after sink panic")
	}
	if err := managed.Kill(sessionID, 1); err != nil {
		t.Fatalf("kill changed by sink panic: %v", err)
	}
}
