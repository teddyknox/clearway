package kernel

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/teddyknox/clearway/internal/conv"
	"github.com/teddyknox/clearway/internal/ftraj"
	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/traj"
)

// The tests build trajectories in nautical miles, feet, and seconds, and
// convert them once, as a planner does.
const cruise = 480 * geom.Kt

func newTestKernel() *Kernel {
	d, err := Arrangement("dual")
	if err != nil {
		panic(err)
	}
	return New(DefaultParams(), d)
}

// straight returns a level trajectory from a to b (nm) that starts at t0 (s),
// at alt (ft).
func straight(t0 float64, a, b geom.Vec2, alt float64) *traj.Trajectory {
	legs := ftraj.Route(t0, []geom.Vec2{a, b}, []float64{cruise})
	return conv.Traj(&ftraj.Trajectory{Legs: legs, VSegs: ftraj.Level(t0, legs[0].T1, alt)})
}

// tailVia returns a tail for the active contract of f: from its position at
// td, through via (if not nil), to end, level at alt0 or stepping to alt1.
func tailVia(f *Flight, td float64, via *geom.Vec2, end geom.Vec2, alt0, alt1 float64) *traj.Trajectory {
	pts := []geom.Vec2{conv.PM(f.Active.Traj.Pos(sec(td)))}
	speeds := []float64{cruise}
	if via != nil {
		pts = append(pts, *via)
		speeds = append(speeds, cruise)
	}
	pts = append(pts, end)
	legs := ftraj.Route(td, pts, speeds)
	t1 := legs[len(legs)-1].T1
	return conv.Traj(&ftraj.Trajectory{Legs: legs, VSegs: ftraj.Step(td, t1, alt0, td, alt1, 1000.0/60)})
}

// trackOn returns a track of an aircraft that is exactly on tr at t (s).
func trackOn(tr *traj.Trajectory, t float64) Track {
	at := tr.Pos(sec(t))
	x, _ := fx.Int(at.X + fx.M/2).DivFloor(fx.M)
	y, _ := fx.Int(at.Y + fx.M/2).DivFloor(fx.M)
	return Track{T: sec(t), Pos: fx.Vec{X: x, Y: y}, Vel: tr.VelAt(sec(t)), Alt: tr.AltAt(sec(t))}
}

func mustAdmit(t *testing.T, k *Kernel, id FlightID, tr *traj.Trajectory) {
	t.Helper()
	if v := k.Admit(id, tr); !v.OK {
		t.Fatalf("admit %d rejected: %s with %d at %d", id, v.Reason, v.With, v.At)
	}
}

var (
	west = geom.Vec2{X: -160}
	east = geom.Vec2{X: 160}
)

func TestAdmitRejectsConflictInsideCommitHorizon(t *testing.T) {
	k := newTestKernel()
	mustAdmit(t, k, 1, straight(0, west, east, 35000))
	// Opposite direction, same level, meeting in about six minutes.
	if v := k.Admit(2, straight(0, geom.Vec2{X: -60}, west.Scale(1.2), 35000)); v.OK {
		t.Fatal("head-on admission accepted")
	} else if v.Reason != RejConflict || v.With != 1 {
		t.Fatalf("got %s with %d", v.Reason, v.With)
	}
	// Same geometry 1,000 ft higher is separated.
	mustAdmit(t, k, 2, straight(0, geom.Vec2{X: -60}, west.Scale(1.2), 36000))
}

