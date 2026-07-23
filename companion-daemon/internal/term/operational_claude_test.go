package term

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestManagedClaudeOperationalHooksPostCommitAndRedacted(t *testing.T) {
	const sentinel = "CLAUDE-SECRET-SENTINEL-5c2e"
	launcher := &fakeClaudeLauncher{}
	service := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	if err := service.SetApprovalStore(NewApprovalStore()); err != nil {
		t.Fatal(err)
	}
	sink := &captureOperationalSink{}
	if err := service.SetOperationalEventSink(sink); err != nil {
		t.Fatal(err)
	}
	sessionID, err := service.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	rt := service.runtimes[sessionID]
	service.mu.Unlock()
	if rt == nil {
		t.Fatal("runtime not published")
	}

	claudeSessionID := "claude-session-operational"
	toolUseID := "tool-operational"
	inputJSON := fmt.Sprintf(`{"command":"echo ok","description":"%s"}`, sentinel)
	body := fmt.Sprintf(
		`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":%s,"hook_event_name":"PreToolUse","cwd":"/tmp"}`,
		claudeSessionID, toolUseID, inputJSON,
	)
	postHook(t, rt, body)
	rt.processLine([]byte(deferredStreamJSON(claudeSessionID, toolUseID, "Bash", inputJSON)))
	rt.processLine([]byte(fmt.Sprintf(`{"type":"assistant","message":{"content":"%s"}}`, sentinel)))

	waitOperationalKinds(t, sink,
		OperationalProviderInvocationStarted,
		OperationalToolCallStarted,
		OperationalApprovalRequested,
		OperationalStreamObserved,
	)
	if err := service.Stop(sessionID, 1); err != nil {
		t.Fatal(err)
	}
	events := waitOperationalKinds(t, sink, OperationalProviderInvocationFinished)
	for _, event := range events {
		if event.Provider != "claude" || event.SessionID != sessionID ||
			event.RuntimeID != "fake-claude-proc-1" || event.LaunchGeneration != 1 ||
			event.SourceID == "" || event.SourcePosition == "" ||
			event.ReferenceID == "" || event.OccurredAt.IsZero() {
			t.Fatalf("invalid operational identity: %+v", event)
		}
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), sentinel) {
			t.Fatalf("provider content crossed operational seam: %s", encoded)
		}
	}
}

func TestManagedClaudeOperationalSinkPanicCannotChangeLifecycle(t *testing.T) {
	launcher := &fakeClaudeLauncher{}
	service := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	if err := service.SetOperationalEventSink(panicOperationalSink{}); err != nil {
		t.Fatal(err)
	}
	sessionID, err := service.CreateDetached("/tmp")
	if err != nil {
		t.Fatalf("create changed by sink panic: %v", err)
	}
	if _, ok := service.Registry().Get(sessionID); !ok {
		t.Fatal("registered session missing after sink panic")
	}
	if err := service.Stop(sessionID, 1); err != nil {
		t.Fatalf("stop changed by sink panic: %v", err)
	}
}
