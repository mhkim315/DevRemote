package term

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagedClaudeOperationalHooksPostCommitAndRedacted(t *testing.T) {
	const sentinel = "CLAUDE-SECRET-SENTINEL-5c2e"
	launcher := &fakeClaudeLauncher{}
	service := NewManagedClaudeService(testCfg(), launcher, &fakeClaudeAttestor{})
	if err := service.SetApprovalStore(NewApprovalStore()); err != nil {
		t.Fatal(err)
	}
	sink := &captureOperationalSink{panicAfter: true}
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
	current, ok := service.Registry().Get(sessionID)
	if !ok {
		t.Fatal("registry record missing")
	}
	if err := service.Stop(sessionID, current.Epoch); err != nil {
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

func TestManagedClaudeOperationalResolvedAndFinishedAreRedactedAndPanicIsolated(t *testing.T) {
	const sentinel = "CLAUDE-RESOLUTION-SECRET-8d11"
	const claudeSessionID = "claude-session-resolution"
	const toolUseID = "tool-resolution"
	inputJSON := fmt.Sprintf(`{"command":"echo pokitclaudeapprovalprobe","description":"%s"}`, sentinel)

	launcher := &multiLaunchLauncher{}
	service, store, delivery, runtimeOf := newInstalledClaudeService(t, launcher)
	sink := &captureOperationalSink{panicAfter: true}
	if err := service.SetOperationalEventSink(sink); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, proc := range launcher.procs {
			_ = proc.Kill()
		}
	})

	sessionID, err := service.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	service.mu.Lock()
	original := service.runtimes[sessionID]
	service.mu.Unlock()
	if original == nil {
		t.Fatal("original runtime not published")
	}
	body := fmt.Sprintf(
		`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":%s,"hook_event_name":"PreToolUse","cwd":"/tmp"}`,
		claudeSessionID, toolUseID, inputJSON,
	)
	postHook(t, original, body)
	original.processLine([]byte(deferredStreamJSON(claudeSessionID, toolUseID, "Bash", inputJSON)))

	original.turnMu.Lock()
	if len(original.activeApprovals) != 1 {
		original.turnMu.Unlock()
		t.Fatalf("active approvals = %d, want 1", len(original.activeApprovals))
	}
	approvalID := original.activeApprovals[0].approvalID
	original.turnMu.Unlock()
	runtime, ok := runtimeOf(sessionID)
	if !ok {
		t.Fatal("live Claude runtime not resolved")
	}
	claim := store.ClaimForExecution(ClaimRequest{
		SessionID: sessionID, ApprovalID: approvalID, OptionID: "allow_once",
		Runtime: runtime,
		Requester: RequesterContext{
			DeviceID: "device", HostID: "host", BearerSessionID: "bearer", BootID: "boot",
			Permissions: []string{claudeRequiredPerm},
		},
		IdempotencyKey: "operational.resolution",
	})
	if claim.Outcome != ClaimGranted {
		t.Fatalf("claim outcome = %s", claim.Outcome)
	}

	managedDelivery, ok := delivery.(*ClaudeManagedApprovalDelivery)
	if !ok {
		t.Fatalf("delivery type = %T", delivery)
	}
	managedDelivery.timeout = 5 * time.Second
	managedDelivery.drainTimeout = 0
	resumeReady := make(chan struct{})
	managedDelivery.barrier = func(stage string) {
		if stage == "post-resume-spawn" {
			close(resumeReady)
		}
	}
	receiptCh := make(chan DeliveryReceipt, 1)
	go func() {
		receiptCh <- managedDelivery.Deliver(ApprovalDeliveryRequest{
			ClaimToken: claim.Token, Binding: claim.Binding, Payload: claim.Payload,
		})
	}()

	select {
	case <-resumeReady:
	case <-time.After(5 * time.Second):
		t.Fatal("resumed runtime not published")
	}
	resumeURL, posttoolURL := operationalClaudeHookURLs(t, launcher.args[len(launcher.args)-1])
	resumeBody := fmt.Sprintf(
		`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":%s,"hook_event_name":"PreToolUse"}`,
		claudeSessionID, toolUseID, inputJSON,
	)
	response := postOperationalClaudeHook(t, resumeURL, resumeBody)
	if string(response) != string(claudeHookResponseBytes("allow")) {
		t.Fatalf("resume response = %s", response)
	}
	postBody := fmt.Sprintf(
		`{"session_id":"%s","tool_use_id":"%s","tool_name":"Bash","tool_input":%s,"tool_response":{"output":"%s"},"hook_event_name":"PostToolUse"}`,
		claudeSessionID, toolUseID, inputJSON, sentinel,
	)
	_ = postOperationalClaudeHook(t, posttoolURL, postBody)

	var receipt DeliveryReceipt
	select {
	case receipt = <-receiptCh:
	case <-time.After(5 * time.Second):
		t.Fatal("delivery did not finish")
	}
	if receipt.Outcome != DeliveryAccepted {
		t.Fatalf("delivery outcome = %s", receipt.Outcome)
	}
	events := waitOperationalKinds(t, sink,
		OperationalProviderInvocationStarted,
		OperationalToolCallStarted,
		OperationalApprovalRequested,
		OperationalApprovalResolved,
		OperationalToolCallFinished,
		OperationalProviderInvocationFinished,
	)
	current, ok := service.Registry().Get(sessionID)
	if !ok {
		t.Fatal("resumed registry record missing")
	}
	if err := service.Stop(sessionID, current.Epoch); err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), sentinel) {
			t.Fatalf("Claude resolution content crossed operational seam: %s", encoded)
		}
	}
}

func operationalClaudeHookURLs(t *testing.T, argv []string) (string, string) {
	t.Helper()
	var settingsPath string
	for i, arg := range argv {
		if arg == "--settings" && i+1 < len(argv) {
			settingsPath = argv[i+1]
			break
		}
	}
	if settingsPath == "" {
		t.Fatalf("resume argv has no settings path: %v", argv)
	}
	readURL := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(filepath.Dir(settingsPath), name))
		if err != nil {
			t.Fatal(err)
		}
		script := strings.TrimSpace(string(raw))
		start, end := strings.IndexByte(script, '\''), strings.LastIndexByte(script, '\'')
		if start < 0 || end <= start {
			t.Fatalf("invalid hook script %s: %s", name, script)
		}
		return script[start+1 : end]
	}
	return readURL("hook_resume.sh"), readURL("hook_posttool.sh")
}

func postOperationalClaudeHook(t *testing.T, url, body string) []byte {
	t.Helper()
	response, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post Claude hook: %v", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read Claude hook response: %v", err)
	}
	return raw
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
