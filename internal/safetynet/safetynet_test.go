package safetynet

import (
	"math"
	"testing"
)

const kt = 1.0 / 3600

func tr(id int, x, y, vx, vy, alt float64) Track {
	return Track{ID: id, X: x, Y: y, VX: vx * kt, VY: vy * kt, Alt: alt, SelAlt: math.NaN()}
}

func TestHeadOnAlertAndInstruction(t *testing.T) {
	n := New(DefaultParams())
	// 20 nm apart, closing at 960 kt: inside 5 nm in about 56 s.
	a, b := tr(1, -10, 0, 480, 0, 35000), tr(2, 10, 0, -480, 0, 35000)
	al := n.Step(0, []Track{a, b})
	if len(al) != 1 || !al[0].Instruct {
		t.Fatalf("alerts: %+v", al)
	}
	if al[0].TimeToLoss < 50 || al[0].TimeToLoss > 60 {
		t.Errorf("time to loss %.0f s", al[0].TimeToLoss)
	}
	// Both level: the higher identifier moves, 1,000 ft away.
	if al[0].Flight != 2 || al[0].TargetAlt != 36000 {
		t.Errorf("instruction: flight %d to %.0f", al[0].Flight, al[0].TargetAlt)
	}
	// The same pair inside the cooldown gets an alert and no new instruction.
	if al = n.Step(5, []Track{a, b}); len(al) != 1 || al[0].Instruct {
		t.Fatalf("inside cooldown: %+v", al)
	}
}

func TestNoAlertWhenSeparated(t *testing.T) {
	n := New(DefaultParams())
	a := tr(1, -10, 0, 480, 0, 35000)
	for name, b := range map[string]Track{
		"1,000 ft above":    tr(2, 10, 0, -480, 0, 36000),
		"8 nm abeam":        tr(2, 10, 8, -480, 0, 35000),
		"diverging":         tr(2, -20, 0, -480, 0, 35000),
		"far away":          tr(2, 80, 0, -480, 0, 35000),
		"crossing, far off": tr(2, 0, -60, 0, 480, 35000),
	} {
		if al := n.Step(0, []Track{a, b}); len(al) != 0 {
			t.Errorf("%s: unexpected alert %+v", name, al)
		}
	}
}

func TestSelectedAltitudeStopsAClimb(t *testing.T) {
	n := New(DefaultParams())
	level := tr(1, -10, 0, 480, 0, 36000)
	climber := tr(2, 10, 0, -480, 0, 34200)
	climber.VRate = 30
	// With no selected altitude the climb runs into the level above.
	if al := n.Step(0, []Track{level, climber}); len(al) != 1 {
		t.Fatalf("climb through a level not predicted: %+v", al)
	} else if al[0].Flight != 2 || al[0].TargetAlt != 35000 {
		// The climbing aircraft is the one to stop, below the other.
		t.Errorf("instruction: flight %d to %.0f", al[0].Flight, al[0].TargetAlt)
	}
	// With 35,000 ft selected, the aircraft levels off 1,000 ft below.
	climber.SelAlt = 35000
	if al := New(DefaultParams()).Step(0, []Track{level, climber}); len(al) != 0 {
		t.Fatalf("level-off not recognized: %+v", al)
	}
}

func TestThirdAircraftChangesTheChoice(t *testing.T) {
	n := New(DefaultParams())
	a, b := tr(1, -10, 0, 480, 0, 35000), tr(2, 10, 0, -480, 0, 35000)
	// A third aircraft sits right above flight 2's escape level.
	c := tr(3, 6, 0, -480, 0, 36000)
	al := n.Step(0, []Track{a, b, c})
	var got *Alert
	for i := range al {
		if al[i].A == 1 && al[i].B == 2 {
			got = &al[i]
		}
	}
	if got == nil || !got.Instruct {
		t.Fatalf("alerts: %+v", al)
	}
	if got.Flight == 2 && got.TargetAlt == 36000 {
		t.Errorf("flight 2 was sent up into flight 3")
	}
}
