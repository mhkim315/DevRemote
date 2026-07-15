// Package term — SP0: pinned Codex provider identity for the native managed
// runtime. Fail-closed identity check before every managed spawn. This is an
// explicit-path + artifact-digest check, NOT process-image attestation
// (cdhash/EndpointSecurity/supply-chain certification are out of SP0 scope).
package term

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CodexAppServerEntryConfig holds the pinned provider identity.
type CodexAppServerEntryConfig struct {
	Bin     string
	Version string // display version string (e.g. "codex-cli 0.144.1"); NEVER authority
	// AuthorityVersion is the ONE grammar-valid canonical version used as
	// runtime/approval authority (SP1 §3). It is compared exactly against the
	// certified value; the display Version is never compared as authority.
	AuthorityVersion string
	ShimPath         string
	ShimSHA          string
	NativePath       string
	NativeSHA        string
}

// PinnedConfig0x144 returns the CP0-accepted pinned identity for the exact
// @openai/codex@0.144.1 install in the dedicated out-of-repo toolchain prefix.
// Digest and path values match docs/a1_1_cp0_evidence/certification_identity.txt.
func PinnedConfig0x144() CodexAppServerEntryConfig {
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".pokit-cp0-toolchain")
	return CodexAppServerEntryConfig{
		Bin:              filepath.Join(p, "node_modules", ".bin", "codex"),
		Version:          "codex-cli 0.144.1",
		AuthorityVersion: certifiedCodexAuthorityVersion,
		ShimPath:         filepath.Join(p, "node_modules", "@openai", "codex", "bin", "codex.js"),
		ShimSHA:          "134063e133f0b4244fa3b251acf973d4fe4b4aeeacbdc135211bf480f59f1477",
		NativePath:       filepath.Join(p, "node_modules", "@openai", "codex-darwin-arm64", "vendor", "aarch64-apple-darwin", "bin", "codex"),
		NativeSHA:        "29915529b97697def1a957b0505e770aa6a45744435d62fc263e98d7619e167a",
	}
}

// Verify returns an error if the pinned executable is missing, reports the
// wrong version, does not resolve to the verified shim, or any artifact digest
// differs. Mirrors the accepted CP0 harness `_pinned_codex_bin` fail-closed
// checks (version string, realpath, shim + native sha256). Never PATH lookup.
func (c CodexAppServerEntryConfig) Verify() error {
	vs, err := exec.Command(c.Bin, "--version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pinned codex execute: %w", err)
	}
	if got := strings.TrimSpace(string(vs)); got != c.Version {
		return fmt.Errorf("version mismatch: want %q, got %q", c.Version, got)
	}
	r, err := filepath.EvalSymlinks(c.Bin)
	if err != nil {
		return fmt.Errorf("realpath %s: %w", c.Bin, err)
	}
	want, err := filepath.EvalSymlinks(c.ShimPath)
	if err != nil {
		return fmt.Errorf("realpath %s: %w", c.ShimPath, err)
	}
	if r != want {
		return fmt.Errorf("realpath mismatch: .bin/codex does not resolve to the verified shim")
	}
	for _, pair := range []struct{ path, want string }{
		{c.ShimPath, c.ShimSHA},
		{c.NativePath, c.NativeSHA},
	} {
		d, err := fileDigest(pair.path)
		if err != nil {
			return fmt.Errorf("artifact read %s: %w", pair.path, err)
		}
		if d != pair.want {
			return fmt.Errorf("artifact digest mismatch %s: got %s", pair.path, d[:16])
		}
	}
	return nil
}

// fileDigest streams the file through sha256 (the native artifact is large).
func fileDigest(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
