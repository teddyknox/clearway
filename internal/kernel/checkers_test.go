package kernel

import (
	"math"
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/teddyknox/clearway/internal/conv"
	"github.com/teddyknox/clearway/internal/ftraj"
	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/traj"
)

// Tests state values in nautical miles, feet, seconds, and knots.
func nm(x float64) int64  { return conv.Len(x) }
func ft(x float64) int64  { return conv.Alt(x) }
func sec(x float64) int64 { return conv.Time(x) }
func kt(x float64) int64  { return conv.Speed(x * geom.Kt) }

var testSep = SepParams{Sh: 5 * fx.NM, SvTrue: 600 * fx.FT}

func bigMul(x, y int64) *big.Int { return new(big.Int).Mul(big.NewInt(x), big.NewInt(y)) }

// bigRadius is the radius of a tube at t, in picometres.
func bigRadius(tb *Tube, t int64) *big.Int {
	return new(big.Int).Add(bigMul(tb.R0, 1_000_000), bigMul(tb.K, max(0, t-tb.Anchor)))
}

// bigInEffect states the rule of the specification, section 3, for one
// vertical segment at one time, with no slack.
func bigInEffect(tb *Tube, s traj.VSeg, t int64) bool {
	if tb.VMin == 0 {
		return s.T0 <= t && t <= s.T1
	}
	r := bigRadius(tb, t)
	return bigMul(tb.VMin, s.T0-t).Cmp(r) <= 0 && bigMul(tb.VMin, t-s.T1).Cmp(r) <= 0
}

// bigInEffectWithin states the rule under a slack w: the segment is in effect
// at some whole microsecond from t − w to t + w. It does not try them all.
// The first inequality of the rule holds at every time after one at which it
// holds, so a search finds the first such time. From there the second
// inequality compares two functions that are linear on each side of the
// anchor: if it holds anywhere, it holds at the first time, at the last, or at
// the anchor. TestSlackIsATimeTolerance holds this to a trial of every time.
func bigInEffectWithin(tb *Tube, s traj.VSeg, t, w int64) bool {
	lo, hi := t-w, t+w
	if tb.VMin == 0 {
		return s.T0 <= hi && lo <= s.T1
	}
	entered := func(u int64) bool { return bigMul(tb.VMin, s.T0-u).Cmp(bigRadius(tb, u)) <= 0 }
	if !entered(hi) {
		return false
	}
	for first, last := lo, hi; ; {
		if first == last {
			lo = first
			break
		}
		if mid := first + (last-first)/2; entered(mid) {
			last = mid
		} else {
			first = mid + 1
		}
	}
	return bigInEffect(tb, s, lo) || bigInEffect(tb, s, hi) || bigInEffect(tb, s, min(max(tb.Anchor, lo), hi))
}

