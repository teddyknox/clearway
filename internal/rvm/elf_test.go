package rvm

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"
)

// testImage returns a valid image with two segments: 16 bytes at 0x1000,
// and 4 bytes at 0x2000 that 60 zero bytes follow in memory.
func testImage() []byte {
	return buildELF(0x1004,
		segment{vaddr: 0x1000, data: []byte("0123456789abcdef"), flags: 5},
		segment{vaddr: 0x2000, data: []byte{1, 2, 3, 4}, memsz: 64, flags: 6})
}

// The offsets of the two program headers of testImage.
const (
	ph0 = elfHeaderSize
	ph1 = elfHeaderSize + elfProgramSize
)

func TestLoad(t *testing.T) {
	img := testImage()
	m, err := Load(img, 0x4007)
	if err != nil {
		t.Fatal(err)
	}
	if m.PC != 0x1004 || m.Steps != 0 || len(m.Mem) != 0x4007 {
		t.Errorf("pc = %#x, Steps = %d, %#x bytes of memory", m.PC, m.Steps, len(m.Mem))
	}
	for r, v := range m.X {
		if want := uint32(0x4000) * flag(r == 2); v != want {
			t.Errorf("x%d = %#x, want %#x", r, v, want)
		}
	}
	if halted, code := m.Halted(); halted || code != 0 || m.reserved || m.resAddr != 0 {
		t.Errorf("the machine starts halted or with a reservation")
	}
	want := make([]byte, 0x4007)
	copy(want[0x1000:], "0123456789abcdef")
	copy(want[0x2000:], []byte{1, 2, 3, 4})
	if !bytes.Equal(m.Mem, want) {
		t.Errorf("memory differs from the segments")
	}

	// The machine does not share memory with the image.
	for i := range img {
		img[i] = 0xff
	}
	if !bytes.Equal(m.Mem, want) {
		t.Errorf("a change of the image changed the memory of the machine")
	}

	// The stack pointer is the memory size rounded down to a multiple of 16.
	for size, want := range map[uint32]uint32{0x2040: 0x2040, 0x2041: 0x2040, 0x204f: 0x2040, 0x2050: 0x2050, 1 << 20: 1 << 20} {
		m, err := Load(testImage(), size)
		if err != nil || m.X[2] != want {
			t.Errorf("memory of %#x bytes: sp = %#x, error %v, want sp = %#x", size, m.X[2], err, want)
		}
	}
	empty := buildELF(0)
	for size, want := range map[uint32]uint32{0: 0, 1: 0, 15: 0, 16: 16, 31: 16} {
		m, err := Load(empty, size)
		if err != nil || m.X[2] != want || len(m.Mem) != int(size) {
			t.Errorf("memory of %d bytes: sp = %d, error %v, want sp = %d", size, m.X[2], err, want)
		}
	}
}