func TestWindowAndPredictedConflict(t *testing.T) {
	k := newTestKernel()
	mustAdmit(t, k, 1, straight(0, west, east, 35000))
	// Head-on from the far side: the meeting point is beyond the commit
	// horizon and inside the verification horizon.
	mustAdmit(t, k, 2, straight(0, east, west, 35000))
	k.Tick()
	cs := k.Conflicts()
	if len(cs) != 1 || cs[0].A != 1 || cs[0].B != 2 {
		t.Fatalf("conflicts: %+v", cs)
	}
	if cs[0].T < k.P.CommitHorizon || cs[0].T > k.P.Horizon {
		t.Fatalf("conflict at %d µs, want between commit horizon and horizon", cs[0].T)
	}
	if got := k.Coast(); got != cs[0].T {
		t.Fatalf("coast %d, conflict at %d", got, cs[0].T)
	}
	if k.PairState(1, 2) != PairVerified {
		t.Fatal("pair should be verified until the conflict time")
	}
	// An amendment that moves flight 2 up 2,000 ft resolves it.
	f2 := k.Flight(2)
	v := k.Propose(2, tailVia(f2, 150, nil, west.Scale(1.1), 35000, 37000), Automation)
	if !v.OK {
		t.Fatalf("resolution rejected: %s", v.Reason)
	}
	k.Ack(2)
	k.Tick()
	if len(k.Conflicts()) != 0 {
		t.Fatalf("conflict remains: %+v", k.Conflicts())
	}
}

// I5: two amendments that are each safe alone and unsafe together. The second
// one is checked against the state that contains the first.
func TestSerializedCommit(t *testing.T) {
	k := newTestKernel()
	mustAdmit(t, k, 1, straight(0, geom.Vec2{X: -160, Y: 0}, geom.Vec2{X: 160, Y: 0}, 35000))
	mustAdmit(t, k, 2, straight(0, geom.Vec2{X: -160, Y: 30}, geom.Vec2{X: 160, Y: 30}, 35000))
	mid := geom.Vec2{X: 0, Y: 15}
	a := tailVia(k.Flight(1), 150, &mid, geom.Vec2{X: 160, Y: 0}, 35000, 35000)
	b := tailVia(k.Flight(2), 150, &mid, geom.Vec2{X: 160, Y: 30}, 35000, 35000)

	// Each one is acceptable on its own.
	probe := newTestKernel()
	mustAdmit(t, probe, 1, straight(0, geom.Vec2{X: -160, Y: 0}, geom.Vec2{X: 160, Y: 0}, 35000))
	mustAdmit(t, probe, 2, straight(0, geom.Vec2{X: -160, Y: 30}, geom.Vec2{X: 160, Y: 30}, 35000))
	if v := probe.Propose(2, b, Automation); !v.OK {
		t.Fatalf("second amendment alone rejected: %s", v.Reason)
	}

	if v := k.Propose(1, a, Automation); !v.OK {
		t.Fatalf("first amendment rejected: %s", v.Reason)
	}
	if v := k.Propose(2, b, Automation); v.OK || v.Reason != RejConflict {
		t.Fatalf("second amendment: ok=%v reason=%s", v.OK, v.Reason)
	}
}

// I4: while an amendment is pending, the old tube and the new tube are both
// protected.
func TestPendingProtection(t *testing.T) {
	setup := func() *Kernel {
		k := newTestKernel()
		mustAdmit(t, k, 1, straight(0, west, east, 35000))
		// Flight 1 will turn north at x = -140 and leave the cell at the top.
		v := k.Propose(1, tailVia(k.Flight(1), 150, nil, geom.Vec2{X: -140, Y: 165}, 35000, 35000), Automation)
		if !v.OK {
			t.Fatalf("amendment rejected: %s", v.Reason)
		}
		return k
	}
	// Crosses the old path of flight 1 near x = -40, when flight 1 would be there.
	onOld := straight(0, geom.Vec2{X: -40, Y: -120}, geom.Vec2{X: -40, Y: 160}, 35000)
	// Crosses the new path of flight 1 at y = 80, when flight 1 would be there
	// (t = 750 s).
	onNew := straight(0, geom.Vec2{X: -40, Y: 80}, geom.Vec2{X: -170, Y: 80}, 35000)

	k := setup()
	if v := k.Admit(2, onOld); v.OK {
		t.Fatal("pending: conflict with the old tube accepted")
	}
	if v := k.Admit(3, onNew); v.OK {
		t.Fatal("pending: conflict with the new tube accepted")
	}

	k = setup()
	k.Ack(1) // old tube released
	mustAdmit(t, k, 2, onOld)
	if v := k.Admit(3, onNew); v.OK {
		t.Fatal("after ack: conflict with the active tube accepted")
	}

	k = setup()
	k.Refuse(1) // new tube released
	mustAdmit(t, k, 3, onNew)
	if v := k.Admit(2, onOld); v.OK {
		t.Fatal("after refusal: conflict with the active tube accepted")
	}
}

