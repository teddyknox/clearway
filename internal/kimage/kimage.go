// Package kimage holds the kernel program: the kernel in Rust (guest/), built
// for the machine of package rvm, and a way to run it.
//
// The program is the published form of the kernel. A replica, a checker, and
// an outside party all run these bytes on the same machine, and so all
// compute the same answers. docs/vm.md defines the machine, the program's
// input stream, and its answers.
package kimage

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/teddyknox/clearway/internal/rvm"
)

// ELF is the kernel program, as guest/build.sh built it.
//
//go:embed kernel.elf
var ELF []byte

// SHA256 names the kernel program: the SHA-256 of ELF, in hexadecimal.
// guest/build.sh prints it. A test holds ELF to it.
const SHA256 = "000f52918e6d48b14e9b2a09902a53a3af8d5fdcedefb2aa212357f573eeef68"

// Memory is the size of the machine that the program runs in (bytes). The
// program keeps a heap of 40 MiB; the stack grows down from the top.
const Memory = 64 << 20

// Digest returns the SHA-256 of an image, in hexadecimal.
func Digest(elf []byte) string {
	sum := sha256.Sum256(elf)
	return hex.EncodeToString(sum[:])
}

// Machine runs a kernel program in the machine of package rvm. It is a
// record.Program. The zero value runs the embedded program with no limit on
// the number of instructions.
//
// The machine executes the embedded program as translated code (kcode.go,
// which rvm.Translate wrote from ELF), and interprets what the code leaves.
// The answers, the count of instructions, and the states of the machine are
// those of the interpreter alone. Interpret makes the machine interpret every
// instruction: the interpreter defines the machine, and it is the check on
// the translated code.
type Machine struct {
	Image     []byte // the program; nil means ELF
	MaxSteps  uint64 // the most instructions to execute; 0 means no limit
	Interpret bool   // use no translated code
}

// Run executes the program on one input stream. It returns the output, the
// exit code, and the number of instructions executed. A fault of the machine
// and a step limit that is reached are errors: the program did not finish.
func (m Machine) Run(input []byte) (output []byte, exit uint32, steps uint64, err error) {
	image := m.Image
	if image == nil {
		image = ELF
	}
	vm, err := rvm.Load(image, Memory)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("the kernel program does not load: %w", err)
	}
	if m.Image == nil && !m.Interpret {
		if err := vm.UseCode(kernelCode); err != nil {
			return nil, 0, 0, fmt.Errorf("kcode.go is not the translation of kernel.elf: run guest/build.sh: %w", err)
		}
	}
	var out bytes.Buffer
	vm.In, vm.Out = bytes.NewReader(input), &out
	limit := m.MaxSteps
	if limit == 0 {
		limit = 1<<64 - 1
	}
	exit, err = vm.Run(limit)
	if errors.Is(err, rvm.ErrStepLimit) {
		err = fmt.Errorf("the kernel program did not finish in %d instructions", limit)
	}
	return out.Bytes(), exit, vm.Steps, err
}
