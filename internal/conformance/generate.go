package conformance

import (
	"fmt"
	"math"
	"math/rand/v2"

	"github.com/teddyknox/clearway/internal/conv"
	"github.com/teddyknox/clearway/internal/ftraj"
	"github.com/teddyknox/clearway/internal/fx"
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
	{"slack", "One aircraft enters or leaves the level of another, to the microsecond. The answer depends on what a slack covers, with and without a radius that grows almost as fast as the aircraft flies."},
}

// Generate builds the suite. The same seed gives the same suite on one
// machine. It returns an error if the exact implementation and the
// enumeration disagree on any vector.
//
// The generator builds each geometry in nautical miles, feet, and seconds,
// and converts it to the integers of a vector with package conv.
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
	g.slack()
	if g.err != nil {
		return nil, g.err
	}
	return &Suite{
		Name: "clearway-verifier", Spec: SpecVersion, Units: Units,
		Tolerance: Tolerance{Distance: Tol, Time: TimeTol},
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

var stdSep = Sep{Sh: 5 * fx.NM, SvTrue: 600 * fx.FT}

const (
	climb = 1000.0 / 60 // ft/s
	kt    = geom.Kt     // nm/s
)

// sec converts seconds to the microseconds of a vector.
func sec(s float64) int64 { return conv.Time(s) }

type gen struct {
	r     *rand.Rand
	count map[string]int
	cases []Case
	err   error
}

func (g *gen) fail(err error) {
	if g.err == nil {
		g.err = err
	}
}

func (g *gen) add(group, note string, a, b *kernel.Tube, from, to int64, sep Sep) *Case {
	g.count[group]++
	c := Case{
		ID: fmt.Sprintf("%s-%03d", group, g.count[group]), Group: group, Note: note,
		A: FromKernel(a), B: FromKernel(b), From: from, To: to, Sep: sep,
	}
	e, err := Reference(&c)
	if err != nil {
		g.fail(err)
	}
	c.Expect = e
	g.cases = append(g.cases, c)
	return &g.cases[len(g.cases)-1]
}

func (g *gen) pair(group, note string, a, b *kernel.Tube, sep Sep) *Case {
	from := 120 + g.r.Float64()*60
	return g.add(group, note, a, b, sec(from), sec(from+60+g.r.Float64()*1200), sep)
}

// mk converts a trajectory (nm, ft, s) to a tube. anchor is in seconds, r0 in
// nautical miles, k in nautical miles per second, vu0 in feet. The lowest
// speed of the tube is that of its slowest leg. mk panics if the tube is not
// valid: that is a defect of the generator.
func mk(legs []ftraj.Leg, vs []ftraj.VSeg, anchor, r0, k, vu0 float64) *kernel.Tube {
	tr := conv.Traj(&ftraj.Trajectory{Legs: legs, VSegs: vs})
	tb := &kernel.Tube{Traj: tr, Anchor: sec(anchor), R0: conv.Len(r0), K: conv.Speed(k), VU0: conv.Alt(vu0)}
	if err := tb.Validate(); err != nil {
		panic(err)
	}
	// The speed of a valid leg is inside the limit of a lowest speed.
	tb.VMin = tr.MinSpeed()
	return tb
}

// path is one leg from a to b at the given speed, with the vertical profile
// that prof returns for the span of the leg. It is the tube of a contract:
// a radius of 1.2 nm at the anchor and a vertical uncertainty of 200 ft.
func path(t0 float64, a, b geom.Vec2, speed float64, prof func(t0, t1 float64) []ftraj.VSeg, anchor, k float64) *kernel.Tube {
	legs := ftraj.Route(t0, []geom.Vec2{a, b}, []float64{speed})
	return mk(legs, prof(t0, legs[0].T1), anchor, 1.2, k, 200)
}

// straight is path, level at alt.
func straight(t0 float64, a, b geom.Vec2, speed, alt, anchor, k float64) *kernel.Tube {
	return path(t0, a, b, speed, func(t0, t1 float64) []ftraj.VSeg { return ftraj.Level(t0, t1, alt) }, anchor, k)
}

// step returns the profile of path for a change of level from a0 to a1 that
// starts at ts.
func step(a0, ts, a1 float64) func(t0, t1 float64) []ftraj.VSeg {
	return func(t0, t1 float64) []ftraj.VSeg { return ftraj.Step(t0, t1, a0, ts, a1, climb) }
}

// track is the tube of an intruder, as the kernel builds one from a track:
// one straight leg of 300 s from t0, one vertical segment from a0 to a1, a
// radius of 1.5 nm that grows at 2 nm a minute, and a vertical uncertainty of
// 300 ft.
func track(t0 float64, pos, vel geom.Vec2, a0, a1 float64) *kernel.Tube {
	tb := mk([]ftraj.Leg{{T0: t0, T1: t0 + 300, P0: pos, V: vel}},
		[]ftraj.VSeg{{T0: t0, T1: t0 + 300, A0: a0, A1: a1}}, t0, 1.5, 2.0/60, 300)
	tb.Intruder = true
	return tb
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
	legs := ftraj.Route(t0, pts, speeds)
	end := legs[len(legs)-1].T1
	levels := []float64{35000, 36000, 37000}
	alt := levels[r.IntN(len(levels))]
	var vs []ftraj.VSeg
	for t := t0; t < end; {
		tl := math.Min(end, t+60+r.Float64()*300)
		if n := len(vs); n > 0 && vs[n-1].Level() && vs[n-1].A0 == alt {
			vs[n-1].T1 = tl
		} else {
			vs = append(vs, ftraj.VSeg{T0: t, T1: tl, A0: alt, A1: alt})
		}
		t = tl
		if next := levels[r.IntN(len(levels))]; t < end && next != alt {
			tt := math.Min(end, t+math.Abs(next-alt)/climb)
			vs = append(vs, ftraj.VSeg{T0: t, T1: tt, A0: alt, A1: next})
			alt, t = next, tt
		}
	}
	return mk(legs, vs, -r.Float64()*300, 0.8+r.Float64(), r.Float64()*12*kt, 200)
}

func (g *gen) basic() {
	const sp, k = 480 * kt, 10 * kt
	add := func(note string, a, b *kernel.Tube, from, to float64) *Case {
		return g.add("basic", note, a, b, sec(from), sec(to), stdSep)
	}
	w, e := geom.Vec2{X: -80}, geom.Vec2{X: 80}
	a := straight(0, w, e, sp, 35000, 0, k)
	add("Head-on at one level.", a, straight(0, e, w, sp, 35000, 0, k), 0, 1200)
	add("The same paths, 1,000 ft apart: separated by level.", a, straight(0, e, w, sp, 36000, 0, k), 0, 1200)
	add("The same paths, 500 ft apart: not separated by level.", a, straight(0, e, w, sp, 35500, 0, k), 0, 1200)
	add("Parallel tracks 30 nm apart at one level.", a,
		straight(0, geom.Vec2{X: -80, Y: 30}, geom.Vec2{X: 80, Y: 30}, sp, 35000, 0, k), 0, 1200)

	// The pair is 9 nm apart at every time, and it needs 5 + 2·(1.2 + k·t) nm.
	// So the first conflict is the first whole microsecond with 2·k·t above
	// 1.6 nm. The note quotes that time, and the exact time must equal it.
	growth := conv.Speed(k)
	first := (16*fx.NM/10)*fx.M/(2*growth) + 1
	c := add(fmt.Sprintf("Parallel tracks 9 nm apart. The envelopes grow until 9 = 5 + 2(1.2 + k t). With k = 10 kt that is t = 288 s. "+
		"The vector has k = %d µm/s, which is a little less, and the first conflict is at %d µs.", growth, first),
		straight(0, geom.Vec2{X: -100}, geom.Vec2{X: 100}, sp, 35000, 0, k),
		straight(0, geom.Vec2{X: -100, Y: 9}, geom.Vec2{X: 100, Y: 9}, sp, 35000, 0, k), 0, 1500)
	if c.Expect.ExactFirst == nil || *c.Expect.ExactFirst != first {
		g.fail(fmt.Errorf("%s: the note says that the first conflict is at %d µs, and the exact implementation says otherwise", c.ID, first))
	}
	add("The same pair, checked for 120 s only: still clear.",
		straight(0, geom.Vec2{X: -100}, geom.Vec2{X: 100}, sp, 35000, 0, k),
		straight(0, geom.Vec2{X: -100, Y: 9}, geom.Vec2{X: 100, Y: 9}, sp, 35000, 0, k), 0, 120)
	add("Head-on, with one aircraft climbing through the level of the other.", a,
		path(0, e, w, sp, step(34000, 500, 36000), 0, k), 0, 1200)
	add("The interval is empty: from is after to.", a, straight(0, e, w, sp, 35000, 0, k), 900, 800)
	add("The second trajectory starts after the interval ends.", a, straight(1250, e, w, sp, 35000, 0, k), 0, 1200)
	add("The interval ends before the head-on pair is close.", a, straight(0, e, w, sp, 35000, 0, k), 0, 200)
	add("The interval starts after the head-on pair has passed and is separated again.", a, straight(0, e, w, sp, 35000, 0, k), 700, 1200)
	add("Crossing at right angles, 20 nm apart in time at the crossing point.", a,
		straight(-150, geom.Vec2{Y: -80}, geom.Vec2{Y: 80}, sp, 35000, -150, k), 0, 1200)
	add("No growth: two aircraft pass 7.5 nm abeam, with 5 + 1.2 + 1.2 required.", straight(0, w, e, sp, 35000, 0, 0),
		straight(0, geom.Vec2{X: 80, Y: 7.5}, geom.Vec2{X: -80, Y: 7.5}, sp, 35000, 0, 0), 0, 1200)
}

// collision aims two aircraft at one point at one time. The aim is exact: at
// the meeting time, a whole second, the first aircraft is on the meeting
// point to the picometre, and the second is at the stated offset from it.
// The offset is from zero to a few hundredths of a mile.
func (g *gen) collision() {
	r := g.r
	meet := conv.Point(geom.Vec2{X: (r.Float64()*2 - 1) * 40, Y: (r.Float64()*2 - 1) * 40})
	tm := math.Round(300 + r.Float64()*800)
	k := []float64{0, 0.5, 3, 10, 15}[r.IntN(5)] * kt
	miss := []float64{0, 1e-9, 1e-6, 1e-3, 0.03}[r.IntN(5)]
	one := func(at fx.Vec) *kernel.Tube {
		sp := (400 + 120*r.Float64()) * kt
		dir := geom.Polar(1, r.Float64()*2*math.Pi)
		p := conv.NMPoint(at)
		tb := straight(0, p.Sub(dir.Scale(sp*tm)), p.Add(dir.Scale(sp*600)), sp, 35000, 0, k)
		// The leg starts at time zero, and its velocity is a whole number of
		// micrometres per second. So this start puts the aircraft on the
		// point at tm. It is within a millimetre of the converted start.
		l := &tb.Traj.Legs[0]
		l.P0 = fx.Vec{X: at.X - l.V.X*int64(tm), Y: at.Y - l.V.Y*int64(tm)}
		return tb
	}
	a := one(meet)
	off := conv.Point(geom.Polar(miss, r.Float64()*2*math.Pi))
	b := one(meet.Add(off))
	g.add("collision", fmt.Sprintf("Both are aimed at one point at %d µs. The second misses it by (%d, %d) µm. Growth %d µm/s.",
		sec(tm), off.X, off.Y, a.K), a, b, 0, sec(tm+300), stdSep)
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
	// The two velocities are computed from different points and can differ
	// in the last digit. The group needs them equal.
	b.Traj.Legs[0].V, b.VMin = a.Traj.Legs[0].V, a.VMin
	g.add("parallel", fmt.Sprintf("Abeam by %.2f nm at equal velocity.", gap), a, b, 0, sec(300+1100*r.Float64()), stdSep)
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
	// a holds the band down from 36,000 ft until its transition ends, plus
	// its allowance. b takes the band down to 36,000 ft from its transition
	// start, less its allowance. Both have the allowance of a radius of
	// 1.2 nm that grows at k from time zero, at the speed sp.
	vacated := leave + 2000/climb
	allowance := (1.2 + k*vacated) / sp
	nominal := vacated + 2*allowance
	enter := nominal + (r.Float64()*2-1)*4
	a := path(0, p, p.Add(dir.Scale(sp*1400)), sp, step(36000, leave, 34000), 0, k)
	b := path(0, p.Add(off), p.Add(off).Add(dir.Scale(sp*1400)), sp, step(38000, enter, 36000), 0, k)
	g.add(group, fmt.Sprintf("Enters %.2f s after the level is nominally free.", enter-nominal), a, b, 0, sec(1300), sep)
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
	it := track(t0, pos, vel, alt, to)
	if r.Float64() < 0.5 {
		it.VURate = conv.VRate(5 * r.Float64())
	}
	g.add("intruder", "", c, it, sec(t0), sec(t0+300), stdSep)
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
	it := track(t0, p.Add(off), dir.Scale(sp), 35000+gap, 35000+gap)
	it.VURate = conv.VRate(rate)
	// Separated until |gap| - 200 - 300 - rate·τ falls below 600.
	g.add("intruder", fmt.Sprintf("Level, %.0f ft away, with vertical uncertainty growing at %.1f ft/s: separation ends after %.0f s.",
		math.Abs(gap), rate, (math.Abs(gap)-1100)/rate), c, it, sec(t0), sec(t0+300), stdSep)
}

// restart builds pairs whose conflict starts in the middle of the interval,
// and starts the check at the exact first conflict time, or a little to
// either side of it: one microsecond, a millisecond, half a second, five
// seconds. An earlier version of this project's analytic implementation
// reported the end of the conflict when a check started exactly at its
// beginning.
func (g *gen) restart(n int) {
	var ac kernel.AnalyticChecker
	r := g.r
	offsets := []int64{0, 1, -1, 1000, -1000, fx.Sec / 2, -fx.Sec / 2, 5 * fx.Sec, -5 * fx.Sec}
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
		to := sec(tm + 300)
		first := ac.FirstConflict(a, b, 0, to, stdSep.params())
		if !first.Conflict || first.T < 20*fx.Sec {
			continue
		}
		off := offsets[made%len(offsets)]
		g.add("restart", fmt.Sprintf("Starts %+d µs from the exact start of the conflict.", off), a, b, first.T+off, to, stdSep)
		made++
	}
}

