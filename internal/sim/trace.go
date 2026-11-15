package sim

import (
	"fmt"
	"math"
	"strings"

	"github.com/teddyknox/clearway/internal/conv"
	"github.com/teddyknox/clearway/internal/ftraj"
	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/traj"
)

// Recording is a replayable trace of one run, made for the viewer. It holds
// what an observer needs to redraw the cell at any time: the true aircraft
// states, what the kernel believed about each flight, every proposal with its
// verdict, and the events of the layers around the kernel.
type Recording struct {
	Meta    TraceMeta    `json:"meta"`
	Paths   []TracePath  `json:"paths"`
	Frames  []TraceFrame `json:"frames"`
	Events  []TraceEvent `json:"events"`
	Summary string       `json:"summary"`
}

// TraceMeta describes the run and the layout of the compact arrays.
type TraceMeta struct {
	Seed            uint64   `json:"seed"`
	Duration        float64  `json:"duration"`
	FrameEvery      float64  `json:"frameEvery"`
	Radius          float64  `json:"radius"`
	Sh              float64  `json:"sh"`
	SvTrue          float64  `json:"svTrue"`
	K               float64  `json:"k"` // nm/s
	IntruderK       float64  `json:"intruderK"`
	IntruderHorizon float64  `json:"intruderHorizon"`
	Horizon         float64  `json:"horizon"`
	NetLookAhead    float64  `json:"netLookAhead"`
	Planner         string   `json:"planner"`
	Verifier        string   `json:"verifier"`
	Fallback        bool     `json:"fallback"`
	SafetyNet       bool     `json:"safetyNet"`
	Rate            float64  `json:"rate"`
	Scripted        bool     `json:"scripted"`
	Counters        []string `json:"counters"` // names of TraceFrame.Counts
	Aircraft        []string `json:"aircraft"` // names of the columns of TraceFrame.Aircraft
}

// TracePath is the horizontal path of one contract, with the places where it
// changes level. Contracts that differ only in timing share one path.
type TracePath struct {
	Points [][2]float64 `json:"p"`
	Steps  [][4]float64 `json:"s,omitempty"` // x, y, from altitude, to altitude
}

// TraceFrame is the cell at one time.
type TraceFrame struct {
	T         float64     `json:"t"`
	Aircraft  [][]float64 `json:"a"`           // one row per aircraft; see TraceMeta.Aircraft
	Conflicts [][]float64 `json:"c,omitempty"` // flight, flight, time of conflict, 1 if with an intruder, x, y
	Alerts    [][]float64 `json:"l,omitempty"` // flight, flight, time to loss
	Coast     float64     `json:"k"`           // s; -1 if no pair limits it
	Counts    []int       `json:"n"`           // cumulative; see TraceMeta.Counters
}

// TraceEvent is one thing that happened. Kind selects which fields are set.
type TraceEvent struct {
	T    float64      `json:"t"`
	Kind string       `json:"k"`
	A    int          `json:"a,omitempty"`
	B    int          `json:"b,omitempty"`
	Src  string       `json:"src,omitempty"`
	OK   bool         `json:"ok,omitempty"`
	Why  string       `json:"why,omitempty"`
	Pts  [][2]float64 `json:"p,omitempty"`
	Alt  float64      `json:"alt,omitempty"`
	Num  float64      `json:"num,omitempty"`
	Txt  string       `json:"txt,omitempty"`
}

// Aircraft states as the kernel sees them.
const (
	stContract = iota // conforming to its active contract
	stPending         // an amendment is waiting for an answer
	stUnknown         // delivery failed: the aircraft may fly either contract
	stIntruder        // no conforming contract
)

// Truth flags, known to the simulator and not to the kernel.
const (
	flOffRoute   = 1 << iota // flying straight, not on its trajectory
	flOffLevel               // leaving its cleared level
	flNoRadio                // receives no clearance
	flInstructed             // under a Safety Net instruction in the last 5 minutes
	flInside                 // inside the cell
)

var aircraftColumns = []string{"id", "x", "y", "vx", "vy", "alt", "state", "radius", "path", "pending", "flags", "sel"}

type prevState struct {
	intruder bool
	pstate   kernel.PendingState
	active   *kernel.Contract
}

type recorder struct {
	every  float64
	rec    *Recording
	byPtr  map[*traj.Trajectory]int
	byKey  map[string]int
	prev   map[kernel.FlightID]prevState
	exits  [4]uint64
	alerts [][]float64
	alarms int
}

func r2(x float64) float64  { return math.Round(x*100) / 100 }
func r10(x float64) float64 { return math.Round(x/10) * 10 }

