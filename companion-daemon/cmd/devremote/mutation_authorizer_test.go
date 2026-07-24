package main

import (
	"errors"
	"testing"

	"devremote/companion-daemon/internal/devicetrust"
)

func TestCompositionMutationAuthorizerFailsClosedWhenUnready(t *testing.T) {
	if _, err := newCompositionMutationAuthorizer(nil); !errors.Is(err, ErrMutationAuthorityNotReady) {
		t.Fatalf("nil composition construction error = %v, want %v", err, ErrMutationAuthorityNotReady)
	}
	authorizer := &compositionMutationAuthorizer{}
	if err := authorizer.AuthorizeCommit("device", 0, devicetrust.IntentSessionCreate); !errors.Is(err, ErrMutationAuthorityNotReady) {
		t.Fatalf("unready composition error = %v, want %v", err, ErrMutationAuthorityNotReady)
	}
}

func TestCompositionMutationAuthorizerDelegatesToInjectedAuthority(t *testing.T) {
	counting := &countingMutationAuthorizer{}
	authorizer, err := newCompositionMutationAuthorizer(counting)
	if err != nil {
		t.Fatal(err)
	}
	if err := authorizer.AuthorizeCommit("device", 7, devicetrust.IntentSessionCreate); err != nil {
		t.Fatal(err)
	}
	if counting.calls != 1 || counting.intent != devicetrust.IntentSessionCreate {
		t.Fatalf("delegation = calls %d intent %q", counting.calls, counting.intent)
	}
}

type countingMutationAuthorizer struct {
	calls  int
	intent devicetrust.MutationIntent
}

func (a *countingMutationAuthorizer) AuthorizeCommit(_ string, _ uint64, intent devicetrust.MutationIntent) error {
	a.calls++
	a.intent = intent
	return nil
}
