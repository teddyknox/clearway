package conformance

import (
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/traj"
)

// Each group of vectors aims at one way in which a Verifier can be wrong.
// Two of them come from defects that were found in this project's own
// analytic implementation.
var groups = []struct {
	name, purpose string
}{
	{"basic", "Cases with an answer that can be worked out by hand."},
	{"random", "Pairs of unrelated trajectories. Most are clear."},
	{"dense", "Pairs that start close together, with turns and level changes."},
	{"collision", "Two aircraft aimed at one point at one time, or within a few hundredths of a mile. Closed-form roots lose precision here."},
	{"parallel", "Equal velocities. The relative speed is zero, and a conflict can arise only from the growth of the envelopes."},
	{"vertical-timing", "One aircraft leaves a level as another enters it, within seconds. The answer depends on the early and late allowance."},
	{"restart", "A check that starts at, just before, or just after the exact start of a conflict."},
	{"intruder", "One tube built from a track: one straight leg, a fast-growing radius, a vertical uncertainty that may grow."},
	{"tightened", "A standard with a wider horizontal distance and a vertical slack, as a re-check after a disagreement uses."},
}

// Generate builds the suite. The same seed gives the same suite on one
// machine. It returns an error if the exact check and the enumeration
// disagree on any vector.
func Generate(seed uint64) (*Suite, error) {
	g := &gen{r: rand.New(rand.NewPCG(seed, 0x636f6e66)), count: map[string]int{}}
	g.basic()
	for i := 0; i < 40; i++ {
		g.pair("random", "", g.tube(150), g.tube(150), stdSep)
	}
	for i := 0; i < 200; i++ {
		g.pair("dense", "", g.tube(5), g.tube(5), stdSep)
	}
	for i := 0; i < 80; i++ {
		g.collision()
	}
	for i := 0; i < 30; i++ {
		g.parallel()
	}
	for i := 0; i < 80; i++ {
		g.verticalTiming("vertical-timing", stdSep)
	}
	for i := 0; i < 30; i++ {
		g.intruder()
	}
	for i := 0; i < 20; i++ {
		g.intruderGrowth()
	}
	tight := Sep{Sh: stdSep.Sh + Tol, SvTrue: stdSep.SvTrue, VertSlack: TimeTol}
	for i := 0; i < 20; i++ {
		g.pair("tightened", "", g.tube(5), g.tube(5), tight)
	}
	for i := 0; i < 30; i++ {
		g.verticalTiming("tightened", tight)
	}
	g.restart(45)
	if g.err != nil {
		return nil, g.err
	}
	return &Suite{
		Name: "clearway-verifier", Spec: SpecVersion,
		Units:     "nautical miles, feet, seconds",
		Tolerance: Tolerance{Distance: Tol, Time: TimeTol, GuardDistance: GuardD, GuardTime: GuardT},
		Cases:     g.cases,
	}, nil
}

// Purpose returns what a group of vectors is for.
func Purpose(group string) string {
	for _, g := range groups {
		if g.name == group {
			return g.purpose
		}
	}
	return ""
}

// GroupNames returns the groups in their fixed order.
func GroupNames() []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.name
	}
	return out
}

var stdSep = Sep{Sh: 5, SvTrue: 600}

const (
	climb = 1000.0 / 60 // ft/s
	kt    = geom.Kt
)

type gen struct {
	r     *rand.Rand
	count map[string]int
	cases []Case
	err   error
}

func (g *gen) add(group, note string, a, b *kernel.Tube, from, to float64, sep Sep) *Case {
	g.count[group]++
	c := Case{
		ID: fmt.Sprintf("%s-%03d", group, g.count[group]), Group: group, Note: note,
		A: FromKernel(a), B: FromKernel(b), From: from, To: to, Sep: sep,
	}
	e, err := Reference(&c, 0.02)
	if err != nil && g.err == nil {
		g.err = err
	}
	c.Expect = e
	g.cases = append(g.cases, c)
	return &g.cases[len(g.cases)-1]
}

func (g *gen) pair(group, note string, a, b *kernel.Tube, sep Sep) *Case {
	from := 120 + g.r.Float64()*60
	return g.add(group, note, a, b, from, from+60+g.r.Float64()*1200, sep)
}

func mk(legs []traj.Leg, vs []traj.VSeg, anchor, r0, k float64) *kernel.Tube {
	tr := &traj.Trajectory{Legs: legs, VSegs: vs}
	if err := tr.Validate(); err != nil {
		panic(err)
	}
	return &kernel.Tube{Traj: tr, Anchor: anchor, R0: r0, K: k, VU0: 200, VMin: tr.MinSpeed()}
}

