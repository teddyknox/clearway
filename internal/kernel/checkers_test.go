package kernel

import (
	"math"
	"math/rand/v2"
	"testing"

	"autoatc/internal/geom"
	"autoatc/internal/traj"
)

var testSep = SepParams{Sh: 5, SvTrue: 600}

// oracle is a third, brute-force statement of the separation rule. It samples
// the exact predicate on a fine grid. Tests compare both checkers with it.
func oracle(a, b *Tube, from, to float64, sp SepParams, dt float64) (conflict bool, first, margin float64) {
	margin = math.Inf(1)
	lo, hi := span(a, b, from, to)
	if hi <= lo {
		return false, 0, margin
	}
	n := int(math.Ceil((hi - lo) / dt))
	for i := 0; i <= n; i++ {
		t := math.Min(hi, lo+float64(i)*dt)
		sa, sb := a.Sigma(t), b.Sigma(t)
		aLo, aHi := a.Traj.BandOver(t-sa, t+sa)
		bLo, bHi := b.Traj.BandOver(t-sb, t+sb)
		u := a.VU(t) + b.VU(t)
		if aLo-bHi-u >= sp.SvTrue || bLo-aHi-u >= sp.SvTrue {
			continue
		}
		m := a.Traj.PosAt(t).Dist(b.Traj.PosAt(t)) - (sp.Sh + a.Radius(t) + b.Radius(t))
		margin = math.Min(margin, m)
		if m < 0 && !conflict {
			conflict, first = true, t
		}
	}
	return conflict, first, margin
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
	legs := traj.Route(t0, pts, speeds)
	end := legs[len(legs)-1].T1
	levels := []float64{35000, 36000, 37000}
	alt := levels[r.IntN(len(levels))]
	var vs []traj.VSeg
	t := t0
	for t < end {
		seg := 60 + r.Float64()*300
		tl := math.Min(end, t+seg)
		vs = append(vs, traj.VSeg{T0: t, T1: tl, A0: alt, A1: alt})
		t = tl
		if t >= end {
			break
		}
		next := levels[r.IntN(len(levels))]
		if next == alt {
			continue
		}
		tt := math.Min(end, t+math.Abs(next-alt)/(1000.0/60))
		vs = append(vs, traj.VSeg{T0: t, T1: tt, A0: alt, A1: next})
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
	tr := &traj.Trajectory{Legs: legs, VSegs: merged}
	if err := tr.Validate(); err != nil {
		panic(err)
	}
	return &Tube{
		ID: id, Flight: FlightID(id), Traj: tr,
		Anchor: -r.Float64() * 300,
		R0:     0.8 + r.Float64(),
		K:      r.Float64() * 12 * geom.Kt,
		VU0:    200,
		VMin:   tr.MinSpeed(),
	}
}

func TestCheckersAgainstOracle(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	A := AnalyticChecker{}
	B := SampledChecker{Step: 1}
	d := &Dual{A: A, B: B, Tol: 0.75, TimeTol: 1}
	const oracleTol = 0.04 // nm: two aircraft close at most 0.3 nm/s, grid 0.05 s
	var conflicts, clears int
	for i := 0; i < 6000; i++ {
		a := randTube(r, 1, i%4 != 0)
		b := randTube(r, 2, i%4 != 0)
		from := 120 + r.Float64()*60
		to := from + 60 + r.Float64()*1200
		oc, ot, om := oracle(a, b, from, to, testSep, 0.05)
		ra := A.FirstConflict(a, b, from, to, testSep)
		rb := B.FirstConflict(a, b, from, to, testSep)

		// A is exact.
		if om < -oracleTol && !ra.Conflict {
			t.Fatalf("case %d: analytic approved a conflict; oracle margin %.3f at %.1f", i, om, ot)
		}
		if om > oracleTol && ra.Conflict {
			t.Fatalf("case %d: analytic rejected a clear pair; oracle margin %.3f, analytic %.3f", i, om, ra.Margin)
		}
		if oc && ra.Conflict && om < -oracleTol && math.Abs(ra.T-ot) > 0.5 {
			t.Fatalf("case %d: conflict time analytic %.2f, oracle %.2f", i, ra.T, ot)
		}
		if !math.IsInf(om, 1) && !math.IsInf(ra.Margin, 1) && !ra.Conflict && math.Abs(ra.Margin-om) > oracleTol {
			t.Fatalf("case %d: margin analytic %.3f, oracle %.3f", i, ra.Margin, om)
		}
		// B is sound, and conservative by a bounded amount.
		if om < -oracleTol && !rb.Conflict {
			t.Fatalf("case %d: sampled approved a conflict; oracle margin %.3f", i, om)
		}
		if om > d.Tol && rb.Conflict {
			t.Fatalf("case %d: sampled rejected with oracle margin %.3f", i, om)
		}
		// A check that starts at the reported conflict time reports the same
		// time. The kernel does exactly this for a window that is stuck at a
		// predicted conflict.
		// The analytic time is exact, so the re-check returns the same time.
		// The sampled time is the start of an interval that it could not
		// prove; a re-check may prove a little more, and it must still report
		// the conflict no later than the true one.
		if ra.Conflict {
			if r2 := A.FirstConflict(a, b, ra.T, to, testSep); !r2.Conflict || math.Abs(r2.T-ra.T) > 1e-3 {
				t.Fatalf("case %d: analytic: conflict at %.6f, re-check from there gives %+v", i, ra.T, r2)
			}
		}
		if rb.Conflict {
			r2 := B.FirstConflict(a, b, rb.T, to, testSep)
			if !r2.Conflict || r2.T < rb.T || (om < -oracleTol && r2.T > ot+0.1) {
				t.Fatalf("case %d: sampled: conflict at %.3f, re-check gives %+v, oracle conflict at %.3f", i, rb.T, r2, ot)
			}
		}
		// The two agree, or disagree only marginally.
		if _, hard := d.Check(a, b, from, to, testSep); hard {
			t.Fatalf("case %d: hard disagreement: analytic %+v sampled %+v oracle margin %.3f", i, ra, rb, om)
		}
		if oc {
			conflicts++
		} else {
			clears++
		}
	}
	if conflicts < 500 || clears < 500 {
		t.Fatalf("weak test mix: %d conflicts, %d clears", conflicts, clears)
	}
	t.Logf("%d conflicts, %d clears, %d marginal disagreements", conflicts, clears, d.Marginal)
}

func level(t0, t1 float64, from, to geom.Vec2, alt float64) *Tube {
	dur := t1 - t0
	tr := &traj.Trajectory{
		Legs:  []traj.Leg{{T0: t0, T1: t1, P0: from, V: to.Sub(from).Scale(1 / dur)}},
		VSegs: traj.Level(t0, t1, alt),
	}
	return &Tube{Traj: tr, R0: 1.2, K: 10 * geom.Kt, VU0: 200, VMin: tr.MinSpeed()}
}

func TestCheckerKnownCases(t *testing.T) {
	for _, c := range []PairChecker{AnalyticChecker{}, SampledChecker{Step: 1}} {
		// Head-on at the same level.
		a := level(0, 1200, geom.Vec2{X: -80}, geom.Vec2{X: 80}, 35000)
		b := level(0, 1200, geom.Vec2{X: 80}, geom.Vec2{X: -80}, 35000)
		if r := c.FirstConflict(a, b, 0, 1200, testSep); !r.Conflict {
			t.Errorf("%s: head-on at one level not detected", c.Name())
		}
		// Same paths, 1,000 ft apart.
		b = level(0, 1200, geom.Vec2{X: 80}, geom.Vec2{X: -80}, 36000)
		if r := c.FirstConflict(a, b, 0, 1200, testSep); r.Conflict {
			t.Errorf("%s: 1,000 ft apart reported as conflict", c.Name())
		}
		// Same paths, 500 ft apart.
		b = level(0, 1200, geom.Vec2{X: 80}, geom.Vec2{X: -80}, 35500)
		if r := c.FirstConflict(a, b, 0, 1200, testSep); !r.Conflict {
			t.Errorf("%s: 500 ft apart not detected", c.Name())
		}
		// Parallel tracks 30 nm apart at one level.
		b = level(0, 1200, geom.Vec2{X: -80, Y: 30}, geom.Vec2{X: 80, Y: 30}, 35000)
		if r := c.FirstConflict(a, b, 0, 1200, testSep); r.Conflict {
			t.Errorf("%s: parallel 30 nm apart reported as conflict", c.Name())
		}
		// A climb through the level of crossing traffic.
		b = level(0, 1200, geom.Vec2{X: 80}, geom.Vec2{X: -80}, 34000)
		b.Traj.VSegs = traj.Step(0, 1200, 34000, 500, 36000, 1000.0/60)
		b.ev = nil
		if r := c.FirstConflict(a, b, 0, 1200, testSep); !r.Conflict {
			t.Errorf("%s: climb through an occupied level not detected", c.Name())
		}
	}
}

// The envelope grows with time. A pair that is clear near the anchor can be in
// conflict far from it.
func TestEnvelopeGrowth(t *testing.T) {
	a := level(0, 1500, geom.Vec2{X: -100}, geom.Vec2{X: 100}, 35000)
	b := level(0, 1500, geom.Vec2{X: -100, Y: 9}, geom.Vec2{X: 100, Y: 9}, 35000)
	for _, c := range []PairChecker{AnalyticChecker{}, SampledChecker{Step: 1}} {
		if r := c.FirstConflict(a, b, 0, 120, testSep); r.Conflict {
			t.Errorf("%s: 9 nm apart near the anchor reported as conflict", c.Name())
		}
		r := c.FirstConflict(a, b, 0, 1500, testSep)
		if !r.Conflict {
			t.Fatalf("%s: grown envelopes not detected", c.Name())
		}
		// 9 = 5 + 2·(1.2 + K·t)  =>  t = 0.8 nm / (10 kt) = 288 s.
		// The sampled checker may report the conflict early: it allows for
		// the distance the pair could close in half a step.
		if r.T > 290 || r.T < 258 {
			t.Errorf("%s: conflict at %.1f s, want 258 to 290 s", c.Name(), r.T)
		}
		if _, ok := c.(AnalyticChecker); ok && math.Abs(r.T-288) > 0.5 {
			t.Errorf("analytic: conflict at %.2f s, want 288 s", r.T)
		}
	}
}

// Two aircraft aimed at one point at one time, or nearly so. This is the
// geometry in which a missed conflict is a collision, and the one in which
// closed-form roots lose precision. Both checkers must report it, for every
// growth rate, and the analytic time must match the oracle.
func TestCollisionCourses(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	A, B := AnalyticChecker{}, SampledChecker{Step: 1}
	for i := 0; i < 4000; i++ {
		meet := geom.Vec2{X: (r.Float64()*2 - 1) * 40, Y: (r.Float64()*2 - 1) * 40}
		tm := 300 + r.Float64()*800
		k := []float64{0, 0.5, 3, 10, 15}[r.IntN(5)] * geom.Kt
		mk := func(id uint64, miss geom.Vec2) *Tube {
			sp := (400 + 120*r.Float64()) * geom.Kt
			dir := geom.Polar(1, r.Float64()*2*math.Pi)
			from := meet.Add(miss).Sub(dir.Scale(sp * tm))
			to := meet.Add(miss).Add(dir.Scale(sp * 600))
			legs := traj.Route(0, []geom.Vec2{from, to}, []float64{sp})
			tr := &traj.Trajectory{Legs: legs, VSegs: traj.Level(0, legs[0].T1, 35000)}
			return &Tube{ID: id, Flight: FlightID(id), Traj: tr, R0: 1.2, K: k, VU0: 200, VMin: sp}
		}
		// Miss distances from exactly zero to a few hundredths of a mile.
		miss := geom.Polar([]float64{0, 1e-9, 1e-6, 1e-3, 0.03}[r.IntN(5)], r.Float64()*2*math.Pi)
		a, b := mk(1, geom.Vec2{}), mk(2, miss)
		to := tm + 300
		oc, ot, om := oracle(a, b, 0, to, testSep, 0.05)
		if !oc {
			t.Fatalf("case %d: oracle sees no conflict on a collision course", i)
		}
		ra := A.FirstConflict(a, b, 0, to, testSep)
		rb := B.FirstConflict(a, b, 0, to, testSep)
		if !ra.Conflict {
			t.Fatalf("case %d: analytic approved a collision course: margin %.3f, oracle margin %.3f at %.1f (K %.1f kt, miss %.2g nm)",
				i, ra.Margin, om, ot, k/geom.Kt, miss.Norm())
		}
		if !rb.Conflict {
			t.Fatalf("case %d: sampled approved a collision course", i)
		}
		if math.Abs(ra.T-ot) > 0.1 || rb.T > ot+0.1 {
			t.Fatalf("case %d: conflict at analytic %.2f, sampled %.2f, oracle %.2f", i, ra.T, rb.T, ot)
		}
		if math.Abs(ra.Margin-om) > 0.05 {
			t.Fatalf("case %d: margin analytic %.3f, oracle %.3f", i, ra.Margin, om)
		}
	}
}
