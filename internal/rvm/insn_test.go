package rvm

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"testing"
)

// The memory layout of the instruction tests.
const (
	tSize = 0x4000 // the size of memory
	tCode = 0x1000 // the address of the snippet
	tData = 0x2000 // the middle of the data that loads read
)

// The register numbers, by ABI name.
const (
	zero = iota
	ra
	sp
	gp
	tp
	t0
	t1
	t2
	s0
	s1
	a0
	a1
	a2
	a3
	a4
	a5
	a6
	a7
	s2
	s3
	s4
	s5
	s6
	s7
	s8
	s9
	s10
	s11
	t3
	t4
	t5
	t6
)

// junk is the value of a destination register before a test, so that an
// instruction that does not write its result fails the test.
const junk = 0xa5a5a5a5

type regs map[int]uint32

type mem map[uint32][]byte

// An insnTest runs one snippet from a given state and gives the state that
// must follow.
//
// The assembler of the Rust toolchain makes the machine code of asm; see
// build_test.go. An offset in asm is relative to the instruction, so the
// snippet runs at any address.
type insnTest struct {
	name  string
	asm   string
	x     regs    // registers before the run; the others are 0
	mem   mem     // memory before the run, besides the snippet and the data pattern
	steps int     // instructions to complete; 0 means each instruction of the snippet once
	want  regs    // registers that the run changes
	wmem  mem     // memory that the run changes
	pc    *uint32 // pc after the run; nil means the end of the snippet
	fault string  // not empty: the instruction after steps faults with this Reason

	// The state of the reservation after the run.
	reserved bool
	resAddr  uint32
}

// pattern is the byte at addr of the data that the tests load.
func pattern(addr uint32) byte {
	return byte(addr*7+addr>>8) ^ 0x5a
}

// The data pattern covers every address that a 12-bit offset from tData
// reaches.
const (
	patternLo = tData - 0x800
	patternHi = tData + 0x800 + 4
)

// patternAt returns the size bytes of the data pattern at addr.
func patternAt(addr uint32, size int) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = pattern(addr + uint32(i))
	}
	return b
}

// to returns the pc that a test must reach.
func to(pc uint32) *uint32 { return &pc }

