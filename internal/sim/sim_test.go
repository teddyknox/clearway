package sim

import (
	"testing"

	"autoatc/internal/kernel"
)

func run(t *testing.T, mut func(*Config)) *Report {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Duration = 2 * 3600
	mut(&cfg)
	rep, err := Run(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rep.SimInconsistencies != 0 {
		t.Fatalf("%d simulator inconsistencies", rep.SimInconsistencies)
	}
	return rep
}

// The central claim of the design: whatever the planner proposes, two
// aircraft that conform to verified contracts do not lose separation.
func TestKernelHoldsUnderEveryPlanner(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
	}{
		{"heuristic", func(c *Config) {}},
		{"none", func(c *Config) { c.Planner = "none" }},
		{"chaos", func(c *Config) { c.Planner = "chaos" }},
		// With no Fallback Resolver and no Safety Net instructions, nothing
		// but the kernel stands between the chaos planner and the traffic.
		{"chaos alone", func(c *Config) { c.Planner = "chaos"; c.Fallback = false; c.SafetyNet = false }},
		{"dense", func(c *Config) { c.FlightsPerHour = 240 }},
	}
	for _, tc := range cases {
		for seed := uint64(1); seed <= 2; seed++ {
			rep := run(t, func(c *Config) { tc.mut(c); c.Seed = seed })
			if rep.LossKernel != 0 {
				t.Errorf("%s seed %d: %d kernel-class losses: %v", tc.name, seed, rep.LossKernel, rep.LossDetail)
			}
			if rep.Hard != 0 {
				t.Errorf("%s seed %d: %d hard disagreements between correct implementations", tc.name, seed, rep.Hard)
			}
			if rep.Completed < 80 {
				t.Errorf("%s seed %d: only %d flights completed", tc.name, seed, rep.Completed)
			}
		}
	}
}

// The chaos planner must actually be hostile, and the Verifier must reject it
// for each of the reasons it is built to reject.
func TestChaosPlannerIsRejectedForEveryReason(t *testing.T) {
	rep := run(t, func(c *Config) { c.Planner = "chaos"; c.Fallback = false; c.SafetyNet = false })
	ch := rep.Sources["chaos"]
	if ch.Proposed < 1000 || ch.Committed == 0 || ch.Committed > ch.Proposed*3/4 {
		t.Fatalf("chaos proposals: %d proposed, %d committed", ch.Proposed, ch.Committed)
	}
	for _, r := range []kernel.RejectReason{kernel.RejConflict, kernel.RejOpen, kernel.RejMalformed, kernel.RejLead} {
		if rep.Kernel.Rejects[r] == 0 {
			t.Errorf("no rejection for reason %s", r)
		}
	}
	if rep.Losses() != 0 {
		t.Errorf("losses with chaos planner: %v", rep.LossDetail)
	}
}

// Aircraft that leave their contracts cause losses of the intruder class at
// worst. They never cause a kernel-class loss.
func TestAircraftFaults(t *testing.T) {
	faults := func(c *Config) { c.Duration = 4 * 3600; c.PBlunder, c.PLevelBust, c.PNordo = 0.03, 0.03, 0.02 }
	rep := run(t, faults)
	if rep.LossKernel != 0 {
		t.Fatalf("kernel-class losses: %v", rep.LossDetail)
	}
	ex := rep.Kernel.Exits
	if ex[kernel.ExitHorizontal] == 0 || ex[kernel.ExitIntent] == 0 {
		t.Fatalf("faults not detected: exits %v", ex)
	}
	if rep.Kernel.RecontractOK == 0 {
		t.Fatal("no intruder was brought back under contract")
	}
	// Without the selected-altitude check, level busts are found only after
	// the aircraft has left its level.
	late := run(t, func(c *Config) { faults(c); c.Kernel.IntentCheck = false })
	if late.Kernel.Exits[kernel.ExitIntent] != 0 || late.Kernel.Exits[kernel.ExitVertical] == 0 {
		t.Fatalf("exits without intent check: %v", late.Kernel.Exits)
	}
	if late.LossKernel != 0 {
		t.Fatalf("kernel-class losses without intent check: %v", late.LossDetail)
	}
}

// When a clearance gets no answer, the kernel does not know which contract
// the aircraft flies. It protects both, and the Monitor settles the question
// from the track: in favour of the old contract when the clearance never
// arrived, and of the new one when only the reply was lost.
func TestLostCommunications(t *testing.T) {
	rep := run(t, func(c *Config) {
		c.Duration = 4 * 3600
		c.PNordo, c.PLostUplink, c.PLostReply = 0.15, 0.1, 0.3
	})
	if rep.ClearancesFailed == 0 || rep.ClearancesRepeated == 0 {
		t.Fatalf("clearances: %d failed, %d repeated", rep.ClearancesFailed, rep.ClearancesRepeated)
	}
	if rep.Kernel.UnknownDropped == 0 || rep.Kernel.UnknownPromoted == 0 {
		t.Fatalf("lost acknowledgements settled: %d old, %d new", rep.Kernel.UnknownDropped, rep.Kernel.UnknownPromoted)
	}
	if rep.LossKernel != 0 {
		t.Fatalf("kernel-class losses: %v", rep.LossDetail)
	}
	t.Logf("%d failed, settled %d old and %d new, losses %d", rep.ClearancesFailed,
		rep.Kernel.UnknownDropped, rep.Kernel.UnknownPromoted, rep.Losses())
}

// A Verifier implementation that misreads climbs approves unsafe contracts.
// Alone, it lets conforming aircraft lose separation. Paired with a correct
// implementation, it is outvoted, and the disagreement raises alarms.
func TestSecondImplementationCatchesFaultyVerifier(t *testing.T) {
	var alone, paired, alarms int
	for seed := uint64(1); seed <= 3; seed++ {
		base := func(c *Config) { c.Seed = seed; c.Duration = 4 * 3600; c.SafetyNet = false }
		a := run(t, func(c *Config) { base(c); c.Verifier = "broken-single" })
		p := run(t, func(c *Config) { base(c); c.Verifier = "broken-dual" })
		alone += a.LossKernel
		paired += p.LossKernel
		alarms += int(p.Hard)
	}
	if alone == 0 {
		t.Fatal("the faulty implementation caused no loss: the experiment shows nothing")
	}
	if paired != 0 {
		t.Fatalf("%d kernel-class losses with a correct second implementation", paired)
	}
	if alarms == 0 {
		t.Fatal("no alarm was raised")
	}
	t.Logf("faulty alone: %d losses; faulty with correct: %d losses, %d alarms", alone, paired, alarms)
}

// One seed gives one result. The kernel input log replays to the same state.
func TestDeterminismAndReplay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Duration = 3600
	cfg.Planner = "chaos"
	cfg.PBlunder, cfg.PLevelBust = 0.03, 0.03
	a, k, err := RunRecorded(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Run(cfg)
	if a.Digest != b.Digest || a.ClearancesSent != b.ClearancesSent || a.Completed != b.Completed {
		t.Fatal("two runs with one seed differ")
	}
	d, _ := NewVerifier(cfg.Verifier)
	if re := kernel.Replay(cfg.Kernel, d, k.Log); re.Digest() != k.Digest() {
		t.Fatal("replay of the kernel input log reached a different state")
	}
	cfg.Seed = 2
	if c, _ := Run(cfg); c.Digest == a.Digest {
		t.Fatal("two seeds gave one result")
	}
	t.Logf("%d kernel inputs replayed", len(k.Log))
}
