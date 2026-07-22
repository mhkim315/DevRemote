package main

import (
	"context"
	"errors"
	"testing"

	"devremote/companion-daemon/internal/timeline/writer"
)

func TestSTEP4TimelineShadowIsDefaultOff(t *testing.T) {
	called := false
	app, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, Dependencies{
		OpenTimelineShadow: func(writer.Config) (*writer.Writer, error) {
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

func TestSTEP4TimelineUnavailableDoesNotBlockDaemonConstruction(t *testing.T) {
	unavailable := errors.New("timeline path unavailable")
	deps := v1LifecycleDeps(&appV1Watcher{}, &appV1IPC{})
	deps.OpenTimelineShadow = func(config writer.Config) (*writer.Writer, error) {
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
