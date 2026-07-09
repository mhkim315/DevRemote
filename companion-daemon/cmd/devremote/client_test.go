package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildRunPayload_ValidJSON(t *testing.T) {
	body, id := buildRunPayload("echo hello", "")
	if len(body) == 0 {
		t.Fatal("empty body")
	}
	if id == "" {
		t.Fatal("empty id")
	}
	if !strings.HasPrefix(id, "controlled_pty:run-") {
		t.Errorf("id prefix: %q, want 'controlled_pty:run-'", id)
	}

	// Verify valid JSON.
	var decoded struct {
		ID      string `json:"id"`
		Command string `json:"command"`
		CWD     string `json:"cwd,omitempty"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded.Command != "echo hello" {
		t.Errorf("command: %q", decoded.Command)
	}
}

func TestBuildRunPayload_QuotesAndNewlines(t *testing.T) {
	body, _ := buildRunPayload(`echo "hello world"`, "")
	var decoded struct{ Command string }
	json.Unmarshal(body, &decoded)
	if decoded.Command != `echo "hello world"` {
		t.Errorf("command with quotes: %q", decoded.Command)
	}

	// Newlines should be in the JSON string, not break the JSON structure.
	body2, _ := buildRunPayload("echo line1\\necho line2", "")
	var decoded2 struct{ Command string }
	json.Unmarshal(body2, &decoded2)
	if !strings.Contains(decoded2.Command, "line2") {
		t.Errorf("multiline command: %q", decoded2.Command)
	}
}

func TestBuildRunPayload_UniqueIDs(t *testing.T) {
	_, id1 := buildRunPayload("cmd1", "")
	_, id2 := buildRunPayload("cmd2", "")
	if id1 == id2 {
		t.Error("ids must be unique across calls")
	}
}

func TestBuildRunPayload_CWDIncluded(t *testing.T) {
	body, _ := buildRunPayload("pwd", "/tmp")
	var decoded struct{ CWD string `json:"cwd,omitempty"` }
	json.Unmarshal(body, &decoded)
	if decoded.CWD != "/tmp" {
		t.Errorf("cwd: %q, want '/tmp'", decoded.CWD)
	}
}

func TestBuildRunPayload_CWDEmptyOmitted(t *testing.T) {
	body, _ := buildRunPayload("pwd", "")
	// cwd with omitempty should not appear when empty.
	var raw map[string]interface{}
	json.Unmarshal(body, &raw)
	if _, ok := raw["cwd"]; ok {
		t.Error("empty cwd should be omitted from JSON")
	}
}

func TestBuildRunRequest_AuthHeader(t *testing.T) {
	body, _ := buildRunPayload("echo hi", "")
	req, err := buildRunRequest("http://localhost:9171", "test-token-123", body)
	if err != nil {
		t.Fatalf("buildRunRequest: %v", err)
	}
	if req.Header.Get("Authorization") != "Bearer test-token-123" {
		t.Errorf("Authorization: %q", req.Header.Get("Authorization"))
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type: %q", req.Header.Get("Content-Type"))
	}
}

func TestBuildRunRequest_NoAuthHeader(t *testing.T) {
	body, _ := buildRunPayload("echo hi", "")
	req, err := buildRunRequest("http://localhost:9171", "", body)
	if err != nil {
		t.Fatalf("buildRunRequest: %v", err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("Authorization header must be absent when token is empty")
	}
}
