package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teddyknox/clearway/cell/app"
	"github.com/teddyknox/clearway/cell/client"
	"github.com/teddyknox/clearway/internal/record"
)

const sessionVector = "../../conformance/kernel-session-0.2.rec"

// vector reads a published record: its header line, and its steps.
func vector(t testing.TB, name string) (header []byte, steps []app.Step) {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header, steps, err = app.Steps(f)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return header, steps
}

// cell sets up a cell with one replica for each of opts, and starts them.
// The test stops them when it ends.
func cell(t testing.TB, header []byte, every int64, opts ...Options) (homes []string, rs []*Replica) {
	t.Helper()
	homes, err := Setup(t.TempDir(), len(opts), header, every)
	if err != nil {
		t.Fatal(err)
	}
	rs = make([]*Replica, len(opts))
	for i, home := range homes {
		rs[i] = start(t, home, opts[i])
	}
	ready(t, rs...)
	return homes, rs
}

// ready waits until the replicas take part in consensus.
func ready(t testing.TB, rs ...*Replica) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, r := range rs {
		if err := r.Client().Ready(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func start(t testing.TB, home string, opt Options) *Replica {
	t.Helper()
	r, err := Start(home, opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := r.Stop(); err != nil {
			t.Errorf("stop %s: %v", home, err)
		}
	})
	return r
}

// eventually waits until cond holds, and fails the test after d.
func eventually(t testing.TB, d time.Duration, what string, cond func() bool) {
	t.Helper()
	for end := time.Now().Add(d); !cond(); time.Sleep(2 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("after %v: not yet: %s", d, what)
		}
	}
}

// at reports whether a replica has the state that the checkpoint of a
// record states: the same number of lines, kernel time, chain value, and
// state digest.
func at(r *Replica, want record.Checkpoint) bool {
	c := r.App.Checkpoint()
	return c.Cell == want.Cell && c.Seq == want.Seq && c.T == want.T && c.Head == want.Head && c.State == want.State
}

// feed gives the batches of one step of a record to the cell through one
// replica, and checks the verdicts of the cell against those of the record.
func feed(t testing.TB, ctx context.Context, r *Replica, first uint64, s app.Step) {
	t.Helper()
	got := verdicts(t, ctx, r.Client(), first, s.Batches)
	if !bytes.Equal(bytes.Join(got, []byte{'\n'}), bytes.Join(s.Verdicts, []byte{'\n'})) {
		t.Fatalf("before line %d: the cell gave the verdicts %q; the record has %q", s.Want.Seq, got, s.Verdicts)
	}
}

