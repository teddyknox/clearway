package rvm

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// newTest returns a machine with tSize bytes of memory, the given code at
// tCode, and a different value in every register but x0.
func newTest(code []byte) *Machine {
	m := &Machine{Mem: make([]byte, tSize), PC: tCode}
	copy(m.Mem[tCode:], code)
	for r := 1; r < 32; r++ {
		m.X[r] = 0x01010101 * uint32(r)
	}
	return m
}

// wantFault checks that err is a *Fault with the given fields.
func wantFault(t *testing.T, name string, err error, pc uint32, step uint64, reason string) {
	t.Helper()
	var f *Fault
	if !errors.As(err, &f) {
		t.Errorf("%s: error %v, want a fault %q", name, err, reason)
		return
	}
	if want := (Fault{PC: pc, Step: step, Reason: reason}); *f != want {
		t.Errorf("%s: fault %+v, want %+v", name, *f, want)
	}
}

// wantIllegal checks that each word faults as an illegal instruction and
// changes no state. A word whose two low bits are not both 1 is a 16-bit
// code; the test puts a c.nop after it.
func wantIllegal(t *testing.T, words map[string]uint32) {
	t.Helper()
	for name, word := range words {
		reason := fmt.Sprintf("illegal instruction 0x%08x", word)
		code := le32(word)
		if word&3 != 3 {
			if word >= 0x10000 {
				t.Fatalf("%s: %#x is not a 16-bit code", name, word)
			}
			reason = fmt.Sprintf("illegal instruction 0x%04x", word)
			code = []byte{byte(word), byte(word >> 8), 0x01, 0x00}
		}
		for _, noCache := range []bool{false, true} {
			m := newTest(code)
			m.noCache = noCache
			before := m.StateDigest()
			wantFault(t, name, m.Step(), tCode, 0, reason)
			_, err := m.Run(10)
			wantFault(t, name, err, tCode, 0, reason)
			if m.StateDigest() != before || m.Steps != 0 {
				t.Errorf("%s: the fault changed the state", name)
			}
		}
	}
}

func TestIllegalInstructions(t *testing.T) {
	wantIllegal(t, map[string]uint32{
		"all zeros":                  0x0000,
		"all ones":                   0xffffffff,
		"jalr with funct3 1":         0x00051067,
		"branch with funct3 2":       0x00002063,
		"branch with funct3 3":       0x00003063,
		"ld":                         0x00053503,
		"lwu":                        0x00056503,
		"load with funct3 7":         0x00057503,
		"sd":                         0x00a53023,
		"store with funct3 4":        0x00a54023,
		"store with funct3 7":        0x00a57023,
		"slli by 32":                 0x02051513,
		"slli with funct7 0x20":      0x40051513,
		"srli by 32":                 0x02055513,
		"srai by 32":                 0x42055513,
		"srli with funct7 0x10":      0x20055513,
		"sll with funct7 0x20":       0x40b51533,
		"add with funct7 2":          0x04b50533,
		"add with funct7 0x21":       0x42b50533,
		"add with funct7 0x7f":       0xfeb50533,
		"sub with funct3 7":          0x40b57533,
		"addiw":                      0x0015051b,
		"addw":                       0x00b5053b,
		"mulw":                       0x02b5053b,
		"amoadd.d":                   0x00c5b52f,
		"amoadd with funct3 0":       0x00c5852f,
		"lr.w with rs2 1":            0x1015a52f,
		"AMO with funct5 5":          0x28c5a52f,
		"AMO with funct5 9":          0x48c5a52f,
		"AMO with funct5 0x1f":       0xf8c5a52f,
		"fence.i":                    0x0000100f,
		"MISC-MEM with funct3 2":     0x0000200f,
		"csrrw":                      0x30001073,
		"csrrs":                      0x30002573,
		"csrrc":                      0x30003073,
		"csrrwi":                     0x30005073,
		"csrrsi":                     0x30006073,
		"csrrci":                     0x30007073,
		"rdcycle":                    0xc0002573,
		"mret":                       0x30200073,
		"sret":                       0x10200073,
		"uret":                       0x00200073,
		"wfi":                        0x10500073,
		"ecall with rd 1":            0x000000f3,
		"ecall with rs1 1":           0x00008073,
		"ebreak with rd 2":           0x00100173,
		"SYSTEM with funct3 4":       0x00004073,
		"custom-0":                   0x0000000b,
		"custom-1":                   0x0000002b,
		"custom-2":                   0x0000005b,
		"custom-3":                   0x0000007b,
		"OP-V":                       0x00000057,
		"opcode 0x77":                0x00000077,
		"48-bit instruction":         0x0000001f,
		"64-bit instruction":         0x0000003f,
		"48-bit instruction, 0x5f":   0x0000005f,
		"80-bit instruction":         0x0000007f,
		"c.addi4spn with 0":          0x0004,
		"c.addi4spn with 0, a5":      0x001c,
		"compressed, quadrant 0, 4":  0x8000,
		"the same with every bit":    0x9ffc,
		"c.addi16sp with 0":          0x6101,
		"c.lui with 0":               0x6501,
		"c.lui x0 with 0":            0x6001,
		"c.srli by 33":               0x9105,
		"c.srai by 33":               0x9505,
		"c.slli by 33":               0x1506,
		"c.subw":                     0x9d0d,
		"c.addw":                     0x9d2d,
		"compressed, reserved, 0x40": 0x9d4d,
		"compressed, reserved, 0x60": 0x9d6d,
		"c.lwsp to x0":               0x4012,
		"c.jr x0":                    0x8002,
	})
}