func TestProposalRules(t *testing.T) {
	k := newTestKernel()
	mustAdmit(t, k, 1, straight(0, west, east, 35000))
	f := k.Flight(1)

	// I3: a tail that ends inside the cell is open.
	if v := k.Propose(1, tailVia(f, 150, nil, geom.Vec2{X: 20, Y: 20}, 35000, 35000), Automation); v.Reason != RejOpen {
		t.Errorf("open tail: %s", v.Reason)
	}
	// Too close to deliver.
	if v := k.Propose(1, tailVia(f, 60, nil, east, 35000, 35000), Automation); v.Reason != RejLead {
		t.Errorf("short lead: %s", v.Reason)
	}
	// Not continuous with the active contract.
	bad := straight(150, geom.Vec2{X: -100, Y: 40}, east, 35000)
	if v := k.Propose(1, bad, Automation); v.Reason != RejMalformed {
		t.Errorf("discontinuous tail: %s", v.Reason)
	}
	// Wrong starting altitude.
	if v := k.Propose(1, tailVia(f, 150, nil, east, 36000, 36000), Automation); v.Reason != RejMalformed {
		t.Errorf("altitude jump: %s", v.Reason)
	}
	// I1: only the holder of authority may propose.
	if v := k.Propose(1, tailVia(f, 150, nil, east, 35000, 37000), Human); v.Reason != RejAuthority {
		t.Errorf("wrong authority: %s", v.Reason)
	}
	k.SetAuthority(1, Human)
	if v := k.Propose(1, tailVia(f, 150, nil, east, 35000, 37000), Automation); v.Reason != RejAuthority {
		t.Errorf("automation after transfer: %s", v.Reason)
	}
	if v := k.Propose(1, tailVia(f, 150, nil, east, 35000, 37000), Human); !v.OK {
		t.Fatalf("human proposal rejected: %s", v.Reason)
	}
	// One pending amendment at a time.
	if v := k.Propose(1, tailVia(f, 200, nil, east, 35000, 35000), Human); v.Reason != RejBusy {
		t.Errorf("second pending: %s", v.Reason)
	}
	k.Ack(1)
	// The flight is now in a climb from 150 s to 270 s. A divergence point
	// inside the transition is not allowed.
	k.Advance(sec(60))
	f = k.Flight(1)
	mid := conv.Ft(f.Active.Traj.AltAt(sec(200)))
	tail := tailVia(f, 200, nil, east, mid, mid)
	if v := k.Propose(1, tail, Human); v.Reason != RejVertical {
		t.Errorf("divergence inside a transition: %s", v.Reason)
	}
}

func TestMonitorMarksIntruder(t *testing.T) {
	k := newTestKernel()
	tr1 := straight(0, west, east, 35000)
	mustAdmit(t, k, 1, tr1)
	mustAdmit(t, k, 2, straight(0, geom.Vec2{X: -160, Y: 20}, geom.Vec2{X: 160, Y: 20}, 35000))

	k.Advance(sec(100))
	k.UpdateTrack(1, trackOn(tr1, 100))
	if k.Flight(1).Intruder {
		t.Fatal("conforming track marked as intruder")
	}
	// Two miles off the path, turning toward flight 2.
	tk := trackOn(tr1, 105)
	tk.Pos.Y += nm(2)
	tk.Vel = conv.Vel(conv.NMVel(tk.Vel).Rotate(0.5))
	k.Advance(sec(105))
	k.UpdateTrack(1, tk)
	f := k.Flight(1)
	if !f.Intruder || f.Active != nil || f.ITube == nil {
		t.Fatalf("not marked: intruder=%v active=%v", f.Intruder, f.Active != nil)
	}
	if k.Stats.Exits[ExitHorizontal] != 1 {
		t.Fatalf("exit cause counts: %v", k.Stats.Exits)
	}
	k.Tick()
	cs := k.Conflicts()
	if len(cs) != 1 || !cs[0].Intruder || cs[0].A != 2 || cs[0].B != 1 {
		t.Fatalf("conflicts: %+v", cs)
	}
	if k.PairState(1, 2) != PairIntruder {
		t.Fatal("pair state should be intruder")
	}
	// An altitude excursion is a vertical exit.
	k2 := newTestKernel()
	mustAdmit(t, k2, 1, tr1)
	tk = trackOn(tr1, 100)
	tk.Alt += ft(300)
	k2.Advance(sec(100))
	k2.UpdateTrack(1, tk)
	if !k2.Flight(1).Intruder || k2.Stats.Exits[ExitVertical] != 1 {
		t.Fatal("level bust not detected")
	}
	// A wrong selected altitude is caught before the aircraft moves.
	k3 := newTestKernel()
	mustAdmit(t, k3, 1, tr1)
	tk = trackOn(tr1, 100)
	tk.HasSel, tk.SelAlt = true, ft(36000)
	k3.Advance(sec(100))
	k3.UpdateTrack(1, tk)
	if !k3.Flight(1).Intruder || k3.Stats.Exits[ExitIntent] != 1 {
		t.Fatal("selected-altitude mismatch not detected")
	}
}