// separatedAt is a second statement of the separation rule at one time, in
// arbitrary-precision arithmetic. It shares no code with the checkers, with
// package fx, or with the tube functions. It follows the specification,
// sections 3 and 4, line by line.
func separatedAt(a, b *Tube, t int64, sp SepParams) bool {
	bi := big.NewInt
	mul := bigMul
	radius := func(tb *Tube) *big.Int { return bigRadius(tb, t) } // pm
	uncertainty := func(tb *Tube) *big.Int {                      // pm
		return new(big.Int).Add(mul(tb.VU0, 1_000_000), mul(tb.VURate, max(0, t-tb.Anchor)))
	}
	extent := func(tb *Tube) (lo, hi int64) {
		first := true
		for _, s := range tb.Traj.VSegs {
			if !bigInEffectWithin(tb, s, t, sp.VertSlack) {
				continue
			}
			if first {
				lo, hi, first = min(s.A0, s.A1), max(s.A0, s.A1), false
			}
			lo, hi = min(lo, s.A0, s.A1), max(hi, s.A0, s.A1)
		}
		if first {
			panic("no vertical segment in effect inside the trajectory")
		}
		return lo, hi
	}
	position := func(tb *Tube) (x, y *big.Int) { // pm
		tr := tb.Traj
		tc := max(tr.Legs[0].T0, min(tr.Legs[len(tr.Legs)-1].T1, t))
		l := tr.Legs[len(tr.Legs)-1]
		for _, c := range tr.Legs {
			if c.T1 > tc {
				l = c
				break
			}
		}
		return new(big.Int).Add(mul(l.P0.X, 1_000_000), mul(l.V.X, tc-l.T0)),
			new(big.Int).Add(mul(l.P0.Y, 1_000_000), mul(l.V.Y, tc-l.T0))
	}

	// Vertical.
	aLo, aHi := extent(a)
	bLo, bHi := extent(b)
	u := new(big.Int).Add(uncertainty(a), uncertainty(b))
	for _, gap := range []int64{aLo - bHi - sp.SvTrue, bLo - aHi - sp.SvTrue} {
		if mul(gap, 1_000_000).Cmp(u) >= 0 {
			return true
		}
	}
	// Horizontal.
	req := new(big.Int).Add(mul(sp.Sh, 1_000_000), new(big.Int).Add(radius(a), radius(b)))
	if req.Cmp(bi(1<<62)) > 0 {
		return false
	}
	ax, ay := position(a)
	bx, by := position(b)
	dx, dy := new(big.Int).Sub(ax, bx), new(big.Int).Sub(ay, by)
	d2 := new(big.Int).Add(new(big.Int).Mul(dx, dx), new(big.Int).Mul(dy, dy))
	return d2.Cmp(new(big.Int).Mul(req, req)) >= 0
}

// quickInEffectWithin is bigInEffectWithin with the integers of package fx.
func quickInEffectWithin(tb *Tube, s *traj.VSeg, t, w int64) bool {
	if w == 0 {
		return tb.inEffect(s, t, 0) // one microsecond: the rule itself
	}
	lo, hi := t-w, t+w
	if tb.VMin == 0 {
		return s.T0 <= hi && lo <= s.T1
	}
	entered := func(u int64) bool { return fx.Mul(tb.VMin, s.T0-u).Cmp(tb.RadiusPM(u)) <= 0 }
	if !entered(hi) {
		return false
	}
	lo = firstTrue(lo, hi, entered)
	return tb.inEffect(s, lo, 0) || tb.inEffect(s, hi, 0) || tb.inEffect(s, min(max(tb.Anchor, lo), hi), 0)
}

// quickExtent returns the vertical extent of a tube at t under the slack w.
func quickExtent(tb *Tube, t, w int64) (lo, hi int64) {
	first := true
	segs := tb.Traj.VSegs
	for i := range segs {
		s := &segs[i]
		if !quickInEffectWithin(tb, s, t, w) {
			continue
		}
		if first {
			lo, hi, first = s.Lo(), s.Hi(), false
		}
		lo, hi = min(lo, s.Lo()), max(hi, s.Hi())
	}
	if first {
		panic("no vertical segment in effect inside the trajectory")
	}
	return lo, hi
}

// quickSeparatedAt states the same rule with the integers of package fx. It is
// fast enough to scan an interval. sameRule holds it to separatedAt.
func quickSeparatedAt(a, b *Tube, t int64, sp SepParams) bool {
	aLo, aHi := quickExtent(a, t, sp.VertSlack)
	bLo, bHi := quickExtent(b, t, sp.VertSlack)
	if vertSeparated(a, b, aLo, aHi, bLo, bHi, sp.SvTrue, t) {
		return true
	}
	req, ok := reqPM(a, b, sp.Sh, t)
	if !ok {
		return false
	}
	pa, pb := a.Traj.Pos(t), b.Traj.Pos(t)
	return fx.SqSum(pa.X-pb.X, pa.Y-pb.Y).Cmp(fx.Sq(req)) >= 0
}

// sameRule compares the two statements of the rule at random times of a check.
func sameRule(t *testing.T, r *rand.Rand, a, b *Tube, from, to int64, sp SepParams, n int) {
	t.Helper()
	lo, hi := span(a, b, from, to)
	for i := 0; i < n && hi > lo; i++ {
		at := lo + r.Int64N(hi-lo+1)
		if quick, exact := quickSeparatedAt(a, b, at, sp), separatedAt(a, b, at, sp); quick != exact {
			t.Fatalf("at %d the integer statement of the rule says separated=%v, and the arbitrary-precision one says %v", at, quick, exact)
		}
	}
}

