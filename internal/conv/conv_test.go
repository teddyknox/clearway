package conv

import (
	"math"
	"testing"

	"github.com/teddyknox/clearway/internal/ftraj"
	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/geom"
)

func TestUnits(t *testing.T) {
	if Len(1) != fx.NM || Alt(1) != fx.FT || Time(1) != fx.Sec || Speed(1) != fx.NM || VRate(1) != fx.FT {
		t.Fatal("one unit does not convert to the constant of package fx")
	}
	if NM(fx.NM) != 1 || Ft(fx.FT) != 1 || Sec(fx.Sec) != 1 || !math.IsInf(Sec(fx.NoLimit), 1) {
		t.Fatal("the constants of package fx do not convert back to one unit")
	}
	// Every flight level and every whole second is exact.
	for fl := 0.0; fl <= 60000; fl += 500 {
		if Ft(Alt(fl)) != fl {
			t.Fatalf("%v ft does not come back", fl)
		}
	}
	for s := 0.0; s <= 100000; s += 1 {
		if Sec(Time(s)) != s {
			t.Fatalf("%v s does not come back", s)
		}
	}
}

// A value that is not a number, or is too large, becomes a value that the
// kernel rejects. It must not depend on the processor.
func TestValuesOutsideTheLimits(t *testing.T) {
	for _, x := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 1e300, -1e300, 1e12} {
		for name, v := range map[string]int64{"Len": Len(x), "Alt": Alt(x), "Time": Time(x), "Speed": Speed(x)} {
			if v > -fx.MaxTime && v < fx.MaxTime {
				t.Errorf("%s(%v) = %d: inside the limits", name, x, v)
			}
		}
	}
	bad := &ftraj.Trajectory{
		Legs:  []ftraj.Leg{{T0: 0, T1: math.NaN(), P0: geom.Vec2{X: math.Inf(1)}, V: geom.Vec2{Y: math.NaN()}}},
		VSegs: []ftraj.VSeg{{T0: 0, T1: 100, A0: math.NaN(), A1: 1e300}},
	}
	if Traj(bad).Validate() == nil || Traj(nil).Validate() == nil || Traj(&ftraj.Trajectory{}).Validate() == nil {
		t.Fatal("a trajectory with values that are not numbers, or an empty one, is valid")
	}
}

func route() *ftraj.Trajectory {
	pts := []geom.Vec2{{X: -160.3, Y: 12.7}, {X: -20.123456789, Y: 44.4}, {X: 31.7, Y: -8.25}, {X: 170.2, Y: 15.9}}
	legs := ftraj.Route(123.456789, pts, []float64{480 * geom.Kt, 455.5 * geom.Kt, 501.25 * geom.Kt})
	end := legs[len(legs)-1].T1
	return &ftraj.Trajectory{Legs: legs, VSegs: ftraj.Step(123.456789, end, 35000, 700.5, 37000, 1000.0/60)}
}

// A continuous trajectory converts to a valid one: no gap in time, no step
// in altitude, and each leg within a micrometre of the end of the one before.
func TestTrajectory(t *testing.T) {
	fl := route()
	if err := fl.Validate(); err != nil {
		t.Fatal(err)
	}
	tr := Traj(fl)
	if err := tr.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(tr.Legs) != 3 || len(tr.VSegs) != 3 {
		t.Fatalf("%d legs, %d vertical segments", len(tr.Legs), len(tr.VSegs))
	}
	for i := 1; i < len(tr.Legs); i++ {
		prev, l := tr.Legs[i-1], tr.Legs[i]
		end := prev.Pos(prev.T1)
		if dx, dy := fx.Abs(l.P0.X*fx.M-end.X), fx.Abs(l.P0.Y*fx.M-end.Y); dx > fx.M || dy > fx.M {
			t.Errorf("leg %d starts %d, %d pm from the end of the leg before", i, dx, dy)
		}
	}
	// The path stays within a few millimetres of the path in floating point.
	for s := fl.Start(); s <= fl.End(); s += 7.3 {
		want, got := fl.PosAt(s), PM(tr.Pos(Time(s)))
		if d := want.Dist(got); d > 1e-5 {
			t.Fatalf("at %v s the paths are %v nm apart", s, d)
		}
		if a, b := fl.AltAt(s), Ft(tr.AltAt(Time(s))); math.Abs(a-b) > 0.01 {
			t.Fatalf("at %v s the altitudes are %v and %v ft", s, a, b)
		}
	}
	// And it comes back.
	back := Float(tr)
	if err := back.Validate(); err != nil {
		t.Fatal(err)
	}
	if d := back.PosAt(fl.End()).Dist(fl.PosAt(fl.End())); d > 1e-5 {
		t.Fatalf("the end moved by %v nm", d)
	}
}

// A trajectory with a gap keeps its gap, and the kernel rejects it. Traj does
// not repair what a planner got wrong.
func TestGapsAreKept(t *testing.T) {
	for name, change := range map[string]func(tr *ftraj.Trajectory){
		"a jump in position": func(tr *ftraj.Trajectory) { tr.Legs[1].P0.X += 0.5 },
		"a gap in time":      func(tr *ftraj.Trajectory) { tr.Legs[1].T0 += 2 },
		"a step in altitude": func(tr *ftraj.Trajectory) { tr.VSegs[2].A0 += 100 },
		"a short profile":    func(tr *ftraj.Trajectory) { tr.VSegs[2].T1 -= 5 },
	} {
		fl := route()
		change(fl)
		if fl.Validate() == nil {
			t.Fatalf("%s: the floating-point trajectory is valid, so the case tests nothing", name)
		}
		if err := Traj(fl).Validate(); err == nil {
			t.Errorf("%s: the converted trajectory is valid", name)
		}
	}
	// A tail that starts a little way from where it should keeps its start.
	fl := route()
	fl.Legs[0].P0.X += 3
	if got := Traj(fl).Legs[0].P0; got != Point(fl.Legs[0].P0) {
		t.Errorf("the start of the first leg moved: %+v", got)
	}
}
