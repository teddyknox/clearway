# Clearway record format

| | |
|---|---|
| Version | 0.2, draft, 2026-11-15 |
| Purpose | To let a party outside the operator check what a kernel decided, with no help from the operator |
| Built in | `internal/record`, `cmd/record`, and `atcsim -record` |
| Reads with | [spec.md](spec.md) for the kernel, [vm.md](vm.md) for the kernel program that a replay can run, and [design.md](design.md) section 9.5 for why the record exists |

**What changed from 0.1.** Every number in a record is now a whole number, in the units of the kernel: micrometres, microseconds, micrometres per second. Version 0.1 held floating-point numbers, and a replay on another type of processor could differ in the last digit of a result. A record of 0.2 has one replay on every machine.

**Words.** "Must" states a requirement. "May" states a permission.

A *record* is the history of one kernel: every input and every verdict, in order. The lines are chained by hash. *Checkpoints* state the value of the chain and the state of the kernel, and the operator signs them. A party that holds a record can check four things.

| Check | What it shows | What it does not show |
|---|---|---|
| Chain | No line that a checkpoint covers was changed, added, or removed after the checkpoint was made. | Who made the checkpoint. |
| Signature | The checkpoints carry signatures of the keys that the checker trusts. | That the signer did not also sign another history. |
| Witness | The record contains the history that a witness saw at the time. | Anything, if no witness got the checkpoints at the time. |
| Replay | The public kernel, from the same inputs, gives the same verdicts and reaches the same states. | That the inputs are true. A false position replays perfectly. |

Each check finds what the others cannot.

- A change that leaves no trace in the kernel, for example one track that moves by one foot, passes the replay. The chain finds it.
- A verdict that the signer changes and signs again passes the chain and the signatures. The replay finds it.
- A different history that the kernel did produce, signed by the right key, passes every check of the record alone. Only a checkpoint that someone else holds shows that it is not the first history.

So the record makes one promise: **the operator cannot change the history unseen, if one witness outside the operator holds the checkpoints.** It makes no promise about the world outside the kernel.

## 1. The file

A record is a text file in UTF-8. Each line is one JSON object and ends with a newline (byte 0x0A).

- Each object has a name `k` that gives the *kind* of the line.
- A line must have the names of its kind, and no others. Section 2 lists them.
- A writer must not repeat a name in an object.
- **Numbers.** Each number is a whole number in decimal, with no fraction and no exponent. It fits in 64 bits. A line with any other number is not a line of this format.
- **Units.** A time is in microseconds, a length or an altitude in micrometres, and a speed in micrometres per second ([spec.md](spec.md), section 1).

There are three groups of lines.

| Group | Kinds | Chained |
|---|---|---|
| Header | `header` | Yes |
| Input and verdict | Thirteen kinds of input, and `verdict` | Yes |
| Checkpoint | `checkpoint` | No |

The first line is the header, and there is one header. A *chained line* is any line that is not a checkpoint. The header has the *number* 0, and each later chained line has the next number.

## 2. Lines

### 2.1 Header

| Name | Value |
|---|---|
| `clearway-record` | The version of this format: `"0.2"` |
| `spec` | The version of the kernel specification that the kernel follows |
| `cell` | The name of the cell: 1 to 64 of the characters `A-Z a-z 0-9 . _ -` |
| `verifier` | The Verifier arrangement that the kernel declares: `"dual"` or `"single"` (specification, section 5.3) |
| `params` | The parameters of the cell, by the names of the specification, section 8, in the units of the kernel. `IntentCheck` is `true` or `false` |
| `note` | Free text. Optional |

The kernel starts with an empty Store and the clock at 0.

### 2.2 Inputs

Each input line is one input of the kernel. The kernel applies inputs one at a time, in the order of the lines. `f` is a flight identifier, a whole number. `by` is `"automation"` or `"human"`.

| `k` | Other names | The kernel does this (specification) |
|---|---|---|
| `advance` | `t` | Moves the clock to `t`, if `t` is later (7.10) |
| `admit` | `f`, `traj` | Admit (7.2) |
| `propose` | `f`, `by`, `traj` | Propose (7.3). `traj` is the tail |
| `recontract` | `f`, `by`, `traj` | ProposeRecontract (7.4) |
| `ack` | `f` | Ack (7.5) |
| `refuse` | `f` | Refuse (7.5) |
| `fail` | `f` | DeliveryFailed (7.5) |
| `track` | `f`, `t`, `p`, `v`, `alt`, `vr`, and `sel` if the aircraft broadcasts a selected altitude | UpdateTrack (7.6) |
| `mark-intruder` | `f` | Makes the flight an intruder (7.10) |
| `reanchor` | none | Re-anchors every eligible flight (7.8) |
| `tick` | none | Tick (7.9) |
| `remove` | `f` | Removes the flight (7.10) |
| `authority` | `f`, `by` | Gives authority for the flight to `by` (7.10) |