// oracle scans the interval of a check in steps and returns the first scanned
// time at which the pair is not separated.
func oracle(a, b *Tube, from, to int64, sp SepParams, step int64) (conflict bool, first int64) {
	lo, hi := span(a, b, from, to)
	if hi <= lo {
		return false, 0
	}
	for t := lo; ; t += step {
		t = min(t, hi)
		if !quickSeparatedAt(a, b, t, sp) {
			return true, t
		}
		if t == hi {
			return false, 0
		}
	}
}

// checkExact holds an answer of the exact checker to the definition: at the
// reported time the pair is not separated, and one microsecond earlier it is.
// A sample of earlier times must be separated too.
func checkExact(t *testing.T, r *rand.Rand, name string, a, b *Tube, from, to int64, sp SepParams, res Result) {
	t.Helper()
	lo, hi := span(a, b, from, to)
	if !res.Conflict {
		for i := 0; i < 20 && hi > lo; i++ {
			if at := lo + r.Int64N(hi-lo+1); !separatedAt(a, b, at, sp) {
				t.Fatalf("%s: no conflict reported, and the pair is not separated at %d", name, at)
			}
		}
		return
	}
	if res.T < lo || res.T > hi {
		t.Fatalf("%s: conflict at %d, outside %d to %d", name, res.T, lo, hi)
	}
	if separatedAt(a, b, res.T, sp) {
		t.Fatalf("%s: conflict reported at %d, and the pair is separated then", name, res.T)
	}
	if res.T > lo && !separatedAt(a, b, res.T-1, sp) {
		t.Fatalf("%s: conflict reported at %d, and the pair is already not separated one microsecond before", name, res.T)
	}
	for i := 0; i < 20 && res.T > lo; i++ {
		if at := lo + r.Int64N(res.T-lo); !separatedAt(a, b, at, sp) {
			t.Fatalf("%s: conflict reported at %d, and the pair is not separated at %d", name, res.T, at)
		}
	}
}

func randTube(r *rand.Rand, id uint64, dense bool) *Tube {
	t0 := 90 + r.Float64()*30
	nLegs := 1 + r.IntN(3)
	spread := 150.0
	if dense {
		spread = 9
	}
	pts := []geom.Vec2{{X: (r.Float64()*2 - 1) * spread, Y: (r.Float64()*2 - 1) * spread}}
	var speeds []float64
	for i := 0; i < nLegs; i++ {
		last := pts[len(pts)-1]
		step := geom.Polar(20+r.Float64()*80, r.Float64()*2*math.Pi)
		pts = append(pts, last.Add(step))
		speeds = append(speeds, (400+r.Float64()*120)*geom.Kt)
	}
	legs := ftraj.Route(t0, pts, speeds)
	end := legs[len(legs)-1].T1
	levels := []float64{35000, 36000, 37000}
	alt := levels[r.IntN(len(levels))]
	var vs []ftraj.VSeg
	t := t0
	for t < end {
		seg := 60 + r.Float64()*300
		tl := math.Min(end, t+seg)
		vs = append(vs, ftraj.VSeg{T0: t, T1: tl, A0: alt, A1: alt})
		t = tl
		if t >= end {
			break
		}
		next := levels[r.IntN(len(levels))]
		if next == alt {
			continue
		}
		tt := math.Min(end, t+math.Abs(next-alt)/(1000.0/60))
		vs = append(vs, ftraj.VSeg{T0: t, T1: tt, A0: alt, A1: next})
		alt, t = next, tt
	}
	// Merge adjacent level segments at the same altitude.
	merged := vs[:1]
	for _, s := range vs[1:] {
		p := &merged[len(merged)-1]
		if p.Level() && s.Level() && p.A0 == s.A0 {
			p.T1 = s.T1
			continue
		}
		merged = append(merged, s)
	}
	tr := conv.Traj(&ftraj.Trajectory{Legs: legs, VSegs: merged})
	tb := &Tube{
		ID: id, Flight: FlightID(id), Traj: tr,
		Anchor: sec(-r.Float64() * 300),
		R0:     nm(0.8 + r.Float64()),
		K:      kt(r.Float64() * 12),
		VU0:    ft(200),
		VMin:   tr.MinSpeed(),
	}
	// Some tubes are those of an intruder: no early or late allowance, or a
	// radius that grows faster than the aircraft flies, and an uncertainty
	// that grows.
	switch r.IntN(8) {
	case 0:
		tb.VMin = 0
	case 1:
		tb.K, tb.VMin = kt(120), kt(60+r.Float64()*120)
		tb.VURate = ft(r.Float64() * 2)
	}
	if err := tb.Validate(); err != nil {
		panic(err)
	}
	return tb
}

