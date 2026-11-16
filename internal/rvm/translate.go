package rvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"go/format"
	"sort"
	"strconv"
	"strings"
)

// A Code is host code for the instructions of one program. Translate writes
// its source. The interpreter of this package defines the machine; a Code is
// a faster way to reach the same states.
//
// Lo and Hi bound the addresses of the instructions that the code stands
// for: Lo <= address < Hi. Sum is the SHA-256 of those bytes of memory.
//
// Run executes instructions from pc and returns the pc of the next
// instruction and the count of instructions that it executed. The count is
// at most budget. After Run, the registers and the memory are those that the
// interpreter gives after the same count of instructions from the same
// state. Run stops, and leaves the next instruction to the interpreter, when:
//
//   - pc is not the first instruction of a translated block;
//   - the instruction is not an integer computation, a load, a store, a
//     branch, or a jump: an environment call, ebreak, an atomic instruction,
//     or a code that is not an instruction;
//   - the instruction would fault, or would store into Lo to Hi;
//   - the budget is less than the count of instructions in the next block.
//
// Run requires x[0] to be 0 and does not change it. It requires the bytes of
// mem from Lo to Hi to be the bytes that Sum covers.
type Code struct {
	Lo, Hi uint32
	Sum    [32]byte
	Run    func(x *[32]uint32, mem []byte, pc uint32, budget uint64) (next uint32, steps uint64)
}

// UseCode makes the machine execute c where c applies, and interpret every
// other instruction. A nil c makes the machine interpret everything. The
// result of a run does not depend on UseCode: the count of instructions and
// the state after each of them are the same.
//
// UseCode returns an error, and changes nothing, if the bytes of memory from
// c.Lo to c.Hi are not the bytes that c was translated from.
//
// If the guest stores into c.Lo to c.Hi, the machine stops using c. The
// caller must not change those bytes of Mem itself while c is in use; it
// calls UseCode again after such a change.
func (m *Machine) UseCode(c *Code) error {
	if c == nil {
		m.code = nil
		return nil
	}
	if c.Run == nil || c.Lo >= c.Hi || uint64(c.Hi) > uint64(len(m.Mem)) {
		return errors.New("rvm: translated code does not fit the memory of the machine")
	}
	if sha256.Sum256(m.Mem[c.Lo:c.Hi]) != c.Sum {
		return errors.New("rvm: translated code is not for the program in memory")
	}
	m.code = c
	return nil
}

// Limits of the translation. They change the speed of the code and nothing
// else.
const (
	maxBlock = 64   // the most instructions in one block
	chunkAim = 1200 // instructions in one function of the code, about
	chunkMax = 2400 // the most instructions in one function of the code
)

// The classes of instruction, as the translation treats them.
const (
	clsPlain  = iota // changes registers or memory, and goes to the next instruction
	clsBranch        // goes to one of two fixed addresses
	clsJal           // goes to one fixed address
	clsJalr          // goes to an address from a register
	clsStop          // left to the interpreter
)

func classOf(op uint8) int {
	switch {
	case op >= opBeq && op <= opBgeu:
		return clsBranch
	case op == opJal:
		return clsJal
	case op == opJalr:
		return clsJalr
	case op == opIllegal || op == opEcall || op == opEbreak || op >= opLr:
		return clsStop
	}
	return clsPlain
}

// A tblock is a run of instructions that the code executes as one: control
// enters at the first and leaves after the last.
type tblock struct {
	pc    uint32
	ins   []insn
	at    []uint32 // the address of each instruction
	next  uint32   // the address after the last instruction
	chunk int      // the function of the code that holds the block
	index int      // the number of the block in its function, from 1
}

type translator struct {
	mem    []byte
	lo, hi uint32
	leader map[uint32]bool // the first instructions of blocks
	calls  map[uint32]bool // the targets of calls: where a function starts
	work   []uint32
	inside []bool // by (address − lo)/2: a scanned instruction covers these two bytes
}