// kind holds the values of a tube that do not come from its trajectory: the
// radius at the anchor (nm), its growth (nm/s), and the vertical uncertainty
// (ft).
type kind struct{ r0, k, vu0 float64 }

// contract is the kind of the tube of a contract.
func contract(k float64) kind { return kind{1.2, k, 200} }

// fastGrowing has the values of an intruder tube. At 150 to 170 kt its
// radius grows at 0.7 to 0.8 of the speed of the aircraft.
var fastGrowing = kind{1.5, 2.0 / 60, 300}

// abeam returns two tubes on straight parallel paths one nautical mile apart,
// with one velocity, from time zero to end (s). Both are level at 36,000 ft
// and have their anchor at time zero. The pair is never horizontally
// separated. So an answer for it depends on the vertical part alone.
func abeam(p, dir geom.Vec2, sp, end float64, ka, kb kind) (a, b *kernel.Tube) {
	one := func(from geom.Vec2, kd kind) *kernel.Tube {
		legs := ftraj.Route(0, []geom.Vec2{from, from.Add(dir.Scale(sp * end))}, []float64{sp})
		return mk(legs, ftraj.Level(0, legs[0].T1, 36000), 0, kd.r0, kd.k, kd.vu0)
	}
	a, b = one(p, ka), one(p.Add(dir.Perp()), kb)
	b.Traj.Legs[0].V, b.VMin = a.Traj.Legs[0].V, a.VMin
	return a, b
}

