package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/teddyknox/clearway/internal/conv"
	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/traj"
)

const suiteFile = "../../conformance/verifier-0.2.json"

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
	if s.Name != "clearway-verifier" || s.Spec != SpecVersion || s.Units != Units {
		t.Fatalf("suite %q for specification %q, in %q", s.Name, s.Spec, s.Units)
	}
	if want := (Tolerance{Distance: Tol, Time: TimeTol}); s.Tolerance != want {
		t.Fatalf("tolerance %+v, want %+v", s.Tolerance, want)
	}
	ids, classes, groups := map[string]bool{}, map[string]int{}, map[string]int{}
	for _, c := range s.Cases {
		if ids[c.ID] {
			t.Fatalf("duplicate id %s", c.ID)
		}
		ids[c.ID] = true
		classes[c.Expect.Class]++
		groups[c.Group]++
		e := c.Expect
		if e.Class != Clear && (e.FirstMin == nil || e.FirstMax == nil) {
			t.Fatalf("%s: class %s without a time window", c.ID, e.Class)
		}
		// The exact first conflict time is there for a conflict, and only
		// then. It is the end of the time window.
		if (e.Class == Conflict) != (e.ExactFirst != nil) || (e.ExactFirst != nil && *e.ExactFirst != *e.FirstMax) {
			t.Fatalf("%s: class %s with a wrong exact first conflict time", c.ID, e.Class)
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

// Every stored expectation is confirmed by enumeration, which shares no
// geometry with the exact implementation that produced it.
func TestExpectationsAgreeWithEnumeration(t *testing.T) {
	for _, c := range load(t).Cases {
		if err := Confirm(&c); err != nil {
			t.Error(err)
		}
	}
}

// The enumeration scans with the integer statement of the rule. It must say
// what the arbitrary-precision statement says: at random times of every
// vector, at the times around every start and end of a leg or a segment, and
// for tubes with no early or late allowance.
func TestFastRuleIsTheRule(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	compared, conflicts := 0, 0
	same := func(id string, a, b *Tube, sep Sep, at int64) {
		t.Helper()
		fast, exact := separatedFast(a, b, sep, at), SeparatedAt(a, b, sep, at)
		if fast != exact {
			t.Fatalf("%s at %d: the integer statement says separated=%v, and the arbitrary-precision statement says %v", id, at, fast, exact)
		}
		compared++
		if !exact {
			conflicts++
		}
	}
	for _, c := range load(t).Cases {
		lo, hi := c.interval()
		if hi <= lo {
			continue
		}
		times := []int64{lo, hi}
		for _, tb := range []*Tube{&c.A, &c.B} {
			for _, l := range tb.Legs {
				times = append(times, l.T0-1, l.T0, l.T1, l.T1+1)
			}
			for _, s := range tb.VSegs {
				times = append(times, s.T0-1, s.T0, s.T1, s.T1+1)
			}
		}
		for _, p := range []*int64{c.Expect.FirstMin, c.Expect.FirstMax} {
			if p != nil {
				times = append(times, *p-1, *p, *p+1)
			}
		}
		for i := 0; i < 20; i++ {
			times = append(times, lo+r.Int64N(hi-lo+1))
		}
		noAllowance := c
		noAllowance.A.VMin, noAllowance.B.VMin = 0, 0
		for _, at := range times {
			if at < lo || at > hi {
				continue
			}
			same(c.ID, &c.A, &c.B, c.Sep, at)
			same(c.ID, &c.A, &c.B, c.Sep.tightened(&c.A, &c.B), at)
			same(c.ID, &noAllowance.A, &noAllowance.B, c.Sep, at)
		}
	}
	if conflicts < 500 || compared-conflicts < 500 {
		t.Fatalf("weak mix: %d comparisons, %d not separated", compared, conflicts)
	}
	t.Logf("%d comparisons, %d not separated", compared, conflicts)
}

// The same at the limits of valid values, where the required distance can
// be above the largest that is compared.
func TestFastRuleIsTheRuleAtTheLimits(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	edge := func(limit int64) int64 {
		switch r.IntN(4) {
		case 0:
			return limit
		case 1:
			return -limit
		}
		return r.Int64N(2*limit+1) - limit
	}
	compared, conflicts := 0, 0
	for i := 0; compared < 20000; i++ {
		t0 := edge(fx.MaxTime - 4000*fx.Sec)
		mk := func(t0 int64) Tube {
			dur := 1 + r.Int64N(3600*fx.Sec)
			tb := Tube{
				Legs:   []Leg{{T0: t0, T1: t0 + dur, P0: [2]int64{edge(fx.MaxCoord), edge(fx.MaxCoord)}, V: [2]int64{edge(fx.MaxSpeed), edge(fx.MaxSpeed)}}},
				VSegs:  []VSeg{{T0: t0, T1: t0 + dur, A0: edge(fx.MaxCoord), A1: edge(fx.MaxCoord)}},
				Anchor: edge(fx.MaxTime),
				R0:     r.Int64N(fx.MaxLen + 1), K: r.Int64N(fx.MaxSpeed + 1),
				VU0: r.Int64N(fx.MaxLen + 1), VURate: r.Int64N(fx.MaxSpeed + 1), VMin: r.Int64N(2*fx.MaxSpeed + 1),
			}
			if r.IntN(3) == 0 {
				tb.R0, tb.K, tb.VU0, tb.VURate = r.Int64N(4*fx.NM), r.Int64N(20*fx.Kt), r.Int64N(500*fx.FT), 0
			}
			return tb
		}
		c := Case{A: mk(t0), B: mk(t0 + r.Int64N(600*fx.Sec) - 300*fx.Sec), From: -2 * fx.MaxTime, To: 2 * fx.MaxTime,
			Sep: Sep{Sh: 1 + r.Int64N(fx.MaxLen), SvTrue: r.Int64N(fx.MaxLen + 1), VertSlack: r.Int64N(10 * fx.Sec)}}
		if _, _, _, err := c.kernel(); err != nil {
			continue
		}
		lo, hi := c.interval()
		for j := 0; j < 10 && hi > lo; j++ {
			at := lo + r.Int64N(hi-lo+1)
			fast, exact := separatedFast(&c.A, &c.B, c.Sep, at), SeparatedAt(&c.A, &c.B, c.Sep, at)
			if fast != exact {
				t.Fatalf("pair %d at %d: the integer statement says separated=%v, and the arbitrary-precision statement says %v", i, at, fast, exact)
			}
			compared++
			if !exact {
				conflicts++
			}
		}
	}
	if conflicts < 500 || compared-conflicts < 500 {
		t.Fatalf("weak mix: %d comparisons, %d not separated", compared, conflicts)
	}
	t.Logf("%d comparisons, %d not separated", compared, conflicts)
}

// Under a slack w, a vertical segment is in effect at t if it is in effect,
// with no slack, at some whole microsecond from t − w to t + w
// (specification, section 3). Neither statement of the rule tries each of
// those microseconds: both search, and both rest on the reasons in the
// comment on inEffectWithin. This test tries each one, for tubes whose times
// are a few thousand microseconds, and holds both statements to the result:
// the vertical extent of a tube, and the answer for a pair.
//
// The pair is two aircraft at one point, so it is never horizontally
// separated. b is level and has no allowance. a starts at a level far from
// b, descends to the level of b, and climbs back. So the pair is separated
// exactly when the extent of a is the far level alone: before a transition
// comes into effect, and after both have gone out of effect.
func TestSlackIsEveryMicrosecond(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	const (
		start, end  = 0, 3500    // both trajectories (µs)
		first, last = -300, 3800 // the times at which the rule is tried with no slack
		near, far   = 1000, 2000 // the two levels (µm)
		svTrue      = far - near // the two levels are vertically separated, and no more
	)
	var changes, conflicts, compared int
	for i := 0; i < 120; i++ {
		// The times at which a starts and ends its two transitions.
		var at [4]int64
		at[0] = 1 + r.Int64N(1200)
		for k := 1; k < 4; k++ {
			at[k] = at[k-1] + 1 + r.Int64N(500)
		}
		leg := []Leg{{T0: start, T1: end}}
		a := Tube{
			Legs: leg,
			VSegs: []VSeg{
				{T0: start, T1: at[0], A0: far, A1: far},
				{T0: at[0], T1: at[1], A0: far, A1: near},
				{T0: at[1], T1: at[2], A0: near, A1: near},
				{T0: at[2], T1: at[3], A0: near, A1: far},
				{T0: at[3], T1: end, A0: far, A1: far},
			},
			Anchor: r.Int64N(3500) - 500,
			R0:     r.Int64N(300),                 // the aircraft covers its radius in up to 300 µs
			K:      r.Int64N(2_000_000),           // slower and faster than the aircraft
			VMin:   1_000_000 + r.Int64N(500_000), // about 1 pm in 1 µs
		}
		switch r.IntN(6) {
		case 0:
			a.VMin = 0
		case 1:
			a.K = a.VMin
		case 2:
			a.K = 0
		}
		b := Tube{Legs: leg, VSegs: []VSeg{{T0: start, T1: end, A0: near, A1: near}}}
		sep := Sep{Sh: 1, SvTrue: svTrue, VertSlack: r.Int64N(250)}
		c := Case{A: a, B: b, From: start, To: end, Sep: sep}
		if _, _, _, err := c.kernel(); err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		// in[k][u − first]: segment k of a is in effect at u, with no slack.
		var in [5][last - first + 1]bool
		for k := range a.VSegs {
			for u := int64(first); u <= last; u++ {
				in[k][u-first] = a.inEffect(&a.VSegs[k], u)
			}
		}
		w, prev := sep.VertSlack, true
		for t0 := int64(start); t0 <= end; t0++ {
			// The extent by a trial of every microsecond from t0 − w to t0 + w.
			var lo, hi int64
			ok := false
			for k, s := range a.VSegs {
				some := false
				for u := t0 - w; u <= t0+w && !some; u++ {
					some = in[k][u-first]
				}
				if !some {
					continue
				}
				if !ok {
					lo, hi, ok = min(s.A0, s.A1), max(s.A0, s.A1), true
				}
				lo, hi = min(lo, s.A0, s.A1), max(hi, s.A0, s.A1)
			}
			want := ok && lo == far
			where := func() string { return fmt.Sprintf("case %d (%+v, slack %d) at %d", i, a, w, t0) }
			if gotLo, gotHi, gotOK := a.extent(t0, w); gotLo != lo || gotHi != hi || gotOK != ok {
				t.Fatalf("%s: the arbitrary-precision statement gives the extent %d to %d (%v), and a trial of every time gives %d to %d (%v)", where(), gotLo, gotHi, gotOK, lo, hi, ok)
			}
			if gotLo, gotHi, gotOK := a.extentFast(t0, w); gotLo != lo || gotHi != hi || gotOK != ok {
				t.Fatalf("%s: the integer statement gives the extent %d to %d (%v), and a trial of every time gives %d to %d (%v)", where(), gotLo, gotHi, gotOK, lo, hi, ok)
			}
			if got := SeparatedAt(&a, &b, sep, t0); got != want {
				t.Fatalf("%s: SeparatedAt says %v, and a trial of every time says %v", where(), got, want)
			}
			if got := separatedFast(&a, &b, sep, t0); got != want {
				t.Fatalf("%s: separatedFast says %v, and a trial of every time says %v", where(), got, want)
			}
			if want != prev {
				changes++
			}
			prev = want
			compared++
			if !want {
				conflicts++
			}
		}
	}
	if changes < 150 || conflicts < compared/5 || compared-conflicts < compared/5 {
		t.Fatalf("weak test: %d comparisons, %d not separated, and the answer changes %d times", compared, conflicts, changes)
	}
	t.Logf("%d comparisons, %d not separated, the answer changes %d times", compared, conflicts, changes)
}

// A vector is untrusted. A tube or a standard that is not valid is an error
// for the loader, for the reference, for the enumeration, and for a built-in
// Verifier. It is not a panic and not an answer.
func TestInvalidVectorIsAnError(t *testing.T) {
	whole := load(t)
	good := whole.Cases[0]
	// loads writes a suite of one case to a file and reads it back.
	loads := func(c Case) error {
		one := *whole
		one.Cases = []Case{c}
		f, err := os.Create(filepath.Join(t.TempDir(), "suite.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := one.Write(f); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		_, err = Load(f.Name())
		return err
	}
	for _, tc := range []struct {
		name  string
		spoil func(c *Case)
	}{
		{"no legs", func(c *Case) { c.A.Legs = nil }},
		{"no vertical segments", func(c *Case) { c.B.VSegs = nil }},
		{"a leg that ends before it starts", func(c *Case) { c.A.Legs = []Leg{{T0: 5, T1: 5}} }},
		{"a time beyond the limit", func(c *Case) { c.B.Anchor = fx.MaxTime + 1 }},
		{"a negative radius", func(c *Case) { c.A.R0 = -1 }},
		{"a step in altitude", func(c *Case) {
			s := c.B.VSegs[0]
			mid := s.T0 + (s.T1-s.T0)/2
			c.B.VSegs = []VSeg{{T0: s.T0, T1: mid, A0: s.A0, A1: s.A0}, {T0: mid, T1: s.T1, A0: s.A0 + 1, A1: s.A0 + 1}}
		}},
		{"a negative horizontal standard", func(c *Case) { c.Sep.Sh = -1 }},
		{"a horizontal standard beyond the limit", func(c *Case) { c.Sep.Sh = fx.MaxLen + 1 }},
		{"a negative vertical standard", func(c *Case) { c.Sep.SvTrue = -1 }},
		{"a vertical standard beyond the limit", func(c *Case) { c.Sep.SvTrue = fx.MaxLen + 1 }},
		{"a negative slack", func(c *Case) { c.Sep.VertSlack = -1 }},
		{"a slack beyond the limit", func(c *Case) { c.Sep.VertSlack = 1<<40 + 1 }},
	} {
		c := good
		tc.spoil(&c)
		if err := loads(c); err == nil {
			t.Errorf("%s: the loader accepts the vector", tc.name)
		}
		if _, err := Reference(&c); err == nil {
			t.Errorf("%s: the reference gives an expectation", tc.name)
		}
		if err := Confirm(&c); err == nil {
			t.Errorf("%s: the enumeration confirms the expectation", tc.name)
		}
		for _, name := range []string{"analytic", "sampled"} {
			v, _ := BuiltinByName(name)
			if _, err := v.Check(&c); err == nil {
				t.Errorf("%s: %s gives an answer", tc.name, name)
			}
		}
	}
	// The limits themselves are valid.
	atLimit := good
	atLimit.Sep = Sep{Sh: fx.MaxLen, SvTrue: fx.MaxLen, VertSlack: 1 << 40}
	for _, c := range []*Case{&good, &atLimit} {
		if _, err := Reference(c); err != nil {
			t.Fatalf("a valid case: %v", err)
		}
		if err := loads(*c); err != nil {
			t.Fatalf("a valid case: %v", err)
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
	d, err := kernel.Arrangement("dual")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range s.Cases {
		a, b, sp, err := c.kernel()
		if err != nil {
			t.Fatalf("%s: %v", c.ID, err)
		}
		if _, hard := d.Check(a, b, c.From, c.To, sp); hard {
			t.Errorf("%s: the two implementations disagree beyond the tolerance", c.ID)
		}
		// The suite and the kernel tighten a standard in the same way.
		if got, want := c.Sep.tightened(&c.A, &c.B).params(), kernel.Tighten(a, b, sp, Tol, TimeTol); got != want {
			t.Errorf("%s: the suite tightens the standard to %+v, and the kernel to %+v", c.ID, got, want)
		}
	}
	t.Logf("%d marginal disagreements, %d hard", d.Marginal, d.Hard)
}

// reading is one case as a Verifier reads it. A mutant changes its reading
// and not the case: a faulty Verifier gets valid tubes and misreads them.
type reading struct {
	c        *Case
	a, b     *kernel.Tube
	from, to int64
	sp       kernel.SepParams
}

// exact returns the exact answer for the reading.
func (r *reading) exact() Answer {
	res := kernel.AnalyticChecker{}.FirstConflict(r.a, r.b, r.from, r.to, r.sp)
	return Answer{ID: r.c.ID, Conflict: res.Conflict, T: res.T}
}

// mutant is a Verifier with one deliberate defect.
type mutant func(r *reading) Answer

func (m mutant) Check(c *Case) (Answer, error) {
	// The tubes are new for each call, so a mutant may change them.
	a, b, sp, err := c.kernel()
	if err != nil {
		return Answer{}, err
	}
	return m(&reading{c: c, a: a, b: b, from: c.From, to: c.To, sp: sp}), nil
}

// with returns the exact answer for a changed reading.
func with(change func(r *reading)) mutant {
	return func(r *reading) Answer {
		change(r)
		return r.exact()
	}
}

// oldClosedForm is the stationary-point formula in floating point that this
// project's analytic implementation first used. Rounding can make its
// discriminant negative on a collision course, and it then finds no minimum.
// It computes in nautical miles and seconds, as the kernel did then.
func oldClosedForm(r *reading) Answer {
	c := r.c
	if len(c.A.Legs) != 1 || len(c.B.Legs) != 1 || len(c.A.VSegs) != 1 || len(c.B.VSegs) != 1 || c.A.VSegs[0].A0 != c.B.VSegs[0].A0 {
		return r.exact()
	}
	lo, hi := c.interval()
	if hi <= lo {
		return Answer{ID: c.ID}
	}
	at := conv.Sec(lo)
	a, b := conv.Float(r.a.Traj), conv.Float(r.b.Traj)
	radius := func(tb *kernel.Tube) float64 {
		return conv.NM(tb.R0) + conv.NMps(tb.K)*math.Max(0, at-conv.Sec(tb.Anchor))
	}
	p := a.PosAt(at).Sub(b.PosAt(at))
	v := a.VelAt(at).Sub(b.VelAt(at))
	req, m, length := conv.NM(r.sp.Sh)+radius(r.a)+radius(r.b), conv.NMps(r.a.K+r.b.K), conv.Sec(hi-lo)
	g := func(x float64) float64 { return p.Add(v.Scale(x)).Norm() - (req + m*x) }
	least := math.Min(g(0), g(length))
	vv, pv, pp := v.Dot(v), p.Dot(v), p.Dot(p)
	r1, r2, n := solveQuadratic(vv*(vv-m*m), 2*pv*(vv-m*m), pv*pv-m*m*pp)
	if n >= 1 && r1 > 0 && r1 < length {
		least = math.Min(least, g(r1))
	}
	if n == 2 && r2 > 0 && r2 < length {
		least = math.Min(least, g(r2))
	}
	if least >= 0 {
		return Answer{ID: c.ID}
	}
	return r.exact()
}

// solveQuadratic returns the real roots of a·x² + b·x + c = 0 in ascending
// order. n is the number of roots returned: 0, 1 (linear case), or 2. The
// kernel used it until the closed form was replaced. It lives on here for
// the mutant above.
func solveQuadratic(a, b, c float64) (r1, r2 float64, n int) {
	if math.Abs(a) < 1e-14 {
		if math.Abs(b) < 1e-14 {
			return 0, 0, 0
		}
		return -c / b, 0, 1
	}
	disc := b*b - 4*a*c
	if disc < 0 {
		return 0, 0, 0
	}
	s := math.Sqrt(disc)
	var q float64
	if b >= 0 {
		q = -0.5 * (b + s)
	} else {
		q = -0.5 * (b - s)
	}
	r1 = q / a
	if q != 0 {
		r2 = c / q
	} else {
		r2 = r1
	}
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	return r1, r2, 2
}

// movedTimes applies a slack as version 0.1 of the rule did: it moves the
// start of every vertical segment earlier by the slack, and its end later.
// Then VMin·(s.T0 − w − t) <= R(t) and VMin·(t − s.T1 − w) <= R(t) decide.
// That is not a tolerance of w in time when the radius grows: a segment
// comes into effect less than w earlier, and stays in effect more than w
// longer.
func movedTimes(r *reading) {
	for _, tb := range []*kernel.Tube{r.a, r.b} {
		for i := range tb.Traj.VSegs {
			tb.Traj.VSegs[i].T0 -= r.sp.VertSlack
			tb.Traj.VSegs[i].T1 += r.sp.VertSlack
		}
	}
	r.sp.VertSlack = 0
}

// oldSampled is this project's sampled implementation with the vertical part
// that it had before the slack became a tolerance in time. For an interval
// lo to hi it takes one allowance, ceil(R(hi)/VMin), the largest of the
// interval, and counts every segment whose own times meet the span from
// lo − allowance − slack to hi + allowance + slack. At lo the aircraft can
// be late by less than that allowance. So the method counts a segment that
// the aircraft has left, for about K·(hi − lo)/VMin too long. That is more
// than the tolerance when the radius grows almost as fast as the aircraft
// flies. The horizontal part is that of the implementation today.
func oldSampled(r *reading) Answer {
	ans := Answer{ID: r.c.ID}
	lo := max(r.from, r.a.Start(), r.b.Start(), r.a.Anchor, r.b.Anchor)
	if hi := min(r.to, r.a.End(), r.b.End()); hi > lo {
		ans.Conflict = oldSampledNode(r.a, r.b, lo, hi, r.sp, &ans.T)
	}
	return ans
}

func oldSampledNode(a, b *kernel.Tube, lo, hi int64, sp kernel.SepParams, first *int64) bool {
	allowance := func(tb *kernel.Tube) int64 {
		if tb.VMin == 0 {
			return 0
		}
		if s, ok := tb.RadiusPM(hi).DivCeil(tb.VMin); ok && s < 1<<56 {
			return s
		}
		return 1 << 56
	}
	// band is the union of the bands of the segments whose times meet t0 to
	// t1. An interval of a check lies inside the trajectory, so there is one.
	band := func(tr *traj.Trajectory, t0, t1 int64) (lo, hi int64) {
		found := false
		for _, s := range tr.VSegs {
			if s.T1 < t0 || s.T0 > t1 {
				continue
			}
			if !found {
				lo, hi, found = s.Lo(), s.Hi(), true
				continue
			}
			lo, hi = min(lo, s.Lo()), max(hi, s.Hi())
		}
		return lo, hi
	}
	sa, sb := allowance(a)+sp.VertSlack, allowance(b)+sp.VertSlack
	aLo, aHi := band(a.Traj, lo-sa, hi+sa)
	bLo, bHi := band(b.Traj, lo-sb, hi+sb)
	u := a.VUPM(hi).Add(b.VUPM(hi))
	clear := func(gap int64) bool { return gap >= 0 && fx.Mul(gap, fx.M).Cmp(u) >= 0 }
	if clear(aLo-bHi-sp.SvTrue) || clear(bLo-aHi-sp.SvTrue) {
		return false
	}
	mid := lo + (hi-lo)/2
	pa, pb := a.Traj.Pos(mid), b.Traj.Pos(mid)
	d := fx.ISqrt(fx.SqSum(pa.X-pb.X, pa.Y-pb.Y))
	va, na := a.Traj.SpeedOver(lo, hi)
	vb, nb := b.Traj.SpeedOver(lo, hi)
	closure := fx.Mul(va+vb, max(mid-lo, hi-mid)).Add(fx.Mul(int64(na+nb-2), 2*traj.Join*fx.M))
	req := fx.Mul(sp.Sh, fx.M).Add(a.RadiusPM(hi)).Add(b.RadiusPM(hi))
	if req.Cmp(fx.Int(fx.MaxReq)) <= 0 && fx.Int(int64(d)).Sub(closure).Cmp(req) >= 0 {
		return false
	}
	if hi-lo <= fx.Sec {
		*first = lo
		return true
	}
	return oldSampledNode(a, b, lo, mid, sp, first) || oldSampledNode(a, b, mid+1, hi, sp, first)
}

// A suite that passes every Verifier proves nothing. Each mutant below has
// one defect, and the suite must catch it. Where a group of vectors was built
// for that defect, the mutant must fail in that group.
func TestSuiteCatchesDefects(t *testing.T) {
	s := load(t)
	flat := func(tb *kernel.Tube) {
		for i := range tb.Traj.VSegs {
			tb.Traj.VSegs[i].A1 = tb.Traj.VSegs[i].A0
		}
	}
	cases := []struct {
		name, group string
		v           Verifier
	}{
		{"approves everything", "", mutant(func(r *reading) Answer { return Answer{ID: r.c.ID} })},
		{"rejects everything", "", mutant(func(r *reading) Answer { return Answer{ID: r.c.ID, Conflict: true, T: r.from} })},
		{"ignores the growth of the envelope", "parallel", with(func(r *reading) { r.a.K, r.b.K = 0, 0 })},
		{"ignores the early and late allowance", "vertical-timing", with(func(r *reading) { r.a.VMin, r.b.VMin = 0, 0 })},
		{"reads a transition as level flight", "basic", with(func(r *reading) { flat(r.a); flat(r.b) })},
		{"ignores the altitude tolerance", "", with(func(r *reading) { r.a.VU0, r.b.VU0, r.a.VURate, r.b.VURate = 0, 0, 0, 0 })},
		{"ignores growth of the vertical uncertainty", "intruder", with(func(r *reading) { r.a.VURate, r.b.VURate = 0, 0 })},
		{"ignores the vertical slack", "tightened", with(func(r *reading) { r.sp.VertSlack = 0 })},
		{"checks beyond the end of the interval", "basic", with(func(r *reading) { r.to = 2 * fx.MaxTime })},
		{"checks before the start of the interval", "basic", with(func(r *reading) { r.from = -2 * fx.MaxTime })},
		{"reports the conflict three seconds late", "restart", mutant(func(r *reading) Answer {
			a := r.exact()
			a.T += 3 * fx.Sec
			return a
		})},
		{"reports the conflict far too early", "", mutant(func(r *reading) Answer {
			a := r.exact()
			a.T = r.from
			return a
		})},
		// The first defect found in this project: a check that starts at the
		// beginning of a conflict reports its end.
		{"reports the end of a conflict that starts at once", "restart", mutant(func(r *reading) Answer {
			a := r.exact()
			if lo, _ := r.c.interval(); a.Conflict && a.T-lo < 2000 {
				a.T += 60 * fx.Sec
			}
			return a
		})},
		// The second: no minimum is found on a collision course.
		{"loses the minimum on a collision course", "collision", mutant(oldClosedForm)},
		// The third and the fourth: the slack and the sampled method of
		// version 0.1 of the rule.
		{"applies a slack by moving the times of a segment", "slack", with(movedTimes)},
		{"takes the allowance of the end of an interval for all of it", "slack", mutant(oldSampled)},
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
	f := func(v int64) *int64 { return &v }
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
		{"conflict found at the start of the window", conflict, Answer{Conflict: true, T: 100}, true},
		{"conflict found at the end of the window", conflict, Answer{Conflict: true, T: 110}, true},
		{"conflict missed", conflict, Answer{}, false},
		{"conflict reported one microsecond late", conflict, Answer{Conflict: true, T: 111}, false},
		{"conflict reported one microsecond early", conflict, Answer{Conflict: true, T: 99}, false},
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

// The time of an answer from another process is read only for a conflict, and
// it must then be an integer.
func TestDecodeAnswer(t *testing.T) {
	for _, tc := range []struct {
		line string
		want Answer
		ok   bool
	}{
		{`{"id":"x","conflict":true,"t":641702526}`, Answer{ID: "x", Conflict: true, T: 641702526}, true},
		{`{"id":"x","conflict":true,"t":-5}`, Answer{ID: "x", Conflict: true, T: -5}, true},
		{`{"id":"x","conflict":false,"t":0}`, Answer{ID: "x"}, true},
		{`{"id":"x","conflict":false}`, Answer{ID: "x"}, true},
		{`{"id":"x","conflict":false,"t":0.5}`, Answer{ID: "x"}, true},
		{`{"id":"x","conflict":true,"t":641702526.5}`, Answer{}, false},
		{`{"id":"x","conflict":true,"t":6.41702526e8}`, Answer{}, false},
		{`{"id":"x","conflict":true,"t":"641702526"}`, Answer{}, false},
		{`{"id":"x","conflict":true,"t":null}`, Answer{}, false},
		{`{"id":"x","conflict":true}`, Answer{}, false},
		{`{"id":"y","conflict":false}`, Answer{}, false},
		{`not JSON`, Answer{}, false},
	} {
		got, err := decodeAnswer([]byte(tc.line), "x")
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("%s: answer %+v, error %v", tc.line, got, err)
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
