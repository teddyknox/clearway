package rvm

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestSourceIsDeterministic checks the rules that keep the machine the same
// on every host. The files of the package, other than its tests:
//
//   - import only packages that read no clock, no random number, and no
//     state of the host;
//   - use no floating-point type and no floating-point constant;
//   - start no goroutine.
func TestSourceIsDeterministic(t *testing.T) {
	allowed := map[string]bool{
		"crypto/sha256":   true,
		"encoding/binary": true,
		"errors":          true,
		"fmt":             true,
		"io":              true,
		// For Translate, which writes Go source.
		"bytes":     true,
		"go/format": true,
		"sort":      true,
		"strconv":   true,
		"strings":   true,
	}
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files++
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range f.Imports {
			if p, _ := strconv.Unquote(im.Path.Value); !allowed[p] {
				t.Errorf("%s imports %s", name, p)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.Ident:
				switch n.Name {
				case "float32", "float64", "complex64", "complex128":
					t.Errorf("%s: %s", fset.Position(n.Pos()), n.Name)
				}
			case *ast.BasicLit:
				if n.Kind == token.FLOAT || n.Kind == token.IMAG {
					t.Errorf("%s: floating-point constant %s", fset.Position(n.Pos()), n.Value)
				}
			case *ast.GoStmt:
				t.Errorf("%s: go statement", fset.Position(n.Pos()))
			}
			return true
		})
	}
	if files == 0 {
		t.Fatal("no source file found")
	}
}
