package main

import (
	"fmt"
	"sync"

	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/sim"
)

type row struct {
	name string
	mut  func(*sim.Config)
}

var experiments = map[string][]row{
	// Does safety depend on the quality of the planner?
	"planners": {
		{"heuristic planner", func(c *sim.Config) {}},
		{"no planner (fallback only)", func(c *sim.Config) { c.Planner = "none" }},
		{"chaos planner", func(c *sim.Config) { c.Planner = "chaos" }},
		{"chaos planner, no fallback", func(c *sim.Config) { c.Planner = "chaos"; c.Fallback = false }},
		{"chaos, no fallback, no safety net", func(c *sim.Config) { c.Planner = "chaos"; c.Fallback = false; c.SafetyNet = false }},
	},
	// What does a second Verifier implementation buy? The Safety Net is off
	// so that a Verifier fault shows as a loss.
	"verifiers": {
		{"two correct implementations", func(c *sim.Config) { c.SafetyNet = false }},
		{"one correct implementation", func(c *sim.Config) { c.SafetyNet = false; c.Verifier = "single" }},
		{"one faulty implementation", func(c *sim.Config) { c.SafetyNet = false; c.Verifier = "broken-single" }},
		{"one faulty + one correct", func(c *sim.Config) { c.SafetyNet = false; c.Verifier = "broken-dual" }},
	},
	// Aircraft that do not do what they were cleared to do.
	"faults": {
		{"no aircraft faults", func(c *sim.Config) {}},
		{"2% leave route, 2% leave level, 1% no radio", faults},
		{"same, selected altitude not checked", func(c *sim.Config) { faults(c); c.Kernel.IntentCheck = false }},
		{"same, Safety Net alerts only", func(c *sim.Config) { faults(c); c.SafetyNet = false }},
	},
	// How far ahead should the Safety Net look? It predicts from state alone,
	// so it does not know that an aircraft is about to turn.
	"net": {
		{"chaos planner, net looks 120 s ahead", func(c *sim.Config) { c.Planner = "chaos" }},
		{"chaos planner, 90 s", func(c *sim.Config) { c.Planner = "chaos"; c.NetLookAhead = 90 }},
		{"chaos planner, 60 s", func(c *sim.Config) { c.Planner = "chaos"; c.NetLookAhead = 60 }},
		{"aircraft faults, net looks 120 s ahead", faults},
		{"aircraft faults, 90 s", func(c *sim.Config) { faults(c); c.NetLookAhead = 90 }},
		{"aircraft faults, 60 s", func(c *sim.Config) { faults(c); c.NetLookAhead = 60 }},
	},
	// How well must aircraft hold their contracts?
	"growth": {
		{"K = 3 kt", func(c *sim.Config) { c.Kernel.K = 3 * geom.Kt }},
		{"K = 6 kt", func(c *sim.Config) { c.Kernel.K = 6 * geom.Kt }},
		{"K = 10 kt", func(c *sim.Config) { c.Kernel.K = 10 * geom.Kt }},
		{"K = 15 kt", func(c *sim.Config) { c.Kernel.K = 15 * geom.Kt }},
	},
	"density": {
		{"60 flights/h", func(c *sim.Config) { c.FlightsPerHour = 60 }},
		{"120 flights/h", func(c *sim.Config) { c.FlightsPerHour = 120 }},
		{"180 flights/h", func(c *sim.Config) { c.FlightsPerHour = 180 }},
		{"240 flights/h", func(c *sim.Config) { c.FlightsPerHour = 240 }},
	},
}

func faults(c *sim.Config) { c.PBlunder, c.PLevelBust, c.PNordo = 0.02, 0.02, 0.01 }

var order = []string{"planners", "verifiers", "faults", "net", "growth", "density"}

type total struct {
	hours, delay, entryDelay                float64
	completed, flown, denied, peak          int
	conflicts, to10, to5, to2               int
	lossK, lossL, lossU, lossI              int
	netCaused, netMissed                    int
	alertsV, alertsU, alertsI, instructions int
	hard, marginal, rejected, proposals     uint64
	coastMin, closest, closestV             float64
	exits                                   uint64
}