A trajectory has the form of the conformance suite: `{"legs":[{"t0":…,"t1":…,"p0":[x,y],"v":[vx,vy]}],"vsegs":[{"t0":…,"t1":…,"a0":…,"a1":…}]}`. In a track, `p` is the position `[x, y]`, `v` is the velocity `[vx, vy]`, `alt` is the altitude, `vr` is the vertical rate, and `sel` is the selected altitude.

### 2.3 Verdict

The kernel answers each `admit`, `propose`, and `recontract` with a verdict. The verdict line must be the next chained line after the input.

| Verdict | Line |
|---|---|
| Committed | `{"k":"verdict","ok":true,"contract":N}`. `N` is the identifier of the new tube (specification, section 6) |
| Rejected | `{"k":"verdict","ok":false,"reason":R}`. `R` is a reason of the specification, section 7 |
| Rejected for `conflict` or `disagree` | The line also has `with`, the other flight, and `at`, the time of the conflict |

### 2.4 An example

```
{"k":"header","clearway-record":"0.2","spec":"0.2","cell":"test-cell","verifier":"dual","params":{"Sh":9260000000,…},"note":"…"}
{"k":"advance","t":0}
{"k":"admit","f":1,"traj":{"legs":[{"t0":0,"t1":2400000000,"p0":[-296320000000,0],"v":[246933333,0]}],"vsegs":[{"t0":0,"t1":2400000000,"a0":10668000000,"a1":10668000000}]}}
{"k":"verdict","ok":true,"contract":1}
{"k":"track","f":1,"t":5000000,"p":[-295085333335,0],"v":[246933333,0],"alt":10668000000,"vr":0,"sel":10668000000}
{"k":"tick"}
{"k":"checkpoint","cell":"test-cell","t":60000000,"seq":91,"head":"3f…","state":"a7…","sigs":[{"key":"6a…","sig":"0c…"}]}
```

The flight of the example is level at 35,000 ft (10,668,000,000 µm), at 480 kt (246,933,333 µm/s).

## 3. The chain

The chain gives one value of 32 bytes for each chained line.

- `h(−1)` is 32 zero bytes.
- `h(n) = SHA-256( h(n−1) ‖ line n )`.

`line n` is the bytes of chained line n as they are in the file, without the newline. A reader hashes the bytes that it reads. It does not parse a line and write it again, so the format needs no rule for a canonical form of JSON.

`h(n)` depends on every byte of lines 0 to n and on their order. The header is line 0, so the chain covers the parameters and the name of the cell.

## 4. Checkpoint

A checkpoint states: after chained line `seq`, the chain has the value `head`, the clock of the kernel shows `t`, and the kernel has the state digest `state`.

| Name | Value |
|---|---|
| `cell` | The name of the cell |
| `seq` | The number of the last chained line before the checkpoint |
| `t` | The clock of the kernel |
| `head` | `h(seq)`, as 64 hexadecimal digits |
| `state` | The state digest (section 5), as 64 hexadecimal digits |
| `sigs` | A list of signatures. Each has `key`, an Ed25519 public key as 64 hexadecimal digits, and `sig`, a signature as 128 hexadecimal digits. Optional |

- A checkpoint may stand after any chained line, except between a proposal and its verdict.
- A checkpoint is a statement about the chain. It is not a part of the chain. A record without one of its checkpoints is the same history: a later checkpoint covers the same lines.
- Chained lines after the last checkpoint have no checkpoint that covers them.

**The signed message.** Each signature is an Ed25519 signature (RFC 8032) of these six lines of text. Each line ends with a newline.

```
clearway checkpoint 0.2
<cell>
<seq, in decimal>
<t, in decimal>
<head, 64 hexadecimal digits>
<state, 64 hexadecimal digits>
```

Hexadecimal digits in the message are lowercase. Every value of the checkpoint is in the message, so no value can change under a signature.

**Several signers.** Each replica of a kernel signs with its own key (design, section 9.5). A checker names the keys that it trusts and the number of them that each checkpoint needs. One key that signs twice counts once.

## 5. State digest

The state digest is the SHA-256 of the bytes below. It covers every value that a later operation of the kernel can read: the clock, each flight with its track and its tubes, each window, and the result of the last Tick.

The bytes are the 19 bytes of the text `clearway state 0.2` with a newline, and then a sequence of *words*. A word is 8 bytes, least significant byte first.

| Notation | Word |
|---|---|
| `u(x)` | The whole number x, not negative |
| `s(x)` | The whole number x in two's complement |
| `b(x)` | 1 if x is true, otherwise 0 |

