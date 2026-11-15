package rvm

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// regNames are the ABI names of the registers, as llvm-objdump prints them.
var regNames = [32]string{
	"zero", "ra", "sp", "gp", "tp", "t0", "t1", "t2", "s0", "s1", "a0", "a1", "a2", "a3", "a4", "a5",
	"a6", "a7", "s2", "s3", "s4", "s5", "s6", "s7", "s8", "s9", "s10", "s11", "t3", "t4", "t5", "t6",
}

// opNames are the mnemonics of the operations, by op.
var opNames = [numOps]string{
	opLui: "lui", opAuipc: "auipc", opJal: "jal", opJalr: "jalr",
	opBeq: "beq", opBne: "bne", opBlt: "blt", opBge: "bge", opBltu: "bltu", opBgeu: "bgeu",
	opLb: "lb", opLh: "lh", opLw: "lw", opLbu: "lbu", opLhu: "lhu", opSb: "sb", opSh: "sh", opSw: "sw",
	opAddi: "addi", opSlti: "slti", opSltiu: "sltiu", opXori: "xori", opOri: "ori", opAndi: "andi",
	opSlli: "slli", opSrli: "srli", opSrai: "srai",
	opAdd: "add", opSub: "sub", opSll: "sll", opSlt: "slt", opSltu: "sltu", opXor: "xor", opSrl: "srl",
	opSra: "sra", opOr: "or", opAnd: "and",
	opMul: "mul", opMulh: "mulh", opMulhsu: "mulhsu", opMulhu: "mulhu",
	opDiv: "div", opDivu: "divu", opRem: "rem", opRemu: "remu",
	opFence: "fence", opEcall: "ecall", opEbreak: "ebreak",
	opLr: "lr.w", opSc: "sc.w", opAmoswap: "amoswap.w", opAmoadd: "amoadd.w", opAmoxor: "amoxor.w",
	opAmoand: "amoand.w", opAmoor: "amoor.w", opAmomin: "amomin.w", opAmomax: "amomax.w",
	opAmominu: "amominu.w", opAmomaxu: "amomaxu.w",
}

// signedHex writes v as llvm-objdump writes a signed immediate.
func signedHex(v uint32) string {
	if int32(v) < 0 {
		return fmt.Sprintf("-0x%x", -v)
	}
	return fmt.Sprintf("0x%x", v)
}

// disasm writes a decoded instruction at pc as the full instruction that it
// executes, in the syntax of llvm-objdump without aliases. An instruction
// outside the instruction set gives "".
func disasm(d insn, pc uint32) string {
	name := opNames[d.op]
	rd, rs1, rs2 := regNames[d.rd], regNames[d.rs1], regNames[d.rs2]
	switch d.op {
	case opIllegal:
		return ""
	case opLui, opAuipc:
		return fmt.Sprintf("%s %s, 0x%x", name, rd, d.imm>>12)
	case opJal:
		return fmt.Sprintf("%s %s, 0x%x", name, rd, pc+d.imm)
	case opJalr, opLb, opLh, opLw, opLbu, opLhu:
		return fmt.Sprintf("%s %s, %s(%s)", name, rd, signedHex(d.imm), rs1)
	case opBeq, opBne, opBlt, opBge, opBltu, opBgeu:
		return fmt.Sprintf("%s %s, %s, 0x%x", name, rs1, rs2, pc+d.imm)
	case opSb, opSh, opSw:
		return fmt.Sprintf("%s %s, %s(%s)", name, rs2, signedHex(d.imm), rs1)
	case opAddi, opSlti, opSltiu, opXori, opOri, opAndi:
		return fmt.Sprintf("%s %s, %s, %s", name, rd, rs1, signedHex(d.imm))
	case opSlli, opSrli, opSrai:
		return fmt.Sprintf("%s %s, %s, 0x%x", name, rd, rs1, d.imm)
	case opFence, opEcall, opEbreak:
		return name
	case opLr:
		return fmt.Sprintf("%s %s, (%s)", name, rd, rs1)
	case opSc, opAmoswap, opAmoadd, opAmoxor, opAmoand, opAmoor, opAmomin, opAmomax, opAmominu, opAmomaxu:
		return fmt.Sprintf("%s %s, %s, (%s)", name, rd, rs2, rs1)
	}
	return fmt.Sprintf("%s %s, %s, %s", name, rd, rs1, rs2)
}

