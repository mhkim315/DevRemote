package term

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateLogPath(t *testing.T) {
	dir := t.TempDir()

	base := filepath.Join(dir, "base")
	os.MkdirAll(base, 0755)

	// Valid target
	targetValid := filepath.Join(base, "transcript.jsonl")
	if err := ValidateLogPath(base, targetValid); err != nil {
		t.Errorf("expected valid path to pass, got error: %v", err)
	}

	// Invalid extension
	targetInvalidExt := filepath.Join(base, "transcript.txt")
	if err := ValidateLogPath(base, targetInvalidExt); err == nil {
		t.Errorf("expected invalid extension to fail")
	}

	// Path traversal via string manipulation
	targetTraversal := filepath.Join(base, "..", "escaped.jsonl")
	if err := ValidateLogPath(base, targetTraversal); err == nil {
		t.Errorf("expected path traversal to fail")
	}

	// Symlink escape
	escapeTarget := filepath.Join(dir, "escaped.jsonl")
	f, _ := os.Create(escapeTarget)
	f.Close()

	symlinkPath := filepath.Join(base, "symlink.jsonl")
	os.Symlink(escapeTarget, symlinkPath)

	if err := ValidateLogPath(base, symlinkPath); err == nil {
		t.Errorf("expected symlink escape to fail")
	}
}
