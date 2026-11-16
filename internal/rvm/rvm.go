package rvm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The environment calls. The guest gives the number in register a7.
const (
	sysHalt  = 0
	sysRead  = 1
	sysWrite = 2
)

// The registers that an environment call uses.
const (
	regA0 = 10
	regA1 = 11
	regA7 = 17
)

// ErrStepLimit is the error of Run when the guest has neither halted nor
// faulted after the given count of instructions.
var ErrStepLimit = errors.New("rvm: step limit reached")

// ErrHalted is the error of Step when the guest has halted.
var ErrHalted = errors.New("rvm: machine has halted")

// A Fault reports an instruction that the machine cannot execute. PC is the
// address of that instruction. Step is the count of instructions that the
// machine executed before it. Reason says which rule the instruction broke;
// it depends on the machine state alone.
type Fault struct {
	PC     uint32
	Step   uint64
	Reason string
}

func (f *Fault) Error() string {
	return fmt.Sprintf("rvm: fault at pc 0x%08x, step %d: %s", f.PC, f.Step, f.Reason)
}

// A faultKind names one rule that an instruction can break.
type faultKind uint8

const (
	faultNone     faultKind = iota
	faultOddPC              // the pc is odd
	faultFetch              // the instruction is not inside memory
	faultIllegal            // the instruction is not in the instruction set
	faultLoad               // a load reads outside memory
	faultStore              // a store writes outside memory
	faultAlign              // an atomic instruction has an address that is not a multiple of 4
	faultEbreak             // the instruction is ebreak
	faultEcall              // the environment call number is unknown
	faultSysRead            // the buffer of a read call is not inside memory
	faultSysWrite           // the buffer of a write call is not inside memory
)

// newFault builds the Fault of an instruction at pc. The arguments a and b
// are: for faultIllegal, the instruction word; for faultEcall, the call
// number; for the memory faults, the address and the count of bytes.
func (m *Machine) newFault(kind faultKind, pc, a, b uint32) *Fault {
	var reason string
	switch kind {
	case faultOddPC:
		reason = "odd pc"
	case faultFetch:
		reason = "fetch out of range"
	case faultIllegal:
		if a&3 == 3 {
			reason = fmt.Sprintf("illegal instruction 0x%08x", a)
		} else {
			reason = fmt.Sprintf("illegal instruction 0x%04x", a&0xffff)
		}
	case faultLoad:
		reason = fmt.Sprintf("load out of range: %d bytes at 0x%08x", b, a)
	case faultStore:
		reason = fmt.Sprintf("store out of range: %d bytes at 0x%08x", b, a)
	case faultAlign:
		reason = fmt.Sprintf("misaligned atomic access at 0x%08x", a)
	case faultEbreak:
		reason = "ebreak"
	case faultEcall:
		reason = fmt.Sprintf("unknown ecall %d", a)
	case faultSysRead:
		reason = fmt.Sprintf("read buffer out of range: %d bytes at 0x%08x", b, a)
	case faultSysWrite:
		reason = fmt.Sprintf("write buffer out of range: %d bytes at 0x%08x", b, a)
	}
	return &Fault{PC: pc, Step: m.Steps, Reason: reason}
}