// fetch decodes the instruction at pc, if all of it lies from lo to hi.
func (t *translator) fetch(pc uint32) (insn, bool) {
	if pc&1 != 0 || pc < t.lo || uint64(pc)+2 > uint64(t.hi) {
		return insn{}, false
	}
	raw := uint32(binary.LittleEndian.Uint16(t.mem[pc:]))
	if raw&3 == 3 {
		if uint64(pc)+4 > uint64(t.hi) {
			return insn{}, false
		}
		raw = binary.LittleEndian.Uint32(t.mem[pc:])
	}
	return decode(raw), true
}

func (t *translator) add(pc uint32) {
	if pc&1 == 0 && pc >= t.lo && pc < t.hi && !t.leader[pc] {
		t.leader[pc] = true
		t.work = append(t.work, pc)
	}
}

// scan follows the instructions from one leader, and adds as leaders the
// addresses that control can reach from them.
func (t *translator) scan(pc uint32) {
	for count := 1; ; count++ {
		d, ok := t.fetch(pc)
		if !ok {
			return
		}
		for a := pc; a < pc+uint32(d.size); a += 2 {
			t.inside[(a-t.lo)/2] = true
		}
		next := pc + uint32(d.size)
		switch classOf(d.op) {
		case clsBranch:
			t.add(pc + d.imm)
			t.add(next)
			return
		case clsJal:
			t.add(pc + d.imm)
			if d.rd != 0 {
				t.calls[pc+d.imm] = true
			}
			t.add(next)
			return
		case clsJalr, clsStop:
			t.add(next)
			return
		}
		pc = next
		if t.leader[pc] {
			return
		}
		if count == maxBlock {
			t.add(pc)
			return
		}
	}
}

func (t *translator) drain() {
	for len(t.work) > 0 {
		pc := t.work[len(t.work)-1]
		t.work = t.work[:len(t.work)-1]
		t.scan(pc)
	}
}

// Translate returns the Go source of a Code for the program image, loaded
// into a machine with memSize bytes of memory. The source is one file of the
// package pkg. It declares the variable name, a *Code, and other names that
// start with name.
//
// The code stands for the instructions of the executable segments of the
// image. It is exact for every block that it holds, because each block is
// decoded from the bytes at its own first address, by the decoder of the
// interpreter. Which blocks it holds changes its speed alone: control that
// reaches any other address goes to the interpreter.
func Translate(image []byte, memSize uint32, pkg, name string) ([]byte, error) {
	m, err := Load(image, memSize)
	if err != nil {
		return nil, err
	}
	if m.codeLo >= m.codeHi {
		return nil, errors.New("rvm: the image has no executable segment")
	}
	return translate(m.Mem, m.codeLo, m.codeHi, m.PC, m.loadEnd, sha256.Sum256(image), pkg, name)
}

