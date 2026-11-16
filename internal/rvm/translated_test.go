//go:build !rvmbootstrap

package rvm

import (
	"bytes"
	"errors"
	"io"
	"math/rand/v2"
	"testing"
)

// These tests hold translated code to the interpreter. The interpreter
// defines the machine, and the other tests of this package hold it to the
// RISC-V specification. So a test here runs one program twice from one
// state, once with translated code and once without, and requires the same
// state after: pc, registers, memory, reservation, halt, the count of
// instructions, and the error.

// counted returns a copy of c that adds the instructions that it executes
// to *n.
func counted(c *Code, n *uint64) *Code {
	cc := *c
	cc.Run = func(x *[32]uint32, mem []byte, pc uint32, budget uint64) (uint32, uint64) {
		next, steps := c.Run(x, mem, pc, budget)
		*n += steps
		if steps > budget {
			panic("translated code executed more than its budget")
		}
		return next, steps
	}
	return &cc
}

// same fails the test if the two machines differ in any part of their state,
// or the two errors differ.
func same(t *testing.T, what string, a, b *Machine, errA, errB error) {
	t.Helper()
	text := func(err error) string {
		if err == nil {
			return "no error"
		}
		return err.Error()
	}
	switch {
	case text(errA) != text(errB):
		t.Fatalf("%s: the interpreter gives %q; with translated code the machine gives %q", what, text(errA), text(errB))
	case a.PC != b.PC || a.Steps != b.Steps:
		t.Fatalf("%s: the interpreter is at pc %#x after %d instructions; with translated code the machine is at %#x after %d", what, a.PC, a.Steps, b.PC, b.Steps)
	case a.X != b.X:
		for r := range a.X {
			if a.X[r] != b.X[r] {
				t.Fatalf("%s: x%d is %#x in the interpreter and %#x with translated code", what, r, a.X[r], b.X[r])
			}
		}
	case !bytes.Equal(a.Mem, b.Mem):
		for i := range a.Mem {
			if a.Mem[i] != b.Mem[i] {
				t.Fatalf("%s: the byte at %#x is %#x in the interpreter and %#x with translated code", what, i, a.Mem[i], b.Mem[i])
			}
		}
	case a.reserved != b.reserved || a.resAddr != b.resAddr || a.halted != b.halted || a.exit != b.exit:
		t.Fatalf("%s: the reservation or the halt state differs", what)
	}
}

// Every instruction that Translate translates, from random states. The
// registers hold values at the edges of the operations, addresses at the
// edges of memory and of the code, and random values. The budget of a run is
// random too, so that a run also stops inside what follows.
func TestTranslatedOps(t *testing.T) {
	mem, lo, hi, slots := opsProgram()
	r := rand.New(rand.NewPCG(21, 22))
	pool := []uint32{
		0, 1, 2, 3, 4, 5, 7, 8, 15, 16, 31, 32, 33, 63, 64, 0xff, 0x100, 0x7ff, 0x800, 0xffff, 0x10000,
		0x7fffffff, 0x80000000, 0x80000001, 0xfffffffe, 0xffffffff, 0xfffff800,
		opsSize, opsSize - 1, opsSize - 2, opsSize - 4, opsSize - 8, lo, hi, 0x100, 0x1c00,
	}
	value := func() uint32 {
		switch r.IntN(6) {
		case 0:
			return r.Uint32()
		case 1:
			return slots[r.IntN(len(slots))].pc + uint32(r.IntN(5))*2 // an address of code
		}
		return pool[r.IntN(len(pool))] + uint32(r.IntN(9)) - 4
	}
	var total, inCode, completed uint64
	for _, s := range slots {
		for trial := 0; trial < 250; trial++ {
			var x [32]uint32
			for i := 1; i < 32; i++ {
				x[i] = value()
				if trial%5 == 0 {
					// Pairs of the few values at which an operation has a
					// special result: the most negative number, -1, 0.
					x[i] = [...]uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff}[r.IntN(5)]
				}
			}
			var here uint64
			a := &Machine{PC: s.pc, X: x, Mem: bytes.Clone(mem)}
			b := &Machine{PC: s.pc, X: x, Mem: bytes.Clone(mem)}
			if err := b.UseCode(counted(opsCode, &here)); err != nil {
				t.Fatal(err)
			}
			budget := uint64(1 + r.IntN(24))
			_, errA := a.Run(budget)
			_, errB := b.Run(budget)
			same(t, "one instruction", a, b, errA, errB)
			total, inCode = total+a.Steps, inCode+here

			// The code must execute the instruction of the slot whenever the
			// instruction completes, and the code is the one to execute it:
			// the interpreter takes the instructions that Translate leaves
			// to it, and every store into the code.
			d := s.d
			mine := classOf(d.op) != clsStop
			if d.op >= opSb && d.op <= opSw {
				addr, size := uint64(x[d.rs1]+d.imm), uint64(1)<<(d.op-opSb)
				mine = !(addr < uint64(hi) && addr+size > uint64(lo))
			}
			switch {
			case a.Steps == 0:
				// The instruction faulted.
			case mine && here == 0:
				t.Fatalf("the code did not execute the instruction at %#x (operation %d), which completed", s.pc, d.op)
			case !mine && b.Steps == here:
				t.Fatalf("the code executed the instruction at %#x (operation %d), which it must leave to the interpreter", s.pc, d.op)
			case mine:
				completed++
			}
		}
	}
	if inCode*2 < total || completed < uint64(len(slots))*100 {
		t.Errorf("weak test: translated code executed %d of %d instructions, and the instruction of the slot in %d runs", inCode, total, completed)
	}
	t.Logf("%d instructions on %d slots, %d of them in translated code; the instruction of the slot completed in translated code in %d runs", total, len(slots), inCode, completed)
}

