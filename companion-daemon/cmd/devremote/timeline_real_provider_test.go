package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
)

func TestTimelineRealManagedCodexPathRedactsEveryOutput(t *testing.T) {
	logs := captureTimelineLogs(t)
	path := filepath.Join(t.TempDir(), "codex-timeline.jsonl")
	launcher := &sp1FakeLauncher{}
	service := term.NewManagedCodexServiceForTest(launcher, func() error { return nil })
	app := newRealTimelineProviderApp(t, Config{
		InsecureLocalOnly: true, EnableManagedCodex: true,
		EnableTimelineShadow: true, TimelineShadowPath: path, EnableCockpit: true,
	}, func(deps *Dependencies) {
		deps.Managed = service
	})

	sessionID, err := service.CreateAttached("")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SubmitPrompt(sessionID, 1, "prompt-"+sp1SecretCmd); err != nil {
		t.Fatal(err)
	}
	waitForTimelineKinds(t, app.timelineWriter,
		contract.EventProviderInvocationStarted,
		contract.EventApprovalRequested,
	)
	if err := service.Kill(sessionID, 1); err != nil {
		t.Fatal(err)
	}
	waitForTimelineKinds(t, app.timelineWriter, contract.EventProviderInvocationFinished)
	assertRealTimelineOutputsRedacted(t, app, path, logs,
		[]string{sp1SecretCmd, sp1SecretCWD},
		contract.EventProviderInvocationStarted,
		contract.EventApprovalRequested,
		contract.EventProviderInvocationFinished,
	)
}

func TestTimelineRealManagedClaudePathRedactsEveryOutput(t *testing.T) {
	const sentinel = "CLAUDE-REAL-TIMELINE-SECRET-4e2a"
	logs := captureTimelineLogs(t)
	path := filepath.Join(t.TempDir(), "claude-timeline.jsonl")
	launcher := &compFakeLauncher{resumeWCh: make(chan struct{})}
	service := term.NewManagedClaudeService(
		term.ClaudeEntryConfig{
			Bin: "claude", Version: "2.1.209", AuthorityVersion: "2.1.209",
			PinnedPath:   "/tmp/fake-claude",
			PinnedDigest: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		launcher, &compFakeAttestor{},
	)
	app := newRealTimelineProviderApp(t, Config{
		InsecureLocalOnly: true, EnableManagedClaude: true,
		EnableTimelineShadow: true, TimelineShadowPath: path, EnableCockpit: true,
	}, func(deps *Dependencies) {
		deps.ManagedClaude = service
	})

	sessionID, err := service.CreateDetached("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	const claudeSessionID = "claude-real-timeline-session"
	const toolUseID = "claude-real-timeline-tool"
	inputJSON := fmt.Sprintf(
		`{"command":"echo pokitclaudeapprovalprobe","description":"%s"}`, sentinel,
	)
	fireInitialHook(t, captureInitialHookURL(t, launcher),
		claudeSessionID, toolUseID, "Bash", inputJSON)
	deferred := fmt.Sprintf(
		`{"type":"result","stop_reason":"tool_deferred","session_id":"%s","deferred_tool_use":{"id":"%s","name":"Bash","input":%s}}`,
		claudeSessionID, toolUseID, inputJSON,
	)
	assistant := fmt.Sprintf(
		`{"type":"assistant","message":{"content":[{"type":"text","text":"%s"}]}}`,
		sentinel,
	)
	launcher.mu.Lock()
	process := launcher.procs[0]
	launcher.mu.Unlock()
	if _, err := process.StdoutW.Write([]byte(deferred + "\n" + assistant + "\n")); err != nil {
		t.Fatal(err)
	}
	waitForTimelineKinds(t, app.timelineWriter,
		contract.EventProviderInvocationStarted,
		contract.EventToolCallStarted,
		contract.EventApprovalRequested,
		contract.EventStreamObserved,
	)
	if err := service.Stop(sessionID, 1); err != nil {
		t.Fatal(err)
	}
	waitForTimelineKinds(t, app.timelineWriter, contract.EventProviderInvocationFinished)
	assertRealTimelineOutputsRedacted(t, app, path, logs,
		[]string{sentinel},
		contract.EventProviderInvocationStarted,
		contract.EventToolCallStarted,
		contract.EventApprovalRequested,
		contract.EventStreamObserved,
		contract.EventProviderInvocationFinished,
	)
}

func newRealTimelineProviderApp(
	t *testing.T,
	config Config,
	mutate func(*Dependencies),
) *App {
	t.Helper()
	deps := testDeps()
	deps.OpenTimelineShadow = func(config writer.Config, auth writer.ProducerAuth) (*writer.Writer, error) {
		return writer.Open(config, auth)
	}
	mutate(&deps)
	app, err := NewAppWithDeps(config, deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})
	return app
}

func captureTimelineLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	previous := log.Writer()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &logs
}

func waitForTimelineKinds(t *testing.T, timeline *writer.Writer, want ...contract.EventKind) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		seen := make(map[contract.EventKind]bool)
		for _, event := range timeline.ReadRecent(128) {
			seen[event.EventKind] = true
		}
		complete := true
		for _, kind := range want {
			if !seen[kind] {
				complete = false
				break
			}
		}
		if complete {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Timeline kinds not observed: %v (recent=%+v)", want, timeline.ReadRecent(128))
}

func assertRealTimelineOutputsRedacted(
	t *testing.T,
	app *App,
	path string,
	logs *bytes.Buffer,
	secrets []string,
	want ...contract.EventKind,
) {
	t.Helper()
	waitForTimelineKinds(t, app.timelineWriter, want...)
	jsonl, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := json.Marshal(app.timelineWriter.ReadRecent(128))
	if err != nil {
		t.Fatal(err)
	}
	app.cockpitStore.Refresh()
	cockpitJSON, err := json.Marshal(app.cockpitStore.ReadAll())
	if err != nil {
		t.Fatal(err)
	}
	for surface, raw := range map[string][]byte{
		"jsonl": jsonl, "ring": ring, "cockpit": cockpitJSON, "log": logs.Bytes(),
	} {
		for _, secret := range secrets {
			if bytes.Contains(raw, []byte(secret)) {
				t.Fatalf("%s leaked provider secret %q: %s", surface, secret, raw)
			}
		}
	}
}