func TestFloatInstructionsFault(t *testing.T) {
	wantIllegal(t, map[string]uint32{
		"flw":       0x00052507,
		"fld":       0x00053507,
		"fsw":       0x00a52027,
		"fsd":       0x00a53027,
		"fadd.s":    0x00b57553,
		"fadd.d":    0x02b57553,
		"fmadd.s":   0x60b57543,
		"fmsub.s":   0x60b57547,
		"fnmsub.s":  0x60b5754b,
		"fnmadd.s":  0x60b5754f,
		"fcvt.w.s":  0xc0057553,
		"fmv.x.w":   0xe0050553,
		"feq.s":     0xa0b52553,
		"c.fld":     0x2000,
		"c.fld, 1":  0x3ffc,
		"c.flw":     0x6000,
		"c.flw, 1":  0x7ffc,
		"c.fsd":     0xa000,
		"c.fsd, 1":  0xbffc,
		"c.fsw":     0xe000,
		"c.fsw, 1":  0xfffc,
		"c.fldsp":   0x2002,
		"c.fldsp 1": 0x3ffe,
		"c.flwsp":   0x6002,
		"c.flwsp 1": 0x7ffe,
		"c.fsdsp":   0xa002,
		"c.fsdsp 1": 0xbffe,
		"c.fswsp":   0xe002,
		"c.fswsp 1": 0xfffe,
	})
}

// TestCompressedHints checks the compressed codes that the specification
// names hints: each adds 2 to pc and changes nothing else.
func TestCompressedHints(t *testing.T) {
	for name, word := range map[string]uint16{
		"c.nop with 1": 0x0005,
		"c.addi a0, 0": 0x0501,
		"c.li x0, 5":   0x4015,
		"c.lui x0, 1":  0x6005,
		"c.srli a0, 0": 0x8101,
		"c.srai a0, 0": 0x8501,
		"c.slli x0, 1": 0x0006,
		"c.slli a0, 0": 0x0502,
		"c.mv x0, a1":  0x802e,
		"c.add x0, a1": 0x902e,
	} {
		for _, noCache := range []bool{false, true} {
			m := newTest([]byte{byte(word), byte(word >> 8)})
			m.noCache = noCache
			want := newTest([]byte{byte(word), byte(word >> 8)})
			want.PC += 2
			if err := m.Step(); err != nil {
				t.Errorf("%s: %v", name, err)
			} else if m.StateDigest() != want.StateDigest() {
				t.Errorf("%s: pc = %#x, x = %#x: the hint changed more than pc", name, m.PC, m.X)
			}
		}
	}
}

