package client

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/teddyknox/clearway/cell/app"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/record"
)

// A Gate puts a cell before the kernel of a simulation: it is a sim.Gate.
// The simulator gives it every input of its own kernel. The Gate gives them
// to the cell in batches, and it holds the simulator at each proposal until
// the cell has given a verdict with a certificate that passes the check.
//
// The kernel of the simulator is then a copy that the simulator reads. The
// verdict that counts is the verdict of the cell: if the two differ, the
// Gate fails, and the simulator stops.
//
// A batch ends at each proposal and before each line that moves the clock.
// The Gate waits for the cell only at a proposal and at a checkpoint. It
// sends the other batches and goes on.
type Gate struct {
	c      *Client
	trust  *Trust
	ctx    context.Context
	budget time.Duration

	n       uint64   // the number of the next batch
	pending [][]byte // the lines of the batch that is open
	err     error
	waits   []time.Duration // for each verdict: from the proposal to the checked certificate
	late    int
}

// NewGate returns a Gate that talks to the cell through c and checks
// certificates against trust. The cell must have no input yet. budget is
// the time that the cell may take for one verdict: the Gate counts each
// verdict that takes longer, and it does not fail for it.
func NewGate(ctx context.Context, c *Client, trust *Trust, budget time.Duration) *Gate {
	return &Gate{c: c, trust: trust, ctx: ctx, budget: budget}
}

func (g *Gate) fail(format string, a ...any) {
	if g.err == nil {
		g.err = fmt.Errorf("gate: "+format, a...)
	}
}

// Err returns the first failure of the Gate, if any.
func (g *Gate) Err() error { return g.err }

// take closes the open batch and returns its number and its lines.
func (g *Gate) take() (uint64, [][]byte) {
	n, lines := g.n, g.pending
	g.n, g.pending = g.n+1, nil
	return n, lines
}

// Input takes one input of the kernel of the simulator.
func (g *Gate) Input(in kernel.Input) {
	if g.err != nil {
		return
	}
	line, err := record.InputLine(in)
	if err != nil {
		g.fail("%v", err)
		return
	}
	if in.Kind == kernel.InAdvance && len(g.pending) > 0 {
		n, lines := g.take()
		if err := g.c.Send(g.ctx, n, lines); err != nil {
			g.fail("%v", err)
			return
		}
	}
	g.pending = append(g.pending, line)
}

// Verdict takes the verdict of the kernel of the simulator for the proposal
// that Input got last. It returns when the cell has given the same verdict,
// with a certificate that passes the check, or when the Gate has failed.
func (g *Gate) Verdict(v kernel.Verdict) {
	if g.err != nil {
		return
	}
	want, err := record.VerdictLine(v)
	if err != nil {
		g.fail("%v", err)
		return
	}
	began := time.Now()
	n, lines := g.take()
	res, err := g.c.Submit(g.ctx, n, lines)
	if err != nil {
		g.fail("the cell gave no verdict for batch %d: %v", n, err)
		return
	}
	cert, err := g.c.Certificate(g.ctx, res)
	if err != nil {
		g.fail("no certificate for batch %d: %v", n, err)
		return
	}
	cn, clines, verdicts, err := cert.Verify(g.trust)
	if err != nil {
		g.fail("batch %d: %v", n, err)
		return
	}
	if cn != n || !bytes.Equal(bytes.Join(clines, []byte{'\n'}), bytes.Join(lines, []byte{'\n'})) {
		g.fail("the certificate is for batch %d and other lines, not for batch %d", cn, n)
		return
	}
	if len(verdicts) != 1 || !bytes.Equal(verdicts[0], want) {
		g.fail("batch %d: the cell gave the verdicts %q; the kernel of the simulator gave %q", n, verdicts, want)
		return
	}
	took := time.Since(began)
	g.waits = append(g.waits, took)
	if g.budget > 0 && took > g.budget {
		g.late++
	}
}

// Checkpoint compares the record of the simulator so far with the cell: the
// number of chained lines, the kernel time, the chain value, and the state
// digest. It waits until the cell has applied every input.
func (g *Gate) Checkpoint(want record.Checkpoint) {
	if g.err != nil {
		return
	}
	if len(g.pending) > 0 {
		n, lines := g.take()
		if _, err := g.c.Submit(g.ctx, n, lines); err != nil {
			g.fail("the cell did not apply batch %d: %v", n, err)
			return
		}
	} else {
		// The batches that the Gate sent and did not wait for.
		for {
			next, err := g.c.Next(g.ctx)
			if err != nil {
				g.fail("%v", err)
				return
			}
			if next == g.n {
				break
			}
			if err := wait(g.ctx); err != nil {
				g.fail("the cell has applied %d batches of %d: %v", next, g.n, err)
				return
			}
		}
	}
	have, err := g.c.Checkpoint(g.ctx)
	if err != nil {
		g.fail("%v", err)
		return
	}
	if have.Seq != want.Seq || have.T != want.T || have.Head != want.Head || have.State != want.State {
		g.fail("at line %d the cell has the checkpoint %s; the simulator has %s", want.Seq, have.MarshalLine(), unsigned(want).MarshalLine())
	}
}

// Waits says how long the simulator waited for the cell.
type Waits struct {
	Batches  uint64        // the batches of inputs that the Gate gave to the cell
	Verdicts int           // the verdicts that the simulator waited for
	Median   time.Duration // from a proposal to its checked certificate
	P99      time.Duration
	Max      time.Duration
	Late     int // the verdicts that took longer than the budget
}

// Waits returns the times that the Gate has measured so far.
func (g *Gate) Waits() Waits {
	w := Waits{Batches: g.n, Verdicts: len(g.waits), Late: g.late}
	if len(g.waits) == 0 {
		return w
	}
	s := append([]time.Duration(nil), g.waits...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	w.Median, w.P99, w.Max = s[len(s)/2], s[len(s)*99/100], s[len(s)-1]
	return w
}

// Send gives batch n of the inputs of the cell to the replica and does not
// wait for a block. Send returns an error if the replica refuses the batch.
func (c *Client) Send(ctx context.Context, n uint64, lines [][]byte) error {
	res, err := c.rpc.BroadcastTxSync(ctx, app.InputTx(n, lines))
	if err != nil {
		return fmt.Errorf("batch %d: %w", n, err)
	}
	if res.Code != app.CodeOK {
		return fmt.Errorf("the replica refused batch %d: code %d", n, res.Code)
	}
	return nil
}
