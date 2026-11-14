// Package arch checks the dependency rules of the design.
package arch

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// imports returns the import paths of the non-test files of a package.
func imports(t *testing.T, dir string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			out[p] = true
		}
	}
	if len(out) == 0 {
		t.Fatalf("no imports found in %s", dir)
	}
	return out
}

// The kernel is Tier A. It may use geometry and trajectories. It must not
// depend on the planner, the simulator, or the Safety Net, and it must not
// use random numbers or the wall clock.
func TestKernelDependencies(t *testing.T) {
	allowed := map[string]bool{"github.com/teddyknox/clearway/internal/geom": true, "github.com/teddyknox/clearway/internal/traj": true}
	for p := range imports(t, "../kernel") {
		if strings.HasPrefix(p, "github.com/teddyknox/clearway/") && !allowed[p] {
			t.Errorf("kernel imports %s", p)
		}
		if strings.HasPrefix(p, "math/rand") || p == "time" || p == "crypto/rand" {
			t.Errorf("kernel imports %s: Tier A must be deterministic", p)
		}
	}
}

// The Tactical Safety Net shares no code with the rest of the system.
func TestSafetyNetIsIndependent(t *testing.T) {
	for p := range imports(t, "../safetynet") {
		if strings.HasPrefix(p, "github.com/teddyknox/clearway/") {
			t.Errorf("safety net imports %s", p)
		}
	}
}

// The two Verifier implementations do not call each other's geometry.
func TestVerifierImplementationsAreSeparate(t *testing.T) {
	a, err := os.ReadFile("../kernel/analytic.go")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../kernel/sampled.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"horizCheck", "bandOver(", "events()", "vertEvent"} {
		if strings.Contains(string(b), name) {
			t.Errorf("sampled.go uses %s from the analytic implementation", name)
		}
	}
	for _, name := range []string{".BandOver(", ".MaxSpeedOver(", ".Sigma(", ".node("} {
		if strings.Contains(string(a), name) {
			t.Errorf("analytic.go uses %s from the sampled implementation", name)
		}
	}
}