// straight is one leg from a to b at the given speed, level at alt.
func straight(t0 float64, a, b geom.Vec2, speed, alt, anchor, k float64) *kernel.Tube {
	legs := traj.Route(t0, []geom.Vec2{a, b}, []float64{speed})
	return mk(legs, traj.Level(t0, legs[0].T1, alt), anchor, 1.2, k)
}

// tube is a random trajectory of one to three legs with level changes.
func (g *gen) tube(spread float64) *kernel.Tube {
	r := g.r
	t0 := 90 + r.Float64()*30
	pts := []geom.Vec2{{X: (r.Float64()*2 - 1) * spread, Y: (r.Float64()*2 - 1) * spread}}
	var speeds []float64
	for i, n := 0, 1+r.IntN(3); i < n; i++ {
		pts = append(pts, pts[len(pts)-1].Add(geom.Polar(20+r.Float64()*80, r.Float64()*2*math.Pi)))
		speeds = append(speeds, (400+r.Float64()*120)*kt)
	}
	legs := traj.Route(t0, pts, speeds)
	end := legs[len(legs)-1].T1
	levels := []float64{35000, 36000, 37000}
	alt := levels[r.IntN(len(levels))]
	var vs []traj.VSeg
	for t := t0; t < end; {
		tl := math.Min(end, t+60+r.Float64()*300)
		if n := len(vs); n > 0 && vs[n-1].Level() && vs[n-1].A0 == alt {
			vs[n-1].T1 = tl
		} else {
			vs = append(vs, traj.VSeg{T0: t, T1: tl, A0: alt, A1: alt})
		}
		t = tl
		if next := levels[r.IntN(len(levels))]; t < end && next != alt {
			tt := math.Min(end, t+math.Abs(next-alt)/climb)
			vs = append(vs, traj.VSeg{T0: t, T1: tt, A0: alt, A1: next})
			alt, t = next, tt
		}
	}
	return mk(legs, vs, -r.Float64()*300, 0.8+r.Float64(), r.Float64()*12*kt)
}

func (g *gen) basic() {
	const sp, k = 480 * kt, 10 * kt
	w, e := geom.Vec2{X: -80}, geom.Vec2{X: 80}
	a := straight(0, w, e, sp, 35000, 0, k)
	g.add("basic", "Head-on at one level.", a, straight(0, e, w, sp, 35000, 0, k), 0, 1200, stdSep)
	g.add("basic", "The same paths, 1,000 ft apart: separated by level.", a, straight(0, e, w, sp, 36000, 0, k), 0, 1200, stdSep)
	g.add("basic", "The same paths, 500 ft apart: not separated by level.", a, straight(0, e, w, sp, 35500, 0, k), 0, 1200, stdSep)
	g.add("basic", "Parallel tracks 30 nm apart at one level.", a,
		straight(0, geom.Vec2{X: -80, Y: 30}, geom.Vec2{X: 80, Y: 30}, sp, 35000, 0, k), 0, 1200, stdSep)
	g.add("basic", "Parallel tracks 9 nm apart. The envelopes grow until 9 = 5 + 2(1.2 + k t), at t = 288 s.",
		straight(0, geom.Vec2{X: -100}, geom.Vec2{X: 100}, sp, 35000, 0, k),
		straight(0, geom.Vec2{X: -100, Y: 9}, geom.Vec2{X: 100, Y: 9}, sp, 35000, 0, k), 0, 1500, stdSep)
	g.add("basic", "The same pair, checked for 120 s only: still clear.",
		straight(0, geom.Vec2{X: -100}, geom.Vec2{X: 100}, sp, 35000, 0, k),
		straight(0, geom.Vec2{X: -100, Y: 9}, geom.Vec2{X: 100, Y: 9}, sp, 35000, 0, k), 0, 120, stdSep)
	climber := straight(0, e, w, sp, 34000, 0, k)
	climber.Traj.VSegs = traj.Step(0, climber.End(), 34000, 500, 36000, climb)
	g.add("basic", "Head-on, with one aircraft climbing through the level of the other.", a, climber, 0, 1200, stdSep)
	g.add("basic", "The interval is empty: from is after to.", a, straight(0, e, w, sp, 35000, 0, k), 900, 800, stdSep)
	g.add("basic", "The second trajectory starts after the interval ends.", a, straight(1250, e, w, sp, 35000, 0, k), 0, 1200, stdSep)
	g.add("basic", "The interval ends before the head-on pair is close.", a, straight(0, e, w, sp, 35000, 0, k), 0, 200, stdSep)
	g.add("basic", "The interval starts after the head-on pair has passed and is separated again.", a, straight(0, e, w, sp, 35000, 0, k), 700, 1200, stdSep)
	g.add("basic", "Crossing at right angles, 20 nm apart in time at the crossing point.", a,
		straight(-150, geom.Vec2{Y: -80}, geom.Vec2{Y: 80}, sp, 35000, -150, k), 0, 1200, stdSep)
	g.add("basic", "No growth: two aircraft pass 7.5 nm abeam, with 5 + 1.2 + 1.2 required.", straight(0, w, e, sp, 35000, 0, 0),
		straight(0, geom.Vec2{X: 80, Y: 7.5}, geom.Vec2{X: -80, Y: 7.5}, sp, 35000, 0, 0), 0, 1200, stdSep)
}

