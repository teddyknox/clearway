package kimage

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/teddyknox/clearway/internal/rvm"
)

// A Process is the embedded kernel program, running in the machine, with an
// input that arrives in parts. It is a record.Stepper.
//
// The program reads one message at a time and answers it at once. So a host
// can give it one input of the kernel, read the verdict, and give the next.
// The answers and the count of instructions are those of a run that has the
// whole input from the start: Machine.Run.
//
// A Process is not safe for use by more than one goroutine at a time.
type Process struct {
	vm  *rvm.Machine
	in  feed
	out bytes.Buffer
	err error // the program has stopped
}

// feed is the input stream of a Process: the bytes that the host has given
// and the program has not read.
type feed struct{ data []byte }

func (f *feed) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, rvm.ErrNoInput
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

var _ io.Reader = (*feed)(nil)

// Start loads the kernel program into a new machine. The program has not run
// yet. If interpret is true, the machine uses no translated code.
func Start(interpret bool) (*Process, error) {
	vm, err := rvm.Load(ELF, Memory)
	if err != nil {
		return nil, fmt.Errorf("the kernel program does not load: %w", err)
	}
	if !interpret {
		if err := vm.UseCode(kernelCode); err != nil {
			return nil, fmt.Errorf("kcode.go is not the translation of kernel.elf: run guest/build.sh: %w", err)
		}
	}
	p := &Process{vm: vm}
	vm.In, vm.Out = &p.in, &p.out
	return p, nil
}

// Step gives the program more bytes of its input stream, and runs the
// program until it has read them all and asks for more. It returns what the
// program wrote in that time.
//
// The input may end inside a message: the program then waits inside that
// message, and the next Step continues it.
//
// If the program stops, Step returns an error, and so does every later
// call. The program stops when it reads the end message, when its input is
// not a stream that docs/vm.md defines, and when the kernel has a defect.
func (p *Process) Step(input []byte) ([]byte, error) {
	if p.err != nil {
		return nil, p.err
	}
	p.in.data = append(p.in.data, input...)
	p.out.Reset()
	exit, err := p.vm.Run(1<<64 - 1)
	switch {
	case errors.Is(err, rvm.ErrNoInput):
		return bytes.Clone(p.out.Bytes()), nil
	case err == nil:
		p.err = fmt.Errorf("the kernel program stopped with exit code %d", exit)
	default:
		p.err = fmt.Errorf("the kernel program stopped: %w", err)
	}
	return bytes.Clone(p.out.Bytes()), p.err
}

// Steps returns the count of instructions that the program has executed.
func (p *Process) Steps() uint64 { return p.vm.Steps }

// MachineDigest returns the state digest of the machine: every register and
// every byte of its memory (docs/vm.md).
func (p *Process) MachineDigest() [32]byte { return p.vm.StateDigest() }