// With a lost acknowledgement the kernel waits until the track shows which
// contract the aircraft flies.
func TestUnknownIsSettledByTheMonitor(t *testing.T) {
	for _, fliesNew := range []bool{true, false} {
		k := newTestKernel()
		oldTr := straight(0, west, east, 35000)
		mustAdmit(t, k, 1, oldTr)
		v := k.Propose(1, tailVia(k.Flight(1), 150, nil, geom.Vec2{X: -140, Y: 165}, 35000, 35000), Automation)
		if !v.OK {
			t.Fatal(v.Reason)
		}
		newTr := v.Contract.Traj
		k.DeliveryFailed(1)
		if k.Flight(1).PState != PendingUnknown {
			t.Fatal("state should be unknown")
		}
		flown := oldTr
		if fliesNew {
			flown = newTr
		}
		for now := 5.0; now <= 400; now += 5 {
			k.Advance(sec(now))
			k.UpdateTrack(1, trackOn(flown, now))
			if k.Flight(1).PState == PendingNone {
				break
			}
		}
		f := k.Flight(1)
		if f.Intruder || f.PState != PendingNone || f.Pending != nil {
			t.Fatalf("fliesNew=%v: not settled: intruder=%v state=%d", fliesNew, f.Intruder, f.PState)
		}
		if fliesNew && (f.Active.Traj != newTr || k.Stats.UnknownPromoted != 1) {
			t.Fatal("new contract should be active")
		}
		if !fliesNew && (f.Active.Traj != oldTr || k.Stats.UnknownDropped != 1) {
			t.Fatal("old contract should be active")
		}
	}
}

func TestReanchor(t *testing.T) {
	k := newTestKernel()
	tr := straight(0, west, east, 35000)
	mustAdmit(t, k, 1, tr)
	now := sec(600)
	k.Advance(now)
	want := fx.Mul(k.P.BaseR0(), fx.M).Add(fx.Mul(k.P.K, now))
	if got := k.Flight(1).Active.RadiusPM(now); got != want {
		t.Fatalf("radius before re-anchor: %v pm, want %v", got, want)
	}
	// The aircraft is 1 nm ahead of its nominal position.
	tk := trackOn(tr, 600)
	tk.Pos.X += nm(1)
	k.UpdateTrack(1, tk)
	done := k.ReanchorAll()
	if len(done) != 1 || fx.Abs(done[0].Dt-sec(1/cruise)) > 10 {
		t.Fatalf("re-anchor: %+v, want a shift of about %d µs", done, sec(1/cruise))
	}
	act := k.Flight(1).Active
	// The nominal position matches the track to the distance flown in one
	// microsecond, and the path is exactly the old one.
	at := act.Traj.Pos(now)
	if fx.Abs(at.X-tk.Pos.X*fx.M) > 300*fx.M || at.Y != tk.Pos.Y*fx.M {
		t.Fatalf("nominal position %v pm does not match the track %v µm", at, tk.Pos)
	}
	if l, was := act.Traj.Legs[0], tr.Legs[0]; l.P0 != was.P0 || l.V != was.V || l.T0 != was.T0-done[0].Dt || l.T1 != was.T1-done[0].Dt {
		t.Fatalf("the re-anchor changed the path: %+v, was %+v", l, was)
	}
	if got := act.RadiusPM(now); got != fx.Mul(k.P.BaseR0(), fx.M) {
		t.Fatalf("radius after re-anchor: %v pm", got)
	}
}