// translate is Translate for a memory that already holds the program. The
// instructions lie from lo to hi, the program starts at entry, and its data
// lies below dataEnd.
func translate(mem []byte, lo, hi, entry, dataEnd uint32, imageSum [32]byte, pkg, name string) ([]byte, error) {
	if lo&1 != 0 || lo >= hi || uint64(hi) > uint64(len(mem)) {
		return nil, errors.New("rvm: the code span is not inside memory")
	}
	t := &translator{
		mem: mem, lo: lo, hi: hi,
		leader: map[uint32]bool{}, calls: map[uint32]bool{},
		inside: make([]bool, (hi-lo+1)/2),
	}
	// Leaders: the entry, each word of data that is an address of code (a
	// table of jumps or of functions), and what control reaches from them.
	t.add(entry)
	t.calls[entry] = true
	for a := uint32(0); uint64(a)+4 <= uint64(dataEnd) && uint64(a)+4 <= uint64(len(mem)); a += 4 {
		if a >= lo && a < hi {
			continue
		}
		t.add(binary.LittleEndian.Uint32(mem[a:]))
	}
	t.drain()
	// What no leader reached is code that only a computed jump enters, or
	// padding. Take each such address as a leader too.
	for a := lo; uint64(a)+2 <= uint64(hi); a += 2 {
		if !t.inside[(a-lo)/2] {
			t.add(a)
			t.drain()
		}
	}

	// Blocks, in order of address.
	leaders := make([]uint32, 0, len(t.leader))
	for pc := range t.leader {
		leaders = append(leaders, pc)
	}
	sort.Slice(leaders, func(i, j int) bool { return leaders[i] < leaders[j] })
	var blocks []*tblock
	for _, pc := range leaders {
		b := &tblock{pc: pc, next: pc}
		for len(b.ins) < maxBlock {
			d, ok := t.fetch(b.next)
			if !ok {
				break
			}
			b.ins, b.at = append(b.ins, d), append(b.at, b.next)
			b.next += uint32(d.size)
			if classOf(d.op) != clsPlain || t.leader[b.next] {
				break
			}
		}
		if len(b.ins) > 0 {
			blocks = append(blocks, b)
		}
	}
	if len(blocks) == 0 {
		return nil, errors.New("rvm: the image has no instruction to translate")
	}

	// Functions of the code: runs of blocks. A function ends where a function
	// of the program starts, once it is long enough.
	var chunks [][]*tblock
	size := 0
	for _, b := range blocks {
		if len(chunks) == 0 || size+len(b.ins) > chunkMax || (size >= chunkAim && t.calls[b.pc]) {
			chunks = append(chunks, nil)
			size = 0
		}
		c := len(chunks) - 1
		chunks[c] = append(chunks[c], b)
		b.chunk, b.index = c, len(chunks[c])
		size += len(b.ins)
	}
	if len(chunks) > 0xffff {
		return nil, errors.New("rvm: the program is too large to translate")
	}
	byPC := make(map[uint32]*tblock, len(blocks))
	for _, b := range blocks {
		byPC[b.pc] = b
	}

	e := &emitter{t: t, name: name, byPC: byPC, qual: "rvm."}
	if pkg == "rvm" {
		e.qual = ""
	}
	w := &e.w
	fmt.Fprintf(w, "// Code generated by rvm.Translate from a program with the SHA-256 %x. DO NOT EDIT.\n\n", imageSum)
	fmt.Fprintf(w, "package %s\n\n", pkg)
	if pkg != "rvm" {
		fmt.Fprintf(w, "import \"github.com/teddyknox/clearway/internal/rvm\"\n\n")
	}
	fmt.Fprintf(w, "// %s is host code for the instructions from %#x to %#x of the program:\n// %d instructions in %d blocks.\n", name, lo, hi, e.count(blocks), len(blocks))
	fmt.Fprintf(w, "var %s = &%sCode{\nLo: %#x,\nHi: %#x,\nSum: [32]byte{", name, e.qual, lo, hi)
	for i, v := range sha256.Sum256(mem[lo:hi]) {
		if i > 0 {
			fmt.Fprint(w, ", ")
		}
		fmt.Fprintf(w, "%#02x", v)
	}
	fmt.Fprintf(w, "},\nRun: %sRun,\n}\n\n", name)

	fmt.Fprintf(w, "// %sAt returns the function and the block of the code that start at pc: the\n// number of the function, from 1, in the high 16 bits, and the number of the\n// block in the low 16 bits. It returns 0 if no block starts at pc.\n", name)
	fmt.Fprintf(w, "func %sAt(pc uint32) uint32 {\ni := (pc - %#x) >> 1\nif pc&1 != 0 || pc < %#x || i >= uint32(len(%sTable)) {\nreturn 0\n}\nreturn %sTable[i]\n}\n\n", name, lo, lo, name, name)
	fmt.Fprintf(w, "var %sTable [%d]uint32\n\n", name, (hi-lo+1)/2)
	fmt.Fprintf(w, "func init() {\nfor f, starts := range %sStarts {\nfor b, pc := range starts {\n%sTable[(pc-%#x)>>1] = uint32(f+1)<<16 | uint32(b+1)\n}\n}\n}\n\n", name, name, lo)

	fmt.Fprintf(w, "// %sRun is the Run of %s.\n", name, name)
	fmt.Fprintf(w, "func %sRun(x *[32]uint32, mem []byte, pc uint32, budget uint64) (uint32, uint64) {\nvar n uint64\nfor {\ne := %sAt(pc)\nif e == 0 {\nreturn pc, n\n}\nnext, k, stop := %sFuncs[e>>16-1](x, mem, pc, e&0xffff, budget-n)\npc, n = next, n+k\nif stop {\nreturn pc, n\n}\n}\n}\n\n", name, name, name)

	fmt.Fprintf(w, "var %sFuncs = [...]func(x *[32]uint32, mem []byte, pc, block uint32, budget uint64) (uint32, uint64, bool){\n", name)
	for c := range chunks {
		fmt.Fprintf(w, "%sF%d,\n", name, c+1)
	}
	fmt.Fprintf(w, "}\n\n")
	fmt.Fprintf(w, "// %sStarts holds, for each function of the code, the first address of each of\n// its blocks.\n", name)
	fmt.Fprintf(w, "var %sStarts = [...][]uint32{\n", name)
	for _, c := range chunks {
		fmt.Fprint(w, "{")
		for i, b := range c {
			if i > 0 {
				fmt.Fprint(w, ", ")
			}
			fmt.Fprintf(w, "%#x", b.pc)
		}
		fmt.Fprint(w, "},\n")
	}
	fmt.Fprintf(w, "}\n\n")

	for c, chunk := range chunks {
		e.chunk(c, chunk)
	}
	e.helpers()

	src, err := format.Source(e.w.Bytes())
	if err != nil {
		return nil, fmt.Errorf("rvm: the translation is not valid Go: %w", err)
	}
	return src, nil
}

