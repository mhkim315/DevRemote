package main

import (
	"net/http"
	"net/http/httptest"
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

// TestManagedNativeStatusRoute_Authenticated: the SP0 read route is wired
// behind production auth. In remote mode an anonymous request is rejected by
// the device-principal gate; in insecure-local mode the dev token reaches the
// handler (404 for an unknown managed id — not an auth error).
func TestManagedNativeStatusRoute_Authenticated(t *testing.T) {
	// Remote (production) mode: device bearer required.
	remote, err := NewAppWithDeps(Config{EnableManagedCodex: true}, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps(remote): %v", err)
	}
	remoteSrv := httptest.NewServer(remote.server.Handler)
	defer remoteSrv.Close()
	resp, err := http.Get(remoteSrv.URL + "/api/sessions/codex_app_server:x/native-status")
	if err != nil {
		t.Fatalf("anon get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("remote anonymous status = %d, want 401", resp.StatusCode)
	}

	// Insecure-local mode: dev token reaches the handler.
	local, err := NewAppWithDeps(Config{InsecureLocalOnly: true, EnableManagedCodex: true}, testDeps())
	if err != nil {
		t.Fatalf("NewAppWithDeps(local): %v", err)
	}
	localSrv := httptest.NewServer(local.server.Handler)
	defer localSrv.Close()
	req, _ := http.NewRequest("GET", localSrv.URL+"/api/sessions/codex_app_server:x/native-status", nil)
	req.Header.Set("Authorization", "Bearer dev-token")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("auth get: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Fatalf("authenticated status = %d, want 404 for unknown id", resp2.StatusCode)
	}

	// Dedicated managed list route: 401 anonymous in remote mode, 200 with
	// dev token in insecure-local mode.
	resp3, err := http.Get(remoteSrv.URL + "/api/managed-sessions")
	if err != nil {
		t.Fatalf("anon list get: %v", err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Fatalf("remote anonymous managed list = %d, want 401", resp3.StatusCode)
	}
	reqList, _ := http.NewRequest("GET", localSrv.URL+"/api/managed-sessions", nil)
	reqList.Header.Set("Authorization", "Bearer dev-token")
	resp4, err := http.DefaultClient.Do(reqList)
	if err != nil {
		t.Fatalf("auth list get: %v", err)
	}
	resp4.Body.Close()
	if resp4.StatusCode != http.StatusOK {
		t.Fatalf("authenticated managed list = %d, want 200", resp4.StatusCode)
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
