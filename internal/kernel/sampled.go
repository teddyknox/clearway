package kernel

import "math"

// SampledChecker is Verifier implementation B.
//
// It shares no geometry code with AnalyticChecker. It evaluates the nominal
// positions at the midpoint of an interval and bounds the motion inside the
// interval by the speeds of the two aircraft. If the bound proves separation,
// the interval is done. Otherwise it halves the interval. An interval of at
// most Step seconds that it cannot prove is a conflict.
//
// The result is conservative by at most the distance two aircraft close in
// Step seconds.
type SampledChecker struct {
	Step float64 // s
}

func (SampledChecker) Name() string { return "sampled" }

func (c SampledChecker) FirstConflict(a, b *Tube, from, to float64, sp SepParams) Result {
	res := Result{Margin: math.Inf(1)}
	lo, hi := span(a, b, from, to)
	if hi <= lo {
		return res
	}
	c.node(a, b, lo, hi, sp, &res)
	return res
}

func (c SampledChecker) node(a, b *Tube, lo, hi float64, sp SepParams, res *Result) bool {
	// Vertical: every band that either aircraft may occupy during [lo, hi].
	// An aircraft may be late by Sigma(lo) at the start of the interval and
	// early by Sigma(hi) at its end. Both t - Sigma(t) and t + Sigma(t)
	// increase with t, so these two bounds are exact for the interval.
	vs := sp.VertSlack
	aLo, aHi := a.Traj.BandOver(lo-a.Sigma(lo)-vs, hi+a.Sigma(hi)+vs)
	bLo, bHi := b.Traj.BandOver(lo-b.Sigma(lo)-vs, hi+b.Sigma(hi)+vs)
	u := a.VU(hi) + b.VU(hi)
	if aLo-bHi-u >= sp.SvTrue || bLo-aHi-u >= sp.SvTrue {
		return false
	}
	// Horizontal: distance at the midpoint, less the most the pair can close
	// in half the interval, less the largest required distance.
	mid := 0.5 * (lo + hi)
	d := a.Traj.PosAt(mid).Dist(b.Traj.PosAt(mid))
	closure := a.Traj.MaxSpeedOver(lo, hi) + b.Traj.MaxSpeedOver(lo, hi)
	bound := d - closure*0.5*(hi-lo) - (sp.Sh + a.Radius(hi) + b.Radius(hi))
	if bound >= 0 {
		res.Margin = math.Min(res.Margin, bound)
		return false
	}
	if hi-lo <= c.Step {
		res.Margin = math.Min(res.Margin, bound)
		res.Conflict = true
		res.T = lo
		return true
	}
	if c.node(a, b, lo, mid, sp, res) {
		return true
	}
	return c.node(a, b, mid, hi, sp, res)
}
