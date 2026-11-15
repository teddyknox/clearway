package sim

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"os"
	"strings"
	"testing"

	"github.com/teddyknox/clearway/internal/conv"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/record"
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

// testKey is a signing key for tests. It protects nothing.
var testKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))

func trust(k ed25519.PrivateKey) record.Options {
	return record.Options{Trusted: []ed25519.PublicKey{k.Public().(ed25519.PublicKey)}}
}

// One seed gives one result, down to every byte of the kernel's record. The
// record passes every check: the chain, the signatures, and a replay that
// reproduces every verdict and every state digest.
func TestDeterminismAndReplay(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Duration = 3600
	cfg.Planner = "chaos"
	cfg.PBlunder, cfg.PLevelBust = 0.03, 0.03
	var rec, again bytes.Buffer
	a, err := RunRecord(cfg, &rec, testKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RunRecord(cfg, &again, testKey); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rec.Bytes(), again.Bytes()) {
		t.Fatal("two runs with one seed wrote different records")
	}
	b, _ := Run(cfg)
	if a.Digest != b.Digest || a.ClearancesSent != b.ClearancesSent || a.Completed != b.Completed {
		t.Fatal("a recorded run and a plain run with one seed differ")
	}

	rep, err := record.Verify(bytes.NewReader(rec.Bytes()), trust(testKey))
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() || !rep.ReplayDone {
		t.Fatalf("the record does not pass: %v", rep.Failures)
	}
	var rejects uint64
	for _, n := range a.Kernel.Rejects {
		rejects += n
	}
	if uint64(rep.Commits) != a.Kernel.Commits || uint64(rep.Rejects) != rejects || rep.Commits == 0 || rep.Rejects == 0 {
		t.Fatalf("record: %d commits, %d rejects; kernel: %d commits, %d rejects", rep.Commits, rep.Rejects, a.Kernel.Commits, rejects)
	}
	if rep.Checkpoints != 60 || rep.Tail != 0 || rep.Unsigned != 0 || rep.End != conv.Time(cfg.Duration) {
		t.Fatalf("%d checkpoints, %d lines after the last, %d unsigned, end at %d µs", rep.Checkpoints, rep.Tail, rep.Unsigned, rep.End)
	}
	if hex.EncodeToString(lastState(t, rec.Bytes())) != a.Digest {
		t.Fatal("the last checkpoint does not state the final state of the kernel")
	}

	cfg.Seed = 2
	if c, _ := Run(cfg); c.Digest == a.Digest {
		t.Fatal("two seeds gave one result")
	}
	t.Logf("%d lines, %d bytes, %d commits, %d rejects, %d checkpoints", rep.Lines, rec.Len(), rep.Commits, rep.Rejects, rep.Checkpoints)
}

func lastState(t *testing.T, rec []byte) []byte {
	t.Helper()
	cps, err := record.ReadCheckpoints(bytes.NewReader(rec))
	if err != nil || len(cps) == 0 {
		t.Fatalf("checkpoints: %d, %v", len(cps), err)
	}
	return cps[len(cps)-1].State[:]
}

var update = flag.Bool("update", false, "write the kernel vector of this package again")

// trafficVector is a published record of half an hour of dense traffic under
// the hostile planner: a kernel vector (conformance/README.md). An arm64
// machine wrote it.
const trafficVector = "../../conformance/kernel-traffic-0.2.rec.gz"

func trafficVectorConfig() Config {
	cfg := DefaultConfig()
	cfg.Duration = 1800
	cfg.FlightsPerHour = 240
	cfg.Planner = "chaos"
	cfg.PBlunder, cfg.PLevelBust = 0.05, 0.05
	return cfg
}

// The kernel must compute the same bits on every machine (specification,
// section 1). This machine must replay a record that another machine wrote:
// every verdict with its conflict time, and every state digest.
//
// Before the rule for arithmetic, a replay of this traffic on another
// processor type failed at the checkpoint for the fourth minute.
func TestKernelVectorTraffic(t *testing.T) {
	if *update {
		var buf bytes.Buffer
		z, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if _, err := RunRecord(trafficVectorConfig(), z); err != nil {
			t.Fatal(err)
		}
		if err := z.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(trafficVector, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(trafficVector)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := record.Verify(z, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OK() || !rep.ReplayDone {
		t.Fatalf("this machine does not replay the vector: %v\n"+
			"If you changed what the kernel does, on purpose, write the vector again:\n"+
			"  go test ./internal/sim -run TestKernelVectorTraffic -update\n"+
			"If you did not, the kernel computes other bits on this machine than on the one that wrote the vector.", rep.Failures)
	}
	if rep.Commits < 150 || rep.Rejects < 150 || rep.Checkpoints != 30 || rep.Tail != 0 {
		t.Fatalf("a weak vector: %d commits, %d rejects, %d checkpoints, tail %d", rep.Commits, rep.Rejects, rep.Checkpoints, rep.Tail)
	}
	t.Logf("%d lines, %d commits, %d rejects", rep.Lines, rep.Commits, rep.Rejects)
}

// A kernel with a faulty Verifier writes a record that is consistent and
// correctly signed. A replay with the public implementations finds the first
// clearance that the kernel approved and that the specification refuses.
func TestReplayCatchesFaultyVerifier(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Duration = 4 * 3600
	cfg.SafetyNet = false
	cfg.Verifier = "broken-single"
	var rec bytes.Buffer
	sim, err := RunRecord(cfg, &rec, testKey)
	if err != nil {
		t.Fatal(err)
	}
	opt := trust(testKey)
	rep, err := record.Verify(bytes.NewReader(rec.Bytes()), opt)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ReplayDone || len(rep.Failures) == 0 {
		t.Fatal("the replay found nothing")
	}
	last := rep.Failures[len(rep.Failures)-1]
	for _, f := range rep.Failures {
		t.Log(f)
		if f.Class != record.Replay {
			t.Fatalf("a failure that is not of the replay: %v", f)
		}
	}
	msg := last.Msg
	if !strings.Contains(msg, `the record says "committed`) || !strings.Contains(msg, `the replay says "rejected: conflict`) {
		t.Fatalf("the replay does not name an unsafe approval: %s", msg)
	}
	// Without the replay, nothing is wrong with the record.
	opt.NoReplay = true
	if rep, _ := record.Verify(bytes.NewReader(rec.Bytes()), opt); !rep.OK() {
		t.Fatalf("chain and signatures: %v", rep.Failures)
	}
	t.Logf("%d kernel-class losses in the run: %v", sim.LossKernel, sim.LossDetail)
}