// notInSet are the instructions that llvm-objdump knows without an
// extension flag and that are not in the instruction set of the machine.
var notInSet = map[string]bool{
	"<unknown>": true, "unimp": true, "c.unimp": true,
	"mret": true, "sret": true, "uret": true, "dret": true, "wfi": true, "mnret": true,
	"csrrw": true, "csrrs": true, "csrrc": true, "csrrwi": true, "csrrsi": true, "csrrci": true,
	"sfence.vma": true, "sfence.w.inval": true, "sfence.inval.ir": true,
	"hfence.vvma": true, "hfence.gvma": true, "sinval.vma": true, "sctrclr": true,
}

// expand rewrites one line of llvm-objdump as the full instruction that the
// RISC-V specification gives for it: a compressed instruction becomes its
// expansion, the aq and rl suffixes go, and the operands of fence go. An
// instruction that is not in the instruction set of the machine gives "".
func expand(mnemonic string, ops []string) (string, error) {
	if notInSet[mnemonic] {
		return "", nil
	}
	arg := func(i int) string {
		if i < len(ops) {
			return ops[i]
		}
		return "?"
	}
	switch mnemonic {
	case "c.addi4spn":
		return fmt.Sprintf("addi %s, %s, %s", arg(0), arg(1), arg(2)), nil
	case "c.lw", "c.sw", "c.lwsp", "c.swsp":
		return fmt.Sprintf("%s %s, %s", strings.TrimSuffix(mnemonic[2:], "sp"), arg(0), arg(1)), nil
	case "c.nop":
		if len(ops) == 1 { // a hint: c.addi zero, imm
			return fmt.Sprintf("addi zero, zero, %s", arg(0)), nil
		}
		return "addi zero, zero, 0x0", nil
	case "c.addi", "c.andi", "c.srli", "c.srai", "c.slli":
		return fmt.Sprintf("%s %s, %s, %s", mnemonic[2:], arg(0), arg(0), arg(1)), nil
	case "c.srli64", "c.srai64", "c.slli64": // hints: a shift by 0
		return fmt.Sprintf("%s %s, %s, 0x0", strings.TrimSuffix(mnemonic[2:], "64"), arg(0), arg(0)), nil
	case "c.addi16sp":
		return fmt.Sprintf("addi sp, sp, %s", arg(1)), nil
	case "c.li":
		return fmt.Sprintf("addi %s, zero, %s", arg(0), arg(1)), nil
	case "c.lui":
		return fmt.Sprintf("lui %s, %s", arg(0), arg(1)), nil
	case "c.sub", "c.xor", "c.or", "c.and", "c.add":
		return fmt.Sprintf("%s %s, %s, %s", mnemonic[2:], arg(0), arg(0), arg(1)), nil
	case "c.mv":
		return fmt.Sprintf("add %s, zero, %s", arg(0), arg(1)), nil
	case "c.j":
		return fmt.Sprintf("jal zero, %s", arg(0)), nil
	case "c.jal":
		return fmt.Sprintf("jal ra, %s", arg(0)), nil
	case "c.beqz":
		return fmt.Sprintf("beq %s, zero, %s", arg(0), arg(1)), nil
	case "c.bnez":
		return fmt.Sprintf("bne %s, zero, %s", arg(0), arg(1)), nil
	case "c.jr":
		return fmt.Sprintf("jalr zero, 0x0(%s)", arg(0)), nil
	case "c.jalr":
		return fmt.Sprintf("jalr ra, 0x0(%s)", arg(0)), nil
	case "c.ebreak":
		return "ebreak", nil
	case "ecall", "ebreak":
		return mnemonic, nil
	case "fence", "fence.tso", "pause":
		return "fence", nil
	case "prefetch.i", "prefetch.r", "prefetch.w":
		// llvm-objdump gives this name to an ori that writes x0. The low
		// five bits of the immediate are the kind of the prefetch.
		off, reg, _ := strings.Cut(strings.TrimSuffix(arg(0), ")"), "(")
		v, err := strconv.ParseInt(off, 0, 32)
		if err != nil {
			return "", err
		}
		kind := map[string]uint32{"prefetch.i": 0, "prefetch.r": 1, "prefetch.w": 3}[mnemonic]
		return fmt.Sprintf("ori zero, %s, %s", reg, signedHex(uint32(v)|kind)), nil
	}
	if strings.HasPrefix(mnemonic, "c.") {
		return "", fmt.Errorf("unknown compressed instruction")
	}
	// The atomic instructions carry the aq and rl bits as a suffix.
	for _, suffix := range []string{".aqrl", ".aq", ".rl"} {
		mnemonic = strings.TrimSuffix(mnemonic, suffix)
	}
	return mnemonic + " " + strings.Join(ops, ", "), nil
}

