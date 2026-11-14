// Package sim is a fast-time simulation of one en route cell.
//
// It owns everything the kernel must not own: true aircraft positions, random
// traffic, communication delays, and injected faults. It measures what the
// kernel and the layers around it achieve.
package sim

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"

	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/planner"
	"github.com/teddyknox/clearway/internal/safetynet"
	"github.com/teddyknox/clearway/internal/traj"
)

// Config selects one simulation run.
type Config struct {
	Seed           uint64
	Duration       float64 // s
	FlightsPerHour float64
	Planner        string // "heuristic", "chaos", or "none"
	ChaosRate      float64
	Fallback       bool
	SafetyNet      bool    // the net issues instructions; alerts are always counted
	NetLookAhead   float64 // Safety Net prediction time (s)
	Verifier       string  // "dual", "single", "broken-single", or "broken-dual"
	Kernel         kernel.Params
	StepFraction   float64 // share of flights with a planned level change

	PBlunder    float64 // per flight: leaves its route
	PLevelBust  float64 // per flight: leaves its level
	PNordo      float64 // per flight: receives no clearance
	PLostUplink float64 // per clearance sent
	PLostReply  float64 // per reply sent
	PRefuse     float64 // per clearance received

	Record bool // keep the kernel input log

	// Scripted, if not empty, replaces random traffic with these flights.
	Scripted []ScriptedFlight
}

// ScriptedFlight is one hand-placed flight for a demonstration scenario.
// Angles are in degrees, counter-clockwise from east, on the cell boundary.
type ScriptedFlight struct {
	At       float64 // time of the admission request (s); entry follows 15 min later
	EntryDeg float64
	ExitDeg  float64
	Level    float64 // ft
	SpeedKt  float64
	StepFrac float64 // planned level change at this fraction of the transit; 0 for none
	StepBy   float64 // ft

	BlunderFrac float64 // the aircraft leaves its route at this fraction; 0 for none
	BlunderDeg  float64
	BustFrac    float64 // the aircraft leaves its level at this fraction; 0 for none
	BustBy      float64
	NoRadio     bool
}

// DefaultConfig returns a six-hour run at 120 flights per hour with all
// layers on and a low rate of communication faults.
func DefaultConfig() Config {
	return Config{
		Seed: 1, Duration: 6 * 3600, FlightsPerHour: 120,
		Planner: "heuristic", ChaosRate: 2, Fallback: true, SafetyNet: true, NetLookAhead: 120, Verifier: "dual",
		Kernel: kernel.DefaultParams(), StepFraction: 0.2,
		PLostUplink: 0.01, PLostReply: 0.01, PRefuse: 0.02,
	}
}

const (
	lead         = 150.0 // s from proposal to divergence point
	approachTime = 900.0 // s from admission request to the cell boundary
	minLevel     = 29000.0
	maxLevel     = 41000.0
	replyTimeout = 45.0
)

// flattenChecker is a faulty Verifier implementation for experiments. It
// reads a vertical transition as level flight at the starting altitude, so it
// approves climbs and descents through occupied levels.
type flattenChecker struct{ inner kernel.PairChecker }

func (f flattenChecker) Name() string { return "faulty-" + f.inner.Name() }

func flatten(tb *kernel.Tube) *kernel.Tube {
	changed := false
	vs := make([]traj.VSeg, len(tb.Traj.VSegs))
	for i, s := range tb.Traj.VSegs {
		if !tb.Intruder && !s.Level() {
			s.A1, changed = s.A0, true
		}
		vs[i] = s
	}
	if !changed {
		return tb
	}
	return tb.WithTraj(&traj.Trajectory{Legs: tb.Traj.Legs, VSegs: vs})
}

func (f flattenChecker) FirstConflict(a, b *kernel.Tube, from, to float64, sp kernel.SepParams) kernel.Result {
	return f.inner.FirstConflict(flatten(a), flatten(b), from, to, sp)
}