// Machine is one RV32IMAC hart with flat memory. The package comment defines
// its behaviour.
//
// Load builds a Machine from a program. The caller may read and change the
// exported fields between calls of Run and Step; a Machine with only the
// exported fields set is also valid. A Machine is not safe for use by more
// than one goroutine at a time.
//
// The machine calls In only for a read of 1 byte or more. After In reports
// the end of the input, the machine does not call In again: every later
// read copies 0 bytes. The machine calls Out once for each write of 1 byte
// or more, with a slice of Mem; Out must not keep or change the slice.
type Machine struct {
	PC    uint32
	X     [32]uint32
	Mem   []byte
	In    io.Reader // input stream of ecall 1; nil means empty
	Out   io.Writer // output stream of ecall 2; nil discards
	Steps uint64    // instructions executed so far

	halted   bool
	exit     uint32 // the exit code; 0 until the guest halts
	reserved bool   // lr.w has set a reservation, and no sc.w has cleared it
	resAddr  uint32 // the address of the reservation; 0 when none is set
	eof      bool   // In has reported the end of the input
	hostErr  error  // the first error of In or Out; the machine cannot continue

	// cache holds decoded instructions. The instruction at pc is in entry
	// (pc/2) mod len(cache), and len(cache) is a power of two. An entry is
	// valid for a pc when its field raw equals the four bytes at that pc.
	// The zero entry is valid: the word 0 is not an instruction. So the
	// cache changes the speed of the machine and nothing else.
	cache   []insn
	noCache bool   // decode every instruction afresh; tests set this
	buf     []byte // holds input between In and Mem

	// code is host code for the program, or nil: see UseCode. It changes the
	// speed of the machine and nothing else.
	code *Code

	// What Load found in the image, for Translate: the span of the
	// executable segments, and the end of the last segment.
	codeLo, codeHi, loadEnd uint32
}

// Halted reports whether the guest has halted, and its exit code. The exit
// code is 0 while the guest has not halted.
func (m *Machine) Halted() (bool, uint32) {
	return m.halted, m.exit
}

// Run executes until the guest halts, a fault occurs, or maxSteps more
// instructions have run.
//
// If the guest halts, or has halted before, Run returns its exit code and a
// nil error. If an instruction faults, Run returns a *Fault; the machine
// state is the state before that instruction, so a second call returns an
// equal Fault. If the limit comes first, Run returns ErrStepLimit, and a
// second call continues from the same state. If In or Out returns an error,
// Run returns an error that wraps it, and so does every later call.
func (m *Machine) Run(maxSteps uint64) (exit uint32, err error) {
	if m.hostErr != nil {
		return 0, m.hostErr
	}
	if m.halted {
		return m.exit, nil
	}
	if err := m.run(maxSteps); err != nil {
		return 0, err
	}
	if m.halted {
		return m.exit, nil
	}
	return 0, ErrStepLimit
}

// Step executes one instruction. It always interprets the instruction.
//
// Step returns nil when the instruction completes; a halt is a completed
// instruction. Step returns ErrHalted, and does nothing, if the guest halted
// before the call. A fault and a stream error give the errors that Run
// gives.
func (m *Machine) Step() error {
	if m.hostErr != nil {
		return m.hostErr
	}
	if m.halted {
		return ErrHalted
	}
	return m.interpret(1)
}

// minCacheLen is the cache length of a machine that Load did not build.
const minCacheLen = 1 << 10

// When translated code does not start at the pc, the machine interprets up
// to this many instructions before it tries the code again.
const interpretRun = 32

// run executes at most limit instructions. It stops early at a halt, at a
// fault, and at a stream error. Its contract is that of interpret.
//
// If the machine has translated code, run executes the code where the code
// applies, and interprets the instructions that the code leaves: see Code.
// The states that it passes through are those of the interpreter.
func (m *Machine) run(limit uint64) error {
	if m.code == nil {
		return m.interpret(limit)
	}
	for done := uint64(0); done < limit && !m.halted; {
		m.X[0] = 0
		pc, n := m.code.Run(&m.X, m.Mem, m.PC, limit-done)
		m.PC, m.Steps, done = pc, m.Steps+n, done+n
		if done == limit {
			break
		}
		// The code left the next instruction to the interpreter. If the code
		// executed nothing, no block starts here: interpret a few more.
		k := uint64(1)
		if n == 0 {
			k = min(interpretRun, limit-done)
		}
		before := m.Steps
		if err := m.interpret(k); err != nil {
			return err
		}
		done += m.Steps - before
		if m.code == nil {
			// The guest stored into its own code.
			return m.interpret(limit - done)
		}
	}
	return nil
}