// descend gives a tube of abeam the profile: level at a0 (ft) until ts (µs),
// a transition to a1 that takes 120 s, and level at a1 to the end. The
// transition is the second segment.
func descend(tb *kernel.Tube, a0 float64, ts int64, a1 float64) {
	te, from, to := ts+120*fx.Sec, conv.Alt(a0), conv.Alt(a1)
	tb.Traj.VSegs = []traj.VSeg{
		{T0: tb.Start(), T1: ts, A0: from, A1: from},
		{T0: ts, T1: te, A0: from, A1: to},
		{T0: te, T1: tb.End(), A0: to, A1: to},
	}
}

// startFor returns the start of a segment that is first in effect at t, with
// no slack: t + floor(R(t)/VMin). At t the aircraft may be early by that
// much. One microsecond before t it may not.
func startFor(tb *kernel.Tube, t int64) int64 {
	q, _ := tb.RadiusPM(t).DivFloor(tb.VMin)
	return t + q
}

// lastFor returns the last time at which a segment that ends at te is in
// effect, with no slack. It requires an anchor at time zero, te >= 0, and a
// radius that grows more slowly than the aircraft flies. It solves
// VMin·(t − te) <= M·R0 + K·t for t.
func lastFor(tb *kernel.Tube, te int64) int64 {
	q, _ := fx.Mul(tb.VMin, te).Add(fx.Mul(tb.R0, fx.M)).DivFloor(tb.VMin - tb.K)
	return q
}