// TestRegisterZero checks that x0 reads as 0 after the caller wrote another
// value to X[0], and that no instruction changes it.
func TestRegisterZero(t *testing.T) {
	golden := loadGolden(t)
	for _, noCache := range []bool{false, true} {
		m := newTest(code(t, golden, "add a0, zero, a1"))
		m.noCache = noCache
		m.X[0], m.X[a1] = 5, 9
		if err := m.Step(); err != nil || m.X[a0] != 9 || m.X[0] != 0 {
			t.Errorf("Step: a0 = %d, x0 = %d, error %v, want 9, 0, nil", m.X[a0], m.X[0], err)
		}
		m = newTest(code(t, golden, "sw zero, 0(a1)"))
		m.noCache = noCache
		m.X[0], m.X[a1] = 0xffffffff, tData
		copy(m.Mem[tData:], "junk")
		if _, err := m.Run(1); err != ErrStepLimit || !bytes.Equal(m.Mem[tData:tData+4], []byte{0, 0, 0, 0}) || m.X[0] != 0 {
			t.Errorf("Run: memory %x, x0 = %d, error %v", m.Mem[tData:tData+4], m.X[0], err)
		}
		// An instruction with rd = x0, after instructions that write
		// other registers.
		m = newTest(append(append(append([]byte{}, code(t, golden, "addi a0, a1, 1")...),
			code(t, golden, "addi zero, a0, 5")...), code(t, golden, "add a0, zero, a1")...))
		m.noCache = noCache
		m.X[a1] = 9
		if _, err := m.Run(3); err != ErrStepLimit || m.X[a0] != 9 || m.X[0] != 0 {
			t.Errorf("three instructions: a0 = %d, x0 = %d, error %v", m.X[a0], m.X[0], err)
		}
	}
}

// TestFetchFaults checks the pc values at which no instruction can start.
func TestFetchFaults(t *testing.T) {
	golden := loadGolden(t)
	nop := code(t, golden, "c.nop")
	add := code(t, golden, asmAdd1)

	for _, c := range []struct {
		name   string
		pc     uint32
		reason string
	}{
		{"pc at the end of memory", tSize, "fetch out of range"},
		{"pc one byte before the end", tSize - 1, "odd pc"},
		{"pc after the end", tSize + 0x1000, "fetch out of range"},
		{"pc at the top of the address space", 0xfffffffe, "fetch out of range"},
		{"odd pc at the top of the address space", 0xffffffff, "odd pc"},
		{"odd pc in memory", tCode + 1, "odd pc"},
		{"odd pc outside memory", tSize + 1, "odd pc"},
	} {
		for _, noCache := range []bool{false, true} {
			m := newTest(nop)
			m.PC, m.noCache = c.pc, noCache
			before := m.StateDigest()
			wantFault(t, c.name, m.Step(), c.pc, 0, c.reason)
			_, err := m.Run(5)
			wantFault(t, c.name, err, c.pc, 0, c.reason)
			if m.StateDigest() != before || m.Steps != 0 {
				t.Errorf("%s: the fault changed the state", c.name)
			}
			// Run with a limit of 0 fetches nothing.
			if _, err := m.Run(0); err != ErrStepLimit {
				t.Errorf("%s: Run(0) = %v, want ErrStepLimit", c.name, err)
			}
		}
	}

	for _, noCache := range []bool{false, true} {
		// A compressed instruction in the last two bytes executes; the
		// fetch after it faults.
		m := newTest(nil)
		m.noCache = noCache
		copy(m.Mem[tSize-2:], nop)
		m.PC = tSize - 2
		if err := m.Step(); err != nil {
			t.Errorf("c.nop in the last two bytes: %v", err)
		}
		wantFault(t, "pc after the last instruction", m.Step(), tSize, 1, "fetch out of range")

		// A full instruction in the last four bytes executes.
		m = newTest(nil)
		m.noCache = noCache
		copy(m.Mem[tSize-4:], add)
		m.PC = tSize - 4
		before := m.X[a0]
		if err := m.Step(); err != nil || m.X[a0] != before+1 {
			t.Errorf("addi in the last four bytes: a0 = %#x, error %v", m.X[a0], err)
		}

		// A full instruction that starts in the last two bytes does not.
		m = newTest(nil)
		m.noCache = noCache
		copy(m.Mem[tSize-2:], add[:2])
		m.PC = tSize - 2
		wantFault(t, "full instruction across the end of memory", m.Step(), tSize-2, 0, "fetch out of range")

		// A jump out of memory completes; the next fetch faults.
		m = newTest(code(t, golden, "jal ra, 524288"))
		m.noCache = noCache
		_, err := m.Run(10)
		wantFault(t, "jump out of memory", err, tCode+524288, 1, "fetch out of range")
	}

	// A machine with too little memory for one instruction.
	for size := 0; size < 4; size++ {
		m := &Machine{Mem: make([]byte, size)}
		for i := range m.Mem {
			m.Mem[i] = 0xff
		}
		wantFault(t, fmt.Sprintf("memory of %d bytes", size), m.Step(), 0, 0, "fetch out of range")
	}
	var empty Machine
	wantFault(t, "the zero Machine", empty.Step(), 0, 0, "fetch out of range")
}

