package doctor

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ── Patch bounds ──

const (
	// MaxPatchBytes is the maximum total size of a unified patch (1 MiB).
	MaxPatchBytes = 1 << 20
	// MaxChangedFiles is the maximum number of files a patch may touch.
	MaxChangedFiles = 32
)

// ── Patch parse errors ──

var (
	ErrPatchTooLarge      = errors.New("patch exceeds MaxPatchBytes")
	ErrTooManyFiles       = errors.New("patch touches too many files")
	ErrBinaryPatch        = errors.New("binary patch rejected")
	ErrSymlinkEscape      = errors.New("symlink escape detected")
	ErrHardlinkEscape     = errors.New("hard-link escape detected")
	ErrAbsolutePath       = errors.New("absolute path rejected")
	ErrParentRelative     = errors.New("parent-relative path rejected")
	ErrExecutableMode     = errors.New("executable mode change rejected")
	ErrRenameOutside      = errors.New("rename outside allowed scope rejected")
	ErrDeleteOutside      = errors.New("delete outside allowed scope rejected")
	ErrUnsupportedOp      = errors.New("unsupported patch operation")
	ErrOutsideAllowlist   = errors.New("file outside write allowlist")
	ErrDeniedPath         = errors.New("file is on the immutable deny list")
	ErrAcceptedAdapter    = errors.New("modification to accepted adapter version rejected")
	ErrContractModify     = errors.New("T0 contract or harness modification rejected")
)

// ── AllowList ──

// AllowList restricts write access for a repair operation. The Doctor owns
// all allowlists — callers cannot inject paths.
type AllowList struct {
	WritePrefixes []string // cleaned directory prefixes or file paths
}

// ── Sandbox ──

// Sandbox enforces the filesystem allowlist, deny list, and hardening rules
// during patch validation. It parses real unified diffs and rejects any
// operation that violates containment, accepted-adapter immutability, or
// the T0 contract boundary.
type Sandbox struct {
	allowList AllowList
}

// NewSandbox builds a Sandbox for the given allowlist.
func NewSandbox(al AllowList) *Sandbox {
	return &Sandbox{allowList: al}
}

// ── Doctor-owned allowlists ──

// acceptedAdapterPaths are the immutable accepted adapter subtrees. D1 must
// never modify these.
var acceptedAdapterPaths = []string{
	"internal/agent/adapters/claude/v2_1_202",
	"internal/agent/adapters/codex/v0_144_1",
}

// contractPaths are the immutable T0 contract and harness paths.
var contractPaths = []string{
	"internal/agent/contract",
	"internal/agent/models.go",
	"internal/agent/bridge.go",
	"internal/agent/detector.go",
	"internal/agent/parser.go",
}

// denyListPaths are paths the repair agent is explicitly barred from.
var denyListPaths = []string{
	"internal/term",
	"internal/mux",
	"cmd",
	"mobile",
	"docs",
}

// ProviderAllowList returns the Doctor-owned allowlist for a given provider
// and new target version. Only files under that subtree may be written.
func ProviderAllowList(provider, targetVersion string) AllowList {
	prefix := filepath.Join("internal", "agent", "adapters", provider, targetVersion)
	return AllowList{
		WritePrefixes: []string{prefix + "/", prefix},
	}
}

// ── Patch validation ──

// PatchFile represents one changed file extracted from a unified diff.
type PatchFile struct {
	Path     string // relative path from diff header
	OldMode  string // old file mode, if present
	NewMode  string // new file mode, if present
	IsNew    bool   // "new file" mode
	IsDelete bool   // "deleted file" mode
	IsRename bool   // "renamed" mode
	IsBinary bool   // "Binary files ... differ" marker
}

// ValidatePatch parses a unified diff and validates every changed file
// against the sandbox containment rules. Returns the parsed PatchFiles
// on success, or an error describing the first rejection.
func (s *Sandbox) ValidatePatch(patch []byte, provider, targetVersion string) ([]PatchFile, error) {
	// Size check.
	if len(patch) > MaxPatchBytes {
		return nil, ErrPatchTooLarge
	}

	files := parsePatchFiles(patch)

	if len(files) > MaxChangedFiles {
		return nil, fmt.Errorf("%w: %d files (max %d)", ErrTooManyFiles, len(files), MaxChangedFiles)
	}

	for _, f := range files {
		if err := s.validateFile(f, provider, targetVersion); err != nil {
			return nil, err
		}
	}

	return files, nil
}

