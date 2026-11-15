package traj

import (
	"math/big"
	"strings"
	"testing"

	"github.com/teddyknox/clearway/internal/fx"
)

// good returns a valid trajectory: two legs with a turn, and a climb.
func good() *Trajectory {
	const v = 250_000_000 // µm/s: 250 m/s
	return &Trajectory{
		Legs: []Leg{
			{T0: 0, T1: 100 * fx.Sec, P0: fx.Vec{X: 0, Y: 0}, V: fx.Vec{X: v}},
			{T0: 100 * fx.Sec, T1: 300 * fx.Sec, P0: fx.Vec{X: 100 * v, Y: 0}, V: fx.Vec{Y: v}},
		},
		VSegs: []VSeg{
			{T0: 0, T1: 50 * fx.Sec, A0: 35000 * fx.FT, A1: 35000 * fx.FT},
			{T0: 50 * fx.Sec, T1: 170 * fx.Sec, A0: 35000 * fx.FT, A1: 37000 * fx.FT},
			{T0: 170 * fx.Sec, T1: 300 * fx.Sec, A0: 37000 * fx.FT, A1: 37000 * fx.FT},
		},
	}
}

// Validate accepts a valid trajectory and names each kind of defect.
func TestValidate(t *testing.T) {
	if err := good().Validate(); err != nil {
		t.Fatal(err)
	}
	var none *Trajectory
	if none.Validate() == nil || (&Trajectory{}).Validate() == nil {
		t.Fatal("an empty trajectory is valid")
	}
	many := good()
	for len(many.VSegs) <= MaxParts {
		last := many.VSegs[len(many.VSegs)-1]
		many.VSegs[len(many.VSegs)-1].T1 = last.T0 + 1
		many.VSegs = append(many.VSegs, VSeg{T0: last.T0 + 1, T1: last.T1, A0: last.A1, A1: last.A1})
	}
	for name, c := range map[string]struct {
		change func(tr *Trajectory)
		want   string
	}{
		"a leg that ends before it starts":    {func(tr *Trajectory) { tr.Legs[0].T1 = -1 }, "leg 0 malformed"},
		"a leg with no duration":              {func(tr *Trajectory) { tr.Legs[0].T1 = 0 }, "leg 0 malformed"},
		"a gap in time between legs":          {func(tr *Trajectory) { tr.Legs[1].T0++ }, "leg 1 time gap"},
		"a jump in position between legs":     {func(tr *Trajectory) { tr.Legs[1].P0.Y = Join + 1 }, "leg 1 position jump"},
		"a time outside the limit":            {func(tr *Trajectory) { tr.Legs[1].T1 = fx.MaxTime + 1 }, "leg 1 malformed"},
		"a coordinate outside the limit":      {func(tr *Trajectory) { tr.Legs[0].P0.X = -fx.MaxCoord - 1 }, "leg 0 malformed"},
		"a speed outside the limit":           {func(tr *Trajectory) { tr.Legs[0].V.Y = fx.MaxSpeed + 1 }, "leg 0 malformed"},
		"a leg that leaves the valid area":    {func(tr *Trajectory) { tr.Legs[1].P0.Y = fx.MaxCoord - 1; tr.Legs[1].V.Y = fx.MaxSpeed }, "leg 1 leaves the valid area"},
		"a vertical segment with no time":     {func(tr *Trajectory) { tr.VSegs[1].T1 = tr.VSegs[1].T0 }, "vseg 1 malformed"},
		"a gap in time in the profile":        {func(tr *Trajectory) { tr.VSegs[1].T0-- }, "vseg 1 time gap"},
		"a step in altitude":                  {func(tr *Trajectory) { tr.VSegs[2].A0++ }, "vseg 2 altitude jump"},
		"an altitude outside the limit":       {func(tr *Trajectory) { tr.VSegs[0].A0 = fx.MaxCoord + 1 }, "vseg 0 malformed"},
		"a profile shorter than the legs":     {func(tr *Trajectory) { tr.VSegs[2].T1-- }, "does not span"},
		"a profile that starts after a leg":   {func(tr *Trajectory) { tr.VSegs[0].T0++ }, "does not span"},
		"more segments than a trajectory has": {func(tr *Trajectory) { *tr = *many }, "too many"},
	} {
		tr := good()
		c.change(tr)
		if err := tr.Validate(); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v, want one with %q", name, err, c.want)
		}
	}
	// A leg may start up to Join from the end of the leg before it.
	near := good()
	near.Legs[1].P0.Y = Join
	if err := near.Validate(); err != nil {
		t.Errorf("a join of exactly Join: %v", err)
	}
	// A leg that ends exactly on the limit of the valid area is valid.
	edge := &Trajectory{
		Legs:  []Leg{{T0: 0, T1: fx.Sec, P0: fx.Vec{X: fx.MaxCoord - fx.MaxSpeed}, V: fx.Vec{X: fx.MaxSpeed}}},
		VSegs: []VSeg{{T0: 0, T1: fx.Sec}},
	}
	if err := edge.Validate(); err != nil {
		t.Errorf("a leg that ends on the limit: %v", err)
	}
	edge.Legs[0].T1++
	edge.VSegs[0].T1++
	if edge.Validate() == nil {
		t.Error("a leg that ends one microsecond beyond the limit is valid")
	}
}