func TestCheckersAgainstOracle(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	A := AnalyticChecker{}
	B := SampledChecker{Step: fx.Sec}
	d := &Dual{A: A, B: B, Tol: Tol, TimeTol: TimeTol}
	var conflicts, clears, early int
	for i := 0; i < 4000; i++ {
		a := randTube(r, 1, i%4 != 0)
		b := randTube(r, 2, i%4 != 0)
		from := sec(120 + r.Float64()*60)
		to := from + sec(60+r.Float64()*1200)
		sp := testSep
		if i%5 == 0 {
			sp.VertSlack = sec(r.Float64() * 3)
		}
		sameRule(t, r, a, b, from, to, sp, 25)
		oc, ot := oracle(a, b, from, to, sp, 50_000)
		ra := A.FirstConflict(a, b, from, to, sp)
		rb := B.FirstConflict(a, b, from, to, sp)

		// A is exact.
		if oc && (!ra.Conflict || ra.T > ot) {
			t.Fatalf("case %d: the oracle finds a conflict at %d; analytic gives %+v", i, ot, ra)
		}
		checkExact(t, r, "analytic", a, b, from, to, sp, ra)
		// B is sound, and never late.
		if ra.Conflict && (!rb.Conflict || rb.T > ra.T) {
			t.Fatalf("case %d: conflict at %d; sampled gives %+v", i, ra.T, rb)
		}
		// B is conservative only inside the tolerance: a conflict that it
		// reports is a conflict under the tightened standard, at that time or
		// before it.
		if rb.Conflict {
			if rt := A.FirstConflict(a, b, from, to, Tighten(a, b, sp, Tol, TimeTol)); !rt.Conflict || rt.T > rb.T {
				t.Fatalf("case %d: sampled reports a conflict at %d; under the tightened standard the exact answer is %+v", i, rb.T, rt)
			}
			early++
		}
		// A check that starts at the reported conflict time reports the same
		// time. The kernel does exactly this for a window that is stuck at a
		// predicted conflict.
		if ra.Conflict {
			if r2 := A.FirstConflict(a, b, ra.T, to, sp); ra.T < min(to, a.End(), b.End()) && (!r2.Conflict || r2.T != ra.T) {
				t.Fatalf("case %d: analytic: conflict at %d, re-check from there gives %+v", i, ra.T, r2)
			}
		}
		// The sampled time is the start of an interval that it could not
		// prove. A re-check from there splits the time differently and may
		// prove more. If there is a conflict, it must still report it, and
		// not late.
		if rb.Conflict && rb.T < min(to, a.End(), b.End()) {
			r2 := B.FirstConflict(a, b, rb.T, to, sp)
			if (r2.Conflict && r2.T < rb.T) || (ra.Conflict && (!r2.Conflict || r2.T > ra.T)) {
				t.Fatalf("case %d: sampled: conflict at %d, re-check gives %+v, exact conflict %+v", i, rb.T, r2, ra)
			}
		}
		// The two agree, or disagree only inside the tolerance.
		if _, hard := d.Check(a, b, from, to, sp); hard {
			t.Fatalf("case %d: hard disagreement: analytic %+v sampled %+v", i, ra, rb)
		}
		if ra.Conflict {
			conflicts++
		} else {
			clears++
		}
	}
	if conflicts < 500 || clears < 500 || early < conflicts {
		t.Fatalf("weak test mix: %d conflicts, %d clears, %d sampled conflicts", conflicts, clears, early)
	}
	t.Logf("%d conflicts, %d clears, %d marginal disagreements", conflicts, clears, d.Marginal)
}