func (g *gen) collision() {
	r := g.r
	meet := geom.Vec2{X: (r.Float64()*2 - 1) * 40, Y: (r.Float64()*2 - 1) * 40}
	tm := 300 + r.Float64()*800
	k := []float64{0, 0.5, 3, 10, 15}[r.IntN(5)] * kt
	miss := []float64{0, 1e-9, 1e-6, 1e-3, 0.03}[r.IntN(5)]
	one := func(off geom.Vec2) *kernel.Tube {
		sp := (400 + 120*r.Float64()) * kt
		dir := geom.Polar(1, r.Float64()*2*math.Pi)
		at := meet.Add(off)
		return straight(0, at.Sub(dir.Scale(sp*tm)), at.Add(dir.Scale(sp*600)), sp, 35000, 0, k)
	}
	a, b := one(geom.Vec2{}), one(geom.Polar(miss, r.Float64()*2*math.Pi))
	g.add("collision", fmt.Sprintf("Miss distance %g nm, growth %g kt.", miss, k/kt), a, b, 0, tm+300, stdSep)
}

func (g *gen) parallel() {
	r := g.r
	sp := (420 + 80*r.Float64()) * kt
	dir := geom.Polar(1, r.Float64()*2*math.Pi)
	gap := 6 + 12*r.Float64()
	k := (1 + 11*r.Float64()) * kt
	from := geom.Vec2{X: (r.Float64()*2 - 1) * 30, Y: (r.Float64()*2 - 1) * 30}
	side := dir.Perp().Scale(gap)
	a := straight(0, from, from.Add(dir.Scale(sp*1500)), sp, 35000, 0, k)
	b := straight(0, from.Add(side), from.Add(side).Add(dir.Scale(sp*1500)), sp, 35000, 0, k)
	g.add("parallel", fmt.Sprintf("Abeam by %.2f nm at equal velocity.", gap), a, b, 0, 300+1100*r.Float64(), stdSep)
}

// verticalTiming puts two aircraft on nearly the same path. One descends out
// of a level and the other descends into it. Without the early and late
// allowance the two bands never overlap. With it, they overlap or not by a
// margin of a few seconds.
func (g *gen) verticalTiming(group string, sep Sep) {
	r := g.r
	sp := (430 + 60*r.Float64()) * kt
	dir := geom.Polar(1, r.Float64()*2*math.Pi)
	p := geom.Vec2{X: (r.Float64()*2 - 1) * 20, Y: (r.Float64()*2 - 1) * 20}
	off := dir.Perp().Scale(r.Float64() * 3)
	k := (2 + 10*r.Float64()) * kt
	leave := 300 + 200*r.Float64()
	a := straight(0, p, p.Add(dir.Scale(sp*1400)), sp, 36000, 0, k)
	a.Traj.VSegs = traj.Step(0, a.End(), 36000, leave, 34000, climb)
	b := straight(0, p.Add(off), p.Add(off).Add(dir.Scale(sp*1400)), sp, 38000, 0, k)
	// a holds the band down from 36,000 ft until its transition ends, plus
	// its allowance. b takes the band down to 36,000 ft from its transition
	// start, less its allowance.
	vacated := leave + 2000/climb
	nominal := vacated + a.Sigma(vacated) + b.Sigma(vacated)
	enter := nominal + (r.Float64()*2-1)*4
	b.Traj.VSegs = traj.Step(0, b.End(), 38000, enter, 36000, climb)
	g.add(group, fmt.Sprintf("Enters %.2f s after the level is nominally free.", enter-nominal), a, b, 0, 1300, sep)
}

