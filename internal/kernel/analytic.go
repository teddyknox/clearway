package kernel

import (
	"sort"

	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/traj"
)

// AnalyticChecker is Verifier implementation A. It is exact: it returns the
// first whole microsecond at which the two tubes are not separated, as the
// specification defines it, and it reports no conflict only if there is none.
//
// It splits the interval at every time where a leg, the set of vertical
// segments in effect, or the vertical separation changes. Inside one piece
// the relative position is linear in time and so is the required distance.
// Then
//
//	q(t) = |relative position|² − (required distance)²
//
// is a quadratic polynomial in t with integer values, and the pair is in
// horizontal conflict exactly where q(t) < 0. The margin |relative position|
// − required distance is convex, so those times form one run. The checker
// finds the first of them by searching on the sign of exact integers. It
// takes no square root and has no tolerance.
type AnalyticChecker struct{}

func (AnalyticChecker) Name() string { return "analytic" }

// The ends of a span that has no start or no end.
const (
	never  = -fx.NoLimit
	always = fx.NoLimit
)

// vertEvent is a span of whole microseconds, open to close, over which one
// vertical segment is in effect. One segment has one span or two.
type vertEvent struct {
	open, close int64
	lo, hi      int64
}

// events returns the spans over which the vertical segments are in effect
// under the slack w.
//
// With no slack, the segment s is in effect at t if
//
//	VMin·(s.T0 − t) <= R(t)   and   VMin·(t − s.T1) <= R(t).
//
// The radius R(t) is M·R0 up to the anchor and M·R0 + K·(t − Anchor) from it.
// So events solves each inequality for t twice: once for the times up to the
// anchor and once for the times from it. That gives at most two spans.
//
// Under a slack w, s is in effect at t if it is in effect, with no slack, at
// some whole microsecond from t − w to t + w. So each span becomes wider by w
// at both ends.
func (tb *Tube) events(w int64) []vertEvent {
	if w == 0 && tb.ev != nil {
		return tb.ev
	}
	segs := tb.Traj.VSegs
	ev := make([]vertEvent, 0, len(segs))
	base := fx.Mul(tb.R0, fx.M).Sub(fx.Mul(tb.K, tb.Anchor)) // from the anchor, R(t) = base + K·t
	var early int64                                          // up to the anchor, the allowance: floor(M·R0 / VMin)
	if tb.VMin > 0 {
		early = tb.R0 * fx.M / tb.VMin // M·R0 is below 2^60
	}
	for i := range segs {
		s := &segs[i]
		add := func(open, close int64) {
			if open <= close {
				ev = append(ev, vertEvent{open: widen(open, -w), close: widen(close, w), lo: s.Lo(), hi: s.Hi()})
			}
		}
		if tb.VMin == 0 {
			add(s.T0, s.T1)
			continue
		}
		// Up to the anchor:
		//   VMin·(T0 − t) <= M·R0  ⇔  t >= T0 − floor(M·R0 / VMin)
		//   VMin·(t − T1) <= M·R0  ⇔  t <= T1 + floor(M·R0 / VMin)
		open1 := s.T0 - early
		close1 := min(s.T1+early, tb.Anchor)
		// From the anchor:
		//   VMin·(T0 − t) <= base + K·t  ⇔  t >= (VMin·T0 − base) / (VMin + K)
		//   VMin·(t − T1) <= base + K·t  ⇔  (VMin − K)·t <= VMin·T1 + base
		open2 := max(ceilDiv(fx.Mul(tb.VMin, s.T0).Sub(base), tb.VMin+tb.K), tb.Anchor)
		close2 := int64(always)
		n := fx.Mul(tb.VMin, s.T1).Add(base)
		switch {
		case tb.VMin > tb.K:
			close2 = floorDiv(n, tb.VMin-tb.K)
		case tb.VMin == tb.K:
			if n.Sign() < 0 {
				close2 = never
			}
		default:
			// The radius grows faster than the aircraft flies: from some time
			// on, the segment stays in effect.
			open2 = max(open2, ceilDiv(n.Neg(), tb.K-tb.VMin))
		}
		if open1 <= close1 && open2 <= close2 && close1 == tb.Anchor && open2 == tb.Anchor {
			add(open1, close2) // the two spans meet at the anchor
			continue
		}
		add(open1, close1)
		add(open2, close2)
	}
	if w == 0 {
		tb.ev = ev
	}
	return ev
}

// widen moves the end of a span by d. An end that is never or always stays,
// and an end that would pass one of them becomes it.
func widen(t, d int64) int64 {
	switch {
	case t <= never || t >= always:
		return t
	case d < 0 && t < never-d:
		return never
	case d > 0 && t > always-d:
		return always
	}
	return t + d
}

// floorDiv and ceilDiv divide by a positive number. A quotient beyond every
// valid time becomes never or always.
func floorDiv(n fx.I128, d int64) int64 {
	if q, ok := n.DivFloor(d); ok && q > never && q < always {
		return q
	}
	if n.Sign() < 0 {
		return never
	}
	return always
}

func ceilDiv(n fx.I128, d int64) int64 {
	if q, ok := n.DivCeil(d); ok && q > never && q < always {
		return q
	}
	if n.Sign() < 0 {
		return never
	}
	return always
}

// bandAt returns the vertical extent at t: the union of the bands of the
// events that contain t. ok is false if none does.
func bandAt(ev []vertEvent, t int64) (lo, hi int64, ok bool) {
	return bandOver(ev, t, t)
}

// bandOver returns the union of the bands of the events that meet t0 to t1.
func bandOver(ev []vertEvent, t0, t1 int64) (lo, hi int64, ok bool) {
	for _, e := range ev {
		if e.open > t1 || e.close < t0 {
			continue
		}
		if !ok {
			lo, hi, ok = e.lo, e.hi, true
			continue
		}
		lo, hi = min(lo, e.lo), max(hi, e.hi)
	}
	return lo, hi, ok
}

