package conformance

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/teddyknox/clearway/internal/kernel"
)

// Verifier answers one case.
type Verifier interface {
	Check(c *Case) (Answer, error)
}

// Builtin wraps one of this module's implementations.
type Builtin struct{ Checker kernel.PairChecker }

func (b Builtin) Check(c *Case) (Answer, error) {
	r := b.Checker.FirstConflict(c.A.Kernel(1), c.B.Kernel(2), c.From, c.To, c.Sep.params())
	return Answer{ID: c.ID, Conflict: r.Conflict, T: r.T}, nil
}

// BuiltinByName returns the implementation called "analytic" or "sampled".
func BuiltinByName(name string) (Verifier, error) {
	switch name {
	case "analytic":
		return Builtin{kernel.AnalyticChecker{}}, nil
	case "sampled":
		return Builtin{kernel.SampledChecker{Step: 1}}, nil
	}
	return nil, fmt.Errorf("no built-in Verifier called %q", name)
}

// request is what the runner sends to an outside Verifier: a case without
// its expected answer.
type request struct {
	ID   string  `json:"id"`
	A    Tube    `json:"a"`
	B    Tube    `json:"b"`
	From float64 `json:"from"`
	To   float64 `json:"to"`
	Sep  Sep     `json:"sep"`
}

// External is a Verifier in another process, in any language.
//
// Protocol: the runner writes one case as one line of JSON to the process's
// standard input. The process writes one answer as one line of JSON to its
// standard output: {"id": ..., "conflict": true or false, "t": seconds}. It
// must flush after each line. "t" is read only when "conflict" is true. The
// process should exit when its input ends.
type External struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// Start runs the outside Verifier given by argv.
func Start(ctx context.Context, argv []string) (*External, error) {
	if len(argv) == 0 {
		return nil, errors.New("no command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &External{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20)}, nil
}

func (e *External) Check(c *Case) (Answer, error) {
	line, err := json.Marshal(request{c.ID, c.A, c.B, c.From, c.To, c.Sep})
	if err != nil {
		return Answer{}, err
	}
	if _, err := e.in.Write(append(line, '\n')); err != nil {
		return Answer{}, fmt.Errorf("write to Verifier: %w", err)
	}
	reply, err := e.out.ReadBytes('\n')
	if err != nil {
		return Answer{}, fmt.Errorf("read from Verifier: %w", err)
	}
	var a Answer
	if err := json.Unmarshal(reply, &a); err != nil {
		return Answer{}, fmt.Errorf("answer is not JSON: %q", reply)
	}
	if a.ID != c.ID {
		return Answer{}, fmt.Errorf("answer is for %q, case is %q", a.ID, c.ID)
	}
	return a, nil
}

// Close ends the outside Verifier.
func (e *External) Close() error {
	e.in.Close()
	return e.cmd.Wait()
}

// Failure is one case that a Verifier got wrong.
type Failure struct {
	Case   *Case
	Answer Answer
	Reason string
}

// Result is the outcome of running a suite.
type Result struct {
	Total    int
	ByClass  map[string]int // cases per class
	Rejected map[string]int // per class, how many the Verifier called a conflict
	Failures []Failure
}

// Run puts every case of the suite to v and judges the answers.
func Run(s *Suite, v Verifier) (*Result, error) {
	res := &Result{ByClass: map[string]int{}, Rejected: map[string]int{}}
	for i := range s.Cases {
		c := &s.Cases[i]
		ans, err := v.Check(c)
		if err != nil {
			return res, fmt.Errorf("%s: %w", c.ID, err)
		}
		res.Total++
		res.ByClass[c.Expect.Class]++
		if ans.Conflict {
			res.Rejected[c.Expect.Class]++
		}
		if err := Judge(c, ans); err != nil {
			res.Failures = append(res.Failures, Failure{c, ans, err.Error()})
		}
	}
	return res, nil
}
