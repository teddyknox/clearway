package sim

import (
	"encoding/json"
	"testing"
)

// A recording must agree with the report of the same run, and making it must
// not change the run.
func TestTraceAgreesWithTheRun(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Duration = 3600
	cfg.Planner = "chaos"
	cfg.PBlunder, cfg.PLevelBust, cfg.PNordo = 0.05, 0.05, 0.03
	cfg.SafetyNet = false

	rec, rep, err := Trace(cfg)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := Run(cfg)
	if plain.Digest != rep.Digest || plain.ClearancesSent != rep.ClearancesSent || plain.Losses() != rep.Losses() {
		t.Fatal("tracing changed the run")
	}

	if want := int(cfg.Duration / rec.Meta.FrameEvery); len(rec.Frames) != want {
		t.Fatalf("%d frames, want %d", len(rec.Frames), want)
	}
	count := func(name string) int {
		for i, n := range rec.Meta.Counters {
			if n == name {
				return rec.Frames[len(rec.Frames)-1].Counts[i]
			}
		}
		t.Fatalf("no counter %q", name)
		return 0
	}
	if count("commits") != int(rep.Kernel.Commits) || count("proposals") != int(rep.Kernel.Proposals) ||
		count("lossIntruder") != rep.LossIntruder || count("acked") != rep.ClearancesAcked {
		t.Error("the last frame's counters differ from the report")
	}

	kinds := map[string]int{}
	last := 0.0
	for _, e := range rec.Events {
		if e.T < last {
			t.Fatalf("events out of order at %.0f", e.T)
		}
		last = e.T
		kinds[e.Kind]++
		if e.Kind == "propose" && len(e.Pts) < 2 {
			t.Fatal("a proposal without a path")
		}
	}
	if kinds["loss"] != rep.Losses() {
		t.Errorf("%d loss events, %d losses in the report", kinds["loss"], rep.Losses())
	}
	if kinds["ack"] != rep.ClearancesAcked || kinds["fail"] != rep.ClearancesFailed {
		t.Errorf("acknowledgements %d/%d, failures %d/%d", kinds["ack"], rep.ClearancesAcked, kinds["fail"], rep.ClearancesFailed)
	}
	for _, k := range []string{"admit", "propose", "fault", "intruder", "recontracted"} {
		if kinds[k] == 0 {
			t.Errorf("no %q event in a run that must have some", k)
		}
	}

	cols := len(rec.Meta.Aircraft)
	for _, f := range rec.Frames {
		if len(f.Counts) != len(rec.Meta.Counters) {
			t.Fatal("counter row has the wrong length")
		}
		for _, row := range f.Aircraft {
			if len(row) != cols {
				t.Fatal("aircraft row has the wrong length")
			}
			for _, pi := range []float64{row[8], row[9]} {
				if pi < -1 || int(pi) >= len(rec.Paths) {
					t.Fatalf("path index %v out of range", pi)
				}
			}
		}
	}
	// Re-anchoring makes a new contract every minute. Paths must not grow
	// with it: contracts that differ only in timing share one path.
	if len(rec.Paths) > 3*int(rep.Kernel.Commits) {
		t.Errorf("%d paths for %d commits", len(rec.Paths), rep.Kernel.Commits)
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d frames, %d paths, %d events, %.1f MB", len(rec.Frames), len(rec.Paths), len(rec.Events), float64(len(b))/1e6)
}
