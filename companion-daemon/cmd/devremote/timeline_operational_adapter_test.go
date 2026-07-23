package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"devremote/companion-daemon/internal/cockpit"
	"devremote/companion-daemon/internal/term"
	"devremote/companion-daemon/internal/timeline/writer"
)

type compositionClaudeAttestor struct{}

func (compositionClaudeAttestor) Certify(string) error { return nil }

type compositionOperationalSink struct{}

func (compositionOperationalSink) SubmitAfterCommit(term.OperationalEvent) {}

func TestSTEP91CompositionInstallsTimelineSinkIntoManagedRuntimes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline.jsonl")
	codex := term.NewManagedCodexServiceForTest(appV1CodexLauncher{}, func() error { return nil })
	claude := term.NewManagedClaudeServiceForTest(appV1CodexLauncher{}, compositionClaudeAttestor{})
	deps := testDeps()
	deps.Managed = codex
	deps.ManagedClaude = claude
	deps.OpenTimelineShadow = func(config writer.Config, auth writer.ProducerAuth) (*writer.Writer, error) {
		return writer.Open(config, auth)
	}
	app, err := NewAppWithDeps(Config{
		InsecureLocalOnly: true, EnableTimelineShadow: true, TimelineShadowPath: path,
		EnableManagedCodex: true, EnableManagedClaude: true,
	}, deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
	})

	if err := codex.SetOperationalEventSink(compositionOperationalSink{}); err == nil ||
		!strings.Contains(err.Error(), "already configured") {
		t.Fatalf("Codex Timeline sink was not installed by composition: %v", err)
	}
	if err := claude.SetOperationalEventSink(compositionOperationalSink{}); err == nil ||
		!strings.Contains(err.Error(), "already configured") {
		t.Fatalf("Claude Timeline sink was not installed by composition: %v", err)
	}
}

func TestTimelineOperationalAdapterBindsRevokesAndRedactsEveryProjection(t *testing.T) {
	const sentinel = "TIMELINE-SECRET-SENTINEL-a91e"
	path := filepath.Join(t.TempDir(), "timeline.jsonl")
	producers := writer.NewProducerStore()
	timelineWriter, err := writer.Open(writer.Config{Path: path}, producers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { timelineWriter.Close() })
	adapter := newTimelineOperationalAdapter(producers, producers)

	oldLogWriter := log.Writer()
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldLogWriter) })

	now := time.Now().UTC()
	base := term.OperationalEvent{
		Provider: "codex", SessionID: "codex_app_server:privacy",
		RuntimeID: "runtime-privacy", LaunchGeneration: 1,
		SourceID: "source-" + sentinel, SourcePosition: "position-" + sentinel,
		ReferenceID: "reference-" + sentinel, OccurredAt: now,
	}
	kinds := []term.OperationalEventKind{
		term.OperationalProviderInvocationStarted,
		term.OperationalToolCallStarted,
		term.OperationalApprovalRequested,
		term.OperationalApprovalResolved,
		term.OperationalToolCallFinished,
		term.OperationalStreamObserved,
		term.OperationalProviderInvocationFinished,
	}
	for i, kind := range kinds {
		event := base
		event.Kind = kind
		event.SourcePosition += string(rune('a' + i))
		adapter.SubmitAfterCommit(event)
	}

	deadline := time.Now().Add(5 * time.Second)
	for timelineWriter.Stats().Appended != uint64(len(kinds)) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := timelineWriter.Stats(); got.Appended != uint64(len(kinds)) || got.Dropped != 0 || got.Failures != 0 {
		t.Fatalf("Timeline stats = %+v, want all seven events appended", got)
	}

	adapter.mu.Lock()
	remaining := len(adapter.capabilities)
	adapter.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("finished runtime retained %d capabilities", remaining)
	}
	late := base
	late.Kind = term.OperationalToolCallStarted
	late.SourcePosition = "late-" + sentinel
	adapter.SubmitAfterCommit(late)
	time.Sleep(10 * time.Millisecond)
	if got := timelineWriter.Stats().Appended; got != uint64(len(kinds)) {
		t.Fatalf("event appended after producer revoke: appended=%d", got)
	}

	// Revocation is an exit invariant, not contingent on successfully
	// constructing the optional finish envelope.
	second := base
	second.LaunchGeneration = 2
	second.Kind = term.OperationalProviderInvocationStarted
	adapter.SubmitAfterCommit(second)
	deadline = time.Now().Add(5 * time.Second)
	for timelineWriter.Stats().Appended != uint64(len(kinds)+1) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	malformedFinish := second
	malformedFinish.Kind = term.OperationalProviderInvocationFinished
	malformedFinish.OccurredAt = time.Time{}
	adapter.SubmitAfterCommit(malformedFinish)
	late = second
	late.Kind = term.OperationalToolCallStarted
	adapter.SubmitAfterCommit(late)
	time.Sleep(10 * time.Millisecond)
	if got := timelineWriter.Stats().Appended; got != uint64(len(kinds)+1) {
		t.Fatalf("malformed finish failed to revoke producer: appended=%d", got)
	}

	jsonl, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ring, err := json.Marshal(timelineWriter.ReadRecent(128))
	if err != nil {
		t.Fatal(err)
	}
	cockpitStore := cockpit.NewCockpitStore(cockpit.Sources{Timeline: timelineWriter})
	cockpitStore.Refresh()
	cockpitJSON, err := json.Marshal(cockpitStore.ReadAll())
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{
		"jsonl": jsonl, "ring": ring, "cockpit": cockpitJSON, "log": logs.Bytes(),
	} {
		if bytes.Contains(raw, []byte(sentinel)) {
			t.Fatalf("%s leaked provider secret: %s", name, raw)
		}
	}
}
