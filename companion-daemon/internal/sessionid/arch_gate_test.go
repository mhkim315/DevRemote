package sessionid

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PA2b keeps canonical session parsing in this package. The physical removal
// of the former lifecycle package means the remaining enforceable invariant is
// that production has no second first-colon parser.

func productionGoFiles(t *testing.T, roots ...string) []string {
	t.Helper()
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(files) == 0 {
		t.Fatal("architecture gate found no production files — wrong working directory?")
	}
	return files
}

func fileImports(t *testing.T, path string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var imports []string
	for _, imp := range f.Imports {
		imports = append(imports, strings.Trim(imp.Path.Value, `"`))
	}
	return imports
}

// Exactly one identity parser implementation exists. No production
// file outside internal/sessionid may split a session ID at the first colon.
func TestPA2b_ArchGate_NoDuplicateParser(t *testing.T) {
	needle := "SplitN(" // combined with a colon literal on the same line
	selfDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range productionGoFiles(t, "../../cmd", "../../internal") {
		abs, err := filepath.Abs(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(abs, selfDir+string(filepath.Separator)) {
			continue // the single allowed implementation
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, needle) && strings.Contains(line, `":"`) {
				t.Errorf("%s:%d: colon-split parser outside internal/sessionid: %s", path, i+1, strings.TrimSpace(line))
			}
		}
	}
}
