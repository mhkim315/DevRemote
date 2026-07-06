package mux

import (
	"context"
	"testing"
)

func TestParseTmuxListSessionLine(t *testing.T) {
	target, name, ok := parseTmuxListSessionLine("$8::POKIT::tmux:aider")
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

func TestTmuxAdapter_Contract(t *testing.T) {
	RunAdapterContract(t, "tmux", func(t *testing.T) Adapter {
		return NewTmuxAdapterWithRunner(&recordingRunner{
			runFunc: func(_ context.Context, _ CommandOptions, args ...string) ([]byte, error) {
				if len(args) > 1 && args[1] == "list-sessions" {
					return []byte("$0::POKIT::dev\n$1::POKIT::build"), nil
				}
				return nil, nil
			},
		})
	})
}
