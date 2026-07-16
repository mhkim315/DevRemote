// Package term — C1D: OS-neutral Claude version/artifact certification seam.
// The attestor verifies the exact pinned Claude Code version AND artifact
// identity (path + SHA-256 digest) before any managed spawn. PATH lookup
// alone is not certification.
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
	PinnedPath       string // absolute path; resolved binary must match
	PinnedDigest     string // hex-encoded SHA-256 of the binary at PinnedPath (empty = skip)
}

// PinnedClaudeConfig returns the C0D-accepted pinned identity. The digest is
// left empty so the production binary can be attested at install time.
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
	if got := strings.TrimSpace(string(vs)); got != c.Version {
		return fmt.Errorf("claude version mismatch: want %q, got %q", c.Version, got)
	}
	return nil
}

// ClaudeAttestor is the OS-neutral certification seam.
type ClaudeAttestor interface {
	// Certify verifies that the candidate executable matches the pinned
	// identity (version + path + digest). Returns nil if certified.
	Certify(exe string) error
}

// productionClaudeAttestor performs version + path + optional digest checks.
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

	// 2. Pinned path check.
	if a.cfg.PinnedPath == "" {
		return fmt.Errorf("claude certify: pinned path not configured")
	}
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

	// 3. Digest check (when configured). A replaced binary with the same
	// version string at the same path will still fail if the digest differs.
	if a.cfg.PinnedDigest != "" {
		actual, err := fileDigest(resolved)
		if err != nil {
			return fmt.Errorf("claude certify: digest read: %w", err)
		}
		if actual != a.cfg.PinnedDigest {
			return fmt.Errorf("claude certify: digest mismatch: want %s, got %s", a.cfg.PinnedDigest, actual[:16])
		}
	}

	return nil
}

// NewClaudeAttestor creates the production attestor for the pinned config.
func NewClaudeAttestor(cfg ClaudeEntryConfig) ClaudeAttestor {
	return productionClaudeAttestor{cfg: cfg}
}