// A position is exact in picometres. At the time where two legs meet, the
// later leg gives it. A time outside the trajectory maps to its nearest end.
func TestPosition(t *testing.T) {
	tr := good()
	tr.Legs[1].P0.Y = 7 // within Join of the end of leg 0, and not on it
	const v = 250_000_000
	for _, c := range []struct {
		t    int64
		want fx.Vec
	}{
		{0, fx.Vec{}},
		{1, fx.Vec{X: v}}, // one microsecond at v µm/s is v pm
		{100*fx.Sec - 1, fx.Vec{X: 100*v*fx.M - v}}, // the last time of leg 0
		{100 * fx.Sec, fx.Vec{X: 100 * v * fx.M, Y: 7 * fx.M}},
		{300 * fx.Sec, fx.Vec{X: 100 * v * fx.M, Y: 7*fx.M + 200*v*fx.M}},
		{-5, fx.Vec{}},
		{900 * fx.Sec, fx.Vec{X: 100 * v * fx.M, Y: 7*fx.M + 200*v*fx.M}},
	} {
		if got := tr.Pos(c.t); got != c.want {
			t.Errorf("Pos(%d) = %+v, want %+v", c.t, got, c.want)
		}
	}
	if tr.VelAt(100*fx.Sec) != (fx.Vec{Y: v}) || tr.VelAt(100*fx.Sec-1) != (fx.Vec{X: v}) {
		t.Error("VelAt at the join")
	}
}

// An altitude inside a transition is linear and rounded down.
func TestAltitude(t *testing.T) {
	tr := good()
	lo, hi := int64(35000*fx.FT), int64(37000*fx.FT)
	for at := int64(50 * fx.Sec); at <= 170*fx.Sec; at += 7_777_777 {
		// floor(lo + (hi − lo)·(at − 50 s)/120 s)
		num := new(big.Int).Mul(big.NewInt(hi-lo), big.NewInt(at-50*fx.Sec))
		want := lo + new(big.Int).Div(num, big.NewInt(120*fx.Sec)).Int64()
		if got := tr.AltAt(at); got != want {
			t.Fatalf("AltAt(%d) = %d, want %d", at, got, want)
		}
	}
	if tr.AltAt(-1) != lo || tr.AltAt(1<<40) != hi {
		t.Error("AltAt outside the trajectory")
	}
}