func TestRecontractIntruder(t *testing.T) {
	k := newTestKernel()
	tr := straight(0, west, east, 35000)
	mustAdmit(t, k, 1, tr)
	k.Advance(sec(100))
	tk := trackOn(tr, 100)
	tk.Pos.Y += nm(3)
	k.UpdateTrack(1, tk)
	if !k.Flight(1).Intruder {
		t.Fatal("expected intruder")
	}
	if v := k.Propose(1, straight(250, conv.NMPoint(tk.Pos), east, 35000), Automation); v.Reason != RejIntruder {
		t.Fatalf("amendment for an intruder: %s", v.Reason)
	}
	ts := 250.0
	start := conv.NMPoint(tk.Pos).Add(conv.NMVel(tk.Vel).Scale(ts - 100))
	if v := k.ProposeRecontract(1, straight(ts, start.Add(geom.Vec2{Y: 5}), east, 35000), Automation); v.Reason != RejMalformed {
		t.Fatalf("re-contract from the wrong place: %s", v.Reason)
	}
	re := straight(ts, start, east, 35000)
	if v := k.ProposeRecontract(1, re, Automation); !v.OK {
		t.Fatalf("re-contract rejected: %s", v.Reason)
	}
	k.Ack(1)
	if !k.Flight(1).Intruder {
		t.Fatal("still an intruder until the Monitor sees it conform")
	}
	k.Advance(sec(255))
	k.UpdateTrack(1, trackOn(re, 255))
	f := k.Flight(1)
	if f.Intruder || f.ITube != nil || f.Active == nil || k.Stats.RecontractOK != 1 {
		t.Fatal("re-contract did not take effect")
	}
}

// checkI2 checks I2 with the oracle: every pair of contract tubes is separated
// at every scanned time from now to the last microsecond before the end of its
// verified window. The check is exact: it has no tolerance.
func checkI2(t *testing.T, k *Kernel) {
	t.Helper()
	var tubes []*Tube
	for _, id := range k.IDs() {
		tubes = contractTubes(tubes, k.Flight(id))
	}
	for i, a := range tubes {
		for _, b := range tubes[i+1:] {
			if a.Flight == b.Flight {
				continue
			}
			u, ok := k.windows[mkKey(a.ID, b.ID)]
			if !ok {
				t.Fatalf("no window for tubes %d and %d", a.ID, b.ID)
			}
			if u <= k.now {
				continue
			}
			if c, at := oracle(a, b, k.now, u-1, k.sep(), 250_000); c {
				t.Fatalf("I2 violated: flights %d and %d are not separated at %d, window to %d, now %d",
					a.Flight, b.Flight, at, u, k.now)
			}
		}
	}
}