// level returns a tube for level flight from one point to another (nm), from
// t0 to t1 (s), at alt (ft).
func level(t0, t1 float64, from, to geom.Vec2, alt float64) *Tube {
	tr := conv.Traj(&ftraj.Trajectory{
		Legs:  []ftraj.Leg{{T0: t0, T1: t1, P0: from, V: to.Sub(from).Scale(1 / (t1 - t0))}},
		VSegs: ftraj.Level(t0, t1, alt),
	})
	return &Tube{Traj: tr, R0: nm(1.2), K: 10 * fx.NM / 3600, VU0: ft(200), VMin: tr.MinSpeed()}
}

func TestCheckerKnownCases(t *testing.T) {
	for _, c := range []PairChecker{AnalyticChecker{}, SampledChecker{Step: fx.Sec}} {
		end := sec(1200)
		// Head-on at the same level.
		a := level(0, 1200, geom.Vec2{X: -80}, geom.Vec2{X: 80}, 35000)
		b := level(0, 1200, geom.Vec2{X: 80}, geom.Vec2{X: -80}, 35000)
		if r := c.FirstConflict(a, b, 0, end, testSep); !r.Conflict {
			t.Errorf("%s: head-on at one level not detected", c.Name())
		}
		// Same paths, 1,000 ft apart.
		b = level(0, 1200, geom.Vec2{X: 80}, geom.Vec2{X: -80}, 36000)
		if r := c.FirstConflict(a, b, 0, end, testSep); r.Conflict {
			t.Errorf("%s: 1,000 ft apart reported as conflict", c.Name())
		}
		// Same paths, 500 ft apart.
		b = level(0, 1200, geom.Vec2{X: 80}, geom.Vec2{X: -80}, 35500)
		if r := c.FirstConflict(a, b, 0, end, testSep); !r.Conflict {
			t.Errorf("%s: 500 ft apart not detected", c.Name())
		}
		// Parallel tracks 30 nm apart at one level.
		b = level(0, 1200, geom.Vec2{X: -80, Y: 30}, geom.Vec2{X: 80, Y: 30}, 35000)
		if r := c.FirstConflict(a, b, 0, end, testSep); r.Conflict {
			t.Errorf("%s: parallel 30 nm apart reported as conflict", c.Name())
		}
		// A climb through the level of crossing traffic.
		b = level(0, 1200, geom.Vec2{X: 80}, geom.Vec2{X: -80}, 34000)
		b.Traj.VSegs = conv.Traj(&ftraj.Trajectory{
			Legs:  []ftraj.Leg{{T0: 0, T1: 1200}},
			VSegs: ftraj.Step(0, 1200, 34000, 500, 36000, 1000.0/60),
		}).VSegs
		if r := c.FirstConflict(a, b, 0, end, testSep); !r.Conflict {
			t.Errorf("%s: climb through an occupied level not detected", c.Name())
		}
	}
}

// The envelope grows with time. A pair that is clear near the anchor can be in
// conflict far from it. The exact checker gives the microsecond.
func TestEnvelopeGrowth(t *testing.T) {
	a := level(0, 1500, geom.Vec2{X: -100}, geom.Vec2{X: 100}, 35000)
	b := level(0, 1500, geom.Vec2{X: -100, Y: 9}, geom.Vec2{X: 100, Y: 9}, 35000)
	// The two are 9 nm apart. They need 5 + 2·(1.2 + K·t) nm. So the conflict
	// starts at the first whole microsecond with 2·K·t > 1.6 nm.
	k := int64(10 * fx.NM / 3600)
	want := (16*fx.NM/10*fx.M)/(2*k) + 1
	for _, c := range []PairChecker{AnalyticChecker{}, SampledChecker{Step: fx.Sec}} {
		if r := c.FirstConflict(a, b, 0, sec(120), testSep); r.Conflict {
			t.Errorf("%s: 9 nm apart near the anchor reported as conflict", c.Name())
		}
		r := c.FirstConflict(a, b, 0, sec(1500), testSep)
		if !r.Conflict {
			t.Fatalf("%s: grown envelopes not detected", c.Name())
		}
		// The sampled checker may report the conflict early: it allows for
		// the distance the pair could close in half a step.
		if r.T > want || r.T < want-sec(30) {
			t.Errorf("%s: conflict at %d µs, want at most 30 s before %d", c.Name(), r.T, want)
		}
		if _, ok := c.(AnalyticChecker); ok && r.T != want {
			t.Errorf("analytic: conflict at %d µs, want %d", r.T, want)
		}
	}
	if s := conv.Sec(want); math.Abs(s-288) > 0.001 {
		t.Fatalf("the expected time is %v s, not about 288 s", s)
	}
}