// NewVerifier builds the Verifier arrangement named by mode.
func NewVerifier(mode string) (*kernel.Dual, error) {
	a, b := kernel.PairChecker(kernel.AnalyticChecker{}), kernel.PairChecker(kernel.SampledChecker{Step: 1})
	switch mode {
	case "dual":
		return &kernel.Dual{A: a, B: b, Tol: 0.75, TimeTol: 1}, nil
	case "single":
		return &kernel.Dual{A: a}, nil
	case "broken-single":
		return &kernel.Dual{A: flattenChecker{a}}, nil
	case "broken-dual":
		return &kernel.Dual{A: flattenChecker{a}, B: b, Tol: 0.75, TimeTol: 1}, nil
	}
	return nil, fmt.Errorf("unknown verifier %q", mode)
}

type delivery struct {
	flight    kernel.FlightID
	contract  *kernel.Contract
	attempt   int
	arriveAt  float64
	replyAt   float64
	timeoutAt float64
	arrived   bool
	ack       bool
	done      bool
}

type losEpisode struct {
	lastSeen float64
}

// alertWatch follows a pair after a Safety Net alert that the kernel did not
// expect, to measure how close the two aircraft really came.
type alertWatch struct {
	until   float64
	closest float64
}

// instruction is the last Safety Net instruction given to one aircraft.
type instruction struct {
	at       float64
	other    kernel.FlightID // the aircraft the instruction was meant to avoid
	nuisance bool            // the kernel held the pair as separated beyond the look-ahead
}

type conflictEpisode struct {
	minTTC   float64
	lastSeen float64
	intruder bool
}

type world struct {
	cfg  Config
	k    *kernel.Kernel
	net  *safetynet.Net
	env  *planner.Env
	heur *planner.Heuristic
	fb   *planner.Fallback
	cha  *planner.Chaos
	ops  *rand.Rand // delivery delays and losses

	now        float64
	arrivals   []*arrival
	nextArr    int
	waiting    []*arrival // asked for admission and not yet admitted
	spawns     []*aircraft
	ac         map[kernel.FlightID]*aircraft
	order      []kernel.FlightID
	deliveries []*delivery
	failStreak map[kernel.FlightID]int

	los        map[[2]kernel.FlightID]*losEpisode
	alerts     map[[2]kernel.FlightID]float64
	watch      map[[2]kernel.FlightID]*alertWatch
	instructed map[kernel.FlightID]instruction
	conflicts  map[[3]int]*conflictEpisode
	coast      []float64
	rep        Report
	tr         *recorder // nil unless the run is traced for the viewer
}

// Run executes one simulation and returns its report.
func Run(cfg Config) (*Report, error) {
	w, err := newWorld(cfg)
	if err != nil {
		return nil, err
	}
	w.run()
	return &w.rep, nil
}

// RunRecorded executes one simulation and also returns the kernel, so that a
// test can replay the input log.
func RunRecorded(cfg Config) (*Report, *kernel.Kernel, error) {
	cfg.Record = true
	w, err := newWorld(cfg)
	if err != nil {
		return nil, nil, err
	}
	w.run()
	return &w.rep, w.k, nil
}

func netParams(cfg Config) safetynet.Params {
	p := safetynet.DefaultParams()
	if cfg.NetLookAhead > 0 {
		p.LookAhead = cfg.NetLookAhead
	}
	return p
}