// ecallMachine returns a machine at an ecall instruction with the given
// call number and arguments.
func ecallMachine(t *testing.T, number, arg0, arg1 uint32) *Machine {
	t.Helper()
	m := newTest(code(t, loadGolden(t), asmEcall))
	m.X[a7], m.X[a0], m.X[a1] = number, arg0, arg1
	return m
}

func TestEcallHalt(t *testing.T) {
	m := ecallMachine(t, sysHalt, 42, 99)
	if halted, code := m.Halted(); halted || code != 0 {
		t.Errorf("Halted() before the halt = %v, %d", halted, code)
	}
	if err := m.Step(); err != nil {
		t.Fatal(err)
	}
	if halted, code := m.Halted(); !halted || code != 42 {
		t.Errorf("Halted() = %v, %d, want true, 42", halted, code)
	}
	if m.PC != tCode+4 || m.Steps != 1 {
		t.Errorf("pc = %#x, Steps = %d after the halt, want %#x, 1", m.PC, m.Steps, tCode+4)
	}
	digest := m.StateDigest()
	if err := m.Step(); err != ErrHalted {
		t.Errorf("Step after the halt = %v, want ErrHalted", err)
	}
	// The exit code is the value of a0 at the halt, whatever a0 holds later.
	m.X[a0] = 7
	if exit, err := m.Run(10); exit != 42 || err != nil {
		t.Errorf("Run after the halt = %d, %v, want 42, nil", exit, err)
	}
	m.X[a0] = 42
	if m.StateDigest() != digest || m.Steps != 1 {
		t.Errorf("calls after the halt changed the state")
	}

	// A halt on the last permitted step is a halt, not a step limit.
	m = newTest(code(t, loadGolden(t), asmHalt))
	if exit, err := m.Run(3); exit != 42 || err != nil || m.Steps != 3 {
		t.Errorf("Run(3) = %d, %v with %d steps, want 42, nil with 3 steps", exit, err, m.Steps)
	}
	m = newTest(code(t, loadGolden(t), asmHalt))
	if _, err := m.Run(2); err != ErrStepLimit {
		t.Errorf("Run(2) = %v, want ErrStepLimit", err)
	}
	if exit, err := m.Run(1); exit != 42 || err != nil {
		t.Errorf("Run(1) after Run(2) = %d, %v, want 42, nil", exit, err)
	}

	// The digest of a halted machine differs from the digest of a machine
	// in the same state that has not halted.
	running := ecallMachine(t, sysHalt, 42, 99)
	running.PC += 4
	halted := ecallMachine(t, sysHalt, 42, 99)
	if err := halted.Step(); err != nil {
		t.Fatal(err)
	}
	if running.StateDigest() == halted.StateDigest() {
		t.Errorf("the state digest does not cover the halt")
	}
}

