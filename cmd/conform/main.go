// Command conform runs the Verifier conformance suite.
//
//	conform -builtin analytic                run one of this module's implementations
//	conform -exec "python3 verifier.py"      run an implementation in another process
//	conform -generate                        rebuild the vectors (for maintainers)
//
// An implementation in another process speaks a line protocol on its standard
// input and output. conformance/README.md describes it.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/teddyknox/clearway/internal/conformance"
)

func main() {
	suitePath := flag.String("suite", "conformance/verifier-0.1.json", "the suite of vectors")
	builtin := flag.String("builtin", "", "run a built-in implementation: analytic or sampled")
	execCmd := flag.String("exec", "", "run an outside implementation: the command and its arguments")
	generate := flag.Bool("generate", false, "rebuild the suite and write it to -suite")
	seed := flag.Uint64("seed", 1, "seed for -generate")
	limit := flag.Duration("timeout", 10*time.Minute, "time limit for an outside implementation")
	show := flag.Int("show", 20, "failures to print")
	flag.Parse()

	if *generate {
		if err := writeSuite(*suitePath, *seed); err != nil {
			fail(err)
		}
		return
	}
	suite, err := conformance.Load(*suitePath)
	if err != nil {
		fail(err)
	}
	var v conformance.Verifier
	name := *builtin
	switch {
	case *builtin != "" && *execCmd != "":
		fail(fmt.Errorf("give -builtin or -exec, not both"))
	case *builtin != "":
		if v, err = conformance.BuiltinByName(*builtin); err != nil {
			fail(err)
		}
	case *execCmd != "":
		ctx, cancel := context.WithTimeout(context.Background(), *limit)
		defer cancel()
		ext, err := conformance.Start(ctx, strings.Fields(*execCmd))
		if err != nil {
			fail(err)
		}
		defer ext.Close()
		v, name = ext, *execCmd
	default:
		flag.Usage()
		os.Exit(2)
	}

	start := time.Now()
	res, err := conformance.Run(suite, v)
	if err != nil {
		fail(err)
	}
	fmt.Printf("suite     %s, specification %s, %d cases\n", suite.Name, suite.Spec, res.Total)
	fmt.Printf("verifier  %s\n", name)
	for _, class := range []string{conformance.Conflict, conformance.Clear, conformance.Either} {
		fmt.Printf("%-9s %4d cases, %4d reported as conflict\n", class, res.ByClass[class], res.Rejected[class])
	}
	byGroup := map[string]int{}
	for _, f := range res.Failures {
		byGroup[f.Case.Group]++
	}
	if len(res.Failures) == 0 {
		fmt.Printf("result    PASS in %.1f s\n", time.Since(start).Seconds())
		return
	}
	names := make([]string, 0, len(byGroup))
	for g := range byGroup {
		names = append(names, g)
	}
	sort.Strings(names)
	fmt.Printf("result    FAIL: %d of %d cases\n", len(res.Failures), res.Total)
	for _, g := range names {
		fmt.Printf("          %-16s %3d  %s\n", g, byGroup[g], conformance.Purpose(g))
	}
	for i, f := range res.Failures {
		if i == *show {
			fmt.Printf("          ... and %d more\n", len(res.Failures)-i)
			break
		}
		fmt.Printf("  %-22s %s\n", f.Case.ID, f.Reason)
	}
	os.Exit(1)
}

func writeSuite(path string, seed uint64) error {
	suite, err := conformance.Generate(seed)
	if err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := suite.Write(f); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	classes, perGroup := map[string]int{}, map[string]int{}
	for _, c := range suite.Cases {
		classes[c.Expect.Class]++
		perGroup[c.Group]++
	}
	fmt.Printf("wrote %s: %d cases (%d conflict, %d clear, %d either)\n", path, len(suite.Cases),
		classes[conformance.Conflict], classes[conformance.Clear], classes[conformance.Either])
	for _, g := range conformance.GroupNames() {
		fmt.Printf("  %-16s %3d\n", g, perGroup[g])
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "conform:", err)
	os.Exit(1)
}