func newWorld(cfg Config) (*world, error) {
	d, err := NewVerifier(cfg.Verifier)
	if err != nil {
		return nil, err
	}
	switch cfg.Planner {
	case "heuristic", "chaos", "none":
	default:
		return nil, fmt.Errorf("unknown planner %q", cfg.Planner)
	}
	w := &world{
		cfg:        cfg,
		k:          kernel.New(cfg.Kernel, d),
		net:        safetynet.New(netParams(cfg)),
		ops:        rand.New(rand.NewPCG(cfg.Seed, 0x6f7073)),
		ac:         map[kernel.FlightID]*aircraft{},
		failStreak: map[kernel.FlightID]int{},
		los:        map[[2]kernel.FlightID]*losEpisode{},
		alerts:     map[[2]kernel.FlightID]float64{},
		watch:      map[[2]kernel.FlightID]*alertWatch{},
		instructed: map[kernel.FlightID]instruction{},
		conflicts:  map[[3]int]*conflictEpisode{},
	}
	w.k.Record = cfg.Record
	w.rep.Config = cfg
	w.env = &planner.Env{
		K: w.k, Plans: map[kernel.FlightID]*planner.Plan{}, NoComm: map[kernel.FlightID]bool{},
		Lead: lead, MinLevel: minLevel, MaxLevel: maxLevel, OnCommit: w.onCommit,
	}
	w.heur = &planner.Heuristic{ActAt: 720}
	w.fb = &planner.Fallback{At: 300}
	w.cha = &planner.Chaos{Rate: cfg.ChaosRate, R: rand.New(rand.NewPCG(cfg.Seed, 0x6368616f73))}
	w.generateTraffic()
	return w, nil
}

// generateTraffic draws every flight of the run: Poisson arrivals, a chord
// across the cell, a level by direction of flight, and the per-flight errors
// and faults.
func (w *world) generateTraffic() {
	r := rand.New(rand.NewPCG(w.cfg.Seed, 0x74726166))
	R := w.cfg.Kernel.CellRadius
	K := w.cfg.Kernel.K
	if len(w.cfg.Scripted) > 0 {
		script := append([]ScriptedFlight(nil), w.cfg.Scripted...)
		sort.SliceStable(script, func(i, j int) bool { return script[i].At < script[j].At })
		for i, sf := range script {
			a := &arrival{id: kernel.FlightID(i + 1), firstRequest: sf.At, requestAt: sf.At}
			a.entry = geom.Polar(R, sf.EntryDeg*math.Pi/180)
			a.exit = geom.Polar(R, sf.ExitDeg*math.Pi/180)
			a.dir = a.exit.Sub(a.entry).Unit()
			a.speed, a.level = sf.SpeedKt*geom.Kt, sf.Level
			a.stepFrac, a.stepBy = sf.StepFrac, sf.StepBy
			a.drift = (r.Float64()*2 - 1) * 0.8 * K
			a.latAmp = r.Float64() * 0.3 * w.cfg.Kernel.W
			a.latPhase = r.Float64() * 2 * math.Pi
			a.latOmega = 2 * math.Pi / (120 + 280*r.Float64())
			a.climbFactor = 1.1 + 0.4*r.Float64()
			a.blunderFrac, a.bustFrac = -1, -1
			if sf.BlunderFrac > 0 {
				a.blunderFrac, a.blunderTurn = sf.BlunderFrac, sf.BlunderDeg*math.Pi/180
			}
			if sf.BustFrac > 0 {
				a.bustFrac, a.bustBy = sf.BustFrac, sf.BustBy
			}
			a.nordo = sf.NoRadio
			w.arrivals = append(w.arrivals, a)
		}
		return
	}
	rate := w.cfg.FlightsPerHour / 3600
	id := kernel.FlightID(1)
	for t := r.ExpFloat64() / rate; t < w.cfg.Duration-approachTime; t += r.ExpFloat64() / rate {
		th := r.Float64() * 2 * math.Pi
		a := &arrival{id: id, firstRequest: t, requestAt: t}
		a.entry = geom.Polar(R, th)
		a.exit = geom.Polar(R, th+math.Pi+(r.Float64()*2-1)*math.Pi/3)
		a.dir = a.exit.Sub(a.entry).Unit()
		a.speed = (430 + 60*r.Float64()) * geom.Kt
		if a.dir.X > 0 {
			a.level = 31000 + 2000*float64(r.IntN(5))
		} else {
			a.level = 30000 + 2000*float64(r.IntN(6))
		}
		if r.Float64() < w.cfg.StepFraction {
			a.stepFrac = 0.3 + 0.4*r.Float64()
			a.stepBy = 2000
			if r.Float64() < 0.5 {
				a.stepBy = -2000
			}
		}
		a.drift = (r.Float64()*2 - 1) * 0.8 * K
		a.latAmp = r.Float64() * 0.3 * w.cfg.Kernel.W
		a.latPhase = r.Float64() * 2 * math.Pi
		a.latOmega = 2 * math.Pi / (120 + 280*r.Float64())
		a.climbFactor = 1.1 + 0.4*r.Float64()
		a.blunderFrac, a.bustFrac = -1, -1
		// Fault draws are always made, so that traffic does not depend on
		// the fault rates.
		u1, u2, u3 := r.Float64(), r.Float64(), r.Float64()
		f1, f2, turn, sign := 0.1+0.7*r.Float64(), 0.1+0.7*r.Float64(), (20+20*r.Float64())*math.Pi/180, r.Float64()
		a.nordo = u1 < w.cfg.PNordo
		if u2 < w.cfg.PBlunder {
			a.blunderFrac, a.blunderTurn = f1, turn
			if sign < 0.5 {
				a.blunderTurn = -turn
			}
		}
		if u3 < w.cfg.PLevelBust {
			a.bustFrac, a.bustBy = f2, 1000
			if sign < 0.5 {
				a.bustBy = -1000
			}
		}
		w.arrivals = append(w.arrivals, a)
		id++
	}
}

