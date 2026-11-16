# The machine and the kernel program

| | |
|---|---|
| Version | Machine 0.1, input stream 2. Draft, 2026-11-15 |
| Purpose | To define the one machine that runs the kernel, and the one program that is the kernel, so that every party computes the same bits |
| Code | [internal/rvm](../internal/rvm) is the machine. [guest/](../guest) is the kernel in Rust. [internal/kimage](../internal/kimage) holds the built program and runs it |
| Reads with | [spec.md](spec.md) for what the kernel computes. [record.md](record.md) for the record that a replay checks |

**Words.** "Must" states a requirement. The *machine* is the virtual machine of section 2. The *kernel program* is the file of section 3. The *host* is the software that runs the machine and gives it input.

## 1. Why a machine

Replicas of a cell vote on the state of the kernel, and an outside party replays a record of it ([record.md](record.md)). Both need the same inputs to give the same bits everywhere. Two things can break that.

1. **Arithmetic.** Floating point rounds, and how it rounds can depend on the processor. The first kernel of this project gave different last digits on two processor types, and a record of one did not replay on the other. The kernel now computes with integers only ([spec.md](spec.md), section 1). That removes this cause.
2. **Translation.** A compiler turns source code into instructions for one processor. Two compilers, two versions of one compiler, or two processor types give different instructions. A defect in any of them can give a different result on one replica only.

The machine removes the second cause. The kernel is published as one program for one small instruction set. A replica, a checker, and an outside party all run the same bytes, on a machine that this document defines completely. The SHA-256 of the program names it. Two parties that run the program with that hash on a correct machine get the same answers, whatever their own processor is.

The machine has no floating-point instruction. A program that tried to use floating point would stop with a fault.

**Two kernels.** The repository also holds the kernel in Go (`internal/kernel`). The simulator uses it: it runs in the same process, and it is some times as fast as the kernel program in the machine (section 5). The two must agree on every bit. Section 5 says how that is tested.

## 2. The machine

The machine is one RISC-V processor of 32 bits with the extensions M, A, and C (RV32IMAC), a flat memory, an input stream, and an output stream. The result of a run depends on three things alone: the bytes of the program, the size of the memory, and the bytes of the input.

### 2.1 State

- A program counter `pc`.
- 32 registers, `x0` to `x31`, of 32 bits each. `x0` is always 0: an instruction that writes `x0` changes nothing.
- A memory of a fixed number of bytes. The valid addresses are 0 to size − 1. A value of 2 or 4 bytes is little-endian. The address of a load or a store need not be a multiple of the size of the value.
- A reservation: set or not set, with an address when it is set.
- A halt state: halted or not, with an exit code when halted.

### 2.2 Program and start state

The program is an ELF32 little-endian executable (`ET_EXEC`) for RISC-V (`e_machine` 243). The loader copies the file bytes of each `PT_LOAD` segment to the virtual address of the segment. The segments must lie inside the memory, be in rising order of address, and not overlap. Every other byte of memory starts as 0.

At the start:

- `pc` is the entry address.
- `x2`, the stack pointer, is the size of the memory, rounded down to a multiple of 16.
- Every other register is 0. No reservation is set. The machine has not halted.

### 2.3 Instructions

The machine executes one instruction at a time. It reads 2 bytes at `pc`. If the two low bits of that value are not both 1, the 2 bytes are a compressed instruction. If they are, the instruction is the 4 bytes at `pc`. An instruction that is not a jump or a taken branch adds its length to `pc`.

The instruction set is RV32I with the extensions M, A, and C, as the RISC-V unprivileged specification defines them, with these rules.

