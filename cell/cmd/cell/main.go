// Command cell runs the kernel of a cell on several replicas that agree
// through CometBFT (docs/replication.md).
//
//	cell run   -record FILE [-out FILE]      four replicas in this process replay a record
//	cell setup -dir DIR -record FILE         write the files of a cell
//	cell node  -home DIR [-out FILE]         run one replica until it is interrupted
//	cell feed  -home DIR -record FILE        give the inputs of a record to a running cell
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/teddyknox/clearway/cell/app"
	"github.com/teddyknox/clearway/cell/client"
	"github.com/teddyknox/clearway/cell/node"
	"github.com/teddyknox/clearway/internal/record"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "cell:", err)
		os.Exit(1)
	}
}

const usage = "usage: cell run|setup|node|feed [flags]; cell COMMAND -h lists the flags"

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "run":
		return runCell(args[1:], out)
	case "setup":
		return setup(args[1:], out)
	case "node":
		return runNode(args[1:], out)
	case "feed":
		return feed(args[1:], out)
	}
	return errors.New(usage)
}

// readRecord reads a record, which gzip may have compressed: its header
// line and its steps.
func readRecord(name string) (header []byte, steps []app.Step, err error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(name, ".gz") {
		if r, err = gzip.NewReader(f); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	header, steps, err = app.Steps(r)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", name, err)
	}
	return header, steps, nil
}

// quorum returns the number of replicas of n that a block needs: more than
// two thirds.
func quorum(n int) int { return 2*n/3 + 1 }

// writeRecord writes the record that a replica holds to the file name, and
// checks it as a party outside the cell does: with the public keys of the
// genesis file and the quorum.
func writeRecord(r *node.Replica, name string, out io.Writer) error {
	var b bytes.Buffer
	if _, err := r.Journal.WriteTo(&b); err != nil {
		return err
	}
	if name != "" {
		if err := os.WriteFile(name, b.Bytes(), 0o644); err != nil {
			return err
		}
	}
	keys, err := app.GenesisKeys(r.GenesisFile())
	if err != nil {
		return err
	}
	q := quorum(len(keys))
	rep, err := record.Verify(bytes.NewReader(b.Bytes()), record.Options{Trusted: keys, Quorum: q})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "record     %d chained lines, %d checkpoints, each signed by %d of %d replicas or more\n", rep.Lines, rep.Checkpoints, q, len(keys))
	for _, f := range rep.Failures {
		fmt.Fprintf(out, "FAIL       %v\n", f)
	}
	if len(rep.Failures) > 0 {
		return fmt.Errorf("the record of the cell fails the check: %d failures", len(rep.Failures))
	}
	if name != "" {
		fmt.Fprintf(out, "wrote      %s\ncheck it   record verify -quorum %d", name, q)
		for _, k := range keys {
			fmt.Fprintf(out, " -key %s", record.FormatPublicKey(k))
		}
		fmt.Fprintf(out, " %s\n", name)
	}
	return nil
}

