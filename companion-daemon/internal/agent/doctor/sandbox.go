package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	ErrOutsideAllowlist = errors.New("file outside write allowlist")
	ErrDeniedPath       = errors.New("file is on the immutable deny list")
	ErrAcceptedAdapter  = errors.New("modification to accepted adapter version rejected")
	ErrContractModify   = errors.New("T0 contract or harness modification rejected")
	ErrSymlinkEscape    = errors.New("symlink escape detected")
	ErrAbsolutePath     = errors.New("absolute path rejected")
	ErrParentRelative   = errors.New("parent-relative path rejected")
	ErrProviderInvalid  = errors.New("provider not in closed vocabulary")
	ErrVersionInvalid   = errors.New("target version fails grammar")
	ErrTestFileRejected = errors.New("_test.go files rejected — candidate must not provide its own acceptance test")
)

// ── Closed vocabulary ──

var validProviders = map[string]bool{"claude": true, "codex": true}

var versionGrammar = regexp.MustCompile(`^v?[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

func ValidateProvider(p string) error {
	if !validProviders[p] {
		return fmt.Errorf("%w: %q", ErrProviderInvalid, p)
	}
	return nil
}

// CanonicalVersion converts a raw observed version to a safe directory name.
func CanonicalVersion(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("%w: empty version", ErrVersionInvalid)
	}
	if strings.Contains(raw, "/") || strings.Contains(raw, "\\") || strings.Contains(raw, "..") {
		return "", fmt.Errorf("%w: traversal in version %q", ErrVersionInvalid, raw)
	}
	v := strings.ReplaceAll(raw, ".", "_")
	v = strings.ReplaceAll(v, "-", "_")
	if !strings.HasPrefix(v, "v") && !strings.HasPrefix(v, "V") {
		v = "v" + v
	}
	if !versionGrammar.MatchString(v) {
		return "", fmt.Errorf("%w: %q", ErrVersionInvalid, v)
	}
	return v, nil
}

// ── Immutable paths ──

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

// ── Sandbox ──

type Sandbox struct {
	workspaceRoot string // owned, never "."
	provider      string
	targetDir     string // canonical version directory name
}

func NewSandbox(workspaceRoot, provider, targetVersion string) (*Sandbox, error) {
	if err := ValidateProvider(provider); err != nil {
		return nil, err
	}
	dir, err := CanonicalVersion(targetVersion)
	if err != nil {
		return nil, err
	}
	return &Sandbox{workspaceRoot: workspaceRoot, provider: provider, targetDir: dir}, nil
}

// Validate checks all PatchOperations against containment rules.
func (s *Sandbox) Validate(ops []PatchOperation) error {
	if len(ops) == 0 {
		return ErrEmptyPatch
	}
	for i := range ops {
		if err := s.validateOp(&ops[i]); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sandbox) validateOp(op *PatchOperation) error {
	if op.IsBinary {
		return fmt.Errorf("%w: %s", ErrBinaryPatch, op.DiffPath)
	}
	if op.IsRename {
		return fmt.Errorf("%w: %s", ErrRenameRejected, op.DiffPath)
	}
	if op.IsDelete {
		return fmt.Errorf("%w: %s", ErrDeleteRejected, op.DiffPath)
	}
	if op.IsNew && hasExecBit(op.NewMode) {
		return fmt.Errorf("%w: %s (new file mode %s)", ErrExecutableMode, op.DiffPath, op.NewMode)
	}
	if !op.IsNew && modeChangeCreatesExecutable(op.OldMode, op.NewMode) {
		return fmt.Errorf("%w: %s (%s→%s)", ErrExecutableMode, op.DiffPath, op.OldMode, op.NewMode)
	}
	// Candidate must not provide its own acceptance test or fixtures.
	if strings.HasSuffix(op.DiffPath, "_test.go") {
		return fmt.Errorf("%w: %s", ErrTestFileRejected, op.DiffPath)
	}

	path := op.DiffPath
	cleaned, err := HardenedPath(s.workspaceRoot, path)
	if err != nil {
		return err
	}

	if isAcceptedAdapterPath(cleaned) {
		return fmt.Errorf("%w: %s", ErrAcceptedAdapter, cleaned)
	}
	if isContractPath(cleaned) {
		return fmt.Errorf("%w: %s", ErrContractModify, cleaned)
	}
	if !s.isAllowedWrite(cleaned) {
		return fmt.Errorf("%w: %s", ErrOutsideAllowlist, cleaned)
	}
	return nil
}

func (s *Sandbox) isAllowedWrite(cleaned string) bool {
	allowed := filepath.Join("internal", "agent", "adapters", s.provider, s.targetDir)
	ac := filepath.Clean(allowed)
	return cleaned == ac || strings.HasPrefix(cleaned, ac+"/")
}

// ── HardenedPath ──

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

	components := strings.Split(cleaned, "/")
	accum := repoRoot
	for _, comp := range components {
		if comp == "" || comp == "." || comp == ".." {
			continue
		}
		accum = filepath.Join(accum, comp)
		fi, err := os.Lstat(accum)
		if err != nil {
			if os.IsNotExist(err) {
				continue // new files/dirs allowed
			}
			return "", fmt.Errorf("cannot stat %s: %w", comp, err)
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: %s", ErrSymlinkEscape, accum)
		}
	}
	return cleaned, nil
}

func hasExecBit(mode string) bool {
	if len(mode) < 3 {
		return false
	}
	return strings.ContainsAny(mode[len(mode)-3:], "7531")
}

func modeChangeCreatesExecutable(oldMode, newMode string) bool {
	if oldMode == "" || newMode == "" {
		return false
	}
	return !hasExecBit(oldMode) && hasExecBit(newMode)
}

func isAcceptedAdapterPath(cleaned string) bool {
	for _, a := range acceptedAdapterPaths {
		ca := filepath.Clean(a)
		if cleaned == ca || strings.HasPrefix(cleaned, ca+"/") {
			return true
		}
	}
	return false
}

func isContractPath(cleaned string) bool {
	for _, c := range contractPaths {
		cc := filepath.Clean(c)
		if cleaned == cc || strings.HasPrefix(cleaned, cc+"/") {
			return true
		}
	}
	return false
}
