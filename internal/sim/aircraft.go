package sim

import (
	"math"

	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/traj"
)

const (
	modeContract = iota // the crew flies the trajectory in flown
	modeFree            // the crew flies straight: a blunder, or a Safety Net instruction
)

// arrival is one flight before it is admitted, with the error and fault draws
// that belong to it. All draws are made when traffic is generated, so that two
// runs with one seed see the same traffic whatever the automation does.
type arrival struct {
	id           kernel.FlightID
	firstRequest float64
	requestAt    float64
	attempts     int

	entry, exit, dir geom.Vec2
	speed, level     float64
	stepFrac, stepBy float64 // planned level change; stepBy == 0 if none

	drift       float64 // along-track drift (nm/s)
	latAmp      float64
	latPhase    float64
	latOmega    float64
	climbFactor float64

	nordo       bool
	blunderFrac float64 // fraction of the transit at which the fault starts; <0 if none
	blunderTurn float64
	bustFrac    float64
	bustBy      float64
}

// aircraft is the true state of one flight.
type aircraft struct {
	id  kernel.FlightID
	arr *arrival

	flown     *traj.Trajectory
	tau       float64 // progress along flown, in trajectory time
	mode      int
	pendingRe *traj.Trajectory // a re-contract received and not yet started

	pos, vel           geom.Vec2
	alt, vrate, selAlt float64
	blend              geom.Vec2

	freeTarget     float64
	freeDelayUntil float64
	busting        bool
	bustTarget     float64
	blunderAt      float64
	bustAt         float64

	startT     float64
	inside     bool
	enteredAt  float64
	amendments int
}

func newAircraft(a *arrival, tr *traj.Trajectory, transit float64) *aircraft {
	t0 := tr.Start()
	ac := &aircraft{
		id: a.id, arr: a, flown: tr, tau: t0, startT: t0,
		pos: tr.PosAt(t0), vel: tr.VelAt(t0), alt: tr.AltAt(t0),
		enteredAt: math.NaN(), blunderAt: math.Inf(1), bustAt: math.Inf(1),
	}
	ac.selAlt = ac.alt
	// The approach takes 900 s. Faults start inside the cell.
	if a.blunderFrac >= 0 {
		ac.blunderAt = t0 + 900 + a.blunderFrac*transit
	}
	if a.bustFrac >= 0 {
		ac.bustAt = t0 + 900 + a.bustFrac*transit
	}
	return ac
}

// step advances the true state to time now.
func (ac *aircraft) step(now, dt float64) {
	a := ac.arr
	if ac.pendingRe != nil && now >= ac.pendingRe.Start() {
		ac.flown, ac.pendingRe = ac.pendingRe, nil
		ac.tau = now - dt
		ac.blend = ac.pos.Sub(ac.flown.PosAt(ac.tau))
		ac.mode, ac.busting = modeContract, false
	}
	if ac.mode == modeContract && !ac.busting && ac.pendingRe == nil {
		switch {
		case now >= ac.blunderAt:
			ac.blunderAt = math.Inf(1)
			ac.mode = modeFree
			ac.vel = ac.vel.Rotate(a.blunderTurn)
			ac.freeTarget, ac.freeDelayUntil = ac.selAlt, 0
		case now >= ac.bustAt:
			ac.bustAt = math.Inf(1)
			if seg := ac.flown.VSegAt(ac.tau); seg.Level() {
				ac.busting, ac.bustTarget = true, seg.A0+a.bustBy
			}
		}
	}

	prevPos, prevAlt := ac.pos, ac.alt
	var target, rate float64
	switch ac.mode {
	case modeContract:
		sp := ac.flown.LegAt(ac.tau).Speed()
		ac.tau += dt * (1 + a.drift/sp)
		dir := ac.flown.VelAt(ac.tau).Unit()
		lat := a.latAmp * math.Sin(a.latPhase+a.latOmega*now)
		ac.blend = ac.blend.Scale(math.Exp(-dt / 30))
		ac.pos = ac.flown.PosAt(ac.tau).Add(dir.Perp().Scale(lat)).Add(ac.blend)
		ac.vel = ac.pos.Sub(prevPos).Scale(1 / dt)
		seg := ac.flown.VSegAt(ac.tau)
		target, rate = seg.A1, 25
		if !seg.Level() {
			rate = math.Abs(seg.A1-seg.A0) / (seg.T1 - seg.T0) * a.climbFactor
		}
		if ac.busting {
			target, rate = ac.bustTarget, 30
		}
	case modeFree:
		ac.pos = ac.pos.Add(ac.vel.Scale(dt))
		target = ac.freeTarget
		if now >= ac.freeDelayUntil {
			rate = 40
		}
	}
	if d := target - ac.alt; math.Abs(d) <= rate*dt {
		ac.alt = target
	} else if d > 0 {
		ac.alt += rate * dt
	} else if rate > 0 {
		ac.alt -= rate * dt
	}
	ac.vrate = (ac.alt - prevAlt) / dt
	ac.selAlt = target
}

// tactical applies a Safety Net instruction: fly the present heading and go
// to the given altitude.
func (ac *aircraft) tactical(target, now float64) {
	if ac.mode != modeFree {
		ac.freeDelayUntil = now + 5
	}
	ac.mode, ac.busting, ac.pendingRe = modeFree, false, nil
	ac.freeTarget = target
}

func (ac *aircraft) track(now float64) kernel.Track {
	return kernel.Track{T: now, Pos: ac.pos, Vel: ac.vel, Alt: ac.alt, VRate: ac.vrate, SelAlt: ac.selAlt}
}

// done reports whether the flight has reached the end of its contract or has
// left the area.
func (ac *aircraft) done(radius float64) bool {
	if ac.mode == modeContract && ac.pendingRe == nil {
		return ac.tau >= ac.flown.End()-1
	}
	return ac.pos.Norm() > radius+40
}
