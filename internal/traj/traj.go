// Package traj defines 4D trajectories: a horizontal path flown at a stated
// ground speed, and a vertical profile of level and transition segments.
//
// Units: nautical miles, feet, seconds.
package traj

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"autoatc/internal/geom"
)

const (
	timeEps = 1e-6
	posEps  = 1e-5
	altEps  = 1e-6
)

// Leg is a straight horizontal segment flown at constant ground velocity.
type Leg struct {
	T0, T1 float64
	P0     geom.Vec2
	V      geom.Vec2 // nm/s
}

func (l Leg) PosAt(t float64) geom.Vec2 { return l.P0.Add(l.V.Scale(t - l.T0)) }
func (l Leg) EndPos() geom.Vec2         { return l.PosAt(l.T1) }
func (l Leg) Speed() float64            { return l.V.Norm() }

// VSeg is a vertical segment. A0 == A1 is level flight. Otherwise the segment
// is a transition, and the assigned band is the whole range between A0 and A1
// for the whole duration of the segment.
type VSeg struct {
	T0, T1 float64
	A0, A1 float64
}

func (s VSeg) Lo() float64 { return math.Min(s.A0, s.A1) }
func (s VSeg) Hi() float64 { return math.Max(s.A0, s.A1) }
func (s VSeg) Level() bool { return s.A0 == s.A1 }

// Trajectory is immutable after construction. Legs and VSegs each cover the
// same time span without gaps.
type Trajectory struct {
	Legs  []Leg
	VSegs []VSeg
}

func (tr *Trajectory) Start() float64 { return tr.Legs[0].T0 }
func (tr *Trajectory) End() float64   { return tr.Legs[len(tr.Legs)-1].T1 }

// Validate reports the first structural defect: an empty or reversed segment,
// a gap in time, a jump in position or altitude, or mismatched time spans.
func (tr *Trajectory) Validate() error {
	if tr == nil || len(tr.Legs) == 0 || len(tr.VSegs) == 0 {
		return errors.New("empty trajectory")
	}
	for i, l := range tr.Legs {
		if !(l.T1 > l.T0) || math.IsNaN(l.P0.X+l.P0.Y+l.V.X+l.V.Y) || math.IsInf(l.T0+l.T1, 0) {
			return fmt.Errorf("leg %d malformed", i)
		}
		if i > 0 {
			p := tr.Legs[i-1]
			if math.Abs(p.T1-l.T0) > timeEps {
				return fmt.Errorf("leg %d time gap", i)
			}
			if p.EndPos().Dist(l.P0) > posEps {
				return fmt.Errorf("leg %d position jump", i)
			}
		}
	}
	for i, s := range tr.VSegs {
		if !(s.T1 > s.T0) || math.IsNaN(s.A0+s.A1) {
			return fmt.Errorf("vseg %d malformed", i)
		}
		if i > 0 {
			p := tr.VSegs[i-1]
			if math.Abs(p.T1-s.T0) > timeEps {
				return fmt.Errorf("vseg %d time gap", i)
			}
			if math.Abs(p.A1-s.A0) > altEps {
				return fmt.Errorf("vseg %d altitude jump", i)
			}
		}
	}
	if math.Abs(tr.VSegs[0].T0-tr.Start()) > timeEps ||
		math.Abs(tr.VSegs[len(tr.VSegs)-1].T1-tr.End()) > timeEps {
		return errors.New("vertical profile does not span the legs")
	}
	return nil
}

// LegAt returns the leg in effect at t. Times outside the trajectory map to
// the first or the last leg.
func (tr *Trajectory) LegAt(t float64) *Leg {
	i := sort.Search(len(tr.Legs), func(i int) bool { return tr.Legs[i].T1 > t })
	if i >= len(tr.Legs) {
		i = len(tr.Legs) - 1
	}
	return &tr.Legs[i]
}

func (tr *Trajectory) vsegAt(t float64) *VSeg {
	i := sort.Search(len(tr.VSegs), func(i int) bool { return tr.VSegs[i].T1 > t })
	if i >= len(tr.VSegs) {
		i = len(tr.VSegs) - 1
	}
	return &tr.VSegs[i]
}

func (tr *Trajectory) clamp(t float64) float64 {
	return math.Max(tr.Start(), math.Min(tr.End(), t))
}

// PosAt returns the nominal position at t, clamped to the trajectory span.
func (tr *Trajectory) PosAt(t float64) geom.Vec2 {
	t = tr.clamp(t)
	return tr.LegAt(t).PosAt(t)
}

// VelAt returns the nominal ground velocity at t.
func (tr *Trajectory) VelAt(t float64) geom.Vec2 { return tr.LegAt(tr.clamp(t)).V }

// AltAt returns the nominal altitude at t, linear inside a transition.
func (tr *Trajectory) AltAt(t float64) float64 {
	t = tr.clamp(t)
	s := tr.vsegAt(t)
	return s.A0 + (s.A1-s.A0)*(t-s.T0)/(s.T1-s.T0)
}