type emitter struct {
	t    *translator
	name string
	qual string
	byPC map[uint32]*tblock
	w    bytes.Buffer

	div, divu, rem, remu bool // the helpers that the code calls
}

func (e *emitter) count(blocks []*tblock) int {
	n := 0
	for _, b := range blocks {
		n += len(b.ins)
	}
	return n
}

// chunk writes one function of the code.
//
// The registers of the machine stay in x. A block reads a register from x
// once, into a local variable, and writes each new value of a register to x
// at once. So x is current before every instruction, and the code can stop
// before any of them.
func (e *emitter) chunk(c int, blocks []*tblock) {
	w := &e.w
	memory, jalr := false, false
	for _, b := range blocks {
		for _, d := range b.ins {
			jalr = jalr || d.op == opJalr
			memory = memory || (d.op >= opLb && d.op <= opSw)
		}
	}
	fmt.Fprintf(w, "// %sF%d holds the blocks from %#x to %#x.\n", e.name, c+1, blocks[0].pc, blocks[len(blocks)-1].next)
	fmt.Fprintf(w, "func %sF%d(x *[32]uint32, mem []byte, pc, block uint32, budget uint64) (uint32, uint64, bool) {\n", e.name, c+1)
	fmt.Fprintf(w, "var n uint64\nstop := false\n")
	if memory {
		fmt.Fprintf(w, "size := uint64(len(mem))\n")
	}
	if jalr {
		fmt.Fprintf(w, "dispatch:\n")
	}
	fmt.Fprintf(w, "switch block {\n")
	for _, b := range blocks {
		fmt.Fprintf(w, "case %d:\ngoto b%x\n", b.index, b.pc)
	}
	fmt.Fprintf(w, "}\ngoto exit\n")
	for i, b := range blocks {
		var following *tblock
		if i+1 < len(blocks) {
			following = blocks[i+1]
		}
		e.block(c, b, following)
	}
	fmt.Fprintf(w, "exit:\nreturn pc, n, stop\n}\n\n")
}

// leave writes the statements that stop the code before the instruction at
// pc, which is instruction k of its block.
func (e *emitter) leave(pc uint32, k int) string {
	if k == 0 {
		return fmt.Sprintf("pc, stop = %#x, true\ngoto exit\n", pc)
	}
	return fmt.Sprintf("pc, stop = %#x, true\nn += %d\ngoto exit\n", pc, k)
}

// jump writes the statements that go to a fixed address. following is the
// block that comes next in the function.
func (e *emitter) jump(c int, target uint32, following *tblock) string {
	if b := e.byPC[target]; b != nil && b.chunk == c {
		if b == following {
			return ""
		}
		return fmt.Sprintf("goto b%x\n", target)
	}
	return fmt.Sprintf("pc = %#x\ngoto exit\n", target)
}