- There is no privileged architecture: no control and status register, no interrupt, no address translation. There is no floating point.
- `fence` does nothing, whatever its fields other than `funct3` hold.
- Division by zero gives a quotient of all ones and a remainder equal to the dividend. The signed division of −2^31 by −1 gives the quotient −2^31 and the remainder 0.
- `lr.w` loads a word and sets the reservation to its address. `sc.w` stores its word and writes 0 to `rd` if the reservation is set and has the address of the `sc.w`. If not, it stores nothing and writes 1. `sc.w` clears the reservation in both cases. No other instruction changes the reservation.
- An AMO instruction loads a word, writes to memory the result of its operation on that word and `rs2`, and writes the loaded word to `rd`. The `aq` and `rl` bits have no effect.
- The address of `lr.w`, of `sc.w`, and of an AMO instruction must be a multiple of 4.
- A compressed instruction acts as the full instruction that the specification gives for it. The link value of `c.jal` and `c.jalr` is `pc + 2`. The compressed instructions are those of RV32C that have no floating-point meaning. An encoding that the specification names a hint executes as the full instruction that it abbreviates, which changes no state but `pc`.
- `jalr` and `c.jr` clear bit 0 of the target address.

### 2.4 Environment calls

The instruction `ecall` asks the host for a service. Register `a7` (`x17`) holds the number of the call. `a0` (`x10`) and `a1` (`x11`) hold the arguments, and the result goes to `a0`. A completed call adds 4 to `pc`.

| Call | Name | Effect |
|---|---|---|
| 0 | halt | The machine stops. The value of `a0` is the exit code. |
| 1 | read | The machine copies bytes from the input stream to memory at address `a0`, and writes the count of bytes to `a0`. The count is `a1`, unless the input ends first. Then it is the number of bytes that remained, and 0 at the end of the input. The bytes of the buffer after the count stay unchanged. |
| 2 | write | The machine appends `a1` bytes of memory at address `a0` to the output stream, and writes `a1` to `a0`. |

A read or a write of 0 bytes touches no address and writes 0 to `a0`. What the program sees does not depend on how the host divides the input into parts.

The machine has no other service: no clock, no random number, no file, no network.

### 2.5 Faults

A fault stops the run. The instruction that faults changes no state: `pc` stays at that instruction, and the count of executed instructions does not include it. These are all the faults.

- The `pc` is odd.
- The 2 bytes at `pc`, or the 4 bytes of a full instruction, are not all inside the memory.
- The instruction is not in the instruction set. This includes every floating-point instruction, full or compressed, every CSR instruction, `mret`, `wfi`, `fence.i`, each encoding that RV32IMAC reserves or leaves to custom extensions, and each instruction longer than 4 bytes.
- A load, a store, `lr.w`, `sc.w`, or an AMO instruction touches an address outside the memory.
- The address of `lr.w`, of `sc.w`, or of an AMO instruction is not a multiple of 4. The machine checks this before it checks the range.
- The instruction is `ebreak` or `c.ebreak`.
- The number of an environment call is not 0, 1, or 2.
- The buffer of a read or a write call, of 1 byte or more, is not all inside the memory. The call then reads no input and writes no output.

### 2.6 The host

Three events come from the host and not from the program.

- The host may stop a run after a number of instructions, and continue it later.
- The host may have no input yet. If a read call asks for more bytes than the host has, and the input has not ended, the run stops before the call. The state of the machine is the state before the call, and the count of instructions does not include it. The host continues the run when it has more input. So a host can give a program one message, read the answer, and then give the next.
- If the input or the output stream fails, the run ends and cannot continue.

The machine counts the instructions that it executes. The count is part of the result: the same program on the same input executes the same number of instructions on every correct machine.

### 2.7 Machine state digest

A machine can state its whole state as one SHA-256 value. Two machines that ran the same program on the same input for the same number of instructions have the same digest. The digest covers, in this order, all little-endian: the tag `clearway rvm state 0.1` and a line feed, `pc`, the 32 registers, the reservation (a flag and an address), the halt state (a flag and an exit code), the size of the memory as 64 bits, and the memory.

### 2.8 Translated code

The interpreter of `internal/rvm` executes one instruction at a time. It defines the machine, and it is slow. For a program that is known ahead of time, the package also writes *translated code*: Go source that does what the instructions of the program do. The Go compiler builds it into the instructions of the host. `rvm.Translate` writes the source, and `cmd/rvgen` is the command for it.

