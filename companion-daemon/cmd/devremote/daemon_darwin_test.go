//go:build darwin

package main

import (
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

func TestValidatePaths_RejectsUnknownTransactionPhase(t *testing.T) {
	state := &daemonState{Phase: "half-installed"}
	if err := validateDaemonPaths(state); err == nil {
		t.Fatal("unknown transaction phase accepted")
	}
}