// Trace executes one simulation and returns its recording and its report.
func Trace(cfg Config) (*Recording, *Report, error) {
	w, err := newWorld(cfg)
	if err != nil {
		return nil, nil, err
	}
	p := cfg.Kernel
	counters := []string{"proposals", "commits"}
	for i := kernel.RejectReason(1); i < kernel.NumReasons; i++ {
		counters = append(counters, "rej:"+i.String())
	}
	counters = append(counters, "sent", "acked", "refused", "failed", "admitted", "completed",
		"alertsVerified", "alertsUnresolved", "alertsIntruder", "instructions",
		"lossKernel", "lossLatency", "lossUnresolved", "lossIntruder", "hard", "marginal", "reanchors", "exits", "inCell")
	r := &recorder{
		every: 5,
		byPtr: map[*traj.Trajectory]int{},
		byKey: map[string]int{},
		prev:  map[kernel.FlightID]prevState{},
		rec: &Recording{Meta: TraceMeta{
			Seed: cfg.Seed, Duration: cfg.Duration, FrameEvery: 5, Radius: conv.NM(p.CellRadius),
			Sh: conv.NM(p.Sh), SvTrue: conv.Ft(p.SvTrue()), K: conv.NMps(p.K), IntruderK: conv.NMps(p.IntruderK),
			IntruderHorizon: conv.Sec(p.IntruderHorizon),
			Horizon:         conv.Sec(p.Horizon), NetLookAhead: w.net.P.LookAhead,
			Planner: cfg.Planner, Verifier: cfg.Verifier, Fallback: cfg.Fallback, SafetyNet: cfg.SafetyNet,
			Rate: cfg.FlightsPerHour, Scripted: len(cfg.Scripted) > 0,
			Counters: counters, Aircraft: aircraftColumns,
		}},
	}
	w.tr = r
	w.env.OnPropose = func(id kernel.FlightID, tr *ftraj.Trajectory, source string, v kernel.Verdict) {
		e := TraceEvent{T: w.now, Kind: "propose", A: int(id), Src: source, OK: v.OK, Pts: polyline(tr)}
		e.Alt = tr.VSegs[len(tr.VSegs)-1].A1
		if !v.OK {
			e.Why, e.B = v.Reason.String(), int(v.With)
		}
		r.event(e)
	}
	w.k.D.OnHard = func(a, b *kernel.Tube, _, _ int64, _, _ kernel.Result) {
		// A faulty implementation can raise thousands of alarms. The counter
		// in each frame has them all; the event list keeps the first ones.
		if r.alarms++; r.alarms <= 400 {
			r.event(TraceEvent{T: w.now, Kind: "alarm", A: int(a.Flight), B: int(b.Flight)})
		}
	}
	w.run()
	r.rec.Summary = w.rep.String()
	return r.rec, &w.rep, nil
}

func polyline(tr *ftraj.Trajectory) [][2]float64 {
	pts := make([][2]float64, 0, len(tr.Legs)+1)
	p0 := tr.Legs[0].P0
	pts = append(pts, [2]float64{r2(p0.X), r2(p0.Y)})
	for _, l := range tr.Legs {
		e := l.EndPos()
		pts = append(pts, [2]float64{r2(e.X), r2(e.Y)})
	}
	return pts
}

func (r *recorder) event(e TraceEvent) {
	if r == nil {
		return
	}
	e.T = r2(e.T)
	e.Num, e.Alt = r2(e.Num), r10(e.Alt)
	r.rec.Events = append(r.rec.Events, e)
}

func (r *recorder) alerting(key [2]kernel.FlightID, ttl float64) {
	if r == nil {
		return
	}
	r.alerts = append(r.alerts, []float64{float64(key[0]), float64(key[1]), ttl})
}

// path returns the index of the path of tr, adding it if it is new.
func (r *recorder) path(tr *traj.Trajectory) float64 {
	if tr == nil {
		return -1
	}
	if i, ok := r.byPtr[tr]; ok {
		return float64(i)
	}
	fl := conv.Float(tr)
	p := TracePath{Points: polyline(fl)}
	for _, s := range fl.VSegs {
		if !s.Level() {
			at := fl.PosAt(s.T0)
			p.Steps = append(p.Steps, [4]float64{r2(at.X), r2(at.Y), s.A0, s.A1})
		}
	}
	var b strings.Builder
	fmt.Fprint(&b, p.Points, p.Steps)
	key := b.String()
	i, ok := r.byKey[key]
	if !ok {
		i = len(r.rec.Paths)
		r.rec.Paths = append(r.rec.Paths, p)
		r.byKey[key] = i
	}
	r.byPtr[tr] = i
	return float64(i)
}

