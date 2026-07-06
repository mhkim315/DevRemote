package mux

import (
	"strings"
	"testing"
)

func TestMapAgentProcesses(t *testing.T) {
	// 1. Multiple Claude tags on different surfaces
	// 2. Codex/Gemini process name fallback
	// 3. Shell-only surface
	// 4. Ambiguous agents on same surface

	snap := CmuxTopSnapshot{
		Tags: []TopTag{
			{Ref: "tag:claude1", Workspace: "workspace:1", Provider: "claude_code"},
			{Ref: "tag:claude2", Workspace: "workspace:2", Provider: "claude_code"},
		},
		Processes: []TopProcess{
			// Shell only
			{PID: 100, Parent: "surface:1", Name: "zsh"},

			// Claude 1 on surface:2 (process appears twice)
			{PID: 200, Parent: "surface:2", Name: "zsh"},
			{PID: 201, Parent: "surface:2", Name: "node"},
			{PID: 201, Parent: "tag:claude1", Name: "node"},

			// Claude 2 on surface:3 (process appears twice)
			{PID: 300, Parent: "surface:3", Name: "zsh"},
			{PID: 301, Parent: "surface:3", Name: "node"},
			{PID: 301, Parent: "tag:claude2", Name: "node"},

			// Codex fallback on surface:4
			{PID: 400, Parent: "surface:4", Name: "zsh"},
			{PID: 401, Parent: "surface:4", Name: "codex-cli"},

			// Ambiguous on surface:5
			{PID: 500, Parent: "surface:5", Name: "zsh"},
			{PID: 501, Parent: "surface:5", Name: "gemini-cli"},
			{PID: 502, Parent: "surface:5", Name: "codex-cli"},
		},
	}

	mapped, err := mapAgentProcesses(snap)
	if err == nil {
		t.Fatalf("expected ambiguity error on surface:5, got nil")
	}
	if !strings.Contains(err.Error(), "ambiguous agent processes") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// Remove ambiguous agent to test the rest
	snap.Processes = snap.Processes[:len(snap.Processes)-1]

	mapped, err = mapAgentProcesses(snap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mapped["surface:1"].PID != 100 || mapped["surface:1"].Provider != "" {
		t.Errorf("expected surface:1 to be shell PID 100, got %v", mapped["surface:1"])
	}

	if mapped["surface:2"].PID != 201 || mapped["surface:2"].Provider != "claude" {
		t.Errorf("expected surface:2 to be claude PID 201, got %v", mapped["surface:2"])
	}

	if mapped["surface:3"].PID != 301 || mapped["surface:3"].Provider != "claude" {
		t.Errorf("expected surface:3 to be claude PID 301, got %v", mapped["surface:3"])
	}

	if mapped["surface:4"].PID != 401 || mapped["surface:4"].Provider != "codex" {
		t.Errorf("expected surface:4 to be codex PID 401, got %v", mapped["surface:4"])
	}
}
