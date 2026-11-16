package kimage

import (
	"bufio"
	"bytes"
	"math/rand/v2"
	"testing"

	"github.com/teddyknox/clearway/internal/record"
)

// lines returns the lines of a published record, without their newlines.
func lines(t *testing.T, name string) [][]byte {
	t.Helper()
	var out [][]byte
	sc := bufio.NewScanner(open(t, name))
	sc.Buffer(nil, 64<<20)
	for sc.Scan() {
		out = append(out, bytes.Clone(sc.Bytes()))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// A record.Session gives the kernel program one line of a record at a time,
// as a replica of a cell does. It must give the record back: each verdict
// line byte for byte, and at each checkpoint the number of the line, the
// kernel time, the value of the chain, and the state digest.
func TestSessionReproducesTheVectors(t *testing.T) {
	for _, name := range []string{"../../conformance/kernel-session-0.2.rec", "../../conformance/kernel-traffic-0.2.rec.gz"} {
		ls := lines(t, name)
		checkpoints, err := record.ReadCheckpoints(open(t, name))
		if err != nil {
			t.Fatal(err)
		}
		proc, err := Start(false)
		if err != nil {
			t.Fatal(err)
		}
		s, err := record.NewSession(ls[0], proc)
		if err != nil {
			t.Fatal(err)
		}
		var verdict []byte // the verdict that the next line must be
		verdicts, next := 0, 0
		for i, l := range ls[1:] {
			switch {
			case bytes.HasPrefix(l, []byte(`{"k":"verdict"`)):
				if !bytes.Equal(l, verdict) {
					t.Fatalf("%s, line %d: the record has %s; the session gave %s", name, i+2, l, verdict)
				}
				verdict = nil
				verdicts++
			case bytes.HasPrefix(l, []byte(`{"k":"checkpoint"`)):
				got, err := s.Checkpoint()
				if err != nil {
					t.Fatal(err)
				}
				want := checkpoints[next]
				next++
				if got.Seq != want.Seq || got.T != want.T || got.Head != want.Head || got.State != want.State || got.Cell != want.Cell {
					t.Fatalf("%s, line %d: the checkpoint of the session is %+v; the record has %+v", name, i+2, got, want)
				}
			default:
				if verdict != nil {
					t.Fatalf("%s, line %d: the session gave a verdict that the record does not have", name, i+2)
				}
				if err := record.CheckInput(l); err != nil {
					t.Fatalf("%s, line %d: %v", name, i+2, err)
				}
				if verdict, err = s.Apply(l); err != nil {
					t.Fatalf("%s, line %d: %v", name, i+2, err)
				}
			}
		}
		if verdicts == 0 || next != len(checkpoints) || next == 0 {
			t.Fatalf("%s: %d verdicts and %d of %d checkpoints", name, verdicts, next, len(checkpoints))
		}
		t.Logf("%s: %d verdicts and %d checkpoints, one line at a time, in %d instructions", name, verdicts, next, proc.Steps())
	}
}

// What the program computes does not depend on how its input is cut into
// parts: one byte at a time, or in parts of any length, it writes the bytes
// and executes the count of instructions of a run that has all of the input.
func TestProcessInParts(t *testing.T) {
	var c capture
	if rep, err := record.ReplayProgram(open(t, "../../conformance/kernel-session-0.2.rec"), &c); err != nil || !rep.OK() {
		t.Fatal(err, rep)
	}
	whole, _, steps, err := Machine{}.Run(c.input)
	if err != nil {
		t.Fatal(err)
	}
	// The stream without its end message: a Process stops at the end message.
	input := c.input[:len(c.input)-1]
	r := rand.New(rand.NewPCG(8, 9))
	for _, interpret := range []bool{false, true} {
		for trial := 0; trial < 3; trial++ {
			p, err := Start(interpret)
			if err != nil {
				t.Fatal(err)
			}
			var out []byte
			for rest := input; len(rest) > 0; {
				n := 1 + r.IntN(600)
				if trial == 0 {
					n = 1 + r.IntN(3)
				}
				n = min(n, len(rest))
				got, err := p.Step(rest[:n])
				if err != nil {
					t.Fatal(err)
				}
				out, rest = append(out, got...), rest[n:]
			}
			if !bytes.Equal(out, whole) {
				t.Fatalf("interpret %v: the output in parts differs from the output of one run", interpret)
			}
			// The end message: the program halts with exit code 0, and the
			// count of instructions is that of the one run.
			if _, err := p.Step([]byte{255}); err == nil || p.Steps() != steps {
				t.Fatalf("interpret %v: after the end message: %v, %d instructions, want a stop after %d", interpret, err, p.Steps(), steps)
			}
			if _, err := p.Step(nil); err == nil {
				t.Fatal("a Step after the program stopped gave no error")
			}
		}
	}
}

// A stream that the program cannot read stops it, and Step says so.
func TestProcessStops(t *testing.T) {
	p, err := Start(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Step([]byte("not a stream")); err == nil {
		t.Fatal("the program read a stream with no header")
	}
}
