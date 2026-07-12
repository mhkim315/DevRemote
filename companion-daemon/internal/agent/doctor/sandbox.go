package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxPatchBytes   = 1 << 20 // 1 MiB
	MaxChangedFiles = 32
)

var (
	ErrPatchTooLarge       = errors.New("patch exceeds MaxPatchBytes")
	ErrTooManyFiles        = errors.New("patch touches too many files")
	ErrEmptyPatch          = errors.New("empty patch rejected")
	ErrMalformedPatch      = errors.New("malformed patch — no valid diff headers")
	ErrBinaryPatch         = errors.New("binary patch rejected")
	ErrSymlinkEscape       = errors.New("symlink escape detected")
	ErrHardlinkEscape      = errors.New("hard-link escape detected")
	ErrAbsolutePath        = errors.New("absolute path rejected")
	ErrParentRelative      = errors.New("parent-relative path rejected")
	ErrExecutableMode      = errors.New("executable mode change rejected")
	ErrRenameRejected      = errors.New("rename rejected")
	ErrDeleteRejected      = errors.New("delete rejected")
	ErrOutsideAllowlist    = errors.New("file outside write allowlist")
	ErrDeniedPath          = errors.New("file is on the immutable deny list")
	ErrAcceptedAdapter     = errors.New("modification to accepted adapter version rejected")
	ErrContractModify      = errors.New("T0 contract or harness modification rejected")
)

// ── AllowList ──

type AllowList struct {
	WritePrefixes []string
}

// ── Sandbox ──

type Sandbox struct {
	allowList AllowList
}

func NewSandbox(al AllowList) *Sandbox { return &Sandbox{allowList: al} }

// ── Doctor-owned allowlists ──

var acceptedAdapterPaths = []string{
	"internal/agent/adapters/claude/v2_1_202",
	"internal/agent/adapters/codex/v0_144_1",
}

var contractPaths = []string{
	"internal/agent/contract",
	"internal/agent/models.go",
	"internal/agent/bridge.go",
	"internal/agent/detector.go",
	"internal/agent/parser.go",
}

var denyListPaths = []string{
	"internal/term",
	"internal/mux",
	"cmd",
	"mobile",
	"docs",
}

func ProviderAllowList(provider, targetVersion string) AllowList {
	prefix := filepath.Join("internal", "agent", "adapters", provider, targetVersion)
	return AllowList{WritePrefixes: []string{prefix + "/", prefix}}
}

// ── PatchFile ──

type PatchFile struct {
	Path     string
	OldMode  string
	NewMode  string
	IsNew    bool
	IsDelete bool
	IsRename bool
	IsBinary bool
}

// ── ValidatePatch ──

func (s *Sandbox) ValidatePatch(patch []byte, provider, targetVersion string) ([]PatchFile, error) {
	if len(patch) == 0 {
		return nil, ErrEmptyPatch
	}
	if len(patch) > MaxPatchBytes {
		return nil, ErrPatchTooLarge
	}

	files, err := parsePatchFiles(patch)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, ErrMalformedPatch
	}
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

func (s *Sandbox) validateFile(f PatchFile, provider, targetVersion string) error {
	cleaned, err := HardenedPath(".", f.Path)
	if err != nil {
		return err
	}

	// REJECT: binary always.
	if f.IsBinary {
		return fmt.Errorf("%w: %s", ErrBinaryPatch, cleaned)
	}

	// REJECT: rename always.
	if f.IsRename {
		return fmt.Errorf("%w: %s", ErrRenameRejected, cleaned)
	}

	// REJECT: delete always.
	if f.IsDelete {
		return fmt.Errorf("%w: %s", ErrDeleteRejected, cleaned)
	}

	// REJECT: new file with executable mode.
	if f.IsNew && hasExecBit(f.NewMode) {
		return fmt.Errorf("%w: %s (new file mode %s)", ErrExecutableMode, cleaned, f.NewMode)
	}

	// REJECT: existing file mode change adding executable bit.
	if !f.IsNew && modeChangeCreatesExecutable(f.OldMode, f.NewMode) {
		return fmt.Errorf("%w: %s (%s → %s)", ErrExecutableMode, cleaned, f.OldMode, f.NewMode)
	}

	// Security boundaries BEFORE allowlist.
	if isDeniedPath(cleaned) {
		return fmt.Errorf("%w: %s", ErrDeniedPath, cleaned)
	}
	if isAcceptedAdapterPath(cleaned) {
		return fmt.Errorf("%w: %s", ErrAcceptedAdapter, cleaned)
	}
	if isContractPath(cleaned) {
		return fmt.Errorf("%w: %s", ErrContractModify, cleaned)
	}

	if !s.isAllowedWrite(cleaned, provider, targetVersion) {
		return fmt.Errorf("%w: %s", ErrOutsideAllowlist, cleaned)
	}

	return nil
}