// want holds a vector of the group "slack" to its construction. The
// reference must give the class and the time window that the construction
// gives. The transition of tube b must be first in effect, with no slack, at
// first. If last is not zero, the transition of tube a must be last in
// effect, with no slack, at last. So every time in a note is confirmed by
// the statement of the rule in arbitrary-precision arithmetic.
func (g *gen) want(c *Case, first, last int64, class string, firstMin, firstMax int64) {
	e := c.Expect
	if e.Class != class || (class != Clear && (*e.FirstMin != firstMin || *e.FirstMax != firstMax)) {
		g.fail(fmt.Errorf("%s: built for class %s with the time window %d to %d, and the reference gives %+v", c.ID, class, firstMin, firstMax, e))
	}
	if s := &c.B.VSegs[1]; !c.B.inEffect(s, first) || c.B.inEffect(s, first-1) {
		g.fail(fmt.Errorf("%s: the transition of b is not first in effect at %d", c.ID, first))
	}
	if s := &c.A.VSegs[min(1, len(c.A.VSegs)-1)]; last != 0 && (!c.A.inEffect(s, last) || c.A.inEffect(s, last+1)) {
		g.fail(fmt.Errorf("%s: the transition of a is not last in effect at %d", c.ID, last))
	}
}

// slack builds the group "slack". In every pair one aircraft, b, descends
// from 38,000 ft to 36,000 ft, the level of the other aircraft, a. The pair
// is never horizontally separated. So the first conflict is the first time
// at which the transition of b and a segment of a at 36,000 ft count
// together. The construction fixes that time to the microsecond.
//
//   - enter: a stays level. The check starts far before the first conflict,
//     exactly one second before it, or one microsecond more than a second
//     before it.
//   - enterSlack: the same under a slack. The first conflict is earlier by
//     exactly the slack.
//   - leave: a descends out of 36,000 ft, and its radius grows almost as fast
//     as it flies. b comes into effect just close enough to the last time of
//     a for a conflict, or one microsecond too far from it, under the
//     standard and under the tightened standard. It does so with no slack
//     and with a slack of one second.
func (g *gen) slack() {
	// The pair for which this project's sampled implementation was once
	// earlier than the tolerance allowed, when a slack moved the times of a
	// segment.
	a, b := abeam(geom.Vec2{X: 3, Y: 4}, geom.Vec2{X: 1}, 400*kt, 1100, contract(12*kt), contract(12*kt))
	descend(b, 38000, 600_990_000, 36000)
	c := g.add("slack", "b descends to the level of a. Its transition is first in effect at 573000001 µs. "+
		"The time window starts exactly one second before that, although the radius grows.", a, b, 0, sec(1024), stdSep)
	g.want(c, 573_000_001, 0, Conflict, 572_000_001, 573_000_001)

	for i := 0; i < 11; i++ {
		g.enter(i)
	}
	for i := 0; i < 8; i++ {
		g.enterSlack(i)
	}
	for i := 0; i < 12; i++ {
		g.leave(i, 0)
	}
	for i := 0; i < 8; i++ {
		g.leave(i, fx.Sec)
	}
}