func le32(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func flag(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// product returns bits shift to shift+31 of the exact product of a and b.
// Each factor is signed or unsigned.
func product(a, b uint32, signedA, signedB bool, shift uint) uint32 {
	x, y := big.NewInt(int64(a)), big.NewInt(int64(b))
	if signedA {
		x = big.NewInt(int64(int32(a)))
	}
	if signedB {
		y = big.NewInt(int64(int32(b)))
	}
	p := new(big.Int).Mul(x, y)
	p.Rsh(p, shift)
	p.And(p, big.NewInt(0xffffffff))
	return uint32(p.Uint64())
}

// quotient returns the quotient or the remainder of a by b, rounded to
// zero, with the results that RISC-V defines for a zero divisor.
func quotient(a, b uint32, signed, remainder bool) uint32 {
	if b == 0 {
		if remainder {
			return a
		}
		return 0xffffffff
	}
	x, y := big.NewInt(int64(a)), big.NewInt(int64(b))
	if signed {
		x, y = big.NewInt(int64(int32(a))), big.NewInt(int64(int32(b)))
	}
	q, r := new(big.Int).QuoRem(x, y, new(big.Int))
	if remainder {
		q = r
	}
	q.And(q, big.NewInt(0xffffffff))
	return uint32(q.Uint64())
}

// operands are the register values of the generated tests.
var operands = []uint32{0, 1, 2, 31, 32, 33, 0x7fffffff, 0x80000000, 0x80000001, 0xfffffffe, 0xffffffff, 0x12345678, 0x9abcdef0}

// insnTests returns the instruction tests for a snippet at tCode.
func insnTests() []insnTest { return insnTestsAt(tCode) }

// insnTestsAt returns the instruction tests for a snippet at address base.
// The snippets do not depend on base.
func insnTestsAt(base uint32) []insnTest {
	var ts []insnTest
	add := func(tc insnTest) {
		if tc.name == "" {
			tc.name = tc.asm
		}
		ts = append(ts, tc)
	}

	// Register-register operations, on every pair of operands.
	type binop struct {
		name string
		f    func(a, b uint32) uint32
	}
	binops := []binop{
		{"add", func(a, b uint32) uint32 { return a + b }},
		{"sub", func(a, b uint32) uint32 { return a - b }},
		{"sll", func(a, b uint32) uint32 { return a << (b % 32) }},
		{"slt", func(a, b uint32) uint32 { return flag(int32(a) < int32(b)) }},
		{"sltu", func(a, b uint32) uint32 { return flag(a < b) }},
		{"xor", func(a, b uint32) uint32 { return a ^ b }},
		{"srl", func(a, b uint32) uint32 { return a >> (b % 32) }},
		{"sra", func(a, b uint32) uint32 { return uint32(int32(a) >> (b % 32)) }},
		{"or", func(a, b uint32) uint32 { return a | b }},
		{"and", func(a, b uint32) uint32 { return a & b }},
		{"mul", func(a, b uint32) uint32 { return product(a, b, false, false, 0) }},
		{"mulh", func(a, b uint32) uint32 { return product(a, b, true, true, 32) }},
		{"mulhsu", func(a, b uint32) uint32 { return product(a, b, true, false, 32) }},
		{"mulhu", func(a, b uint32) uint32 { return product(a, b, false, false, 32) }},
		{"div", func(a, b uint32) uint32 { return quotient(a, b, true, false) }},
		{"divu", func(a, b uint32) uint32 { return quotient(a, b, false, false) }},
		{"rem", func(a, b uint32) uint32 { return quotient(a, b, true, true) }},
		{"remu", func(a, b uint32) uint32 { return quotient(a, b, false, true) }},
	}
	for _, op := range binops {
		for _, a := range operands {
			for _, b := range operands {
				add(insnTest{
					name: fmt.Sprintf("%s %#x %#x", op.name, a, b),
					asm:  op.name + " a0, a1, a2",
					x:    regs{a0: junk, a1: a, a2: b},
					want: regs{a0: op.f(a, b)},
				})
			}
		}
	}

	// The corner cases of the M extension, with the results written out.
	const minInt = 0x80000000
	for _, c := range []struct {
		op         string
		a, b, want uint32
	}{
		{"div", 7, 0, 0xffffffff},
		{"div", 0, 0, 0xffffffff},
		{"div", minInt, 0, 0xffffffff},
		{"divu", 7, 0, 0xffffffff},
		{"divu", 0, 0, 0xffffffff},
		{"rem", 7, 0, 7},
		{"rem", 0xfffffff9, 0, 0xfffffff9},
		{"rem", minInt, 0, minInt},
		{"remu", 7, 0, 7},
		{"remu", 0xfffffff9, 0, 0xfffffff9},
		{"div", minInt, 0xffffffff, minInt},
		{"rem", minInt, 0xffffffff, 0},
		{"divu", minInt, 0xffffffff, 0},
		{"remu", minInt, 0xffffffff, minInt},
		{"div", minInt, 1, minInt},
		{"rem", minInt, 1, 0},
		{"div", 0xfffffff9, 2, 0xfffffffd}, // -7 / 2 = -3
		{"rem", 0xfffffff9, 2, 0xffffffff}, // -7 rem 2 = -1
		{"div", 7, 0xfffffffe, 0xfffffffd}, // 7 / -2 = -3
		{"rem", 7, 0xfffffffe, 1},          // 7 rem -2 = 1
		{"div", 0xfffffff9, 0xfffffffe, 3}, // -7 / -2 = 3
		{"rem", 0xfffffff9, 0xfffffffe, 0xffffffff},
		{"divu", 0xfffffff9, 2, 0x7ffffffc},
		{"remu", 0xfffffff9, 2, 1},
		{"mul", 0xffffffff, 0xffffffff, 1},
		{"mul", 0x10000, 0x10000, 0},
		{"mulh", 0xffffffff, 0xffffffff, 0},
		{"mulh", minInt, minInt, 0x40000000},
		{"mulh", 0x7fffffff, 0x7fffffff, 0x3fffffff},
		{"mulh", minInt, 0x7fffffff, 0xc0000000},
		{"mulhu", 0xffffffff, 0xffffffff, 0xfffffffe},
		{"mulhu", minInt, minInt, 0x40000000},
		{"mulhsu", 0xffffffff, 0xffffffff, 0xffffffff},
		{"mulhsu", minInt, 0xffffffff, minInt},
		{"mulhsu", 0x7fffffff, 0xffffffff, 0x7ffffffe},
		{"mulhsu", 0xffffffff, 1, 0xffffffff},
		{"mulhsu", 1, 0xffffffff, 0},
	} {
		add(insnTest{
			name: fmt.Sprintf("corner %s %#x %#x", c.op, c.a, c.b),
			asm:  c.op + " a0, a1, a2",
			x:    regs{a0: junk, a1: c.a, a2: c.b},
			want: regs{a0: c.want},
		})
	}

	// One register in two roles, and x0 as a source and as a destination.
	add(insnTest{asm: "add a0, a0, a0", x: regs{a0: 0x80000003}, want: regs{a0: 6}})
	add(insnTest{asm: "sub a0, a0, a0", x: regs{a0: 77}, want: regs{a0: 0}})
	add(insnTest{asm: "mul a1, a1, a2", x: regs{a1: 6, a2: 7}, want: regs{a1: 42}})
	add(insnTest{asm: "div a2, a1, a2", x: regs{a1: 42, a2: 7}, want: regs{a2: 6}})
	add(insnTest{asm: "add a0, zero, a1", x: regs{a0: junk, a1: 9}, want: regs{a0: 9}})
	add(insnTest{asm: "sub a0, zero, a1", x: regs{a0: junk, a1: 9}, want: regs{a0: 0xfffffff7}})
	add(insnTest{asm: "add zero, a1, a2", x: regs{a1: 9, a2: 8}})
	add(insnTest{asm: "mulhu zero, a1, a2", x: regs{a1: 0xffffffff, a2: 0xffffffff}})
	add(insnTest{asm: "addi zero, a0, 5", x: regs{a0: 9}})
	add(insnTest{asm: "lui zero, 0x12345"})
	add(insnTest{asm: "auipc zero, 1"})
	add(insnTest{asm: "add t6, t5, t4", x: regs{t6: junk, t5: 3, t4: 4}, want: regs{t6: 7}})
	add(insnTest{asm: "add ra, sp, gp", x: regs{ra: junk, sp: 3, gp: 4}, want: regs{ra: 7}})

	// Immediate operations, with each bit of the immediate.
	imms := []int32{0, 1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2047, -1, -2, -1024, -2048, 0x555, -0x556}
	type immop struct {
		name string
		f    func(a, imm uint32) uint32
	}
	immops := []immop{
		{"addi", func(a, imm uint32) uint32 { return a + imm }},
		{"slti", func(a, imm uint32) uint32 { return flag(int32(a) < int32(imm)) }},
		{"sltiu", func(a, imm uint32) uint32 { return flag(a < imm) }},
		{"xori", func(a, imm uint32) uint32 { return a ^ imm }},
		{"ori", func(a, imm uint32) uint32 { return a | imm }},
		{"andi", func(a, imm uint32) uint32 { return a & imm }},
	}
	for _, op := range immops {
		for _, imm := range imms {
			for _, a := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xfffffffe, 0xffffffff, 0x12345678} {
				add(insnTest{
					name: fmt.Sprintf("%s %#x %d", op.name, a, imm),
					asm:  fmt.Sprintf("%s a0, a1, %d", op.name, imm),
					x:    regs{a0: junk, a1: a},
					want: regs{a0: op.f(a, uint32(imm))},
				})
			}
		}
	}
	shifts := []uint32{0, 1, 2, 4, 8, 16, 21, 31}
	for _, sh := range shifts {
		for _, a := range []uint32{0x80000001, 0x7fffffff, 0xffffffff, 0x12345678} {
			add(insnTest{name: fmt.Sprintf("slli %#x %d", a, sh), asm: fmt.Sprintf("slli a0, a1, %d", sh),
				x: regs{a0: junk, a1: a}, want: regs{a0: a << sh}})
			add(insnTest{name: fmt.Sprintf("srli %#x %d", a, sh), asm: fmt.Sprintf("srli a0, a1, %d", sh),
				x: regs{a0: junk, a1: a}, want: regs{a0: a >> sh}})
			add(insnTest{name: fmt.Sprintf("srai %#x %d", a, sh), asm: fmt.Sprintf("srai a0, a1, %d", sh),
				x: regs{a0: junk, a1: a}, want: regs{a0: uint32(int32(a) >> sh)}})
		}
	}
	// The results of shifts by 0 and by 31, written out.
	add(insnTest{name: "slli by 31", asm: "slli a0, a1, 31", x: regs{a0: junk, a1: 3}, want: regs{a0: 0x80000000}})
	add(insnTest{name: "srli by 31", asm: "srli a0, a1, 31", x: regs{a0: junk, a1: 0xc0000000}, want: regs{a0: 1}})
	add(insnTest{name: "srai by 31", asm: "srai a0, a1, 31", x: regs{a0: junk, a1: 0xc0000000}, want: regs{a0: 0xffffffff}})
	add(insnTest{name: "srai by 0", asm: "srai a0, a1, 0", x: regs{a0: junk, a1: 0xc0000000}, want: regs{a0: 0xc0000000}})
	add(insnTest{name: "sra by 32", asm: "sra a0, a1, a2", x: regs{a0: junk, a1: 0xc0000000, a2: 32}, want: regs{a0: 0xc0000000}})
	add(insnTest{name: "sll by 63", asm: "sll a0, a1, a2", x: regs{a0: junk, a1: 1, a2: 63}, want: regs{a0: 0x80000000}})

	// lui and auipc, with each bit of the immediate.
	uimms := []uint32{0, 0xfffff, 0x12345, 0xaaaaa, 0x55555}
	for k := 0; k < 20; k++ {
		uimms = append(uimms, 1<<k)
	}
	for _, v := range uimms {
		add(insnTest{asm: fmt.Sprintf("lui a0, %#x", v), x: regs{a0: junk}, want: regs{a0: v << 12}})
		add(insnTest{asm: fmt.Sprintf("auipc a0, %#x", v), x: regs{a0: junk}, want: regs{a0: base + v<<12}})
	}

	// jal, with each bit of the offset.
	joffs := []int32{0, -2, 1<<20 - 2, -1 << 20, 0xaaaaa, -0xaaaaa - 2, 0x55554}
	for k := 1; k < 20; k++ {
		joffs = append(joffs, 1<<k)
	}
	for _, off := range joffs {
		add(insnTest{asm: fmt.Sprintf("jal ra, %d", off), x: regs{ra: junk}, want: regs{ra: base + 4}, pc: to(base + uint32(off))})
	}
	add(insnTest{asm: "jal zero, 8", pc: to(base + 8)})
	add(insnTest{asm: "jal t6, -8", x: regs{t6: junk}, want: regs{t6: base + 4}, pc: to(base - 8)})

	// jalr clears bit 0 of the target and reads rs1 before it writes rd.
	add(insnTest{asm: "jalr ra, 0(a0)", x: regs{ra: junk, a0: 0x2468}, want: regs{ra: base + 4}, pc: to(0x2468)})
	add(insnTest{asm: "jalr ra, 5(a0)", x: regs{ra: junk, a0: 0x2468}, want: regs{ra: base + 4}, pc: to(0x246c)})
	add(insnTest{asm: "jalr a0, 0(a0)", x: regs{a0: 0x2469}, want: regs{a0: base + 4}, pc: to(0x2468)})
	add(insnTest{asm: "jalr zero, -4(a0)", x: regs{a0: 0x2468}, pc: to(0x2464)})
	add(insnTest{asm: "jalr ra, -2048(a0)", x: regs{ra: junk, a0: 0x2468}, want: regs{ra: base + 4}, pc: to(0x1c68)})
	add(insnTest{asm: "jalr ra, 2047(a0)", x: regs{ra: junk, a0: 0x2468}, want: regs{ra: base + 4}, pc: to(0x2c66)})
	add(insnTest{name: "jalr wraps", asm: "jalr ra, 2047(a0)", x: regs{ra: junk, a0: 0xffffffff}, want: regs{ra: base + 4}, pc: to(0x7fe)})
	add(insnTest{asm: "jalr t6, 1(t5)", x: regs{t6: junk, t5: 0x100}, want: regs{t6: base + 4}, pc: to(0x100)})

	// Branches: each condition on signed and unsigned operands, and each bit
	// of the offset.
	type branch struct {
		name string
		f    func(a, b uint32) bool
	}
	branches := []branch{
		{"beq", func(a, b uint32) bool { return a == b }},
		{"bne", func(a, b uint32) bool { return a != b }},
		{"blt", func(a, b uint32) bool { return int32(a) < int32(b) }},
		{"bge", func(a, b uint32) bool { return int32(a) >= int32(b) }},
		{"bltu", func(a, b uint32) bool { return a < b }},
		{"bgeu", func(a, b uint32) bool { return a >= b }},
	}
	for _, br := range branches {
		for _, a := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
			for _, b := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff} {
				pc := base + 4
				if br.f(a, b) {
					pc = base + 16
				}
				add(insnTest{
					name: fmt.Sprintf("%s %#x %#x", br.name, a, b),
					asm:  br.name + " a0, a1, 16",
					x:    regs{a0: a, a1: b},
					pc:   to(pc),
				})
			}
		}
	}
	boffs := []int32{-2, 4094, -4096, 0xaaa, -0xaaa - 2, 0x554}
	for k := 1; k < 12; k++ {
		boffs = append(boffs, 1<<k)
	}
	for _, off := range boffs {
		add(insnTest{asm: fmt.Sprintf("beq a0, a0, %d", off), x: regs{a0: 5}, pc: to(base + uint32(off))})
		add(insnTest{asm: fmt.Sprintf("bne a0, a0, %d", off), x: regs{a0: 5}, pc: to(base + 4)})
		add(insnTest{asm: fmt.Sprintf("bltu t5, t6, %d", off), x: regs{t5: 5, t6: 6}, pc: to(base + uint32(off))})
	}

	// Loads: sign extension, each bit of the offset, and addresses that are
	// not a multiple of the size.
	offs := []int32{0, 1, 2, 3, 4, 8, 16, 32, 64, 128, 256, 512, 1024, 2047, -1, -4, -2048, 0x555, -0x556}
	for _, off := range offs {
		addr := tData + uint32(off)
		p := patternAt(addr, 4)
		add(insnTest{asm: fmt.Sprintf("lb a0, %d(a1)", off), x: regs{a0: junk, a1: tData}, want: regs{a0: uint32(int32(int8(p[0])))}})
		add(insnTest{asm: fmt.Sprintf("lbu a0, %d(a1)", off), x: regs{a0: junk, a1: tData}, want: regs{a0: uint32(p[0])}})
		add(insnTest{asm: fmt.Sprintf("lh a0, %d(a1)", off), x: regs{a0: junk, a1: tData},
			want: regs{a0: uint32(int32(int16(uint16(p[0]) | uint16(p[1])<<8)))}})
		add(insnTest{asm: fmt.Sprintf("lhu a0, %d(a1)", off), x: regs{a0: junk, a1: tData},
			want: regs{a0: uint32(p[0]) | uint32(p[1])<<8}})
		add(insnTest{asm: fmt.Sprintf("lw a0, %d(a1)", off), x: regs{a0: junk, a1: tData},
			want: regs{a0: binary.LittleEndian.Uint32(p)}})
	}
	const far = 0x3000 // an address outside the snippet and the data pattern
	for _, c := range []struct {
		asm   string
		bytes []byte
		want  uint32
	}{
		{"lb a0, 0(a1)", []byte{0x80}, 0xffffff80},
		{"lb a0, 0(a1)", []byte{0x7f}, 0x7f},
		{"lb a0, 0(a1)", []byte{0xff}, 0xffffffff},
		{"lbu a0, 0(a1)", []byte{0x80}, 0x80},
		{"lbu a0, 0(a1)", []byte{0xff}, 0xff},
		{"lh a0, 0(a1)", []byte{0x00, 0x80}, 0xffff8000},
		{"lh a0, 0(a1)", []byte{0xff, 0x7f}, 0x7fff},
		{"lh a0, 0(a1)", []byte{0x34, 0x92}, 0xffff9234},
		{"lhu a0, 0(a1)", []byte{0x00, 0x80}, 0x8000},
		{"lhu a0, 0(a1)", []byte{0x34, 0x92}, 0x9234},
		{"lw a0, 0(a1)", []byte{0x78, 0x56, 0x34, 0x92}, 0x92345678},
		{"lh a0, 1(a1)", []byte{0, 0x34, 0x92}, 0xffff9234},
		{"lhu a0, 3(a1)", []byte{0, 0, 0, 0x34, 0x92}, 0x9234},
		{"lw a0, 1(a1)", []byte{0, 0x78, 0x56, 0x34, 0x92}, 0x92345678},
		{"lw a0, 2(a1)", []byte{0, 0, 0x78, 0x56, 0x34, 0x92}, 0x92345678},
		{"lw a0, 3(a1)", []byte{0, 0, 0, 0x78, 0x56, 0x34, 0x92}, 0x92345678},
	} {
		add(insnTest{name: fmt.Sprintf("%s of %x", c.asm, c.bytes), asm: c.asm,
			x: regs{a0: junk, a1: far}, mem: mem{far: c.bytes}, want: regs{a0: c.want}})
	}
	add(insnTest{asm: "lw zero, 0(a1)", x: regs{a1: tData}})
	add(insnTest{asm: "lw a1, 0(a1)", x: regs{a1: tData}, want: regs{a1: binary.LittleEndian.Uint32(patternAt(tData, 4))}})
	add(insnTest{name: "lw at the end of memory", asm: "lw a0, 0(a1)", x: regs{a0: junk, a1: tSize - 4},
		mem: mem{tSize - 4: {1, 2, 3, 4}}, want: regs{a0: 0x04030201}})
	add(insnTest{name: "lh at the end of memory", asm: "lh a0, 0(a1)", x: regs{a0: junk, a1: tSize - 2},
		mem: mem{tSize - 2: {1, 0x82}}, want: regs{a0: 0xffff8201}})
	add(insnTest{name: "lbu at the end of memory", asm: "lbu a0, 0(a1)", x: regs{a0: junk, a1: tSize - 1},
		mem: mem{tSize - 1: {0x99}}, want: regs{a0: 0x99}})
	add(insnTest{name: "lbu address wraps to 0", asm: "lbu a0, 1(a1)", x: regs{a0: junk, a1: 0xffffffff},
		mem: mem{0: {0x77}}, want: regs{a0: 0x77}})
	add(insnTest{name: "lw address wraps to 0", asm: "lw a0, 4(a1)", x: regs{a0: junk, a1: 0xfffffffc},
		mem: mem{0: {1, 2, 3, 4}}, want: regs{a0: 0x04030201}})

	// Stores.
	const val = 0x8badf00d
	for _, off := range offs {
		addr := tData + uint32(off)
		add(insnTest{asm: fmt.Sprintf("sb a0, %d(a1)", off), x: regs{a0: val, a1: tData}, wmem: mem{addr: {0x0d}}})
		add(insnTest{asm: fmt.Sprintf("sh a0, %d(a1)", off), x: regs{a0: val, a1: tData}, wmem: mem{addr: {0x0d, 0xf0}}})
		add(insnTest{asm: fmt.Sprintf("sw a0, %d(a1)", off), x: regs{a0: val, a1: tData}, wmem: mem{addr: {0x0d, 0xf0, 0xad, 0x8b}}})
	}
	add(insnTest{asm: "sw zero, 0(a1)", x: regs{a1: tData}, wmem: mem{tData: {0, 0, 0, 0}}})
	add(insnTest{asm: "sw a1, 0(a1)", x: regs{a1: tData}, wmem: mem{tData: le32(tData)}})
	add(insnTest{name: "sw at the end of memory", asm: "sw a0, 0(a1)", x: regs{a0: val, a1: tSize - 4}, wmem: mem{tSize - 4: le32(val)}})
	add(insnTest{name: "sh at the end of memory", asm: "sh a0, 0(a1)", x: regs{a0: val, a1: tSize - 2}, wmem: mem{tSize - 2: {0x0d, 0xf0}}})
	add(insnTest{name: "sb at the end of memory", asm: "sb a0, 0(a1)", x: regs{a0: val, a1: tSize - 1}, wmem: mem{tSize - 1: {0x0d}}})
	add(insnTest{name: "sw address wraps to 0", asm: "sw a0, 4(a1)", x: regs{a0: val, a1: 0xfffffffc}, wmem: mem{0: le32(val)}})

	// A load or a store that touches one byte outside memory faults and
	// changes nothing.
	for _, c := range []struct {
		asm  string
		addr uint32
		size int
	}{
		{"lw a0, 0(a1)", tSize - 3, 4},
		{"lw a0, 0(a1)", tSize, 4},
		{"lw a0, 0(a1)", 0xffffffff, 4},
		{"lw a0, 0(a1)", 0xfffffffd, 4},
		{"lh a0, 0(a1)", tSize - 1, 2},
		{"lhu a0, 0(a1)", tSize - 1, 2},
		{"lhu a0, 0(a1)", 0xffffffff, 2},
		{"lb a0, 0(a1)", tSize, 1},
		{"lbu a0, 0(a1)", tSize, 1},
		{"lbu a0, 0(a1)", 0x80000000, 1},
		{"lbu a0, 0(a1)", 0xffffffff, 1},
	} {
		add(insnTest{name: fmt.Sprintf("%s at %#x", c.asm, c.addr), asm: c.asm, x: regs{a0: junk, a1: c.addr},
			fault: fmt.Sprintf("load out of range: %d bytes at 0x%08x", c.size, c.addr)})
	}
	for _, c := range []struct {
		asm  string
		addr uint32
		size int
	}{
		{"sw a0, 0(a1)", tSize - 3, 4},
		{"sw a0, 0(a1)", tSize - 1, 4},
		{"sw a0, 0(a1)", tSize, 4},
		{"sw a0, 0(a1)", 0xffffffff, 4},
		{"sh a0, 0(a1)", tSize - 1, 2},
		{"sh a0, 0(a1)", 0xffffffff, 2},
		{"sb a0, 0(a1)", tSize, 1},
		{"sb a0, 0(a1)", 0xffffffff, 1},
	} {
		add(insnTest{name: fmt.Sprintf("%s at %#x", c.asm, c.addr), asm: c.asm, x: regs{a0: val, a1: c.addr},
			fault: fmt.Sprintf("store out of range: %d bytes at 0x%08x", c.size, c.addr)})
	}
	add(insnTest{asm: "lw a0, -1(zero)", x: regs{a0: junk}, fault: "load out of range: 4 bytes at 0xffffffff"})

	// fence does nothing.
	add(insnTest{asm: "fence"})
	add(insnTest{asm: "fence rw, w"})
	add(insnTest{asm: "fence.tso"})
	add(insnTest{asm: "pause"})
	add(insnTest{asm: "nop"})

	// ebreak faults.
	add(insnTest{asm: "ebreak", fault: "ebreak"})
	add(insnTest{asm: "c.ebreak", fault: "ebreak"})
	add(insnTest{name: "ebreak after two instructions", asm: "addi a0, zero, 1\naddi a0, a0, 1\nebreak",
		want: regs{a0: 2}, fault: "ebreak"})

	// lr.w and sc.w.
	word := []byte{0x78, 0x56, 0x34, 0x92}
	add(insnTest{asm: "lr.w a0, (a1)", x: regs{a0: junk, a1: far}, mem: mem{far: word},
		want: regs{a0: 0x92345678}, reserved: true, resAddr: far})
	add(insnTest{name: "sc.w after lr.w succeeds", asm: "lr.w a0, (a1)\nsc.w a2, a3, (a1)",
		x: regs{a0: junk, a1: far, a2: junk, a3: val}, mem: mem{far: word},
		want: regs{a0: 0x92345678, a2: 0}, wmem: mem{far: le32(val)}})
	add(insnTest{name: "sc.w without lr.w fails", asm: "sc.w a2, a3, (a1)",
		x: regs{a1: far, a2: junk, a3: val}, mem: mem{far: word}, want: regs{a2: 1}})
	add(insnTest{name: "sc.w at another address fails and clears the reservation",
		asm: "lr.w a0, (a1)\nsc.w a2, a3, (a4)\nsc.w a5, a3, (a1)",
		x:   regs{a0: junk, a1: far, a2: junk, a3: val, a4: far + 4, a5: junk}, mem: mem{far: word},
		want: regs{a0: 0x92345678, a2: 1, a5: 1}})
	add(insnTest{name: "the second sc.w fails", asm: "lr.w a0, (a1)\nsc.w a2, a3, (a1)\nsc.w a5, a0, (a1)",
		x: regs{a0: junk, a1: far, a2: junk, a3: val, a5: junk}, mem: mem{far: word},
		want: regs{a0: 0x92345678, a2: 0, a5: 1}, wmem: mem{far: le32(val)}})
	add(insnTest{name: "the second lr.w moves the reservation", asm: "lr.w a0, (a1)\nlr.w a0, (a4)\nsc.w a2, a3, (a1)",
		x: regs{a0: junk, a1: far, a2: junk, a3: val, a4: far + 4}, mem: mem{far: word},
		want: regs{a0: 0, a2: 1}})
	add(insnTest{name: "a store between lr.w and sc.w keeps the reservation", asm: "lr.w a0, (a1)\nsw a4, 0(a1)\nsc.w a2, a3, (a1)",
		x: regs{a0: junk, a1: far, a2: junk, a3: val, a4: 5}, mem: mem{far: word},
		want: regs{a0: 0x92345678, a2: 0}, wmem: mem{far: le32(val)}})
	add(insnTest{name: "sc.w reads rs2 before it writes rd", asm: "lr.w a0, (a1)\nsc.w a3, a3, (a1)",
		x: regs{a0: junk, a1: far, a3: val}, mem: mem{far: word},
		want: regs{a0: 0x92345678, a3: 0}, wmem: mem{far: le32(val)}})
	add(insnTest{name: "lr.w and sc.w with aq and rl", asm: "lr.w.aqrl a0, (a1)\nsc.w.aqrl a2, a3, (a1)",
		x: regs{a0: junk, a1: far, a2: junk, a3: val}, mem: mem{far: word},
		want: regs{a0: 0x92345678, a2: 0}, wmem: mem{far: le32(val)}})
	add(insnTest{name: "lr.w with rd = rs1", asm: "lr.w a1, (a1)\nsc.w a2, a3, (a4)",
		x: regs{a1: far, a2: junk, a3: val, a4: far}, mem: mem{far: word},
		want: regs{a1: 0x92345678, a2: 0}, wmem: mem{far: le32(val)}})
	add(insnTest{name: "sc.w with rd = rs1", asm: "lr.w a0, (a1)\nsc.w a1, a3, (a1)",
		x: regs{a0: junk, a1: far, a3: val}, mem: mem{far: word},
		want: regs{a0: 0x92345678, a1: 0}, wmem: mem{far: le32(val)}})
	add(insnTest{name: "a failed sc.w with rd = rs1", asm: "sc.w a1, a3, (a1)",
		x: regs{a1: far, a3: val}, mem: mem{far: word}, want: regs{a1: 1}})
	add(insnTest{name: "lr.w to x0 sets the reservation", asm: "lr.w zero, (a1)\nsc.w a2, a3, (a1)",
		x: regs{a1: far, a2: junk, a3: val}, mem: mem{far: word}, want: regs{a2: 0}, wmem: mem{far: le32(val)}})

	// The AMO instructions: rd gets the old word, memory gets the result.
	type amo struct {
		name string
		f    func(old, b uint32) uint32
	}
	amos := []amo{
		{"amoswap.w", func(old, b uint32) uint32 { return b }},
		{"amoadd.w", func(old, b uint32) uint32 { return old + b }},
		{"amoxor.w", func(old, b uint32) uint32 { return old ^ b }},
		{"amoand.w", func(old, b uint32) uint32 { return old & b }},
		{"amoor.w", func(old, b uint32) uint32 { return old | b }},
		{"amomin.w", func(old, b uint32) uint32 { return uint32(min(int32(old), int32(b))) }},
		{"amomax.w", func(old, b uint32) uint32 { return uint32(max(int32(old), int32(b))) }},
		{"amominu.w", func(old, b uint32) uint32 { return min(old, b) }},
		{"amomaxu.w", func(old, b uint32) uint32 { return max(old, b) }},
	}
	for _, op := range amos {
		for _, old := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff, 0x12345678} {
			for _, b := range []uint32{0, 1, 0x7fffffff, 0x80000000, 0xffffffff, 0x9abcdef0} {
				add(insnTest{
					name: fmt.Sprintf("%s %#x %#x", op.name, old, b),
					asm:  op.name + " a0, a2, (a1)",
					x:    regs{a0: junk, a1: far, a2: b},
					mem:  mem{far: le32(old)},
					want: regs{a0: old},
					wmem: mem{far: le32(op.f(old, b))},
				})
			}
		}
		add(insnTest{asm: op.name + ".aqrl a0, a2, (a1)", x: regs{a0: junk, a1: far, a2: 3}, mem: mem{far: le32(5)},
			want: regs{a0: 5}, wmem: mem{far: le32(op.f(5, 3))}})
		add(insnTest{name: op.name + " at an address that is not a multiple of 4", asm: op.name + " a0, a2, (a1)",
			x: regs{a0: junk, a1: far + 2, a2: 3}, fault: fmt.Sprintf("misaligned atomic access at 0x%08x", far+2)})
		add(insnTest{name: op.name + " outside memory", asm: op.name + " a0, a2, (a1)",
			x: regs{a0: junk, a1: tSize, a2: 3}, fault: fmt.Sprintf("store out of range: 4 bytes at 0x%08x", tSize)})
	}
	// The results of the four comparisons, written out.
	add(insnTest{name: "amomin.w is signed", asm: "amomin.w a0, a2, (a1)", x: regs{a0: junk, a1: far, a2: 1},
		mem: mem{far: le32(0x80000000)}, want: regs{a0: 0x80000000}})
	add(insnTest{name: "amomax.w is signed", asm: "amomax.w a0, a2, (a1)", x: regs{a0: junk, a1: far, a2: 1},
		mem: mem{far: le32(0x80000000)}, want: regs{a0: 0x80000000}, wmem: mem{far: le32(1)}})
	add(insnTest{name: "amominu.w is unsigned", asm: "amominu.w a0, a2, (a1)", x: regs{a0: junk, a1: far, a2: 1},
		mem: mem{far: le32(0x80000000)}, want: regs{a0: 0x80000000}, wmem: mem{far: le32(1)}})
	add(insnTest{name: "amomaxu.w is unsigned", asm: "amomaxu.w a0, a2, (a1)", x: regs{a0: junk, a1: far, a2: 1},
		mem: mem{far: le32(0x80000000)}, want: regs{a0: 0x80000000}})
	add(insnTest{name: "amoadd.w with rd = rs2", asm: "amoadd.w a2, a2, (a1)", x: regs{a1: far, a2: 3},
		mem: mem{far: le32(5)}, want: regs{a2: 5}, wmem: mem{far: le32(8)}})
	add(insnTest{name: "amoswap.w with rd = rs1", asm: "amoswap.w a1, a2, (a1)", x: regs{a1: far, a2: 3},
		mem: mem{far: le32(5)}, want: regs{a1: 5}, wmem: mem{far: le32(3)}})
	add(insnTest{name: "amoadd.w to x0", asm: "amoadd.w zero, a2, (a1)", x: regs{a1: far, a2: 3},
		mem: mem{far: le32(5)}, wmem: mem{far: le32(8)}})
	add(insnTest{name: "an AMO keeps the reservation", asm: "lr.w a0, (a1)\namoadd.w a4, a2, (a1)\nsc.w a5, a3, (a1)",
		x: regs{a0: junk, a1: far, a2: 3, a3: val, a4: junk, a5: junk}, mem: mem{far: le32(5)},
		want: regs{a0: 5, a4: 5, a5: 0}, wmem: mem{far: le32(val)}})
	for _, c := range []struct {
		asm   string
		addr  uint32
		fault string
	}{
		{"lr.w a0, (a1)", far + 1, "misaligned atomic access at 0x00003001"},
		{"lr.w a0, (a1)", far + 2, "misaligned atomic access at 0x00003002"},
		{"lr.w a0, (a1)", tSize, "load out of range: 4 bytes at 0x00004000"},
		{"lr.w a0, (a1)", 0xfffffffc, "load out of range: 4 bytes at 0xfffffffc"},
		{"lr.w a0, (a1)", 0xffffffff, "misaligned atomic access at 0xffffffff"},
		{"sc.w a0, a2, (a1)", far + 3, "misaligned atomic access at 0x00003003"},
		{"sc.w a0, a2, (a1)", tSize, "store out of range: 4 bytes at 0x00004000"},
	} {
		add(insnTest{name: fmt.Sprintf("%s at %#x", c.asm, c.addr), asm: c.asm, x: regs{a0: junk, a1: c.addr, a2: 3}, fault: c.fault})
	}
	add(insnTest{name: "a faulting sc.w keeps the reservation", asm: "lr.w a0, (a1)\nsc.w a2, a3, (a4)",
		x: regs{a0: junk, a1: far, a2: junk, a3: val, a4: far + 2}, want: regs{a0: 0},
		fault: "misaligned atomic access at 0x00003002", reserved: true, resAddr: far})

	// Two loops, which a wrong pc would break.
	add(insnTest{name: "loop", asm: "addi a0, zero, 0\naddi a1, zero, 10\n1:\nadd a0, a0, a1\naddi a1, a1, -1\nbne a1, zero, 1b",
		x: regs{a0: junk, a1: junk}, steps: 32, want: regs{a0: 55, a1: 0}})
	add(insnTest{name: "compressed loop", asm: "c.li a0, 0\nc.li a1, 10\n1:\nc.add a0, a1\nc.addi a1, -1\nc.bnez a1, 1b",
		x: regs{a0: junk, a1: junk}, steps: 32, want: regs{a0: 55, a1: 0}})
	add(insnTest{name: "call and return", asm: "jal ra, 2f\naddi a0, a0, 1\njal zero, 3f\n2:\naddi a0, a0, 16\njalr zero, 0(ra)\n3:",
		x: regs{ra: junk}, steps: 5, want: regs{a0: 17, ra: base + 4}})
	add(insnTest{name: "compressed call and return", asm: "c.jal 2f\nc.addi a0, 1\nc.j 3f\n2:\nc.addi a0, 16\nc.jr ra\n3:",
		x: regs{ra: junk}, steps: 5, want: regs{a0: 17, ra: base + 2}})

	// The compressed instructions. A register in a three-bit field is one of
	// s0, s1, a0 to a5; the tests use the first and the last.
	const stack = 0x2ff0 // a stack pointer below far
	for _, imm := range []uint32{4, 8, 16, 32, 64, 128, 256, 512, 1020, 0x154, 0x2a8} {
		add(insnTest{asm: fmt.Sprintf("c.addi4spn a0, sp, %d", imm), x: regs{a0: junk, sp: stack}, want: regs{a0: stack + imm}})
	}
	add(insnTest{asm: "c.addi4spn s0, sp, 4", x: regs{s0: junk, sp: stack}, want: regs{s0: stack + 4}})
	add(insnTest{asm: "c.addi4spn a5, sp, 1020", x: regs{a5: junk, sp: 0xfffffffc}, want: regs{a5: 1016}})

	for _, off := range []uint32{0, 4, 8, 16, 32, 64, 124, 0x54, 0x28} {
		addr := tData + off
		add(insnTest{asm: fmt.Sprintf("c.lw a0, %d(a1)", off), x: regs{a0: junk, a1: tData},
			want: regs{a0: binary.LittleEndian.Uint32(patternAt(addr, 4))}})
		add(insnTest{asm: fmt.Sprintf("c.sw a0, %d(a1)", off), x: regs{a0: val, a1: tData}, wmem: mem{addr: le32(val)}})
	}
	add(insnTest{asm: "c.lw s0, 4(a5)", x: regs{s0: junk, a5: far}, mem: mem{far + 4: word}, want: regs{s0: 0x92345678}})
	add(insnTest{asm: "c.lw a5, 4(s0)", x: regs{a5: junk, s0: far}, mem: mem{far + 4: word}, want: regs{a5: 0x92345678}})
	add(insnTest{asm: "c.lw a0, 0(a0)", x: regs{a0: far}, mem: mem{far: word}, want: regs{a0: 0x92345678}})
	add(insnTest{name: "c.lw at an address that is not a multiple of 4", asm: "c.lw a0, 4(a1)", x: regs{a0: junk, a1: far - 1},
		mem: mem{far + 3: word}, want: regs{a0: 0x92345678}})
	add(insnTest{asm: "c.sw s0, 4(a5)", x: regs{s0: val, a5: far}, wmem: mem{far + 4: le32(val)}})
	add(insnTest{asm: "c.sw a5, 4(s0)", x: regs{a5: val, s0: far}, wmem: mem{far + 4: le32(val)}})
	add(insnTest{asm: "c.sw a0, 0(a0)", x: regs{a0: far}, wmem: mem{far: le32(far)}})
	add(insnTest{name: "c.lw outside memory", asm: "c.lw a0, 4(a1)", x: regs{a0: junk, a1: tSize - 7},
		fault: "load out of range: 4 bytes at 0x00003ffd"})
	add(insnTest{name: "c.sw outside memory", asm: "c.sw a0, 4(a1)", x: regs{a0: val, a1: tSize - 7},
		fault: "store out of range: 4 bytes at 0x00003ffd"})

	add(insnTest{asm: "c.nop"})
	simm6 := []int32{1, 2, 4, 8, 16, 31, -1, -32, 21, -22}
	for _, imm := range simm6 {
		add(insnTest{asm: fmt.Sprintf("c.addi a0, %d", imm), x: regs{a0: 1000}, want: regs{a0: 1000 + uint32(imm)}})
		add(insnTest{asm: fmt.Sprintf("c.li a0, %d", imm), x: regs{a0: junk}, want: regs{a0: uint32(imm)}})
		add(insnTest{asm: fmt.Sprintf("c.andi a0, %d", imm), x: regs{a0: 0x9abcdef5}, want: regs{a0: 0x9abcdef5 & uint32(imm)}})
		// c.lui gives the 6-bit immediate as the 20-bit value of lui.
		add(insnTest{asm: fmt.Sprintf("c.lui a0, %#x", uint32(imm)&0xfffff), x: regs{a0: junk}, want: regs{a0: uint32(imm) << 12}})
	}
	add(insnTest{asm: "c.addi ra, 1", x: regs{ra: 0xffffffff}, want: regs{ra: 0}})
	add(insnTest{asm: "c.addi t6, -1", x: regs{t6: 0}, want: regs{t6: 0xffffffff}})
	add(insnTest{asm: "c.addi sp, 4", x: regs{sp: stack}, want: regs{sp: stack + 4}})
	add(insnTest{asm: "c.li a0, 0", x: regs{a0: junk}, want: regs{a0: 0}})
	add(insnTest{asm: "c.li ra, 5", x: regs{ra: junk}, want: regs{ra: 5}})
	add(insnTest{asm: "c.li t6, -5", x: regs{t6: junk}, want: regs{t6: 0xfffffffb}})
	add(insnTest{asm: "c.andi a0, 0", x: regs{a0: junk}, want: regs{a0: 0}})
	add(insnTest{asm: "c.andi s0, 15", x: regs{s0: 0xff}, want: regs{s0: 15}})
	add(insnTest{asm: "c.andi a5, -16", x: regs{a5: 0xff}, want: regs{a5: 0xf0}})
	add(insnTest{asm: "c.lui ra, 1", x: regs{ra: junk}, want: regs{ra: 0x1000}})
	add(insnTest{asm: "c.lui t6, 0xfffff", x: regs{t6: junk}, want: regs{t6: 0xfffff000}})
	add(insnTest{asm: "c.lui gp, 0xfffe0", x: regs{gp: junk}, want: regs{gp: 0xfffe0000}})

	for _, imm := range []int32{16, 32, 64, 128, 256, 496, -16, -512, 336, -352} {
		add(insnTest{asm: fmt.Sprintf("c.addi16sp sp, %d", imm), x: regs{sp: stack}, want: regs{sp: stack + uint32(imm)}})
	}

	for _, sh := range []uint32{1, 2, 4, 8, 16, 21, 31} {
		for _, a := range []uint32{0x80000001, 0x7ffffffe, 0x12345678} {
			add(insnTest{name: fmt.Sprintf("c.srli %#x %d", a, sh), asm: fmt.Sprintf("c.srli a0, %d", sh), x: regs{a0: a}, want: regs{a0: a >> sh}})
			add(insnTest{name: fmt.Sprintf("c.srai %#x %d", a, sh), asm: fmt.Sprintf("c.srai a0, %d", sh), x: regs{a0: a}, want: regs{a0: uint32(int32(a) >> sh)}})
			add(insnTest{name: fmt.Sprintf("c.slli %#x %d", a, sh), asm: fmt.Sprintf("c.slli a0, %d", sh), x: regs{a0: a}, want: regs{a0: a << sh}})
		}
	}
	add(insnTest{asm: "c.srli s0, 4", x: regs{s0: 0x80000000}, want: regs{s0: 0x08000000}})
	add(insnTest{asm: "c.srli a5, 4", x: regs{a5: 0x80000000}, want: regs{a5: 0x08000000}})
	add(insnTest{asm: "c.srai s0, 4", x: regs{s0: 0x80000000}, want: regs{s0: 0xf8000000}})
	add(insnTest{asm: "c.srai a5, 4", x: regs{a5: 0x80000000}, want: regs{a5: 0xf8000000}})
	add(insnTest{asm: "c.slli ra, 4", x: regs{ra: 0x18000001}, want: regs{ra: 0x80000010}})
	add(insnTest{asm: "c.slli t6, 4", x: regs{t6: 0x18000001}, want: regs{t6: 0x80000010}})

	type cop struct {
		name string
		f    func(a, b uint32) uint32
	}
	for _, op := range []cop{
		{"c.sub", func(a, b uint32) uint32 { return a - b }},
		{"c.xor", func(a, b uint32) uint32 { return a ^ b }},
		{"c.or", func(a, b uint32) uint32 { return a | b }},
		{"c.and", func(a, b uint32) uint32 { return a & b }},
	} {
		for _, p := range [][2]uint32{{5, 9}, {0x80000000, 1}, {0xf0f0f0f0, 0x3c3c3c3c}, {0, 0xffffffff}} {
			add(insnTest{name: fmt.Sprintf("%s %#x %#x", op.name, p[0], p[1]), asm: op.name + " a0, a1",
				x: regs{a0: p[0], a1: p[1]}, want: regs{a0: op.f(p[0], p[1])}})
		}
		add(insnTest{asm: op.name + " s0, a5", x: regs{s0: 0xf0f0f0f0, a5: 0x3c3c3c3c}, want: regs{s0: op.f(0xf0f0f0f0, 0x3c3c3c3c)}})
		add(insnTest{asm: op.name + " a5, s0", x: regs{a5: 0xf0f0f0f0, s0: 0x3c3c3c3c}, want: regs{a5: op.f(0xf0f0f0f0, 0x3c3c3c3c)}})
		add(insnTest{asm: op.name + " s1, s1", x: regs{s1: 0xf0f0f0f0}, want: regs{s1: op.f(0xf0f0f0f0, 0xf0f0f0f0)}})
	}

	cjoffs := []int32{-2, 2046, -2048, 0x554, -0x556, 0x2aa}
	for k := 1; k < 11; k++ {
		cjoffs = append(cjoffs, 1<<k)
	}
	for _, off := range cjoffs {
		add(insnTest{asm: fmt.Sprintf("c.j %d", off), pc: to(base + uint32(off))})
		add(insnTest{asm: fmt.Sprintf("c.jal %d", off), x: regs{ra: junk}, want: regs{ra: base + 2}, pc: to(base + uint32(off))})
	}
	cboffs := []int32{-2, 254, -256, 0x54, -0x56, 0xaa}
	for k := 1; k < 8; k++ {
		cboffs = append(cboffs, 1<<k)
	}
	for _, off := range cboffs {
		add(insnTest{name: fmt.Sprintf("c.beqz %d taken", off), asm: fmt.Sprintf("c.beqz a0, %d", off), pc: to(base + uint32(off))})
		add(insnTest{name: fmt.Sprintf("c.beqz %d not taken", off), asm: fmt.Sprintf("c.beqz a0, %d", off), x: regs{a0: 0x80000000}, pc: to(base + 2)})
		add(insnTest{name: fmt.Sprintf("c.bnez %d taken", off), asm: fmt.Sprintf("c.bnez a0, %d", off), x: regs{a0: 0x80000000}, pc: to(base + uint32(off))})
		add(insnTest{name: fmt.Sprintf("c.bnez %d not taken", off), asm: fmt.Sprintf("c.bnez a0, %d", off), pc: to(base + 2)})
	}
	add(insnTest{asm: "c.beqz s0, 8", pc: to(base + 8)})
	add(insnTest{asm: "c.beqz a5, 8", pc: to(base + 8)})
	add(insnTest{asm: "c.bnez s0, 8", x: regs{s0: 1}, pc: to(base + 8)})
	add(insnTest{asm: "c.bnez a5, 8", x: regs{a5: 1}, pc: to(base + 8)})

	for _, off := range []uint32{0, 4, 8, 16, 32, 64, 128, 252, 0x54, 0xa8} {
		addr := tData + off
		add(insnTest{asm: fmt.Sprintf("c.lwsp a0, %d(sp)", off), x: regs{a0: junk, sp: tData},
			want: regs{a0: binary.LittleEndian.Uint32(patternAt(addr, 4))}})
		add(insnTest{asm: fmt.Sprintf("c.swsp a0, %d(sp)", off), x: regs{a0: val, sp: tData}, wmem: mem{addr: le32(val)}})
	}
	add(insnTest{asm: "c.lwsp ra, 4(sp)", x: regs{ra: junk, sp: far}, mem: mem{far + 4: word}, want: regs{ra: 0x92345678}})
	add(insnTest{asm: "c.lwsp t6, 4(sp)", x: regs{t6: junk, sp: far}, mem: mem{far + 4: word}, want: regs{t6: 0x92345678}})
	add(insnTest{asm: "c.lwsp sp, 4(sp)", x: regs{sp: far}, mem: mem{far + 4: word}, want: regs{sp: 0x92345678}})
	add(insnTest{asm: "c.swsp ra, 4(sp)", x: regs{ra: val, sp: far}, wmem: mem{far + 4: le32(val)}})
	add(insnTest{asm: "c.swsp t6, 4(sp)", x: regs{t6: val, sp: far}, wmem: mem{far + 4: le32(val)}})
	add(insnTest{asm: "c.swsp zero, 4(sp)", x: regs{sp: far}, mem: mem{far + 4: word}, wmem: mem{far + 4: {0, 0, 0, 0}}})
	add(insnTest{asm: "c.swsp sp, 4(sp)", x: regs{sp: far}, wmem: mem{far + 4: le32(far)}})
	add(insnTest{name: "c.lwsp outside memory", asm: "c.lwsp a0, 252(sp)", x: regs{a0: junk, sp: tSize - 255},
		fault: "load out of range: 4 bytes at 0x00003ffd"})
	add(insnTest{name: "c.swsp outside memory", asm: "c.swsp a0, 252(sp)", x: regs{a0: val, sp: tSize - 255},
		fault: "store out of range: 4 bytes at 0x00003ffd"})

	add(insnTest{asm: "c.jr a0", x: regs{a0: 0x2469}, pc: to(0x2468)})
	add(insnTest{asm: "c.jr ra", x: regs{ra: 0x2468}, pc: to(0x2468)})
	add(insnTest{asm: "c.jr t6", x: regs{t6: 0xfffffffe}, pc: to(0xfffffffe)})
	add(insnTest{asm: "c.jalr a0", x: regs{ra: junk, a0: 0x2469}, want: regs{ra: base + 2}, pc: to(0x2468)})
	add(insnTest{asm: "c.jalr ra", x: regs{ra: 0x2468}, want: regs{ra: base + 2}, pc: to(0x2468)})
	add(insnTest{asm: "c.jalr t6", x: regs{ra: junk, t6: 0x2468}, want: regs{ra: base + 2}, pc: to(0x2468)})
	add(insnTest{asm: "c.mv a0, a1", x: regs{a0: junk, a1: 77}, want: regs{a0: 77}})
	add(insnTest{asm: "c.mv t6, ra", x: regs{t6: junk, ra: 77}, want: regs{t6: 77}})
	add(insnTest{asm: "c.mv ra, t6", x: regs{ra: junk, t6: 77}, want: regs{ra: 77}})
	add(insnTest{asm: "c.mv sp, a0", x: regs{sp: junk, a0: 77}, want: regs{sp: 77}})
	add(insnTest{asm: "c.add a0, a1", x: regs{a0: 0xffffffff, a1: 78}, want: regs{a0: 77}})
	add(insnTest{asm: "c.add t6, ra", x: regs{t6: 70, ra: 7}, want: regs{t6: 77}})
	add(insnTest{asm: "c.add ra, t6", x: regs{ra: 70, t6: 7}, want: regs{ra: 77}})
	add(insnTest{asm: "c.add a0, a0", x: regs{a0: 0x80000003}, want: regs{a0: 6}})
	add(insnTest{asm: "c.add sp, a0", x: regs{sp: stack, a0: 16}, want: regs{sp: stack + 16}})

	return ts
}

