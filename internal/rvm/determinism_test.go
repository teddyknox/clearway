package rvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"
)

// guestDigest is the file that holds the result of the guest workload with
// seed 1 on the committed guest program. The rebuild of the guest writes it.
const guestDigest = "testdata/guest.digest"

// A chunkReader delivers its bytes in parts of pseudo-random lengths.
type chunkReader struct {
	data []byte
	rng  prng
}

func (c *chunkReader) Read(p []byte) (int, error) {
	if len(c.data) == 0 {
		return 0, io.EOF
	}
	n := min(len(p), len(c.data), 1+c.rng.IntN(5000))
	copy(p, c.data[:n])
	c.data = c.data[n:]
	return n, nil
}

// A trace is what one run of the guest shows: the state digest at each
// stop, the output, the count of steps, and the exit code.
type trace struct {
	digests [][32]byte
	output  []byte
	steps   uint64
	exit    uint32
}

// traceGuest runs the guest on the input to its halt, with a stop at each
// of the given step counts, which rise.
func traceGuest(t *testing.T, in io.Reader, stops []uint64, prepare func(*Machine)) trace {
	t.Helper()
	m, err := Load(loadGuest(t), guestMem)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	m.In, m.Out = in, &out
	if prepare != nil {
		prepare(m)
	}
	var tr trace
	for _, stop := range stops {
		if _, err := m.Run(stop - m.Steps); err != ErrStepLimit || m.Steps != stop {
			t.Fatalf("stop at step %d: Run = %v at step %d", stop, err, m.Steps)
		}
		tr.digests = append(tr.digests, m.StateDigest())
	}
	tr.exit, err = m.Run(guestStepLimit)
	if err != nil {
		t.Fatal(err)
	}
	tr.digests = append(tr.digests, m.StateDigest())
	tr.output, tr.steps = out.Bytes(), m.Steps
	return tr
}

// TestDeterminism runs the guest program several times on one input: twice
// in the same way, with the input in parts of different lengths, with other
// decode cache sizes, and with single steps. Every run must show the same
// state digest at each stop, the same output, and the same count of steps.
func TestDeterminism(t *testing.T) {
	g := newGuestWorkload(1)
	stops := []uint64{0, 1, 2, 3, 1000, 12345, 1 << 16, 1 << 20, 3_000_000, 5_000_000, 10_000_000, 15_000_000}
	first := traceGuest(t, bytes.NewReader(g.in), stops, nil)
	if first.exit != guestExit || !bytes.Equal(first.output, g.out) {
		t.Fatalf("the first run has a wrong result")
	}
	for i := 1; i < len(first.digests); i++ {
		if first.digests[i] == first.digests[i-1] {
			t.Errorf("the state digest did not change between stop %d and stop %d", i-1, i)
		}
	}

	runs := map[string]func() trace{
		"second run": func() trace { return traceGuest(t, bytes.NewReader(g.in), stops, nil) },
		"input of one byte at a time": func() trace {
			return traceGuest(t, iotest.OneByteReader(bytes.NewReader(g.in)), stops, nil)
		},
		"input in parts of random lengths": func() trace {
			return traceGuest(t, &chunkReader{data: g.in, rng: prng{state: 5}}, stops, nil)
		},
		"input with the end in the last part": func() trace {
			return traceGuest(t, iotest.DataErrReader(bytes.NewReader(g.in)), stops, nil)
		},
		"no decode cache": func() trace {
			return traceGuest(t, bytes.NewReader(g.in), stops, func(m *Machine) { m.noCache = true })
		},
		"decode cache of 8 entries": func() trace {
			return traceGuest(t, bytes.NewReader(g.in), stops, func(m *Machine) { m.cache = make([]insn, 8) })
		},
		"single steps to step 12345": func() trace {
			return traceGuest(t, bytes.NewReader(g.in), stops[5:], func(m *Machine) {
				for range 12345 {
					if err := m.Step(); err != nil {
						t.Fatal(err)
					}
				}
			})
		},
	}
	for name, run := range runs {
		tr := run()
		want := first.digests
		if len(tr.digests) < len(want) {
			want = want[len(want)-len(tr.digests):]
		}
		for i := range want {
			if tr.digests[i] != want[i] {
				t.Errorf("%s: the state digest differs at stop %d of %d", name, i, len(want))
				break
			}
		}
		if tr.steps != first.steps || tr.exit != first.exit || !bytes.Equal(tr.output, first.output) {
			t.Errorf("%s: %d steps, exit code %#x, %d bytes of output; the first run has %d, %#x, %d",
				name, tr.steps, tr.exit, len(tr.output), first.steps, first.exit, len(first.output))
		}
	}
}