// entering returns a pair of contract tubes in which b descends to the level
// of a, with a transition that is first in effect, with no slack, at first.
// first is one microsecond after a whole second from 300 s to 800 s.
func (g *gen) entering() (a, b *kernel.Tube, first int64) {
	r := g.r
	sp := (380 + 100*r.Float64()) * kt
	k := (6 + 6*r.Float64()) * kt
	dir := geom.Polar(1, r.Float64()*2*math.Pi)
	p := geom.Vec2{X: (r.Float64()*2 - 1) * 20, Y: (r.Float64()*2 - 1) * 20}
	a, b = abeam(p, dir, sp, 1100, contract(k), contract(k))
	first = int64(300+r.IntN(501))*fx.Sec + 1
	descend(b, 38000, startFor(b, first), 36000)
	return a, b, first
}

func (g *gen) enter(i int) {
	a, b, first := g.entering()
	note := fmt.Sprintf("b descends to the level of a. Its transition is first in effect at %d µs.", first)
	from, to := int64(0), sec(1024)
	// A check of 64, 128, or 256 s. A method that halves it comes to
	// intervals of one second.
	length := int64(64<<g.r.IntN(3)) * fx.Sec
	switch i % 3 {
	case 1:
		from, to = first-TimeTol, first-TimeTol+length
		note += " The check starts exactly one second before that: a conflict at the start of the check conforms."
	case 2:
		from, to = first-TimeTol-1, first-TimeTol-1+length
		note += " The check starts one second and one microsecond before that: a conflict at the start of the check does not conform."
	}
	g.want(g.add("slack", note, a, b, from, to, stdSep), first, 0, Conflict, first-TimeTol, first)
}

