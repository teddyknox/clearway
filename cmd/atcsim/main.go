// Command atcsim runs the fast-time simulation of one Clearway cell.
//
//	atcsim                         one run with default settings
//	atcsim -planner chaos -hours 12
//	atcsim -exp planners           a named set of runs, printed as a table
//	atcsim -record run.rec -key cell.key
//	                               also write the record of the kernel, signed
package main

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/teddyknox/clearway/internal/conv"
	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/record"
	"github.com/teddyknox/clearway/internal/sim"
)

func main() {
	cfg := sim.DefaultConfig()
	hours := flag.Float64("hours", cfg.Duration/3600, "simulated hours")
	// The kernel holds K in whole micrometres per second. Show it in knots,
	// to a thousandth: that converts back to the same K.
	kKt := flag.Float64("k", math.Round(conv.NMps(cfg.Kernel.K)/geom.Kt*1000)/1000, "growth of along-track uncertainty (kt)")
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
	recPath := flag.String("record", "", "write the record of the kernel to this file (docs/record.md)")
	keyPath := flag.String("key", "", "with -record: sign each checkpoint with the private key in this file")
	flag.Parse()
	cfg.Duration = *hours * 3600
	cfg.Kernel.K = conv.Speed(*kKt * geom.Kt)

	if *exp != "" {
		if err := runExperiments(*exp, cfg, *seeds); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	start := time.Now()
	rep, err := runOnce(cfg, *recPath, *keyPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "atcsim:", err)
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

// runOnce executes one run. If recPath is not empty, it writes the record of
// the kernel there, and signs each checkpoint with the key in keyPath, if any.
func runOnce(cfg sim.Config, recPath, keyPath string) (*sim.Report, error) {
	if recPath == "" {
		if keyPath != "" {
			return nil, errors.New("-key needs -record")
		}
		return sim.Run(cfg)
	}
	var signers []ed25519.PrivateKey
	if keyPath != "" {
		text, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, err
		}
		key, err := record.ParsePrivateKey(string(text))
		if err != nil {
			return nil, fmt.Errorf("%s: %v", keyPath, err)
		}
		signers = append(signers, key)
	}
	f, err := os.Create(recPath)
	if err != nil {
		return nil, err
	}
	rep, err := sim.RunRecord(cfg, f, signers...)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return rep, err
}
