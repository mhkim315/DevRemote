// archgate verifies the PB.5a V1-only managed PTY lifecycle boundary.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

func main() {
	task := flag.Int("task", 1, "remediation task")
	flag.Parse()
	if *task != 1 {
		return
	}
	f, err := parser.ParseFile(token.NewFileSet(), "internal/term/owned_pty_runtime.go", nil, parser.AllErrors)
	if err != nil {
		fail(err.Error())
	}
	var spawnCall bool
	ast.Inspect(f, func(n ast.Node) bool {
		s, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if x, ok := s.X.(*ast.SelectorExpr); ok {
			if id, ok := x.X.(*ast.Ident); ok && id.Name == "o" && x.Sel.Name == "v1Spawn" && s.Sel.Name == "Spawn" {
				spawnCall = true
			}
		}
		if id, ok := s.X.(*ast.Ident); ok && id.Name == "mux" {
			fail("managed runtime imports mux selector")
		}
		return true
	})
	if !spawnCall {
		fail("Create path does not call o.v1Spawn.Spawn")
	}
	src, err := os.ReadFile("internal/term/owned_pty_runtime.go")
	if err != nil {
		fail(err.Error())
	}
	for _, forbidden := range []string{"ManagedPTYLauncher ", "o.spawn", "fallback", "NewV1FromOld"} {
		if strings.Contains(string(src), forbidden) {
			fail("forbidden legacy lifecycle reference: " + forbidden)
		}
	}
	fmt.Println("PASS: PB.5a V1-only managed lifecycle")
}
func fail(msg string) { fmt.Fprintln(os.Stderr, "FAIL:", msg); os.Exit(1) }