// interpret executes at most limit instructions. It stops early at a halt,
// at a fault, and at a stream error.
//
// The caller makes sure that the guest has not halted and that hostErr is
// nil. interpret returns nil at the limit and at a halt. It adds the count
// of completed instructions to Steps. At a fault it returns a *Fault and
// leaves the state as it was before the faulting instruction.
func (m *Machine) interpret(limit uint64) error {
	if len(m.cache) == 0 {
		m.cache = make([]insn, minCacheLen)
	}
	var (
		cache   = m.cache
		mask    = uint32(len(cache) - 1)
		noCache = m.noCache
		mem     = m.Mem
		size    = uint64(len(mem))
		x       = &m.X
		pc      = m.PC
		n       uint64 // instructions completed in this call
		tail    insn   // the instruction of the slow path

		kind       faultKind
		arg1, arg2 uint32

		// The span of the translated code, or an empty span. A store into
		// it makes the code stale.
		codeLo, codeHi uint64
	)
	if m.code != nil {
		codeLo, codeHi = uint64(m.code.Lo), uint64(m.code.Hi)
	}
	x[0] = 0

loop:
	for n < limit {
		// Fetch and decode.
		var d *insn
		if pc&1 == 0 && uint64(pc)+4 <= size {
			raw := binary.LittleEndian.Uint32(mem[pc:])
			d = &cache[pc>>1&mask]
			if d.raw != raw || noCache {
				*d = decode(raw)
			}
		} else {
			// Fewer than four bytes remain, or the pc is odd.
			if pc&1 != 0 {
				kind = faultOddPC
				break loop
			}
			if uint64(pc)+2 > size {
				kind = faultFetch
				break loop
			}
			raw := uint32(binary.LittleEndian.Uint16(mem[pc:]))
			if raw&3 == 3 {
				kind = faultFetch
				break loop
			}
			tail = decode(raw)
			d = &tail
		}

		// Execute. A case that faults leaves every register and every
		// byte of memory unchanged.
		var (
			a    = x[d.rs1&31]
			b    = x[d.rs2&31]
			imm  = d.imm
			rd   = d.rd & 31
			next = pc + uint32(d.size)
		)
		switch d.op {
		case opLui:
			x[rd] = imm
		case opAuipc:
			x[rd] = pc + imm
		case opJal:
			x[rd] = next
			next = pc + imm
		case opJalr:
			x[rd] = next
			next = (a + imm) &^ 1

		case opBeq:
			if a == b {
				next = pc + imm
			}
		case opBne:
			if a != b {
				next = pc + imm
			}
		case opBlt:
			if int32(a) < int32(b) {
				next = pc + imm
			}
		case opBge:
			if int32(a) >= int32(b) {
				next = pc + imm
			}
		case opBltu:
			if a < b {
				next = pc + imm
			}
		case opBgeu:
			if a >= b {
				next = pc + imm
			}

		case opLb:
			addr := a + imm
			if uint64(addr) >= size {
				kind, arg1, arg2 = faultLoad, addr, 1
				break loop
			}
			x[rd] = uint32(int32(int8(mem[addr])))
		case opLbu:
			addr := a + imm
			if uint64(addr) >= size {
				kind, arg1, arg2 = faultLoad, addr, 1
				break loop
			}
			x[rd] = uint32(mem[addr])
		case opLh:
			addr := a + imm
			if uint64(addr)+2 > size {
				kind, arg1, arg2 = faultLoad, addr, 2
				break loop
			}
			x[rd] = uint32(int32(int16(binary.LittleEndian.Uint16(mem[addr:]))))
		case opLhu:
			addr := a + imm
			if uint64(addr)+2 > size {
				kind, arg1, arg2 = faultLoad, addr, 2
				break loop
			}
			x[rd] = uint32(binary.LittleEndian.Uint16(mem[addr:]))
		case opLw:
			addr := a + imm
			if uint64(addr)+4 > size {
				kind, arg1, arg2 = faultLoad, addr, 4
				break loop
			}
			x[rd] = binary.LittleEndian.Uint32(mem[addr:])

		case opSb:
			addr := a + imm
			if uint64(addr) >= size {
				kind, arg1, arg2 = faultStore, addr, 1
				break loop
			}
			mem[addr] = uint8(b)
			if uint64(addr) < codeHi && uint64(addr)+1 > codeLo {
				m.code, codeHi = nil, 0
			}
		case opSh:
			addr := a + imm
			if uint64(addr)+2 > size {
				kind, arg1, arg2 = faultStore, addr, 2
				break loop
			}
			binary.LittleEndian.PutUint16(mem[addr:], uint16(b))
			if uint64(addr) < codeHi && uint64(addr)+2 > codeLo {
				m.code, codeHi = nil, 0
			}
		case opSw:
			addr := a + imm
			if uint64(addr)+4 > size {
				kind, arg1, arg2 = faultStore, addr, 4
				break loop
			}
			binary.LittleEndian.PutUint32(mem[addr:], b)
			if uint64(addr) < codeHi && uint64(addr)+4 > codeLo {
				m.code, codeHi = nil, 0
			}

		case opAddi:
			x[rd] = a + imm
		case opSlti:
			var v uint32
			if int32(a) < int32(imm) {
				v = 1
			}
			x[rd] = v
		case opSltiu:
			var v uint32
			if a < imm {
				v = 1
			}
			x[rd] = v
		case opXori:
			x[rd] = a ^ imm
		case opOri:
			x[rd] = a | imm
		case opAndi:
			x[rd] = a & imm
		case opSlli:
			x[rd] = a << (imm & 31)
		case opSrli:
			x[rd] = a >> (imm & 31)
		case opSrai:
			x[rd] = uint32(int32(a) >> (imm & 31))

		case opAdd:
			x[rd] = a + b
		case opSub:
			x[rd] = a - b
		case opSll:
			x[rd] = a << (b & 31)
		case opSlt:
			var v uint32
			if int32(a) < int32(b) {
				v = 1
			}
			x[rd] = v
		case opSltu:
			var v uint32
			if a < b {
				v = 1
			}
			x[rd] = v
		case opXor:
			x[rd] = a ^ b
		case opSrl:
			x[rd] = a >> (b & 31)
		case opSra:
			x[rd] = uint32(int32(a) >> (b & 31))
		case opOr:
			x[rd] = a | b
		case opAnd:
			x[rd] = a & b

		case opMul:
			x[rd] = a * b
		case opMulh:
			x[rd] = uint32(uint64(int64(int32(a))*int64(int32(b))) >> 32)
		case opMulhsu:
			x[rd] = uint32(uint64(int64(int32(a))*int64(b)) >> 32)
		case opMulhu:
			x[rd] = uint32(uint64(a) * uint64(b) >> 32)
		case opDiv:
			switch {
			case b == 0:
				x[rd] = 0xffffffff
			case a == 0x80000000 && b == 0xffffffff:
				x[rd] = 0x80000000
			default:
				x[rd] = uint32(int32(a) / int32(b))
			}
		case opDivu:
			if b == 0 {
				x[rd] = 0xffffffff
			} else {
				x[rd] = a / b
			}
		case opRem:
			switch {
			case b == 0:
				x[rd] = a
			case a == 0x80000000 && b == 0xffffffff:
				x[rd] = 0
			default:
				x[rd] = uint32(int32(a) % int32(b))
			}
		case opRemu:
			if b == 0 {
				x[rd] = a
			} else {
				x[rd] = a % b
			}

		case opFence:

		case opLr:
			if a&3 != 0 {
				kind, arg1 = faultAlign, a
				break loop
			}
			if uint64(a)+4 > size {
				kind, arg1, arg2 = faultLoad, a, 4
				break loop
			}
			x[rd] = binary.LittleEndian.Uint32(mem[a:])
			m.reserved, m.resAddr = true, a
		case opSc:
			if a&3 != 0 {
				kind, arg1 = faultAlign, a
				break loop
			}
			if uint64(a)+4 > size {
				kind, arg1, arg2 = faultStore, a, 4
				break loop
			}
			if m.reserved && m.resAddr == a {
				binary.LittleEndian.PutUint32(mem[a:], b)
				if uint64(a) < codeHi && uint64(a)+4 > codeLo {
					m.code, codeHi = nil, 0
				}
				x[rd] = 0
			} else {
				x[rd] = 1
			}
			m.reserved, m.resAddr = false, 0
		case opAmoswap, opAmoadd, opAmoxor, opAmoand, opAmoor, opAmomin, opAmomax, opAmominu, opAmomaxu:
			if a&3 != 0 {
				kind, arg1 = faultAlign, a
				break loop
			}
			if uint64(a)+4 > size {
				kind, arg1, arg2 = faultStore, a, 4
				break loop
			}
			old := binary.LittleEndian.Uint32(mem[a:])
			v := b
			switch d.op {
			case opAmoadd:
				v = old + b
			case opAmoxor:
				v = old ^ b
			case opAmoand:
				v = old & b
			case opAmoor:
				v = old | b
			case opAmomin:
				if int32(old) < int32(b) {
					v = old
				}
			case opAmomax:
				if int32(old) > int32(b) {
					v = old
				}
			case opAmominu:
				if old < b {
					v = old
				}
			case opAmomaxu:
				if old > b {
					v = old
				}
			}
			binary.LittleEndian.PutUint32(mem[a:], v)
			if uint64(a) < codeHi && uint64(a)+4 > codeLo {
				m.code, codeHi = nil, 0
			}
			x[rd] = old

		case opEcall:
			ptr, count := x[regA0], x[regA1]
			switch x[regA7] {
			case sysHalt:
				m.halted, m.exit = true, ptr
				pc = next
				n++
				break loop
			case sysRead:
				// A call for zero bytes touches no address.
				var got uint32
				if count != 0 {
					if uint64(ptr)+uint64(count) > size {
						kind, arg1, arg2 = faultSysRead, ptr, count
						break loop
					}
					var err error
					got, err = m.read(mem[uint64(ptr) : uint64(ptr)+uint64(count)])
					if uint64(ptr) < codeHi && uint64(ptr)+uint64(count) > codeLo {
						m.code, codeHi = nil, 0
					}
					if err != nil {
						m.hostErr = fmt.Errorf("rvm: input stream: %w", err)
						break loop
					}
				}
				x[regA0] = got
			case sysWrite:
				if count != 0 {
					if uint64(ptr)+uint64(count) > size {
						kind, arg1, arg2 = faultSysWrite, ptr, count
						break loop
					}
					if m.Out != nil {
						if _, err := m.Out.Write(mem[uint64(ptr) : uint64(ptr)+uint64(count)]); err != nil {
							m.hostErr = fmt.Errorf("rvm: output stream: %w", err)
							break loop
						}
					}
				}
				x[regA0] = count
			default:
				kind, arg1 = faultEcall, x[regA7]
				break loop
			}

		case opEbreak:
			kind = faultEbreak
			break loop

		default: // opIllegal
			kind, arg1 = faultIllegal, d.raw
			break loop
		}

		x[0] = 0
		pc = next
		n++
	}

	m.PC = pc
	m.Steps += n
	if kind != faultNone {
		return m.newFault(kind, pc, arg1, arg2)
	}
	return m.hostErr
}

// read copies bytes from the input stream into p, which is a part of guest
// memory, and returns the count of bytes that it copied.
//
// read fills p unless the input ends first. After the input has ended, read
// copies nothing and does not call In again. A byte of p that read does not
// count stays unchanged: read gives In a buffer of its own, because a Reader
// may write to all of the buffer that it gets. If In returns an error other
// than the end of the input, read returns that error; the bytes that In
// delivered before the error are lost.
func (m *Machine) read(p []byte) (uint32, error) {
	if m.In == nil || m.eof {
		return 0, nil
	}
	if m.buf == nil {
		m.buf = make([]byte, 1<<16)
	}
	total := 0
	for total < len(p) {
		chunk := m.buf[:min(len(m.buf), len(p)-total)]
		got, err := io.ReadFull(m.In, chunk)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			m.eof = true
		} else if err != nil {
			return 0, err
		}
		copy(p[total:], chunk[:got])
		total += got
		if m.eof {
			break
		}
	}
	return uint32(total), nil
}
