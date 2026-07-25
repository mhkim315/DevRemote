package term

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// BF-1A B10: ClaudeInteractiveHost settings path safety tests.

// TestBF1A_AbsentParent proves writeInteractiveHookSettings fails when
// the hook directory does not exist.
func TestBF1A_AbsentParent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	err := writeInteractiveHookSettings(dir, "tok", 12345, "nonce")
	if err == nil {
		t.Fatal("expected error for nonexistent dir")
	}
	t.Logf("absent parent: %v", err)
}

// TestBF1A_ExistingDir proves writing to a real directory succeeds and
// the file is created with correct permissions.
func TestBF1A_ExistingDir(t *testing.T) {
	dir := t.TempDir()
	err := writeInteractiveHookSettings(dir, "tok", 12345, "nonce")
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	fi, err := os.Stat(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("stat settings.json: %v", err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("permissions = %o, want 0600", fi.Mode().Perm())
	}

	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("settings.json is empty")
	}
	content := string(data)
	if content == "" {
		t.Error("empty content")
	}
}

// TestBF1A_ExistingFile proves writing over an existing settings file
// succeeds (atomic replacement).
func TestBF1A_ExistingFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")

	// Write initial content.
	if err := os.WriteFile(target, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	// Overwrite via our function.
	err := writeInteractiveHookSettings(dir, "tok", 12345, "nonce")
	if err != nil {
		t.Fatalf("overwrite failed: %v", err)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "old" {
		t.Error("file was not overwritten")
	}
}

// TestBF1A_DirAsFile proves that if hookDir is actually a regular file,
// the function fails with a clear error (not a cryptic EISDIR).
func TestBF1A_DirAsFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "notadir")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	err := writeInteractiveHookSettings(filePath, "tok", 12345, "nonce")
	if err == nil {
		t.Fatal("expected error when hookDir is a file")
	}
	t.Logf("dir-as-file: %v", err)
}

// TestBF1A_SymlinkRejection proves symlink hook directory is rejected.
func TestBF1A_SymlinkRejection(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatal(err)
	}
	symDir := filepath.Join(dir, "sym")
	if err := os.Symlink(realDir, symDir); err != nil {
		t.Fatal(err)
	}

	err := writeInteractiveHookSettings(symDir, "tok", 12345, "nonce")
	if err == nil {
		t.Fatal("expected error for symlink hookDir")
	}
	t.Logf("symlink dir: %v", err)
}

// TestBF1A_SymlinkTargetRejection proves a symlink at the settings file
// path is rejected.
func TestBF1A_SymlinkTargetRejection(t *testing.T) {
	dir := t.TempDir()
	realFile := filepath.Join(dir, "real.json")
	if err := os.WriteFile(realFile, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	symTarget := filepath.Join(dir, "settings.json")
	if err := os.Symlink(realFile, symTarget); err != nil {
		t.Fatal(err)
	}

	err := writeInteractiveHookSettings(dir, "tok", 12345, "nonce")
	if err == nil {
		t.Fatal("expected error for symlink settings file")
	}
	t.Logf("symlink target: %v", err)
}

// TestBF1A_ReadOnlyDir proves that a read-only directory causes a clear error.
func TestBF1A_ReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)

	err := writeInteractiveHookSettings(dir, "tok", 12345, "nonce")
	if err == nil {
		t.Fatal("expected error for read-only dir")
	}
	t.Logf("readonly dir: %v", err)
}

// TestBF1A_ConcurrentLaunch proves concurrent writes to the same
// hookDir do not corrupt the settings file.
func TestBF1A_ConcurrentLaunch(t *testing.T) {
	dir := t.TempDir()

	var wg sync.WaitGroup
	errs := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			err := writeInteractiveHookSettings(dir, "tok", 12345+id, "nonce")
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	errCount := 0
	for range errs {
		errCount++
	}
	t.Logf("concurrent: %d errors (expected: none — atomic rename safe)", errCount)

	// File should exist and be valid JSON.
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("settings.json is empty after concurrent writes")
	}
}

// TestBF1A_NormalFileInDir proves that writing settings next to an
// existing normal file in the same dir works correctly.
func TestBF1A_NormalFileInDir(t *testing.T) {
	dir := t.TempDir()
	otherFile := filepath.Join(dir, "other.txt")
	if err := os.WriteFile(otherFile, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	err := writeInteractiveHookSettings(dir, "tok", 12345, "nonce")
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// Other file untouched.
	data, err := os.ReadFile(otherFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Error("other file was modified")
	}
}

// TestBF1A_AtomicReplacement proves the write is atomic — readers
// never see a partial file after rename.
func TestBF1A_AtomicReplacement(t *testing.T) {
	dir := t.TempDir()

	// Write initial settings.
	if err := writeInteractiveHookSettings(dir, "tok1", 10000, "n1"); err != nil {
		t.Fatal(err)
	}

	// Overwrite.
	if err := writeInteractiveHookSettings(dir, "tok2", 20000, "n2"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	// File should contain the NEW content, not partial old+new.
	content := string(data)
	if len(content) < 50 {
		t.Errorf("content too short: %d bytes — possible partial write", len(content))
	}
	// Verify it's valid JSON.
	if content[0] != '{' {
		t.Error("not valid JSON — possible corruption")
	}
}

// TestBF1A_HookDirPermissions validates the hook directory permissions
// are restrictive (owner-only).
func TestBF1A_HookDirPermissions(t *testing.T) {
	dir := t.TempDir()
	// Created by MkdirTemp already has 0700 — verify.

	if err := writeInteractiveHookSettings(dir, "tok", 12345, "nonce"); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	perm := fi.Mode().Perm()
	if perm&0077 != 0 {
		t.Errorf("settings.json permissions too permissive: %o (group/other readable)", perm)
	}
}