// entryTrajectory returns the requested trajectory of a, starting at t0 at
// the given level.
func (w *world) entryTrajectory(a *arrival, t0, level float64) (*traj.Trajectory, float64) {
	start := a.entry.Sub(a.dir.Scale(a.speed * approachTime))
	end := a.exit.Add(a.dir.Scale(10))
	legs := traj.Route(t0, []geom.Vec2{start, end}, []float64{a.speed})
	t1 := legs[0].T1
	transit := a.entry.Dist(a.exit) / a.speed
	vs := traj.Level(t0, t1, level)
	if to := level + a.stepBy; a.stepBy != 0 && to >= minLevel && to <= maxLevel {
		vs = traj.Step(t0, t1, level, t0+approachTime+a.stepFrac*transit, to, planner.ClimbRate)
	}
	return &traj.Trajectory{Legs: legs, VSegs: vs}, transit
}

// admit asks the kernel to admit every waiting flight. It offers the filed
// level first, then nearby levels and later times. A flight that the kernel
// does not admit waits outside and asks again: this is the back-pressure that
// a Flow Manager would apply.
func (w *world) admit() {
	for w.nextArr < len(w.arrivals) && w.arrivals[w.nextArr].requestAt <= w.now {
		w.waiting = append(w.waiting, w.arrivals[w.nextArr])
		w.nextArr++
	}
	keep := w.waiting[:0]
	for _, a := range w.waiting {
		if a.requestAt > w.now {
			keep = append(keep, a)
			continue
		}
		admitted := false
	try:
		for _, delay := range []float64{0, 60, 120} {
			for _, dl := range []float64{0, 2000, -2000} {
				level := a.level + dl
				if level < minLevel || level > maxLevel {
					continue
				}
				tr, transit := w.entryTrajectory(a, w.now+delay, level)
				if v := w.k.Admit(a.id, tr); v.OK {
					w.env.Plans[a.id] = &planner.Plan{Exit: tr.PosAt(tr.End()), Speed: a.speed, Level: level}
					w.spawns = append(w.spawns, newAircraft(a, tr, transit))
					w.rep.Admitted++
					w.tr.event(TraceEvent{T: w.now, Kind: "admit", A: int(a.id), Alt: level})
					w.rep.EntryDelaySum += tr.Start() - a.firstRequest
					w.rep.EntryDelayMax = math.Max(w.rep.EntryDelayMax, tr.Start()-a.firstRequest)
					if dl != 0 {
						w.rep.EntryLevelChanged++
					}
					admitted = true
					break try
				}
			}
		}
		if !admitted {
			a.attempts++
			a.requestAt = w.now + 60
			w.rep.EntryDenied++
			keep = append(keep, a)
		}
	}
	w.waiting = keep
}

