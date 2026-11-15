// Package kernel is the separation-critical core of a cell: the Contract
// Store, the Clearance Verifier, and the Conformance Monitor.
//
// The package is deterministic. It uses no random numbers, no wall clock, no
// learned model, and no floating point. It does not import the planner, the
// simulator, or the safety net.
//
// Every quantity is an integer in the units of package fx: micrometres,
// microseconds, micrometres per second. A position at a time is an exact
// number of picometres, and every comparison that decides separation is a
// comparison of integers (specification, sections 1 to 4).
//
// guest/kernel holds the same kernel in Rust, built for the RISC-V machine of
// package rvm. The two must give the same verdicts and the same state digests,
// bit for bit.
package kernel

import (
	"errors"

	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/traj"
)

// FlightID names one flight.
type FlightID int64

// Tube is the protected envelope of one trajectory. A Tube is immutable after
// construction.
//
// Horizontal: at time t, the aircraft is inside a disc of radius R0 +
// K·max(0, t − Anchor) around the nominal position. A point at along-track
// offset s on the path is at most |s| from the nominal point, so one disc
// covers along-track and cross-track tolerance on straight and on bent paths.
//
// Vertical: the aircraft is inside the assigned band of every vertical segment
// that is in effect at t (see inEffect), widened by the vertical uncertainty.
type Tube struct {
	ID       uint64
	Flight   FlightID
	Traj     *traj.Trajectory
	Anchor   int64 // time at which the radius equals R0 (µs)
	R0       int64 // protected radius at Anchor (µm)
	K        int64 // growth of the radius (µm/s)
	VU0      int64 // vertical uncertainty at Anchor (µm)
	VURate   int64 // growth of the vertical uncertainty (µm/s)
	VMin     int64 // lowest ground speed on the trajectory (µm/s); 0: no early or late allowance
	Intruder bool  // built from a track, not from a contract

	ev []vertEvent // cache for the exact checker, for a slack of zero
}

// Validate reports the first defect of a tube that did not come from the
// kernel. The kernel builds only valid tubes.
func (tb *Tube) Validate() error {
	if err := tb.Traj.Validate(); err != nil {
		return err
	}
	switch {
	case tb.Anchor < -fx.MaxTime || tb.Anchor > fx.MaxTime:
		return errors.New("anchor outside the valid times")
	case tb.R0 < 0 || tb.R0 > fx.MaxLen || tb.VU0 < 0 || tb.VU0 > fx.MaxLen:
		return errors.New("radius or vertical uncertainty outside the valid lengths")
	case tb.K < 0 || tb.K > fx.MaxSpeed || tb.VURate < 0 || tb.VURate > fx.MaxSpeed:
		return errors.New("growth rate outside the valid speeds")
	case tb.VMin < 0 || tb.VMin > 2*fx.MaxSpeed:
		return errors.New("lowest speed outside the valid speeds")
	}
	return nil
}

// WithTraj returns a copy of tb that uses tr. Fault-injection code uses it to
// model a Verifier implementation that misreads a trajectory.
func (tb *Tube) WithTraj(tr *traj.Trajectory) *Tube {
	cp := *tb
	cp.Traj, cp.ev = tr, nil
	return &cp
}

func (tb *Tube) Start() int64 { return tb.Traj.Start() }
func (tb *Tube) End() int64   { return tb.Traj.End() }

// RadiusPM returns the protected horizontal radius at t, in picometres:
// M·R0 + K·max(0, t − Anchor).
func (tb *Tube) RadiusPM(t int64) fx.I128 {
	return fx.Mul(tb.R0, fx.M).Add(fx.Mul(tb.K, max(0, t-tb.Anchor)))
}

// VUPM returns the vertical uncertainty at t, in picometres:
// M·VU0 + VURate·max(0, t − Anchor).
func (tb *Tube) VUPM(t int64) fx.I128 {
	return fx.Mul(tb.VU0, fx.M).Add(fx.Mul(tb.VURate, max(0, t-tb.Anchor)))
}

