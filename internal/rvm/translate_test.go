package rvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"testing"
)

// The tests of translated code need translated code, and translated code is
// Go source that the compiler must build. So two files of this package are
// written by Translate and kept in the repository:
//
//	ops_code_test.go    the code for the program of opsProgram
//	guest_code_test.go  the code for testdata/guest.elf
//
// TestTranslationsAreCurrent fails if either file is not what Translate
// writes now. To write them again:
//
//	RVM_WRITE_CODE=1 go test -tags rvmbootstrap ./internal/rvm -run TestTranslationsAreCurrent
//
// The tag leaves out the two files and the tests that use them, so that the
// command works when the files are missing or do not compile.
const writeCode = "RVM_WRITE_CODE=1 go test -tags rvmbootstrap ./internal/rvm -run TestTranslationsAreCurrent"

// bootTag is the first line of a file that the tag rvmbootstrap leaves out.
const bootTag = "//go:build !rvmbootstrap\n\n"

// The program of opsProgram: 8 KiB of memory, with one instruction in each
// slot of 8 bytes.
const (
	opsSize = 0x2000
	opsLo   = 0x800
	opsSlot = 8
)

// An opSlot is one instruction of the program of opsProgram.
type opSlot struct {
	pc uint32
	d  insn
}

func encR(f7, rs2, rs1, f3, rd uint32) uint32 {
	return f7<<25 | rs2<<20 | rs1<<15 | f3<<12 | rd<<7 | 0x33
}

func encI(opcode uint32, imm int32, rs1, f3, rd uint32) uint32 {
	return uint32(imm)&0xfff<<20 | rs1<<15 | f3<<12 | rd<<7 | opcode
}

func encS(imm int32, rs2, rs1, f3 uint32) uint32 {
	u := uint32(imm)
	return u>>5&0x7f<<25 | rs2<<20 | rs1<<15 | f3<<12 | u&0x1f<<7 | 0x23
}

func encB(imm int32, rs2, rs1, f3 uint32) uint32 {
	u := uint32(imm)
	return u>>12&1<<31 | u>>5&0x3f<<25 | rs2<<20 | rs1<<15 | f3<<12 | u>>1&0xf<<8 | u>>11&1<<7 | 0x63
}

func encJ(imm int32, rd uint32) uint32 {
	u := uint32(imm)
	return u>>20&1<<31 | u>>1&0x3ff<<21 | u>>11&1<<20 | u>>12&0xff<<12 | rd<<7 | 0x6f
}

