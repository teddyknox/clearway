// Command atcsim runs the fast-time simulation of one AutoATC cell.
//
//	atcsim                         one run with default settings
//	atcsim -planner chaos -hours 12
//	atcsim -exp planners           a named set of runs, printed as a table
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"autoatc/internal/geom"
	"autoatc/internal/sim"
)

func main() {
	cfg := sim.DefaultConfig()
	hours := flag.Float64("hours", cfg.Duration/3600, "simulated hours")
	kKt := flag.Float64("k", cfg.Kernel.K/geom.Kt, "growth of along-track uncertainty (kt)")
	flag.Uint64Var(&cfg.Seed, "seed", cfg.Seed, "random seed")
	flag.Float64Var(&cfg.FlightsPerHour, "rate", cfg.FlightsPerHour, "flights per hour")
	flag.StringVar(&cfg.Planner, "planner", cfg.Planner, "heuristic, chaos, or none")
	flag.Float64Var(&cfg.ChaosRate, "chaos-rate", cfg.ChaosRate, "unsolicited chaos proposals per 10 s")
	flag.StringVar(&cfg.Verifier, "verifier", cfg.Verifier, "dual, single, broken-single, or broken-dual")
	flag.BoolVar(&cfg.Fallback, "fallback", cfg.Fallback, "run the Fallback Resolver")
	flag.BoolVar(&cfg.SafetyNet, "safetynet", cfg.SafetyNet, "let the Safety Net issue instructions")
	flag.Float64Var(&cfg.NetLookAhead, "net-lookahead", cfg.NetLookAhead, "Safety Net prediction time (s)")
	flag.BoolVar(&cfg.Kernel.IntentCheck, "intent", cfg.Kernel.IntentCheck, "Monitor checks broadcast selected altitude")
	flag.Float64Var(&cfg.PBlunder, "blunder", cfg.PBlunder, "probability per flight of leaving the route")
	flag.Float64Var(&cfg.PLevelBust, "bust", cfg.PLevelBust, "probability per flight of leaving the level")
	flag.Float64Var(&cfg.PNordo, "nordo", cfg.PNordo, "probability per flight of receiving no clearance")
	asJSON := flag.Bool("json", false, "print the report as JSON")
	exp := flag.String("exp", "", "run a named experiment: planners, verifiers, faults, net, growth, density, all")
	seeds := flag.Int("seeds", 3, "seeds per experiment row")
	flag.Parse()
	cfg.Duration = *hours * 3600
	cfg.Kernel.K = *kKt * geom.Kt

	if *exp != "" {
		if err := runExperiments(*exp, cfg, *seeds); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	start := time.Now()
	rep, err := sim.Run(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	rep.Wall = time.Since(start)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
		return
	}
	fmt.Print(rep)
	if rep.LossKernel > 0 {
		os.Exit(2)
	}
}