// Random admissions and random amendments, many of them unsafe. Whatever the
// kernel commits must satisfy I2, I3, and I4. The run is recorded, and a
// replay must reach the same state.
func TestRandomProposalsKeepInvariants(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 11))
	k := newTestKernel()
	log := &memLog{}
	k.Rec = log
	levels := []float64{34000, 35000, 36000}
	rim := func() geom.Vec2 { return geom.Polar(160, r.Float64()*2*math.Pi) }
	next := FlightID(1)
	var commits, rejects int
	for now := 0.0; now < 3600; now += 10 {
		k.Advance(sec(now))
		// Tracks: every flight follows its active contract exactly.
		for _, id := range append([]FlightID(nil), k.IDs()...) {
			f := k.Flight(id)
			if f.Active == nil {
				continue
			}
			if sec(now) >= f.Active.End() {
				k.Remove(id)
				continue
			}
			if sec(now) >= f.Active.Start() {
				k.UpdateTrack(id, trackOn(f.Active.Traj, now))
			}
		}
		if int(now)%60 == 0 {
			k.ReanchorAll()
		}
		if r.Float64() < 0.5 {
			a, b := rim(), rim()
			if a.Dist(b) > 120 {
				if v := k.Admit(next, straight(now+10, a, b, levels[r.IntN(3)])); v.OK {
					commits++
				} else {
					rejects++
				}
				next++
			}
		}
		for n := 0; n < 3 && len(k.IDs()) > 0; n++ {
			id := k.IDs()[r.IntN(len(k.IDs()))]
			f := k.Flight(id)
			if f.Active == nil || f.Intruder {
				continue
			}
			td := now + 120 + r.Float64()*120
			if sec(td) >= f.Active.End()-sec(60) {
				continue
			}
			alt := conv.Ft(f.Active.Traj.AltAt(sec(td)))
			via := geom.Polar(r.Float64()*120, r.Float64()*2*math.Pi)
			end := rim()
			if r.Float64() < 0.1 {
				end = end.Scale(0.5) // an open contract
			}
			tail := tailVia(f, td, &via, end, alt, levels[r.IntN(3)])
			if v := k.Propose(id, tail, Automation); v.OK {
				commits++
				switch r.IntN(4) {
				case 0:
					k.Refuse(id)
				case 1: // stays pending for a while
				default:
					k.Ack(id)
				}
			} else {
				rejects++
			}
		}
		// Settle old pending amendments.
		for _, id := range k.IDs() {
			if f := k.Flight(id); f.Pending != nil && sec(now) > f.Pending.DivergeAt-sec(30) {
				k.Refuse(id)
			}
		}
		k.Tick()
		if int(now)%50 == 0 {
			checkI2(t, k)
		}
		for _, id := range k.IDs() {
			f := k.Flight(id)
			for _, tb := range contractTubes(nil, f) {
				if !k.closed(tb.Traj) {
					t.Fatalf("I3 violated: flight %d", id)
				}
			}
			if f.Pending != nil && f.Active == nil {
				t.Fatalf("I4 violated: flight %d has a pending contract and no active one", id)
			}
		}
	}
	if commits < 200 || rejects < 200 {
		t.Fatalf("weak mix: %d commits, %d rejects", commits, rejects)
	}
	if k.D.Hard != 0 {
		t.Fatalf("%d hard disagreements", k.D.Hard)
	}
	t.Logf("%d commits, %d rejects (%v), %d marginal disagreements, %d reanchors",
		commits, rejects, k.Stats.Rejects, k.D.Marginal, k.Stats.ReanchorOK)

	// Replay: a new kernel that applies the recorded inputs must return the
	// same verdicts and reach the same state.
	d, err := Arrangement("dual")
	if err != nil {
		t.Fatal(err)
	}
	re := New(k.P, d)
	answered := 0
	for i, in := range log.inputs {
		v, has := re.Apply(in)
		if has != in.Kind.HasVerdict() {
			t.Fatalf("input %d of kind %d: verdict %v", i, in.Kind, has)
		}
		if !has {
			continue
		}
		want := log.verdicts[answered]
		answered++
		if v.OK != want.OK || v.Reason != want.Reason || v.With != want.With || v.At != want.At ||
			(v.OK && v.Contract.ID != want.Contract.ID) {
			t.Fatalf("input %d: the replay returned %+v, the kernel returned %+v", i, v, want)
		}
	}
	if answered != len(log.verdicts) || answered != commits+rejects {
		t.Fatalf("%d verdicts recorded, %d replayed, %d proposals made", len(log.verdicts), answered, commits+rejects)
	}
	if re.StateDigest() != k.StateDigest() {
		t.Fatal("replay reached a different state")
	}
}

// memLog keeps the inputs and the verdicts of a kernel in memory.
type memLog struct {
	inputs   []Input
	verdicts []Verdict
}

func (m *memLog) Input(in Input)    { m.inputs = append(m.inputs, in) }
func (m *memLog) Verdict(v Verdict) { m.verdicts = append(m.verdicts, v) }

