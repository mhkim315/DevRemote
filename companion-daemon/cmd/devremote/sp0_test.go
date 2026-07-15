package main

import (
	"testing"
)

// ── SP0-P1: CLI structured request + production composition ──

// TestBuildRunCreateRequest_DetachCodexStructured: `pokit run --detach codex`
// (exactly the recognized profile token) is sent as a structured profile
// request with NO command string. All other forms keep the legacy shape.
func TestBuildRunCreateRequest_DetachCodexStructured(t *testing.T) {
	got := buildRunCreateRequest([]string{"codex"}, "/tmp", true)
	if got["profileId"] != "codex" || got["detach"] != true || got["operation"] != "create" {
		t.Fatalf("structured request = %v", got)
	}
	if _, hasCmd := got["command"]; hasCmd {
		t.Fatalf("structured request must not carry a command string: %v", got)
	}

	// Non-detach codex keeps the legacy command form (interactive attach path).
	legacy := buildRunCreateRequest([]string{"codex"}, "", false)
	if legacy["command"] != "codex" {
		t.Fatalf("legacy request = %v, want command form", legacy)
	}
	if _, hasProfile := legacy["profileId"]; hasProfile {
		t.Fatalf("legacy request must not carry profileId: %v", legacy)
	}

	// Extra tokens are NOT the recognized profile — legacy form.
	multi := buildRunCreateRequest([]string{"codex", "--help"}, "", true)
	if multi["command"] != "codex --help" {
		t.Fatalf("multi-token request = %v, want command form", multi)
	}
	if _, hasProfile := multi["profileId"]; hasProfile {
		t.Fatalf("multi-token request must not carry profileId: %v", multi)
	}
}

// TestNewAppWithDeps_ManagedCodexFlag: the production composition root
// constructs the managed runtime service exactly when the SP0 feature flag is
// enabled, and never otherwise.
func TestNewAppWithDeps_ManagedCodexFlag(t *testing.T) {
	on, err := NewAppWithDeps(Config{InsecureLocalOnly: true, EnableManagedCodex: true}, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps(on): %v", err)
	}
	if on.managed == nil {
		t.Fatal("EnableManagedCodex=true did not construct the managed service")
	}
	if on.managed.Registry() == nil {
		t.Fatal("managed service has no owned registry")
	}

	off, err := NewAppWithDeps(Config{InsecureLocalOnly: true}, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps(off): %v", err)
	}
	if off.managed != nil {
		t.Fatal("managed service constructed with the flag off")
	}
}
