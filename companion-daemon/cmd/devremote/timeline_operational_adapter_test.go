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
	codexRegistry := term.NewManagedSessionRegistry(4)
	for _, record := range []term.ManagedSessionRecord{
		{SessionID: "codex_app_server:privacy", Provider: "codex", ProcessID: "runtime-privacy", Epoch: 1},
		{SessionID: "codex_app_server:privacy-two", Provider: "codex", ProcessID: "runtime-privacy", Epoch: 2},
	} {
		if err := codexRegistry.Register(record); err != nil {
			t.Fatal(err)
		}
	}
	adapter := newTimelineOperationalAdapter(
		producers, newManagedOperationalRuntimeVerifier(codexRegistry, nil),
	)

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
	sender := adapter.BindOperationalRuntime(term.OperationalRuntimeIdentity{
		Provider: base.Provider, SessionID: base.SessionID,
		RuntimeID: base.RuntimeID, LaunchGeneration: base.LaunchGeneration,
	})
	if sender == nil {
		t.Fatal("runtime sender was not bound")
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
		sender.SubmitAfterCommit(event)
	}

	deadline := time.Now().Add(5 * time.Second)
	for timelineWriter.Stats().Appended != uint64(len(kinds)) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := timelineWriter.Stats(); got.Appended != uint64(len(kinds)) || got.Dropped != 0 || got.Failures != 0 {
		t.Fatalf("Timeline stats = %+v, want all seven events appended", got)
	}

	late := base
	late.Kind = term.OperationalToolCallStarted
	late.SourcePosition = "late-" + sentinel
	sender.SubmitAfterCommit(late)
	time.Sleep(10 * time.Millisecond)
	if got := timelineWriter.Stats().Appended; got != uint64(len(kinds)) {
		t.Fatalf("event appended after producer revoke: appended=%d", got)
	}

	// Revocation is an exit invariant, not contingent on successfully
	// constructing the optional finish envelope.
	second := base
	second.SessionID = "codex_app_server:privacy-two"
	second.LaunchGeneration = 2
	second.Kind = term.OperationalProviderInvocationStarted
	secondSender := adapter.BindOperationalRuntime(term.OperationalRuntimeIdentity{
		Provider: second.Provider, SessionID: second.SessionID,
		RuntimeID: second.RuntimeID, LaunchGeneration: second.LaunchGeneration,
	})
	if secondSender == nil {
		t.Fatal("second runtime sender was not bound")
	}
	secondSender.SubmitAfterCommit(second)
	deadline = time.Now().Add(5 * time.Second)
	for timelineWriter.Stats().Appended != uint64(len(kinds)+1) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	malformedFinish := second
	malformedFinish.Kind = term.OperationalProviderInvocationFinished
	malformedFinish.OccurredAt = time.Time{}
	secondSender.SubmitAfterCommit(malformedFinish)
	late = second
	late.Kind = term.OperationalToolCallStarted
	secondSender.SubmitAfterCommit(late)
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

func TestTimelineOperationalAdapterRequiresExactRegisteredRuntimeTuple(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline.jsonl")
	producers := writer.NewProducerStore()
	timelineWriter, err := writer.Open(writer.Config{Path: path}, producers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { timelineWriter.Close() })
	registry := term.NewManagedSessionRegistry(1)
	const sessionID = "codex_app_server:exact"
	if err := registry.Register(term.ManagedSessionRecord{
		SessionID: sessionID, Provider: "codex", ProcessID: "runtime-exact", Epoch: 7,
	}); err != nil {
		t.Fatal(err)
	}
	adapter := newTimelineOperationalAdapter(
		producers, newManagedOperationalRuntimeVerifier(registry, nil),
	)
	base := term.OperationalEvent{
		Kind:     term.OperationalProviderInvocationStarted,
		Provider: "codex", SessionID: sessionID, RuntimeID: "runtime-exact", LaunchGeneration: 7,
		SourceID: "source", SourcePosition: "position", ReferenceID: "reference",
		OccurredAt: time.Now().UTC(),
	}
	for name, mutate := range map[string]func(*term.OperationalRuntimeIdentity){
		"provider":   func(identity *term.OperationalRuntimeIdentity) { identity.Provider = "claude" },
		"runtime":    func(identity *term.OperationalRuntimeIdentity) { identity.RuntimeID = "runtime-other" },
		"generation": func(identity *term.OperationalRuntimeIdentity) { identity.LaunchGeneration++ },
	} {
		t.Run(name, func(t *testing.T) {
			identity := term.OperationalRuntimeIdentity{
				Provider: base.Provider, SessionID: base.SessionID,
				RuntimeID: base.RuntimeID, LaunchGeneration: base.LaunchGeneration,
			}
			mutate(&identity)
			if sender := adapter.BindOperationalRuntime(identity); sender != nil {
				t.Fatalf("mismatched %s tuple created sender", name)
			}
			if got := timelineWriter.Stats(); got.Appended != 0 || got.Dropped != 0 {
				t.Fatalf("mismatched %s tuple reached writer: %+v", name, got)
			}
		})
	}
	sender := adapter.BindOperationalRuntime(term.OperationalRuntimeIdentity{
		Provider: base.Provider, SessionID: base.SessionID,
		RuntimeID: base.RuntimeID, LaunchGeneration: base.LaunchGeneration,
	})
	if sender == nil {
		t.Fatal("exact tuple did not bind sender")
	}
	sender.SubmitAfterCommit(base)
	deadline := time.Now().Add(time.Second)
	for timelineWriter.Stats().Appended != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := timelineWriter.Stats(); got.Appended != 1 || got.Dropped != 0 {
		t.Fatalf("exact tuple did not bind: %+v", got)
	}
}