Translated code changes how fast the machine reaches a state. It must not change the state. These rules keep it so.

- **One decoder.** `Translate` reads each instruction with the decoder of the interpreter.
- **Blocks.** Translated code is made of *blocks*. A block is a run of instructions that control enters at the first and leaves after the last. Each block is decoded from the bytes at its own first address. If control reaches an address at which no block starts, the interpreter continues from there. So the choice of blocks changes the speed alone.
- **Plain instructions only.** A block holds integer computations, loads, stores, branches, and jumps. The interpreter executes every other instruction: an environment call, `ebreak`, an atomic instruction, and a code that is not an instruction.
- **No fault in translated code.** Before a load or a store, translated code tests the address. If the instruction would fault, translated code stops before it, and the interpreter executes it and reports the fault. Translated code is Go with no unsafe code. An address that it did not test would stop the host program. It could not reach memory outside the machine.
- **Stores into the program.** Translated code stops before a store that touches the instructions that it stands for. The interpreter executes that store. From then on, the machine uses no translated code.
- **The registers are current before each instruction.** Translated code writes each new value of a register to the registers of the machine at once. So it can stop before any instruction, and the state is then the state that the interpreter has there.
- **The count of instructions.** Translated code counts the instructions that it executes. A run with a limit of N instructions stops after exactly N, with translated code or without it. So two parties can still compare machine state digests at any count.
- **The program must be the program.** Translated code states the SHA-256 of the instruction bytes that it stands for. The machine refuses translated code if the bytes in its memory differ.

The interpreter stays the definition, and it is the check on translated code. Section 5 says how translated code is held to it. `record verify -vm -interpret` uses the interpreter alone.

## 3. The kernel program

| | |
|---|---|
| Source | [guest/kernel](../guest/kernel): the kernel of [spec.md](spec.md), in Rust. [guest/rv](../guest/rv): the entry point, the environment calls, and a heap |
| File | [internal/kimage/kernel.elf](../internal/kimage/kernel.elf), 124,512 bytes |
| SHA-256 | `000f52918e6d48b14e9b2a09902a53a3af8d5fdcedefb2aa212357f573eeef68` |
| Translated code | `internal/kimage/kcode.go`: 38,278 instructions in 6,604 blocks (section 2.8). `rvm.Translate` wrote it from the file above, and a test fails if it is not what `Translate` writes now |
| Memory | 64 MiB |
| Built with | `guest/build.sh`: rustc 1.99.1, target `riscv32imac-unknown-none-elf`, no network |

**Rules of the source.** A test in `internal/arch` holds `guest/kernel` to them.

- It has no runtime and no dependency: `#![no_std]`, and no other crate.
- It has no floating-point type and no floating-point literal.
- It has no unsafe code: `#![forbid(unsafe_code)]`. Only `guest/rv` has unsafe code, for the environment calls and the heap.
- An integer overflow is a defect. The program is built with overflow checks. On an overflow it stops with exit code 101, and it does not continue with a wrong value.

**What `guest/rv` adds.** The machine gives a program a stack pointer and three environment calls. The kernel also needs a heap. `guest/rv` has one block of 40 MiB, cut into blocks whose sizes are powers of two. A freed block is used again for the next request of its size. The program has one thread and no interrupts.

`guest/rv` keeps no input and no output of its own. It asks the machine for exactly the bytes that the kernel needs next, and it gives the machine each answer when the kernel has it. So a host that gives one message gets the answer to it before it must give the next (section 2.6).

**To build it.**

```bash
./guest/build.sh
```

The script builds the program, copies it to `internal/kimage/kernel.elf`, writes its translated code to `internal/kimage/kcode.go`, and prints its SHA-256. A change to the program needs the two new files and the new hash in `internal/kimage/kimage.go` in the same commit. A test fails if they differ.

## 4. The two streams

