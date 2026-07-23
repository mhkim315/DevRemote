package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"devremote/companion-daemon/internal/agent"
	"devremote/companion-daemon/internal/timeline/contract"
	"devremote/companion-daemon/internal/timeline/writer"
)

func TestSTEP4TimelineShadowIsDefaultOff(t *testing.T) {
	called := false
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, Dependencies{
		OpenTimelineShadow: func(writer.Config, writer.ProducerAuth) (*writer.Writer, error) {
			called = true
			return nil, errors.New("must not open when disabled")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if called || app.timelineWriter != nil {
		t.Fatalf("disabled shadow constructed: called=%v writer=%v", called, app.timelineWriter)
	}
}

func TestSTEP5WorkspaceLeaseIsDefaultOff(t *testing.T) {
	off, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if off.workspaceLeases != nil {
		t.Fatal("workspace lease constructed while flag is off")
	}
	on, err := NewAppWithDeps(Config{InsecureLocalOnly: true, EnableWorkspaceLease: true}, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if on.workspaceLeases == nil {
		t.Fatal("workspace lease was not constructed while flag is on")
	}
}

func TestSTEP4TimelineUnavailableDoesNotBlockDaemonConstruction(t *testing.T) {
	unavailable := errors.New("timeline path unavailable")
	deps := v1LifecycleDeps(&appV1Watcher{}, &appV1IPC{})
	deps.OpenTimelineShadow = func(config writer.Config, _ writer.ProducerAuth) (*writer.Writer, error) {
		if config.Path != "/unavailable/timeline.jsonl" {
			t.Fatalf("path = %q", config.Path)
		}
		return nil, unavailable
	}
	app, err := NewAppWithDeps(Config{
		InsecureLocalOnly: true, EnableTimelineShadow: true, TimelineShadowPath: "/unavailable/timeline.jsonl",
	}, deps)
	if err != nil {
		t.Fatalf("Timeline failure blocked daemon construction: %v", err)
	}
	if app.timelineWriter != nil {
		t.Fatal("unavailable Timeline writer was retained")
	}
	app.server.Addr = "127.0.0.1:0"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := app.Run(ctx); err != nil {
		t.Fatalf("Timeline failure blocked daemon startup/shutdown: %v", err)
	}
}

func TestSTEP4FailedAppConstructionClosesTimelineWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeline-shadow.jsonl")
	var opened *writer.Writer
	deps := Dependencies{
		OpenTimelineShadow: func(config writer.Config, _ writer.ProducerAuth) (*writer.Writer, error) {
			var err error
			opened, err = writer.Open(config, nil)
			return opened, err
		},
	}
	_, err := NewAppWithDeps(Config{
		InsecureLocalOnly: true, EnableTimelineShadow: true, TimelineShadowPath: path,
		ListenAddr: "not-a-loopback-address",
	}, deps)
	if err == nil {
		t.Fatal("invalid listen address unexpectedly constructed App")
	}
	if opened == nil {
		t.Fatal("Timeline writer was not opened by regression setup")
	}
	if opened.Append(step4Envelope(t)) {
		t.Fatal("Timeline writer remained open after failed App construction")
	}
	if got := opened.Stats(); got != (writer.Stats{Dropped: 1}) {
		t.Fatalf("stats = %+v, want closed-writer drop", got)
	}
}

func step4Envelope(t *testing.T) contract.Envelope {
	t.Helper()
	scope := contract.Scope{SessionID: "controlled_pty:one", RuntimeID: "runtime-1", LaunchGeneration: 1}
	e, err := contract.NewEnvelope(contract.Envelope{
		SchemaVersion: contract.SchemaV1, PayloadVersion: contract.PayloadV1,
		EventKind: contract.EventToolCallStarted,
		SessionID: scope.SessionID, RuntimeID: scope.RuntimeID, LaunchGeneration: scope.LaunchGeneration,
		Provider: "codex", SourceIncarnation: "run-1",
		SourceIdentity: contract.SourceIdentity{Kind: "provider", ID: "session-1"}, SourcePosition: "position-1",
		OccurredAt: time.Unix(1, 0).UTC(), ObservedAt: time.Unix(2, 0).UTC(), RedactionPolicyVersion: "redaction-v1",
		Payload:         contract.Payload{Redacted: &contract.RedactedPayload{Summary: "safe"}},
		EvidenceSources: contract.EvidenceSources{Provider: &contract.ProviderEvidenceRef{ID: "provider-evidence-1", Scope: scope}},
		References:      contract.References{ToolCall: &contract.TypedReference{Kind: contract.ReferenceToolCall, ID: "tool-1", Scope: scope}},
		T0Event:         agent.AgentEvent{ID: "t0-1", SessionID: scope.SessionID, AgentKind: "codex", Type: agent.EventToolCallStarted},
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
