package conformance

import (
	"fmt"
	"math/big"

	"github.com/teddyknox/clearway/internal/fx"
)

// This file states the separation rule of the specification, sections 3 and
// 4, for the tubes of a vector, and confirms an expectation with it. Nothing
// here shares code with a Verifier implementation.
//
// The rule is stated twice.
//
//   - SeparatedAt uses arbitrary-precision arithmetic. It follows the rule
//     line by line, and it cannot overflow. It is the reference.
//   - separatedFast uses the 128-bit integers of package fx. It is fast
//     enough to test every step of an interval. A test holds it to
//     SeparatedAt.
//
// Both require two valid tubes, a valid standard, and a time inside both
// trajectories. Every time of the interval of a check is inside both.
//
// Under a slack w, a vertical segment is in effect at t if it is in effect,
// with no slack, at some whole microsecond from t − w to t + w. Neither
// statement tries each of those microseconds: see inEffectWithin. A test
// does try each, on tubes with short times, and holds both statements to the
// result.

// ScanStep is the step of the enumeration that confirms an expectation (µs).
const ScanStep = 20_000

func (tb *Tube) start() int64 { return tb.Legs[0].T0 }
func (tb *Tube) end() int64   { return tb.Legs[len(tb.Legs)-1].T1 }

// interval returns the times that a check of c covers: every whole
// microsecond from lo to hi. If hi <= lo, the check covers nothing. Each tube
// must have a leg.
func (c *Case) interval() (lo, hi int64) {
	lo = max(c.From, c.A.start(), c.B.start(), c.A.Anchor, c.B.Anchor)
	hi = min(c.To, c.A.end(), c.B.end())
	return lo, hi
}

var (
	million = big.NewInt(1_000_000)               // picometres in a micrometre
	maxReq  = new(big.Int).Lsh(big.NewInt(1), 62) // the largest required distance that is compared (pm)
)

func bi(x int64) *big.Int         { return big.NewInt(x) }
func sum(x, y *big.Int) *big.Int  { return new(big.Int).Add(x, y) }
func diff(x, y *big.Int) *big.Int { return new(big.Int).Sub(x, y) }
func prod(x, y *big.Int) *big.Int { return new(big.Int).Mul(x, y) }

// grown returns M·v0 + rate·max(0, t − anchor), in picometres. With R0 and K
// it is the radius at t. With VU0 and VURate it is the vertical uncertainty.
func (tb *Tube) grown(v0, rate, t int64) *big.Int {
	since := diff(bi(t), bi(tb.Anchor))
	if since.Sign() < 0 {
		since = bi(0)
	}
	return sum(prod(million, bi(v0)), prod(bi(rate), since))
}

// entered and notLeft are the two inequalities of the rule for a segment in
// effect, for a tube with an allowance (VMin > 0).
//
// entered: the aircraft, as early as it may be, has started s at t.
//
//	VMin·(s.T0 − t) <= R(t)
//
// notLeft: the aircraft, as late as it may be, has not ended s at t.
//
//	VMin·(t − s.T1) <= R(t)
func (tb *Tube) entered(s *VSeg, t int64) bool {
	return prod(bi(tb.VMin), diff(bi(s.T0), bi(t))).Cmp(tb.grown(tb.R0, tb.K, t)) <= 0
}

func (tb *Tube) notLeft(s *VSeg, t int64) bool {
	return prod(bi(tb.VMin), diff(bi(t), bi(s.T1))).Cmp(tb.grown(tb.R0, tb.K, t)) <= 0
}

// inEffect reports whether the segment s is in effect at t, with no slack.
// With no allowance (VMin = 0), s is in effect from s.T0 to s.T1.
func (tb *Tube) inEffect(s *VSeg, t int64) bool {
	if tb.VMin == 0 {
		return s.T0 <= t && t <= s.T1
	}
	return tb.entered(s, t) && tb.notLeft(s, t)
}

