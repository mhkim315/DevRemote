package devicetrust

import "fmt"

// InsecureLocalOnlyMutationAuthorizer is the explicit authority for the
// intentionally unauthenticated local mode. It is only installed by
// application composition when InsecureLocalOnly is enabled; it is not a
// fallback for a missing registry authorizer.
//
// The IPC listener is restricted to mode 0600 and the HTTP listener is
// restricted to loopback in this mode. Consequently the local transport is
// the authority boundary, and local mutations carry the empty identity and
// epoch zero. Any caller that supplies a device identity must use the
// registry-backed authority instead.
type InsecureLocalOnlyMutationAuthorizer struct{}

// NewInsecureLocalOnlyMutationAuthorizer returns the explicit local-mode
// authority. The returned value is always non-nil.
func NewInsecureLocalOnlyMutationAuthorizer() MutationAuthorizer {
	return InsecureLocalOnlyMutationAuthorizer{}
}

func (InsecureLocalOnlyMutationAuthorizer) AuthorizeCommit(deviceID string, expectedEpoch uint64, _ MutationIntent) error {
	if deviceID != "" || expectedEpoch != 0 {
		return fmt.Errorf("local mutation requires empty device identity and epoch zero")
	}
	return nil
}

func (a InsecureLocalOnlyMutationAuthorizer) AuthorizeAndCommit(deviceID string, expectedEpoch uint64, intent MutationIntent, commit func() error) error {
	if err := a.AuthorizeCommit(deviceID, expectedEpoch, intent); err != nil {
		return err
	}
	if commit == nil {
		return fmt.Errorf("mutation commit callback is required")
	}
	return commit()
}
