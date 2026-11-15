// Package traj defines the 4D trajectories of the kernel: a horizontal path
// flown at a stated ground velocity, and a vertical profile of level and
// transition segments.
//
// Every value is an integer in the units of package fx: micrometres,
// microseconds, micrometres per second. Time is a whole number of
// microseconds, and a position at a time is an exact number of picometres
// (specification, section 2). The package has no floating point.
package traj

import (
	"errors"
	"fmt"

	"github.com/teddyknox/clearway/internal/fx"
)

const (
	// Join is the largest distance, on each axis, between the end of a leg
	// and the start of the next (µm). A planner computes a path in other
	// units, and a velocity in whole micrometres per second cannot end a leg
	// on an exact point.
	Join = 18_520

	// MaxParts is the largest number of legs, and of vertical segments, in
	// one trajectory.
	MaxParts = 256
)

// Leg is a straight horizontal segment flown at constant ground velocity.
// It covers the times T0 to T1. At the time where one leg ends and the next
// starts, the next leg gives the position.
type Leg struct {
	T0, T1 int64  // µs
	P0     fx.Vec // µm
	V      fx.Vec // µm/s
}

// Pos returns the position on the leg at t, in picometres.
func (l *Leg) Pos(t int64) fx.Vec { return fx.Pos(l.P0, l.V, l.T0, t) }

// VSeg is a vertical segment. A0 == A1 is level flight. Otherwise the segment
// is a transition, and the assigned band is the whole range between A0 and A1
// for the whole duration of the segment.
type VSeg struct {
	T0, T1 int64 // µs
	A0, A1 int64 // µm
}

func (s VSeg) Lo() int64   { return min(s.A0, s.A1) }
func (s VSeg) Hi() int64   { return max(s.A0, s.A1) }
func (s VSeg) Level() bool { return s.A0 == s.A1 }

// Trajectory is immutable after construction. Legs and VSegs each cover the
// same span of time without gaps.
type Trajectory struct {
	Legs  []Leg
	VSegs []VSeg
}

func (tr *Trajectory) Start() int64 { return tr.Legs[0].T0 }
func (tr *Trajectory) End() int64   { return tr.Legs[len(tr.Legs)-1].T1 }

func inside(limit int64, vals ...int64) bool {
	for _, v := range vals {
		if v < -limit || v > limit {
			return false
		}
	}
	return true
}

// Validate reports the first defect. A valid trajectory:
//
//   - has from 1 to MaxParts legs and from 1 to MaxParts vertical segments;
//   - has every time, coordinate, altitude, and velocity inside the limits of
//     package fx, and every position on every leg inside them too;
//   - has legs that follow each other with no gap in time, each starting
//     within Join of the end of the leg before it, on each axis;
//   - has vertical segments that follow each other with no gap in time and no
//     step in altitude, over exactly the span of the legs.
//
// The kernel computes only with valid trajectories. On a valid trajectory no
// operation of this package or of the Verifier can overflow.
func (tr *Trajectory) Validate() error {
	if tr == nil || len(tr.Legs) == 0 || len(tr.VSegs) == 0 {
		return errors.New("empty trajectory")
	}
	if len(tr.Legs) > MaxParts || len(tr.VSegs) > MaxParts {
		return errors.New("too many legs or vertical segments")
	}
	limit := fx.Mul(fx.MaxCoord, fx.M)
	join := int64(Join) * fx.M
	var prevEnd [2]fx.I128
	for i, l := range tr.Legs {
		if l.T1 <= l.T0 || !inside(fx.MaxTime, l.T0, l.T1) || !inside(fx.MaxCoord, l.P0.X, l.P0.Y) ||
			!inside(fx.MaxSpeed, l.V.X, l.V.Y) {
			return fmt.Errorf("leg %d malformed", i)
		}
		start := [2]fx.I128{fx.Mul(l.P0.X, fx.M), fx.Mul(l.P0.Y, fx.M)}
		end := [2]fx.I128{start[0].Add(fx.Mul(l.V.X, l.T1-l.T0)), start[1].Add(fx.Mul(l.V.Y, l.T1-l.T0))}
		for axis := range end {
			if end[axis].Cmp(limit) > 0 || end[axis].Cmp(limit.Neg()) < 0 {
				return fmt.Errorf("leg %d leaves the valid area", i)
			}
		}
		if i > 0 {
			if l.T0 != tr.Legs[i-1].T1 {
				return fmt.Errorf("leg %d time gap", i)
			}
			for axis := range start {
				// Both values are inside the limit, so the difference fits.
				if d := start[axis].Sub(prevEnd[axis]).Int64(); d > join || d < -join {
					return fmt.Errorf("leg %d position jump", i)
				}
			}
		}
		prevEnd = end
	}
	for i, s := range tr.VSegs {
		if s.T1 <= s.T0 || !inside(fx.MaxTime, s.T0, s.T1) || !inside(fx.MaxCoord, s.A0, s.A1) {
			return fmt.Errorf("vseg %d malformed", i)
		}
		if i > 0 {
			if s.T0 != tr.VSegs[i-1].T1 {
				return fmt.Errorf("vseg %d time gap", i)
			}
			if s.A0 != tr.VSegs[i-1].A1 {
				return fmt.Errorf("vseg %d altitude jump", i)
			}
		}
	}
	if tr.VSegs[0].T0 != tr.Start() || tr.VSegs[len(tr.VSegs)-1].T1 != tr.End() {
		return errors.New("vertical profile does not span the legs")
	}
	return nil
}

