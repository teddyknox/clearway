// Package conv converts between the floating-point values of the simulator
// and the planners (nautical miles, feet, seconds) and the integers of the
// kernel (micrometres, microseconds; package fx).
//
// The kernel does not use this package. Everything that enters the kernel
// passes through it once and is an integer from then on.
package conv

import (
	"math"

	"github.com/teddyknox/clearway/internal/ftraj"
	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/traj"
)

// outside is beyond every limit of package fx. A value that is not a number,
// or is too large, becomes ±outside, so that the kernel rejects it. The
// conversion of such a value by the processor would differ between machines.
const outside = 1 << 62

func round(x float64) int64 {
	switch {
	case x != x, x >= outside:
		return outside
	case x <= -outside:
		return -outside
	}
	return int64(math.Round(x))
}

// Time converts seconds to microseconds, and Sec converts back. Sec returns
// +Inf for fx.NoLimit.
func Time(s float64) int64 { return round(s * fx.Sec) }

func Sec(t int64) float64 {
	if t == fx.NoLimit {
		return math.Inf(1)
	}
	return float64(t) / fx.Sec
}

// Len converts nautical miles to micrometres, and NM converts back.
func Len(nm float64) int64 { return round(nm * fx.NM) }
func NM(um int64) float64  { return float64(um) / fx.NM }

// Alt converts feet to micrometres, and Ft converts back.
func Alt(ft float64) int64 { return round(ft * fx.FT) }
func Ft(um int64) float64  { return float64(um) / fx.FT }

// Speed converts nautical miles per second to micrometres per second, and
// NMps converts back. VRate and Ftps do the same for feet per second.
func Speed(nmps float64) int64 { return round(nmps * fx.NM) }
func NMps(v int64) float64     { return float64(v) / fx.NM }
func VRate(ftps float64) int64 { return round(ftps * fx.FT) }
func Ftps(v int64) float64     { return float64(v) / fx.FT }

// Point and Vel convert a position and a velocity to integers. NMPoint and
// NMVel convert back. PM converts a position in picometres to nautical miles.
func Point(p geom.Vec2) fx.Vec   { return fx.Vec{X: Len(p.X), Y: Len(p.Y)} }
func Vel(v geom.Vec2) fx.Vec     { return fx.Vec{X: Speed(v.X), Y: Speed(v.Y)} }
func NMPoint(p fx.Vec) geom.Vec2 { return geom.Vec2{X: NM(p.X), Y: NM(p.Y)} }
func NMVel(v fx.Vec) geom.Vec2   { return geom.Vec2{X: NMps(v.X), Y: NMps(v.Y)} }
func PM(p fx.Vec) geom.Vec2 {
	return geom.Vec2{X: float64(p.X) / (fx.NM * fx.M), Y: float64(p.Y) / (fx.NM * fx.M)}
}

// The tolerances of package ftraj: two values this close are meant to be
// equal.
const (
	timeEps = 1e-6 // s
	posEps  = 1e-5 // nm
	altEps  = 1e-6 // ft
)

// Traj converts a trajectory to integers.
//
// Where the floating-point trajectory is continuous, the result is exactly
// continuous in time and in altitude, and each leg starts within a micrometre
// of the end of the leg before it. Where it is not, the result keeps the gap,
// and the kernel rejects it. A leg or a segment shorter than a microsecond is
// dropped. Traj does not validate: the kernel does.
func Traj(tr *ftraj.Trajectory) *traj.Trajectory {
	out := &traj.Trajectory{}
	if tr == nil {
		return out
	}
	for i, l := range tr.Legs {
		t0, t1, p0 := Time(l.T0), Time(l.T1), Point(l.P0)
		if n := len(out.Legs); i > 0 && n > 0 {
			prev, was := &out.Legs[n-1], tr.Legs[i-1]
			if math.Abs(was.T1-l.T0) <= timeEps {
				t0 = prev.T1
			}
			if end, ok := endOf(prev); ok && was.EndPos().Dist(l.P0) <= posEps {
				p0 = end
			}
		}
		if t1 <= t0 && l.T1 > l.T0 {
			continue
		}
		out.Legs = append(out.Legs, traj.Leg{T0: t0, T1: t1, P0: p0, V: Vel(l.V)})
	}
	for i, s := range tr.VSegs {
		t0, t1, a0, a1 := Time(s.T0), Time(s.T1), Alt(s.A0), Alt(s.A1)
		if n := len(out.VSegs); i > 0 && n > 0 {
			prev, was := &out.VSegs[n-1], tr.VSegs[i-1]
			if math.Abs(was.T1-s.T0) <= timeEps {
				t0 = prev.T1
			}
			if math.Abs(was.A1-s.A0) <= altEps {
				a0 = prev.A1
			}
		}
		if t1 <= t0 && s.T1 > s.T0 {
			continue
		}
		out.VSegs = append(out.VSegs, traj.VSeg{T0: t0, T1: t1, A0: a0, A1: a1})
	}
	// The vertical profile covers the span of the legs.
	if nl, nv := len(out.Legs), len(out.VSegs); nl > 0 && nv > 0 && len(tr.Legs) > 0 && len(tr.VSegs) > 0 {
		if math.Abs(tr.VSegs[0].T0-tr.Legs[0].T0) <= timeEps {
			out.VSegs[0].T0 = out.Legs[0].T0
		}
		if math.Abs(tr.VSegs[len(tr.VSegs)-1].T1-tr.Legs[len(tr.Legs)-1].T1) <= timeEps {
			out.VSegs[nv-1].T1 = out.Legs[nl-1].T1
		}
	}
	return out
}

// endOf returns the end of a leg, rounded to the nearest micrometre. ok is
// false if the leg is too far outside the limits to compute with.
func endOf(l *traj.Leg) (end fx.Vec, ok bool) {
	axis := func(p0, v int64) (int64, bool) {
		pm := fx.Mul(p0, fx.M).Add(fx.Mul(v, l.T1-l.T0)).Add(fx.Int(fx.M / 2))
		return pm.DivFloor(fx.M)
	}
	if l.T1-l.T0 < 0 || l.T1-l.T0 > 2*fx.MaxTime {
		return end, false
	}
	var okX, okY bool
	end.X, okX = axis(l.P0.X, l.V.X)
	end.Y, okY = axis(l.P0.Y, l.V.Y)
	return end, okX && okY
}

// Float converts a trajectory of the kernel to floating point. The result is
// exact up to the rounding of one division for each value.
func Float(tr *traj.Trajectory) *ftraj.Trajectory {
	if tr == nil {
		return nil
	}
	out := &ftraj.Trajectory{Legs: make([]ftraj.Leg, len(tr.Legs)), VSegs: make([]ftraj.VSeg, len(tr.VSegs))}
	for i, l := range tr.Legs {
		out.Legs[i] = ftraj.Leg{T0: Sec(l.T0), T1: Sec(l.T1), P0: NMPoint(l.P0), V: NMVel(l.V)}
	}
	for i, s := range tr.VSegs {
		out.VSegs[i] = ftraj.VSeg{T0: Sec(s.T0), T1: Sec(s.T1), A0: Ft(s.A0), A1: Ft(s.A1)}
	}
	return out
}
