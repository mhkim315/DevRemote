package doctor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Workspace is an isolated directory containing the full accepted baseline
// plus the patched candidate adapter.
type Workspace struct {
	Root         string
	CandidatePkg string // relative package path for go test

	// postPatchDigest is the source-tree digest computed immediately after
	// patch application. It is immutable once set — any source mutation
	// after this point is a contract violation.
	postPatchDigest string
}

func (w *Workspace) Cleanup() error {
	if w.Root != "" {
		return os.RemoveAll(w.Root)
	}
	return nil
}

// Digest returns the post-patch source-tree digest.
func (w *Workspace) Digest() string { return w.postPatchDigest }

// PrepareWorkspace copies the full agent tree from repoRoot into a temp dir,
// pre-places the Pokit-owned conformance test, applies the validated patch,
// and returns the workspace. No source files may be written after this point.
func PrepareWorkspace(repoRoot string, ops []PatchOperation, provider, targetDir string) (*Workspace, error) {
	tmpDir, err := os.MkdirTemp("", "d1-workspace-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create workspace: %w", err)
	}

	// Copy full repo tree (needed for go build/test in workspace).
	if err := copyDir(repoRoot, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("copy repo: %w", err)
	}

	// Pre-place Pokit-owned conformance test BEFORE patch application.
	// The candidate adapter patch must NOT include _test.go files (enforced
	// by the sandbox). This test file is immutable from the candidate's
	// perspective — if the patch tries to overwrite it, the sandbox rejects
	// the patch. If the patch creates a different file that would shadow it,
	// the sandbox rejects _test.go paths entirely.
	candidateDir := filepath.Join(tmpDir, "internal", "agent", "adapters", provider, targetDir)
	if err := os.MkdirAll(candidateDir, 0755); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("candidate dir: %w", err)
	}
	if err := writeConformanceDriver(repoRoot, candidateDir, targetDir); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("conformance driver: %w", err)
	}

	// Apply patch (adapter implementation only).
	if err := ApplyPatch(tmpDir, ops); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("apply patch: %w", err)
	}

	// Compute post-patch source-tree digest immediately. Any source mutation
	// after this point is detected by the orchestrator.
	digest := workspaceDigest(tmpDir)

	candidatePkg := "./internal/agent/adapters/" + provider + "/" + targetDir + "/"
	return &Workspace{Root: tmpDir, CandidatePkg: candidatePkg, postPatchDigest: digest}, nil
}

// writeConformanceDriver writes the Pokit-owned conformance test file into
// the candidate directory. The package name matches the target directory.
// The template is read from testdata/conformance_test.go.tpl and the
// PACKAGE_NAME placeholder is substituted.
func writeConformanceDriver(repoRoot, candidateDir, targetDir string) error {
	tplPath := filepath.Join(repoRoot, "internal", "agent", "doctor", "testdata", "conformance_test.go.tpl")
	tpl, err := os.ReadFile(tplPath)
	if err != nil {
		return fmt.Errorf("read conformance template: %w", err)
	}
	content := strings.ReplaceAll(string(tpl), "PACKAGE_NAME", targetDir)

	path := filepath.Join(candidateDir, "conformance_test.go")
	return os.WriteFile(path, []byte(content), 0644)
}

func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		// Skip .git and vendor directories.
		if e.IsDir() && (e.Name() == ".git" || e.Name() == "vendor" || e.Name() == "dist") {
			continue
		}
		sp := filepath.Join(src, e.Name())
		dp := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyDir(sp, dp); err != nil {
				return err
			}
		} else {
			if err := copyFile(sp, dp); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	os.MkdirAll(filepath.Dir(dst), 0755)
	return os.WriteFile(dst, data, 0644)
}

// workspaceDigest computes a deterministic SHA-256 hash of all tracked source
// files in the workspace (skips .git, vendor, dist, and build artifacts).
func workspaceDigest(root string) string {
	h := sha256.New()
	var paths []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := filepath.Base(p)
			if base == ".git" || base == "vendor" || base == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip binary / generated files.
		name := d.Name()
		if strings.HasSuffix(name, ".o") || strings.HasSuffix(name, ".a") ||
			strings.HasSuffix(name, ".exe") || strings.HasSuffix(name, ".test") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		paths = append(paths, rel)
		return nil
	})
	sort.Strings(paths)
	for _, p := range paths {
		data, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			continue
		}
		h.Write([]byte(p))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// RecreateWorkspace independently creates a fresh workspace from the same
// baseline and exact patch, then returns its digest. Used to verify that
// the reviewed patch deterministically reproduces the tested workspace.
func RecreateWorkspace(repoRoot string, ops []PatchOperation, provider, targetDir string) (string, error) {
	ws, err := PrepareWorkspace(repoRoot, ops, provider, targetDir)
	if err != nil {
		return "", err
	}
	defer ws.Cleanup()
	return ws.Digest(), nil
}
