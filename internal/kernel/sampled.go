package kernel

import (
	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/traj"
)

// SampledChecker is Verifier implementation B.
//
// It shares no geometry code with AnalyticChecker. It evaluates the nominal
// positions at the middle of an interval and bounds the motion inside the
// interval by the speeds of the two aircraft. If the bound proves separation,
// the interval is done. Otherwise it halves the interval. An interval of at
// most Step that it cannot prove is a conflict.
//
// Every rounding goes to the safe side: a distance is rounded down, and a
// speed is rounded up. So the checker is conservative. For an interval that it
// cannot prove, it reports the start of the interval. The specification,
// section 5.2, bounds how far that may be from the exact answer: the interval
// is at most Step long, and Step must not be longer than the time tolerance.
type SampledChecker struct {
	Step int64 // µs
}

func (SampledChecker) Name() string { return "sampled" }

func (c SampledChecker) FirstConflict(a, b *Tube, from, to int64, sp SepParams) Result {
	lo, hi := span(a, b, from, to)
	if hi <= lo {
		return Result{}
	}
	var res Result
	c.node(a, b, lo, hi, sp, &res)
	return res
}

// occupied returns the union of the bands of the vertical segments that the
// aircraft may occupy at some whole microsecond from t0 to t1. ok is false if
// there is none.
//
// The aircraft may occupy the segment s at t if it may have entered s,
// VMin·(s.T0 − t) <= R(t), and may not yet have left it, VMin·(t − s.T1) <=
// R(t). occupied tests the two at the ends of the interval, and solves
// nothing: s counts if the first holds at t1, and the second at t0 or at t1.
// The test is exact, for three reasons.
//
//   - Once the aircraft may have entered s, that stays so. So it must hold
//     at t1.
//   - The radius does not grow more slowly as time passes. So the amount by
//     which the second inequality fails, VMin·(t − s.T1) − R(t), is least at
//     one end of the interval. So the inequality must hold at t0 or at t1.
//   - If the second holds at t0 only, and the first at t1 only, both hold at
//     the first time at which the first holds. In one microsecond the amount
//     above grows by at most VMin, and one microsecond before, when the first
//     inequality still fails, it is below −VMin: a segment is at least one
//     microsecond long.
//
// For a segment that is still ahead of the interval, the second inequality
// holds at every time of the interval, and for one that is behind, the first
// does. So occupied computes one side only.
func occupied(tb *Tube, t0, t1 int64) (lo, hi int64, ok bool) {
	r0, r1 := tb.RadiusPM(t0), tb.RadiusPM(t1)
	segs := tb.Traj.VSegs
	for i := range segs {
		s := &segs[i]
		var in bool
		switch {
		case s.T0 <= t1 && t0 <= s.T1:
			in = true // nominal at a time of the interval
		case tb.VMin == 0:
			// No allowance.
		case s.T0 > t1:
			in = fx.Mul(tb.VMin, s.T0-t1).Cmp(r1) <= 0 // ahead: entered by t1
		default:
			// Behind: not left at t0, or not left at t1.
			in = fx.Mul(tb.VMin, t0-s.T1).Cmp(r0) <= 0 || fx.Mul(tb.VMin, t1-s.T1).Cmp(r1) <= 0
		}
		if !in {
			continue
		}
		if !ok {
			lo, hi, ok = s.Lo(), s.Hi(), true
			continue
		}
		lo, hi = min(lo, s.Lo()), max(hi, s.Hi())
	}
	return lo, hi, ok
}

func (c SampledChecker) node(a, b *Tube, lo, hi int64, sp SepParams, res *Result) bool {
	// Vertical: every segment that either aircraft may occupy at a time from
	// lo to hi, or within the slack of one.
	aLo, aHi, okA := occupied(a, lo-sp.VertSlack, hi+sp.VertSlack)
	bLo, bHi, okB := occupied(b, lo-sp.VertSlack, hi+sp.VertSlack)
	u := a.VUPM(hi).Add(b.VUPM(hi)) // the largest uncertainty of the interval
	clear := func(gap int64) bool { return gap >= 0 && fx.Mul(gap, fx.M).Cmp(u) >= 0 }
	if okA && okB && (clear(aLo-bHi-sp.SvTrue) || clear(bLo-aHi-sp.SvTrue)) {
		return false
	}
	// Horizontal: the distance at the middle, less the most that the pair can
	// close on the way to either end, less the largest required distance.
	mid := lo + (hi-lo)/2
	pa, pb := a.Traj.Pos(mid), b.Traj.Pos(mid)
	d := fx.ISqrt(fx.SqSum(pa.X-pb.X, pa.Y-pb.Y))
	va, na := a.Traj.SpeedOver(lo, hi)
	vb, nb := b.Traj.SpeedOver(lo, hi)
	// A leg may start up to traj.Join from the end of the leg before it, on
	// each axis: at most 2·Join in distance.
	closure := fx.Mul(va+vb, max(mid-lo, hi-mid)).Add(fx.Mul(int64(na+nb-2), 2*traj.Join*fx.M))
	// d is below 2^63: both positions are inside ±2^61 on each axis.
	req := fx.Mul(sp.Sh, fx.M).Add(a.RadiusPM(hi)).Add(b.RadiusPM(hi))
	if req.Cmp(fx.Int(fx.MaxReq)) <= 0 && fx.Int(int64(d)).Sub(closure).Cmp(req) >= 0 {
		return false
	}
	if hi-lo <= c.Step {
		res.Conflict = true
		res.T = lo
		return true
	}
	if c.node(a, b, lo, mid, sp, res) {
		return true
	}
	return c.node(a, b, mid+1, hi, sp, res)
}