// A program that stores into its own code. The store goes to the
// interpreter, the machine stops using the code, and the new instruction
// runs.
func TestTranslatedStoreIntoCode(t *testing.T) {
	mem, _, _, slots := opsProgram()
	var store *opSlot
	for i := range slots {
		if d := slots[i].d; d.op == opSw && d.rs1 == 11 && d.rs2 == 12 && d.imm == 0 {
			store = &slots[i]
		}
	}
	if store == nil {
		t.Fatal("the program has no sw x12, 0(x11)")
	}
	run := func(code *Code) (*Machine, error) {
		m := &Machine{PC: store.pc, Mem: bytes.Clone(mem)}
		m.X[11] = store.pc + 4                  // the c.ebreak after the store
		m.X[12] = encI(0x13, 7, 0, 0, 13)       // addi x13, x0, 7
		if err := m.UseCode(code); err != nil { // a nil code is no code
			t.Fatal(err)
		}
		_, err := m.Run(2)
		return m, err
	}
	var inCode uint64
	a, errA := run(nil)
	b, errB := run(counted(opsCode, &inCode))
	same(t, "a store into code", a, b, errA, errB)
	if !errors.Is(errA, ErrStepLimit) || a.X[13] != 7 || a.Steps != 2 {
		t.Fatalf("the interpreter did not run the new instruction: x13 = %d after %d instructions, %v", a.X[13], a.Steps, errA)
	}
	if inCode != 0 || b.code != nil {
		t.Fatalf("translated code executed %d instructions, and the machine still uses it: %v", inCode, b.code != nil)
	}
}

func TestUseCode(t *testing.T) {
	mem, _, _, _ := opsProgram()
	m := &Machine{Mem: bytes.Clone(mem)}
	if err := m.UseCode(opsCode); err != nil {
		t.Fatal(err)
	}
	// One byte of the code changed: the code is for another program.
	m.Mem[opsCode.Lo+9] ^= 1
	if err := m.UseCode(opsCode); err == nil {
		t.Error("UseCode took code for another program")
	}
	// A memory that does not hold the code.
	if err := (&Machine{Mem: make([]byte, opsCode.Hi-1)}).UseCode(opsCode); err == nil {
		t.Error("UseCode took code that does not fit the memory")
	}
	if err := m.UseCode(nil); err != nil || m.code != nil {
		t.Error("UseCode(nil) did not remove the code")
	}
}

