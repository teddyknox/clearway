// Package planner holds the Tier B proposal sources: a heuristic Trajectory
// Planner, the deterministic Fallback Resolver, and a deliberately bad
// "chaos" planner that stands in for a faulty learned model.
//
// Nothing here is trusted. Every proposal goes through kernel.Propose, and the
// kernel decides.
package planner

import (
	"math"
	"math/rand/v2"
	"sort"

	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/traj"
)

// ClimbRate is the conservative vertical rate used to size a transition (ft/s).
const ClimbRate = 1000.0 / 60

// Plan is what the planner knows about a flight apart from its contract.
type Plan struct {
	Exit  geom.Vec2 // end point of every contract for this flight, outside the cell
	Speed float64   // filed ground speed (nm/s)
	Level float64   // filed level (ft)
}

// Counter counts the proposals of one source.
type Counter struct{ Proposed, Committed uint64 }

// Env is the planner's view of one cell at one planning step.
type Env struct {
	K        *kernel.Kernel
	Plans    map[kernel.FlightID]*Plan
	NoComm   map[kernel.FlightID]bool // flights that did not answer the last clearances
	Lead     float64                  // time from proposal to divergence point (s)
	MinLevel float64
	MaxLevel float64
	OnCommit func(id kernel.FlightID, c *kernel.Contract, source string)
	// OnPropose, if set, sees every proposal and the kernel's verdict.
	OnPropose func(id kernel.FlightID, tr *traj.Trajectory, source string, v kernel.Verdict)
	Stats     map[string]*Counter

	handled map[kernel.FlightID]bool
	lastTry map[[2]kernel.FlightID]float64
}

// Begin starts a planning step.
func (e *Env) Begin() {
	e.handled = map[kernel.FlightID]bool{}
	if e.lastTry == nil {
		e.lastTry = map[[2]kernel.FlightID]float64{}
	}
	if e.Stats == nil {
		e.Stats = map[string]*Counter{}
	}
}

func (e *Env) stat(source string) *Counter {
	c := e.Stats[source]
	if c == nil {
		c = &Counter{}
		e.Stats[source] = c
	}
	return c
}

func (e *Env) propose(id kernel.FlightID, tail *traj.Trajectory, source string) bool {
	c := e.stat(source)
	c.Proposed++
	v := e.K.Propose(id, tail, kernel.Automation)
	if e.OnPropose != nil {
		e.OnPropose(id, tail, source, v)
	}
	if !v.OK {
		return false
	}
	c.Committed++
	e.handled[id] = true
	e.OnCommit(id, v.Contract, source)
	return true
}

type cand struct {
	id   kernel.FlightID
	tail *traj.Trajectory
	cost float64
}

func build(td float64, pts []geom.Vec2, speeds []float64, a0, a1 float64) *traj.Trajectory {
	legs := traj.Route(td, pts, speeds)
	if len(legs) == 0 {
		return nil
	}
	end := legs[len(legs)-1].T1
	return &traj.Trajectory{Legs: legs, VSegs: traj.Step(td, end, a0, td, a1, ClimbRate)}
}

// remaining returns the path of the active contract after td.
func remaining(act *kernel.Contract, td float64) (pts []geom.Vec2, speeds []float64) {
	pts = []geom.Vec2{act.Traj.PosAt(td)}
	for _, l := range act.Traj.Legs {
		if l.T1 <= td {
			continue
		}
		pts = append(pts, l.EndPos())
		speeds = append(speeds, l.Speed())
	}
	return pts, speeds
}

// movable returns the flight if the planner may amend it now, with its
// divergence time and its level there.
func (e *Env) movable(id kernel.FlightID, before float64) (*kernel.Flight, float64, float64, bool) {
	f := e.K.Flight(id)
	if f == nil || f.Intruder || f.Active == nil || f.PState != kernel.PendingNone ||
		f.Authority != kernel.Automation || e.NoComm[id] || e.handled[id] {
		return nil, 0, 0, false
	}
	td := e.K.Now() + e.Lead
	if td >= f.Active.End()-30 || td <= f.Active.Start() || td >= before {
		return nil, 0, 0, false
	}
	sg := f.Active.Sigma(td)
	lo, hi := f.Active.Traj.BandOver(td-sg, td+sg)
	if lo != hi {
		return nil, 0, 0, false
	}
	return f, td, lo, true
}

// Manoeuvre kinds and their magnitudes, in the order a source prefers them.
type option struct {
	kind string  // "speed", "level", "dogleg"
	mag  float64 // speed factor, level change (ft), or lateral offset (nm)
	cost float64
}

var heuristicOptions = []option{
	{"speed", 0.94, 1}, {"speed", 1.06, 1},
	{"speed", 0.90, 2}, {"speed", 1.10, 2},
	{"level", 2000, 3}, {"level", -2000, 3},
	{"dogleg", 8, 4}, {"dogleg", -8, 4},
	{"level", 1000, 5}, {"level", -1000, 5},
	{"dogleg", 16, 6}, {"dogleg", -16, 6},
}