// guestDigestText returns the content of guestDigest for a guest program:
// the hashes of the program, of the input of the workload with seed 1, and
// of the output, the count of steps, and the state digest at the halt.
func guestDigestText(elf []byte) (string, error) {
	g := newGuestWorkload(1)
	m, err := Load(elf, guestMem)
	if err != nil {
		return "", err
	}
	out := sha256.New()
	m.In, m.Out = bytes.NewReader(g.in), out
	if _, err := m.Run(guestStepLimit); err != nil {
		return "", err
	}
	state := m.StateDigest()
	var b strings.Builder
	b.WriteString("# The result of the guest workload with seed 1 on guest.elf; see\n")
	b.WriteString("# internal/rvm/determinism_test.go. A machine that gives another result is\n")
	b.WriteString("# not the machine of this package.\n")
	b.WriteString("# RVM_REBUILD_GUEST=1 go test ./internal/rvm/ writes this file. Do not edit it.\n")
	fmt.Fprintf(&b, "program %x\n", sha256.Sum256(elf))
	fmt.Fprintf(&b, "input   %x\n", sha256.Sum256(g.in))
	fmt.Fprintf(&b, "output  %x\n", out.Sum(nil))
	fmt.Fprintf(&b, "steps   %d\n", m.Steps)
	fmt.Fprintf(&b, "state   %x\n", state)
	return b.String(), nil
}

// TestGuestDigest compares a run of the guest workload with the committed
// result. The committed result came from one host; an equal result on
// another host, or with another version of Go, shows that the machine does
// not depend on the host.
func TestGuestDigest(t *testing.T) {
	want, err := os.ReadFile(guestDigest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := guestDigestText(loadGuest(t))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("the run differs from %s:\n%s\nwant:\n%s", guestDigest, got, want)
	}
}

// TestStateDigest checks the encoding that the comment of StateDigest
// defines, and that the digest covers each part of the state.
func TestStateDigest(t *testing.T) {
	m := &Machine{PC: 0x12345678, Mem: []byte{1, 2, 3, 4, 5}}
	for r := range m.X {
		m.X[r] = 0x1000 + uint32(r)
	}
	m.reserved, m.resAddr = true, 0x40
	m.halted, m.exit = true, 77

	// The input of the hash, written out.
	le := binary.LittleEndian
	in := []byte("clearway rvm state 0.1\n")
	in = le.AppendUint32(in, 0x12345678)
	in = le.AppendUint32(in, 0) // x0
	for r := 1; r < 32; r++ {
		in = le.AppendUint32(in, 0x1000+uint32(r))
	}
	in = le.AppendUint32(in, 1)
	in = le.AppendUint32(in, 0x40)
	in = le.AppendUint32(in, 1)
	in = le.AppendUint32(in, 77)
	in = le.AppendUint64(in, 5)
	in = append(in, 1, 2, 3, 4, 5)
	base := m.StateDigest()
	if base != sha256.Sum256(in) {
		t.Errorf("StateDigest = %x, want the SHA-256 of the documented encoding", base)
	}

	// Each part of the state changes the digest.
	changes := map[string]func(m *Machine){
		"pc":                      func(m *Machine) { m.PC++ },
		"the first byte":          func(m *Machine) { m.Mem[0]++ },
		"the last byte":           func(m *Machine) { m.Mem[4]++ },
		"the length of memory":    func(m *Machine) { m.Mem = m.Mem[:4] },
		"the reservation":         func(m *Machine) { m.reserved, m.resAddr = false, 0 },
		"the reservation address": func(m *Machine) { m.resAddr = 0x44 },
		"the halt":                func(m *Machine) { m.halted, m.exit = false, 0 },
		"the exit code":           func(m *Machine) { m.exit = 78 },
	}
	for r := 1; r < 32; r++ {
		changes[fmt.Sprintf("x%d", r)] = func(m *Machine) { m.X[r] ^= 0x80000000 }
	}
	seen := map[[32]byte]string{base: "the base state"}
	for name, change := range changes {
		c := *m
		c.Mem = bytes.Clone(m.Mem)
		change(&c)
		d := c.StateDigest()
		if other, ok := seen[d]; ok {
			t.Errorf("a change of %s gives the digest of %s", name, other)
		}
		seen[d] = name
	}

	// The digest does not cover Steps, the streams, the decode cache, or a
	// value that the caller wrote to x0. StateDigest changes nothing.
	c := *m
	c.Mem = bytes.Clone(m.Mem)
	c.Steps, c.In, c.Out, c.cache, c.noCache, c.eof = 99, strings.NewReader("x"), io.Discard, make([]insn, 4), true, true
	c.X[0] = 5
	if c.StateDigest() != base {
		t.Errorf("the digest covers a value that is not machine state")
	}
	if m.StateDigest() != base || m.PC != 0x12345678 || m.X[1] != 0x1001 || !bytes.Equal(m.Mem, []byte{1, 2, 3, 4, 5}) {
		t.Errorf("StateDigest changed the machine")
	}
}
