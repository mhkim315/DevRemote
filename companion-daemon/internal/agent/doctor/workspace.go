package doctor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Workspace is an isolated directory containing the full accepted baseline
// plus the patched candidate adapter.
type Workspace struct {
	Root         string
	CandidatePkg string // relative package path for go test
}

func (w *Workspace) Cleanup() error {
	if w.Root != "" {
		return os.RemoveAll(w.Root)
	}
	return nil
}

// Digest computes a deterministic hash of the workspace contents after patch
// application. Uses file listing with mod times as a lightweight content proxy.
func (w *Workspace) Digest() string {
	if w.Root == "" {
		return ""
	}
	return workspaceDigest(w.Root)
}

// PrepareWorkspace copies the full agent tree from repoRoot into a temp dir,
// applies the validated patch, and returns the workspace. Preserves go.mod,
// go.sum, T0 contract, accepted T1/T2 adapters, and all required packages.
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

// workspaceDigest computes a deterministic hash of files in the workspace.
// Walks the candidate adapter subtree and hashes file contents sorted by path.
func workspaceDigest(root string) string {
	h := sha256.New()
	var paths []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
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