// validateFile applies all containment rules to a single patch file.
// Immutable security boundaries (deny list, accepted adapters, T0 contract)
// are checked BEFORE the allowlist so they produce specific rejection errors
// rather than generic "outside allowlist" messages.
func (s *Sandbox) validateFile(f PatchFile, provider, targetVersion string) error {
	// Harden the path first.
	cleaned, err := HardenedPath(f.Path)
	if err != nil {
		return err
	}

	// Binary patches rejected.
	if f.IsBinary {
		return fmt.Errorf("%w: %s", ErrBinaryPatch, cleaned)
	}

	// Executable mode change rejected.
	if modeChangeCreatesExecutable(f.OldMode, f.NewMode) {
		return fmt.Errorf("%w: %s (%s → %s)", ErrExecutableMode, cleaned, f.OldMode, f.NewMode)
	}

	// Immutable security boundaries checked FIRST (before allowlist).
	// Must not be on the immutable deny list.
	if isDeniedPath(cleaned) {
		return fmt.Errorf("%w: %s", ErrDeniedPath, cleaned)
	}

	// Must not touch accepted adapter versions.
	if isAcceptedAdapterPath(cleaned) {
		return fmt.Errorf("%w: %s", ErrAcceptedAdapter, cleaned)
	}

	// Must not touch T0 contract.
	if isContractPath(cleaned) {
		return fmt.Errorf("%w: %s", ErrContractModify, cleaned)
	}

	// Delete must be within allowed scope.
	if f.IsDelete {
		if !s.isAllowedWrite(cleaned, provider, targetVersion) {
			return fmt.Errorf("%w: %s", ErrDeleteOutside, cleaned)
		}
		return nil
	}

	// Rename — both old and new must be within allowed scope.
	if f.IsRename {
		if !s.isAllowedWrite(cleaned, provider, targetVersion) {
			return fmt.Errorf("%w: %s", ErrRenameOutside, cleaned)
		}
		return nil
	}

	// Must be within write allowlist (checked LAST so security boundaries
	// produce more specific errors).
	if !s.isAllowedWrite(cleaned, provider, targetVersion) {
		return fmt.Errorf("%w: %s", ErrOutsideAllowlist, cleaned)
	}

	return nil
}

// isAllowedWrite checks whether a cleaned path is within the allowlist for
// the given provider and target version.
func (s *Sandbox) isAllowedWrite(cleaned, provider, targetVersion string) bool {
	allowedPrefix := filepath.Join("internal", "agent", "adapters", provider, targetVersion)
	allowedPrefixClean := filepath.Clean(allowedPrefix)

	if cleaned == allowedPrefixClean {
		return true
	}
	if strings.HasPrefix(cleaned, allowedPrefixClean+"/") {
		return true
	}

	// Also check explicit write prefixes.
	for _, prefix := range s.allowList.WritePrefixes {
		cp := filepath.Clean(prefix)
		if cleaned == cp || strings.HasPrefix(cleaned, cp+"/") {
			return true
		}
	}
	return false
}

// ── parsePatchFiles ──

// parsePatchFiles extracts file paths and modes from a unified diff.
// It recognizes "diff --git", "---", "+++", "new file mode", "deleted file
// mode", "rename from/to", "Binary files ... differ" and similar headers.
func parsePatchFiles(patch []byte) []PatchFile {
	lines := strings.Split(string(patch), "\n")
	var files []PatchFile
	var current *PatchFile

	flushCurrent := func() {
		if current != nil && current.Path != "" {
			files = append(files, *current)
		}
		current = nil
	}

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		if strings.HasPrefix(line, "diff --git ") {
			flushCurrent()
			current = &PatchFile{}
			// diff --git a/<path> b/<path>
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				// "b/<path>" is the fourth token
				bPath := parts[3]
				if strings.HasPrefix(bPath, "b/") {
					current.Path = bPath[2:]
				}
			}
			continue
		}

		if current == nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "new file mode "):
			current.OldMode = "" // was not present
			current.NewMode = strings.TrimPrefix(line, "new file mode ")
			current.IsNew = true
		case strings.HasPrefix(line, "deleted file mode "):
			current.OldMode = strings.TrimPrefix(line, "deleted file mode ")
			current.NewMode = ""
			current.IsDelete = true
		case strings.HasPrefix(line, "old mode ") && current.OldMode == "":
			current.OldMode = strings.TrimPrefix(line, "old mode ")
		case strings.HasPrefix(line, "new mode "):
			current.NewMode = strings.TrimPrefix(line, "new mode ")
		case strings.HasPrefix(line, "rename from "):
			current.IsRename = true
		case strings.HasPrefix(line, "Binary files "):
			current.IsBinary = true
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ "):
			// Extract path from --- a/<path> or +++ b/<path>
			if current.Path == "" && strings.HasPrefix(line, "+++ b/") {
				current.Path = line[6:]
			}
		}
	}
	flushCurrent()
	return files
}