// Two aircraft aimed at one point at one time, or nearly so. This is the
// geometry in which a missed conflict is a collision, and the one in which
// closed-form roots in floating point lost precision. Both checkers must
// report it, for every growth rate, and the exact time must be the first.
func TestCollisionCourses(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	A, B := AnalyticChecker{}, SampledChecker{Step: fx.Sec}
	for i := 0; i < 4000; i++ {
		meet := geom.Vec2{X: (r.Float64()*2 - 1) * 40, Y: (r.Float64()*2 - 1) * 40}
		tm := 300 + r.Float64()*800
		k := kt([]float64{0, 0.5, 3, 10, 15}[r.IntN(5)])
		mk := func(id uint64, miss geom.Vec2) *Tube {
			sp := (400 + 120*r.Float64()) * geom.Kt
			dir := geom.Polar(1, r.Float64()*2*math.Pi)
			from := meet.Add(miss).Sub(dir.Scale(sp * tm))
			to := meet.Add(miss).Add(dir.Scale(sp * 600))
			legs := ftraj.Route(0, []geom.Vec2{from, to}, []float64{sp})
			tr := conv.Traj(&ftraj.Trajectory{Legs: legs, VSegs: ftraj.Level(0, legs[0].T1, 35000)})
			return &Tube{ID: id, Flight: FlightID(id), Traj: tr, R0: nm(1.2), K: k, VU0: ft(200), VMin: tr.MinSpeed()}
		}
		// Miss distances from exactly zero to a few hundredths of a mile.
		miss := geom.Polar([]float64{0, 1e-9, 1e-6, 1e-3, 0.03}[r.IntN(5)], r.Float64()*2*math.Pi)
		a, b := mk(1, geom.Vec2{}), mk(2, miss)
		to := sec(tm + 300)
		sameRule(t, r, a, b, 0, to, testSep, 10)
		if oc, _ := oracle(a, b, 0, to, testSep, 50_000); !oc {
			t.Fatalf("case %d: the oracle sees no conflict on a collision course", i)
		}
		ra := A.FirstConflict(a, b, 0, to, testSep)
		rb := B.FirstConflict(a, b, 0, to, testSep)
		if !ra.Conflict || !rb.Conflict {
			t.Fatalf("case %d: a collision course was approved: analytic %+v, sampled %+v (K %d µm/s)", i, ra, rb, k)
		}
		checkExact(t, r, "analytic", a, b, 0, to, testSep, ra)
		if rb.T > ra.T {
			t.Fatalf("case %d: conflict at %d; sampled reports it late, at %d", i, ra.T, rb.T)
		}
	}
}

