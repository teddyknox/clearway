package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os/exec"
	"reflect"
	"testing"
	"time"

	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/kernel"
)

const suiteFile = "../../conformance/verifier-0.1.json"

func load(t *testing.T) *Suite {
	t.Helper()
	s, err := Load(suiteFile)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustPass(t *testing.T, s *Suite, name string, v Verifier) *Result {
	t.Helper()
	res, err := Run(s, v)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	for i, f := range res.Failures {
		if i == 5 {
			break
		}
		t.Errorf("%s: %s: %s (answer %+v)", name, f.Case.ID, f.Reason, f.Answer)
	}
	if len(res.Failures) > 0 {
		t.Fatalf("%s fails %d of %d cases", name, len(res.Failures), res.Total)
	}
	return res
}

// The file in the repository is well formed, covers every group and every
// class, and survives a round trip through its own writer.
func TestSuiteFile(t *testing.T) {
	s := load(t)
	if s.Name != "clearway-verifier" || s.Spec != SpecVersion {
		t.Fatalf("suite %q for specification %q", s.Name, s.Spec)
	}
	want := Tolerance{Distance: Tol, Time: TimeTol, GuardDistance: GuardD, GuardTime: GuardT}
	if s.Tolerance != want {
		t.Fatalf("tolerances %+v, want %+v", s.Tolerance, want)
	}
	ids, classes, groups := map[string]bool{}, map[string]int{}, map[string]int{}
	for _, c := range s.Cases {
		if ids[c.ID] {
			t.Fatalf("duplicate id %s", c.ID)
		}
		ids[c.ID] = true
		classes[c.Expect.Class]++
		groups[c.Group]++
		if c.Expect.Class != Clear && (c.Expect.FirstMin == nil || c.Expect.FirstMax == nil) {
			t.Fatalf("%s: class %s without a time window", c.ID, c.Expect.Class)
		}
	}
	for _, class := range []string{Conflict, Clear, Either} {
		if classes[class] < 30 {
			t.Errorf("only %d cases of class %s", classes[class], class)
		}
	}
	for _, g := range GroupNames() {
		if groups[g] < 10 || Purpose(g) == "" {
			t.Errorf("group %s: %d cases, purpose %q", g, groups[g], Purpose(g))
		}
	}
	var buf bytes.Buffer
	if err := s.Write(&buf); err != nil {
		t.Fatal(err)
	}
	var again Suite
	if err := json.Unmarshal(buf.Bytes(), &again); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*s, again) {
		t.Fatal("the suite changes when it is written and read back")
	}
}

// Every stored class is confirmed by enumeration, which shares no geometry
// with the exact check that produced it.
func TestExpectationsAgreeWithEnumeration(t *testing.T) {
	for _, c := range load(t).Cases {
		found, _, _ := Dense(c.A.Kernel(1), c.B.Kernel(2), c.From, c.To, c.Sep.params(), 0.02)
		if c.Expect.Class == Conflict && !found {
			t.Errorf("%s: class conflict, and the enumeration finds none", c.ID)
		}
		if c.Expect.Class == Clear && found {
			t.Errorf("%s: class clear, and the enumeration finds a conflict", c.ID)
		}
	}
}

// Both of this module's implementations conform, and together they never
// disagree beyond the tolerance.
func TestBuiltinsConform(t *testing.T) {
	s := load(t)
	for _, name := range []string{"analytic", "sampled"} {
		v, err := BuiltinByName(name)
		if err != nil {
			t.Fatal(err)
		}
		res := mustPass(t, s, name, v)
		t.Logf("%s: %d cases; in the band it rejects %d of %d", name, res.Total, res.Rejected[Either], res.ByClass[Either])
	}
	d := &kernel.Dual{A: kernel.AnalyticChecker{}, B: kernel.SampledChecker{Step: 1}, Tol: Tol, TimeTol: TimeTol}
	for _, c := range s.Cases {
		if _, hard := d.Check(c.A.Kernel(1), c.B.Kernel(2), c.From, c.To, c.Sep.params()); hard {
			t.Errorf("%s: the two implementations disagree beyond the tolerance", c.ID)
		}
	}
	t.Logf("%d marginal disagreements, %d hard", d.Marginal, d.Hard)
}