func runExperiments(name string, base sim.Config, seeds int) error {
	names := []string{name}
	if name == "all" {
		names = order
	}
	for _, n := range names {
		rows, ok := experiments[n]
		if !ok {
			return fmt.Errorf("unknown experiment %q", n)
		}
		totals := make([]total, len(rows))
		var mu sync.Mutex
		var wg sync.WaitGroup
		var firstErr error
		for i, r := range rows {
			totals[i].coastMin, totals[i].closest, totals[i].closestV = 1e18, 1e18, 1e18
			for s := 0; s < seeds; s++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					cfg := base
					cfg.Seed = base.Seed + uint64(s)
					r.mut(&cfg)
					rep, err := sim.Run(cfg)
					mu.Lock()
					defer mu.Unlock()
					if err != nil {
						firstErr = err
						return
					}
					t := &totals[i]
					t.hours += rep.FlightHours()
					t.delay += rep.TransitDelaySum
					t.entryDelay += rep.EntryDelaySum
					t.completed += rep.Completed
					t.flown += rep.AmendmentsFlown
					t.denied += rep.EntryDenied
					t.peak = max(t.peak, rep.PeakInCell)
					t.conflicts += rep.Conflicts
					t.to10 += rep.ConflictsTo10
					t.to5 += rep.ConflictsTo5
					t.to2 += rep.ConflictsTo2
					t.lossK += rep.LossKernel
					t.lossL += rep.LossLatency
					t.lossU += rep.LossUnresolved
					t.lossI += rep.LossIntruder
					t.netCaused += rep.LossNetCaused
					t.netMissed += rep.LossNetNotPrevented
					t.alertsV += rep.AlertsVerified
					t.alertsU += rep.AlertsUnresolved
					t.alertsI += rep.AlertsIntruder
					t.instructions += rep.Instructions
					t.hard += rep.Hard
					t.marginal += rep.Marginal
					t.proposals += rep.Kernel.Proposals
					t.rejected += rep.Kernel.Proposals - rep.Kernel.Commits
					t.coastMin = min(t.coastMin, rep.CoastMin)
					if rep.ClosestVerified > 0 {
						t.closestV = min(t.closestV, rep.ClosestVerified)
					}
					if rep.AlertsVerified > 0 {
						t.closest = min(t.closest, rep.VerifiedAlertClosest)
					}
					for _, e := range rep.Kernel.Exits {
						t.exits += e
					}
				}()
			}
		}
		wg.Wait()
		if firstErr != nil {
			return firstErr
		}
		fmt.Printf("\n### %s (%d seeds x %.0f h, %.0f flights/h unless stated)\n\n", n, seeds, base.Duration/3600, base.FlightsPerHour)
		fmt.Println("| Run | Flight hours | Losses: kernel / latency / unresolved / intruder | Losses the net caused / did not prevent | Net alerts: verified / unresolved / intruder | Closest after verified alert (nm) | Net instructions | Hard disagreements | Rejected / proposed | Clearances per flight | Mean delay (s) | Open at 10 / 5 / 2 min | Least coast (min) | Closest verified pair (nm) |")
		fmt.Println("|---|---|---|---|---|---|---|---|---|---|---|---|---|---|")
		for i, r := range rows {
			t := totals[i]
			n := float64(max(t.completed, 1))
			closest := "-"
			if t.alertsV > 0 && t.closest < 1e17 {
				closest = fmt.Sprintf("%.1f", t.closest)
			} else if t.alertsV > 0 {
				closest = "none inside 700 ft"
			}
			fmt.Printf("| %s | %.0f | %d / %d / %d / %d | %d / %d | %d / %d / %d | %s | %d | %d | %d / %d | %.2f | %.0f | %d / %d / %d | %.1f | %.1f |\n",
				r.name, t.hours, t.lossK, t.lossL, t.lossU, t.lossI, t.netCaused, t.netMissed, t.alertsV, t.alertsU, t.alertsI, closest, t.instructions,
				t.hard, t.rejected, t.proposals, float64(t.flown)/n, t.delay/n,
				t.to10, t.to5, t.to2, t.coastMin/60, t.closestV)
		}
	}
	return nil
}
