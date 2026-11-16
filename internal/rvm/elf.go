package rvm

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// The values of the ELF32 format that Load reads.
const (
	elfHeaderSize  = 52
	elfProgramSize = 32 // one entry of the program header table

	elfClass32     = 1
	elfDataLittle  = 1
	elfVersion     = 1
	elfTypeExec    = 2
	elfMachineRISC = 243
	elfProgramLoad = 1 // PT_LOAD
	elfFlagExec    = 1 // PF_X
	elfManyHeaders = 0xffff
)

// maxCacheLen bounds the decode cache: 1<<22 entries hold 8 MiB of code
// without a collision and take 64 MiB.
const maxCacheLen = 1 << 22

// Load builds a machine from an ELF image with memSize bytes of memory.
//
// The image must be an ELF32 little-endian executable (ET_EXEC) for RISC-V.
// Load copies the file bytes of every PT_LOAD segment to the segment's
// virtual address; the other bytes of memory are zero. The segments must lie
// inside memory, and they must stand in the program header table in rising
// address order without overlap. Load ignores the section headers and every
// other segment type.
//
// The machine starts with pc at the entry address, sp (x2) at memSize rounded
// down to a multiple of 16, and every other register 0. Load does not check
// the entry address: a bad one faults at the first instruction.
//
// Load returns an error, and no machine, for an image that breaks one of
// these rules. Load allocates memSize bytes; the caller chooses a size that
// the host can supply.
func Load(elf []byte, memSize uint32) (*Machine, error) {
	if len(elf) < elfHeaderSize {
		return nil, errors.New("rvm: ELF image is shorter than the ELF header")
	}
	le := binary.LittleEndian
	switch {
	case string(elf[:4]) != "\x7fELF":
		return nil, errors.New("rvm: image is not an ELF file")
	case elf[4] != elfClass32:
		return nil, errors.New("rvm: ELF image is not 32-bit")
	case elf[5] != elfDataLittle:
		return nil, errors.New("rvm: ELF image is not little-endian")
	case elf[6] != elfVersion || le.Uint32(elf[20:]) != elfVersion:
		return nil, errors.New("rvm: ELF image has an unknown version")
	case le.Uint16(elf[16:]) != elfTypeExec:
		return nil, errors.New("rvm: ELF image is not an executable (ET_EXEC)")
	case le.Uint16(elf[18:]) != elfMachineRISC:
		return nil, errors.New("rvm: ELF image is not for RISC-V")
	}
	var (
		entry = le.Uint32(elf[24:])
		phoff = uint64(le.Uint32(elf[28:]))
		phnum = uint64(le.Uint16(elf[44:]))
	)
	if phnum == elfManyHeaders {
		return nil, errors.New("rvm: ELF image has too many program headers")
	}
	if phnum != 0 && le.Uint16(elf[42:]) != elfProgramSize {
		return nil, errors.New("rvm: ELF image has a wrong program header size")
	}
	if phnum != 0 && phoff+phnum*elfProgramSize > uint64(len(elf)) {
		return nil, errors.New("rvm: ELF program header table is outside the image")
	}

	// Check every segment before the allocation of memory.
	type segment struct{ offset, vaddr, filesz uint64 }
	var (
		segs           []segment
		end            uint64 // the end of the last PT_LOAD segment
		codeLo, codeHi uint64 // the span of the executable segments
	)
	for i := uint64(0); i < phnum; i++ {
		ph := elf[phoff+i*elfProgramSize:]
		if le.Uint32(ph[0:]) != elfProgramLoad {
			continue
		}
		var (
			offset = uint64(le.Uint32(ph[4:]))
			vaddr  = uint64(le.Uint32(ph[8:]))
			filesz = uint64(le.Uint32(ph[16:]))
			memsz  = uint64(le.Uint32(ph[20:]))
			flags  = le.Uint32(ph[24:])
		)
		switch {
		case filesz > memsz:
			return nil, fmt.Errorf("rvm: ELF segment %d has more file bytes than memory bytes", i)
		case offset+filesz > uint64(len(elf)):
			return nil, fmt.Errorf("rvm: ELF segment %d is outside the image", i)
		case vaddr+memsz > uint64(memSize):
			return nil, fmt.Errorf("rvm: ELF segment %d [0x%x, 0x%x) does not fit in %d bytes of memory", i, vaddr, vaddr+memsz, memSize)
		case vaddr < end:
			return nil, fmt.Errorf("rvm: ELF segment %d overlaps an earlier segment or is out of order", i)
		}
		end = vaddr + memsz
		segs = append(segs, segment{offset, vaddr, filesz})
		if flags&elfFlagExec != 0 && memsz != 0 {
			if codeHi == 0 {
				codeLo = vaddr
			}
			codeHi = end
		}
	}

	m := &Machine{PC: entry, Mem: make([]byte, memSize)}
	m.codeLo, m.codeHi, m.loadEnd = uint32(codeLo), uint32(codeHi), uint32(end)
	m.X[2] = memSize &^ 15
	for _, s := range segs {
		copy(m.Mem[s.vaddr:], elf[s.offset:s.offset+s.filesz])
	}

	// One cache entry for every two bytes of code keeps the entries of
	// different instructions apart.
	n := uint64(minCacheLen)
	for n < (codeHi-codeLo)/2 && n < maxCacheLen {
		n *= 2
	}
	m.cache = make([]insn, n)
	return m, nil
}