func TestEcallUnknown(t *testing.T) {
	for _, number := range []uint32{3, 4, 93, 0x80000000, 0xffffffff} {
		m := ecallMachine(t, number, tData, 4)
		m.In = strings.NewReader("input")
		var out bytes.Buffer
		m.Out = &out
		before := m.StateDigest()
		_, err := m.Run(10)
		wantFault(t, "unknown ecall", err, tCode, 0, fmt.Sprintf("unknown ecall %d", number))
		if m.StateDigest() != before || out.Len() != 0 {
			t.Errorf("ecall %d: the fault changed the state", number)
		}
	}
}

// again moves the machine back to its ecall instruction with new arguments.
func again(m *Machine, number, arg0, arg1 uint32) {
	m.PC = tCode
	m.X[a7], m.X[a0], m.X[a1] = number, arg0, arg1
}

// A scribbler is a Reader that writes to all of the buffer that it gets,
// as the contract of io.Reader permits, and delivers one byte.
type scribbler struct{ done bool }

func (s *scribbler) Read(p []byte) (int, error) {
	if s.done {
		return 0, io.EOF
	}
	for i := range p {
		p[i] = 0xee
	}
	s.done = true
	p[0] = 'x'
	return 1, nil
}

// A countReader counts the calls of Read.
type countReader struct {
	r     io.Reader
	calls int
}

func (c *countReader) Read(p []byte) (int, error) {
	c.calls++
	return c.r.Read(p)
}