// A whole program: the guest of the other tests, which computes with 64-bit
// and 128-bit numbers, hashes, copies memory, calls through tables, uses
// atomic instructions, and reads and writes its streams. Its output is
// checked against values that Go computes. With translated code the machine
// must pass through the states of the interpreter: the test stops both
// machines at random counts of instructions and compares them.
func TestTranslatedGuest(t *testing.T) {
	image := loadGuest(t)
	for _, seed := range []uint64{1, 2} {
		g := newGuestWorkload(seed)
		ref, refOut, refExit := runGuest(t, g.in)

		var inCode uint64
		code := counted(guestCode, &inCode)
		load := func(c *Code) (*Machine, *bytes.Buffer) {
			m, err := Load(image, guestMem)
			if err != nil {
				t.Fatal(err)
			}
			if err := m.UseCode(c); err != nil {
				t.Fatal(err)
			}
			out := &bytes.Buffer{}
			m.In, m.Out = bytes.NewReader(g.in), out
			return m, out
		}

		// One run to the halt.
		m, out := load(code)
		exit, err := m.Run(guestStepLimit)
		if err != nil || exit != refExit {
			t.Fatalf("seed %d: with translated code the guest ends with exit code %#x, %v", seed, exit, err)
		}
		checkGuestOutput(t, g, out.Bytes())
		same(t, "the guest at its halt", ref, m, nil, nil)
		if !bytes.Equal(out.Bytes(), refOut) || m.StateDigest() != ref.StateDigest() {
			t.Fatalf("seed %d: the output or the state digest differs", seed)
		}
		if inCode*100 < m.Steps*95 {
			t.Errorf("seed %d: translated code executed %d of %d instructions", seed, inCode, m.Steps)
		}

		// The same run in pieces of random length.
		r := rand.New(rand.NewPCG(seed, 5))
		a, outA := load(nil)
		b, outB := load(guestCode)
		for stops := 0; ; stops++ {
			k := uint64(1 + r.IntN(int(ref.Steps/40)))
			if stops%5 == 0 {
				k = uint64(1 + r.IntN(40))
			}
			_, errA := a.Run(k)
			_, errB := b.Run(k)
			same(t, "the guest at a stop", a, b, errA, errB)
			if !bytes.Equal(outA.Bytes(), outB.Bytes()) {
				t.Fatalf("seed %d: the output differs after %d instructions", seed, a.Steps)
			}
			if errA == nil {
				if a.Steps != ref.Steps {
					t.Fatalf("seed %d: the run in pieces took %d instructions, and the run in one took %d", seed, a.Steps, ref.Steps)
				}
				t.Logf("seed %d: %d instructions, %d in translated code; the same state at %d stops", seed, m.Steps, inCode, stops+1)
				break
			}
			if !errors.Is(errA, ErrStepLimit) {
				t.Fatalf("seed %d: %v", seed, errA)
			}
		}
	}
}

// trickle is an input stream that has a few bytes at a time and reports
// ErrNoInput in between, as a host does that receives its input in parts.
type trickle struct {
	data []byte
	r    *rand.Rand
	dry  bool
}

func (s *trickle) Read(p []byte) (int, error) {
	if s.dry = !s.dry; s.dry {
		return 0, ErrNoInput
	}
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.data[:min(len(s.data), 1+s.r.IntN(7))])
	s.data = s.data[n:]
	if s.r.IntN(2) == 0 {
		return n, ErrNoInput // some bytes, and no more for now
	}
	return n, nil
}

// A host that has no input yet stops the machine before the read call, and
// the machine continues later. The guest must see the same bytes, and the
// run must take the same count of instructions, as with all the input from
// the start: with translated code and without.
func TestNoInputYet(t *testing.T) {
	g := newGuestWorkload(1)
	ref, refOut, _ := runGuest(t, g.in)
	for _, code := range []*Code{nil, guestCode} {
		m, err := Load(loadGuest(t), guestMem)
		if err != nil {
			t.Fatal(err)
		}
		if err := m.UseCode(code); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		m.In, m.Out = &trickle{data: g.in, r: rand.New(rand.NewPCG(7, 7))}, &out
		waits := 0
		for {
			_, err := m.Run(guestStepLimit)
			if err == nil {
				break
			}
			if !errors.Is(err, ErrNoInput) {
				t.Fatal(err)
			}
			// The read call has not happened: the pc is at the call, and a
			// run from here that still has no input changes nothing.
			if waits++; waits%50 == 0 {
				dry := *m
				dry.In = &trickle{dry: false}
				dry.Mem = bytes.Clone(m.Mem)
				if _, err := dry.Run(guestStepLimit); !errors.Is(err, ErrNoInput) || dry.PC != m.PC || dry.Steps != m.Steps || dry.X != m.X || !bytes.Equal(dry.Mem, m.Mem) {
					t.Fatalf("a run with no input changed the machine: %v; pc %#x %#x, steps %d %d, x %v, mem %v", err, dry.PC, m.PC, dry.Steps, m.Steps, dry.X == m.X, bytes.Equal(dry.Mem, m.Mem))
				}
			}
		}
		same(t, "the guest with input in parts", ref, m, nil, nil)
		if !bytes.Equal(out.Bytes(), refOut) {
			t.Fatal("the output differs")
		}
		if waits < 100 {
			t.Fatalf("weak test: the machine waited for input %d times", waits)
		}
	}
}
