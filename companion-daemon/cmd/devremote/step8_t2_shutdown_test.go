package main

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSTEP8T2ShutdownReclaimsOptionalObserverWorker(t *testing.T) {
	before := runtime.NumGoroutine()
	app, err := NewAppWithDeps(Config{
		InsecureLocalOnly: true, EnableCockpit: true, EnableTimelineShadow: true,
		TimelineShadowPath: filepath.Join(t.TempDir(), "timeline.jsonl"),
	}, Dependencies{})
	if err != nil {
		t.Fatal(err)
	}
	if afterStart := runtime.NumGoroutine(); afterStart < before+1 {
		t.Fatalf("optional writer worker did not start: before=%d after=%d", before, afterStart)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if afterShutdown := runtime.NumGoroutine(); afterShutdown > before {
		t.Fatalf("observer worker leaked: before=%d after=%d", before, afterShutdown)
	}
}