func (s *Sandbox) isAllowedWrite(cleaned, provider, targetVersion string) bool {
	allowed := filepath.Join("internal", "agent", "adapters", provider, targetVersion)
	ac := filepath.Clean(allowed)
	if cleaned == ac || strings.HasPrefix(cleaned, ac+"/") {
		return true
	}
	for _, prefix := range s.allowList.WritePrefixes {
		cp := filepath.Clean(prefix)
		if cleaned == cp || strings.HasPrefix(cleaned, cp+"/") {
			return true
		}
	}
	return false
}

// ── parsePatchFiles ──

func parsePatchFiles(patch []byte) ([]PatchFile, error) {
	text := string(patch)
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmptyPatch
	}

	lines := strings.Split(text, "\n")
	var files []PatchFile
	var current *PatchFile
	hasHeader := false

	flush := func() {
		if current != nil && current.Path != "" {
			files = append(files, *current)
		}
		current = nil
	}

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")

		if strings.HasPrefix(line, "diff --git ") {
			flush()
			hasHeader = true
			current = &PatchFile{}
			current.Path = parseDiffGitPath(line)
			continue
		}

		if current == nil {
			continue
		}

		switch {
		case strings.HasPrefix(line, "new file mode "):
			current.NewMode = strings.TrimPrefix(line, "new file mode ")
			current.IsNew = true
		case strings.HasPrefix(line, "deleted file mode "):
			current.OldMode = strings.TrimPrefix(line, "deleted file mode ")
			current.IsDelete = true
		case strings.HasPrefix(line, "old mode ") && current.OldMode == "":
			current.OldMode = strings.TrimPrefix(line, "old mode ")
		case strings.HasPrefix(line, "new mode ") && current.NewMode == "":
			current.NewMode = strings.TrimPrefix(line, "new mode ")
		case strings.HasPrefix(line, "rename from ") || strings.HasPrefix(line, "rename to "):
			current.IsRename = true
		case strings.HasPrefix(line, "Binary files "):
			current.IsBinary = true
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ "):
			if current.Path == "" {
				if p := parseTriplePlusPath(line); p != "" {
					current.Path = p
				}
			}
		}
	}
	flush()

	if !hasHeader {
		return nil, ErrMalformedPatch
	}
	return files, nil
}

// parseDiffGitPath extracts the "b/" path from "diff --git a/<path> b/<path>".
// Handles git-style quoted paths (paths starting/ending with ").
func parseDiffGitPath(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	parts := parseGitDiffPaths(rest)
	if len(parts) >= 2 {
		return parts[1] // "b/" path
	}
	// Fallback: space-split
	fields := strings.Fields(rest)
	if len(fields) >= 4 && strings.HasPrefix(fields[3], "b/") {
		return fields[3][2:]
	}
	return ""
}

// parseGitDiffPaths splits "a/<path> b/<path>" handling quoted paths.
func parseGitDiffPaths(s string) []string {
	var result []string
	for len(s) > 0 {
		s = strings.TrimLeft(s, " \t")
		if len(s) == 0 {
			break
		}
		if s[0] == '"' {
			path, rest := parseQuotedPath(s[1:])
			result = append(result, path)
			s = rest
		} else {
			end := strings.IndexAny(s, " \t")
			if end < 0 {
				result = append(result, s)
				break
			}
			result = append(result, s[:end])
			s = s[end:]
		}
	}
	// Strip "a/" and "b/" prefixes if present.
	for i, p := range result {
		if strings.HasPrefix(p, "a/") {
			result[i] = p[2:]
		} else if strings.HasPrefix(p, "b/") {
			result[i] = p[2:]
		}
	}
	return result
}

