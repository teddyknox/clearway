package kernel

import (
	"math"
	"sort"

	"github.com/teddyknox/clearway/internal/geom"
)

// AnalyticChecker is Verifier implementation A.
//
// It splits the interval at every point where a leg, a vertical band, or the
// vertical separation state changes. Inside one piece the relative motion is
// linear and the required distance is linear, so the margin is a convex
// function of time. Bisection finds its minimum and its first crossing of
// zero. The result is exact to about a nanosecond of flight time.
type AnalyticChecker struct{}

func (AnalyticChecker) Name() string { return "analytic" }

// vertEvent is the time span over which one vertical segment is in effect,
// after the early/late allowance Sigma is applied.
type vertEvent struct {
	open, close float64
	lo, hi      float64
}

func (tb *Tube) events() []vertEvent {
	if tb.ev != nil {
		return tb.ev
	}
	segs := tb.Traj.VSegs
	ev := make([]vertEvent, len(segs))
	var s0, sk float64
	if tb.VMin > 0 {
		s0, sk = tb.R0/tb.VMin, tb.K/tb.VMin
	}
	for i, s := range segs {
		e := vertEvent{open: math.Inf(-1), close: math.Inf(1), lo: s.Lo(), hi: s.Hi()}
		if i > 0 {
			// Earliest t with t + Sigma(t) >= s.T0.
			e.open = (s.T0 - s0 + sk*tb.Anchor) / (1 + sk)
		}
		if i < len(segs)-1 && sk < 1 {
			// Latest t with t - Sigma(t) <= s.T1.
			e.close = (s.T1 + s0 - sk*tb.Anchor) / (1 - sk)
		}
		ev[i] = e
	}
	tb.ev = ev
	return ev
}

// bandOver returns the union of the bands in effect at any time in [t0, t1].
func bandOver(ev []vertEvent, t0, t1 float64) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, e := range ev {
		if e.open <= t1 && e.close >= t0 {
			lo = math.Min(lo, e.lo)
			hi = math.Max(hi, e.hi)
		}
	}
	return lo, hi
}

func (AnalyticChecker) FirstConflict(a, b *Tube, from, to float64, sp SepParams) Result {
	res := Result{Margin: math.Inf(1)}
	lo, hi := span(a, b, from, to)
	if hi <= lo {
		return res
	}
	eva, evb := a.events(), b.events()

	// Fast path: vertically separated over the whole interval.
	vs := sp.VertSlack
	aLo, aHi := bandOver(eva, lo-vs, hi+vs)
	bLo, bHi := bandOver(evb, lo-vs, hi+vs)
	u := a.VU(hi) + b.VU(hi)
	if aLo-bHi-u >= sp.SvTrue || bLo-aHi-u >= sp.SvTrue {
		return res
	}

	bps := make([]float64, 0, 16)
	bps = append(bps, lo, hi)
	add := func(t float64) {
		if t > lo && t < hi {
			bps = append(bps, t)
		}
	}
	for _, l := range a.Traj.Legs {
		add(l.T1)
	}
	for _, l := range b.Traj.Legs {
		add(l.T1)
	}
	for _, e := range eva {
		add(e.open - vs)
		add(e.close + vs)
	}
	for _, e := range evb {
		add(e.open - vs)
		add(e.close + vs)
	}
	sort.Float64s(bps)

	uRate := a.VURate + b.VURate
	kSum := a.K + b.K
	for i := 0; i+1 < len(bps); i++ {
		s, e := bps[i], bps[i+1]
		if e-s < 1e-9 {
			continue
		}
		mid := 0.5 * (s + e)
		aLo, aHi := bandOver(eva, mid-vs, mid+vs)
		bLo, bHi := bandOver(evb, mid-vs, mid+vs)
		us := a.VU(s) + b.VU(s)
		// Vertical gaps are linear and do not increase with time.
		g1 := aLo - bHi - us - sp.SvTrue
		g2 := bLo - aHi - us - sp.SvTrue
		tv := -1.0 // time after s until which vertical separation holds
		for _, g := range [2]float64{g1, g2} {
			if g >= 0 {
				if uRate <= 0 {
					tv = math.Inf(1)
				} else {
					tv = math.Max(tv, g/uRate)
				}
			}
		}
		if tv >= e-s {
			continue
		}
		us0 := s
		if tv > 0 {
			us0 = s + tv
		}
		la, lb := a.Traj.LegAt(mid), b.Traj.LegAt(mid)
		p := la.PosAt(us0).Sub(lb.PosAt(us0))
		v := la.V.Sub(lb.V)
		c := sp.Sh + a.Radius(us0) + b.Radius(us0)
		conflict, x, margin := horizCheck(p, v, c, kSum, e-us0)
		res.Margin = math.Min(res.Margin, margin)
		if conflict {
			res.Conflict = true
			res.T = us0 + x
			return res
		}
	}
	return res
}

// horizCheck examines g(x) = |p + v·x| - (c + m·x) for x in [0, length].
// It returns the least value of g, whether g is negative anywhere, and the
// first x at which g becomes negative.
//
// g is convex: a norm of an affine function, less a linear function. So its
// slope never decreases, its least value is where the slope changes sign, and
// the set where g is negative is one interval. Both the minimum and the first
// crossing are found by bisection.
//
// Bisection is used in place of closed-form roots for two reasons, each
// found as a defect in an earlier version of this function:
//
//   - On a near-exact collision course the stationary-point equation has a
//     discriminant that cancels to zero, and rounding can make it negative.
//     The closed form then finds no minimum and approves the pair.
//   - A check that starts exactly at a crossing has a root at zero that
//     rounding can make negative. The closed form then reports the far end
//     of the conflict as its start.
func horizCheck(p, v geom.Vec2, c, m, length float64) (conflict bool, x, margin float64) {
	g := func(x float64) float64 { return p.Add(v.Scale(x)).Norm() - (c + m*x) }
	// rising reports whether g increases at x. At zero distance the norm
	// has a corner; that point is the minimum of the norm, so g is taken as
	// rising there.
	rising := func(x float64) bool {
		q := p.Add(v.Scale(x))
		n := q.Norm()
		if n < 1e-12 {
			return true
		}
		return q.Dot(v)/n-m > 0
	}
	argmin := length
	switch {
	case rising(0):
		argmin = 0
	case rising(length):
		lo, hi := 0.0, length // slope not positive at lo, positive at hi
		for i := 0; i < 80 && hi-lo > 1e-9; i++ {
			if mid := 0.5 * (lo + hi); rising(mid) {
				hi = mid
			} else {
				lo = mid
			}
		}
		argmin = lo
		if g(hi) < g(lo) {
			argmin = hi
		}
	}
	margin = g(argmin)
	if margin >= 0 {
		return false, 0, margin
	}
	if g(0) < 0 {
		return true, 0, margin
	}
	lo, hi := 0.0, argmin // g(lo) >= 0 > g(hi)
	for i := 0; i < 80 && hi-lo > 1e-9; i++ {
		if mid := 0.5 * (lo + hi); g(mid) < 0 {
			hi = mid
		} else {
			lo = mid
		}
	}
	return true, lo, margin
}