// TestLoadAccepts checks images at the edge of the rules.
func TestLoadAccepts(t *testing.T) {
	le := binary.LittleEndian
	for name, edit := range map[string]func(b []byte) []byte{
		"no program header, with a wild table offset": func(b []byte) []byte {
			le.PutUint16(b[44:], 0)
			le.PutUint32(b[28:], 0xfffffff0)
			return b
		},
		"a segment that is not PT_LOAD, with wild fields": func(b []byte) []byte {
			le.PutUint32(b[ph0:], 4)
			le.PutUint32(b[ph0+4:], 0xffffffff)
			le.PutUint32(b[ph0+8:], 0xffffffff)
			le.PutUint32(b[ph0+16:], 0xffffffff)
			le.PutUint32(b[ph0+20:], 0x1000)
			return b
		},
		"segments that touch": func(b []byte) []byte {
			le.PutUint32(b[ph1+8:], 0x1010)
			return b
		},
		"a segment of no bytes": func(b []byte) []byte {
			le.PutUint32(b[ph1+16:], 0)
			le.PutUint32(b[ph1+20:], 0)
			return b
		},
		"a segment of no bytes at the end of memory": func(b []byte) []byte {
			le.PutUint32(b[ph1+8:], 0x2040)
			le.PutUint32(b[ph1+16:], 0)
			le.PutUint32(b[ph1+20:], 0)
			return b
		},
		"a segment that ends at the end of memory": func(b []byte) []byte { return b },
		"bytes after the last segment":             func(b []byte) []byte { return append(b, 1, 2, 3) },
		"an entry address outside memory": func(b []byte) []byte {
			le.PutUint32(b[24:], 0xfffffffe)
			return b
		},
	} {
		if m, err := Load(edit(testImage()), 0x2040); err != nil || m == nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// TestLoadErrors checks that Load returns an error, and no machine, for
// each kind of bad image.
func TestLoadErrors(t *testing.T) {
	le := binary.LittleEndian
	put16 := func(off int, v uint16) func([]byte) []byte {
		return func(b []byte) []byte { le.PutUint16(b[off:], v); return b }
	}
	put32 := func(off int, v uint32) func([]byte) []byte {
		return func(b []byte) []byte { le.PutUint32(b[off:], v); return b }
	}
	cut := func(n int) func([]byte) []byte {
		return func(b []byte) []byte { return b[:n] }
	}
	size := len(testImage())
	for _, c := range []struct {
		name    string
		edit    func([]byte) []byte
		memSize uint32
		want    string // a part of the error
	}{
		{"an empty image", cut(0), 0x4000, "shorter than the ELF header"},
		{"a cut header", cut(elfHeaderSize - 1), 0x4000, "shorter than the ELF header"},
		{"a wrong magic number", func(b []byte) []byte { b[0] = 0; return b }, 0x4000, "not an ELF file"},
		{"a 64-bit image", func(b []byte) []byte { b[4] = 2; return b }, 0x4000, "not 32-bit"},
		{"a big-endian image", func(b []byte) []byte { b[5] = 2; return b }, 0x4000, "not little-endian"},
		{"a wrong ident version", func(b []byte) []byte { b[6] = 0; return b }, 0x4000, "unknown version"},
		{"a wrong file version", put32(20, 2), 0x4000, "unknown version"},
		{"a relocatable file", put16(16, 1), 0x4000, "not an executable"},
		{"a shared object", put16(16, 3), 0x4000, "not an executable"},
		{"a core file", put16(16, 4), 0x4000, "not an executable"},
		{"no machine", put16(18, 0), 0x4000, "not for RISC-V"},
		{"an x86 image", put16(18, 3), 0x4000, "not for RISC-V"},
		{"an ARM image", put16(18, 40), 0x4000, "not for RISC-V"},
		{"an x86-64 image", put16(18, 62), 0x4000, "not for RISC-V"},
		{"an AArch64 image", put16(18, 183), 0x4000, "not for RISC-V"},
		{"a wrong program header size", put16(42, 56), 0x4000, "program header size"},
		{"too many program headers", put16(44, 0xffff), 0x4000, "too many program headers"},
		{"a program header table after the image", put32(28, uint32(size-elfProgramSize)), 0x4000, "table is outside the image"},
		{"a program header table at the top of the address space", put32(28, 0xfffffff0), 0x4000, "table is outside the image"},
		{"a cut program header table", cut(ph1 + 10), 0x4000, "table is outside the image"},
		{"cut segment bytes", cut(size - 1), 0x4000, "segment 1 is outside the image"},
		{"a segment offset after the image", put32(ph0+4, uint32(size)), 0x4000, "segment 0 is outside the image"},
		{"a segment offset at the top of the address space", put32(ph0+4, 0xfffffff8), 0x4000, "segment 0 is outside the image"},
		{"more file bytes than memory bytes", put32(ph1+20, 3), 0x4000, "more file bytes than memory bytes"},
		{"a segment past the end of memory", nil, 0x203f, "segment 1 [0x2000, 0x2040) does not fit in 8255 bytes"},
		{"a segment after the end of memory", nil, 0x1800, "segment 1 [0x2000, 0x2040) does not fit"},
		{"memory smaller than the first segment", nil, 0x100f, "segment 0 [0x1000, 0x1010) does not fit"},
		{"no memory", nil, 0, "segment 0 [0x1000, 0x1010) does not fit"},
		{"a segment at the top of the address space", put32(ph1+8, 0xfffffff0), 0x4000, "does not fit"},
		{"a segment with a memory size of 4 GiB", put32(ph1+20, 0xffffffff), 0x4000, "does not fit"},
		{"segments that overlap", put32(ph1+8, 0x100f), 0x4000, "overlaps an earlier segment"},
		{"segments at one address", put32(ph1+8, 0x1000), 0x4000, "overlaps an earlier segment"},
		{"segments out of order", put32(ph1+8, 0x800), 0x4000, "out of order"},
	} {
		img := testImage()
		if c.edit != nil {
			img = c.edit(img)
		}
		m, err := Load(img, c.memSize)
		if err == nil || m != nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: Load = %v, %v, want an error with %q", c.name, m != nil, err, c.want)
		}
	}
}

// TestLoadTruncated cuts images at every length. Load must return an error
// while a byte that it needs is missing, and must never panic.
func TestLoadTruncated(t *testing.T) {
	img := testImage()
	for n := range len(img) {
		if m, err := Load(img[:n], 0x4000); err == nil || m != nil {
			t.Errorf("image cut to %d of %d bytes: Load returned no error", n, len(img))
		}
	}

	guest := loadGuest(t)
	f, err := elf.NewFile(bytes.NewReader(guest))
	if err != nil {
		t.Fatal(err)
	}
	var needed uint64 // the count of bytes that Load reads
	for _, p := range f.Progs {
		if p.Type == elf.PT_LOAD {
			needed = max(needed, p.Off+p.Filesz)
		}
	}
	if needed == 0 || needed >= uint64(len(guest)) {
		t.Fatalf("the guest image needs %d of its %d bytes", needed, len(guest))
	}
	for n := 0; n <= len(guest); n++ {
		if n > 600 && n%211 != 0 && uint64(n) != needed-1 && uint64(n) != needed && n != len(guest) {
			continue
		}
		m, err := Load(guest[:n], guestMem)
		if (err == nil) != (uint64(n) >= needed) {
			t.Errorf("guest image cut to %d bytes, of which Load needs %d: error %v", n, needed, err)
		}
		if (m == nil) != (err != nil) {
			t.Errorf("guest image cut to %d bytes: machine %v with error %v", n, m != nil, err)
		}
	}

	// The guest needs more memory than its segments end at.
	if _, err := Load(guest, 1<<20); err == nil || !strings.Contains(err.Error(), "does not fit in 1048576 bytes") {
		t.Errorf("guest in 1 MiB: error %v", err)
	}
}

// TestLoadEntry checks that a bad entry address is a fault of the first
// instruction, not an error of Load.
func TestLoadEntry(t *testing.T) {
	for entry, reason := range map[uint32]string{
		0x1001:     "odd pc",
		0x4000:     "fetch out of range",
		0xfffffffe: "fetch out of range",
		0x3000:     "illegal instruction 0x0000",
	} {
		m, err := Load(buildELF(entry, segment{vaddr: 0x1000, data: []byte{1, 0, 1, 0}}), 0x4000)
		if err != nil {
			t.Fatal(err)
		}
		_, err = m.Run(10)
		wantFault(t, fmt.Sprintf("entry %#x", entry), err, entry, 0, reason)
	}
}
