// Package safetynet is the Tactical Safety Net: the last ground-based layer.
//
// It is independent of the kernel by construction. It imports nothing from
// this module, it does not read contracts, and it has its own arithmetic.
// It predicts from state only: position, velocity, altitude, vertical rate,
// and the selected altitude that the aircraft broadcasts.
//
// Units: nautical miles, feet, seconds.
package safetynet

import "math"

// Track is one aircraft state.
type Track struct {
	ID     int
	X, Y   float64 // nm
	VX, VY float64 // nm/s
	Alt    float64 // ft
	VRate  float64 // ft/s
	SelAlt float64 // ft; NaN if not broadcast
}

// Params are the alerting thresholds.
type Params struct {
	Sh        float64 // horizontal threshold (nm)
	SvAlert   float64 // vertical threshold between true altitudes (ft)
	LookAhead float64 // prediction time (s)
	Cooldown  float64 // least time between two instructions for one pair (s)
	MinAlt    float64 // lowest altitude the net may instruct (ft)
	MaxAlt    float64 // highest altitude the net may instruct (ft)
	ClimbRate float64 // vertical rate the net assumes for its own instruction (ft/s)
}

// DefaultParams returns the initial design values.
func DefaultParams() Params {
	return Params{Sh: 5, SvAlert: 700, LookAhead: 120, Cooldown: 60, MinAlt: 28000, MaxAlt: 42000, ClimbRate: 40}
}

// Alert is one predicted loss of separation.
type Alert struct {
	A, B       int
	TimeToLoss float64
	Instruct   bool    // the net issues an instruction with this alert
	Flight     int     // the aircraft instructed
	TargetAlt  float64 // the altitude instructed
}

// Net holds the cooldown state between steps.
type Net struct {
	P    Params
	last map[[2]int]float64
}

func New(p Params) *Net { return &Net{P: p, last: map[[2]int]float64{}} }

// altAt extrapolates altitude. An aircraft that broadcasts a selected
// altitude is assumed to level off there.
func altAt(tr Track, t float64) float64 {
	a := tr.Alt + tr.VRate*t
	if math.IsNaN(tr.SelAlt) || tr.VRate == 0 {
		return a
	}
	if tr.VRate > 0 && tr.SelAlt >= tr.Alt && a > tr.SelAlt {
		return tr.SelAlt
	}
	if tr.VRate < 0 && tr.SelAlt <= tr.Alt && a < tr.SelAlt {
		return tr.SelAlt
	}
	return a
}

// predict returns the first time within the look-ahead at which a and b are
// closer than both thresholds.
func (n *Net) predict(a, b Track) (float64, bool) {
	dx, dy := a.X-b.X, a.Y-b.Y
	dvx, dvy := a.VX-b.VX, a.VY-b.VY
	T := n.P.LookAhead
	if math.Hypot(dx, dy) > n.P.Sh+math.Hypot(dvx, dvy)*T {
		return 0, false
	}
	if math.Abs(a.Alt-b.Alt) > n.P.SvAlert+(math.Abs(a.VRate)+math.Abs(b.VRate))*T {
		return 0, false
	}
	const step = 2.0
	for t := 0.0; t <= T; t += step {
		if math.Hypot(dx+dvx*t, dy+dvy*t) < n.P.Sh && math.Abs(altAt(a, t)-altAt(b, t)) < n.P.SvAlert {
			return t, true
		}
	}
	return 0, false
}

// Step examines every pair of tracks and returns the alerts, soonest first in
// the order of the input. For a pair that is not in cooldown it also chooses
// one aircraft and one altitude, and marks the alert as an instruction.
//
// Guarantee: the choice depends only on the tracks passed in. The net does
// not know whether an aircraft will turn or change speed.
func (n *Net) Step(now float64, tracks []Track) []Alert {
	var out []Alert
	for i := range tracks {
		for j := i + 1; j < len(tracks); j++ {
			a, b := tracks[i], tracks[j]
			ttl, hit := n.predict(a, b)
			if !hit {
				continue
			}
			al := Alert{A: a.ID, B: b.ID, TimeToLoss: ttl}
			key := [2]int{a.ID, b.ID}
			if last, seen := n.last[key]; !seen || now-last >= n.P.Cooldown {
				if mover, target, ok := n.resolve(a, b, tracks); ok {
					al.Instruct, al.Flight, al.TargetAlt = true, mover.ID, target
					n.last[key] = now
				}
			}
			out = append(out, al)
		}
	}
	return out
}

// resolve chooses a vertical escape for the pair.
//
// If exactly one aircraft is changing altitude, that aircraft stops short of
// the other. Otherwise the aircraft with the higher identifier moves away
// from the other. If the first choice would bring the mover close to a third
// aircraft, the net tries the other aircraft in the opposite direction.
func (n *Net) resolve(a, b Track, all []Track) (Track, float64, bool) {
	type option struct {
		mover, other Track
		up           bool
	}
	moving := func(t Track) bool { return math.Abs(t.VRate) > 3 }
	var opts []option
	switch {
	case moving(a) && !moving(b):
		opts = []option{{a, b, a.Alt > b.Alt}, {b, a, b.Alt >= a.Alt}}
	case moving(b) && !moving(a):
		opts = []option{{b, a, b.Alt > a.Alt}, {a, b, a.Alt >= b.Alt}}
	default:
		hi, lo := a, b
		if b.ID > a.ID {
			hi, lo = b, a
		}
		opts = []option{{hi, lo, hi.Alt >= lo.Alt}, {lo, hi, lo.Alt > hi.Alt}}
	}
	target := func(o option) float64 {
		if o.up {
			return math.Ceil((o.other.Alt+1000)/1000) * 1000
		}
		return math.Floor((o.other.Alt-1000)/1000) * 1000
	}
	for _, o := range opts {
		tg := target(o)
		if tg < n.P.MinAlt || tg > n.P.MaxAlt {
			continue
		}
		m := o.mover
		m.SelAlt = tg
		m.VRate = n.P.ClimbRate
		if tg < m.Alt {
			m.VRate = -n.P.ClimbRate
		}
		clear := true
		for _, c := range all {
			if c.ID == o.mover.ID || c.ID == o.other.ID {
				continue
			}
			if _, hit := n.predict(m, c); hit {
				clear = false
				break
			}
		}
		if clear {
			return o.mover, tg, true
		}
	}
	// No clear option: issue the first one that is inside the altitude limits.
	for _, o := range opts {
		if tg := target(o); tg >= n.P.MinAlt && tg <= n.P.MaxAlt {
			return o.mover, tg, true
		}
	}
	return Track{}, 0, false
}
