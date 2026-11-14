package traj

import (
	"math"
	"testing"

	"github.com/teddyknox/clearway/internal/geom"
)

func line(t0 float64) *Trajectory {
	legs := Route(t0, []geom.Vec2{{X: 0}, {X: 100}, {X: 100, Y: 50}}, []float64{0.1, 0.125})
	return &Trajectory{Legs: legs, VSegs: Step(t0, legs[1].T1, 35000, t0+200, 37000, 1000.0/60)}
}

func TestRouteAndProfile(t *testing.T) {
	tr := line(10)
	if err := tr.Validate(); err != nil {
		t.Fatal(err)
	}
	if tr.Start() != 10 || math.Abs(tr.End()-(10+1000+400)) > 1e-9 {
		t.Fatalf("span %.1f to %.1f", tr.Start(), tr.End())
	}
	if p := tr.PosAt(510); math.Abs(p.X-50) > 1e-9 || p.Y != 0 {
		t.Fatalf("position on leg 1: %+v", p)
	}
	if p := tr.PosAt(1210); math.Abs(p.X-100) > 1e-9 || math.Abs(p.Y-25) > 1e-9 {
		t.Fatalf("position on leg 2: %+v", p)
	}
	if p := tr.PosAt(1e9); p.Dist(geom.Vec2{X: 100, Y: 50}) > 1e-9 {
		t.Fatalf("position after the end: %+v", p)
	}
	// Level, then 2,000 ft at 1,000 ft/min from t = 210 to 330, then level.
	for _, c := range []struct{ t, alt float64 }{{100, 35000}, {270, 36000}, {400, 37000}} {
		if got := tr.AltAt(c.t); math.Abs(got-c.alt) > 1e-6 {
			t.Errorf("altitude at %.0f: %.0f, want %.0f", c.t, got, c.alt)
		}
	}
	// A transition occupies its whole range for its whole duration.
	if lo, hi := tr.BandOver(250, 250); lo != 35000 || hi != 37000 {
		t.Errorf("band inside the transition: %.0f to %.0f", lo, hi)
	}
	if lo, hi := tr.BandOver(100, 150); lo != 35000 || hi != 35000 {
		t.Errorf("band before the transition: %.0f to %.0f", lo, hi)
	}
	if lo, hi := tr.BandOver(100, 215); lo != 35000 || hi != 37000 {
		t.Errorf("band over the start of the transition: %.0f to %.0f", lo, hi)
	}
	if tr.MinSpeed() != 0.1 || tr.MaxSpeedOver(0, 5000) != 0.125 || tr.MaxSpeedOver(20, 30) != 0.1 {
		t.Error("leg speeds")
	}
}

func TestShiftAndSplice(t *testing.T) {
	tr := line(0)
	sh := tr.Shift(-30)
	if sh.PosAt(470).Dist(tr.PosAt(500)) > 1e-9 || sh.AltAt(240) != tr.AltAt(270) {
		t.Fatal("shift does not move every time")
	}
	td := 600.0
	p := tr.PosAt(td)
	legs := Route(td, []geom.Vec2{p, {X: 200, Y: -40}}, []float64{0.12})
	tail := &Trajectory{Legs: legs, VSegs: Level(td, legs[0].T1, 37000)}
	sp, err := Splice(tr, td, tail)
	if err != nil {
		t.Fatal(err)
	}
	if sp.PosAt(300).Dist(tr.PosAt(300)) > 1e-9 || sp.AltAt(300) != tr.AltAt(300) {
		t.Fatal("splice changed the trajectory before the splice time")
	}
	if sp.PosAt(sp.End()).Dist(geom.Vec2{X: 200, Y: -40}) > 1e-6 {
		t.Fatal("splice does not end at the tail's end")
	}
	// A tail that starts somewhere else does not make a valid trajectory.
	bad := &Trajectory{Legs: Route(td, []geom.Vec2{{X: 0, Y: 9}, {X: 200}}, []float64{0.12}), VSegs: Level(td, td+10, 37000)}
	bad.VSegs[0].T1 = bad.Legs[0].T1
	if _, err := Splice(tr, td, bad); err == nil {
		t.Fatal("discontinuous splice accepted")
	}
	// A tail at another altitude does not either.
	wrong := &Trajectory{Legs: legs, VSegs: Level(td, legs[0].T1, 39000)}
	if _, err := Splice(tr, td, wrong); err == nil {
		t.Fatal("altitude jump accepted")
	}
}