The host gives the program one input stream: a header, and then the inputs of one kernel in order. The program writes one output stream: its answers. The program reads a message, applies it, writes the answer if there is one, and reads the next. So a host can give the whole of a record at once, or one message at a time. `record verify -vm` does the first. `kimage.Process` does the second, for the replicas of a cell ([replication.md](replication.md)).

Every number is little-endian. `i64` is a signed integer of 64 bits in two's complement. `u8`, `u32`, and `u64` are unsigned. A flag is a `u8` that is 0 or 1.

### 4.1 Header

| Field | Type | Meaning |
|---|---|---|
| magic | 8 bytes | `CLWK`, then the version 2, then three zero bytes |
| arrangement | `u8` | 0: the exact Verifier alone. 1: the exact and the sampled Verifier ([spec.md](spec.md), section 5.3) |
| parameters | 17 × `i64`, a flag, `i64` | `Sh`, `Sv`, `VTol`, `Horizon`, `CommitHorizon`, `IntruderHorizon`, `ExtendSlack`, `W`, `E0`, `Bh`, `K`, `MinLead`, `CellRadius`, `IntruderR0`, `IntruderK`, `IntruderVU`, `RecontractTol`, then the intent check as a flag, then `IntentLookahead` |

The host must check the parameters before it starts the program ([spec.md](spec.md), section 8). The program does not check them.

### 4.2 Messages

Each message starts with one byte, its kind.

| Kind | Input | Fields after the kind |
|---|---|---|
| 0 | Advance | time `i64` |
| 1 | Admit | flight `i64`, trajectory |
| 2 | Propose | flight `i64`, proposer `u8`, trajectory |
| 3 | ProposeRecontract | flight `i64`, proposer `u8`, trajectory |
| 4 | Ack | flight `i64` |
| 5 | Refuse | flight `i64` |
| 6 | DeliveryFailed | flight `i64` |
| 7 | UpdateTrack | flight, time, x, y, velocity x, velocity y, altitude, vertical rate: 8 × `i64`. Then a flag: the track has a selected altitude. Then the selected altitude `i64` |
| 8 | MarkIntruder | flight `i64` |
| 9 | Re-anchor | none |
| 10 | Remove | flight `i64` |
| 11 | Tick | none |
| 12 | SetAuthority | flight `i64`, actor `u8` |
| 250 | A request for the state digest | none |
| 255 | The end of the stream | none |

A proposer or an actor is 0 for automation and 1 for a human.

A **trajectory** is: the number of legs `u32`; for each leg `t0`, `t1`, `p0.x`, `p0.y`, `v.x`, `v.y` as 6 × `i64`; the number of vertical segments `u32`; for each segment `t0`, `t1`, `a0`, `a1` as 4 × `i64`. The program reads a trajectory with more than 256 legs or segments to its end and treats it as empty. The kernel rejects an empty trajectory as *malformed*.

### 4.3 Answers

The program writes one answer for each Admit, Propose, and ProposeRecontract, and one for each request for the state digest, in the order of the messages.

| Answer | Bytes |
|---|---|
| A verdict that commits | `1`, `1`, the tube identifier of the contract `u64` |
| A verdict that rejects | `1`, `0`, the reason `u8`, the other flight `i64`, the time of the conflict `i64` |
| A state digest | `2`, then 32 bytes ([record.md](record.md), section 5) |

The reasons, from 1: *unknown-flight*, *duplicate*, *authority*, *intruder*, *busy*, *malformed*, *lead*, *vertical*, *open*, *conflict*, *disagree*.

### 4.4 Exit codes

| Code | Meaning |
|---|---|
| 0 | The program read the end of the stream. |
| 2 | The input ended inside a message, or does not start with `CLWK`. |
| 3 | The header is for another version of the stream. |
| 4 | A message has a kind or a value that the stream does not define. |
| 101 | A defect in the kernel: an integer overflow, an index out of range, or a full heap. |

## 5. How it is tested

**The interpreter** (`internal/rvm`).