```
s(clock)
u(last tube identifier)                     0 if the kernel has made no tube
u(number of flights)
for each flight, in ascending order of its identifier:
    s(flight)
    u(authority)                            0 automation, 1 human
    u(pending state)                        0 none, 1 awaiting, 2 unknown
    b(intruder)
    b(has a track)
    if it has a track:
        s(t) s(x) s(y) s(vx) s(vy) s(alt) s(vertical rate)
        b(has a selected altitude) s(selected altitude)     the altitude is 0 if the track has none
    CONTRACT(active)
    CONTRACT(pending)
    TUBE(intruder tube)
u(number of windows)
for each window, in ascending order of (lower tube, higher tube):
    u(lower tube identifier) u(higher tube identifier) s(end of the window)
u(number of predicted conflicts)
for each predicted conflict, in the order of the list:
    s(flight A) s(flight B) s(t) b(with an intruder)
s(coast time)                               the largest 64-bit integer if no two contracts have a predicted conflict

TUBE(none)     = u(0)
TUBE(x)        = u(1) u(identifier) s(anchor) s(r0) s(k) s(vu0) s(vuRate) s(vmin)
                 u(number of legs), then for each leg: s(t0) s(t1) s(x) s(y) s(vx) s(vy)
                 u(number of vertical segments), then for each: s(t0) s(t1) s(a0) s(a1)
CONTRACT(none) = u(0)
CONTRACT(c)    = TUBE(c) s(divergeAt) b(issued to an intruder)
```

The specification defines each value: the Store in section 6, and the list of predicted conflicts and the coast time in section 7.9.

Every value of the Store is an integer, so the digest has one form. Two kernels that follow the specification and have applied the same inputs have the same digest, on any machine and in any language. The kernel in Go and the kernel in Rust of this repository do ([vm.md](vm.md), section 5).

## 6. Checking a record

`record verify` reads a record once and makes these checks. A record is untrusted data: the checker trusts no value in it.

**Format.** Each line is a line of section 2, 3, or 4, with the names of its kind and no others. The header comes first. Each proposal has its verdict on the next chained line. After a failure of this check, the checker stops.

**Chain.** At each checkpoint, `cell` is the cell of the header, `seq` is the number of the last chained line, and `head` is the value that the checker computed. A failure names the lines between the last checkpoint and this one.

- If the checkpoint is signed as the checker requires, the checker continues from the `seq` and `head` of the checkpoint. One changed line then fails one checkpoint, and the later lines are still checked.
- Otherwise the checker continues from its own values.

**Signature.** Each signature is valid for the message of section 4. If the checker names trusted keys, each checkpoint has valid signatures from the required number of them.

With no trusted key, the checker tests each signature against the key that the record itself names. This shows that the record is consistent. It does not show who made it: anyone can make a record that passes.

**Witness.** The checker takes a list of checkpoints from a source other than the record. For each one, the chain of the record must have the same value at the same line. A witnessed checkpoint counts only if it is signed as the checker requires.

- If the value differs, the record is not the history that the witness saw. The signer has signed two histories.
- If the record ends before the line, the end of the record is missing.

**Replay.** The checker builds a kernel from the header, with the Verifier implementations of its own program, and applies every input.

- Each verdict of the replay must equal the verdict line: the same decision, the same reason, the same other flight, the same time to the microsecond, and the same tube identifier.
- At each checkpoint, the clock and the state digest of the replay must equal `t` and `state`.

The replay depends on the inputs alone, and not on what the record says about the kernel. So it continues after a difference where it can.

| Difference | The checker |
|---|---|
| A state digest | Reports the first and counts the rest. The replay continues |
| Both kernels reject a proposal, for different reasons | Reports the first and counts the rest. The replay continues: the two hold the same contracts |
| One kernel commits a proposal and the other rejects it | Reports it and stops the replay: from here the two hold different contracts |

The last row is the finding that matters most. If the record says *committed* and the replay says *rejected: conflict*, the kernel approved a clearance that the public Verifier refuses.

**Replay with the kernel program.** With `-vm`, the checker also gives the inputs of the record to the kernel program in the machine of [vm.md](vm.md): the header, each input, and a request for the state digest at each checkpoint. It compares each answer of the program with the record, bit for bit, and reports the first difference. The machine runs the program as translated code. With `-interpret` it interprets every instruction, which is the definition of the machine and about 16 times as slow. Both give the same answers and the same count of instructions. This replay does not use the Go kernel of the checker. It uses the bytes that the hash in the result names. So it is the replay that two parties with different computers can both make and compare.

**What the result says.** The tool prints one line for each check, and for a check that did not run it says so. `PASS` means that every check that ran found nothing. Without a trusted key and a witness, that is a statement about the file only.

## 7. Writing a record