func runCell(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("cell run", flag.ContinueOnError)
	rec := fs.String("record", "", "the record whose inputs the cell gets (docs/record.md; .gz is read as gzip)")
	n := fs.Int("replicas", 4, "the number of replicas")
	every := fs.Int64("every", 60_000_000, "kernel time between two checkpoints that the replicas sign (µs); 0: after every block with an input")
	window := fs.Int("window", 16, "batches of inputs that wait for a block at one time")
	interpret := fs.Bool("interpret", false, "the last replica interprets every instruction of the kernel program")
	dir := fs.String("dir", "", "keep the files of the cell in this directory (default: a temporary directory)")
	outName := fs.String("out", "", "write the record of the cell, with the signatures of the replicas, to this file")
	limit := fs.Duration("timeout", 30*time.Minute, "give up after this time")
	verbose := fs.Bool("v", false, "print the errors that CometBFT logs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *rec == "" || *n < 1 {
		return errors.New("cell run needs -record, and one replica or more")
	}
	header, steps, err := readRecord(*rec)
	if err != nil {
		return err
	}
	if *dir == "" {
		tmp, err := os.MkdirTemp("", "clearway-cell-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		*dir = tmp
	}
	homes, err := node.Setup(*dir, *n, header, *every)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *limit)
	defer cancel()
	rs := make([]*node.Replica, len(homes))
	for i, home := range homes {
		opt := node.Options{Interpret: *interpret && i == len(homes)-1}
		if *verbose {
			opt.Log = os.Stderr
		}
		r, err := node.Start(home, opt)
		if err != nil {
			return err
		}
		defer r.Stop()
		rs[i] = r
	}
	for _, r := range rs {
		if err := r.Client().Ready(ctx); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "cell       %d replicas; a block needs %d of them\n", len(rs), quorum(len(rs)))
	began := time.Now()
	done, err := rs[0].Client().Replay(ctx, steps, *window)
	if err != nil {
		return err
	}
	took := time.Since(began)
	fmt.Fprintf(out, "replayed   %d batches of inputs in blocks %d to %d, in %v\n", done.Batches, done.First, done.Last, took.Round(time.Millisecond))
	fmt.Fprintf(out, "verdicts   %d, each equal to the verdict of the record\n", done.Verdicts)
	fmt.Fprintf(out, "state      equal to the record at each of its %d checkpoints\n", len(steps))

	// Every replica reaches the last checkpoint of the record, and a quorum
	// signs every checkpoint that the replicas sign.
	last := steps[len(steps)-1].Want
	for {
		ok := true
		for _, r := range rs {
			c := r.App.Checkpoint()
			ok = ok && c.Seq == last.Seq && c.Head == last.Head && c.State == last.State
			for _, m := range r.Journal.Checkpoints() {
				ok = ok && len(m.Sigs) >= quorum(len(rs))
			}
		}
		if ok {
			break
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the replicas did not all reach the last checkpoint with the signatures of a quorum: %w", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	fmt.Fprintf(out, "replicas   all %d have the last state of the record: line %d, state %x\n", len(rs), last.Seq, last.State)
	return writeRecord(rs[0], *outName, out)
}

func setup(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("cell setup", flag.ContinueOnError)
	dir := fs.String("dir", "", "the directory for the files of the cell")
	rec := fs.String("record", "", "a record whose header line names the cell and gives the parameters of the kernel")
	n := fs.Int("replicas", 4, "the number of replicas")
	every := fs.Int64("every", 60_000_000, "kernel time between two checkpoints that the replicas sign (µs); 0: after every block with an input")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dir == "" || *rec == "" || *n < 1 {
		return errors.New("cell setup needs -dir, -record, and one replica or more")
	}
	header, _, err := readRecord(*rec)
	if err != nil {
		return err
	}
	homes, err := node.Setup(*dir, *n, header, *every)
	if err != nil {
		return err
	}
	for _, home := range homes {
		addr, err := node.RPCAddress(home)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "replica    %s  answers at %s\n", home, addr)
	}
	return nil
}

func runNode(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("cell node", flag.ContinueOnError)
	home := fs.String("home", "", "the home directory of the replica, as cell setup wrote it")
	interpret := fs.Bool("interpret", false, "interpret every instruction of the kernel program")
	verbose := fs.Bool("v", false, "print every message of CometBFT, not only the errors")
	outName := fs.String("out", "", "when the replica stops, write the record of the cell to this file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *home == "" {
		return errors.New("cell node needs -home")
	}
	r, err := node.Start(*home, node.Options{Interpret: *interpret, Log: os.Stderr, Verbose: *verbose})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "replica    %s runs; its key is %s\n", *home, r.Key)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	if err := r.Stop(); err != nil {
		return err
	}
	c := r.App.Checkpoint()
	fmt.Fprintf(out, "stopped    after block %d: line %d, state %x\n", r.Height(), c.Seq, c.State)
	if *outName == "" {
		return nil
	}
	return writeRecord(r, *outName, out)
}

func feed(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("cell feed", flag.ContinueOnError)
	home := fs.String("home", "", "the home directory of the replica that takes the inputs")
	rec := fs.String("record", "", "the record whose inputs the cell gets (.gz is read as gzip)")
	window := fs.Int("window", 16, "batches of inputs that wait for a block at one time")
	limit := fs.Duration("timeout", 30*time.Minute, "give up after this time")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *home == "" || *rec == "" {
		return errors.New("cell feed needs -home and -record")
	}
	_, steps, err := readRecord(*rec)
	if err != nil {
		return err
	}
	addr, err := node.RPCAddress(*home)
	if err != nil {
		return err
	}
	c, err := client.Dial(addr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *limit)
	defer cancel()
	if err := c.Ready(ctx); err != nil {
		return err
	}
	began := time.Now()
	done, err := c.Replay(ctx, steps, *window)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "replayed   %d batches of inputs in blocks %d to %d, in %v\n", done.Batches, done.First, done.Last, time.Since(began).Round(time.Millisecond))
	fmt.Fprintf(out, "verdicts   %d, each equal to the verdict of the record\n", done.Verdicts)
	fmt.Fprintf(out, "state      equal to the record at each of its %d checkpoints\n", len(steps))
	return nil
}
