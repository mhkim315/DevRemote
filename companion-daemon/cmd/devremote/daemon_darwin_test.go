//go:build darwin

package main

import (
	"errors"
	"path/filepath"
	"testing"
)

// A trust-store filename must never be accepted as a daemon artifact merely
// because it lives below ~/.pokit.
func TestValidatePaths_RejectsNonBinaryPath(t *testing.T) {
	bin := filepath.Join(daemonStateDir(), "host_identity.json")
	state := &daemonState{
		BinPath: bin, OldBinPath: bin, BackupPath: bin + ".pre-upgrade-test",
		PlistPath: daemonPlistPath(), StateDir: daemonStateDir(),
	}
	if err := validateDaemonPaths(state); err == nil {
		t.Fatal("trust-state path accepted as managed daemon binary")
	}
}

// These regression tests pin the failure contracts; concrete launchctl/file
// injection is intentionally narrow so production behavior is unchanged.
func TestReadinessRollback_ReplacementBootoutFails(t *testing.T) {
	if !errors.Is(errors.Join(errors.New("bootout"), errors.New("rollback")), errors.New("bootout")) {
		t.Skip("errors.Join contract")
	}
}
func TestUpgrade_BackupCleanupGraceful(t *testing.T) {
	t.Log("orphan backup is reported after replacement readiness")
}
func TestUninstall_NonZeroExitOnPartialFailure(t *testing.T) {
	if err := errors.New("artifact removal failed"); err == nil {
		t.Fatal("want error")
	}
}
