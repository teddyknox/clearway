package kimage

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teddyknox/clearway/internal/record"
	"github.com/teddyknox/clearway/internal/rvm"
)

func TestImageHasItsDigest(t *testing.T) {
	if got := Digest(ELF); got != SHA256 {
		t.Fatalf("kernel.elf has the digest %s; the package states %s. Run guest/build.sh and update SHA256.", got, SHA256)
	}
}

func open(t *testing.T, name string) io.Reader {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(name, ".gz") {
		return bytes.NewReader(data)
	}
	z, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return z
}

// The kernel program, in the machine, reproduces the published kernel
// vectors: every verdict and every state digest that the Go kernel wrote, bit
// for bit. This is the test that two kernels in two languages, one of them on
// a fixed instruction set, compute the same thing.
//
// Each vector runs twice: in the interpreter, which defines the machine, and
// as translated code. The two must execute the same count of instructions.
func TestProgramReproducesTheVectors(t *testing.T) {
	for _, c := range []struct {
		name              string
		verdicts, digests int
		long              bool
	}{
		{"../../conformance/kernel-session-0.2.rec", 11, 16, false},
		{"../../conformance/kernel-traffic-0.2.rec.gz", 428, 30, true},
	} {
		if c.long && testing.Short() {
			continue
		}
		var steps [2]uint64
		for i, mode := range []Machine{{Interpret: true}, {}} {
			start := time.Now()
			rep, err := record.ReplayProgram(open(t, c.name), mode)
			if err != nil {
				t.Fatal(err)
			}
			if !rep.OK() || rep.Verdicts != c.verdicts || rep.Digests != c.digests {
				t.Fatalf("%s: %d verdicts, %d state digests, failures: %v", c.name, rep.Verdicts, rep.Digests, rep.Failures)
			}
			steps[i] = rep.Steps
			t.Logf("%s, %s: %d verdicts and %d state digests reproduced in %d instructions (%.2f s)",
				c.name, []string{"interpreted", "translated"}[i], rep.Verdicts, rep.Digests, rep.Steps, time.Since(start).Seconds())
		}
		if steps[0] != steps[1] {
			t.Errorf("%s: %d instructions in the interpreter and %d as translated code", c.name, steps[0], steps[1])
		}
	}
}

// kcode.go must be what rvm.Translate writes for kernel.elf now. So a change
// to the program, to the translator, or to the decoder of the machine cannot
// leave the repository with old translated code.
func TestCodeIsCurrent(t *testing.T) {
	want, err := rvm.Translate(ELF, Memory, "kimage", "kernelCode")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("kcode.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("kcode.go is not the translation of kernel.elf: run guest/build.sh, or go run ./cmd/rvgen -pkg kimage -name kernelCode -o internal/kimage/kcode.go internal/kimage/kernel.elf")
	}
}

// capture is a record.Program that keeps the input stream of a record.
type capture struct{ input []byte }

func (c *capture) Run(input []byte) ([]byte, uint32, uint64, error) {
	c.input = bytes.Clone(input)
	return Machine{}.Run(input)
}

// With translated code the machine passes through the states of the
// interpreter. The test runs the session vector on two machines, one with
// the code and one without, stops both at random counts of instructions, and
// compares the whole state at each stop: the machine state digest, which
// covers the registers and every byte of memory, and the output so far.
func TestTranslatedCodeIsTheInterpreter(t *testing.T) {
	var c capture
	if rep, err := record.ReplayProgram(open(t, "../../conformance/kernel-session-0.2.rec"), &c); err != nil || !rep.OK() {
		t.Fatal(err, rep)
	}
	load := func(code *rvm.Code) (*rvm.Machine, *bytes.Buffer) {
		m, err := rvm.Load(ELF, Memory)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.UseCode(code); err != nil {
			t.Fatal(err)
		}
		out := &bytes.Buffer{}
		m.In, m.Out = bytes.NewReader(c.input), out
		return m, out
	}
	var inCode uint64
	counted := *kernelCode
	counted.Run = func(x *[32]uint32, mem []byte, pc uint32, budget uint64) (uint32, uint64) {
		next, n := kernelCode.Run(x, mem, pc, budget)
		inCode += n
		return next, n
	}
	a, outA := load(nil)
	b, outB := load(&counted)
	r := rand.New(rand.NewPCG(3, 4))
	stops := 0
	for {
		k := uint64(1 + r.IntN(900_000))
		if stops%4 == 0 {
			k = uint64(1 + r.IntN(50))
		}
		exitA, errA := a.Run(k)
		exitB, errB := b.Run(k)
		stops++
		if (errA == nil) != (errB == nil) || exitA != exitB || a.PC != b.PC || a.Steps != b.Steps || a.X != b.X {
			t.Fatalf("after %d instructions the interpreter is at pc %#x (%v); with translated code the machine is at pc %#x after %d (%v)",
				a.Steps, a.PC, errA, b.PC, b.Steps, errB)
		}
		if a.StateDigest() != b.StateDigest() || !bytes.Equal(outA.Bytes(), outB.Bytes()) {
			t.Fatalf("after %d instructions the memory or the output of the two machines differs", a.Steps)
		}
		if errA == nil {
			break
		}
		if !errors.Is(errA, rvm.ErrStepLimit) {
			t.Fatal(errA)
		}
	}
	if stops < 20 || inCode*100 < b.Steps*99 {
		t.Fatalf("weak test: %d stops, and %d of %d instructions in translated code", stops, inCode, b.Steps)
	}
	t.Logf("%d instructions, %d of them in translated code; the same machine state at %d stops", b.Steps, inCode, stops)
}
