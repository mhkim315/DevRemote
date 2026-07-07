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
		extra := make(map[string]bool)
		return NewTmuxAdapterWithRunner(&recordingRunner{
			runFunc: func(ctx context.Context, _ CommandOptions, args ...string) ([]byte, error) {
				if len(args) < 2 {
					return nil, nil
				}
				switch args[1] {
				case "list-sessions":
					if ctx.Err() != nil {
						return nil, ctx.Err()
					}
					base := "$0::POKIT::dev\n$1::POKIT::build"
					for name := range extra {
						base += "\n$" + name + "::POKIT::" + name
					}
					return []byte(base), nil
				case "new-session":
					for i, a := range args {
						if a == "-s" && i+1 < len(args) {
							extra[args[i+1]] = true
						}
					}
					return nil, nil
				case "kill-session":
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
				}
				return nil, nil
			},
		})
	}

	cfg := &ContractConfig{
		ExpectScreenReader:      true,
		ExpectHistoryReader:     true,
		ExpectStreamOpener:      true,
		ExpectProcessProvider:   true,
		ExpectSessionCreator:    true,
		ExpectSessionTerminator: true,
	}

	RunAdapterContract(t, "tmux", factory)
	RunScreenHistoryContract(t, cfg, factory)
	RunProcessInfoContract(t, cfg, factory)
	RunCreateDiscoverTerminateContract(t, cfg, factory)
	RunLiveStreamContract(t, cfg, factory)
}