func (g *gen) intruder() {
	r := g.r
	c := g.tube(5)
	sp := (380 + 140*r.Float64()) * kt
	pos := geom.Vec2{X: (r.Float64()*2 - 1) * 10, Y: (r.Float64()*2 - 1) * 10}
	vel := geom.Polar(sp, r.Float64()*2*math.Pi)
	t0 := 120 + 40*r.Float64()
	alt := 35000 + 2000*r.Float64()
	to := alt
	if r.Float64() < 0.5 {
		to = alt + (r.Float64()*2-1)*2000
	}
	tr := &traj.Trajectory{
		Legs:  []traj.Leg{{T0: t0, T1: t0 + 300, P0: pos, V: vel}},
		VSegs: []traj.VSeg{{T0: t0, T1: t0 + 300, A0: alt, A1: to}},
	}
	it := &kernel.Tube{Traj: tr, Anchor: t0, R0: 1.5, K: 2.0 / 60, VU0: 300, VMin: sp, Intruder: true}
	if r.Float64() < 0.5 {
		it.VURate = 5 * r.Float64()
	}
	g.add("intruder", "", c, it, t0, t0+300, stdSep)
}

// intruderGrowth puts an intruder right beside a level aircraft, far enough
// above or below to be separated at first. Its vertical uncertainty grows
// until the two are no longer separated. A Verifier that ignores the growth
// approves the pair.
func (g *gen) intruderGrowth() {
	r := g.r
	sp := (430 + 60*r.Float64()) * kt
	dir := geom.Polar(1, r.Float64()*2*math.Pi)
	p := geom.Vec2{X: (r.Float64()*2 - 1) * 20, Y: (r.Float64()*2 - 1) * 20}
	t0 := 100 + 50*r.Float64()
	c := straight(0, p.Sub(dir.Scale(sp*t0)), p.Add(dir.Scale(sp*600)), sp, 35000, 0, (2+8*r.Float64())*kt)
	gap := 1400 + 1000*r.Float64()
	if r.Float64() < 0.5 {
		gap = -gap
	}
	rate := 1 + 4*r.Float64()
	off := dir.Perp().Scale(r.Float64() * 3)
	tr := &traj.Trajectory{
		Legs:  []traj.Leg{{T0: t0, T1: t0 + 300, P0: p.Add(off), V: dir.Scale(sp)}},
		VSegs: []traj.VSeg{{T0: t0, T1: t0 + 300, A0: 35000 + gap, A1: 35000 + gap}},
	}
	it := &kernel.Tube{Traj: tr, Anchor: t0, R0: 1.5, K: 2.0 / 60, VU0: 300, VURate: rate, VMin: sp, Intruder: true}
	// Separated until |gap| - 200 - 300 - rate·τ falls below 600.
	g.add("intruder", fmt.Sprintf("Level, %.0f ft away, with vertical uncertainty growing at %.1f ft/s: separation ends after %.0f s.",
		math.Abs(gap), rate, (math.Abs(gap)-1100)/rate), c, it, t0, t0+300, stdSep)
}

// restart builds pairs whose conflict starts in the middle of the interval,
// and starts the check at the exact first conflict time, or a little to
// either side of it. An earlier version of this project's analytic
// implementation reported the end of the conflict when a check started exactly
// at its beginning.
func (g *gen) restart(n int) {
	var ac kernel.AnalyticChecker
	r := g.r
	offsets := []float64{0, 1e-9, -1e-9, 1e-3, -1e-3, 0.5, -0.5, 5, -5}
	for made := 0; made < n; {
		meet := geom.Vec2{X: (r.Float64()*2 - 1) * 40, Y: (r.Float64()*2 - 1) * 40}
		tm := 400 + r.Float64()*600
		k := r.Float64() * 12 * kt
		one := func(off geom.Vec2) *kernel.Tube {
			sp := (400 + 120*r.Float64()) * kt
			dir := geom.Polar(1, r.Float64()*2*math.Pi)
			at := meet.Add(off)
			return straight(0, at.Sub(dir.Scale(sp*tm)), at.Add(dir.Scale(sp*600)), sp, 35000, 0, k)
		}
		a, b := one(geom.Vec2{}), one(geom.Polar(r.Float64()*6, r.Float64()*2*math.Pi))
		to := tm + 300
		first := ac.FirstConflict(a, b, 0, to, stdSep.params())
		if !first.Conflict || first.T < 20 {
			continue
		}
		off := offsets[made%len(offsets)]
		g.add("restart", fmt.Sprintf("Starts %+g s from the exact start of the conflict.", off), a, b, first.T+off, to, stdSep)
		made++
	}
}