func TestTimelineOperationalRuntimeSenderCannotClaimOrRevokeAnotherRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline.jsonl")
	producers := writer.NewProducerStore()
	timelineWriter, err := writer.Open(writer.Config{Path: path}, producers)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { timelineWriter.Close() })
	registry := term.NewManagedSessionRegistry(2)
	for _, record := range []term.ManagedSessionRecord{
		{SessionID: "codex_app_server:sender-a", Provider: "codex", ProcessID: "runtime-a", Epoch: 1},
		{SessionID: "codex_app_server:sender-b", Provider: "codex", ProcessID: "runtime-b", Epoch: 2},
	} {
		if err := registry.Register(record); err != nil {
			t.Fatal(err)
		}
	}
	adapter := newTimelineOperationalAdapter(
		producers, newManagedOperationalRuntimeVerifier(registry, nil),
	)
	identityA := term.OperationalRuntimeIdentity{
		Provider: "codex", SessionID: "codex_app_server:sender-a", RuntimeID: "runtime-a", LaunchGeneration: 1,
	}
	identityB := term.OperationalRuntimeIdentity{
		Provider: "codex", SessionID: "codex_app_server:sender-b", RuntimeID: "runtime-b", LaunchGeneration: 2,
	}
	senderA := adapter.BindOperationalRuntime(identityA)
	senderB := adapter.BindOperationalRuntime(identityB)
	if senderA == nil || senderB == nil {
		t.Fatal("runtime senders were not bound")
	}
	now := time.Now().UTC()
	eventA := term.OperationalEvent{
		Kind: term.OperationalProviderInvocationStarted, Provider: identityA.Provider,
		SessionID: identityA.SessionID, RuntimeID: identityA.RuntimeID, LaunchGeneration: identityA.LaunchGeneration,
		SourceID: "sender-a", SourcePosition: "start", ReferenceID: "sender-a", OccurredAt: now,
	}
	eventB := term.OperationalEvent{
		Kind: term.OperationalProviderInvocationStarted, Provider: identityB.Provider,
		SessionID: identityB.SessionID, RuntimeID: identityB.RuntimeID, LaunchGeneration: identityB.LaunchGeneration,
		SourceID: "sender-b", SourcePosition: "start", ReferenceID: "sender-b", OccurredAt: now,
	}
	senderA.SubmitAfterCommit(eventA)
	senderB.SubmitAfterCommit(eventB)
	deadline := time.Now().Add(time.Second)
	for timelineWriter.Stats().Appended != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := timelineWriter.Stats().Appended; got != 2 {
		t.Fatalf("initial runtime observations = %d, want 2", got)
	}

	// Sender A presents B's tuple first to submit, then to finish/revoke. Both
	// must be rejected before reaching B's capability.
	stolen := eventB
	stolen.Kind = term.OperationalToolCallStarted
	stolen.SourcePosition = "stolen-submit"
	senderA.SubmitAfterCommit(stolen)
	stolen.Kind = term.OperationalProviderInvocationFinished
	stolen.SourcePosition = "stolen-revoke"
	senderA.SubmitAfterCommit(stolen)
	time.Sleep(10 * time.Millisecond)
	if got := timelineWriter.Stats().Appended; got != 2 {
		t.Fatalf("cross-runtime sender reached Timeline: appended=%d", got)
	}

	// B remains independently authorized: A's forged finish cannot revoke it.
	validB := eventB
	validB.Kind = term.OperationalToolCallStarted
	validB.SourcePosition = "b-still-bound"
	senderB.SubmitAfterCommit(validB)
	deadline = time.Now().Add(time.Second)
	for timelineWriter.Stats().Appended != 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := timelineWriter.Stats().Appended; got != 3 {
		t.Fatalf("sender B was revoked by sender A: appended=%d", got)
	}
}