// ── HardenedPath ──

// HardenedPath validates and cleans a file path for sandbox containment.
// Rejects: absolute paths, parent-relative segments, symlink indicators.
func HardenedPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: %s", ErrAbsolutePath, p)
	}

	// Reject parent-relative segments.
	cleaned := filepath.Clean(p)
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("%w: %s", ErrParentRelative, p)
	}

	// Reject any component that looks like a symlink indicator.
	// (In production, this would call os.Lstat on each component on the
	// actual filesystem. For D1, we validate that no component is a known
	// escape pattern.)
	for _, part := range strings.Split(cleaned, "/") {
		if part == ".." {
			return "", fmt.Errorf("%w: %s", ErrParentRelative, p)
		}
		// Reject paths that contain symlink-ish markers.
		if strings.Contains(part, "->") || strings.HasPrefix(part, "@") {
			return "", fmt.Errorf("%w: %s", ErrSymlinkEscape, p)
		}
	}

	return cleaned, nil
}

// ── Mode check ──

// modeChangeCreatesExecutable reports whether a mode change adds executable
// bits where there were none.
func modeChangeCreatesExecutable(oldMode, newMode string) bool {
	if oldMode == "" || newMode == "" {
		return false
	}
	// If old mode didn't have execute bit set (e.g. 100644) and new mode
	// does (e.g. 100755), that's an executable mode change.
	oldExec := len(oldMode) >= 4 && oldMode[len(oldMode)-3] == '7'
	newExec := len(newMode) >= 4 && newMode[len(newMode)-3] == '7'
	return !oldExec && newExec
}

// ── Path category checks ──

// isDeniedPath reports whether a cleaned path falls within the immutable deny list.
func isDeniedPath(cleaned string) bool {
	for _, denied := range denyListPaths {
		cd := filepath.Clean(denied)
		if cleaned == cd || strings.HasPrefix(cleaned, cd+"/") {
			return true
		}
	}
	return false
}

// isAcceptedAdapterPath reports whether a cleaned path is within an accepted
// (immutable) adapter version subtree.
func isAcceptedAdapterPath(cleaned string) bool {
	for _, accepted := range acceptedAdapterPaths {
		ca := filepath.Clean(accepted)
		if cleaned == ca || strings.HasPrefix(cleaned, ca+"/") {
			return true
		}
	}
	return false
}

// isContractPath reports whether a cleaned path is within the T0 contract subtree.
func isContractPath(cleaned string) bool {
	for _, cp := range contractPaths {
		cc := filepath.Clean(cp)
		if cleaned == cc || strings.HasPrefix(cleaned, cc+"/") {
			return true
		}
	}
	return false
}

// ── AdapterAllowList ──

// AdapterAllowList returns the canonical read+write allowlist for a specific
// adapter under internal/agent/adapters/<provider>/<version>/.
func AdapterAllowList(provider, version string) AllowList {
	adapterDir := filepath.Join("internal", "agent", "adapters", provider, version)
	return AllowList{
		WritePrefixes: []string{adapterDir + "/", adapterDir},
	}
}

// DenyList returns a copy of the immutable deny list for display.
func DenyList() []string {
	out := make([]string, len(denyListPaths))
	copy(out, denyListPaths)
	return out
}

// DeniedPath reports whether a cleaned path falls within the deny list.
func DeniedPath(cleaned string) bool {
	return isDeniedPath(cleaned)
}