// firstTrue returns the least whole number from lo to hi for which pred
// holds. pred must hold at hi, and at every number after one at which it
// holds.
func firstTrue(lo, hi int64, pred func(int64) bool) int64 {
	if pred(lo) {
		return lo
	}
	for lo < hi {
		if mid := lo + (hi-lo)/2; pred(mid) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// inEffectWithin reports whether s is in effect, with no slack, at some whole
// microsecond from t − w to t + w. That is the rule under a slack w.
//
// It does not try each microsecond. It rests on three facts.
//
//   - With no allowance, s is in effect from s.T0 to s.T1. Two ranges of
//     whole numbers share a number if each starts no later than the other
//     ends.
//   - entered holds at every time after one at which it holds: its left side
//     falls as t grows, and the radius does not fall. So if it fails at
//     t + w, it fails at every time of the range. Otherwise a search finds
//     the first time of the range at which it holds, and it holds from there
//     to t + w.
//   - On each side of the anchor, both sides of notLeft are linear in t. So
//     if notLeft holds at some time of a range, it holds at an end of the
//     range, or at the anchor if the anchor is in the range.
func (tb *Tube) inEffectWithin(s *VSeg, t, w int64) bool {
	if w == 0 {
		return tb.inEffect(s, t) // one microsecond: the rule itself
	}
	lo, hi := t-w, t+w
	if tb.VMin == 0 {
		return s.T0 <= hi && lo <= s.T1
	}
	if !tb.entered(s, hi) {
		return false
	}
	first := firstTrue(lo, hi, func(u int64) bool { return tb.entered(s, u) })
	return tb.notLeft(s, first) || tb.notLeft(s, hi) || tb.notLeft(s, min(max(tb.Anchor, first), hi))
}

// extent returns the vertical extent of the tube at t under the slack w: the
// lowest and the highest band limit of the segments in effect. ok is false if
// no segment is in effect.
func (tb *Tube) extent(t, w int64) (lo, hi int64, ok bool) {
	for i := range tb.VSegs {
		s := &tb.VSegs[i]
		if !tb.inEffectWithin(s, t, w) {
			continue
		}
		if !ok {
			lo, hi, ok = min(s.A0, s.A1), max(s.A0, s.A1), true
			continue
		}
		lo, hi = min(lo, s.A0, s.A1), max(hi, s.A0, s.A1)
	}
	return lo, hi, ok
}

// position returns the nominal position at t, in picometres. At the time
// where one leg ends and the next starts, the next leg gives the position.
func (tb *Tube) position(t int64) (x, y *big.Int) {
	l := tb.Legs[len(tb.Legs)-1]
	for _, c := range tb.Legs {
		if c.T1 > t {
			l = c
			break
		}
	}
	dt := diff(bi(t), bi(l.T0))
	return sum(prod(million, bi(l.P0[0])), prod(bi(l.V[0]), dt)),
		sum(prod(million, bi(l.P0[1])), prod(bi(l.V[1]), dt))
}

// SeparatedAt reports whether the pair is separated at the whole microsecond
// t under the standard sep: vertically separated, or horizontally separated.
func SeparatedAt(a, b *Tube, sep Sep, t int64) bool {
	// Vertical: the gap between the two extents, less the standard, is at
	// least the sum of the two uncertainties.
	aLo, aHi, okA := a.extent(t, sep.VertSlack)
	bLo, bHi, okB := b.extent(t, sep.VertSlack)
	if okA && okB {
		u := sum(a.grown(a.VU0, a.VURate, t), b.grown(b.VU0, b.VURate, t))
		above := diff(diff(bi(aLo), bi(bHi)), bi(sep.SvTrue)) // a above b
		below := diff(diff(bi(bLo), bi(aHi)), bi(sep.SvTrue)) // a below b
		if prod(million, above).Cmp(u) >= 0 || prod(million, below).Cmp(u) >= 0 {
			return true
		}
	}
	// Horizontal: the distance between the two positions is at least the
	// standard and the two radii. A required distance above the limit is
	// not compared: the pair is then not horizontally separated.
	req := sum(prod(million, bi(sep.Sh)), sum(a.grown(a.R0, a.K, t), b.grown(b.R0, b.K, t)))
	if req.Cmp(maxReq) > 0 {
		return false
	}
	ax, ay := a.position(t)
	bx, by := b.position(t)
	dx, dy := diff(ax, bx), diff(ay, by)
	return sum(prod(dx, dx), prod(dy, dy)).Cmp(prod(req, req)) >= 0
}

func (tb *Tube) grownFast(v0, rate, t int64) fx.I128 {
	return fx.Mul(v0, fx.M).Add(fx.Mul(rate, max(0, t-tb.Anchor)))
}

func (tb *Tube) enteredFast(s *VSeg, t int64) bool {
	return fx.Mul(tb.VMin, s.T0-t).Cmp(tb.grownFast(tb.R0, tb.K, t)) <= 0
}

func (tb *Tube) notLeftFast(s *VSeg, t int64) bool {
	return fx.Mul(tb.VMin, t-s.T1).Cmp(tb.grownFast(tb.R0, tb.K, t)) <= 0
}

// inEffectWithinFast is inEffectWithin with the integers of package fx.
func (tb *Tube) inEffectWithinFast(s *VSeg, t, w int64) bool {
	if w == 0 {
		if tb.VMin == 0 {
			return s.T0 <= t && t <= s.T1
		}
		return tb.enteredFast(s, t) && tb.notLeftFast(s, t)
	}
	lo, hi := t-w, t+w
	if tb.VMin == 0 {
		return s.T0 <= hi && lo <= s.T1
	}
	if !tb.enteredFast(s, hi) {
		return false
	}
	first := firstTrue(lo, hi, func(u int64) bool { return tb.enteredFast(s, u) })
	return tb.notLeftFast(s, first) || tb.notLeftFast(s, hi) || tb.notLeftFast(s, min(max(tb.Anchor, first), hi))
}

// extentFast is extent with the integers of package fx.
func (tb *Tube) extentFast(t, w int64) (lo, hi int64, ok bool) {
	for i := range tb.VSegs {
		s := &tb.VSegs[i]
		if !tb.inEffectWithinFast(s, t, w) {
			continue
		}
		if !ok {
			lo, hi, ok = min(s.A0, s.A1), max(s.A0, s.A1), true
			continue
		}
		lo, hi = min(lo, s.A0, s.A1), max(hi, s.A0, s.A1)
	}
	return lo, hi, ok
}

func (tb *Tube) positionFast(t int64) fx.Vec {
	l := &tb.Legs[len(tb.Legs)-1]
	for i := range tb.Legs {
		if tb.Legs[i].T1 > t {
			l = &tb.Legs[i]
			break
		}
	}
	return fx.Pos(fx.Vec{X: l.P0[0], Y: l.P0[1]}, fx.Vec{X: l.V[0], Y: l.V[1]}, l.T0, t)
}

// separatedFast states the rule of SeparatedAt with the integers of package
// fx. On valid tubes no step can overflow: a position is inside ±2^61 pm on
// each axis, and a required distance that is compared is at most 2^62 pm.
func separatedFast(a, b *Tube, sep Sep, t int64) bool {
	aLo, aHi, okA := a.extentFast(t, sep.VertSlack)
	bLo, bHi, okB := b.extentFast(t, sep.VertSlack)
	if okA && okB {
		u := a.grownFast(a.VU0, a.VURate, t).Add(b.grownFast(b.VU0, b.VURate, t))
		if fx.Mul(aLo-bHi-sep.SvTrue, fx.M).Cmp(u) >= 0 || fx.Mul(bLo-aHi-sep.SvTrue, fx.M).Cmp(u) >= 0 {
			return true
		}
	}
	req := fx.Mul(sep.Sh, fx.M).Add(a.grownFast(a.R0, a.K, t)).Add(b.grownFast(b.R0, b.K, t))
	if req.Cmp(fx.Int(fx.MaxReq)) > 0 {
		return false
	}
	pa, pb := a.positionFast(t), b.positionFast(t)
	return fx.SqSum(pa.X-pb.X, pa.Y-pb.Y).Cmp(fx.Sq(req.Int64())) >= 0
}

// scan tests the rule at lo, lo + ScanStep, lo + 2·ScanStep, and so on, and
// at hi. It returns the first of those times at which the pair is not
// separated. It requires lo <= hi.
func scan(a, b *Tube, sep Sep, lo, hi int64) (t int64, found bool) {
	for t = lo; ; t += ScanStep {
		t = min(t, hi)
		if !separatedFast(a, b, sep, t) {
			return t, true
		}
		if t == hi {
			return 0, false
		}
	}
}

// Confirm holds the expectation of c to the rule, by enumeration. It uses no
// Verifier implementation.
//
//   - Class "conflict": FirstMax is the first conflict time under the
//     standard as given, and FirstMin is the first conflict time under the
//     tightened standard.
//   - Class "either": there is no conflict under the standard as given,
//     FirstMin is the first conflict time under the tightened standard, and
//     FirstMax is the end of the interval.
//   - Class "clear": there is no conflict under either standard.
//
// "The first conflict time" is confirmed in three steps. SeparatedAt says
// that the pair is not separated at that time. SeparatedAt says that the pair
// is separated one microsecond earlier, if that is in the interval. A scan
// in steps of ScanStep finds no earlier conflict. "No conflict" is confirmed
// by a scan of the whole interval.
//
// Confirm returns the first thing that does not hold, or an error if a tube
// or the standard is not valid.
func Confirm(c *Case) error {
	if _, _, _, err := c.kernel(); err != nil {
		return fmt.Errorf("%s: %w", c.ID, err)
	}
	e, tight := c.Expect, c.Sep.tightened(&c.A, &c.B)
	if e.Class != Clear && (e.FirstMin == nil || e.FirstMax == nil) {
		return fmt.Errorf("%s: class %q without a time window", c.ID, e.Class)
	}
	switch e.Class {
	case Conflict:
		if *e.FirstMin > *e.FirstMax {
			return fmt.Errorf("%s: the time window starts at %d and ends before it, at %d", c.ID, *e.FirstMin, *e.FirstMax)
		}
		if err := c.confirmFirst(c.Sep, "the standard", *e.FirstMax); err != nil {
			return err
		}
		return c.confirmFirst(tight, "the tightened standard", *e.FirstMin)
	case Either:
		if _, hi := c.interval(); *e.FirstMax != hi {
			return fmt.Errorf("%s: the time window ends at %d, and the interval ends at %d", c.ID, *e.FirstMax, hi)
		}
		if err := c.confirmNone(c.Sep, "the standard"); err != nil {
			return err
		}
		return c.confirmFirst(tight, "the tightened standard", *e.FirstMin)
	case Clear:
		if err := c.confirmNone(c.Sep, "the standard"); err != nil {
			return err
		}
		return c.confirmNone(tight, "the tightened standard")
	}
	return fmt.Errorf("%s: unknown class %q", c.ID, e.Class)
}

// confirmFirst holds first to the rule as the first conflict time under sep.
func (c *Case) confirmFirst(sep Sep, name string, first int64) error {
	lo, hi := c.interval()
	if hi <= lo || first < lo || first > hi {
		return fmt.Errorf("%s: first conflict under %s at %d, outside the interval %d to %d", c.ID, name, first, lo, hi)
	}
	if SeparatedAt(&c.A, &c.B, sep, first) {
		return fmt.Errorf("%s: first conflict under %s at %d, and the pair is separated then", c.ID, name, first)
	}
	if first == lo {
		return nil
	}
	if !SeparatedAt(&c.A, &c.B, sep, first-1) {
		return fmt.Errorf("%s: first conflict under %s at %d, and the pair is not separated one microsecond earlier", c.ID, name, first)
	}
	if t, found := scan(&c.A, &c.B, sep, lo, first-1); found {
		return fmt.Errorf("%s: first conflict under %s at %d, and the enumeration finds one at %d", c.ID, name, first, t)
	}
	return nil
}

// confirmNone holds to the rule that there is no conflict under sep.
func (c *Case) confirmNone(sep Sep, name string) error {
	lo, hi := c.interval()
	if hi <= lo {
		return nil
	}
	if t, found := scan(&c.A, &c.B, sep, lo, hi); found {
		return fmt.Errorf("%s: no conflict under %s, and the enumeration finds one at %d", c.ID, name, t)
	}
	return nil
}