// The limits of valid values: a check at the edge of every range must not
// overflow, and the exact answer must hold to the definition.
func TestCheckersAtTheLimits(t *testing.T) {
	r := rand.New(rand.NewPCG(9, 10))
	A, B := AnalyticChecker{}, SampledChecker{Step: fx.Sec}
	edge := func(limit int64) int64 {
		switch r.IntN(4) {
		case 0:
			return limit
		case 1:
			return -limit
		}
		return r.Int64N(2*limit+1) - limit
	}
	var checked, conflicts int
	for i := 0; i < 20000; i++ {
		mk := func(id uint64) *Tube {
			t0 := edge(fx.MaxTime - 4000*fx.Sec)
			dur := 1 + r.Int64N(3600*fx.Sec)
			tr := &traj.Trajectory{
				Legs:  []traj.Leg{{T0: t0, T1: t0 + dur, P0: fx.Vec{X: edge(fx.MaxCoord), Y: edge(fx.MaxCoord)}, V: fx.Vec{X: edge(fx.MaxSpeed), Y: edge(fx.MaxSpeed)}}},
				VSegs: []traj.VSeg{{T0: t0, T1: t0 + dur, A0: edge(fx.MaxCoord), A1: edge(fx.MaxCoord)}},
			}
			tb := &Tube{
				ID: id, Traj: tr, Anchor: edge(fx.MaxTime),
				R0: r.Int64N(fx.MaxLen + 1), K: r.Int64N(fx.MaxSpeed + 1),
				VU0: r.Int64N(fx.MaxLen + 1), VURate: r.Int64N(fx.MaxSpeed + 1), VMin: r.Int64N(2*fx.MaxSpeed + 1),
			}
			if r.IntN(3) == 0 {
				tb.R0, tb.K, tb.VU0, tb.VURate = r.Int64N(4*fx.NM), r.Int64N(20*fx.Kt), r.Int64N(500*fx.FT), 0
			}
			return tb
		}
		a, b := mk(1), mk(2)
		if a.Validate() != nil || b.Validate() != nil {
			continue
		}
		// Make the two overlap in time, so that there is something to check.
		b.Traj = b.Traj.Shift(a.Start() - b.Start() + r.Int64N(600*fx.Sec) - 300*fx.Sec)
		if b.Validate() != nil {
			continue
		}
		sp := SepParams{Sh: 1 + r.Int64N(fx.MaxLen), SvTrue: r.Int64N(fx.MaxLen + 1), VertSlack: r.Int64N(10 * fx.Sec)}
		from, to := a.Start()-fx.Sec, a.End()+fx.Sec
		sameRule(t, r, a, b, from, to, sp, 10)
		ra := A.FirstConflict(a, b, from, to, sp)
		rb := B.FirstConflict(a, b, from, to, sp)
		checkExact(t, r, "analytic", a, b, from, to, sp, ra)
		if ra.Conflict && (!rb.Conflict || rb.T > ra.T) {
			t.Fatalf("case %d: conflict at %d; sampled gives %+v", i, ra.T, rb)
		}
		checked++
		if ra.Conflict {
			conflicts++
		}
	}
	if checked < 2000 || conflicts < 500 || checked-conflicts < 500 {
		t.Fatalf("weak mix: %d checked, %d conflicts", checked, conflicts)
	}
	t.Logf("%d pairs at the limits, %d conflicts", checked, conflicts)
}

// A slack is a tolerance in time: under a slack w, a vertical segment is in
// effect at t if it is in effect at some whole microsecond from t − w to t + w
// (specification, section 3). This test tries every such microsecond, for
// tubes whose times are a few thousand microseconds, and holds four things to
// the result: the spans of the exact checker, the test of the sampled checker,
// and the two statements of the rule that the other tests use.
func TestSlackIsATimeTolerance(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 12))
	const first, last = -1500, 4000 // the times that the test looks at
	var changes int
	for i := 0; i < 400; i++ {
		t0 := r.Int64N(2000)
		s := traj.VSeg{T0: t0, T1: t0 + 1 + r.Int64N(600), A0: 1, A1: 2}
		tb := &Tube{
			Traj:   &traj.Trajectory{VSegs: []traj.VSeg{s}},
			Anchor: r.Int64N(3500) - 500,
			R0:     r.Int64N(300),                 // the aircraft covers its radius in up to 300 µs
			K:      r.Int64N(4_000_000),           // slower and faster than the aircraft
			VMin:   1_000_000 + r.Int64N(500_000), // about 1 pm in 1 µs
		}
		switch r.IntN(6) {
		case 0:
			tb.VMin = 0
		case 1:
			tb.K = tb.VMin
		case 2:
			tb.K = 0
		}
		// in[u − first]: in effect at u, with no slack, by the two inequalities.
		in := make([]bool, last-first+1)
		for u := int64(first); u <= last; u++ {
			in[u-first] = bigInEffect(tb, s, u)
		}
		some := func(u0, u1 int64) bool {
			for u := u0; u <= u1; u++ {
				if in[u-first] {
					return true
				}
			}
			return false
		}
		w := r.Int64N(250)
		ev := tb.events(w)
		prev := false
		for at := int64(first) + w; at <= last-w; at++ {
			want := some(at-w, at+w)
			if want != prev {
				changes++
			}
			prev = want
			_, _, exact := bandAt(ev, at)
			if exact != want {
				t.Fatalf("case %d (%+v, segment %d to %d, slack %d): at %d the spans of the exact checker say %v, and a trial of every time says %v",
					i, *tb, s.T0, s.T1, w, at, exact, want)
			}
			if got := bigInEffectWithin(tb, s, at, w); got != want {
				t.Fatalf("case %d: at %d bigInEffectWithin says %v, want %v", i, at, got, want)
			}
			if got := quickInEffectWithin(tb, &s, at, w); got != want {
				t.Fatalf("case %d: at %d quickInEffectWithin says %v, want %v", i, at, got, want)
			}
		}
		// The sampled checker asks about an interval.
		for j := 0; j < 400; j++ {
			u0 := first + r.Int64N(last-first)
			u1 := min(last, u0+r.Int64N(700))
			if _, _, got := occupied(tb, u0, u1); got != some(u0, u1) {
				t.Fatalf("case %d (%+v, segment %d to %d): occupied from %d to %d says %v, and a trial of every time says %v",
					i, *tb, s.T0, s.T1, u0, u1, got, some(u0, u1))
			}
		}
	}
	if changes < 600 {
		t.Fatalf("weak test: the answer changes only %d times", changes)
	}
}