`internal/record.Writer` writes a record for one kernel.

- The kernel passes each input to the Writer before it applies the input. It passes each verdict before the operation returns. So a record that ends with a proposal shows that the kernel stopped before it answered.
- The caller asks for a checkpoint between two operations of the kernel. The simulator asks once for each minute on the kernel's clock, and at the end of the run.
- **Failure.** If a line cannot be written, the Writer keeps the first error and writes nothing more. The file is then a valid record that ends early. The Writer does not stop the kernel. The caller must check for the error before it relies on the record.

## 8. Limits and open points

- **One replay on every machine: what this rests on.** The first kernel of this project computed in floating point, and failed here. A four-hour run was recorded on an arm64 processor and on an amd64 processor, and each record was replayed on the other type. Each replay failed at the checkpoint for the seventh minute. The cause was one instruction, fused multiply-add, which the compiler uses on one type and not on the other.

  The kernel now computes with integers only, and a test reads its source and fails on any floating-point type, literal, or library ([spec.md](spec.md), section 1). With that:
  - Four runs of 24 hours with a hostile planner and aircraft faults were recorded on an arm64 processor and on an amd64 processor, 2.3 to 2.4 million lines each. Each of the eight records replayed on the other type with no difference.
  - The kernel program in the machine of [vm.md](vm.md) reproduces both published vectors, and the same kernel as a native Rust program reproduces three records of four to six hours.
  - Two records are published as vectors, and the tests replay them on every machine ([conformance/README.md](../conformance/README.md)).

  The simulator around the kernel still uses floating point: it flies the aircraft and plans the routes. It gives the kernel integers. So one seed gives two runs that differ a little on two processor types, because the inputs differ. In the first of the four runs above, about 16,000 of 2.4 million lines differed between the two types: positions by up to 3 mm, one conflict time by 2 µs, and the checkpoints after the first of them. A record of either run replays on both types.
- **A kernel by another author.** The replay compares bits, and the specification now defines every bit. The kernel in Rust reproduces the records of the kernel in Go. One author wrote both. A kernel by another author, from the specification alone, has not been tried.
- **The replay in the machine is slower than the Go kernel.** As translated code it takes about 6 times as long: 29 seconds for a record of six hours at 120 flights an hour. In the interpreter alone it takes 7.3 minutes. Both are far faster than the traffic itself ([vm.md](vm.md), section 5).
- **No witness.** The simulator signs with one key. The cell of [replication.md](replication.md) has four replicas, and each signs the checkpoints with its own key. Nothing sends a checkpoint to a witness: `record checkpoints` prints the lines that a witness would keep.
- **The order of writing.** In a replicated kernel the replicas agree on an input before any of them applies it, and a replica that cannot write does not apply. In the simulator one process writes, and a failed write does not stop the kernel. In the cell of [replication.md](replication.md) the replicas agree on each input before any of them applies it. A replica there keeps the record in memory, and does not write it as it goes.
- **A record starts from an empty Store.** A cell that has run for years needs a signed snapshot of the state to start a replay from. The format has none.
- **Keys are files.** There is no key rotation, no revocation, and no link from a key to an organization. An operational system keeps keys in hardware.
- **A record holds positions.** Data terms may forbid its publication. The checkpoints hold no flight data and can be published alone.
- **The record stops at the kernel.** It does not show that a clearance reached an aircraft. The quorum certificate of the design is the first step to that. The prototype makes one for each batch of inputs and checks it ([replication.md](replication.md), section 5). No Gateway uses it, and nothing shows delivery.
- **Time and size.** The checker does not limit the time that a hostile record can take. A record is large: 9 to 13 MB for each hour of a cell with 120 flights an hour, and about a quarter of that after compression.
- **A repeated name in a line is not found.**

## 9. Commands

```
go run ./cmd/record keygen -out cell                   # writes cell.key (private) and cell.pub
go run ./cmd/atcsim -hours 2 -record run.rec -key cell.key
go run ./cmd/record verify -key cell.pub run.rec       # chain, signatures, replay
go run ./cmd/record checkpoints run.rec > held.ckpt    # what a witness keeps
go run ./cmd/record verify -key cell.pub -witness held.ckpt run.rec
go run ./cmd/record verify conformance/kernel-traffic-0.2.rec.gz   # a published vector, not signed
go run ./cmd/record verify -vm conformance/kernel-traffic-0.2.rec.gz   # also with the kernel program, in the machine
go run ./cmd/record verify -vm -interpret conformance/kernel-traffic-0.2.rec.gz   # the same, with every instruction interpreted
```

A file whose name ends in `.gz` is read as gzip.

`record verify` ends with status 0 if the record passes, and 1 if it does not. A private key must not go into version control: `.gitignore` excludes `*.key`.
