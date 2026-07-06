package mux

import "testing"

func TestParseTmuxListSessionLine(t *testing.T) {
	target, name, ok := parseTmuxListSessionLine("$8\ttmux:aider")
	if !ok {
		t.Fatal("expected parse to succeed")
	}
	if target != "$8" {
		t.Fatalf("target = %q, want $8", target)
	}
	if name != "tmux:aider" {
		t.Fatalf("name = %q, want tmux:aider", name)
	}
}