var fallbackOptions = []option{
	{"level", 1000, 1}, {"level", -1000, 1},
	{"level", 2000, 2}, {"level", -2000, 2},
	{"level", 3000, 3}, {"level", -3000, 3},
	{"dogleg", 12, 4}, {"dogleg", -12, 4},
	{"dogleg", 24, 5}, {"dogleg", -24, 5},
	{"speed", 0.90, 6}, {"speed", 1.10, 6},
}

func (e *Env) tail(f *kernel.Flight, td, level float64, o option) *traj.Trajectory {
	pts, speeds := remaining(f.Active, td)
	if len(speeds) == 0 {
		return nil
	}
	switch o.kind {
	case "speed":
		sp := make([]float64, len(speeds))
		for i, s := range speeds {
			sp[i] = s * o.mag
			if sp[i] < 400*geom.Kt || sp[i] > 520*geom.Kt {
				return nil
			}
		}
		return build(td, pts, sp, level, level)
	case "level":
		to := level + o.mag
		if to < e.MinLevel || to > e.MaxLevel {
			return nil
		}
		return build(td, pts, speeds, level, to)
	case "dogleg":
		p0, last := pts[0], pts[len(pts)-1]
		d := last.Sub(p0)
		if d.Norm() < 40 {
			return nil
		}
		via := p0.Add(d.Scale(0.4)).Add(d.Unit().Perp().Scale(o.mag))
		return build(td, []geom.Vec2{p0, via, last}, []float64{speeds[0], speeds[0]}, level, level)
	}
	return nil
}

// resolve tries to remove one predicted conflict with one amendment. It
// proposes candidates in order of cost until the kernel commits one.
func (e *Env) resolve(c kernel.Conflict, opts []option, source string, maxTries int) bool {
	ids := []kernel.FlightID{c.A}
	if !c.Intruder {
		ids = append(ids, c.B)
	}
	var cands []cand
	for _, id := range ids {
		f, td, level, ok := e.movable(id, c.T)
		if !ok {
			continue
		}
		for _, o := range opts {
			if t := e.tail(f, td, level, o); t != nil {
				cands = append(cands, cand{id, t, o.cost})
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].cost < cands[j].cost })
	for i, cd := range cands {
		if i >= maxTries {
			break
		}
		if e.propose(cd.id, cd.tail, source) {
			return true
		}
	}
	return false
}

// Heuristic is the normal Trajectory Planner of the prototype.
//
// It acts on a predicted conflict once the conflict is closer than ActAt.
// Farther out it waits: many far conflicts vanish when re-anchoring shrinks
// the envelopes.
type Heuristic struct {
	ActAt float64 // s
}

func (h *Heuristic) Act(e *Env) {
	now := e.K.Now()
	for _, c := range e.K.Conflicts() {
		if c.T-now > h.ActAt {
			continue
		}
		key := [2]kernel.FlightID{c.A, c.B}
		if last, ok := e.lastTry[key]; ok && now-last < 30 {
			continue
		}
		e.lastTry[key] = now
		e.resolve(c, heuristicOptions, "planner", 16)
	}
}

// Fallback is the deterministic Fallback Resolver. It acts only when a
// conflict is closer than At, and it prefers the most robust manoeuvres.
type Fallback struct {
	At float64 // s
}

func (fb *Fallback) Act(e *Env) {
	now := e.K.Now()
	for _, c := range e.K.Conflicts() {
		if c.T-now > fb.At {
			continue
		}
		e.resolve(c, fallbackOptions, "fallback", 24)
	}
}

// Recontract proposes a new contract for every intruder that can receive one:
// direct to its exit point, at the level it is at or is heading for.
func Recontract(e *Env) {
	now := e.K.Now()
	for _, id := range e.K.IDs() {
		f := e.K.Flight(id)
		plan := e.Plans[id]
		if !f.Intruder || !f.HasTrack || f.Active != nil || f.PState != kernel.PendingNone ||
			f.Authority != kernel.Automation || e.NoComm[id] || plan == nil {
			continue
		}
		key := [2]kernel.FlightID{id, id}
		if last, ok := e.lastTry[key]; ok && now-last < 20 {
			continue
		}
		e.lastTry[key] = now
		tk := f.Track
		if tk.Pos.Norm() > e.K.P.CellRadius && tk.Pos.Dot(tk.Vel) > 0 {
			continue // leaving the cell
		}
		ts := now + e.Lead
		p0 := tk.Pos.Add(tk.Vel.Scale(ts - tk.T))
		a0 := tk.Alt + tk.VRate*(ts-tk.T)
		heading := a0
		if !math.IsNaN(tk.SelAlt) {
			heading = tk.SelAlt
			if (tk.VRate >= 0 && a0 > tk.SelAlt) || (tk.VRate <= 0 && a0 < tk.SelAlt) {
				a0 = tk.SelAlt
			}
		}
		speed := tk.Vel.Norm()
		if speed < 380*geom.Kt || speed > 540*geom.Kt {
			speed = plan.Speed
		}
		base := math.Round(heading/1000) * 1000
		c := e.stat("recontract")
		for _, d := range []float64{0, 1000, -1000, 2000, -2000} {
			lvl := base + d
			if lvl < e.MinLevel || lvl > e.MaxLevel {
				continue
			}
			tr := build(ts, []geom.Vec2{p0, plan.Exit}, []float64{speed}, a0, lvl)
			if tr == nil {
				break
			}
			c.Proposed++
			v := e.K.ProposeRecontract(id, tr, kernel.Automation)
			if e.OnPropose != nil {
				e.OnPropose(id, tr, "recontract", v)
			}
			if v.OK {
				c.Committed++
				e.OnCommit(id, v.Contract, "recontract")
				break
			}
		}
	}
}