// The lowest speed is rounded down and the highest is rounded up, so that an
// allowance and a closing speed are never too small.
func TestSpeeds(t *testing.T) {
	tr := good()
	tr.Legs[0].V = fx.Vec{X: 3, Y: 4}  // exactly 5
	tr.Legs[1].V = fx.Vec{X: 10, Y: 1} // sqrt(101): between 10 and 11
	tr.Legs[1].P0 = fx.Vec{}
	if tr.MinSpeed() != 5 {
		t.Errorf("MinSpeed = %d", tr.MinSpeed())
	}
	if v, n := tr.SpeedOver(0, 300*fx.Sec); v != 11 || n != 2 {
		t.Errorf("SpeedOver the whole trajectory = %d over %d legs", v, n)
	}
	if v, n := tr.SpeedOver(0, 10*fx.Sec); v != 5 || n != 1 {
		t.Errorf("SpeedOver leg 0 = %d over %d legs", v, n)
	}
	if v, n := tr.SpeedOver(400*fx.Sec, 500*fx.Sec); v != 11 || n != 1 {
		t.Errorf("SpeedOver after the end = %d over %d legs", v, n)
	}
}

// Shift moves every time and nothing else. Splice keeps the base up to a time
// and continues with a tail.
func TestShiftAndSplice(t *testing.T) {
	tr := good()
	sh := tr.Shift(-30 * fx.Sec)
	if err := sh.Validate(); err != nil {
		t.Fatal(err)
	}
	if sh.Start() != -30*fx.Sec || sh.End() != 270*fx.Sec || sh.Pos(70*fx.Sec) != tr.Pos(100*fx.Sec) || sh.AltAt(70*fx.Sec) != tr.AltAt(100*fx.Sec) {
		t.Fatal("Shift changed the path")
	}

	at := int64(200 * fx.Sec) // on leg 1, in level flight at 37,000 ft
	p := tr.Pos(at)
	tail := &Trajectory{
		Legs:  []Leg{{T0: at, T1: at + 60*fx.Sec, P0: fx.Vec{X: p.X / fx.M, Y: p.Y / fx.M}, V: fx.Vec{X: -1000, Y: 2000}}},
		VSegs: []VSeg{{T0: at, T1: at + 60*fx.Sec, A0: 37000 * fx.FT, A1: 36000 * fx.FT}},
	}
	out, err := Splice(tr, at, tail)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Legs) != 3 || len(out.VSegs) != 4 || out.End() != at+60*fx.Sec || out.Legs[1].T1 != at || out.VSegs[2].T1 != at {
		t.Fatalf("spliced: %+v", out)
	}
	if out.Pos(at-1) != tr.Pos(at-1) || out.Pos(at+fx.Sec) != tail.Pos(at+fx.Sec) {
		t.Fatal("the spliced trajectory does not follow the base and then the tail")
	}
	// A tail that starts somewhere else, or at another level, or at another
	// time, gives no trajectory.
	off := *tail
	off.Legs = []Leg{tail.Legs[0]}
	off.Legs[0].P0.X += Join + 1
	if _, err := Splice(tr, at, &off); err == nil {
		t.Error("a tail that does not connect was spliced")
	}
	lvl := *tail
	lvl.VSegs = []VSeg{{T0: at, T1: at + 60*fx.Sec, A0: 36000 * fx.FT, A1: 36000 * fx.FT}}
	if _, err := Splice(tr, at, &lvl); err == nil {
		t.Error("a tail at another level was spliced")
	}
	for _, when := range []int64{tr.Start(), tr.End(), at + 1} {
		if _, err := Splice(tr, when, tail); err == nil {
			t.Errorf("a splice at %d was accepted", when)
		}
	}
	// A cut inside a transition ends the cut segment at the altitude there.
	mid := int64(110 * fx.Sec)
	p = tr.Pos(mid)
	climb := &Trajectory{
		Legs:  []Leg{{T0: mid, T1: mid + fx.Sec, P0: fx.Vec{X: p.X / fx.M, Y: p.Y / fx.M}, V: fx.Vec{}}},
		VSegs: []VSeg{{T0: mid, T1: mid + fx.Sec, A0: tr.AltAt(mid), A1: tr.AltAt(mid)}},
	}
	if out, err := Splice(tr, mid, climb); err != nil || out.VSegs[1].A1 != tr.AltAt(mid) {
		t.Errorf("a splice inside a transition: %v", err)
	}
}