- Each instruction, full and compressed, against cases that state the expected registers and memory.
- The decoder against the encodings that the assembler of the Rust toolchain produced for 771 pieces of assembly. With `llvm-objdump` available, the decoder agrees with it on 233,536 codes: every code of 16 bits, and about 184,000 of 32 bits. A digest of those decodes is in a test that needs no tool.
- A program in Rust that does 128-bit arithmetic, division, SHA-256, and memory copies. It executes about 19.8 million instructions, and each byte of its output is compared with the result of Go's own libraries.
- Every fault of section 2.5, and that a fault changes no state.
- The loader: valid files, 33 kinds of invalid file, and every truncation of a valid one.
- A run that stops because the host has no input yet: it changes no state, and it continues to the same result, with translated code and without.
- Determinism: the same run with the input in one part and in many parts, with one call and with one call for each instruction, and with the cache of decoded instructions on and off. The count of instructions and the machine state digest must be equal.
- Two fuzz tests: one gives the loader and the machine arbitrary files, and one runs arbitrary bytes as code in four ways that must agree. The first ran for 39 million inputs without a failure.
- The helper that wrote the interpreter put 142 defects of one line each into it. The tests caught 141. The remaining one does not change what the machine does.

**Translated code** (`internal/rvm`, `internal/kimage`). Each test runs one program twice from one state, with translated code and with the interpreter alone, and requires the same state after: `pc`, registers, every byte of memory, the reservation, the halt state, the count of instructions, and the error.

- *Each instruction.* A program of 566 instructions holds every kind that `Translate` translates, with the destination also a source, with register 0 as a source and as the destination, and in compressed form. Each instruction runs 250 times from random states: values at which an operation has a special result, addresses at the edges of the memory and of the program, and random values. That is 141,500 runs. The test also requires that translated code executed the instruction whenever it could.
- *A whole program.* The Rust program of the interpreter tests, 19.8 million instructions, of which 99.99% run in translated code. Its output is still checked against Go's libraries. The two machines are stopped at more than 200 random counts of instructions and compared.
- *The kernel program.* Both kernel vectors run in the interpreter and as translated code, with the same count of instructions. On the session vector the two machines are stopped at 28 random counts, and their machine state digests are equal.
- *A store into the program.* The interpreter takes it, the machine drops the translated code, and the new instruction runs.
- *The code in the repository is current.* The tests translate the programs again and compare the result with the files.
- *Defects.* 36 defects of one line were put into `Translate`, one at a time. The tests caught 35. The other one changes no line of the code that `Translate` writes.

**The kernel program** (`internal/kimage`, `internal/record`).

- The file has the hash that the code states.
- The program reproduces the two kernel vectors of [conformance/](../conformance/README.md) in the machine: each verdict and each state digest, to the last bit.

| Vector | Verdicts | State digests | Instructions |
|---|---|---|---|
| Session: every kind of input | 11 | 16 | 7,744,230 |
| Traffic: 30 minutes from an empty cell, 240 flights an hour | 428 | 30 | 1,583,215,064 |

- *One message at a time.* A host gives the program the lines of both vectors one at a time. Each verdict line comes back before the next line goes in, and at each checkpoint of the record the program has its number of lines, kernel time, chain value, and state digest. The same holds when the host cuts the input into parts of any size.

- The program also reproduces longer records in the machine. These runs are not in the tests. They were made with `record verify -vm`, as translated code and in the interpreter alone, with the same count of instructions.

| Record | Verdicts | State digests | Instructions |
|---|---|---|---|
| 2 h, a hostile planner with aircraft faults | 2,395 | 120 | 31,473,603,436 |
| 6 h, a hostile planner with aircraft faults and flights with no radio | 7,400 | 360 | 126,821,992,256 |
| 6 h, dense traffic at 240 flights an hour, with aircraft faults (translated code only) | 10,066 | 360 | 281,777,278,454 |

