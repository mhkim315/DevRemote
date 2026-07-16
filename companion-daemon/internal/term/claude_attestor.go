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
	PinnedPath       string
	PinnedDigest     string
}

// PinnedClaudeConfig returns the default identity. PinnedDigest is empty;
// the caller MUST set it via PinnedClaudeConfigWithDigest before use.
func PinnedClaudeConfig() ClaudeEntryConfig {
	home, _ := os.UserHomeDir()
	return ClaudeEntryConfig{
		Bin:              "claude",
		Version:          "2.1.209",
		AuthorityVersion: "2.1.209",
		PinnedPath:       filepath.Join(home, ".local", "share", "claude", "versions", "2.1.209"),
	}
}

// PinnedClaudeConfigWithDigest returns the config with the given digest.
func PinnedClaudeConfigWithDigest(digest string) ClaudeEntryConfig {
	c := PinnedClaudeConfig()
	c.PinnedDigest = digest
	return c
}

func (c ClaudeEntryConfig) Verify() error {
	vs, err := exec.Command(c.Bin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("claude execute: %w", err)
	}
	if got := extractVersion(string(vs)); got != c.Version {
		return fmt.Errorf("claude version mismatch: want %q, got %q", c.Version, got)
	}
	return nil
}

// extractVersion returns the first whitespace-delimited token from a version
// string like "2.1.209 (Claude Code)".
func extractVersion(raw string) string {
	return strings.Fields(strings.TrimSpace(raw))[0]
}

type ClaudeAttestor interface {
	Certify(exe string) error
}

// versionRunner runs `exe --version`. Injectable for tests.
var versionRunner = func(exe string) (string, error) {
	out, err := exec.Command(exe, "--version").CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// digestProvider computes SHA-256 of a file. Injectable for tests.
var digestProvider = fileDigest

type productionClaudeAttestor struct {
	cfg ClaudeEntryConfig
}

func (a productionClaudeAttestor) Certify(exe string) error {
	if a.cfg.PinnedDigest == "" {
		return fmt.Errorf("claude certify: pinned digest not configured — supply a verified SHA-256")
	}
	if a.cfg.PinnedPath == "" {
		return fmt.Errorf("claude certify: pinned path not configured")
	}

	vs, err := versionRunner(exe)
	if err != nil {
		return fmt.Errorf("claude certify: %w", err)
	}
	if extractVersion(vs) != a.cfg.Version {
		return fmt.Errorf("claude certify: version mismatch: want %q, got %q", a.cfg.Version, vs)
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

	actual, err := digestProvider(resolved)
	if err != nil {
		return fmt.Errorf("claude certify: digest read: %w", err)
	}
	if actual != a.cfg.PinnedDigest {
		return fmt.Errorf("claude certify: digest mismatch: want %s, got %s", a.cfg.PinnedDigest, actual[:16])
	}

	return nil
}

func NewClaudeAttestor(cfg ClaudeEntryConfig) ClaudeAttestor {
	return productionClaudeAttestor{cfg: cfg}
}