func (w *world) onCommit(id kernel.FlightID, c *kernel.Contract, _ string) {
	d := &delivery{flight: id, contract: c}
	w.deliveries = append(w.deliveries, d)
	w.send(d)
	w.rep.ClearancesSent++
}

func (w *world) send(d *delivery) {
	d.timeoutAt = w.now + replyTimeout
	d.arrived, d.replyAt = false, math.Inf(1)
	ac := w.ac[d.flight]
	if ac == nil || ac.arr.nordo || w.ops.Float64() < w.cfg.PLostUplink {
		d.arriveAt = math.Inf(1)
		return
	}
	d.arriveAt = w.now + 3 + 17*w.ops.Float64()
}

// receive is the crew's reaction to a clearance. It returns true for an
// acknowledgement and false for a refusal.
func (w *world) receive(d *delivery) bool {
	ac := w.ac[d.flight]
	if ac == nil {
		return false
	}
	c := d.contract
	if ac.flown == c.Traj || ac.pendingRe == c.Traj {
		return true // a repeat of a clearance the crew already holds
	}
	refuse := w.ops.Float64() < w.cfg.PRefuse
	if c.Recontract {
		if refuse || w.now > c.Start()-10 {
			return false
		}
		ac.pendingRe = c.Traj
		return true
	}
	if refuse || ac.mode != modeContract || ac.busting || ac.pendingRe != nil || w.now > c.DivergeAt-10 {
		return false
	}
	// The new trajectory equals the old one up to the divergence point, so
	// the crew can load it at once.
	ac.flown = c.Traj
	ac.amendments++
	return true
}

func (w *world) processDeliveries() {
	live := w.deliveries[:0]
	for _, d := range w.deliveries {
		if !d.arrived && w.now >= d.arriveAt {
			d.arrived = true
			d.ack = w.receive(d)
			if w.ops.Float64() >= w.cfg.PLostReply {
				d.replyAt = w.now + 3 + 17*w.ops.Float64()
			}
		}
		f := w.k.Flight(d.flight)
		current := f != nil && f.Pending == d.contract
		switch {
		case d.arrived && w.now >= d.replyAt:
			d.done = true
			if !current {
				w.rep.RepliesStale++
			} else if d.ack {
				w.k.Ack(d.flight)
				w.rep.ClearancesAcked++
				w.tr.event(TraceEvent{T: w.now, Kind: "ack", A: int(d.flight)})
			} else {
				w.k.Refuse(d.flight)
				w.rep.ClearancesRefused++
				w.tr.event(TraceEvent{T: w.now, Kind: "refuse", A: int(d.flight)})
			}
			w.failStreak[d.flight] = 0
			delete(w.env.NoComm, d.flight)
		case w.now >= d.timeoutAt && d.attempt == 0:
			d.attempt = 1
			w.send(d)
			w.rep.ClearancesRepeated++
		case w.now >= d.timeoutAt:
			d.done = true
			if current {
				w.k.DeliveryFailed(d.flight)
				w.rep.ClearancesFailed++
				w.tr.event(TraceEvent{T: w.now, Kind: "fail", A: int(d.flight)})
			}
			w.failStreak[d.flight]++
			if w.failStreak[d.flight] >= 2 {
				w.env.NoComm[d.flight] = true
			}
		}
		if !d.done {
			live = append(live, d)
		}
	}
	w.deliveries = live
}

func pair(a, b kernel.FlightID) [2]kernel.FlightID {
	if a > b {
		a, b = b, a
	}
	return [2]kernel.FlightID{a, b}
}