// mutant is a Verifier with one deliberate defect.
type mutant func(c *Case) Answer

func (m mutant) Check(c *Case) (Answer, error) { return m(c), nil }

func exact(c *Case) Answer {
	a, _ := Builtin{kernel.AnalyticChecker{}}.Check(c)
	return a
}

// with returns the exact answer for a changed copy of the case.
func with(change func(c *Case)) mutant {
	return func(c *Case) Answer {
		cp := *c
		cp.A.VSegs = append([]VSeg(nil), c.A.VSegs...)
		cp.B.VSegs = append([]VSeg(nil), c.B.VSegs...)
		change(&cp)
		return exact(&cp)
	}
}

// oldClosedForm is the stationary-point formula that this project's analytic
// implementation first used. Rounding can make its discriminant negative on a
// collision course, and it then finds no minimum.
func oldClosedForm(c *Case) Answer {
	if len(c.A.Legs) != 1 || len(c.B.Legs) != 1 || len(c.A.VSegs) != 1 || len(c.B.VSegs) != 1 || c.A.VSegs[0].A0 != c.B.VSegs[0].A0 {
		return exact(c)
	}
	a, b := c.A.Kernel(1), c.B.Kernel(2)
	lo, hi := span(a, b, c.From, c.To)
	if hi <= lo {
		return Answer{ID: c.ID}
	}
	p := a.Traj.PosAt(lo).Sub(b.Traj.PosAt(lo))
	v := a.Traj.VelAt(lo).Sub(b.Traj.VelAt(lo))
	req, m, length := c.Sep.Sh+a.Radius(lo)+b.Radius(lo), a.K+b.K, hi-lo
	g := func(x float64) float64 { return p.Add(v.Scale(x)).Norm() - (req + m*x) }
	least := math.Min(g(0), g(length))
	vv, pv, pp := v.Dot(v), p.Dot(v), p.Dot(p)
	r1, r2, n := geom.SolveQuadratic(vv*(vv-m*m), 2*pv*(vv-m*m), pv*pv-m*m*pp)
	if n >= 1 && r1 > 0 && r1 < length {
		least = math.Min(least, g(r1))
	}
	if n == 2 && r2 > 0 && r2 < length {
		least = math.Min(least, g(r2))
	}
	if least >= 0 {
		return Answer{ID: c.ID}
	}
	return exact(c)
}

