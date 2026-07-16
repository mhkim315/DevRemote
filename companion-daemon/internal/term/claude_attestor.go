// Package term — C1D: OS-neutral Claude version/artifact certification seam.
// The attestor verifies the exact pinned Claude Code version before any managed
// spawn. PATH lookup alone is availability, not certification — the attestor
// must confirm the executable reports the exact pinned version string.
package term

import (
	"fmt"
	"os/exec"
	"strings"
)

// ClaudeEntryConfig holds the pinned Claude provider identity for the managed
// runtime. It is deliberately simpler than CodexAppServerEntryConfig: Claude is
// a single binary with no shim/native split.
type ClaudeEntryConfig struct {
	// Bin is the path to the claude executable (resolved via PATH or absolute).
	Bin string
	// Version is the display version string reported by `claude --version`.
	// This is matched exactly — substring or prefix match is not certification.
	Version string
	// AuthorityVersion is the ONE grammar-valid canonical version used as
	// runtime/approval authority. It is compared exactly; the display Version
	// is never compared as authority.
	AuthorityVersion string
}

// PinnedClaudeConfig returns the C0D-accepted pinned identity for the exact
// Claude Code 2.1.209 install. The version string matches the C0D evidence.
func PinnedClaudeConfig() ClaudeEntryConfig {
	return ClaudeEntryConfig{
		Bin:              "claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
	}
}

// Verify checks that the executable at Bin exists, executes, and reports the
// exact pinned Version string. It returns a descriptive error on any mismatch.
// Never PATH lookup — the Bin must already be resolved.
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

// ClaudeAttestor is the OS-neutral certification seam. The production
// implementation checks the pinned version string; tests inject a fake.
type ClaudeAttestor interface {
	// Certify verifies that the candidate executable matches the pinned
	// identity. Returns nil if certified; a descriptive error otherwise.
	Certify(exe string) error
}

// productionClaudeAttestor verifies the exact pinned version.
type productionClaudeAttestor struct {
	cfg ClaudeEntryConfig
}

func (a productionClaudeAttestor) Certify(exe string) error {
	vs, err := exec.Command(exe, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude certify: %w", err)
	}
	if got := strings.TrimSpace(string(vs)); got != a.cfg.Version {
		return fmt.Errorf("claude certify: version mismatch: want %q, got %q", a.cfg.Version, got)
	}
	return nil
}

// NewClaudeAttestor creates the production attestor for the pinned config.
func NewClaudeAttestor(cfg ClaudeEntryConfig) ClaudeAttestor {
	return productionClaudeAttestor{cfg: cfg}
}