// insnCount returns the count of instructions in machine code that holds no
// data.
func insnCount(code []byte) int {
	n := 0
	for i := 0; i < len(code); n++ {
		if code[i]&3 == 3 {
			i += 4
		} else {
			i += 2
		}
	}
	return n
}

// lastInsn returns the offset of the last instruction of code.
func lastInsn(code []byte) uint32 {
	last := 0
	for i := 0; i < len(code); {
		last = i
		if code[i]&3 == 3 {
			i += 4
		} else {
			i += 2
		}
	}
	return uint32(last)
}

// run executes the test with the snippet at base, with or without the
// decode cache, and compares the whole state: pc, every register, every
// byte of memory, the reservation, and Steps.
func (tc *insnTest) run(t *testing.T, code []byte, base uint32, noCache bool) {
	t.Helper()
	m := &Machine{Mem: make([]byte, tSize), PC: base, noCache: noCache}
	for a := uint32(patternLo); a < patternHi; a++ {
		m.Mem[a] = pattern(a)
	}
	copy(m.Mem[base:], code)
	for a, b := range tc.mem {
		copy(m.Mem[a:], b)
	}
	for r, v := range tc.x {
		m.X[r] = v
	}
	wantMem := bytes.Clone(m.Mem)
	for a, b := range tc.wmem {
		copy(wantMem[a:], b)
	}
	wantX := m.X
	for r, v := range tc.want {
		wantX[r] = v
	}
	steps := tc.steps
	if steps == 0 {
		steps = insnCount(code)
		if tc.fault != "" {
			steps--
		}
	}
	wantPC := base + uint32(len(code))
	if tc.fault != "" {
		wantPC = base + lastInsn(code)
	}
	if tc.pc != nil {
		wantPC = *tc.pc
	}

	for i := 0; i < steps; i++ {
		if err := m.Step(); err != nil {
			t.Errorf("%s: step %d: %v", tc.name, i, err)
			return
		}
	}
	if tc.fault != "" {
		err := m.Step()
		var f *Fault
		if !errors.As(err, &f) {
			t.Errorf("%s: error %v, want a fault %q", tc.name, err, tc.fault)
			return
		}
		if f.Reason != tc.fault || f.PC != wantPC || f.Step != uint64(steps) {
			t.Errorf("%s: fault %+v, want reason %q at pc %#x, step %d", tc.name, *f, tc.fault, wantPC, steps)
		}
	}
	if m.PC != wantPC {
		t.Errorf("%s: pc = %#x, want %#x", tc.name, m.PC, wantPC)
	}
	if m.Steps != uint64(steps) {
		t.Errorf("%s: Steps = %d, want %d", tc.name, m.Steps, steps)
	}
	for r := range m.X {
		if m.X[r] != wantX[r] {
			t.Errorf("%s: x%d = %#x, want %#x", tc.name, r, m.X[r], wantX[r])
		}
	}
	if !bytes.Equal(m.Mem, wantMem) {
		for a := range m.Mem {
			if m.Mem[a] != wantMem[a] {
				t.Errorf("%s: memory at %#x = %#x, want %#x", tc.name, a, m.Mem[a], wantMem[a])
				break
			}
		}
	}
	if m.reserved != tc.reserved || m.resAddr != tc.resAddr {
		t.Errorf("%s: reservation = %v at %#x, want %v at %#x", tc.name, m.reserved, m.resAddr, tc.reserved, tc.resAddr)
	}
	if halted, _ := m.Halted(); halted {
		t.Errorf("%s: the machine has halted", tc.name)
	}
}