// reads returns the registers whose values the translation of d reads.
// Register 0 stands for none.
func reads(d insn) (uint8, uint8) {
	switch op := d.op; {
	case op == opJalr, op >= opLb && op <= opLhu:
		return d.rs1, 0
	case op >= opBeq && op <= opBgeu, op >= opSb && op <= opSw:
		return d.rs1, d.rs2
	case d.rd == 0:
		// A computation into register 0 has no effect.
		return 0, 0
	case op >= opAddi && op <= opSrai:
		return d.rs1, 0
	case op >= opAdd && op <= opRemu:
		return d.rs1, d.rs2
	}
	return 0, 0
}

// A blockState says which registers a block holds in local variables.
type blockState struct {
	e        *emitter
	declared [32]bool   // the block has declared the local variable
	valid    [32]bool   // the local variable holds the value of the register
	later    [][32]bool // by instruction: the registers that the rest of the block reads before it writes them
}

// use returns the Go expression for the value of a register. It reads the
// register from x if the block does not hold it.
func (s *blockState) use(r uint8) string {
	if r == 0 {
		return "uint32(0)"
	}
	if !s.valid[r] {
		if s.declared[r] {
			fmt.Fprintf(&s.e.w, "x%d = x[%d]\n", r, r)
		} else {
			fmt.Fprintf(&s.e.w, "x%d := x[%d]\n", r, r)
		}
		s.declared[r], s.valid[r] = true, true
	}
	return fmt.Sprintf("x%d", r)
}

// set gives the register rd of instruction k the value of a Go expression.
// The value goes to x at once. It also goes to a local variable if the rest
// of the block reads it.
func (s *blockState) set(k int, rd uint8, format string, args ...any) {
	if rd == 0 {
		return
	}
	w, value := &s.e.w, fmt.Sprintf(format, args...)
	if !s.later[k][rd] {
		fmt.Fprintf(w, "x[%d] = %s\n", rd, value)
		s.valid[rd] = false
		return
	}
	if s.declared[rd] {
		fmt.Fprintf(w, "x%d = %s\n", rd, value)
	} else {
		// The conversion gives a constant the type of a register.
		fmt.Fprintf(w, "x%d := uint32(%s)\n", rd, value)
	}
	fmt.Fprintf(w, "x[%d] = x%d\n", rd, rd)
	s.declared[rd], s.valid[rd] = true, true
}

func (e *emitter) block(c int, b *tblock, following *tblock) {
	w := &e.w
	s := &blockState{e: e, later: make([][32]bool, len(b.ins))}
	var live [32]bool
	for k := len(b.ins) - 1; k >= 0; k-- {
		d := b.ins[k]
		s.later[k] = live
		if classOf(d.op) == clsStop {
			live = [32]bool{}
			continue
		}
		live[d.rd] = false
		r1, r2 := reads(d)
		live[r1], live[r2] = true, true
		live[0] = false
	}

	fmt.Fprintf(w, "b%x:\n{\n", b.pc)
	defer fmt.Fprintf(w, "}\n")
	// The block needs a budget for the instructions that it executes: all of
	// them, but for a last one that it leaves to the interpreter.
	need := len(b.ins)
	if classOf(b.ins[need-1].op) == clsStop {
		need--
	}
	if need > 0 {
		fmt.Fprintf(w, "if budget-n < %d {\n%s}\n", need, e.leave(b.pc, 0))
	}
	for k, d := range b.ins {
		pc := b.at[k]
		next := pc + uint32(d.size)
		switch classOf(d.op) {
		case clsStop:
			fmt.Fprint(w, e.leave(pc, k))
			return
		case clsBranch:
			a, bb := s.use(d.rs1), s.use(d.rs2)
			var cond string
			switch d.op {
			case opBeq:
				cond = a + " == " + bb
			case opBne:
				cond = a + " != " + bb
			case opBlt:
				cond = "int32(" + a + ") < int32(" + bb + ")"
			case opBge:
				cond = "int32(" + a + ") >= int32(" + bb + ")"
			case opBltu:
				cond = a + " < " + bb
			case opBgeu:
				cond = a + " >= " + bb
			}
			fmt.Fprintf(w, "n += %d\n", k+1)
			fmt.Fprintf(w, "if %s {\n%s}\n", cond, e.jump(c, pc+d.imm, nil))
			fmt.Fprint(w, e.jump(c, next, following))
			return
		case clsJal:
			s.set(k, d.rd, "%#x", next)
			fmt.Fprintf(w, "n += %d\n", k+1)
			fmt.Fprint(w, e.jump(c, pc+d.imm, following))
			return
		case clsJalr:
			// The target comes from the register before the link is
			// written: the two may be one register.
			fmt.Fprintf(w, "pc = (%s + %#x) &^ 1\n", s.use(d.rs1), d.imm)
			s.set(k, d.rd, "%#x", next)
			fmt.Fprintf(w, "n += %d\n", k+1)
			fmt.Fprintf(w, "if e := %sAt(pc); e>>16 == %d {\nblock = e & 0xffff\ngoto dispatch\n}\ngoto exit\n", e.name, c+1)
			return
		}
		e.plain(s, d, pc, k)
	}
	fmt.Fprintf(w, "n += %d\n", len(b.ins))
	fmt.Fprint(w, e.jump(c, b.next, following))
}