// frame records the state of the cell after one surveillance step, and the
// changes of kernel state since the step before.
func (r *recorder) frame(w *world) {
	if r == nil {
		return
	}
	k := w.k

	// Which cause did the Monitor report since the last step? If more than
	// one, the event does not name it.
	cause, causes := "", 0
	names := [4]string{"left its route envelope", "left its level", "selected a wrong altitude", "instructed by the Safety Net"}
	for i, n := range k.Stats.Exits {
		if n > r.exits[i] {
			cause = names[i]
			causes++
		}
	}
	if causes != 1 {
		cause = "left its contract"
	}
	r.exits = k.Stats.Exits

	next := make(map[kernel.FlightID]prevState, len(w.order))
	fr := TraceFrame{T: w.now, Coast: -1, Aircraft: [][]float64{}}
	R := conv.NM(w.cfg.Kernel.CellRadius)
	inCell := 0
	for _, id := range w.order {
		ac, f := w.ac[id], k.Flight(id)
		if f == nil {
			continue
		}
		p, seen := r.prev[id]
		switch {
		case f.Intruder && !p.intruder:
			r.event(TraceEvent{T: w.now, Kind: "intruder", A: int(id), Txt: cause})
		case !f.Intruder && p.intruder:
			r.event(TraceEvent{T: w.now, Kind: "recontracted", A: int(id)})
		case seen && p.pstate == kernel.PendingUnknown && f.PState == kernel.PendingNone && !f.Intruder:
			which := "old"
			if f.Active != p.active {
				which = "new"
			}
			r.event(TraceEvent{T: w.now, Kind: "settled", A: int(id), Txt: which})
		}
		next[id] = prevState{f.Intruder, f.PState, f.Active}

		state, radius := float64(stContract), 0.0
		path, pending := -1.0, -1.0
		switch {
		case f.Intruder:
			state, radius = stIntruder, conv.NM(w.cfg.Kernel.IntruderR0)
		case f.PState == kernel.PendingUnknown:
			state = stUnknown
		case f.PState == kernel.PendingAwaiting:
			state = stPending
		}
		if f.Active != nil {
			path = r.path(f.Active.Traj)
			if !f.Intruder {
				// The radius is below 2^53 pm here, so the conversion is exact.
				radius = float64(f.Active.RadiusPM(k.Now()).Int64()) / (fx.NM * fx.M)
			}
		}
		if f.Pending != nil {
			pending = r.path(f.Pending.Traj)
		}
		flags := 0
		if ac.mode == modeFree {
			flags |= flOffRoute
		}
		if ac.busting {
			flags |= flOffLevel
		}
		if ac.arr.nordo {
			flags |= flNoRadio
		}
		if ins, ok := w.instructed[id]; ok && w.now-ins.at <= 300 {
			flags |= flInstructed
		}
		if ac.pos.Norm() <= R {
			flags |= flInside
			inCell++
		}
		fr.Aircraft = append(fr.Aircraft, []float64{
			float64(id), r2(ac.pos.X), r2(ac.pos.Y),
			math.Round(ac.vel.X / geom.Kt), math.Round(ac.vel.Y / geom.Kt),
			r10(ac.alt), state, r2(radius), path, pending, float64(flags), r10(ac.selAlt),
		})
	}
	r.prev = next

	for _, c := range k.Conflicts() {
		fa, fb := k.Flight(c.A), k.Flight(c.B)
		if fa == nil || fb == nil || fa.Active == nil {
			continue
		}
		pa := conv.PM(fa.Active.Traj.Pos(c.T))
		var pb geom.Vec2
		switch {
		case c.Intruder && fb.ITube != nil:
			pb = conv.PM(fb.ITube.Traj.Pos(c.T))
		case !c.Intruder && fb.Active != nil:
			pb = conv.PM(fb.Active.Traj.Pos(c.T))
		default:
			continue
		}
		mid := pa.Add(pb).Scale(0.5)
		intr := 0.0
		if c.Intruder {
			intr = 1
		}
		fr.Conflicts = append(fr.Conflicts, []float64{float64(c.A), float64(c.B), r2(conv.Sec(c.T)), intr, r2(mid.X), r2(mid.Y)})
	}
	fr.Alerts, r.alerts = r.alerts, nil
	if c := conv.Sec(k.Coast()); !math.IsInf(c, 1) {
		fr.Coast = math.Round(c)
	}

	st := k.Stats
	n := []int{int(st.Proposals), int(st.Commits)}
	for i := kernel.RejectReason(1); i < kernel.NumReasons; i++ {
		n = append(n, int(st.Rejects[i]))
	}
	var exits uint64
	for _, e := range st.Exits {
		exits += e
	}
	rp := &w.rep
	fr.Counts = append(n, rp.ClearancesSent, rp.ClearancesAcked, rp.ClearancesRefused, rp.ClearancesFailed,
		rp.Admitted, rp.Completed, rp.AlertsVerified, rp.AlertsUnresolved, rp.AlertsIntruder, rp.Instructions,
		rp.LossKernel, rp.LossLatency, rp.LossUnresolved, rp.LossIntruder,
		int(k.D.Hard), int(k.D.Marginal), int(st.ReanchorOK), int(exits), inCell)
	r.rec.Frames = append(r.rec.Frames, fr)
}