// checkSeparation looks for true losses of separation among aircraft inside
// the cell, and classifies each new one by what the kernel claimed.
func (w *world) checkSeparation(in []*aircraft) {
	p := w.cfg.Kernel
	for i, a := range in {
		for _, b := range in[i+1:] {
			if math.Abs(a.alt-b.alt) >= p.SvTrue() {
				continue
			}
			dist := a.pos.Dist(b.pos)
			if dist < 12 && (w.rep.ClosestVerified == 0 || dist < w.rep.ClosestVerified) &&
				w.k.PairState(a.id, b.id) == kernel.PairVerified {
				w.rep.ClosestVerified = dist
			}
			if dist >= p.Sh {
				continue
			}
			key := pair(a.id, b.id)
			ep := w.los[key]
			if ep != nil && w.now-ep.lastSeen <= 30 {
				ep.lastSeen = w.now
				continue
			}
			w.los[key] = &losEpisode{lastSeen: w.now}
			cat := ""
			switch w.k.PairState(a.id, b.id) {
			case kernel.PairVerified:
				if w.k.InsideAny(a.id, a.track(w.now)) && w.k.InsideAny(b.id, b.track(w.now)) {
					cat = "kernel"
					w.rep.LossKernel++
				} else {
					cat = "latency"
					w.rep.LossLatency++
				}
			case kernel.PairExpired:
				cat = "unresolved"
				w.rep.LossUnresolved++
			default:
				cat = "intruder"
				w.rep.LossIntruder++
			}
			// A loss that follows a Safety Net instruction to one of the two
			// aircraft is attributed to the net. If the loss is with the
			// aircraft the instruction was meant to avoid, and the alert was
			// not a nuisance alert, the net failed to prevent it. Otherwise
			// the instruction caused it.
			for i, id := range key {
				ins, ok := w.instructed[id]
				if !ok || w.now-ins.at > 300 {
					continue
				}
				if !ins.nuisance && ins.other == key[1-i] {
					cat += ", not prevented by a net instruction"
					w.rep.LossNetNotPrevented++
				} else {
					cat += ", caused by a net instruction"
					w.rep.LossNetCaused++
				}
				break
			}
			w.tr.event(TraceEvent{T: w.now, Kind: "loss", A: int(a.id), B: int(b.id), Txt: cat, Num: dist, Alt: math.Abs(a.alt - b.alt)})
			if len(w.rep.LossDetail) < 20 {
				w.rep.LossDetail = append(w.rep.LossDetail, fmt.Sprintf(
					"t=%.0f flights %d,%d %s: %.2f nm, %.0f ft", w.now, a.id, b.id, cat, dist, math.Abs(a.alt-b.alt)))
			}
		}
	}
}

func (w *world) safetyNet(in []*aircraft) {
	tracks := make([]safetynet.Track, len(in))
	for i, a := range in {
		tracks[i] = safetynet.Track{
			ID: int(a.id), X: a.pos.X, Y: a.pos.Y, VX: a.vel.X, VY: a.vel.Y,
			Alt: a.alt, VRate: a.vrate, SelAlt: a.selAlt,
		}
	}
	for _, al := range w.net.Step(w.now, tracks) {
		key := pair(kernel.FlightID(al.A), kernel.FlightID(al.B))
		if last, seen := w.alerts[key]; !seen || w.now-last > 30 {
			st := w.k.PairState(key[0], key[1])
			class := "intruder"
			switch {
			case st == kernel.PairVerified && w.k.PairWindow(key[0], key[1]) >= w.now+w.net.P.LookAhead:
				// The kernel holds the pair as separated beyond the net's
				// look-ahead. Either the net or the kernel is wrong.
				class = "verified"
				w.rep.AlertsVerified++
				w.watch[key] = &alertWatch{until: w.now + 240, closest: math.Inf(1)}
			case st == kernel.PairVerified || st == kernel.PairExpired:
				class = "unresolved"
				w.rep.AlertsUnresolved++
			default:
				w.rep.AlertsIntruder++
			}
			w.tr.event(TraceEvent{T: w.now, Kind: "alert", A: int(key[0]), B: int(key[1]), Num: al.TimeToLoss, Txt: class})
		}
		w.tr.alerting(key, al.TimeToLoss)
		w.alerts[key] = w.now
		if al.Instruct && w.cfg.SafetyNet {
			id := kernel.FlightID(al.Flight)
			other := key[0]
			if other == id {
				other = key[1]
			}
			nuisance := w.k.PairState(key[0], key[1]) == kernel.PairVerified &&
				w.k.PairWindow(key[0], key[1]) >= w.now+w.net.P.LookAhead
			if ac := w.ac[id]; ac != nil {
				ac.tactical(al.TargetAlt, w.now)
				w.k.MarkIntruder(id)
				w.rep.Instructions++
				w.instructed[id] = instruction{at: w.now, other: other, nuisance: nuisance}
				txt := ""
				if nuisance {
					txt = "false alert"
				}
				w.tr.event(TraceEvent{T: w.now, Kind: "instruct", A: int(id), B: int(other), Alt: al.TargetAlt, Txt: txt})
			}
		}
	}
}