// plain writes one instruction that goes to the next instruction. Each
// expression is the expression of the interpreter for the same operation.
func (e *emitter) plain(s *blockState, d insn, pc uint32, k int) {
	w := &e.w
	rd := d.rd
	r1, r2 := reads(d)
	var a, b string
	if r1 != 0 || d.rs1 == 0 {
		a = s.use(d.rs1)
	}
	if r2 != 0 || d.rs2 == 0 {
		b = s.use(d.rs2)
	}
	set := func(format string, args ...any) { s.set(k, rd, format, args...) }
	flag := func(cond string) {
		if rd != 0 {
			fmt.Fprintf(w, "var t%d uint32\nif %s {\nt%d = 1\n}\n", k, cond, k)
			set("t%d", k)
		}
	}
	// A load or a store of several bytes goes through a pointer to an array
	// of that size, byte by byte: the compiler makes one load or one store of
	// it. A call of a function for it would not be inlined in a function as
	// large as these.
	load := func(bytes int, value string) {
		fmt.Fprintf(w, "a%d := %s + %#x\nif uint64(a%d)+%d > size {\n%s}\n", k, a, d.imm, k, bytes, e.leave(pc, k))
		if rd == 0 {
			return
		}
		if bytes > 1 {
			fmt.Fprintf(w, "p%d := (*[%d]byte)(mem[a%d:])\n", k, bytes, k)
		}
		set("%s", strings.ReplaceAll(value, "#", strconv.Itoa(k)))
	}
	store := func(bytes int) {
		fmt.Fprintf(w, "a%d := %s + %#x\nif uint64(a%d)+%d > size || (uint64(a%d) < %#x && uint64(a%d)+%d > %#x) {\n%s}\n",
			k, a, d.imm, k, bytes, k, e.t.hi, k, bytes, e.t.lo, e.leave(pc, k))
		if bytes == 1 {
			fmt.Fprintf(w, "mem[a%d] = uint8(%s)\n", k, b)
			return
		}
		fmt.Fprintf(w, "p%d := (*[%d]byte)(mem[a%d:])\n", k, bytes, k)
		for i := 0; i < bytes; i++ {
			fmt.Fprintf(w, "p%d[%d] = uint8(%s >> %d)\n", k, i, b, 8*i)
		}
	}
	switch d.op {
	case opLui:
		set("%#x", d.imm)
	case opAuipc:
		set("%#x", pc+d.imm)

	// In these values, # stands for the number of the instruction.
	case opLb:
		load(1, "uint32(int32(int8(mem[a#])))")
	case opLbu:
		load(1, "uint32(mem[a#])")
	case opLh:
		load(2, "uint32(int32(int16(uint16(p#[0]) | uint16(p#[1])<<8)))")
	case opLhu:
		load(2, "uint32(p#[0]) | uint32(p#[1])<<8")
	case opLw:
		load(4, "uint32(p#[0]) | uint32(p#[1])<<8 | uint32(p#[2])<<16 | uint32(p#[3])<<24")
	case opSb:
		store(1)
	case opSh:
		store(2)
	case opSw:
		store(4)

	case opAddi:
		set("%s + %#x", a, d.imm)
	case opSlti:
		flag(fmt.Sprintf("int32(%s) < %d", a, int32(d.imm)))
	case opSltiu:
		flag(fmt.Sprintf("%s < %#x", a, d.imm))
	case opXori:
		set("%s ^ %#x", a, d.imm)
	case opOri:
		set("%s | %#x", a, d.imm)
	case opAndi:
		set("%s & %#x", a, d.imm)
	case opSlli:
		set("%s << %d", a, d.imm&31)
	case opSrli:
		set("%s >> %d", a, d.imm&31)
	case opSrai:
		set("uint32(int32(%s) >> %d)", a, d.imm&31)

	case opAdd:
		set("%s + %s", a, b)
	case opSub:
		set("%s - %s", a, b)
	case opSll:
		set("%s << (%s & 31)", a, b)
	case opSlt:
		flag(fmt.Sprintf("int32(%s) < int32(%s)", a, b))
	case opSltu:
		flag(fmt.Sprintf("%s < %s", a, b))
	case opXor:
		set("%s ^ %s", a, b)
	case opSrl:
		set("%s >> (%s & 31)", a, b)
	case opSra:
		set("uint32(int32(%s) >> (%s & 31))", a, b)
	case opOr:
		set("%s | %s", a, b)
	case opAnd:
		set("%s & %s", a, b)

	case opMul:
		set("%s * %s", a, b)
	case opMulh:
		set("uint32(uint64(int64(int32(%s))*int64(int32(%s))) >> 32)", a, b)
	case opMulhsu:
		set("uint32(uint64(int64(int32(%s))*int64(%s)) >> 32)", a, b)
	case opMulhu:
		set("uint32(uint64(%s) * uint64(%s) >> 32)", a, b)
	case opDiv:
		e.div = e.div || rd != 0
		set("%sDiv(%s, %s)", e.name, a, b)
	case opDivu:
		e.divu = e.divu || rd != 0
		set("%sDivu(%s, %s)", e.name, a, b)
	case opRem:
		e.rem = e.rem || rd != 0
		set("%sRem(%s, %s)", e.name, a, b)
	case opRemu:
		e.remu = e.remu || rd != 0
		set("%sRemu(%s, %s)", e.name, a, b)

	case opFence:
		// No operation.
	default:
		panic(fmt.Sprintf("rvm: no translation for operation %d", d.op))
	}
}

// helpers writes the functions for division. Division by zero and the one
// signed overflow have the results that the machine defines.
func (e *emitter) helpers() {
	w := &e.w
	if e.div {
		fmt.Fprintf(w, "func %sDiv(a, b uint32) uint32 {\nswitch {\ncase b == 0:\nreturn 0xffffffff\ncase a == 0x80000000 && b == 0xffffffff:\nreturn 0x80000000\n}\nreturn uint32(int32(a) / int32(b))\n}\n\n", e.name)
	}
	if e.divu {
		fmt.Fprintf(w, "func %sDivu(a, b uint32) uint32 {\nif b == 0 {\nreturn 0xffffffff\n}\nreturn a / b\n}\n\n", e.name)
	}
	if e.rem {
		fmt.Fprintf(w, "func %sRem(a, b uint32) uint32 {\nswitch {\ncase b == 0:\nreturn a\ncase a == 0x80000000 && b == 0xffffffff:\nreturn 0\n}\nreturn uint32(int32(a) %% int32(b))\n}\n\n", e.name)
	}
	if e.remu {
		fmt.Fprintf(w, "func %sRemu(a, b uint32) uint32 {\nif b == 0 {\nreturn a\n}\nreturn a %% b\n}\n\n", e.name)
	}
}
