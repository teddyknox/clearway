// Package kernel is the separation-critical core of a cell: the Contract
// Store, the Clearance Verifier, and the Conformance Monitor.
//
// The package is deterministic. It uses no random numbers, no wall clock, and
// no learned model. It does not import the planner, the simulator, or the
// safety net.
//
// Units: nautical miles, feet, seconds.
package kernel

import (
	"math"

	"github.com/teddyknox/clearway/internal/traj"
)

// FlightID names one flight.
type FlightID int

// Tube is the protected envelope of one trajectory. A Tube is immutable after
// construction.
//
// Horizontal: at time t, the aircraft is inside a disc of radius Radius(t)
// around the nominal position. A point at along-track offset s on the path is
// at most |s| from the nominal point, so one disc covers along-track and
// cross-track tolerance on straight and on bent paths.
//
// Vertical: the aircraft is inside the assigned band of every vertical segment
// that it may occupy at t, widened by VU(t). The aircraft may be early or late
// by Sigma(t), so a segment is in effect from Sigma before its start to Sigma
// after its end.
type Tube struct {
	ID       uint64
	Flight   FlightID
	Traj     *traj.Trajectory
	Anchor   float64 // time at which the radius equals R0
	R0       float64 // protected radius at Anchor (nm)
	K        float64 // growth of the radius (nm/s)
	VU0      float64 // vertical uncertainty at Anchor (ft)
	VURate   float64 // growth of the vertical uncertainty (ft/s)
	VMin     float64 // lowest ground speed on the trajectory (nm/s)
	Intruder bool    // built from a track, not from a contract

	ev []vertEvent // cache for the analytic checker
}

// WithTraj returns a copy of tb that uses tr. Fault-injection code uses it to
// model a Verifier implementation that misreads a trajectory.
func (tb *Tube) WithTraj(tr *traj.Trajectory) *Tube {
	cp := *tb
	cp.Traj, cp.ev = tr, nil
	return &cp
}

func (tb *Tube) Start() float64 { return tb.Traj.Start() }
func (tb *Tube) End() float64   { return tb.Traj.End() }

// Radius returns the protected horizontal radius at t.
func (tb *Tube) Radius(t float64) float64 {
	return tb.R0 + tb.K*math.Max(0, t-tb.Anchor)
}

// VU returns the vertical uncertainty at t.
func (tb *Tube) VU(t float64) float64 {
	return tb.VU0 + tb.VURate*math.Max(0, t-tb.Anchor)
}

// Sigma returns the time by which the aircraft may be early or late at t.
func (tb *Tube) Sigma(t float64) float64 {
	if tb.VMin <= 0 {
		return 0
	}
	return tb.Radius(t) / tb.VMin
}

// SepParams is the separation standard that a checker applies.
//
// Two tubes are separated at time t if their discs are at least Sh apart, or
// if their vertical extents, each widened by its uncertainty, are at least
// SvTrue apart.
type SepParams struct {
	Sh     float64 // nm
	SvTrue float64 // ft, between true altitudes

	// VertSlack widens the time span of every vertical band by this much on
	// each side (s). It is zero for a normal check. Dual sets it to tighten
	// the standard when it classifies a disagreement.
	VertSlack float64
}

// Result is the outcome of one pair check over one time interval.
type Result struct {
	Conflict bool
	T        float64 // earliest time at which the tubes are not separated
	Margin   float64 // least horizontal margin while not vertically separated; +Inf if none
}

// PairChecker decides whether two tubes are separated over [from, to].
//
// A checker must be conservative: it may report a conflict for a separated
// pair, and it must not report separation for a pair that is in conflict.
// The caller must pass from >= the Anchor of both tubes.
type PairChecker interface {
	Name() string
	FirstConflict(a, b *Tube, from, to float64, sp SepParams) Result
}

func span(a, b *Tube, from, to float64) (lo, hi float64) {
	lo = math.Max(math.Max(from, a.Start()), math.Max(b.Start(), math.Max(a.Anchor, b.Anchor)))
	hi = math.Min(to, math.Min(a.End(), b.End()))
	return lo, hi
}