// followAlerts measures the true closest approach of each watched pair while
// the two aircraft are within the net's vertical threshold.
func (w *world) followAlerts() {
	for key, aw := range w.watch {
		a, b := w.ac[key[0]], w.ac[key[1]]
		if a != nil && b != nil && math.Abs(a.alt-b.alt) < w.net.P.SvAlert {
			aw.closest = math.Min(aw.closest, a.pos.Dist(b.pos))
		}
		if w.now >= aw.until || a == nil || b == nil {
			if aw.closest < w.rep.VerifiedAlertClosest || w.rep.VerifiedAlertClosest == 0 {
				w.rep.VerifiedAlertClosest = aw.closest
			}
			delete(w.watch, key)
		}
	}
}

func (w *world) trackConflicts() {
	for _, c := range w.k.Conflicts() {
		key := [3]int{int(c.A), int(c.B), 0}
		if c.Intruder {
			key[2] = 1
		}
		ep := w.conflicts[key]
		if ep == nil {
			ep = &conflictEpisode{minTTC: math.Inf(1), intruder: c.Intruder}
			w.conflicts[key] = ep
		}
		ep.minTTC = math.Min(ep.minTTC, c.T-w.now)
		ep.lastSeen = w.now
	}
	for key, ep := range w.conflicts {
		if w.now-ep.lastSeen > 30 {
			w.closeConflict(ep)
			delete(w.conflicts, key)
		}
	}
	if c := w.k.Coast(); !math.IsInf(c, 1) {
		w.coast = append(w.coast, c)
	} else {
		w.coast = append(w.coast, w.cfg.Kernel.Horizon)
	}
}

func (w *world) closeConflict(ep *conflictEpisode) {
	if ep.intruder {
		w.rep.IntruderConflicts++
		return
	}
	w.rep.Conflicts++
	if ep.minTTC <= 600 {
		w.rep.ConflictsTo10++
	}
	if ep.minTTC <= 300 {
		w.rep.ConflictsTo5++
	}
	if ep.minTTC <= 120 {
		w.rep.ConflictsTo2++
	}
}

func (w *world) remove(id kernel.FlightID) {
	delete(w.ac, id)
	i := sort.Search(len(w.order), func(i int) bool { return w.order[i] >= id })
	w.order = append(w.order[:i], w.order[i+1:]...)
	w.k.Remove(id)
	delete(w.env.Plans, id)
	delete(w.env.NoComm, id)
	delete(w.failStreak, id)
	delete(w.instructed, id)
}

