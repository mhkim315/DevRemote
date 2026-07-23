package term

import "testing"

type panicOperationalRevoker struct{}

func (panicOperationalRevoker) SubmitAfterCommit(OperationalEvent) {}
func (panicOperationalRevoker) RevokeOperationalRuntime()          { panic("revoker panic") }

func TestRevokeOperationalRuntimeRecoversPanic(t *testing.T) {
	// Timeline cleanup is evidence-only. A composition revoker panic cannot
	// change the already-authoritative provider lifecycle transition.
	revokeOperationalRuntime(panicOperationalRevoker{})
}