// Chaos is a planner that should not be trusted. It proposes random and
// hostile amendments at a high rate: random levels, random detours, routes
// aimed at other aircraft, contracts that end inside the cell, and tails that
// do not connect. It handles real conflicts only half of the time, with a
// random manoeuvre.
//
// It exists to test one claim: the kernel keeps separation whatever the
// planner proposes.
type Chaos struct {
	Rate float64 // unsolicited proposals per planning step
	R    *rand.Rand
}

func (c *Chaos) Act(e *Env) {
	now := e.K.Now()
	ids := e.K.IDs()
	n := int(c.Rate)
	if c.R.Float64() < c.Rate-float64(n) {
		n++
	}
	for i := 0; i < n && len(ids) > 0; i++ {
		id := ids[c.R.IntN(len(ids))]
		f := e.K.Flight(id)
		plan := e.Plans[id]
		if f.Active == nil || f.Intruder || plan == nil {
			continue
		}
		td := now + e.Lead + c.R.Float64()*120
		if c.R.Float64() < 0.05 {
			td = now + 30 // too late to deliver
		}
		if td >= f.Active.End()-30 || td <= f.Active.Start() {
			continue
		}
		p0 := f.Active.Traj.PosAt(td)
		level := f.Active.Traj.AltAt(td)
		speed := plan.Speed
		anyLevel := e.MinLevel + 1000*float64(c.R.IntN(int((e.MaxLevel-e.MinLevel)/1000)+1))
		var tail *traj.Trajectory
		switch c.R.IntN(6) {
		case 0: // a random level
			pts, speeds := remaining(f.Active, td)
			tail = build(td, pts, speeds, level, anyLevel)
		case 1: // a random detour
			via := geom.Polar(c.R.Float64()*e.K.P.CellRadius, c.R.Float64()*2*math.Pi)
			tail = build(td, []geom.Vec2{p0, via, plan.Exit}, []float64{speed, speed}, level, level)
		case 2: // a random speed
			pts, speeds := remaining(f.Active, td)
			k := 0.85 + 0.3*c.R.Float64()
			for i := range speeds {
				speeds[i] *= k
			}
			tail = build(td, pts, speeds, level, level)
		case 3: // aim at another aircraft, at its level
			other := e.K.Flight(ids[c.R.IntN(len(ids))])
			if other.ID == id || other.Active == nil {
				continue
			}
			meet := td + 120 + c.R.Float64()*400
			via := other.Active.Traj.PosAt(meet)
			sp := p0.Dist(via) / (meet - td)
			if sp < 300*geom.Kt || sp > 600*geom.Kt {
				sp = speed
			}
			tail = build(td, []geom.Vec2{p0, via, plan.Exit}, []float64{sp, speed}, level, other.Active.Traj.AltAt(meet))
		case 4: // an open contract: it ends inside the cell
			end := geom.Polar(c.R.Float64()*e.K.P.CellRadius*0.8, c.R.Float64()*2*math.Pi)
			tail = build(td, []geom.Vec2{p0, end}, []float64{speed}, level, level)
		case 5: // a tail that does not connect
			tail = build(td, []geom.Vec2{p0.Add(geom.Vec2{X: 3}), plan.Exit}, []float64{speed}, level, anyLevel)
		}
		if tail != nil {
			e.propose(id, tail, "chaos")
		}
	}
	for _, cf := range e.K.Conflicts() {
		if c.R.Float64() < 0.5 {
			continue
		}
		id := cf.A
		if !cf.Intruder && c.R.Float64() < 0.5 {
			id = cf.B
		}
		f, td, level, ok := e.movable(id, cf.T)
		if !ok {
			continue
		}
		o := heuristicOptions[c.R.IntN(len(heuristicOptions))]
		if t := e.tail(f, td, level, o); t != nil {
			e.propose(id, t, "chaos")
		}
	}
}
