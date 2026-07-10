// Package devicetrust implements Pokit's local-first device trust root: a
// persistent P-256 host identity and a paired-device registry. It contains no
// network, pairing, token, or authentication middleware — those are later
// M2.5 phases. Storage is behind small interfaces so the MVP owner-only file
// store can be replaced by a macOS Keychain backend without changing callers.
package devicetrust

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	// ErrInsecurePermissions: a trust file is readable by group/other. Reads
	// fail closed rather than trusting a world-readable secret/registry.
	ErrInsecurePermissions = errors.New("trust store has insecure permissions")
	// ErrNoIdentity: no host identity is persisted yet (first run).
	ErrNoIdentity = errors.New("no host identity")
	// ErrCorruptStore: persisted trust data is malformed. Callers must fail
	// closed and NOT silently reset trust to empty.
	ErrCorruptStore = errors.New("corrupt trust store")
	// ErrNotP256: a public/private key is not a P-256 ECDSA key.
	ErrNotP256 = errors.New("key is not a P-256 ECDSA key")
	// ErrDeviceNotFound / ErrDeviceRevoked: registry lookups.
	ErrDeviceNotFound = errors.New("device not found")
	ErrDeviceRevoked  = errors.New("device is revoked")
)

// writeOwnerOnly writes data to path atomically (temp + rename) with 0600
// permissions and a 0700 parent directory. Never receives private-key material
// via a shell/argv — it is a direct file write.
func writeOwnerOnly(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".pokit-tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// readOwnerOnly reads path, failing closed with ErrInsecurePermissions if the
// file is accessible to group or other (permission bits broader than 0600).
func readOwnerOnly(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("%w: %s has mode %o", ErrInsecurePermissions, filepath.Base(path), perm)
	}
	return os.ReadFile(path)
}
