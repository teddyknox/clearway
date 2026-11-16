// Package rvm is a deterministic virtual machine: one RV32IMAC hart with flat
// memory, an input stream, and an output stream.
//
// The result of a run depends on three things alone: the bytes of the
// program, the size of memory, and the bytes of the input. The package uses
// integer arithmetic only. It reads no clock and no random number, and it
// starts no goroutine. This comment defines the machine; a second
// implementation that follows it computes the same bits.
//
// # State
//
// The machine has a program counter pc, 32 registers x0 to x31 of 32 bits
// each, a memory of a fixed number of bytes, a reservation (set or not set,
// with an address when set), and a halt state (halted or not, with an exit
// code when halted). Register x0 is always 0: an instruction that writes x0
// changes nothing.
//
// Memory is one array of bytes. The valid addresses are 0 to size-1. A value
// of 2 or 4 bytes is little-endian. The address of a load or a store need
// not be a multiple of the size of the value.
//
// # Program and start state
//
// The program is an ELF32 little-endian executable (ET_EXEC) for RISC-V
// (e_machine 243). Load copies the file bytes of each PT_LOAD segment to the
// virtual address of the segment. Every other byte of memory starts as 0. At
// the start, pc is the entry address, x2 (sp) is the size of memory rounded
// down to a multiple of 16, every other register is 0, no reservation is
// set, and the machine has not halted.
//
// # Instructions
//
// The machine executes one instruction at a time. It reads 2 bytes at pc. If
// the two low bits of that value are not both 1, the 2 bytes are a
// compressed instruction. If they are, the instruction is the 4 bytes at pc.
// An instruction that is not a jump or a taken branch adds its length to pc.
//
// The instruction set is RV32I with the extensions M, A, and C, as the
// RISC-V unprivileged specification defines them, with these rules:
//
//   - There is no privileged architecture: no control and status register, no
//     interrupt, no address translation. There is no floating point.
//   - fence is a no-operation, whatever its fields other than funct3 hold.
//   - Division by zero gives a quotient of all ones and a remainder equal to
//     the dividend. The signed division of -2^31 by -1 gives the quotient
//     -2^31 and the remainder 0.
//   - lr.w loads a word and sets the reservation to its address. sc.w stores
//     its word and writes 0 to rd if the reservation is set and has the
//     address of the sc.w; if not, it stores nothing and writes 1. sc.w
//     clears the reservation in both cases. No other instruction changes the
//     reservation. An AMO instruction loads a word, writes to memory the
//     result of its operation on that word and rs2, and writes the loaded
//     word to rd. The aq and rl bits have no effect.
//   - The address of lr.w, of sc.w, and of an AMO instruction must be a
//     multiple of 4.
//   - A compressed instruction acts as the full instruction that the
//     specification gives for it. The link value of c.jal and c.jalr is
//     pc + 2. The compressed instructions are those of RV32C that have no
//     floating-point meaning: c.addi4spn, c.lw, c.sw, c.nop, c.addi, c.jal,
//     c.li, c.addi16sp, c.lui, c.srli, c.srai, c.andi, c.sub, c.xor, c.or,
//     c.and, c.j, c.beqz, c.bnez, c.slli, c.lwsp, c.jr, c.mv, c.ebreak,
//     c.jalr, c.add, and c.swsp. An encoding that the specification names a
//     hint executes as the full instruction it abbreviates, which changes no
//     state but pc.
//   - jalr and c.jr clear bit 0 of the target address.
//
// # Environment calls
//
// The instruction ecall asks the host for a service. Register a7 (x17) holds
// the call number, a0 (x10) and a1 (x11) hold the arguments, and the result
// goes to a0. A completed call adds 4 to pc.
//
//   - Call 0, halt: the machine stops. The value of a0 is the exit code.
//   - Call 1, read: the machine copies bytes from the input stream to memory
//     at address a0, and writes the count of bytes to a0. The count is a1,
//     unless the input ends first; then it is the number of bytes that
//     remained, and 0 at the end of the input. The bytes of the buffer after
//     the count stay unchanged. The view of the guest does not depend on how
//     the host divides the input into parts.
//   - Call 2, write: the machine appends a1 bytes of memory at address a0 to
//     the output stream, and writes a1 to a0.
//
// A read or a write of 0 bytes touches no address and writes 0 to a0.
//
// # Faults
//
// A fault stops the run with a *Fault. The instruction that faults changes
// no state: pc stays at that instruction, and Steps does not count it. These
// are all the faults:
//
//   - The pc is odd.
//   - The 2 bytes at pc, or the 4 bytes of a full instruction, are not all
//     inside memory.
//   - The instruction is not in the instruction set. This includes every
//     floating-point instruction, full or compressed, every CSR instruction,
//     mret, wfi, fence.i, each encoding that RV32IMAC reserves or leaves to
//     custom extensions (a compressed shift by 32 or more is one), and each
//     instruction longer than 4 bytes.
//   - A load, a store, lr.w, sc.w, or an AMO instruction touches an address
//     outside memory. sc.w faults so even when the reservation is not set.
//   - The address of lr.w, of sc.w, or of an AMO instruction is not a
//     multiple of 4. The machine checks this before it checks the range.
//   - The instruction is ebreak or c.ebreak.
//   - The number of an environment call is not 0, 1, or 2.
//   - The buffer of a read or a write call, of 1 byte or more, is not all
//     inside memory. The call then reads no input and writes no output.
//
// # Limits of the host
//
// Three events are outside the definition above, because they come from the
// host and not from the guest. Run returns ErrStepLimit when the caller's
// step limit comes before a halt or a fault; the caller can continue the
// run. Run returns ErrNoInput when the guest asks for input that the host
// does not have yet; the read call has then not happened, and the caller can
// continue the run when it has more input. If the input or the output stream
// returns another error, Run returns that error and the machine cannot
// continue.
//
// # Speed
//
// Two things make a run faster, and neither changes its result.
//
// Run keeps decoded instructions in a cache. An entry is used only when the
// bytes at pc equal the bytes that the entry was decoded from, so a store
// into code, by the guest or by the caller, has the same effect as on a
// machine without a cache.
//
// Translate writes Go source for the instructions of one program: a Code.
// A machine with a Code (UseCode) executes the code where it applies and
// interprets every other instruction. The code executes whole blocks of
// integer computations, loads, stores, branches, and jumps. It stops before
// an instruction that would fault, before a store into the program, and
// before every other kind of instruction, and the interpreter takes that
// instruction. The count of instructions and the state after each count are
// those of the interpreter. The interpreter is the definition, and the tests
// hold each Code to it.
package rvm
