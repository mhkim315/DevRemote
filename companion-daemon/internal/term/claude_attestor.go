// Package term — C1D: OS-neutral Claude version/artifact certification seam.
// The attestor verifies the exact pinned Claude Code version, path, AND
// SHA-256 artifact digest before any managed spawn. PATH lookup alone is not
// certification. A missing PinnedDigest is a hard fail-closed error.
package term

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ClaudeEntryConfig holds the pinned Claude provider identity.
type ClaudeEntryConfig struct {
	Bin              string
	Version          string
	AuthorityVersion string
	PinnedPath       string // mandatory: absolute path
	PinnedDigest     string // mandatory: hex-encoded SHA-256
}

// PinnedClaudeConfig returns the C0D-accepted pinned identity. The digest
// field is empty by design — the caller MUST supply a verified digest before
// use, or all Certify calls will fail closed.
func PinnedClaudeConfig() ClaudeEntryConfig {
	home, _ := os.UserHomeDir()
	return ClaudeEntryConfig{
		Bin:              "claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
		PinnedPath:       filepath.Join(home, ".local", "share", "claude", "versions", "2.1.209", "claude"),
		PinnedDigest:     "", // caller MUST set before use
	}
}

// Verify checks the executable reports the exact pinned version string.
func (c ClaudeEntryConfig) Verify() error {
	vs, err := exec.Command(c.Bin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude execute: %w", err)
	}
	if got := strings.TrimSpace(string(vs)); got != c.Version {
		return fmt.Errorf("claude version mismatch: want %q, got %q", c.Version, got)
	}
	return nil
}

// ClaudeAttestor is the OS-neutral certification seam.
type ClaudeAttestor interface {
	Certify(exe string) error
}

type productionClaudeAttestor struct {
	cfg ClaudeEntryConfig
}

// digestProvider computes SHA-256 of a file. Replaceable in tests.
var digestProvider = fileDigest

func (a productionClaudeAttestor) Certify(exe string) error {
	// 1. Config checks (no I/O, fail-closed before any exec).
	if a.cfg.PinnedDigest == "" {
		return fmt.Errorf("claude certify: pinned digest not configured — supply a verified SHA-256")
	}
	if a.cfg.PinnedPath == "" {
		return fmt.Errorf("claude certify: pinned path not configured")
	}

	// 2. Version check.
	vs, err := exec.Command(exe, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude certify: %w", err)
	}
	if got := strings.TrimSpace(string(vs)); got != a.cfg.Version {
		return fmt.Errorf("claude certify: version mismatch: want %q, got %q", a.cfg.Version, got)
	}

	// 3. Path resolution.
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("claude certify: realpath: %w", err)
	}
	want, err := filepath.EvalSymlinks(a.cfg.PinnedPath)
	if err != nil {
		return fmt.Errorf("claude certify: pinned realpath: %w", err)
	}
	if resolved != want {
		return fmt.Errorf("claude certify: path mismatch: resolved %q, pinned %q", resolved, want)
	}
	if fi, err := os.Stat(resolved); err != nil || fi.IsDir() {
		return fmt.Errorf("claude certify: pinned path is not a regular executable")
	}
	actual, err := digestProvider(resolved)
	if err != nil {
		return fmt.Errorf("claude certify: digest read: %w", err)
	}
	if actual != a.cfg.PinnedDigest {
		return fmt.Errorf("claude certify: digest mismatch: want %s, got %s", a.cfg.PinnedDigest, actual[:16])
	}

	return nil
}

// NewClaudeAttestor creates the production attestor for the pinned config.
func NewClaudeAttestor(cfg ClaudeEntryConfig) ClaudeAttestor {
	return productionClaudeAttestor{cfg: cfg}
}