- The same Rust kernel as a native program reproduces three records of four to six hours: 21,788 verdicts and 960 state digests.
- Simulated traffic has only usual inputs. So a test builds random sessions of unusual ones: aircraft that do not move, or that move a micrometre in a second, a radius that grows faster than the aircraft flies, legs of one microsecond, values at the limits, and proposals that the kernel must refuse. The Go kernel applies each session, and the Rust kernel must give the same answers. It does: in the machine for 132 sessions (120 as translated code and 12 in the interpreter alone; 5,559 verdicts and 1,968 state digests), and as a native program for 600 (24,968 verdicts and 8,795 state digests).

**To check a record with the program.**

```bash
go run ./cmd/record verify -vm conformance/kernel-traffic-0.2.rec.gz
```

**Speed.** On one core of a 2025 laptop the interpreter executes about 285 million instructions each second, and translated code about 4.5 billion.

| Record | Go kernel | Kernel program, translated code | Kernel program, interpreter |
|---|---|---|---|
| The traffic vector, 30 minutes from an empty cell | 0.1 s | 0.4 s | 5.6 s |
| 2 h at 120 flights an hour | 1.6 s | 7 s | 1.8 min |
| 6 h at 120 flights an hour | 5 s | 28 s | 7.3 min |
| 6 h at 240 flights an hour | 11 s | 64 s | not run |

So translated code replays six hours of this traffic about 750 times as fast as the traffic itself, and dense traffic about 340 times as fast. The interpreter replays the six hours about 50 times as fast as the traffic. Translated code is about 6 times as slow as the Go kernel, and the interpreter about 90 times.

**Where the time went.** The first kernel program took 4.55 billion instructions for the traffic vector, and the interpreter needed 16 seconds for them. Three measurements explain the distance from a native program, and two changes followed.

- *The program did work twice.* A profile by function showed 53% of the instructions in one computation that depends on one tube alone, and 12% in the speed of a leg. The Rust kernel computed both again at every check of a pair. It now computes them once for each tube, as the Go kernel does. That removed 65% of the instructions and changed no answer.
- *The interpreter.* It spends about 14 processor cycles on one instruction of the program. Translated code spends about 1.
- *Registers of 32 bits.* The same Rust source, built for a 64-bit processor, executes about 3 times fewer instructions. This remains.

## 6. What is not done

- **The build is not reproducible by others yet.** The bytes of the program depend on the version of the compiler. Until a second party builds the same bytes from the same source with a pinned toolchain, the hash names a program, and nothing but this repository links the program to its source.
- **There is one machine.** A second machine by another author, or an existing RISC-V emulator with the three environment calls, would test this one. The machine is tested, and it is not proved.
- **The second kernel is not independent.** One author wrote the Go kernel and the Rust kernel from one specification. They agree on every record that was tried. Both can share one misreading.
- **Translated code puts the Go compiler between the program and the result.** A party that runs translated code trusts the Go compiler to build it right. The interpreter is the check, and it is about 16 times as slow. In the replicated cell a replica can use the interpreter alone, and the replicas compare their states at every block ([replication.md](replication.md)): in its tests, one replica of four does. `record verify -vm -interpret` lets a party that checks a record choose the interpreter.
- **Translated code is large.** The translation of the kernel program is 4.5 MB of Go source in the repository, and it must be written again when the program changes.
- **Registers of 32 bits.** One exact product of two 64-bit values takes many instructions, and so does the integer square root of the sampled Verifier, which works one bit at a time. An instruction set with registers of 64 bits would need about 3 times fewer instructions for the same source. The kernel program is not tuned beyond the one change of section 5.
- **No bound on the work of one input.** The machine counts instructions, so a bound can be stated and enforced. None is yet.
- **A fixed heap.** The program stops with exit code 101 if its 40 MiB are full. Every replica stops at the same input, so they do not diverge, but the cell stops. A cell must refuse traffic before that point.
- **The simulator uses the Go kernel.** The replicated cell runs the program on every replica, and it replays the record of a simulation ([replication.md](replication.md)). The simulator itself does not put its inputs through the cell.
- **The A extension is not needed.** The program has one thread. The target of the compiler includes atomic instructions, so the machine has them.