// opsProgram returns a program that has every kind of instruction that
// Translate translates, and some that it leaves to the interpreter. Each
// instruction stands alone in a slot. The rest of the slot is c.ebreak, so
// that a run stops after the instruction, and a branch or a jump of 4 bytes
// forward or back lands on a c.ebreak.
//
// The registers are chosen so that the destination is also a source, is
// register 0, or is neither; and so that a source is register 0.
func opsProgram() (mem []byte, lo, hi uint32, slots []opSlot) {
	mem = make([]byte, opsSize)
	// Data, the same on every run: a load reads it.
	state := uint32(0x9e3779b9)
	for i := range mem {
		state = state*1664525 + 1013904223
		mem[i] = byte(state >> 24)
	}
	var codes []uint32
	add := func(raw uint32) { codes = append(codes, raw) }

	type triple struct{ rd, rs1, rs2 uint32 }
	triples := []triple{{10, 11, 12}, {10, 10, 11}, {10, 11, 10}, {10, 10, 10}, {0, 11, 12}, {10, 0, 12}, {10, 11, 0}, {5, 28, 31}}
	for _, f := range []struct{ f7, f3 uint32 }{
		{0, 0}, {0x20, 0}, {0, 1}, {0, 2}, {0, 3}, {0, 4}, {0, 5}, {0x20, 5}, {0, 6}, {0, 7}, // add sub sll slt sltu xor srl sra or and
		{1, 0}, {1, 1}, {1, 2}, {1, 3}, {1, 4}, {1, 5}, {1, 6}, {1, 7}, // mul mulh mulhsu mulhu div divu rem remu
	} {
		for _, r := range triples {
			add(encR(f.f7, r.rs2, r.rs1, f.f3, r.rd))
		}
	}
	type pair struct{ rd, rs1 uint32 }
	pairs := []pair{{10, 11}, {10, 10}, {0, 11}, {10, 0}}
	for _, f3 := range []uint32{0, 2, 3, 4, 6, 7} { // addi slti sltiu xori ori andi
		for _, imm := range []int32{0, 1, -1, 2047, -2048} {
			for _, r := range pairs {
				add(encI(0x13, imm, r.rs1, f3, r.rd))
			}
		}
	}
	for _, sh := range []int32{0, 1, 31} {
		for _, r := range pairs[:3] {
			add(encI(0x13, sh, r.rs1, 1, r.rd))       // slli
			add(encI(0x13, sh, r.rs1, 5, r.rd))       // srli
			add(encI(0x13, sh|0x400, r.rs1, 5, r.rd)) // srai
		}
	}
	for _, imm := range []uint32{0, 1, 0x80000, 0xfffff} {
		for _, rd := range []uint32{10, 0} {
			add(imm<<12 | rd<<7 | 0x37) // lui
			add(imm<<12 | rd<<7 | 0x17) // auipc
		}
	}
	for _, f3 := range []uint32{0, 1, 2, 4, 5} { // lb lh lw lbu lhu
		for _, imm := range []int32{0, 4, -4, 2047, -2048} {
			for _, r := range []pair{{10, 11}, {11, 11}, {0, 11}, {10, 0}} {
				add(encI(0x03, imm, r.rs1, f3, r.rd))
			}
		}
	}
	for _, f3 := range []uint32{0, 1, 2} { // sb sh sw
		for _, imm := range []int32{0, 4, -4, 2047} {
			for _, r := range []struct{ rs1, rs2 uint32 }{{11, 12}, {11, 11}, {11, 0}, {0, 12}} {
				add(encS(imm, r.rs2, r.rs1, f3))
			}
		}
	}
	for _, f3 := range []uint32{0, 1, 4, 5, 6, 7} { // beq bne blt bge bltu bgeu
		for _, off := range []int32{4, -4} {
			for _, r := range []struct{ rs1, rs2 uint32 }{{11, 12}, {11, 11}, {11, 0}, {0, 12}} {
				add(encB(off, r.rs2, r.rs1, f3))
			}
		}
	}
	for _, rd := range []uint32{1, 0, 10} {
		add(encJ(4, rd))  // jal
		add(encJ(-4, rd)) // jal
	}
	for _, imm := range []int32{0, 4, -4, 1, 2047} {
		for _, r := range []pair{{1, 11}, {0, 1}, {11, 11}, {10, 0}} {
			add(encI(0x67, imm, r.rs1, 0, r.rd)) // jalr
		}
	}
	add(0x0000000f) // fence
	// Left to the interpreter.
	add(0x00000073)                           // ecall
	add(0x00100073)                           // ebreak
	add(0x1005a52f)                           // lr.w a0, (a1)
	add(0x18c5a52f)                           // sc.w a0, a2, (a1)
	add(0x00c5a52f)                           // amoadd.w a0, a2, (a1)
	add(0x00000000)                           // not an instruction
	add(0xffffffff)                           // not an instruction
	add(encI(0x73, 0x300, 11, 1, 10))         // csrrw: not an instruction of the machine
	add(encR(0, 12, 11, 0, 10) ^ 0x33 ^ 0x53) // fadd.s: not an instruction of the machine

	// Compressed instructions. Each is followed by three c.ebreak.
	compressed := []uint32{
		0x0001, // c.nop
		0x0505, // c.addi a0, 1
		0x557d, // c.li a0, -1
		0x6505, // c.lui a0, 1
		0x6141, // c.addi16sp sp, 16
		0x0020, // c.addi4spn s0, sp, 8
		0x0506, // c.slli a0, 1
		0x8005, // c.srli s0, 1
		0x8405, // c.srai s0, 1
		0x987d, // c.andi s0, -1
		0x8c05, // c.sub s0, s1
		0x8c25, // c.xor s0, s1
		0x8c45, // c.or s0, s1
		0x8c65, // c.and s0, s1
		0x852e, // c.mv a0, a1
		0x952e, // c.add a0, a1
		0x4144, // c.lw s1, 4(a0)
		0xc144, // c.sw s1, 4(a0)
		0x4522, // c.lwsp a0, 8(sp)
		0xc42a, // c.swsp a0, 8(sp)
		0xa011, // c.j 4
		0x2011, // c.jal 4
		0xc011, // c.beqz s0, 4
		0xe011, // c.bnez s0, 4
		0x8082, // c.jr ra
		0x9582, // c.jalr a1
		0x9002, // c.ebreak
	}

	put := func(raw uint32, size int) {
		pc := uint32(opsLo + opsSlot*len(slots))
		at := mem[pc : pc+opsSlot]
		for i := 0; i < opsSlot; i += 2 {
			binary.LittleEndian.PutUint16(at[i:], 0x9002)
		}
		if size == 4 {
			binary.LittleEndian.PutUint32(at, raw)
		} else {
			binary.LittleEndian.PutUint16(at, uint16(raw))
		}
		slots = append(slots, opSlot{pc: pc, d: decode(binary.LittleEndian.Uint32(at))})
	}
	for _, raw := range codes {
		put(raw, 4)
	}
	for _, raw := range compressed {
		put(raw, 2)
	}
	lo, hi = opsLo, uint32(opsLo+opsSlot*len(slots))
	if hi > opsSize-0x200 {
		panic("the program of opsProgram does not fit its memory")
	}
	return mem, lo, hi, slots
}

