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

// PA2b architecture gates (focused test 5 + packet gates), executed as a
// repeatable test over the production source tree:
//
//  1. forbidden-import gate: internal/sessionid must not import internal/mux
//  2. duplicate-parser gate: no second identity parser (first-colon SplitN)
//     exists outside internal/sessionid in production code
//  3. consumer gate: managed/term production code no longer uses the
//     deprecated mux identity wrappers, and the files that imported
//     internal/mux SOLELY for identity parsing no longer import it at all

const muxImportPath = "devremote/companion-daemon/internal/mux"

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

// Gate 1: internal/sessionid must not import internal/mux (or anything under it).
func TestPA2b_ArchGate_ForbiddenImport(t *testing.T) {
	for _, path := range productionGoFiles(t, ".") {
		for _, imp := range fileImports(t, path) {
			if imp == muxImportPath || strings.HasPrefix(imp, muxImportPath+"/") {
				t.Errorf("%s imports %s — internal/sessionid must stay mux-free", path, imp)
			}
		}
	}
}

// Gate 2: exactly one identity parser implementation exists. No production
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

// Gate 3: managed catalog and term production consumers use internal/sessionid
// directly — no deprecated mux identity wrapper references remain in term
// production code, and the files that imported mux solely for identity
// parsing dropped the import entirely.
//
// Exemption: the §5 frozen-unaffected boundaries of
// docs/PA2_LIFECYCLE_TRANSPORT_CONTRACT.md must not be modified by ANY PA2
// sub-packet, and PA2b grants an import exception ONLY to managed_catalog.go
// (and optionally managed_api.go). Frozen files therefore keep calling the
// deprecated mux wrappers (behavior-identical thin delegates) until their
// PA4/PB migration; they are exempted here by exact filename.
func TestPA2b_ArchGate_TermConsumersUseSessionID(t *testing.T) {
	frozen := map[string]bool{
		"managed_codex.go":               true,
		"managed_claude.go":              true,
		"managed_approval_activation.go": true,
		"managed_claude_activation.go":   true,
		"managed_approval_delivery.go":   true,
		"claude_approval_delivery.go":    true,
		"approval_store_gen.go":          true,
		"approval_execution.go":          true,
		"managed_registry.go":            true,
	}
	deprecated := []string{"mux.ParseSessionID(", "mux.SessionRef{", "mux.ValidateAdapterName("}
	for _, path := range productionGoFiles(t, "../term") {
		if frozen[filepath.Base(path)] {
			continue // §5 frozen-unaffected boundary — untouched by PA2b
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		src := string(data)
		for _, ref := range deprecated {
			if strings.Contains(src, ref) {
				t.Errorf("%s still uses deprecated wrapper %s — must import internal/sessionid directly", path, ref)
			}
		}
	}
	// These files' ONLY mux dependency was identity parsing; after PA2b they
	// must not import internal/mux at all.
	for _, rel := range []string{"managed_catalog.go", "approval_delivery.go"} {
		path := filepath.Join("../term", rel)
		for _, imp := range fileImports(t, path) {
			if imp == muxImportPath {
				t.Errorf("%s imports internal/mux again — was identity-parsing-only before PA2b", path)
			}
		}
	}
}