func (AnalyticChecker) FirstConflict(a, b *Tube, from, to int64, sp SepParams) Result {
	lo, hi := span(a, b, from, to)
	if hi <= lo {
		return Result{}
	}
	eva, evb := a.events(sp.VertSlack), b.events(sp.VertSlack)

	// Fast path: vertically separated at every time of the interval. The
	// extents are the widest of the interval, and the uncertainty is the
	// largest.
	aLo, aHi, okA := bandOver(eva, lo, hi)
	bLo, bHi, okB := bandOver(evb, lo, hi)
	if okA && okB && vertSeparated(a, b, aLo, aHi, bLo, bHi, sp.SvTrue, hi) {
		return Result{}
	}

	// Break points: each is the first time of a piece. Inside a piece both
	// legs and both sets of segments in effect are fixed.
	bps := make([]int64, 0, 16)
	bps = append(bps, lo, hi+1)
	add := func(t int64) {
		if t > lo && t <= hi {
			bps = append(bps, t)
		}
	}
	for _, tr := range [2]*traj.Trajectory{a.Traj, b.Traj} {
		for i := 0; i+1 < len(tr.Legs); i++ {
			add(tr.Legs[i].T1)
		}
	}
	for _, ev := range [2][]vertEvent{eva, evb} {
		for _, e := range ev {
			if e.open > never {
				add(e.open)
			}
			if e.close < always {
				add(e.close + 1)
			}
		}
	}
	sort.Slice(bps, func(i, j int) bool { return bps[i] < bps[j] })

	for i := 0; i+1 < len(bps); i++ {
		s, e := bps[i], bps[i+1]-1
		if e < s {
			continue
		}
		aLo, aHi, okA := bandAt(eva, s)
		bLo, bHi, okB := bandAt(evb, s)
		// The time from which the pair is not vertically separated. The
		// uncertainty does not decrease, so the separated times come first.
		hs := s
		if okA && okB && vertSeparated(a, b, aLo, aHi, bLo, bHi, sp.SvTrue, s) {
			if vertSeparated(a, b, aLo, aHi, bLo, bHi, sp.SvTrue, e) {
				continue
			}
			hs = firstTrue(s, e, func(t int64) bool { return !vertSeparated(a, b, aLo, aHi, bLo, bHi, sp.SvTrue, t) })
		}
		if t, found := firstHorizontal(a, b, a.Traj.LegAt(hs), b.Traj.LegAt(hs), sp.Sh, hs, e); found {
			return Result{Conflict: true, T: t}
		}
	}
	return Result{}
}

// firstTrue returns the least t from lo to hi for which pred holds. pred must
// hold at hi, and must hold at every time after one at which it holds.
func firstTrue(lo, hi int64, pred func(int64) bool) int64 {
	for lo < hi {
		mid := lo + (hi-lo)/2
		if pred(mid) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// firstHorizontal returns the first whole microsecond from s to e at which
// the pair is not horizontally separated. la and lb are the legs in effect
// over the whole of s to e.
//
// The pair is not separated at t if the required distance is above
// fx.MaxReq, or if q(t) < 0 with
//
//	q(t) = |S_a(t) − S_b(t)|² − req(t)²        (picometres squared)
//
// q is a quadratic polynomial in t. Its leading coefficient is |Δv|² − m²,
// with Δv the relative velocity and m the growth of the required distance.
// The times with q < 0 form one run, because the margin is convex.
//
//   - Leading coefficient positive: q is convex, and its differences
//     q(t+1) − q(t) do not decrease. The least value is where they turn
//     non-negative. If the least value is negative, the run starts between s
//     and there.
//   - Otherwise q is concave or linear. A run that does not include s must
//     reach e. So q(e) decides whether there is one.
func firstHorizontal(a, b *Tube, la, lb *traj.Leg, sh, s, e int64) (int64, bool) {
	// The required distance does not decrease. Above the limit the pair is in
	// conflict: search the times before that for an earlier one.
	limit, capped := e, false
	if _, ok := reqPM(a, b, sh, e); !ok {
		if _, ok := reqPM(a, b, sh, s); !ok {
			return s, true
		}
		first := firstTrue(s, e, func(t int64) bool { _, ok := reqPM(a, b, sh, t); return !ok })
		limit, capped = first-1, true
	}
	e = limit

	q := func(t int64) fx.I128 {
		pa, pb := la.Pos(t), lb.Pos(t)
		req, _ := reqPM(a, b, sh, t)
		return fx.SqSum(pa.X-pb.X, pa.Y-pb.Y).Diff(fx.Sq(req))
	}
	neg := func(t int64) bool { return q(t).Sign() < 0 }
	found := func() (int64, bool) {
		if neg(s) {
			return s, true
		}
		if e == s {
			return 0, false
		}
		dvx, dvy, m := la.V.X-lb.V.X, la.V.Y-lb.V.Y, a.K+b.K
		lead := fx.Mul(dvx, dvx).Add(fx.Mul(dvy, dvy)).Sub(fx.Mul(m, m))
		if lead.Sign() <= 0 {
			if !neg(e) {
				return 0, false
			}
			return firstTrue(s, e, neg), true
		}
		rising := func(t int64) bool { return q(t+1).Cmp(q(t)) >= 0 }
		least := e
		if rising(e - 1) {
			least = firstTrue(s, e-1, rising)
		}
		if !neg(least) {
			return 0, false
		}
		return firstTrue(s, least, neg), true
	}
	if t, ok := found(); ok {
		return t, true
	}
	if capped {
		return limit + 1, true
	}
	return 0, false
}
