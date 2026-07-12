package doctor

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ── AllowList ──

// AllowList restricts read and write access for a repair operation. Paths are
// relative to the repository root.
type AllowList struct {
	ReadPaths  []string
	WritePaths []string
}

// ── Sandbox ──

// Sandbox enforces the allowlist during patch generation and validation.
type Sandbox struct {
	allowList AllowList
}

// NewSandbox builds a Sandbox enforcing the given allowlist.
func NewSandbox(al AllowList) *Sandbox {
	return &Sandbox{allowList: al}
}

// ── Canonical allowlists ──

// AdapterAllowList returns the canonical allowlist for a specific adapter
// under internal/agent/adapters/<provider>/<version>/.
func AdapterAllowList(provider, version string) AllowList {
	adapterDir := filepath.Join("internal", "agent", "adapters", provider, version)
	return AllowList{
		ReadPaths: []string{
			adapterDir + "/",
			"internal/agent/contract/",
			"internal/agent/models.go",
			"internal/agent/detector.go",
			"internal/agent/parser.go",
		},
		WritePaths: []string{
			adapterDir + "/",
		},
	}
}

// ── ValidatePatch ──

// ValidatePatch returns an error if any changed file falls outside the
// sandbox's write allowlist. Paths are cleaned before comparison.
// An empty changedFiles slice is valid (no-op).
func (s *Sandbox) ValidatePatch(changedFiles []string) error {
	if len(changedFiles) == 0 {
		return nil
	}
	for _, f := range changedFiles {
		cleaned := filepath.Clean(f)
		if !s.isAllowedWrite(cleaned) {
			return fmt.Errorf("file %q is outside the repair sandbox write allowlist", cleaned)
		}
		if DeniedPath(cleaned) {
			return fmt.Errorf("file %q is on the immutable deny list and cannot be modified", cleaned)
		}
	}
	return nil
}

// isAllowedWrite checks whether a cleaned path is within at least one write
// allowlist entry. Prefix matching is used (no globs) to avoid bypass risks.
func (s *Sandbox) isAllowedWrite(cleaned string) bool {
	for _, prefix := range s.allowList.WritePaths {
		cleanedPrefix := filepath.Clean(prefix)
		if strings.HasPrefix(cleaned, cleanedPrefix+string(filepath.Separator)) ||
			cleaned == cleanedPrefix ||
			strings.HasPrefix(cleaned, cleanedPrefix+"/") {
			return true
		}
	}
	return false
}

// ── Deny list ──

// denyListPaths are the immutable paths the repair agent is explicitly barred
// from modifying. This set is fixed and not configurable.
var denyListPaths = []string{
	"internal/agent/contract",
	"internal/agent/models.go",
	"internal/agent/bridge.go",
	"internal/term",
	"internal/mux",
	"cmd",
	"mobile",
	"docs",
}

// DeniedPath reports whether a cleaned path falls within the deny list.
func DeniedPath(cleaned string) bool {
	for _, denied := range denyListPaths {
		if strings.HasPrefix(cleaned, denied+string(filepath.Separator)) ||
			cleaned == denied ||
			strings.HasPrefix(cleaned, denied+"/") {
			return true
		}
	}
	return false
}

// DenyList returns a copy of the immutable deny list for display.
func DenyList() []string {
	out := make([]string, len(denyListPaths))
	copy(out, denyListPaths)
	return out
}