// VSegAt returns a copy of the vertical segment in effect at t.
func (tr *Trajectory) VSegAt(t float64) VSeg { return *tr.vsegAt(tr.clamp(t)) }

// BandOver returns the union of the assigned bands of all vertical segments
// that overlap [t0, t1]. An interval outside the trajectory maps to the
// nearest segment.
func (tr *Trajectory) BandOver(t0, t1 float64) (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, s := range tr.VSegs {
		if s.T1 < t0 || s.T0 > t1 {
			continue
		}
		lo = math.Min(lo, s.Lo())
		hi = math.Max(hi, s.Hi())
	}
	if lo > hi {
		s := tr.VSegs[0]
		if t0 > tr.End() {
			s = tr.VSegs[len(tr.VSegs)-1]
		}
		return s.Lo(), s.Hi()
	}
	return lo, hi
}

// MinSpeed returns the lowest ground speed of any leg.
func (tr *Trajectory) MinSpeed() float64 {
	m := math.Inf(1)
	for _, l := range tr.Legs {
		m = math.Min(m, l.Speed())
	}
	return m
}

// MaxSpeedOver returns the highest ground speed of any leg that overlaps
// [t0, t1].
func (tr *Trajectory) MaxSpeedOver(t0, t1 float64) float64 {
	m := 0.0
	for _, l := range tr.Legs {
		if l.T1 < t0 || l.T0 > t1 {
			continue
		}
		m = math.Max(m, l.Speed())
	}
	if m == 0 {
		return tr.LegAt(t0).Speed()
	}
	return m
}

// Shift returns a copy with dt added to every time.
func (tr *Trajectory) Shift(dt float64) *Trajectory {
	out := &Trajectory{Legs: make([]Leg, len(tr.Legs)), VSegs: make([]VSeg, len(tr.VSegs))}
	for i, l := range tr.Legs {
		l.T0 += dt
		l.T1 += dt
		out.Legs[i] = l
	}
	for i, s := range tr.VSegs {
		s.T0 += dt
		s.T1 += dt
		out.VSegs[i] = s
	}
	return out
}

// Splice returns base up to time t followed by tail. tail must start at t.
// The caller must make tail continuous with base in position and altitude;
// Splice returns an error if the result is not a valid trajectory.
func Splice(base *Trajectory, t float64, tail *Trajectory) (*Trajectory, error) {
	if t <= base.Start() || t >= base.End() {
		return nil, errors.New("splice time outside base")
	}
	if math.Abs(tail.Start()-t) > timeEps {
		return nil, errors.New("tail does not start at splice time")
	}
	out := &Trajectory{}
	for _, l := range base.Legs {
		if l.T0 >= t {
			break
		}
		if l.T1 > t {
			l.T1 = t
		}
		out.Legs = append(out.Legs, l)
	}
	out.Legs = append(out.Legs, tail.Legs...)
	for _, s := range base.VSegs {
		if s.T0 >= t {
			break
		}
		if s.T1 > t {
			s.A1 = s.A0 + (s.A1-s.A0)*(t-s.T0)/(s.T1-s.T0)
			s.T1 = t
		}
		out.VSegs = append(out.VSegs, s)
	}
	out.VSegs = append(out.VSegs, tail.VSegs...)
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return out, nil
}

// Route returns contiguous legs through pts, starting at time t0, with one
// ground speed (nm/s) per leg. Zero-length legs are skipped.
func Route(t0 float64, pts []geom.Vec2, speeds []float64) []Leg {
	var legs []Leg
	t := t0
	for i := 0; i+1 < len(pts); i++ {
		d := pts[i+1].Sub(pts[i])
		dist := d.Norm()
		if dist < 1e-9 || speeds[i] <= 0 {
			continue
		}
		dur := dist / speeds[i]
		legs = append(legs, Leg{T0: t, T1: t + dur, P0: pts[i], V: d.Scale(1 / dur)})
		t += dur
	}
	return legs
}

// Level returns a level profile at alt over [t0, t1].
func Level(t0, t1, alt float64) []VSeg { return []VSeg{{T0: t0, T1: t1, A0: alt, A1: alt}} }

// Step returns a profile over [t0, t1]: level at a0, then a transition to a1
// that starts at ts and proceeds at rate (ft/s), then level at a1. The
// transition is truncated at t1 if it does not fit.
func Step(t0, t1, a0, ts, a1, rate float64) []VSeg {
	if a0 == a1 || rate <= 0 {
		return Level(t0, t1, a0)
	}
	if ts < t0 {
		ts = t0
	}
	if ts >= t1 {
		return Level(t0, t1, a0)
	}
	te := ts + math.Abs(a1-a0)/rate
	var out []VSeg
	if ts > t0 {
		out = append(out, VSeg{T0: t0, T1: ts, A0: a0, A1: a0})
	}
	if te >= t1 {
		return append(out, VSeg{T0: ts, T1: t1, A0: a0, A1: a1})
	}
	out = append(out, VSeg{T0: ts, T1: te, A0: a0, A1: a1})
	return append(out, VSeg{T0: te, T1: t1, A0: a1, A1: a1})
}