// generated returns the files that Translate makes for the tests of this
// package, by name.
func generated(t *testing.T) map[string][]byte {
	t.Helper()
	mem, lo, hi, _ := opsProgram()
	ops, err := translate(mem, lo, hi, lo, 0, sha256.Sum256(mem), "rvm", "opsCode")
	if err != nil {
		t.Fatal(err)
	}
	guest, err := Translate(loadGuest(t), guestMem, "rvm", "guestCode")
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]byte{
		"ops_code_test.go":   append([]byte(bootTag), ops...),
		"guest_code_test.go": append([]byte(bootTag), guest...),
	}
}

// TestTranslationsAreCurrent holds the two files of translated code to what
// Translate writes now. So a change to Translate, to the decoder, or to
// testdata/guest.elf cannot leave the tests on old code.
func TestTranslationsAreCurrent(t *testing.T) {
	for name, want := range generated(t) {
		if os.Getenv("RVM_WRITE_CODE") == "1" {
			if err := os.WriteFile(name, want, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("%v: run %s", err, writeCode)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is not what Translate writes now: run %s", name, writeCode)
		}
	}
}

// The program of opsProgram must hold the instructions that its comments
// name. This guards the encodings that were made by hand.
func TestOpsProgram(t *testing.T) {
	_, lo, hi, slots := opsProgram()
	seen := map[uint8]int{}
	sizes := map[uint8]int{}
	for _, s := range slots {
		seen[s.d.op]++
		sizes[s.d.size]++
	}
	for op := opLui; op <= opFence; op++ {
		if seen[op] == 0 {
			t.Errorf("no slot has operation %d", op)
		}
	}
	for _, op := range []uint8{opIllegal, opEcall, opEbreak, opLr, opSc, opAmoadd} {
		if seen[op] == 0 {
			t.Errorf("no slot has operation %d, which the code leaves to the interpreter", op)
		}
	}
	if sizes[2] < 25 || sizes[4] < 400 {
		t.Errorf("%d compressed and %d full instructions", sizes[2], sizes[4])
	}
	// Some of the compressed encodings, as the decoder must read them.
	want := map[uint32]insn{
		0x0505: {op: opAddi, rd: 10, rs1: 10, imm: 1},
		0x557d: {op: opAddi, rd: 10, imm: 0xffffffff},
		0x852e: {op: opAdd, rd: 10, rs2: 11},
		0x4144: {op: opLw, rd: 9, rs1: 10, imm: 4},
		0xc42a: {op: opSw, rs1: 2, rs2: 10, imm: 8},
		0xa011: {op: opJal, imm: 4},
		0x2011: {op: opJal, rd: 1, imm: 4},
		0xe011: {op: opBne, rs1: 8, imm: 4},
		0x9582: {op: opJalr, rd: 1, rs1: 11},
		0x8c05: {op: opSub, rd: 8, rs1: 8, rs2: 9},
	}
	for raw, w := range want {
		d := decode(raw | 0x9002<<16)
		w.raw, w.size = d.raw, 2
		if d != w {
			t.Errorf("%#04x decodes to %+v, want %+v", raw, d, w)
		}
	}
	t.Logf("%d slots from %#x to %#x", len(slots), lo, hi)
}