// LegAt returns the leg in effect at t: the first leg that ends after t.
// A time at or after the end of the trajectory maps to the last leg, and a
// time before its start to the first.
func (tr *Trajectory) LegAt(t int64) *Leg {
	for i := range tr.Legs {
		if tr.Legs[i].T1 > t {
			return &tr.Legs[i]
		}
	}
	return &tr.Legs[len(tr.Legs)-1]
}

func (tr *Trajectory) vsegAt(t int64) *VSeg {
	for i := range tr.VSegs {
		if tr.VSegs[i].T1 > t {
			return &tr.VSegs[i]
		}
	}
	return &tr.VSegs[len(tr.VSegs)-1]
}

func (tr *Trajectory) clamp(t int64) int64 { return max(tr.Start(), min(tr.End(), t)) }

// Pos returns the nominal position at t, in picometres. A time outside the
// trajectory maps to its nearest end.
func (tr *Trajectory) Pos(t int64) fx.Vec {
	t = tr.clamp(t)
	return tr.LegAt(t).Pos(t)
}

// VelAt returns the nominal ground velocity at t.
func (tr *Trajectory) VelAt(t int64) fx.Vec { return tr.LegAt(tr.clamp(t)).V }

// AltAt returns the nominal altitude at t: linear inside a transition, and
// rounded down to a whole micrometre.
func (tr *Trajectory) AltAt(t int64) int64 {
	t = tr.clamp(t)
	s := tr.vsegAt(t)
	return altAt(s, t)
}

func altAt(s *VSeg, t int64) int64 {
	// |A1 − A0| <= 2^42 and t − T0 <= 2^51: the quotient is inside ±2^42.
	d, _ := fx.Mul(s.A1-s.A0, t-s.T0).DivFloor(s.T1 - s.T0)
	return s.A0 + d
}

// VSegAt returns a copy of the vertical segment in effect at t.
func (tr *Trajectory) VSegAt(t int64) VSeg { return *tr.vsegAt(tr.clamp(t)) }

// MinSpeed returns the lowest ground speed of any leg, rounded down.
func (tr *Trajectory) MinSpeed() int64 {
	m := fx.Speed(tr.Legs[0].V)
	for _, l := range tr.Legs[1:] {
		m = min(m, fx.Speed(l.V))
	}
	return m
}

// SpeedOver returns the highest ground speed of any leg that overlaps the
// times t0 to t1, rounded up, and the number of those legs. With no such leg
// it returns the speed of the leg in effect at t0, and one.
func (tr *Trajectory) SpeedOver(t0, t1 int64) (speed int64, legs int) {
	for _, l := range tr.Legs {
		if l.T1 < t0 || l.T0 > t1 {
			continue
		}
		speed = max(speed, fx.SpeedUp(l.V))
		legs++
	}
	if legs == 0 {
		return fx.SpeedUp(tr.LegAt(t0).V), 1
	}
	return speed, legs
}

// Shift returns a copy with dt added to every time. The caller must validate
// the result.
func (tr *Trajectory) Shift(dt int64) *Trajectory {
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

// Splice returns base up to time t followed by tail. t must lie strictly
// inside base, and tail must start at t. Splice returns an error if the
// result is not a valid trajectory: the caller makes tail continuous with
// base in position and in altitude.
func Splice(base *Trajectory, t int64, tail *Trajectory) (*Trajectory, error) {
	if t <= base.Start() || t >= base.End() {
		return nil, errors.New("splice time outside base")
	}
	if tail.Start() != t {
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
			s.A1 = altAt(&s, t)
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
