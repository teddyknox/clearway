package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/teddyknox/clearway/cell/client"
	"github.com/teddyknox/clearway/cell/node"
	"github.com/teddyknox/clearway/internal/record"
)

const (
	sessionVector = "../../../conformance/kernel-session-0.2.rec"
	trafficVector = "../../../conformance/kernel-traffic-0.2.rec.gz"
)

// TestMain runs the command, and no test, when the environment gives its
// arguments. A test that needs replicas as processes runs this binary.
func TestMain(m *testing.M) {
	if args := os.Getenv(commandArgs); args != "" {
		if err := run(strings.Split(args, "\n"), os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "cell:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const commandArgs = "CELL_TEST_ARGS"

// verify checks a record that the command wrote as the last line of its
// output says: with the keys of the replicas, and the quorum.
func verify(t *testing.T, out, name string, lines uint64) {
	t.Helper()
	var keys []ed25519.PublicKey
	for _, m := range regexp.MustCompile(`-key ([0-9a-f]{64})`).FindAllStringSubmatch(out, -1) {
		k, err := record.ParsePublicKey(m[1])
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k)
	}
	if len(keys) != 4 || !strings.Contains(out, "record verify -quorum 3 ") {
		t.Fatalf("the command does not say how to check the record:\n%s", out)
	}
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rep, err := record.Verify(f, record.Options{Trusted: keys, Quorum: 3})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Failures {
		t.Errorf("the record that the command wrote: %v", f)
	}
	if !rep.ReplayDone || uint64(rep.Lines) != lines || rep.Checkpoints == 0 {
		t.Fatalf("%d lines, want %d; %d checkpoints; replay done %v", rep.Lines, lines, rep.Checkpoints, rep.ReplayDone)
	}
}

// cell run replays a published record on four replicas in one process, and
// writes a record that passes the check with the keys of the replicas.
func TestRun(t *testing.T) {
	for _, c := range []struct {
		name, vector string
		args         []string
		lines        uint64
		long         bool
	}{
		{"session", sessionVector, []string{"-every", "0", "-interpret"}, 816, false},
		{"traffic", trafficVector, nil, 13742, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.long && testing.Short() {
				t.Skip("a long record")
			}
			name := filepath.Join(t.TempDir(), "cell.rec")
			var out bytes.Buffer
			if err := run(append([]string{"run", "-record", c.vector, "-out", name}, c.args...), &out); err != nil {
				t.Fatalf("%v\n%s", err, out.String())
			}
			verify(t, out.String(), name, c.lines)
			t.Logf("\n%s", regexp.MustCompile(`(?m)^check it.*$`).ReplaceAllString(out.String(), "check it   record verify -quorum 3 -key ... cell.rec"))
		})
	}
}

// cell setup writes the files of a cell, cell node runs each replica as a
// process, and cell feed gives the inputs of a published record to the cell
// through one of them. A replica that is interrupted writes the record of
// the cell, and that record passes the check.
func TestProcesses(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	if err := run([]string{"setup", "-dir", dir, "-record", sessionVector, "-every", "0"}, &out); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "cell.rec")
	var first *exec.Cmd
	var said bytes.Buffer
	for i := 0; i < 4; i++ {
		args := []string{"node", "-home", filepath.Join(dir, fmt.Sprintf("replica%d", i))}
		cmd := exec.Command(os.Args[0])
		if i == 0 {
			args = append(args, "-out", name)
			cmd.Stdout = &said
			first = cmd
		}
		cmd.Env = append(os.Environ(), commandArgs+"="+strings.Join(args, "\n"))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}
	// The replicas listen a moment after they start.
	var err error
	for end := time.Now().Add(30 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		out.Reset()
		err = run([]string{"feed", "-home", filepath.Join(dir, "replica1"), "-record", sessionVector, "-timeout", "1m"}, &out)
		if err == nil || !strings.Contains(err.Error(), "connection refused") || time.Now().After(end) {
			break
		}
	}
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "replayed   181 batches") || !strings.Contains(out.String(), "verdicts   11,") {
		t.Fatalf("cell feed says:\n%s", out.String())
	}
	// The record is whole when its last checkpoint has the signatures of a
	// quorum. They come in the blocks after the last input.
	addr, err := node.RPCAddress(filepath.Join(dir, "replica0"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(30 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		m, err := c.Signed(context.Background())
		if err == nil && m.Seq == 815 && len(m.Sigs) >= 3 {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("the last checkpoint has no quorum of signatures: %+v, %v", m, err)
		}
	}
	if err := first.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatalf("the replica that was interrupted: %v\n%s", err, said.String())
	}
	verify(t, said.String(), name, 816)
}
