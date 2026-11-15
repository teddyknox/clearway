package rvm

import (
	"crypto/sha256"
	"encoding/binary"
)

// digestTag starts the input of every state digest.
const digestTag = "clearway rvm state 0.1\n"

// StateDigest returns the SHA-256 of the whole machine state: pc, the 32
// registers, the reservation, the halt state, and memory. Two machines that
// ran the same program on the same input for the same number of steps have
// the same digest.
//
// The input of the hash is, in this order, with every number little-endian:
//
//   - the 23 bytes "clearway rvm state 0.1\n";
//   - pc, 4 bytes;
//   - x0 to x31, 4 bytes each, with x0 as 0;
//   - 4 bytes that hold 1 if a reservation is set and 0 if not, then the
//     address of the reservation, 4 bytes, 0 if none is set;
//   - 4 bytes that hold 1 if the guest has halted and 0 if not, then the
//     exit code, 4 bytes, 0 if the guest has not halted;
//   - the length of memory, 8 bytes;
//   - the bytes of memory.
//
// The digest covers every value that a later instruction can read. It does
// not cover Steps, the position of the input stream, or the output.
//
// StateDigest does not change the machine.
func (m *Machine) StateDigest() [32]byte {
	le := binary.LittleEndian
	head := make([]byte, 0, len(digestTag)+4+32*4+8+8+8)
	head = append(head, digestTag...)
	head = le.AppendUint32(head, m.PC)
	head = le.AppendUint32(head, 0)
	for _, v := range m.X[1:] {
		head = le.AppendUint32(head, v)
	}
	var reserved, halted uint32
	if m.reserved {
		reserved = 1
	}
	if m.halted {
		halted = 1
	}
	head = le.AppendUint32(head, reserved)
	head = le.AppendUint32(head, m.resAddr)
	head = le.AppendUint32(head, halted)
	head = le.AppendUint32(head, m.exit)
	head = le.AppendUint64(head, uint64(len(m.Mem)))

	h := sha256.New()
	h.Write(head)
	h.Write(m.Mem)
	var out [32]byte
	h.Sum(out[:0])
	return out
}