// A pair for which the sampled checker was once earlier than the tolerance
// allows. One aircraft descends to the level of the other. The level of the
// descent comes into effect at the time at which the aircraft, early by its
// own radius, could start it. The sampled checker tests a whole second at
// once, and reports its start. Under version 0.1 of the rule, a tolerance of
// one second moved that time by less than a second, because the radius grows.
func TestSampledStaysInsideTheTolerance(t *testing.T) {
	leg := func(y int64) []traj.Leg {
		return []traj.Leg{{T0: 0, T1: 1_100_000_000, P0: fx.Vec{X: 5_556_000_000, Y: y}, V: fx.Vec{X: 205_777_778}}}
	}
	const fl360, fl380 = 36000 * fx.FT, 38000 * fx.FT
	tube := func(tr *traj.Trajectory) *Tube {
		tb := &Tube{Traj: tr, R0: 2_222_400_000, K: 6_173_333, VU0: 200 * fx.FT, VMin: 205_777_778}
		if err := tb.Validate(); err != nil {
			t.Fatal(err)
		}
		return tb
	}
	a := tube(&traj.Trajectory{Legs: leg(7_408_000_000), VSegs: []traj.VSeg{{T0: 0, T1: 1_100_000_000, A0: fl360, A1: fl360}}})
	b := tube(&traj.Trajectory{Legs: leg(9_260_000_000), VSegs: []traj.VSeg{
		{T0: 0, T1: 600_990_000, A0: fl380, A1: fl380},
		{T0: 600_990_000, T1: 720_990_000, A0: fl380, A1: fl360},
		{T0: 720_990_000, T1: 1_100_000_000, A0: fl360, A1: fl360},
	}})
	const to = 1_024_000_000
	exact := AnalyticChecker{}.FirstConflict(a, b, 0, to, testSep)
	tight := AnalyticChecker{}.FirstConflict(a, b, 0, to, Tighten(a, b, testSep, Tol, TimeTol))
	sampled := SampledChecker{Step: fx.Sec}.FirstConflict(a, b, 0, to, testSep)
	if !exact.Conflict || exact.T != 573_000_001 {
		t.Fatalf("exact: %+v, want a conflict at 573000001", exact)
	}
	// A tolerance of one second in time moves the conflict by one second.
	if !tight.Conflict || tight.T != exact.T-TimeTol {
		t.Fatalf("under the tightened standard: %+v, want a conflict at %d", tight, exact.T-TimeTol)
	}
	if !sampled.Conflict || sampled.T < tight.T || sampled.T > exact.T {
		t.Fatalf("sampled: %+v, want a conflict from %d to %d", sampled, tight.T, exact.T)
	}
}
