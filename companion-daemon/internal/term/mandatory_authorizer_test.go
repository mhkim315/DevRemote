package term

import (
	"errors"
	"testing"

	"devremote/companion-daemon/internal/devicetrust"
)

var errTestMutationDenied = errors.New("test mutation denied")

func TestProductionConstructorsRejectNilMutationAuthorizer(t *testing.T) {
	if _, err := NewManagedCodexService(nil, CodexAppServerEntryConfig{}, nil); err == nil {
		t.Fatal("managed codex accepted nil mutation authorizer")
	}
	if _, err := NewManagedClaudeService(nil, ClaudeEntryConfig{}, nil, nil); err == nil {
		t.Fatal("managed claude accepted nil mutation authorizer")
	}
	if _, err := NewOwnedPTYRuntime(nil, nil, nil); err == nil {
		t.Fatal("owned PTY accepted nil mutation authorizer")
	}
	if _, err := NewLifecycleService(nil, nil, nil); err == nil {
		t.Fatal("lifecycle accepted nil mutation authorizer")
	}
	if _, err := NewApprovalStore(nil); err == nil {
		t.Fatal("approval store accepted nil mutation authorizer")
	}
	if _, err := NewAuthoritativeApprovalStore(nil); err == nil {
		t.Fatal("authoritative approval store accepted nil mutation authorizer")
	}
	if _, err := NewRuntimeDeliveryGate(nil); err == nil {
		t.Fatal("delivery gate accepted nil mutation authorizer")
	}
	if _, err := NewClaudeResumeCoordinator(nil); err == nil {
		t.Fatal("Claude coordinator accepted nil mutation authorizer")
	}
	if _, err := NewTelemetryService(nil, nil, nil, nil); err == nil {
		t.Fatal("telemetry accepted nil mutation authorizer")
	}
}

func TestImmutableServiceAuthorizerDeniesBeforeMutation(t *testing.T) {
	denied := deniedMutationAuthorizer{}
	launcher := &fakeLauncher{}
	service, err := NewManagedCodexService(denied, CodexAppServerEntryConfig{
		Bin:              "/pinned/toolchain/node_modules/.bin/codex",
		Version:          "codex-cli 0.144.1",
		AuthorityVersion: certifiedCodexAuthorityVersion,
	}, launcher)
	if err != nil {
		t.Fatal(err)
	}
	service.verify = func() error { return nil }
	if _, err := service.CreateDetached("", "device", 0); err == nil {
		t.Fatal("denied authorizer allowed managed session creation")
	}
	launcher.mu.Lock()
	launched := launcher.calls
	launcher.mu.Unlock()
	if launched != 0 {
		t.Fatalf("denied create launched provider %d time(s)", launched)
	}
}

type deniedMutationAuthorizer struct{}

func (deniedMutationAuthorizer) AuthorizeCommit(string, uint64, devicetrust.MutationIntent) error {
	return errTestMutationDenied
}
func (deniedMutationAuthorizer) AuthorizeAndCommit(string, uint64, devicetrust.MutationIntent, func() error) error {
	return errTestMutationDenied
}
