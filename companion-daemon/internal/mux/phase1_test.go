package mux

import (
	"context"
	"errors"
	"testing"
)

// Phase 1: Identity and discovery contract tests.

func TestSessionRef_Canonical(t *testing.T) {
	tests := []struct {
		adapter, localID, want string
	}{
		{"tmux", "ai", "tmux:ai"},
		{"tmux", "aider", "tmux:aider"},
		{"tmux", "tmux:aider", "tmux:tmux:aider"},
		{"cmux", "surface:1", "cmux:surface:1"},
		{"tmux", "한글", "tmux:한글"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			ref := SessionRef{Adapter: tt.adapter, LocalID: tt.localID}
			got := ref.Canonical()
			if got != tt.want {
				t.Errorf("Canonical() = %q, want %q", got, tt.want)
			}
			if ref.String() != tt.want {
				t.Errorf("String() = %q, want %q", ref.String(), tt.want)
			}
		})
	}
}

func TestSessionRef_Validate(t *testing.T) {
	valid := SessionRef{Adapter: "tmux", LocalID: "session"}
	if err := valid.Validate(); err != nil {
		t.Errorf("Validate() on valid ref: %v", err)
	}

	tests := []struct {
		name string
		ref  SessionRef
	}{
		{"empty adapter", SessionRef{Adapter: "", LocalID: "x"}},
		{"empty local", SessionRef{Adapter: "tmux", LocalID: ""}},
		{"control char in adapter", SessionRef{Adapter: "tm\x00ux", LocalID: "x"}},
		{"control char in local", SessionRef{Adapter: "tmux", LocalID: "x\x1fy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.ref.Validate(); err == nil {
				t.Error("Validate() returned nil, want error")
			}
		})
	}
}

type testAdapter struct{ name string }

func (a *testAdapter) Name() string                                        { return a.name }
func (a *testAdapter) ListSessions(ctx context.Context) ([]Session, error) { return nil, nil }
func (a *testAdapter) GetSession(id string) (Session, error) {
	return nil, ErrSessionNotFound
}

func TestRegistry_RejectDuplicateAdapter(t *testing.T) {
	reg := MustNewRegistry()
	if err := reg.Register(&testAdapter{name: "dup"}); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := reg.Register(&testAdapter{name: "dup"})
	if err == nil {
		t.Fatal("Register(duplicate) returned nil, want error")
	}
	if !errors.Is(err, ErrDuplicateAdapter) {
		t.Errorf("error = %v, want ErrDuplicateAdapter", err)
	}
}

func TestSentinelErrors_Distinguishable(t *testing.T) {
	errs := []error{
		ErrSessionNotFound,
		ErrAdapterUnavailable,
		ErrUnsupported,
		ErrTimeout,
		ErrInvalidSessionID,
		ErrDuplicateAdapter,
	}
	for i, e1 := range errs {
		for j, e2 := range errs {
			if i != j && errors.Is(e1, e2) {
				t.Errorf("%v and %v should be distinct", e1, e2)
			}
		}
	}
}