// A suite that passes every Verifier proves nothing. Each mutant below has
// one defect, and the suite must catch it. Where a group of vectors was built
// for that defect, the mutant must fail in that group.
func TestSuiteCatchesDefects(t *testing.T) {
	s := load(t)
	flat := func(tb *Tube) {
		for i := range tb.VSegs {
			tb.VSegs[i].A1 = tb.VSegs[i].A0
		}
	}
	cases := []struct {
		name, group string
		v           Verifier
	}{
		{"approves everything", "", mutant(func(c *Case) Answer { return Answer{ID: c.ID} })},
		{"rejects everything", "", mutant(func(c *Case) Answer { return Answer{ID: c.ID, Conflict: true, T: c.From} })},
		{"ignores the growth of the envelope", "parallel", with(func(c *Case) { c.A.K, c.B.K = 0, 0 })},
		{"ignores the early and late allowance", "vertical-timing", with(func(c *Case) { c.A.VMin, c.B.VMin = 0, 0 })},
		{"reads a transition as level flight", "basic", with(func(c *Case) { flat(&c.A); flat(&c.B) })},
		{"ignores the altitude tolerance", "", with(func(c *Case) { c.A.VU0, c.B.VU0, c.A.VURate, c.B.VURate = 0, 0, 0, 0 })},
		{"ignores growth of the vertical uncertainty", "intruder", with(func(c *Case) { c.A.VURate, c.B.VURate = 0, 0 })},
		{"ignores the vertical slack", "tightened", with(func(c *Case) { c.Sep.VertSlack = 0 })},
		{"checks beyond the end of the interval", "basic", with(func(c *Case) { c.To = math.Inf(1) })},
		{"checks before the start of the interval", "basic", with(func(c *Case) { c.From = math.Inf(-1) })},
		{"reports the conflict three seconds late", "restart", mutant(func(c *Case) Answer {
			a := exact(c)
			a.T += 3
			return a
		})},
		{"reports the conflict far too early", "", mutant(func(c *Case) Answer {
			a := exact(c)
			a.T = c.From
			return a
		})},
		// The first defect found in this project: a check that starts at the
		// beginning of a conflict reports its end.
		{"reports the end of a conflict that starts at once", "restart", mutant(func(c *Case) Answer {
			a := exact(c)
			if lo, _ := span(c.A.Kernel(1), c.B.Kernel(2), c.From, c.To); a.Conflict && a.T-lo < 2e-3 {
				a.T += 60
			}
			return a
		})},
		// The second: no minimum is found on a collision course.
		{"loses the minimum on a collision course", "collision", mutant(oldClosedForm)},
	}
	for _, tc := range cases {
		res, err := Run(s, tc.v)
		if err != nil {
			t.Fatal(err)
		}
		inGroup := 0
		for _, f := range res.Failures {
			if f.Case.Group == tc.group {
				inGroup++
			}
		}
		switch {
		case len(res.Failures) == 0:
			t.Errorf("a Verifier that %s passes the suite", tc.name)
		case tc.group != "" && inGroup == 0:
			t.Errorf("a Verifier that %s fails %d cases, and none in group %s", tc.name, len(res.Failures), tc.group)
		default:
			t.Logf("%-48s fails %3d cases (%d in %q)", tc.name, len(res.Failures), inGroup, tc.group)
		}
	}
}

func TestJudge(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	conflict := &Case{Expect: Expect{Class: Conflict, FirstMin: f(100), FirstMax: f(110)}}
	clear := &Case{Expect: Expect{Class: Clear}}
	either := &Case{Expect: Expect{Class: Either, FirstMin: f(100), FirstMax: f(500)}}
	for _, tc := range []struct {
		name string
		c    *Case
		ans  Answer
		ok   bool
	}{
		{"conflict found in the window", conflict, Answer{Conflict: true, T: 105}, true},
		{"conflict missed", conflict, Answer{}, false},
		{"conflict reported late", conflict, Answer{Conflict: true, T: 111}, false},
		{"conflict reported early", conflict, Answer{Conflict: true, T: 99}, false},
		{"conflict with no time", conflict, Answer{Conflict: true, T: math.NaN()}, false},
		{"clear approved", clear, Answer{}, true},
		{"clear rejected", clear, Answer{Conflict: true, T: 5}, false},
		{"band approved", either, Answer{}, true},
		{"band rejected in the window", either, Answer{Conflict: true, T: 300}, true},
		{"band rejected before the window", either, Answer{Conflict: true, T: 50}, false},
	} {
		if err := Judge(tc.c, tc.ans); (err == nil) != tc.ok {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

// A Verifier in another language, in another process, passes through the
// line protocol.
func TestOutsideVerifierInPython(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ext, err := Start(ctx, []string{py, "../../conformance/examples/python/verifier.py"})
	if err != nil {
		t.Fatal(err)
	}
	res := mustPass(t, load(t), "python example", ext)
	if err := ext.Close(); err != nil {
		t.Fatalf("the Verifier did not exit cleanly: %v", err)
	}
	t.Logf("python example: %d cases", res.Total)
}
