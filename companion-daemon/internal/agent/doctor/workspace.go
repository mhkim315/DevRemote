package doctor

import (
	"fmt"
	"os"
	"path/filepath"
)

// Workspace is an isolated directory containing the full accepted baseline
// plus the patched candidate adapter.
type Workspace struct {
	Root       string
	CandidatePkg string // relative package path for go test
}

func (w *Workspace) Cleanup() error {
	if w.Root != "" {
		return os.RemoveAll(w.Root)
	}
	return nil
}

// PrepareWorkspace copies the full agent tree from repoRoot into a temp dir,
// applies the validated patch, and returns the workspace. Preserves go.mod,
// go.sum, T0 contract, accepted T1/T2 adapters, and all required packages.
func PrepareWorkspace(repoRoot string, ops []PatchOperation, provider, targetDir string) (*Workspace, error) {
	tmpDir, err := os.MkdirTemp("", "d1-workspace-*")
	if err != nil {
		return nil, fmt.Errorf("cannot create workspace: %w", err)
	}

	// Copy full internal/agent tree.
	srcAgent := filepath.Join(repoRoot, "internal", "agent")
	dstAgent := filepath.Join(tmpDir, "internal", "agent")
	if err := copyDir(srcAgent, dstAgent); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("copy agent tree: %w", err)
	}

	// Copy go.mod and go.sum.
	for _, f := range []string{"go.mod", "go.sum"} {
		src := filepath.Join(repoRoot, f)
		if _, err := os.Stat(src); err == nil {
			if err := copyFile(src, filepath.Join(tmpDir, f)); err != nil {
				os.RemoveAll(tmpDir)
				return nil, fmt.Errorf("copy %s: %w", f, err)
			}
		}
	}

	// Copy internal/models.go (needed by agent package).
	modelsSrc := filepath.Join(repoRoot, "internal", "models.go")
	modelsDstDir := filepath.Join(tmpDir, "internal")
	os.MkdirAll(modelsDstDir, 0755)
	if _, err := os.Stat(modelsSrc); err == nil {
		copyFile(modelsSrc, filepath.Join(modelsDstDir, "models.go"))
	}

	// Apply patch.
	if err := ApplyPatch(tmpDir, ops); err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("apply patch: %w", err)
	}

	candidatePkg := "./internal/agent/adapters/" + provider + "/" + targetDir + "/"
	return &Workspace{Root: tmpDir, CandidatePkg: candidatePkg}, nil
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