// TestInstructions runs every instruction test at two addresses, one of
// them not a multiple of 4, each with and without the decode cache.
func TestInstructions(t *testing.T) {
	golden := loadGolden(t)
	for _, base := range []uint32{tCode, tCode + 2} {
		for _, tc := range insnTestsAt(base) {
			c := code(t, golden, tc.asm)
			tc.run(t, c, base, false)
			tc.run(t, c, base, true)
		}
	}
}

// TestGoldenEncodings compares some lines of insnGolden with encodings that
// were made by hand from the RISC-V specification. It guards the file that
// the other tests trust.
func TestGoldenEncodings(t *testing.T) {
	golden := loadGolden(t)
	for asm, want := range map[string]string{
		"add a0, a1, a2":       "3385c500", // funct7 0, rs2 12, rs1 11, funct3 0, rd 10, opcode 0x33
		"mul a0, a1, a2":       "3385c502", // funct7 1
		"sub a0, a1, a2":       "3385c540", // funct7 0x20
		"addi a0, a1, 1":       "13851500", // imm 1, rs1 11, funct3 0, rd 10, opcode 0x13
		"addi a0, a1, -1":      "1385f5ff",
		"lui a0, 0x12345":      "37553412",
		"jal ra, 8":            "ef008000",
		"beq a0, a1, 16":       "6308b500", // imm[4:1] = 8 in bits 11:8
		"lw a0, 4(a1)":         "03a54500",
		"sw a0, 4(a1)":         "23a2a500",
		"lr.w a0, (a1)":        "2fa50510",
		"ebreak":               "73001000",
		"c.nop":                "0100",
		"c.jr ra":              "8280",
		"c.ebreak":             "0290",
		"c.li a0, 1":           "0545", // 010 0 01010 00001 01
		"c.addi a0, 1":         "0505", // 000 0 01010 00001 01
		"c.mv a0, a1":          "2e85", // 100 0 01010 01011 10
		"c.add a0, a1":         "2e95", // 100 1 01010 01011 10
		"c.lwsp a0, 4(sp)":     "1245", // 010 0 01010 00100 10
		"c.swsp a0, 4(sp)":     "2ac2", // 110 000100 01010 10
		"c.lw a0, 4(a1)":       "c841", // 010 000 011 10 010 00
		"c.addi4spn a0, sp, 4": "4800", // 000 00000010 010 00
	} {
		if got := fmt.Sprintf("%x", code(t, golden, asm)); got != want {
			t.Errorf("%s: %s has %s, want %s", asm, insnGolden, got, want)
		}
	}
}