// journal returns the record that a replica has built.
func journal(t testing.TB, r *Replica) []byte {
	t.Helper()
	var b bytes.Buffer
	if _, err := r.Journal.WriteTo(&b); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// signed reports whether at least quorum replicas have signed every
// checkpoint of the journal of r.
func signed(r *Replica, quorum int) bool {
	for _, c := range r.Journal.Checkpoints() {
		if len(c.Sigs) < quorum {
			return false
		}
	}
	return true
}

func trusted(t testing.TB, rs []*Replica) []ed25519.PublicKey {
	t.Helper()
	keys := make([]ed25519.PublicKey, len(rs))
	for i, r := range rs {
		k, err := record.ParsePublicKey(r.Key)
		if err != nil {
			t.Fatal(err)
		}
		keys[i] = k
	}
	return keys
}

// Four replicas on the network of this computer get the inputs of a
// published record, each step through another replica. One of them runs the
// kernel program in the interpreter alone. The cell gives the verdicts of
// the record, and at each checkpoint of the record every replica has its
// number of lines, kernel time, chain value, and state digest.
//
// Every replica then holds the same record of the cell, with every
// checkpoint signed by all four. That record passes the check of
// docs/record.md with the keys of the replicas and a quorum of three: the
// check replays the inputs in a kernel of its own.
func TestCell(t *testing.T) {
	header, steps := vector(t, sessionVector)
	_, rs := cell(t, header, 0, Options{}, Options{}, Options{}, Options{Interpret: true})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	began := time.Now()
	n := uint64(0)
	for i, s := range steps {
		feed(t, ctx, rs[i%len(rs)], n, s)
		n += uint64(len(s.Batches))
		for _, r := range rs {
			eventually(t, 20*time.Second, "the replica reaches the checkpoint of the record", func() bool { return at(r, s.Want) })
		}
	}
	fed := time.Since(began)
	for _, r := range rs {
		eventually(t, 20*time.Second, "four signatures for every checkpoint", func() bool { return signed(r, len(rs)) })
	}
	first := journal(t, rs[0])
	for i, r := range rs[1:] {
		if !bytes.Equal(journal(t, r), first) {
			t.Fatalf("replica %d and replica 0 hold different records", i+1)
		}
	}
	rep, err := record.Verify(bytes.NewReader(first), record.Options{Trusted: trusted(t, rs), Quorum: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Failures {
		t.Errorf("the record of the cell: %v", f)
	}
	last := steps[len(steps)-1].Want
	if !rep.ReplayDone || uint64(rep.Lines) != last.Seq+1 || len(rep.Signers) != len(rs) {
		t.Fatalf("%d lines, want %d; replay done %v; signers %v", rep.Lines, last.Seq+1, rep.ReplayDone, rep.Signers)
	}
	t.Logf("%d batches in %d blocks in %v; %d chained lines and %d checkpoints, each signed by %d replicas",
		n, rs[0].Height(), fed.Round(time.Millisecond), rep.Lines, rep.Checkpoints, len(rep.Signers))
}

// verdicts gives batches to the cell through one client and returns the
// verdict lines of the cell, in order.
func verdicts(t testing.TB, ctx context.Context, c *client.Client, first uint64, batches [][][]byte) (got [][]byte) {
	t.Helper()
	err := c.Feed(ctx, first, batches, 8, func(res client.Result) error {
		got = append(got, res.Verdicts...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// TestMain runs one replica, and no test, when the environment names a home
// directory. A test that kills replicas runs them as processes of this
// binary: a process that is killed closes its connections, as a computer
// that fails does.
func TestMain(m *testing.M) {
	if home := os.Getenv(replicaHome); home != "" {
		if _, err := Start(home, Options{Log: os.Stderr}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// The test holds the other end of the standard input. When the test
		// ends, however it ends, the replica ends too.
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const replicaHome = "CELL_TEST_REPLICA_HOME"

// A process is a replica that runs as a process of its own.
type process struct {
	home string
	cmd  *exec.Cmd
	log  *buffer
	*client.Client
}

// run starts the replica with the files in home as a process, and waits
// until it takes part in consensus.
func run(t testing.TB, home string) *process {
	t.Helper()
	addr, err := RPCAddress(home)
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	p := &process{home: home, cmd: exec.Command(os.Args[0]), log: &buffer{}, Client: c}
	p.cmd.Env = append(os.Environ(), replicaHome+"="+home)
	p.cmd.Stderr = p.log
	if _, err := p.cmd.StdinPipe(); err != nil {
		t.Fatal(err)
	}
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.kill)
	end := time.Now().Add(30 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := p.Ready(ctx)
		cancel()
		if err == nil {
			return p
		}
		if time.Now().After(end) {
			t.Fatalf("the replica in %s does not take part: %v; its log:\n%.2000s", home, err, p.log)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// kill ends the process at once, with no chance to store or send anything.
func (p *process) kill() {
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
}

// reaches reports whether the replica of the process has the state that the
// checkpoint of a record states.
func (p *process) reaches(want record.Checkpoint) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := p.Checkpoint(ctx)
	return err == nil && c.Cell == want.Cell && c.Seq == want.Seq && c.T == want.T && c.Head == want.Head && c.State == want.State
}

// A cell of four replicas goes on with three, stops with two, and goes on
// again when the third comes back. Two of the replicas are processes, and
// the test kills them: a killed replica stores nothing more and tells no
// one. A replica that comes back applies every block from the first, gets
// the blocks that it missed from the others, and then has their state.
//
// The cell gives the verdicts of a published record through all of this, and
// at each checkpoint of the record every running replica has its state.
func TestCrashedReplicas(t *testing.T) {
	header, steps := vector(t, sessionVector)
	homes, err := Setup(t.TempDir(), 4, header, 0)
	if err != nil {
		t.Fatal(err)
	}
	opt := Options{CommitTimeout: 1500 * time.Millisecond}
	a, b := start(t, homes[0], opt), start(t, homes[1], opt)
	c, d := run(t, homes[2]), run(t, homes[3])
	ready(t, a, b)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	n := uint64(0)
	check := func(got [][]byte, s app.Step, live ...*process) {
		t.Helper()
		if !bytes.Equal(bytes.Join(got, []byte{'\n'}), bytes.Join(s.Verdicts, []byte{'\n'})) {
			t.Fatalf("before line %d: the cell gave the verdicts %q; the record has %q", s.Want.Seq, got, s.Verdicts)
		}
		for _, r := range []*Replica{a, b} {
			eventually(t, 30*time.Second, "the replica reaches the checkpoint of the record", func() bool { return at(r, s.Want) })
		}
		for _, p := range live {
			eventually(t, 30*time.Second, "the replica process reaches the checkpoint of the record", func() bool { return p.reaches(s.Want) })
		}
	}
	step := func(s app.Step, via *client.Client, live ...*process) {
		t.Helper()
		got := verdicts(t, ctx, via, n, s.Batches)
		n += uint64(len(s.Batches))
		check(got, s, live...)
	}
	step(steps[0], c.Client, c, d)

	// One replica fails. Three of four are a quorum: the cell goes on.
	d.kill()
	missed := a.Height()
	for _, s := range steps[1:4] {
		step(s, b.Client(), c)
	}
	missed = a.Height() - missed

	// A second replica fails. Two of four are not a quorum: the cell makes
	// no block, so it gives no verdict. It does not give a wrong one.
	c.kill()
	held, s := a.Height(), steps[4]
	if _, err := a.Client().Submit(ctx, n, s.Batches[0]); err == nil {
		t.Fatal("the cell applied a batch with two of four replicas")
	}
	if a.Height() != held || b.Height() != held || !at(a, steps[3].Want) || !at(b, steps[3].Want) {
		t.Fatalf("the cell moved with two of four replicas: blocks %d and %d, from %d", a.Height(), b.Height(), held)
	}

	// The third replica comes back, and the batch that waited gets its
	// block.
	c = run(t, homes[2])
	var waited client.Result
	eventually(t, 30*time.Second, "a block holds the batch that waited", func() bool {
		res, err := a.Client().Find(ctx, n, s.Batches[0])
		waited = res
		return err == nil
	})
	got := append(waited.Verdicts, verdicts(t, ctx, c.Client, n+1, s.Batches[1:])...)
	n += uint64(len(s.Batches))
	check(got, s, c)

	// The fourth replica comes back after many blocks. It gets them from
	// the others, and the cell takes inputs through it.
	d = run(t, homes[3])
	for _, s := range steps[5:] {
		step(s, d.Client, c, d)
	}

	// The two replicas of this process hold the same record, and three
	// replicas or more signed every checkpoint of it: those that ran when
	// the cell made it.
	var first []byte
	eventually(t, 30*time.Second, "the replicas hold the same signed record", func() bool {
		first = journal(t, a)
		return signed(a, 3) && signed(b, 3) && bytes.Equal(journal(t, b), first)
	})
	keys, err := app.GenesisKeys(a.GenesisFile())
	if err != nil {
		t.Fatal(err)
	}
	rep, err := record.Verify(bytes.NewReader(first), record.Options{Trusted: keys, Quorum: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Failures {
		t.Errorf("the record of the cell: %v", f)
	}
	if last := steps[len(steps)-1].Want; !rep.ReplayDone || uint64(rep.Lines) != last.Seq+1 || len(rep.Signers) != 4 {
		t.Fatalf("%d lines, want %d; replay done %v; signers %v", rep.Lines, last.Seq+1, rep.ReplayDone, rep.Signers)
	}
	t.Logf("%d batches in %d blocks; one replica missed %d blocks and more; %d checkpoints, signers %v",
		n, a.Height(), missed, rep.Checkpoints, rep.Signers)
}

// buffer is a log that a test reads while a replica writes it.
type buffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// One replica of four is faulty: from one block on, it reports a state and
// verdicts that it does not have. The other three do not agree with it, and
// three are a quorum: the cell gives the verdicts of a published record, and
// its record passes the check with a quorum of three.
//
// The faulty replica cannot go on. It sees that the cell has agreed on a
// block that its own state does not allow, and it stops.
func TestLyingReplica(t *testing.T) {
	const liesFrom = 3
	header, steps := vector(t, sessionVector)
	var log buffer
	_, rs := cell(t, header, 0, Options{}, Options{}, Options{},
		Options{Lie: func(h int64) bool { return h >= liesFrom }, Log: &log})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	honest, liar := rs[:3], rs[3]
	n := uint64(0)
	for i, s := range steps {
		feed(t, ctx, honest[i%3], n, s)
		n += uint64(len(s.Batches))
		for _, r := range honest {
			eventually(t, 30*time.Second, "the replica reaches the checkpoint of the record", func() bool { return at(r, s.Want) })
		}
	}
	if h := liar.Height(); h != liesFrom {
		t.Fatalf("the faulty replica has %d blocks; it lied from block %d, so it cannot accept block %d", h, liesFrom, liesFrom+1)
	}
	if out := log.String(); !strings.Contains(out, "CONSENSUS FAILURE") || !strings.Contains(out, "wrong Block.Header.AppHash") {
		t.Fatalf("the faulty replica did not stop on the block of the others; its log:\n%.2000s", out)
	}
	var first []byte
	eventually(t, 30*time.Second, "the three correct replicas hold the same signed record", func() bool {
		first = journal(t, honest[0])
		for _, r := range honest {
			if !signed(r, 3) || !bytes.Equal(journal(t, r), first) {
				return false
			}
		}
		return true
	})
	rep, err := record.Verify(bytes.NewReader(first), record.Options{Trusted: trusted(t, rs), Quorum: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Failures {
		t.Errorf("the record of the cell: %v", f)
	}
	if last := steps[len(steps)-1].Want; !rep.ReplayDone || uint64(rep.Lines) != last.Seq+1 {
		t.Fatalf("%d lines, want %d; replay done %v", rep.Lines, last.Seq+1, rep.ReplayDone)
	}
	t.Logf("the faulty replica stopped at block %d; the cell went on to block %d; signers %v", liar.Height(), honest[0].Height(), rep.Signers)
}