// inEffect reports whether the vertical segment s counts at time t.
//
// The aircraft may be early or late by R(t)/VMin, the time in which it covers
// its own radius. So s counts from that much before its start to that much
// after its end. The rule is exact, with no division:
//
//	VMin·(s.T0 − t) <= R(t)   and   VMin·(t − s.T1) <= R(t)
//
// with R(t) in picometres. With VMin = 0 there is no allowance, and s counts
// from s.T0 to s.T1.
//
// early moves the start of s earlier by that much. The Monitor uses it to
// read an intent that the aircraft sets before a change of level.
func (tb *Tube) inEffect(s *traj.VSeg, t, early int64) bool {
	if tb.VMin == 0 {
		return s.T0-early <= t && t <= s.T1
	}
	r := tb.RadiusPM(t)
	return fx.Mul(tb.VMin, s.T0-early-t).Cmp(r) <= 0 && fx.Mul(tb.VMin, t-s.T1).Cmp(r) <= 0
}

// Band returns the vertical extent of the tube at t: the lowest and the
// highest band limit of all segments in effect, with the start of each moved
// earlier by early. A time outside the trajectory maps to its nearest segment.
func (tb *Tube) Band(t, early int64) (lo, hi int64) {
	segs := tb.Traj.VSegs
	found := false
	for i := range segs {
		s := &segs[i]
		if !tb.inEffect(s, t, early) {
			continue
		}
		if !found {
			lo, hi, found = s.Lo(), s.Hi(), true
			continue
		}
		lo, hi = min(lo, s.Lo()), max(hi, s.Hi())
	}
	if !found {
		s := segs[0]
		if t > tb.End() {
			s = segs[len(segs)-1]
		}
		return s.Lo(), s.Hi()
	}
	return lo, hi
}

// SepParams is the separation standard that a checker applies.
//
// Two tubes are separated at time t if their discs are at least Sh apart, or
// if their vertical extents, each widened by its uncertainty, are at least
// SvTrue apart.
type SepParams struct {
	Sh     int64 // µm
	SvTrue int64 // µm, between true altitudes

	// VertSlack is a tolerance in time (µs). A vertical segment counts at t
	// if it is in effect at some whole microsecond from t − VertSlack to t +
	// VertSlack. It is zero for a normal check. Dual sets it to tighten the
	// standard when it classifies a disagreement.
	VertSlack int64
}

// Result is the outcome of one pair check over one interval.
type Result struct {
	Conflict bool
	T        int64 // earliest time at which the tubes are not separated (µs)
}

// PairChecker decides whether two tubes are separated at every whole
// microsecond from `from` to `to`.
//
// A checker must be conservative: it may report a conflict for a separated
// pair, and it must not report separation for a pair that is in conflict.
// The tubes must be valid. A checker looks only at times at or after the
// anchor of both tubes and inside both trajectories.
type PairChecker interface {
	Name() string
	FirstConflict(a, b *Tube, from, to int64, sp SepParams) Result
}

// span returns the times that a check covers: lo to hi, if hi > lo.
func span(a, b *Tube, from, to int64) (lo, hi int64) {
	lo = max(from, a.Start(), b.Start(), a.Anchor, b.Anchor)
	hi = min(to, a.End(), b.End())
	return lo, hi
}

// reqPM returns the required distance between the two nominal positions at t,
// in picometres: M·sh + R_a(t) + R_b(t). ok is false if it is above fx.MaxReq:
// the pair is then not horizontally separated.
func reqPM(a, b *Tube, sh, t int64) (req int64, ok bool) {
	r := fx.Mul(sh, fx.M).Add(a.RadiusPM(t)).Add(b.RadiusPM(t))
	if r.Cmp(fx.Int(fx.MaxReq)) > 0 {
		return 0, false
	}
	return r.Int64(), true
}

// vertSeparated reports whether two extents are vertically separated at t:
// the gap between them, less both uncertainties, is at least svTrue.
func vertSeparated(a, b *Tube, aLo, aHi, bLo, bHi, svTrue, t int64) bool {
	gap := max(aLo-bHi, bLo-aHi) - svTrue
	return gap >= 0 && fx.Mul(gap, fx.M).Cmp(a.VUPM(t).Add(b.VUPM(t))) >= 0
}