// parseQuotedPath parses a git-quoted path starting after the opening ".
func parseQuotedPath(s string) (path string, rest string) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			return b.String(), s[i+1:]
		}
		if c == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case '\\', '"':
				b.WriteByte(next)
				i++
			case 't':
				b.WriteByte('\t')
				i++
			case 'n':
				b.WriteByte('\n')
				i++
			default:
				b.WriteByte('\\')
				b.WriteByte(next)
				i++
			}
		} else {
			b.WriteByte(c)
		}
	}
	return b.String(), ""
}

// parseTriplePlusPath extracts the path from "+++ b/<path>".
func parseTriplePlusPath(line string) string {
	rest := strings.TrimPrefix(line, "+++ b/")
	if rest == line {
		// Try without "b/" prefix.
		rest = strings.TrimPrefix(line, "+++ ")
	}
	// Strip trailing tab and timestamp.
	if idx := strings.IndexByte(rest, '\t'); idx >= 0 {
		rest = rest[:idx]
	}
	return rest
}

// ── HardenedPath with actual Lstat ──

func HardenedPath(repoRoot, relPath string) (string, error) {
	if relPath == "" {
		return "", errors.New("empty path")
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("%w: %s", ErrAbsolutePath, relPath)
	}

	cleaned := filepath.Clean(relPath)
	if strings.HasPrefix(cleaned, "..") || strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("%w: %s", ErrParentRelative, relPath)
	}

	// Verify each component against the filesystem with Lstat.
	components := strings.Split(cleaned, "/")
	accum := repoRoot
	for i, comp := range components {
		if comp == "" || comp == "." {
			continue
		}
		if comp == ".." {
			return "", fmt.Errorf("%w: %s", ErrParentRelative, relPath)
		}
		accum = filepath.Join(accum, comp)
		_ = i // checked for symlink below

		fi, err := os.Lstat(accum)
		if err != nil {
			if os.IsNotExist(err) {
				// Non-existent component: allowed for new files/dirs.
				continue
			}
			return "", fmt.Errorf("cannot stat %s: %w", comp, err)
		}

		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: %s is a symlink", ErrSymlinkEscape, accum)
		}
		// intermediate component exists and is not a symlink — ok
	}

	return cleaned, nil
}

// ── Mode checks ──

func hasExecBit(mode string) bool {
	return len(mode) >= 4 && strings.Contains(mode[len(mode)-3:], "7") ||
		len(mode) >= 4 && strings.Contains(mode[len(mode)-3:], "5") ||
		len(mode) >= 4 && strings.Contains(mode[len(mode)-3:], "3") ||
		len(mode) >= 4 && strings.Contains(mode[len(mode)-3:], "1")
}

func modeChangeCreatesExecutable(oldMode, newMode string) bool {
	if oldMode == "" || newMode == "" {
		return false
	}
	return !hasExecBit(oldMode) && hasExecBit(newMode)
}

// ── Path predicates ──

func isDeniedPath(cleaned string) bool {
	for _, denied := range denyListPaths {
		cd := filepath.Clean(denied)
		if cleaned == cd || strings.HasPrefix(cleaned, cd+"/") {
			return true
		}
	}
	return false
}

func isAcceptedAdapterPath(cleaned string) bool {
	for _, accepted := range acceptedAdapterPaths {
		ca := filepath.Clean(accepted)
		if cleaned == ca || strings.HasPrefix(cleaned, ca+"/") {
			return true
		}
	}
	return false
}

func isContractPath(cleaned string) bool {
	for _, cp := range contractPaths {
		cc := filepath.Clean(cp)
		if cleaned == cc || strings.HasPrefix(cleaned, cc+"/") {
			return true
		}
	}
	return false
}

// ── Public helpers ──

func DeniedPath(cleaned string) bool { return isDeniedPath(cleaned) }

func DenyList() []string {
	out := make([]string, len(denyListPaths))
	copy(out, denyListPaths)
	return out
}
