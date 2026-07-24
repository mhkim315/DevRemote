package devicetrust

import (
	crand "crypto/rand"
	"encoding/hex"
	"fmt"
)

// IPCMutationAuthorizer is the transport-scoped authority for the daemon's
// 0600 Unix socket. Its opaque identity is issued only to the IPC composition
// and is never accepted from an HTTP principal or request body.
type IPCMutationAuthorizer struct {
	identity string
}

// NewIPCMutationAuthorizer creates a fresh local IPC capability. The random
// identity prevents the shared service authorizer from treating HTTP's empty
// identity as local authority.
func NewIPCMutationAuthorizer() MutationAuthorizer {
	var raw [32]byte
	if _, err := crand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("IPC mutation authority entropy: %v", err))
	}
	return &IPCMutationAuthorizer{identity: "ipc-local:" + hex.EncodeToString(raw[:])}
}

func (a *IPCMutationAuthorizer) LocalMutationIdentity() (string, uint64) {
	if a == nil {
		return "", 0
	}
	return a.identity, 0
}

func (a *IPCMutationAuthorizer) AuthorizeCommit(deviceID string, expectedEpoch uint64, _ MutationIntent) error {
	if a == nil || deviceID != a.identity || expectedEpoch != 0 {
		return fmt.Errorf("IPC local mutation identity rejected")
	}
	return nil
}

func (a *IPCMutationAuthorizer) AuthorizeAndCommit(deviceID string, expectedEpoch uint64, intent MutationIntent, commit func() error) error {
	if err := a.AuthorizeCommit(deviceID, expectedEpoch, intent); err != nil {
		return err
	}
	if commit == nil {
		return fmt.Errorf("mutation commit callback is required")
	}
	return commit()
}