func (w *world) run() {
	const dt = 1.0
	R := w.cfg.Kernel.CellRadius
	steps := int(w.cfg.Duration / dt)
	var in []*aircraft
	for s := 1; s <= steps; s++ {
		w.now = float64(s) * dt
		w.k.Advance(w.now)

		// Spawn admitted flights at the start of their contracts.
		keep := w.spawns[:0]
		for _, ac := range w.spawns {
			if ac.startT > w.now {
				keep = append(keep, ac)
				continue
			}
			w.ac[ac.id] = ac
			if ac.arr.nordo {
				w.tr.event(TraceEvent{T: w.now, Kind: "fault", A: int(ac.id), Txt: "has no radio"})
			}
			i := sort.Search(len(w.order), func(i int) bool { return w.order[i] >= ac.id })
			w.order = append(w.order, 0)
			copy(w.order[i+1:], w.order[i:])
			w.order[i] = ac.id
		}
		w.spawns = keep

		// Truth.
		in = in[:0]
		var gone []kernel.FlightID
		for _, id := range w.order {
			ac := w.ac[id]
			wasFree, wasBusting := ac.mode == modeFree, ac.busting
			ac.step(w.now, dt)
			if w.tr != nil {
				if !wasFree && ac.mode == modeFree {
					w.tr.event(TraceEvent{T: w.now, Kind: "fault", A: int(id), Txt: "leaves its route"})
				}
				if !wasBusting && ac.busting {
					w.tr.event(TraceEvent{T: w.now, Kind: "fault", A: int(id), Txt: "leaves its level", Alt: ac.bustTarget})
				}
			}
			inside := ac.pos.Norm() <= R
			if inside && !ac.inside && math.IsNaN(ac.enteredAt) {
				ac.enteredAt = w.now
			}
			if !inside && ac.inside {
				w.rep.Completed++
				nominal := ac.arr.entry.Dist(ac.arr.exit) / ac.arr.speed
				w.rep.TransitDelaySum += (w.now - ac.enteredAt) - nominal
				w.rep.AmendmentsFlown += ac.amendments
			}
			ac.inside = inside
			if inside {
				in = append(in, ac)
				w.rep.FlightSeconds += dt
			}
			if ac.done(R) {
				gone = append(gone, id)
			}
		}
		for _, id := range gone {
			w.remove(id)
		}
		if len(in) > w.rep.PeakInCell {
			w.rep.PeakInCell = len(in)
		}
		w.checkSeparation(in)
		w.followAlerts()
		w.processDeliveries()

		if s%5 == 0 {
			for _, id := range w.order {
				w.k.UpdateTrack(id, w.ac[id].track(w.now))
			}
			w.safetyNet(in)
		}
		if s%60 == 0 {
			for _, re := range w.k.ReanchorAll() {
				// Re-anchoring relabels time. The aircraft does not move.
				if ac := w.ac[re.ID]; ac != nil && ac.flown == re.Old {
					ac.flown = re.New
					ac.tau -= re.Dt
				} else if ac != nil && ac.mode == modeContract && !ac.busting {
					w.rep.SimInconsistencies++
				}
			}
		}
		if s%10 == 0 {
			w.admit()
			w.k.Tick()
			w.trackConflicts()
			w.env.Begin()
			switch w.cfg.Planner {
			case "heuristic":
				w.heur.Act(w.env)
			case "chaos":
				w.cha.Act(w.env)
			}
			if w.cfg.Fallback {
				w.fb.Act(w.env)
			}
			planner.Recontract(w.env)
		}
		if s%5 == 0 {
			// After every action of this second, so that a frame shows the
			// state that its counters describe.
			w.tr.frame(w)
		}
	}
	for _, ep := range w.conflicts {
		w.closeConflict(ep)
	}
	w.finish()
}

func (w *world) finish() {
	r := &w.rep
	r.Kernel = w.k.Stats
	r.Marginal, r.Hard = w.k.D.Marginal, w.k.D.Hard
	r.Sources = map[string]planner.Counter{}
	for name, c := range w.env.Stats {
		r.Sources[name] = *c
	}
	sort.Float64s(w.coast)
	if n := len(w.coast); n > 0 {
		r.CoastMin = w.coast[0]
		r.CoastP1 = w.coast[n/100]
		r.CoastMedian = w.coast[n/2]
	}
	r.Digest = w.k.Digest()
}
