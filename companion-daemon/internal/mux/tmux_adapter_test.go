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
	factory := func(t *testing.T) Adapter {
		// Track created sessions so they appear in subsequent list-sessions.
		extra := make(map[string]bool)
		return NewTmuxAdapterWithRunner(&recordingRunner{
			runFunc: func(_ context.Context, _ CommandOptions, args ...string) ([]byte, error) {
				if len(args) < 2 {
					return nil, nil
				}
				switch args[1] {
				case "list-sessions":
					base := "$0::POKIT::dev\n$1::POKIT::build"
					for name := range extra {
						base += "\n$" + name + "::POKIT::" + name
					}
					return []byte(base), nil
				case "new-session":
					// args: [tmux, new-session, -d, -s, <name>]
					for i, a := range args {
						if a == "-s" && i+1 < len(args) {
							extra[args[i+1]] = true
						}
					}
					return nil, nil
				case "kill-session":
					// Remove from tracked set.
					for i, a := range args {
						if a == "-t" && i+1 < len(args) {
							for name := range extra {
								if args[i+1] == "$"+name || args[i+1] == name {
									delete(extra, name)
								}
							}
						}
					}
					return nil, nil
				case "capture-pane":
					for _, a := range args {
						if a == "-S" {
							return []byte("scrollback history"), nil
						}
					}
					return []byte("screen content"), nil
				case "display-message":
					return []byte("1000000000,12345,/home/user/project"), nil
				default:
					return nil, nil
				}
			},
		})
	}
	RunAdapterContract(t, "tmux", factory)
	RunScreenHistoryContract(t, factory)
	RunProcessInfoContract(t, factory)
	RunCreateDiscoverTerminateContract(t, factory)
}
