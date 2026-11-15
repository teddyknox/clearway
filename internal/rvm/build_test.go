package rvm

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The files of the guest programs. The tests read the built files, which are
// committed, so they pass on a machine without the Rust toolchain.
const (
	guestCrate  = "testdata/guest"                // the cargo crate
	guestELF    = "testdata/guest.elf"            // built: the guest program
	insnGolden  = "testdata/insn.golden"          // built: the machine code of each snippet of the tests
	insnSource  = "testdata/guest/src/bin/insn.s" // generated: the snippets of the tests
	guestTarget = "riscv32imac-unknown-none-elf"
)

// TestMain rebuilds the guest files before the tests, but only if the
// environment variable RVM_REBUILD_GUEST is 1 and cargo is on PATH.
func TestMain(m *testing.M) {
	if os.Getenv("RVM_REBUILD_GUEST") == "1" {
		if _, err := exec.LookPath("cargo"); err != nil {
			fmt.Fprintln(os.Stderr, "rvm: RVM_REBUILD_GUEST=1, but cargo is not on PATH; the tests use the committed files")
		} else if err := rebuildGuest(); err != nil {
			fmt.Fprintln(os.Stderr, "rvm: rebuild of the guest files:", err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// The snippets that tests other than TestInstructions use.
const (
	asmEcall = "ecall"
	asmSpin  = "jal zero, 0"
	asmHalt  = "addi a7, zero, 0\naddi a0, zero, 42\necall"

	// asmPatch replaces its first instruction with the word in a1, then
	// executes it again. The caller sets t0 to the address of the snippet.
	asmPatch    = "1:\naddi a0, a0, 1\nbne a2, zero, 2f\naddi a2, zero, 1\nsw a1, 0(t0)\njal zero, 1b\n2:"
	asmAdd100   = "addi a0, a0, 100"
	asmAdd1     = "addi a0, a0, 1"
	asmCAdd1    = "c.addi a0, 1"
	asmCAdd2    = "c.addi a0, 2"
	asmCountTo9 = "1:\naddi a0, a0, 1\nblt a0, a1, 1b"
)

// snippets returns the text of each snippet of the tests once, in the order
// of first use.
func snippets() []string {
	var out []string
	seen := map[string]bool{}
	use := func(asm string) {
		if !seen[asm] {
			seen[asm] = true
			out = append(out, asm)
		}
	}
	for _, tc := range insnTests() {
		use(tc.asm)
	}
	for _, asm := range []string{asmEcall, asmSpin, asmHalt, asmPatch, asmAdd100, asmAdd1, asmCAdd1, asmCAdd2, asmCountTo9} {
		use(asm)
	}
	return out
}

// usesCompressed reports whether a snippet names a compressed instruction.
// The assembler compresses nothing in the other snippets, so that they test
// the full encodings.
func usesCompressed(asm string) bool {
	for _, word := range strings.Fields(asm) {
		if strings.HasPrefix(word, "c.") {
			return true
		}
	}
	return false
}

// rebuildGuest writes insnSource from the test cases, builds the crate with
// cargo, and rewrites guestELF, guestDigest, and insnGolden.
func rebuildGuest() error {
	list := snippets()

	var src strings.Builder
	src.WriteString("# The snippets of the tests of internal/rvm, one for each label insn_N.\n")
	src.WriteString("# RVM_REBUILD_GUEST=1 go test ./internal/rvm/ writes this file. Do not edit it.\n")
	src.WriteString(".section .text.insn,\"ax\",@progbits\n.option norelax\n.globl _start\n_start:\n")
	for i, asm := range list {
		fmt.Fprintf(&src, ".globl insn_%d\ninsn_%d:\n.option push\n", i, i)
		if usesCompressed(asm) {
			src.WriteString(".option rvc\n")
		} else {
			src.WriteString(".option norvc\n")
		}
		for _, line := range strings.Split(asm, "\n") {
			fmt.Fprintf(&src, "    %s\n", strings.TrimSpace(line))
		}
		src.WriteString(".option pop\n")
	}
	fmt.Fprintf(&src, ".globl insn_%d\ninsn_%d:\n", len(list), len(list))
	if err := os.WriteFile(insnSource, []byte(src.String()), 0o644); err != nil {
		return err
	}

	cmd := exec.Command("cargo", "build", "--release", "--target", guestTarget, "--offline")
	cmd.Dir = guestCrate
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cargo build: %v\n%s", err, out)
	}
	built := filepath.Join(guestCrate, "target", guestTarget, "release")

	guest, err := os.ReadFile(filepath.Join(built, "guest"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(guestELF, guest, 0o644); err != nil {
		return err
	}
	digest, err := guestDigestText(guest)
	if err != nil {
		return fmt.Errorf("run of the new guest program: %v", err)
	}
	if err := os.WriteFile(guestDigest, []byte(digest), 0o644); err != nil {
		return err
	}

	// Cut the code of each snippet out of the linked insn program.
	f, err := elf.Open(filepath.Join(built, "insn"))
	if err != nil {
		return err
	}
	defer f.Close()
	text := f.Section(".text")
	if text == nil {
		return fmt.Errorf("insn program has no .text section")
	}
	code, err := text.Data()
	if err != nil {
		return err
	}
	syms, err := f.Symbols()
	if err != nil {
		return err
	}
	addr := map[string]uint64{}
	for _, s := range syms {
		addr[s.Name] = s.Value
	}
	var golden bytes.Buffer
	golden.WriteString("# The machine code of each snippet of the tests of internal/rvm, in hexadecimal,\n")
	golden.WriteString("# from the assembler of the Rust toolchain. A tab ends the code; the rest of\n")
	golden.WriteString("# the line is the snippet, with \" ; \" for each line break.\n")
	golden.WriteString("# RVM_REBUILD_GUEST=1 go test ./internal/rvm/ writes this file. Do not edit it.\n")
	for i, asm := range list {
		lo, ok1 := addr[fmt.Sprintf("insn_%d", i)]
		hi, ok2 := addr[fmt.Sprintf("insn_%d", i+1)]
		if !ok1 || !ok2 || lo < text.Addr || hi < lo || hi-text.Addr > uint64(len(code)) {
			return fmt.Errorf("insn program has no code for snippet %d", i)
		}
		fmt.Fprintf(&golden, "%s\t%s\n", hex.EncodeToString(code[lo-text.Addr:hi-text.Addr]), strings.ReplaceAll(asm, "\n", " ; "))
	}
	return os.WriteFile(insnGolden, golden.Bytes(), 0o644)
}

// loadGolden returns the machine code of each snippet in insnGolden, by the
// text of the snippet.
func loadGolden(t testing.TB) map[string][]byte {
	t.Helper()
	data, err := os.ReadFile(insnGolden)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		code, asm, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("%s: line without a tab: %q", insnGolden, line)
		}
		b, err := hex.DecodeString(code)
		if err != nil {
			t.Fatalf("%s: %v in line %q", insnGolden, err, line)
		}
		out[strings.ReplaceAll(asm, " ; ", "\n")] = b
	}
	return out
}

// code returns the machine code of a snippet. The test fails if insnGolden
// does not hold the snippet.
func code(t testing.TB, golden map[string][]byte, asm string) []byte {
	t.Helper()
	b, ok := golden[asm]
	if !ok {
		t.Fatalf("%s has no code for the snippet %q; run RVM_REBUILD_GUEST=1 go test ./internal/rvm/ with cargo on PATH", insnGolden, asm)
	}
	return b
}

// loadGuest returns the committed guest program.
func loadGuest(t testing.TB) []byte {
	t.Helper()
	b, err := os.ReadFile(guestELF)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A segment is one PT_LOAD segment of a test image.
type segment struct {
	vaddr uint32
	data  []byte
	memsz uint32 // 0 means len(data)
	flags uint32 // 0 means read, write, and execute
}

// buildELF returns an ELF32 RISC-V executable with the given entry address
// and segments.
func buildELF(entry uint32, segs ...segment) []byte {
	le := binary.LittleEndian
	head := make([]byte, elfHeaderSize+elfProgramSize*len(segs))
	copy(head, "\x7fELF")
	head[4], head[5], head[6] = elfClass32, elfDataLittle, elfVersion
	le.PutUint16(head[16:], elfTypeExec)
	le.PutUint16(head[18:], elfMachineRISC)
	le.PutUint32(head[20:], elfVersion)
	le.PutUint32(head[24:], entry)
	le.PutUint32(head[28:], elfHeaderSize)
	le.PutUint16(head[40:], elfHeaderSize)
	le.PutUint16(head[42:], elfProgramSize)
	le.PutUint16(head[44:], uint16(len(segs)))
	out := head
	for i, s := range segs {
		ph := out[elfHeaderSize+elfProgramSize*i:]
		memsz, flags := s.memsz, s.flags
		if memsz == 0 {
			memsz = uint32(len(s.data))
		}
		if flags == 0 {
			flags = 7
		}
		le.PutUint32(ph[0:], elfProgramLoad)
		le.PutUint32(ph[4:], uint32(len(out)))
		le.PutUint32(ph[8:], s.vaddr)
		le.PutUint32(ph[12:], s.vaddr)
		le.PutUint32(ph[16:], uint32(len(s.data)))
		le.PutUint32(ph[20:], memsz)
		le.PutUint32(ph[24:], flags)
		le.PutUint32(ph[28:], 1)
		out = append(out, s.data...)
	}
	return out
}