func TestEcallRead(t *testing.T) {
	const input = "hello, world"
	readers := map[string]func() io.Reader{
		"whole":        func() io.Reader { return strings.NewReader(input) },
		"one byte":     func() io.Reader { return iotest.OneByteReader(strings.NewReader(input)) },
		"half":         func() io.Reader { return iotest.HalfReader(strings.NewReader(input)) },
		"data and EOF": func() io.Reader { return iotest.DataErrReader(strings.NewReader(input)) },
	}
	for name, reader := range readers {
		m := ecallMachine(t, sysRead, tData, 5)
		m.In = reader()
		for i := range 32 {
			m.Mem[tData+i] = '.'
		}
		// A full read.
		if err := m.Step(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if m.X[a0] != 5 || string(m.Mem[tData:tData+8]) != "hello..." || m.PC != tCode+4 {
			t.Errorf("%s: read of 5 bytes: a0 = %d, memory %q, pc %#x", name, m.X[a0], m.Mem[tData:tData+8], m.PC)
		}
		// A read of 0 bytes takes no input, even at an address outside
		// memory.
		again(m, sysRead, 0xffffffff, 0)
		if err := m.Step(); err != nil || m.X[a0] != 0 {
			t.Errorf("%s: read of 0 bytes: a0 = %d, error %v", name, m.X[a0], err)
		}
		// A read that the end of the input cuts short. The bytes after
		// the count stay as they were.
		again(m, sysRead, tData+8, 16)
		if err := m.Step(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if m.X[a0] != 7 || string(m.Mem[tData+8:tData+18]) != ", world..." {
			t.Errorf("%s: short read: a0 = %d, memory %q", name, m.X[a0], m.Mem[tData+8:tData+18])
		}
		// A read at the end of the input.
		again(m, sysRead, tData, 4)
		if err := m.Step(); err != nil || m.X[a0] != 0 || string(m.Mem[tData:tData+5]) != "hello" {
			t.Errorf("%s: read at the end of the input: a0 = %d, error %v", name, m.X[a0], err)
		}
		if m.Steps != 4 {
			t.Errorf("%s: Steps = %d, want 4", name, m.Steps)
		}
	}

	// No input stream is an empty input.
	m := ecallMachine(t, sysRead, tData, 5)
	if err := m.Step(); err != nil || m.X[a0] != 0 {
		t.Errorf("read with a nil In: a0 = %d, error %v", m.X[a0], err)
	}

	// A buffer that is not inside memory faults, and the call reads no
	// input.
	for _, c := range []struct{ addr, count uint32 }{
		{tSize - 2, 4}, {tSize, 1}, {0, tSize + 1}, {0xffffffff, 2}, {4, 0xffffffff}, {0xffffffff, 0xffffffff},
	} {
		m := ecallMachine(t, sysRead, c.addr, c.count)
		in := strings.NewReader(input)
		m.In = in
		before := m.StateDigest()
		_, err := m.Run(10)
		wantFault(t, "read outside memory", err, tCode, 0,
			fmt.Sprintf("read buffer out of range: %d bytes at 0x%08x", c.count, c.addr))
		if m.StateDigest() != before || in.Len() != len(input) {
			t.Errorf("read of %d bytes at %#x: the fault changed the state or took input", c.count, c.addr)
		}
	}
	// A buffer that ends at the end of memory does not fault.
	m = ecallMachine(t, sysRead, tSize-4, 4)
	m.In = strings.NewReader(input)
	if err := m.Step(); err != nil || m.X[a0] != 4 || string(m.Mem[tSize-4:]) != "hell" {
		t.Errorf("read at the end of memory: a0 = %d, error %v", m.X[a0], err)
	}

	// After the end of the input, the machine does not ask the stream
	// again, so a stream that grows later changes nothing.
	var grow bytes.Buffer
	in := &countReader{r: &grow}
	m = ecallMachine(t, sysRead, tData, 4)
	m.In = in
	if err := m.Step(); err != nil || m.X[a0] != 0 {
		t.Errorf("read of an empty stream: a0 = %d, error %v", m.X[a0], err)
	}
	grow.WriteString("late")
	calls := in.calls
	again(m, sysRead, tData, 4)
	if err := m.Step(); err != nil || m.X[a0] != 0 || in.calls != calls {
		t.Errorf("read after the end of the input: a0 = %d, error %v, %d more calls of Read", m.X[a0], err, in.calls-calls)
	}

	// A stream may write to all of its buffer. The guest sees only the
	// bytes that the stream delivers.
	m = ecallMachine(t, sysRead, tData, 8)
	m.In = &scribbler{}
	copy(m.Mem[tData:], "........")
	if err := m.Step(); err != nil || m.X[a0] != 1 || string(m.Mem[tData:tData+8]) != "x......." {
		t.Errorf("read of a stream that writes to all of its buffer: a0 = %d, memory %q, error %v", m.X[a0], m.Mem[tData:tData+8], err)
	}

	// A read that is longer than the buffer between the stream and memory.
	big := make([]byte, 300000)
	for i := range big {
		big[i] = byte(i*7 + i>>8)
	}
	m = &Machine{Mem: make([]byte, 1<<20), PC: tCode}
	copy(m.Mem[tCode:], code(t, loadGolden(t), asmEcall))
	m.X[a7], m.X[a0], m.X[a1] = sysRead, 0x10001, 400000
	m.In = iotest.HalfReader(bytes.NewReader(big))
	if err := m.Step(); err != nil || m.X[a0] != 300000 || !bytes.Equal(m.Mem[0x10001:0x10001+300000], big) || m.Mem[0x10001+300000] != 0 {
		t.Errorf("long read: a0 = %d, error %v", m.X[a0], err)
	}
}

func TestEcallWrite(t *testing.T) {
	m := ecallMachine(t, sysWrite, tData, 5)
	copy(m.Mem[tData:], "hello, world")
	var out bytes.Buffer
	m.Out = &out
	if err := m.Step(); err != nil || m.X[a0] != 5 || out.String() != "hello" || m.PC != tCode+4 {
		t.Errorf("write of 5 bytes: a0 = %d, output %q, error %v", m.X[a0], out.String(), err)
	}
	again(m, sysWrite, tData+5, 7)
	if err := m.Step(); err != nil || m.X[a0] != 7 || out.String() != "hello, world" {
		t.Errorf("second write: a0 = %d, output %q, error %v", m.X[a0], out.String(), err)
	}
	// A write of 0 bytes touches no address.
	again(m, sysWrite, 0xffffffff, 0)
	if err := m.Step(); err != nil || m.X[a0] != 0 || out.String() != "hello, world" {
		t.Errorf("write of 0 bytes: a0 = %d, output %q, error %v", m.X[a0], out.String(), err)
	}
	// A write of the last bytes of memory.
	again(m, sysWrite, tSize-3, 3)
	copy(m.Mem[tSize-3:], "end")
	if err := m.Step(); err != nil || m.X[a0] != 3 || out.String() != "hello, worldend" {
		t.Errorf("write at the end of memory: a0 = %d, output %q, error %v", m.X[a0], out.String(), err)
	}

	// No output stream discards the bytes.
	m = ecallMachine(t, sysWrite, tData, 5)
	if err := m.Step(); err != nil || m.X[a0] != 5 {
		t.Errorf("write with a nil Out: a0 = %d, error %v", m.X[a0], err)
	}

	// A buffer that is not inside memory faults with and without an
	// output stream, and the call writes nothing.
	for _, c := range []struct{ addr, count uint32 }{
		{tSize - 2, 4}, {tSize, 1}, {0, tSize + 1}, {0xffffffff, 2}, {4, 0xffffffff},
	} {
		for _, withOut := range []bool{true, false} {
			m := ecallMachine(t, sysWrite, c.addr, c.count)
			out.Reset()
			if withOut {
				m.Out = &out
			}
			before := m.StateDigest()
			_, err := m.Run(10)
			wantFault(t, "write outside memory", err, tCode, 0,
				fmt.Sprintf("write buffer out of range: %d bytes at 0x%08x", c.count, c.addr))
			if m.StateDigest() != before || out.Len() != 0 {
				t.Errorf("write of %d bytes at %#x: the fault changed the state or wrote output", c.count, c.addr)
			}
		}
	}
}

// A failWriter is a Writer that returns an error.
type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

// TestStreamErrors checks that an error of In or Out stops the machine and
// that the error is neither a fault nor a step limit.
func TestStreamErrors(t *testing.T) {
	boom := errors.New("boom")

	m := ecallMachine(t, sysRead, tData, 5)
	m.In = iotest.ErrReader(boom)
	for range 2 {
		_, err := m.Run(10)
		var f *Fault
		if !errors.Is(err, boom) || errors.As(err, &f) || m.Steps != 0 || m.PC != tCode {
			t.Errorf("read error: Run = %v with %d steps at pc %#x", err, m.Steps, m.PC)
		}
		if err := m.Step(); !errors.Is(err, boom) {
			t.Errorf("read error: Step = %v", err)
		}
		m.In = strings.NewReader("a stream that works")
	}

	m = ecallMachine(t, sysWrite, tData, 5)
	m.Out = failWriter{boom}
	for range 2 {
		if _, err := m.Run(10); !errors.Is(err, boom) || m.Steps != 0 || m.PC != tCode {
			t.Errorf("write error: Run = %v with %d steps at pc %#x", err, m.Steps, m.PC)
		}
		m.Out = io.Discard
	}
}

// TestStepLimit checks that Run stops at its limit and that a later Run
// continues from the same state.
func TestStepLimit(t *testing.T) {
	golden := loadGolden(t)

	// An endless loop.
	m := newTest(code(t, golden, asmSpin))
	for i := 1; i <= 3; i++ {
		if exit, err := m.Run(1000); err != ErrStepLimit || exit != 0 {
			t.Fatalf("Run(1000) = %d, %v, want 0, ErrStepLimit", exit, err)
		}
		if m.Steps != uint64(1000*i) || m.PC != tCode {
			t.Fatalf("after %d calls: Steps = %d, pc = %#x", i, m.Steps, m.PC)
		}
	}
	if halted, _ := m.Halted(); halted {
		t.Errorf("Halted() after a step limit")
	}

	// A counting loop, stopped after every count of steps from 0 to 20.
	for limit := uint64(0); limit <= 20; limit++ {
		m := newTest(code(t, golden, asmCountTo9))
		m.X[a0], m.X[a1] = 0, 9
		_, err := m.Run(limit)
		if limit <= 18 {
			// The loop is nine rounds of two instructions.
			if err != ErrStepLimit || m.Steps != limit || m.X[a0] != uint32(limit+1)/2 {
				t.Errorf("Run(%d) = %v with %d steps and a0 = %d", limit, err, m.Steps, m.X[a0])
			}
		} else {
			// The instruction after the loop is the word 0.
			wantFault(t, "after the loop", err, tCode+8, 18, "illegal instruction 0x0000")
		}
	}

	// The guest program in parts gives the result of the guest program in
	// one run.
	g := newGuestWorkload(1)
	whole, wholeOut, _ := runGuest(t, g.in)
	parts, err := Load(loadGuest(t), guestMem)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	parts.In, parts.Out = bytes.NewReader(g.in), &out
	rng := &prng{state: 7}
	calls := 0
	for {
		limit := uint64(rng.IntN(200000))
		before := parts.Steps
		exit, err := parts.Run(limit)
		calls++
		if err == nil {
			if exit != guestExit {
				t.Errorf("exit code %#x, want %#x", exit, guestExit)
			}
			break
		}
		if err != ErrStepLimit || parts.Steps != before+limit {
			t.Fatalf("Run(%d) = %v with %d steps", limit, err, parts.Steps-before)
		}
	}
	if parts.Steps != whole.Steps || parts.StateDigest() != whole.StateDigest() || !bytes.Equal(out.Bytes(), wholeOut) {
		t.Errorf("a run in %d parts: %d steps, want %d; the state or the output differs", calls, parts.Steps, whole.Steps)
	}
}

// TestStoreIntoCode checks that a store into code that the machine has
// executed changes what the machine executes next, with and without the
// decode cache.
func TestStoreIntoCode(t *testing.T) {
	golden := loadGolden(t)
	add100 := binary.LittleEndian.Uint32(code(t, golden, asmAdd100))

	for _, noCache := range []bool{false, true} {
		// The guest replaces an instruction.
		m := newTest(code(t, golden, asmPatch))
		m.noCache = noCache
		m.X[a0], m.X[a1], m.X[a2], m.X[t0] = 0, add100, 0, tCode
		for m.PC != tCode+uint32(len(code(t, golden, asmPatch))) {
			if err := m.Step(); err != nil {
				t.Fatal(err)
			}
		}
		if m.X[a0] != 101 || m.Steps != 7 {
			t.Errorf("guest store into code: a0 = %d after %d steps, want 101 after 7", m.X[a0], m.Steps)
		}

		// The caller replaces an instruction: a full one by another full
		// one, then by a compressed one, then by another compressed one,
		// then one byte of it.
		m = newTest(code(t, golden, asmAdd1))
		m.noCache = noCache
		m.X[a0] = 0
		for i, c := range []struct {
			code []byte
			want uint32
		}{
			{nil, 1},
			{code(t, golden, asmAdd100), 101},
			{code(t, golden, asmCAdd1), 102},
			{code(t, golden, asmCAdd2), 104},
			{code(t, golden, asmCAdd1)[:1], 105},
			{code(t, golden, asmAdd1), 106},
		} {
			copy(m.Mem[tCode:], c.code)
			m.PC = tCode
			if err := m.Step(); err != nil || m.X[a0] != c.want {
				t.Errorf("caller store into code, step %d: a0 = %d, error %v, want %d", i, m.X[a0], err, c.want)
			}
		}
	}
}

// TestSmallCache runs the guest program with a decode cache of one, two,
// and four entries, in which the instructions displace one another, and
// without the cache. The result must not change.
func TestSmallCache(t *testing.T) {
	g := newGuestRun(3)
	g.sha256(1000)
	g.arith128(4)
	g.atomic(50)
	g.recurse(g.values(100))
	g.exit(guestExit)
	whole, wholeOut, _ := runGuest(t, g.in)

	for _, size := range []int{0, 1, 2, 4} {
		m, err := Load(loadGuest(t), guestMem)
		if err != nil {
			t.Fatal(err)
		}
		if size == 0 {
			m.noCache = true
		} else {
			m.cache = make([]insn, size)
		}
		var out bytes.Buffer
		m.In, m.Out = bytes.NewReader(g.in), &out
		if exit, err := m.Run(guestStepLimit); err != nil || exit != guestExit {
			t.Fatalf("cache of %d entries: Run = %#x, %v", size, exit, err)
		}
		if m.Steps != whole.Steps || m.StateDigest() != whole.StateDigest() || !bytes.Equal(out.Bytes(), wholeOut) {
			t.Errorf("cache of %d entries: the result differs", size)
		}
	}
}
