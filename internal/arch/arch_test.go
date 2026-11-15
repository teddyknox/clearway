// Package arch checks the dependency rules of the design.
package arch

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/teddyknox/clearway/internal/"

// files parses the non-test files of a package of this module.
func files(t *testing.T, dir string) (*token.FileSet, []*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []*ast.File
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		t.Fatalf("no Go files in %s", dir)
	}
	return fset, out
}

// imports returns the import paths of the non-test files of a package.
func imports(t *testing.T, dir string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	_, fs := files(t, dir)
	for _, f := range fs {
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			out[p] = true
		}
	}
	return out
}

// only fails for each import of dir that is in this module and not allowed,
// and for each import that brings in a random number or a clock.
func only(t *testing.T, dir, why string, allowed ...string) {
	t.Helper()
	ok := map[string]bool{}
	for _, a := range allowed {
		ok[module+a] = true
	}
	for p := range imports(t, dir) {
		if strings.HasPrefix(p, module) && !ok[p] {
			t.Errorf("%s imports %s", dir, p)
		}
		if strings.HasPrefix(p, "math/rand") || p == "time" || p == "crypto/rand" {
			t.Errorf("%s imports %s: %s", dir, p, why)
		}
	}
}

// The path of the kernel: what a replica runs, and what a replay of a record
// runs.
var kernelPath = []string{"fx", "traj", "kernel"}

// The kernel is Tier A. It may use its own arithmetic and trajectories. It
// must not depend on the planner, the simulator, or the Safety Net, and it
// must not use random numbers or the wall clock.
func TestKernelDependencies(t *testing.T) {
	const why = "Tier A must be deterministic"
	only(t, "../fx", why)
	only(t, "../traj", why, "fx")
	only(t, "../kernel", why, "fx", "traj")
}

// The kernel computes with integers only (specification, section 1). A
// floating-point value rounds, and how it rounds can depend on the processor
// and on the compiler; an integer does not. So on the path of the kernel there
// is no floating-point type, no floating-point literal, and no import of
// package math. (math/bits is integer arithmetic.)
func TestKernelHasNoFloatingPoint(t *testing.T) {
	for _, name := range kernelPath {
		fset, fs := files(t, "../"+name)
		for _, f := range fs {
			for _, im := range f.Imports {
				if p, _ := strconv.Unquote(im.Path.Value); p == "math" || p == "math/cmplx" || p == "math/big" {
					t.Errorf("%s: imports %s", fset.Position(im.Pos()), p)
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.Ident:
					switch n.Name {
					case "float32", "float64", "complex64", "complex128":
						t.Errorf("%s: %s on the path of the kernel", fset.Position(n.Pos()), n.Name)
					}
				case *ast.BasicLit:
					if n.Kind == token.FLOAT || n.Kind == token.IMAG {
						t.Errorf("%s: the literal %s is not an integer", fset.Position(n.Pos()), n.Value)
					}
				}
				return true
			})
		}
	}
}

// The kernel in Rust has the same rule, and two more: it has no dependency,
// and it has no unsafe code. Only the small program around it (guest/rv),
// which talks to the machine and owns the heap, may use unsafe code.
func TestRustKernelRules(t *testing.T) {
	dir := "../../guest/kernel"
	manifest, err := os.ReadFile(filepath.Join(dir, "Cargo.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(manifest), "dependencies") {
		t.Error("guest/kernel/Cargo.toml names a dependency")
	}
	lib, err := os.ReadFile(filepath.Join(dir, "src", "lib.rs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, attr := range []string{"#![no_std]", "#![forbid(unsafe_code)]"} {
		if !strings.Contains(string(lib), attr) {
			t.Errorf("guest/kernel/src/lib.rs lacks %s", attr)
		}
	}
	float := regexp.MustCompile(`\bf(32|64)\b|\d\.\d`)
	text := regexp.MustCompile(`"[^"]*"`) // a string may hold a version number
	sources, _ := filepath.Glob(filepath.Join(dir, "src", "*.rs"))
	if len(sources) < 5 {
		t.Fatalf("found %d Rust files in guest/kernel/src", len(sources))
	}
	for _, path := range sources {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if float.MatchString(text.ReplaceAllString(code, `""`)) {
				t.Errorf("%s:%d: floating point in the kernel: %s", filepath.Base(path), i+1, strings.TrimSpace(line))
			}
		}
	}
}

// The record is checked by parties outside the operator. It may use the
// kernel, which it replays, and nothing above the kernel. What it writes must
// depend on the kernel's inputs alone, so it uses no random number and no
// wall clock.
func TestRecordDependencies(t *testing.T) {
	only(t, "../record", "a record must depend on the kernel's inputs alone", "fx", "traj", "kernel")
}

// The machine depends on nothing in this module: it runs any program. The
// kernel program depends on the machine alone.
func TestMachineDependencies(t *testing.T) {
	const why = "the machine must be deterministic"
	only(t, "../rvm", why)
	only(t, "../kimage", why, "rvm")
}

// The Tactical Safety Net shares no code with the rest of the system.
func TestSafetyNetIsIndependent(t *testing.T) {
	for p := range imports(t, "../safetynet") {
		if strings.HasPrefix(p, module) {
			t.Errorf("safety net imports %s", p)
		}
	}
}

// The two Verifier implementations do not call each other's geometry. They
// share the tube, and each states the separation rule in its own code.
func TestVerifierImplementationsAreSeparate(t *testing.T) {
	a, err := os.ReadFile("../kernel/analytic.go")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../kernel/sampled.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"firstHorizontal", "firstTrue", "bandOver(", "bandAt(", "events(", "vertEvent", "reqPM(", "vertSeparated("} {
		if strings.Contains(string(b), name) {
			t.Errorf("sampled.go uses %s from the analytic implementation", name)
		}
	}
	for _, name := range []string{"occupied(", ".SpeedOver(", ".node(", "ISqrt"} {
		if strings.Contains(string(a), name) {
			t.Errorf("analytic.go uses %s from the sampled implementation", name)
		}
	}
}