// The state digest must change when any part of the Store changes, and must
// not change when nothing does.
func TestStateDigestCoversTheStore(t *testing.T) {
	build := func() *Kernel {
		k := newTestKernel()
		k.Advance(0)
		k.Admit(1, straight(0, geom.Vec2{X: -160}, geom.Vec2{X: 160}, 35000))
		k.Admit(2, straight(0, geom.Vec2{Y: -160}, geom.Vec2{Y: 160}, 36000))
		k.Advance(sec(60))
		for _, id := range k.IDs() {
			k.UpdateTrack(id, trackOn(k.Flight(id).Active.Traj, 60))
		}
		k.Tick()
		return k
	}
	base := build().StateDigest()
	if build().StateDigest() != base {
		t.Fatal("two kernels with the same inputs have different digests")
	}
	changes := map[string]func(k *Kernel){
		"the clock": func(k *Kernel) { k.Advance(sec(61)) },
		"a track":   func(k *Kernel) { tk := trackOn(k.Flight(1).Active.Traj, 60); tk.VRate = 1; k.UpdateTrack(1, tk) },
		"a selected altitude": func(k *Kernel) {
			tk := trackOn(k.Flight(1).Active.Traj, 60)
			tk.HasSel, tk.SelAlt = true, ft(35000)
			k.UpdateTrack(1, tk)
		},
		"an authority":         func(k *Kernel) { k.SetAuthority(2, Human) },
		"an intruder":          func(k *Kernel) { k.MarkIntruder(2) },
		"a re-anchor":          func(k *Kernel) { k.ReanchorAll() },
		"a removed flight":     func(k *Kernel) { k.Remove(2) },
		"a new flight":         func(k *Kernel) { k.Admit(3, straight(60, geom.Vec2{X: 160}, geom.Vec2{X: -160}, 37000)) },
		"a rejected admission": nil, // must not change the Store
	}
	seen := map[[32]byte]string{}
	for name, change := range changes {
		k := build()
		if change == nil {
			if v := k.Admit(1, straight(60, geom.Vec2{X: 160}, geom.Vec2{X: -160}, 37000)); v.OK {
				t.Fatal("a duplicate admission was committed")
			}
			if k.StateDigest() != base {
				t.Errorf("%s changed the digest", name)
			}
			continue
		}
		change(k)
		d := k.StateDigest()
		if d == base {
			t.Errorf("%s did not change the digest", name)
		}
		if other, dup := seen[d]; dup {
			t.Errorf("%s and %s give the same digest", name, other)
		}
		seen[d] = name
	}
}

// A contract with almost no ground speed turns a small offset into a shift of
// years. The re-anchor must leave such a contract as it is: the result would
// not cover the current time. In floating point this case once produced a
// trajectory whose times were not numbers, and the Verifier did not return.
func TestReanchorKeepsContractWhenShiftIsAbsurd(t *testing.T) {
	k := newTestKernel()
	slow := &traj.Trajectory{
		Legs:  []traj.Leg{{T0: 0, T1: sec(5000), P0: fx.Vec{X: nm(200)}, V: fx.Vec{X: 1, Y: 1}}},
		VSegs: []traj.VSeg{{T0: 0, T1: sec(5000), A0: ft(35000), A1: ft(35000)}},
	}
	mustAdmit(t, k, 1, slow)
	mustAdmit(t, k, 2, straight(0, geom.Vec2{X: -160}, geom.Vec2{X: 160}, 36000))
	k.Advance(sec(60))
	// One metre ahead, at 1.4 µm/s, is eight days ahead.
	k.UpdateTrack(1, Track{T: sec(60), Pos: fx.Vec{X: nm(200) + 1_000_000}, Vel: fx.Vec{X: 1, Y: 1}, Alt: ft(35000)})
	k.UpdateTrack(2, trackOn(k.Flight(2).Active.Traj, 60))
	if k.Flight(1).Intruder {
		t.Fatal("the slow flight is one metre from its place and was marked as an intruder")
	}
	before := k.Flight(1).Active
	done := k.ReanchorAll()
	if k.Flight(1).Active != before {
		t.Fatalf("the contract was replaced: %+v", k.Flight(1).Active.Traj.Legs)
	}
	if len(done) != 1 || done[0].ID != 2 {
		t.Fatalf("re-anchored %+v, want flight 2 alone", done)
	}
}
