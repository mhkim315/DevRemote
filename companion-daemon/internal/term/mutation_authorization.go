package term

import "devremote/companion-daemon/internal/devicetrust"

// localMutationAuthorizer is used only by legacy, unauthenticated unit-test
// constructors that predate device trust. Production composition always passes
// the real DeviceRegistry through the constructor and never uses this value.
// It remains a concrete authorizer so mutation code never has a nil-authorizer
// branch.
type localMutationAuthorizer struct{}

func (localMutationAuthorizer) AuthorizeCommit(string, uint64, devicetrust.MutationIntent) error {
	return nil
}
