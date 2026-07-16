// Package term — C1D: OS-neutral Claude version/artifact certification seam.
// The attestor verifies the exact pinned Claude Code version AND artifact
// identity before any managed spawn. PATH lookup alone is not certification.
//
// The production attestor checks:
// 1. The executable reports the exact pinned version string via --version.
// 2. The resolved binary path matches an optional pinned path.
//
// Platform-specific artifact attestation (code-signing, cdhash) is deferred
// to the OS-specific attester implementation behind the ClaudeAttestor
// interface; the production default performs version + path checks.
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
	Version          string // e.g. "2.1.209" — exact match required
	AuthorityVersion string // canonical authority version string
	PinnedPath       string // optional: absolute path the resolved binary must match (empty = skip)
}

// PinnedClaudeConfig returns the C0D-accepted pinned identity for Claude
// Code 2.1.209. The PinnedPath is the default npm global install location;
// Certify requires the resolved binary to match this path.
func PinnedClaudeConfig() ClaudeEntryConfig {
	home, _ := os.UserHomeDir()
	return ClaudeEntryConfig{
		Bin:              "claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
		PinnedPath:       filepath.Join(home, ".local", "share", "claude", "versions", "2.1.209", "claude"),
	}
}

// Verify checks the executable reports the exact pinned version string.
func (c ClaudeEntryConfig) Verify() error {
	vs, err := exec.Command(c.Bin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude execute: %w", err)
	}
	got := strings.TrimSpace(string(vs))
	if got != c.Version {
		return fmt.Errorf("claude version mismatch: want %q, got %q", c.Version, got)
	}
	return nil
}

// ClaudeAttestor is the OS-neutral certification seam.
type ClaudeAttestor interface {
	// Certify verifies that the candidate executable matches the pinned
	// identity. Returns nil if certified; a descriptive error otherwise.
	// The exe parameter is the resolved absolute path (via LookPath or
	// equivalent).
	Certify(exe string) error
}

// productionClaudeAttestor performs version + optional path verification.
type productionClaudeAttestor struct {
	cfg ClaudeEntryConfig
}

func (a productionClaudeAttestor) Certify(exe string) error {
	// 1. Version check.
	vs, err := exec.Command(exe, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude certify: %w", err)
	}
	if got := strings.TrimSpace(string(vs)); got != a.cfg.Version {
		return fmt.Errorf("claude certify: version mismatch: want %q, got %q", a.cfg.Version, got)
	}

	// 2. Pinned path check (when configured).
	if a.cfg.PinnedPath != "" {
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
		// Verify the pinned file is a regular file.
		if fi, err := os.Stat(resolved); err != nil || fi.IsDir() {
			return fmt.Errorf("claude certify: pinned path is not a regular executable")
		}
	}

	return nil
}

// NewClaudeAttestor creates the production attestor for the pinned config.
func NewClaudeAttestor(cfg ClaudeEntryConfig) ClaudeAttestor {
	return productionClaudeAttestor{cfg: cfg}
}
