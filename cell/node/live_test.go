package node

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/teddyknox/clearway/cell/client"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/record"
	"github.com/teddyknox/clearway/internal/sim"
)

func liveConfig() sim.Config {
	cfg := sim.DefaultConfig()
	cfg.Duration = 1200
	cfg.FlightsPerHour = 240
	cfg.Planner = "heuristic"
	return cfg
}

// liveCell starts a cell of four replicas for a simulation with cfg, and
// returns a gate to it.
func liveCell(t *testing.T, cfg sim.Config, opt Options) ([]*Replica, *client.Gate) {
	t.Helper()
	header, err := record.HeaderLine(sim.RecordHeader(cfg))
	if err != nil {
		t.Fatal(err)
	}
	_, rs := cell(t, header, 60_000_000, opt, opt, opt, opt)
	trust, err := client.TrustFromGenesis(rs[0].GenesisFile())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)
	return rs, client.NewGate(ctx, rs[0].Client(), trust, time.Second)
}

// onVerdict calls f before the gate takes verdict number at.
type onVerdict struct {
	sim.Gate
	at, seen int
	f        func()
}

func (g *onVerdict) Verdict(v kernel.Verdict) {
	if g.seen++; g.seen == g.at {
		g.f()
	}
	g.Gate.Verdict(v)
}

// The simulator runs with a cell of four replicas before its kernel. At
// each proposal it waits for the verdict of the cell, with a certificate
// that passes the check. The run then has the result of a run with no cell:
// the same report, and the same record to the last byte. At each checkpoint
// the cell has the state of the kernel of the simulator.
//
// One replica stops in the middle of the run, and the result is the same.
// The test prints how long the simulation waited for a verdict.
func TestSimulatorThroughTheCell(t *testing.T) {
	cfg := liveConfig()
	var plain bytes.Buffer
	want, err := sim.RunRecord(cfg, &plain)
	if err != nil {
		t.Fatal(err)
	}
	rs, gate := liveCell(t, cfg, Options{})
	stopped := false
	var live bytes.Buffer
	began := time.Now()
	got, err := sim.RunThrough(cfg, &onVerdict{Gate: gate, at: 8, f: func() {
		stopped = rs[3].Stop() == nil
	}}, &live)
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(began)
	if !stopped {
		t.Fatalf("the run had %d verdicts, so no replica stopped", gate.Waits().Verdicts)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the report of the run through the cell differs:\n%+v\nwith no cell:\n%+v", got, want)
	}
	if !bytes.Equal(live.Bytes(), plain.Bytes()) {
		t.Error("the record of the run through the cell is not the record of the run with no cell")
	}
	w := gate.Waits()
	// The test does not fail for a slow verdict: the time depends on what
	// else uses the disk. docs/replication.md has the measured times.
	if w.Verdicts < 10 {
		t.Errorf("waits: %+v", w)
	}
	t.Logf("%g s of traffic through the cell in %v: %d batches, %d verdicts; a verdict took %v (median), %v (99%%), %v (longest); %d over 1 s",
		cfg.Duration, took.Round(time.Millisecond), w.Batches, w.Verdicts, w.Median.Round(time.Millisecond), w.P99.Round(time.Millisecond), w.Max.Round(time.Millisecond), w.Late)
}

// A cell with two replicas of four gives no verdict. The simulation then
// stops at the proposal that waits: no part of it goes on without the cell.
func TestSimulatorStopsWithTheCell(t *testing.T) {
	cfg := liveConfig()
	rs, gate := liveCell(t, cfg, Options{CommitTimeout: 2 * time.Second})
	var live bytes.Buffer
	_, err := sim.RunThrough(cfg, &onVerdict{Gate: gate, at: 6, f: func() {
		if rs[2].Stop() != nil || rs[3].Stop() != nil {
			t.Error("the replicas did not stop")
		}
	}}, &live)
	if err == nil {
		t.Fatal("the simulation went on with two of four replicas")
	}
	if w := gate.Waits(); w.Verdicts != 5 {
		t.Fatalf("the simulation took %d verdicts; the cell lost its quorum before verdict 6", w.Verdicts)
	}
	t.Logf("the run stopped: %v", err)
}