// sectionELF returns an ELF32 RISC-V file with one section, .text, that
// holds the given code at address 0.
func sectionELF(text []byte) []byte {
	const names = "\x00.text\x00.shstrtab\x00"
	le := binary.LittleEndian
	b := make([]byte, elfHeaderSize)
	copy(b, "\x7fELF")
	b[4], b[5], b[6] = elfClass32, elfDataLittle, elfVersion
	le.PutUint16(b[16:], elfTypeExec)
	le.PutUint16(b[18:], elfMachineRISC)
	le.PutUint32(b[20:], elfVersion)
	le.PutUint32(b[36:], 1) // e_flags: the C extension
	le.PutUint16(b[40:], elfHeaderSize)
	textOff := len(b)
	b = append(b, text...)
	namesOff := len(b)
	b = append(b, names...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	le.PutUint32(b[32:], uint32(len(b))) // e_shoff
	le.PutUint16(b[46:], 40)             // e_shentsize
	le.PutUint16(b[48:], 3)              // e_shnum
	le.PutUint16(b[50:], 2)              // e_shstrndx
	section := func(name, kind, flags uint32, off, size int) {
		var sh [40]byte
		le.PutUint32(sh[0:], name)
		le.PutUint32(sh[4:], kind)
		le.PutUint32(sh[8:], flags)
		le.PutUint32(sh[16:], uint32(off))
		le.PutUint32(sh[20:], uint32(size))
		le.PutUint32(sh[32:], 1)
		b = append(b, sh[:]...)
	}
	section(0, 0, 0, 0, 0)
	section(1, 1, 6, textOff, len(text))   // SHT_PROGBITS, SHF_ALLOC | SHF_EXECINSTR
	section(7, 3, 0, namesOff, len(names)) // SHT_STRTAB
	return b
}

var objdumpLine = regexp.MustCompile(`^\s*([0-9a-f]+):\s+((?:[0-9a-f]{2} )+|[0-9a-f]{4}|[0-9a-f]{8})\s+(\S+)\s*(.*)$`)

// decodeCodes returns the codes of the decoder tests: every 16-bit code, and
// many 32-bit codes of every major opcode. A code is a 16-bit code if its
// two low bits are not both 1.
func decodeCodes() []uint32 {
	var codes []uint32
	for c := uint32(0); c < 0x10000; c++ {
		if c&3 != 3 {
			codes = append(codes, c)
		}
	}
	rng := prng{state: 99}
	for major := uint32(0); major < 32; major++ {
		if major&7 == 7 {
			continue // an instruction longer than 32 bits; see TestIllegalInstructions
		}
		for range 6000 {
			w := rng.Uint32()&^0x7f | major<<2 | 3
			// Three codes of four get a funct7 that an instruction uses.
			switch rng.IntN(4) {
			case 0:
				w &= 0x01ffffff
			case 1:
				w = w&0x01ffffff | 0x02000000
			case 2:
				w = w&0x01ffffff | 0x40000000
			}
			codes = append(codes, w)
		}
	}
	for imm := uint32(0); imm < 4096; imm++ {
		codes = append(codes, imm<<20|0x73) // SYSTEM with no register
	}
	for f7 := uint32(0); f7 < 128; f7++ {
		for f3 := uint32(0); f3 < 8; f3++ {
			for _, rs2 := range []uint32{0, 1, 31} {
				fields := f7<<25 | rs2<<20 | 11<<15 | f3<<12 | 10<<7
				codes = append(codes, fields|0x33, fields|0x13, fields|0x2f, fields|0x0f)
			}
		}
	}
	return codes
}

// layout returns the codes one after another as machine code, and the
// address of each code.
func layout(codes []uint32) (text []byte, addrs []uint32) {
	for _, w := range codes {
		addrs = append(addrs, uint32(len(text)))
		if w&3 == 3 {
			text = binary.LittleEndian.AppendUint32(text, w)
		} else {
			text = binary.LittleEndian.AppendUint16(text, uint16(w))
		}
	}
	return text, addrs
}

// decodeDigestWant is the SHA-256 of what the decoder gives for the codes
// of decodeCodes: for each code, its address as 4 little-endian bytes, the
// text of disasm, and a line feed. TestDecodeAgainstObjdump passed with
// llvm-objdump of LLVM 23.1.1 when the decoder gave this digest.
const decodeDigestWant = "81d0506efec11e21f4c935732f7431d5208c347e872e93fe84838b6bbfe79e07"

func decodeDigest() string {
	codes := decodeCodes()
	_, addrs := layout(codes)
	h := sha256.New()
	for i, w := range codes {
		h.Write(binary.LittleEndian.AppendUint32(nil, addrs[i]))
		io.WriteString(h, disasm(decode(w), addrs[i]))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// TestDecodeDigest checks that the decoder gives, for every code of
// decodeCodes, what it gave when llvm-objdump last agreed with it. So a
// change of the decoder fails this test on a machine without llvm-objdump.
// After a change that is right, run TestDecodeAgainstObjdump and then put
// the new digest in decodeDigestWant.
func TestDecodeDigest(t *testing.T) {
	if got := decodeDigest(); got != decodeDigestWant {
		t.Errorf("the decoder gives the digest %s, want %s", got, decodeDigestWant)
	}
}

// TestDecodeAgainstObjdump compares the decoder with the disassembler of
// LLVM on the codes of decodeCodes. For each code, either both name it
// outside RV32IMAC, or both give the same full instruction with the same
// registers and the same immediate.
//
// The test needs llvm-objdump, which the tests must not depend on. It runs
// only if the environment variable RVM_OBJDUMP names the program.
func TestDecodeAgainstObjdump(t *testing.T) {
	objdump := os.Getenv("RVM_OBJDUMP")
	if objdump == "" {
		t.Skip("RVM_OBJDUMP does not name an llvm-objdump")
	}
	words := decodeCodes()
	text, addrs := layout(words)
	index := map[uint32]int{}
	for i, addr := range addrs {
		index[addr] = i
	}
	file := filepath.Join(t.TempDir(), "codes.elf")
	if err := os.WriteFile(file, sectionELF(text), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(objdump, "-d", "-M", "no-aliases", "--mattr=+m,+a,+c", file).Output()
	if err != nil {
		t.Fatalf("%s: %v", objdump, err)
	}

	seen, legal, failures := 0, 0, 0
	for _, line := range strings.Split(string(out), "\n") {
		m := objdumpLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		addr, _ := strconv.ParseUint(m[1], 16, 32)
		i, ok := index[uint32(addr)]
		if !ok {
			t.Fatalf("llvm-objdump starts an instruction inside a word: %q", line)
		}
		seen++
		var ops []string
		if m[4] != "" {
			ops = strings.Split(m[4], ", ")
			// A branch target has a symbol after the address.
			last := &ops[len(ops)-1]
			*last, _, _ = strings.Cut(*last, " <")
		}
		want, err := expand(m[3], ops)
		if err != nil {
			t.Fatalf("%v: %q", err, line)
		}
		if words[i]&0x707f == 0x000f {
			// llvm-objdump knows fence only with rd and rs1 of 0 and a
			// known fm field. The specification tells an
			// implementation to ignore those fields.
			want = "fence"
		}
		got := disasm(decode(words[i]), uint32(addr))
		if got != "" {
			legal++
		}
		if got != want {
			if failures++; failures <= 40 {
				t.Errorf("code %#x: the decoder gives %q, llvm-objdump gives %q (%s)", words[i], got, want, strings.TrimSpace(line))
			}
		}
	}
	if seen != len(words) {
		t.Errorf("llvm-objdump shows %d of %d codes", seen, len(words))
	}
	t.Logf("%d codes, %d of them in the instruction set, %d differences", len(words), legal, failures)
	if got := decodeDigest(); failures == 0 && got != decodeDigestWant {
		t.Errorf("llvm-objdump agrees with the decoder; put the digest %s in decodeDigestWant", got)
	}
}
