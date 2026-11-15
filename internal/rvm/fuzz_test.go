package rvm

import (
	"bytes"
	"errors"
	"testing"
)

// fuzzSteps bounds one run of a fuzz target.
const fuzzSteps = 3000

// checkRun checks what must hold after a Run of at most fuzzSteps
// instructions, whatever the program is.
func checkRun(t *testing.T, m *Machine, start uint64, exit uint32, err error) {
	t.Helper()
	if m.X[0] != 0 {
		t.Fatalf("x0 = %#x", m.X[0])
	}
	if m.Steps-start > fuzzSteps {
		t.Fatalf("%d steps, limit %d", m.Steps-start, fuzzSteps)
	}
	halted, code := m.Halted()
	var f *Fault
	switch {
	case err == nil:
		if !halted || code != exit {
			t.Fatalf("Run returned exit code %#x, but Halted() = %v, %#x", exit, halted, code)
		}
	case err == ErrStepLimit:
		if halted || m.Steps-start != fuzzSteps {
			t.Fatalf("step limit after %d steps, halted %v", m.Steps-start, halted)
		}
	case errors.As(err, &f):
		if halted || f.PC != m.PC || f.Step != m.Steps || f.Reason == "" {
			t.Fatalf("fault %+v at pc %#x, step %d, halted %v", *f, m.PC, m.Steps, halted)
		}
		// A fault changes nothing, so the same fault follows.
		digest := m.StateDigest()
		_, err2 := m.Run(fuzzSteps)
		var f2 *Fault
		if !errors.As(err2, &f2) || *f2 != *f || m.StateDigest() != digest {
			t.Fatalf("after the fault %+v, Run = %v", *f, err2)
		}
	default:
		t.Fatalf("Run = %v", err)
	}
}

// FuzzLoadAndRun loads arbitrary bytes as a program and runs it for a
// bounded count of steps. Load and Run must not panic, and the result must
// be a halt, a fault, or the step limit. A second machine without the
// decode cache must reach the same state.
func FuzzLoadAndRun(f *testing.F) {
	golden := loadGolden(f)
	f.Add([]byte{})
	f.Add(testImage())
	f.Add(buildELF(0x1000, segment{vaddr: 0x1000, data: code(f, golden, asmHalt)}))
	f.Add(buildELF(0x1000, segment{vaddr: 0x1000, data: code(f, golden, asmSpin)}))
	f.Add(buildELF(0x1000, segment{vaddr: 0x1000, data: code(f, golden, asmPatch)}))
	f.Add(buildELF(0x1001, segment{vaddr: 0x1000, data: code(f, golden, asmEcall), memsz: 0x100}))
	f.Add(buildELF(0xfffe, segment{vaddr: 0xfffe, data: code(f, golden, "c.nop")}))
	f.Add(buildELF(0x2000, segment{vaddr: 0x1000, data: []byte("data")}, segment{vaddr: 0x2000, data: code(f, golden, asmCountTo9), memsz: 0x1000}))
	guest := loadGuest(f)
	f.Add(guest[:400])

	f.Fuzz(func(t *testing.T, image []byte) {
		const memSize = 1 << 16
		m, err := Load(image, memSize)
		if err != nil {
			if m != nil {
				t.Fatalf("Load returned a machine with the error %v", err)
			}
			return
		}
		if len(m.Mem) != memSize || m.X[2] != memSize {
			t.Fatalf("%d bytes of memory, sp = %#x", len(m.Mem), m.X[2])
		}
		var out bytes.Buffer
		m.In, m.Out = bytes.NewReader(image), &out
		exit, err := m.Run(fuzzSteps)
		checkRun(t, m, 0, exit, err)

		plain, err2 := Load(image, memSize)
		if err2 != nil {
			t.Fatalf("second Load: %v", err2)
		}
		var out2 bytes.Buffer
		plain.In, plain.Out, plain.noCache = bytes.NewReader(image), &out2, true
		exit2, err2 := plain.Run(fuzzSteps)
		if exit != exit2 || (err == nil) != (err2 == nil) || (err != nil && err.Error() != err2.Error()) ||
			m.Steps != plain.Steps || m.StateDigest() != plain.StateDigest() || !bytes.Equal(out.Bytes(), out2.Bytes()) {
			t.Fatalf("with the decode cache: %#x, %v after %d steps; without: %#x, %v after %d steps",
				exit, err, m.Steps, exit2, err2, plain.Steps)
		}
	})
}

// FuzzRunCode runs arbitrary bytes as code, which reaches the decoder and
// the instructions far more often than FuzzLoadAndRun does. The run must
// not panic. A run in parts, a run of single steps, and a run without the
// decode cache must reach the state of the run in one call.
func FuzzRunCode(f *testing.F) {
	for _, c := range loadGolden(f) {
		f.Add(c, uint32(0))
	}
	f.Add([]byte{0x73, 0, 0, 0, 0x73, 0, 0, 0, 0x73, 0, 0, 0}, uint32(0x01020001))

	f.Fuzz(func(t *testing.T, program []byte, seed uint32) {
		const memSize = 1 << 14
		if len(program) > memSize-tCode {
			return
		}
		// Most registers point into memory, so that loads, stores, and
		// jumps often stay inside it. The seed gives the call number and
		// the arguments of an environment call.
		build := func() (*Machine, *bytes.Buffer) {
			m := &Machine{Mem: make([]byte, memSize), PC: tCode}
			copy(m.Mem[tCode:], program)
			for r := 1; r < 32; r++ {
				m.X[r] = tCode + uint32(r)*4*(seed>>24&0xff)
			}
			m.X[a7] = seed & 3
			m.X[a1] = seed >> 8 & 0xffff
			var out bytes.Buffer
			m.In, m.Out = bytes.NewReader(program), &out
			return m, &out
		}

		m, out := build()
		exit, err := m.Run(fuzzSteps)
		checkRun(t, m, 0, exit, err)

		same := func(name string, o *Machine, oout *bytes.Buffer, oerr error) {
			if (err == nil) != (oerr == nil) || (err != nil && err.Error() != oerr.Error()) || m.Steps != o.Steps ||
				m.StateDigest() != o.StateDigest() || !bytes.Equal(out.Bytes(), oout.Bytes()) {
				t.Fatalf("%s: %v after %d steps; in one call: %v after %d steps", name, oerr, o.Steps, err, m.Steps)
			}
		}

		plain, pout := build()
		plain.noCache = true
		_, perr := plain.Run(fuzzSteps)
		same("without the decode cache", plain, pout, perr)

		small, sout := build()
		small.cache = make([]insn, 2)
		var serr error
		for small.Steps < fuzzSteps {
			if _, serr = small.Run(min(7, fuzzSteps-small.Steps)); serr != ErrStepLimit {
				break
			}
		}
		same("in parts of 7 steps", small, sout, serr)

		single, gout := build()
		var gerr error = ErrStepLimit
		for single.Steps < fuzzSteps {
			if gerr = single.Step(); gerr != nil {
				break
			}
			gerr = ErrStepLimit
			if halted, _ := single.Halted(); halted {
				gerr = nil
				break
			}
		}
		same("in single steps", single, gout, gerr)
	})
}