func (g *gen) enterSlack(i int) {
	a, b, first := g.entering()
	w := int64(fx.Sec)
	if i%4 == 3 {
		w = 10 * fx.Sec
	}
	note := fmt.Sprintf("b descends to the level of a. With no slack its transition is first in effect at %d µs. "+
		"Under the slack of %d µs the first conflict is exactly that much earlier.", first, w)
	to := sec(1024)
	if i%2 == 1 {
		// A Verifier that moves the start of the segment by the slack finds
		// the conflict later than this, because the radius grows.
		to = first - w + 1000
		note += " The check ends 1000 µs after the first conflict."
	}
	c := g.add("slack", note, a, b, 0, to, Sep{Sh: stdSep.Sh, SvTrue: stdSep.SvTrue, VertSlack: w})
	g.want(c, first, 0, Conflict, first-w-TimeTol, first-w)
}

// leave builds a pair in which a descends out of 36,000 ft and b descends
// into it. With no slack, the transition of a is last in effect at the time
// last, and the transition of b is first in effect d later. Under a slack w
// each of the two counts for w longer. So the pair is in conflict if
// d <= 2·w, and it is in conflict under the tightened standard if
// d <= 2·(w + TimeTol).
func (g *gen) leave(i int, w int64) {
	r := g.r
	sp := (150 + 20*r.Float64()) * kt
	dir := geom.Polar(1, r.Float64()*2*math.Pi)
	p := geom.Vec2{X: (r.Float64()*2 - 1) * 20, Y: (r.Float64()*2 - 1) * 20}
	a, b := abeam(p, dir, sp, 1400, fastGrowing, contract(10*kt))
	ts := sec(10 + 30*r.Float64())
	descend(a, 36000, ts, 34000)
	last := lastFor(a, ts+120*fx.Sec)
	d := []int64{2 * w, 2*w + 1, 2 * (w + TimeTol), 2*(w+TimeTol) + 1}[i%4]
	first := last + d
	descend(b, 38000, startFor(b, first), 36000)

	note := fmt.Sprintf("a descends out of the level that b descends into. The radius of a grows at %d µm/s, and a flies at %d µm/s. "+
		"With no slack the transition of a is last in effect at %d µs, and the transition of b is first in effect ", a.K, a.VMin, last)
	if d == 0 {
		note += "at that microsecond."
	} else {
		note += fmt.Sprintf("%d µs later.", d)
	}
	if w > 0 {
		note += fmt.Sprintf(" The slack is %d µs: each of the two counts for that much longer.", w)
	}
	note += []string{
		" That is just close enough for a conflict.",
		" That is one microsecond too far for a conflict. Under the tightened standard the pair is in conflict.",
		" That is just close enough for a conflict under the tightened standard.",
		" That is one microsecond too far for a conflict under the tightened standard.",
	}[i%4]
	end := sec(1024)
	c := g.add("slack", note, a, b, 0, end, Sep{Sh: stdSep.Sh, SvTrue: stdSep.SvTrue, VertSlack: w})
	switch i % 4 {
	case 0:
		g.want(c, first, last, Conflict, last+w-TimeTol, last+w)
	case 1:
		g.want(c, first, last, Either, last+w+1-TimeTol, end)
	case 2:
		g.want(c, first, last, Either, last+w+TimeTol, end)
	case 3:
		g.want(c, first, last, Clear, 0, 0)
	}
}
