# Kernel prototype and simulation results

| | |
|---|---|
| Status | Prototype 0.2, 2026-11-15 |
| Scope | The kernel of [design.md](design.md) section 6 for one en route cell, with a fast-time simulator around it |
| Code | Go 1.26, standard library only, with a viewer in plain JavaScript. The kernel a second time in Rust, with no dependency, built as one program for a small RISC-V machine that is also here. |

The prototype exists to answer two questions.

1. Does the central claim of the design hold when it is built? The claim: two aircraft that conform to verified contracts do not lose separation, whatever the planner proposes.
2. What did the design leave out? A design that has not been built hides its gaps.

## 1. Results in brief

All numbers come from 22 configurations, each run for 8 seeds of 24 simulated hours. Section 6 has the tables.

- **The kernel held.** The 21 configurations with a correct Verifier arrangement cover about 325,000 simulated flight hours. No loss of separation occurred between two aircraft that conformed to verified contracts. The closest such pair came to 7.4 nm against a standard of 5 nm.
- **Planner quality changed efficiency, not kernel safety.** With a deliberately hostile planner, and with the Fallback Resolver and the Safety Net switched off, the Verifier received 218,000 proposals and rejected 96,000. Flights received 4.4 clearances each in place of 0.4, and the mean delay rose from 5 s to more than 630 s. No kernel-class loss occurred.
- **The second Verifier implementation earned its place.** A faulty implementation alone produced 99 losses between conforming aircraft. The same faulty implementation beside a correct one produced none, and raised 37,470 alarms.
- **The second implementation also found two real defects in the first** during this work. One of them approved about a third of near-exact collision courses (section 7).
- **The Safety Net prevented every loss caused by aircraft faults** in these runs: 41 losses with alerts only, none with instructions.
- **The Safety Net also caused a loss.** It predicts from state alone, so it does not know that an aircraft will turn. Under the hostile planner it raised 157 alerts on pairs that the kernel had verified as separated, and one instruction that followed such an alert led to a loss.
- **The growth rate of along-track uncertainty drives workload.** From 3 kt to 15 kt, clearances per flight rose from 0.29 to 0.46, and conflicts still open ten minutes ahead rose from 2 to 56.
- **An outsider can check what the kernel decided.** A run writes a record: every input and verdict, chained by hash, with signed checkpoints. A replay of the record of a kernel with a faulty Verifier named a clearance that the kernel approved and that the specification refuses (section 6.7).
- **The kernel gives the same bits everywhere.** The first replay on another processor type failed: the kernel computed in floating point, and the last digits differed. The kernel now computes with integers only, and every rule of the specification is a comparison of exact integers. The kernel also exists a second time, in Rust, as one program for a small machine that this repository defines. That program reproduces the records of the Go kernel, to the last bit (section 6.8).

These are results of a simplified model (section 9). They test the logic of the design. They are not a safety rate.

## 2. What the prototype contains

| Package | Tier | Content |
|---|---|---|
| `internal/kernel` | A | Contract Store, Clearance Verifier with two implementations, Conformance Monitor, verified windows, re-anchoring, a hook that passes every input and verdict to a recorder, and a state digest. Integers only |
| `internal/safetynet` | A, independent | Tactical Safety Net. It imports nothing from this module. |
| `internal/fx`, `internal/traj` | A | Integer arithmetic with exact 128-bit products, and trajectories in micrometres and microseconds |
| `guest/` | A | The kernel a second time, in Rust: no runtime, no dependency, no floating point, no unsafe code. `guest/rv` makes it a program for the machine |
| `internal/rvm`, `internal/kimage`, `cmd/rvgen` | A | The machine: an interpreter for RISC-V RV32IMAC with three environment calls, and a translator that turns a program into Go code that reaches the same states faster. And the built kernel program, with its hash and its translated code. See [vm.md](vm.md) |
| `internal/ftraj`, `internal/geom`, `internal/conv` | — | The floating-point trajectories and geometry of the planner and the simulator, and the one place where their values become integers for the kernel |
| `internal/planner` | B | Heuristic Trajectory Planner, Fallback Resolver, re-contracting of intruders, and a hostile "chaos" planner |
| `internal/sim` | — | True aircraft states, traffic, clearance delivery, faults, measurement |
| `internal/arch` | — | Tests that enforce the dependency rules of the design |
| `cmd/atcsim` | — | Command-line tool and experiment runner |
| `cmd/atcviz` | — | Viewer: runs a scenario, records it, and replays it in a browser (section 10) |
| `internal/conformance`, `cmd/conform` | — | Test vectors for a Verifier in any language, the rules that judge an answer, and the runner. See [spec.md](spec.md) |
| `internal/record`, `cmd/record` | — | The record of a kernel: the writer, the checker with its replay, and the command. See [record.md](record.md) |

| Modelled | Not modelled |
|---|---|
| Invariants I1 to I5 and assumptions A1 to A3 of the design | Surface, terminal airspace, runways, wake |
| One cell with entry and exit contracts | Several cells, boundary agreements, island mode |
| A deterministic kernel of integer arithmetic, published as one program for one machine, and a record of it: a hash chain, signed checkpoints, and a replay | Replicas, quorum certificates, witnesses |
| Level flight, climbs and descents, speed changes, lateral detours | Holding, weather, wind fields |
| Data link delay, lost uplinks, lost replies, refusals, one repeat | Voice, speech recognition |
| Aircraft that leave their route, leave their level, or have no radio | Surveillance faults, spoofing |
| The Safety Net with alerts and vertical instructions | Airborne collision avoidance |
| Automation authority; the authority rule itself | Supervisors and their decisions |

## 3. How to run

Run the tests. They take about 30 seconds. They need no Rust toolchain: the built kernel program is in the repository.

```bash
go test ./...
```

Run one six-hour simulation with default settings.

```bash
go run ./cmd/atcsim
```

Run every experiment of section 6. This takes about six minutes on 16 cores.

```bash
go run ./cmd/atcsim -exp all -hours 24 -seeds 8
```

Write the record of a run, signed with a new key, and check it. [record.md](record.md) says what each check shows.

```bash
go run ./cmd/record keygen -out cell
```

```bash
go run ./cmd/atcsim -hours 2 -record run.rec -key cell.key
```

```bash
go run ./cmd/record verify -key cell.pub run.rec
```

Replay the record with the kernel program in the machine as well (section 6.8). With `-interpret`, the machine interprets every instruction: the reference, and about 16 times as slow.

```bash
go run ./cmd/record verify -key cell.pub -vm run.rec
```

Useful flags: `-planner heuristic|chaos|none`, `-verifier dual|single|broken-single|broken-dual`, `-fallback=false`, `-safetynet=false`, `-rate` (flights per hour), `-k` (growth of along-track uncertainty in knots), `-blunder`, `-bust`, `-nordo` (fault probability per flight), `-net-lookahead`, `-seed`, `-json`.

A default run simulates six hours of about 700 flights in about two seconds.

## 4. Model

### 4.1 Cell and traffic

The cell is a disc of 150 nm radius. Flights arrive as a Poisson process, cross on a chord, and fly at 430 to 490 kt at a level between FL300 and FL400 chosen by direction of flight. One flight in five has a planned 2,000 ft level change. A flight asks for admission 15 minutes before the boundary. The kernel admits it at the filed level, at a nearby level, or up to two minutes later. A flight that is not admitted waits and asks again.

### 4.2 Parameters

All values are initial design values (†).

| Parameter | Value |
|---|---|
| Horizontal standard | 5 nm |
| Vertical standard between assigned levels | 1,000 ft |
| Altitude-keeping tolerance | 200 ft |
| Verification horizon | 20 min |
| Commit horizon: an amendment must be conflict-free this far ahead | 15 min |
| Lateral tolerance, surveillance tolerance, detection buffer | 0.5, 0.2, 0.5 nm |
| Growth of along-track uncertainty, K | 10 kt |
| Re-anchor period | 60 s |
| Surveillance period | 5 s |
| Time from proposal to divergence point | 150 s |
| Planner acts on a conflict at | 12 min |
| Fallback Resolver acts at | 5 min |
| Safety Net look-ahead | 120 s |

### 4.3 The tube

A tube is the protected envelope of one contract.

- **Horizontal.** At time t the aircraft is inside a disc around its nominal position. The radius is 1.2 nm at the anchor time and grows by K. One disc covers along-track and cross-track tolerance, on straight and on bent paths.
- **Vertical.** A level segment occupies its level. A climb or descent occupies its whole range for its whole duration. Because the aircraft may be early or late, each vertical segment is in effect from before its start to after its end, by the time the aircraft needs to cross the radius.
- **Separation.** Two tubes are separated at time t if the discs are at least 5 nm apart, or if the vertical extents, each widened by 200 ft, are at least 600 ft apart. For two level aircraft this is the 1,000 ft standard.

The Monitor uses a conformance envelope that is smaller than the tube by the detection buffer. An aircraft that leaves the conformance envelope is still inside its tube when the Monitor reports it.

The kernel holds every value as an integer: lengths in micrometres, times in microseconds, speeds in micrometres per second. Five nautical miles and 1,000 feet are exact in these units. The planner and the simulator compute in floating point, in nautical miles, feet, and seconds. `internal/conv` is the one place where their values become integers, rounded to the nearest unit. A leg with a rounded velocity ends up to a few millimetres from the next waypoint, so the kernel accepts a join within 18.5 mm (specification, section 2).

### 4.4 Aircraft

A true aircraft follows its trajectory with an along-track drift of up to 0.8 K, a lateral oscillation of up to 0.15 nm, and a climb rate above the conservative rate that the contract assumes. Three faults can be injected per flight: the aircraft turns 20 to 40 degrees off its route, the aircraft leaves its level for the next one, or the aircraft receives no clearance.

### 4.5 Classes of loss

A loss of separation is two aircraft inside the cell closer than 5 nm and closer than 600 ft. The simulator counts each loss once and classifies it by what the kernel claimed when it began.

| Class | Meaning | Whose failure |
|---|---|---|
| Kernel | Both aircraft were inside their contracts, and the pair was inside its verified window. | The kernel. This class must stay at zero. |
| Latency | The pair was verified. One aircraft had left its contract, and the Monitor had not reported it yet. | The Monitor's detection time. |
| Unresolved | Both aircraft conformed. The kernel had predicted the conflict, and nothing resolved it. | The planner and the layers below it. |
| Intruder | At least one aircraft was a known intruder. | The aircraft, and then the tactical layers. |

## 5. How the kernel is checked

| Test | What it shows |
|---|---|
| `TestCheckersAgainstOracle` | Both Verifier implementations against the rule itself, stated a second time in arbitrary-precision arithmetic, on 4,000 random pairs. The exact one reports the first microsecond of each conflict. The sampled one never approves a conflict, is never late, and is never outside the tolerance. |
| `TestCheckersAtTheLimits` | 20,000 pairs with values at the limits of what is valid. Nothing overflows, and the exact answer holds to the definition. |
| `TestSlackIsATimeTolerance`, `TestSampledStaysInsideTheTolerance` | A tolerance in time is what it says. Both implementations are held to a trial of every microsecond on small tubes. The second test is the pair that showed the defect of section 7, item 3. |
| `TestCollisionCourses` | Both implementations report 4,000 exact and near-exact collision courses, for every growth rate. |
| `TestSerializedCommit` | I5: two amendments that are each safe alone and unsafe together. The second is rejected. |
| `TestPendingProtection` | I4: while an amendment is pending, a third flight may conflict with neither the old tube nor the new one. |
| `TestProposalRules` | I1 and I3, and the rules for lead time, continuity, and vertical transitions. |
| `TestUnknownIsSettledByTheMonitor` | After a lost acknowledgement, the track decides which contract is active. |
| `TestMonitorMarksIntruder`, `TestRecontractIntruder`, `TestReanchor` | The Conformance Monitor, the return of an intruder to a contract, and re-anchoring. |
| `TestRandomProposalsKeepInvariants` | One hour of random admissions and random amendments. After every step, the brute-force check confirms I2 for every pair inside its verified window. A second kernel that applies the recorded inputs returns the same verdicts and reaches the same state digest. |
| `TestKernelHoldsUnderEveryPlanner` | No kernel-class loss with the heuristic planner, no planner, the chaos planner, the chaos planner alone, and dense traffic. |
| `TestSecondImplementationCatchesFaultyVerifier` | A faulty implementation alone loses separation. Beside a correct one it does not, and alarms are raised. |
| `TestAircraftFaults`, `TestLostCommunications` | Faults and lost messages never become kernel-class losses. |
| `TestDeterminismAndReplay` | One seed gives one result, down to every byte of the record. The record passes the chain, the signature, and the replay checks. |
| `TestReplayCatchesFaultyVerifier` | The record of a kernel with a faulty Verifier is consistent and correctly signed. The replay names a proposal that the kernel committed and that the public implementations reject for a conflict. |
| `TestStateDigestCoversTheStore` | The state digest changes with the clock, a track, an authority, an intruder, a re-anchor, and each flight that comes or goes. It does not change when a proposal is rejected. |
| `internal/record` | Twenty-three kinds of change to a record, each found by the check that is there to find it. A history that the signer rewrites: found by the replay, or by a witness alone. Signers and quorum. Every input and verdict written and read back with the same bits. Records with extreme values, and a fuzz target: the checker must report on any bytes. |
| `internal/conformance` | Both implementations pass the 628 conformance vectors. A Verifier in Python passes them through the line protocol. Sixteen Verifiers with one deliberate defect each all fail. |
| `TestKernelVectorSession`, `TestKernelVectorTraffic` | The kernel on this machine replays two published records that an arm64 machine wrote, to the last bit of every conflict time and state digest. The same tests pass in an amd64 build. |
| `internal/kimage` | The kernel program has the hash that the code states. In the machine it reproduces both published records, in the interpreter and as translated code, with the same count of instructions. Its translated code is current, and the machine with it has the state of the interpreter at random stops. |
| `TestKernelsAgreeOnUnusualInputs` | The two kernels on inputs that no simulated traffic has: aircraft that do not move, a radius that grows faster than the aircraft flies, legs of one microsecond, values at the limits, and proposals that the kernel must refuse. 132 random sessions in the machine, 12 of them in the interpreter alone, and 600 with the native program when it is built. |
| `TestRustKernelNative` | The kernel in Rust, as a native program, reproduces a scripted session, the traffic record, and any record that the caller names. It runs when that program is built. |
| `internal/rvm` | The interpreter: each instruction, each fault, the loader, and that a run depends on the program and its input alone ([vm.md](vm.md), section 5). |
| `TestTranslatedOps`, `TestTranslatedGuest` | Translated code against the interpreter: every translated kind of instruction from 141,500 random states, and a program of 19.8 million instructions stopped at random counts. The state must be the same: registers, memory, the count of instructions, and the error. |
| `internal/arch` | The kernel does not import the planner, the simulator, the Safety Net, random numbers, or the clock. On the path of the kernel there is no floating-point type, no floating-point literal, and no use of package `math`, in Go and in Rust. The Rust kernel has no dependency and no unsafe code. The machine imports nothing from the module. The record imports the kernel and nothing above it, and no random numbers or clock. The Safety Net imports nothing from the module. The two implementations do not call each other's geometry. |

## 6. Results

Each row is 8 seeds of 24 simulated hours at 120 flights per hour unless the row says otherwise. Communication faults are on in every run: 1% lost uplinks, 1% lost replies, 2% refusals.

### 6.1 Planner quality

| Planner | Flight hours | Kernel-class losses | Other losses | Clearances per flight | Mean delay (s) | Proposals rejected | Conflicts open at 10 / 5 / 2 min | Net instructions |
|---|---|---|---|---|---|---|---|---|
| Heuristic | 13,897 | 0 | 0 | 0.39 | 5 | 44,269 of 75,736 | 24 / 4 / 0 | 0 |
| None: Fallback Resolver only | 13,876 | 0 | 0 | 0.41 | 1 | 38,758 of 70,572 | 7,242 / 5,198 / 8 | 14 |
| Chaos | 17,820 | 0 | 1 intruder | 4.42 | 645 | 100,118 of 222,946 | 61 / 4 / 0 | 165 |
| Chaos, no Fallback Resolver | 17,796 | 0 | 0 | 4.33 | 641 | 96,618 of 217,622 | 51 / 2 / 2 | 159 |
| Chaos, no Fallback Resolver, Safety Net alerts only | 17,760 | 0 | 1 unresolved | 4.37 | 634 | 96,290 of 217,971 | 57 / 2 / 1 | 0 |

The chaos planner proposes random levels, random detours, routes aimed at other aircraft, contracts that end inside the cell, and tails that do not connect. In one 24-hour run the Verifier rejected its proposals for a conflict (5,975), a malformed trajectory (2,496), an open contract (2,393), too little lead time (856), a divergence inside a transition (493), and a pending amendment (457).

Findings:

- No planner produced a kernel-class loss.
- The last row has nothing but the kernel between the chaos planner and the traffic. Its one loss is of the unresolved class: the kernel had predicted the conflict, and no layer was left to act on it. This is a failure of liveness, as the design expects, and the reason for the escalation ladder.
- The Fallback Resolver alone handled this traffic with less delay than the heuristic planner. It prefers level changes, which cost no time. It acts late: 5,198 conflicts were still open at five minutes, and eight were still open at two.
- The one intruder-class loss under the chaos planner was caused by a Safety Net instruction (section 6.4).

### 6.2 A second Verifier implementation

In these runs the Safety Net alerts only, so that a Verifier fault shows as a loss. The faulty implementation reads a climb or descent as level flight at the starting altitude.

| Verifier arrangement | Flight hours | Kernel-class losses | Closest verified pair (nm) | Alarms |
|---|---|---|---|---|
| Two correct implementations | 13,897 | 0 | 8.3 | 0 |
| One correct implementation | 13,897 | 0 | 8.2 | — |
| One faulty implementation | 13,890 | 99 | 0.1 | — |
| One faulty, one correct | 13,900 | 0 | 8.5 | 37,470 |

A commit needs both implementations to approve. So a fault that makes one implementation too permissive cannot reach an aircraft. It shows as alarms.

Two correct implementations raised no alarm in any configuration, in about 2.5 million proposals. Reaching that took four corrections (section 7).

### 6.3 Aircraft faults

Fault rates per flight: 2% leave the route, 2% leave the level, 1% receive no clearance.

| Run | Flight hours | Kernel-class losses | Intruder-class losses | Net alerts: verified / unresolved / intruder | Net instructions |
|---|---|---|---|---|---|
| No aircraft faults | 13,897 | 0 | 0 | 0 / 0 / 0 | 0 |
| Faults | 13,902 | 0 | 0 | 6 / 0 / 46 | 54 |
| Faults, selected altitude not checked | 13,902 | 0 | 0 | 22 / 0 / 37 | 61 |
| Faults, Safety Net alerts only | 13,902 | 0 | 41 | 6 / 0 / 47 | 0 |

In one 24-hour run with faults, the Monitor reported 58 route deviations and 46 wrong selected altitudes, the planner brought 107 intruders back under contract, and none of those re-contracts failed.

Findings:

- An aircraft that leaves its contract is outside the kernel's guarantee. In these runs the Safety Net's instructions prevented all 41 losses that occurred without them.
- The selected-altitude check catches a level bust before the aircraft moves. Without it, the Safety Net raised 22 alerts on pairs that the kernel still held as verified. With it, 6.
- No loss of the latency class occurred. The detection buffer of 0.5 nm was enough for a 5-second surveillance period in this model.

### 6.4 The Safety Net and aircraft that turn

The Safety Net extrapolates each track in a straight line. A contract with a turn in the next two minutes can look like a conflict to it. The kernel knows better, but the net is independent of the kernel by design.

| Run | Net alerts on verified pairs | Net instructions | Losses caused by a net instruction |
|---|---|---|---|
| Chaos planner, look-ahead 120 s | 157 | 165 | 1 |
| Chaos planner, 90 s | 87 | 90 | 0 |
| Chaos planner, 60 s | 35 | 36 | 0 |
| Aircraft faults, 120 s | 6 | 54 | 0 |
| Aircraft faults, 90 s | 1 | 43 | 0 |
| Aircraft faults, 60 s | 1 | 44 | 0 |

Findings:

- After the 161 alerts on verified pairs in the last row of section 6.1, where the net could not instruct, no pair came closer than 9.3 nm. The alerts were false.
- With instructions on, one false alert sent an aircraft out of its contract and into a loss. The closest approach after a false alert was 3.3 nm.
- A shorter look-ahead removed most false alerts and, in this model, still prevented every loss from aircraft faults.
- With the heuristic planner the net raised no false alert at 120 flights per hour, one at 180, and two at 240. The chaos planner makes the problem visible because it issues many detours.

This is a design finding, not a tuning result. Section 8 states the change.

### 6.5 Growth of along-track uncertainty

K is how fast the kernel must assume an aircraft can drift from its contract between re-anchors.

| K | Clearances per flight | Proposals rejected | Conflicts open at 10 / 5 / 2 min |
|---|---|---|---|
| 3 kt | 0.29 | 24,502 of 53,652 | 2 / 0 / 0 |
| 6 kt | 0.33 | 32,230 of 62,385 | 12 / 2 / 0 |
| 10 kt | 0.39 | 44,269 of 75,736 | 24 / 4 / 0 |
| 15 kt | 0.46 | 63,367 of 96,294 | 56 / 13 / 2 |

No run had a kernel-class loss. A larger K costs clearances and planning effort, and it leaves less time: at 15 kt two conflicts were still open two minutes ahead. This supports principle P1 of the design. How well aircraft hold an assigned trajectory sets the capacity of the system.

### 6.6 Traffic density

| Flights per hour | Flight hours | Clearances per flight | Proposals rejected | Conflicts open at 10 / 5 / 2 min | Net instructions | Losses |
|---|---|---|---|---|---|---|
| 60 | 7,016 | 0.21 | 9,937 of 23,695 | 1 / 0 / 0 | 0 | 0 |
| 120 | 13,897 | 0.39 | 44,269 of 75,736 | 24 / 4 / 0 | 0 | 0 |
| 180 | 20,806 | 0.56 | 120,286 of 173,017 | 165 / 29 / 3 | 6 | 0 |
| 240 | 27,820 | 0.71 | 291,273 of 368,946 | 560 / 130 / 42 | 60 | 0 |

At 240 flights per hour the cell held up to 185 aircraft at once. No loss occurred. The heuristic planner is the limit: 42 conflicts were still open at two minutes, and the Safety Net issued 60 instructions. The kernel admitted every flight on its first request, at most 130 s late or at a nearby level, so back-pressure did not act. A real Flow Manager must act before the planner saturates.

### 6.7 The record

`atcsim -record` writes the record of the kernel: every input and every verdict in order, chained by hash, with a signed checkpoint for each minute. `record verify` checks the chain and the signatures, and replays the kernel. [record.md](record.md) defines the format and says what each check shows. These runs are single runs with seed 1 at 120 flights per hour.

| Run | Chained lines | Size | Check with replay |
|---|---|---|---|
| 2 h, hostile planner, aircraft faults | 140,602 | 18.6 MB, 4.3 MB compressed | 1.3 s |
| 4 h, hostile planner, aircraft faults, flights with no radio | 341,096 | 45.6 MB | 3.1 s |
| 4 h, faulty Verifier alone, Safety Net off | 288,803 | 38.2 MB | 0.7 s |

- **Changes are found.** In the two-hour record, one track was given another altitude. The replay differed 189 lines later, and the next checkpoint did not match the chain. With the last 30 minutes cut off, the record passed alone, and failed against the checkpoints that a witness would hold.
- **The replay found an unsafe approval.** The record of the faulty-Verifier run is consistent and correctly signed. The replay reported three things: the first state digest that differs, at line 7,271; the first proposal that both kernels reject for different conflicts, at line 17,417; and line 41,846, where the record says that an amendment for flight 64 was committed, and the public implementations reject it for a conflict with flight 62. The replay stops there, because the two kernels then hold different contracts. The run itself had one kernel-class loss, later and between two other flights.
- **A record has one replay on every machine.** This failed at first. With the first kernel, which computed in floating point, the four-hour run was recorded on an arm64 processor and on an amd64 processor. Each replay on the other type failed at the checkpoint for the seventh minute. The kernel now computes with integers only (section 7, item 5). With that, four runs of 24 hours were recorded on each type, 2.3 to 2.4 million lines each, and each of the eight records replayed on the other type with no difference.
- **Two minutes of fuzzing**, 14 million inputs, did not stop the checker. A test with extreme values did find a defect in the first kernel (section 7, item 4).

### 6.8 The kernel twice, and the machine

The kernel exists twice: in Go, and in Rust. The Rust kernel is built as one program for a small machine, and [vm.md](vm.md) defines both. Each line of the table gives the inputs of records that the Go kernel wrote to the Rust kernel, and compares every answer: each verdict, with its conflict time to the microsecond, and the state digest at each checkpoint.

| The Rust kernel as | Records | Verdicts | State digests | Result |
|---|---|---|---|---|
| A native program | Three runs of 4 to 6 h: a hostile planner with aircraft faults, dense traffic with faults, and the exact Verifier alone | 21,788 | 960 | The same bits |
| The kernel program, in the machine | The session vector | 11 | 16 | The same bits, in 8.3 million instructions |
| The kernel program, in the machine | The traffic vector: 30 minutes from an empty cell, 240 flights an hour | 428 | 30 | The same bits, in 1.59 billion instructions |
| A native program | 600 random sessions of unusual inputs: very slow aircraft, values at the limits, proposals that must be refused | 24,968 | 8,795 | The same bits |
| The kernel program, in the machine | 132 such sessions | 5,559 | 1,968 | The same bits, in 7.2 billion instructions |
| The kernel program, in the machine | 2 h, a hostile planner with aircraft faults | 2,395 | 120 | The same bits, in 32 billion instructions |
| The kernel program, in the machine | 6 h, a hostile planner with aircraft faults and flights with no radio | 7,400 | 360 | The same bits, in 127 billion instructions |
| The kernel program, in the machine | 6 h, dense traffic at 240 flights an hour, with aircraft faults | 10,066 | 360 | The same bits, in 283 billion instructions |

- **The two kernels agree.** No verdict and no state digest differed in any record that was tried.
- **The machine was slow, and the causes were measured.** The first kernel program needed 34 minutes in the machine for the six-hour record, against 5 seconds for the Go kernel. Three things cost, and two were changed ([vm.md](vm.md), section 5).
  - The Rust kernel computed two values of a tube again at every check of a pair: 65% of its instructions. It now computes them once. The six-hour record went from 523 to 127 billion instructions, with the same answers.
  - The machine interpreted each instruction, at about 14 processor cycles for one. It now also runs the program as *translated code*: Go source that a translator writes from the program, held to the interpreter by tests. Translated code needs about 1 cycle for one instruction.
  - The instruction set has registers of 32 bits, so one exact product of the kernel takes many instructions. This remains.
- **The speeds now.** The six-hour record takes 29 seconds as translated code and 7.3 minutes in the interpreter, with the same count of instructions and the same answers. Six hours of dense traffic take 64 seconds as translated code. That is 340 to 750 times as fast as the traffic, and about 6 times as slow as the Go kernel.
- **What this does not show.** One author wrote both kernels from one specification, so both can share one misreading. The machine is tested and not proved. The program's bytes depend on the version of the compiler, and no second party has built them yet. [vm.md](vm.md), section 6, lists these limits.

## 7. Defects that the prototype found in itself

Five defects appeared during this work. Each one is a small case study for the design. Items 1, 2, 4, and 5 are defects of the first kernel, which computed in floating point.

**1. The analytic implementation reported the end of a conflict as its start.**
When a check started exactly at the moment a conflict began, a root that should have been zero came out negative by rounding error. The code then took the other root, the time at which the conflict ends. The kernel recorded the pair as verified during the conflict, and re-anchoring stopped. A run with the analytic implementation alone showed one loss between conforming aircraft at 4.99 nm. With both implementations the defect was invisible, because the kernel takes the earlier of the two conflict times.

**2. The analytic implementation approved near-exact collision courses.**
For two aircraft aimed at one point at one time, the equation for the closest approach has a discriminant that cancels to zero. Rounding made it negative, the code found no minimum, and it approved the pair. The closed form missed 1,316 of 4,000 such courses in a direct test. The first random test did not contain this geometry. After the rule in item 3 was corrected, two hard disagreements remained in the full set of runs: the sampled implementation had rejected a pair and the alarm had fired. The one that was examined was this defect, and both disappeared with the fix.

Both defects were fixed by one change: the closed-form roots were replaced by a search on a convex function, which has no cancellation. `TestCollisionCourses` and a re-check test now cover both. In the integer kernel the same search runs on exact integers, and the answer is the first microsecond of the conflict.

**3. The rule for "the two implementations disagree" was wrong three times.**
The sampled implementation works in steps of one second. The analytic one is exact. Two altitude changes 0.4 s apart overlap for one and not for the other. The first rule compared distance margins only and raised false alarms. The rule is now: run the approving implementation again with the standard tightened by 0.75 nm and by one second. If it then rejects, the pair is inside the tolerance band and the disagreement is marginal. If it still approves, one implementation is wrong.

The third error was in "tightened by one second". The first rule moved the start and the end of each vertical segment by one second. Where the radius of a tube grows, that moves a conflict by less than one second: about 25 ms less for a contract. The sampled implementation tests one second at a time and reports the start of a second that it cannot prove. So it could report a time that was earlier than the tolerance allowed, and it could reject a pair that the tightened standard clears. The conformance suite of version 0.1 allowed every time a guard band of 0.1 s, to keep rounding from deciding a test, and the guard band hid this. With integers the guard band was removed. The port of the suite then built a pair for which the sampled implementation was 29 ms outside the tolerance. A tolerance in time now means what it says: a segment counts if it is in effect within one second of the time of the test. The sampled implementation was also changed. It had used its largest allowance for a whole interval, and it now tests the rule itself at the two ends of the interval.

**4. A contract with almost no ground speed stopped the kernel.**
A re-anchor divides by the ground speed of the current leg. For a speed too small to divide by, the shift was not a number, and the kernel built a contract whose times were not numbers. The sampled implementation then split an interval with no length, without end, until the stack was full, and the process stopped. No simulated flight is that slow. The case came from a test that replays a record with extreme values: a record is untrusted data, and the checker gives it to a kernel. A re-anchor now keeps the old contract when the shift does not give a valid trajectory. In the integer kernel no value can be "not a number". The check stays in another form: the shifted trajectory must be valid and must cover the present time.

**5. The kernel computed different bits on different processors.**
The design needs replicas that agree on every bit. The kernel did not: an arm64 processor and an amd64 processor disagreed in the last digit of a conflict time, and so in the end of a verified window. No test on one machine can see this. The first replay of a record on the other processor type showed it within minutes. The cause was not in the kernel's logic. The compiler joins a product and a sum into one fused operation on one processor type, and the result is rounded once where two operations round twice.

The first fix was a rule: write every product so that it is rounded alone. It made the two types agree, and it was the wrong fix. Agreement then rested on every compiler and every processor that a replica or an outside party would ever use, and on a test that reads source code for products. The kernel was then changed in two ways.

- *Integers only.* Every value is a whole number of micrometres, microseconds, or micrometres per second, and a position at a whole microsecond is an exact number of picometres. Products are exact in 128 bits. Each rule of the specification is a comparison of integers, with one answer. The exact Verifier now takes no square root and has no tolerance of its own.
- *One program, one machine.* The kernel was written again in Rust and built as one program for a RISC-V instruction set with no floating point. A small machine in this repository runs it. A party that runs the same bytes gets the same answers on any computer (section 6.8).

Lessons for the design:

- A second implementation of a different kind finds defects that tests of one implementation do not. Both real defects were in closed-form geometry, the part that looks most trustworthy.
- A second implementation can also hide a defect in the first. Each implementation must also be run alone in test.
- The most dangerous geometry, the exact collision course, is the one where floating-point arithmetic is weakest. It needs its own tests, and it is one more reason to compute with integers.
- "Disagreement" between a conservative and an exact implementation needs a stated tolerance in distance and in time. Without one, the alarm is noise.
- One input must not be able to stop the kernel (design, section 9.4). Random traffic does not test that. Inputs that are built to be extreme do.
- "Deterministic" must be shown on two different machines. A program that uses no random number and no clock can still depend on the processor.
- A rule that makes two machines agree is weaker than a form of the kernel that cannot disagree. The smallest change was the wrong one here.
- A guard band hides defects in what it guards. Exact arithmetic let the suite drop its guard band, and a defect in the tolerance rule showed at once.

## 8. What the prototype changes in the design

The design document is updated with these points.

| Topic | Change |
|---|---|
| Re-anchoring | The design did not say how the envelope of a contract stays small. Without a rule it grows without limit. The kernel now re-anchors each contract on the track once per minute. A re-anchor moves the times of the contract, not its path. It is accepted only if no verified window gets shorter. |
| Two envelopes | The Monitor needs a conformance envelope that is smaller than the protected tube. The difference is the distance an aircraft can stray during the detection time. |
| Vertical timing | An aircraft that is ahead of its contract starts its climb early. Each vertical segment must be in effect earlier and later than its nominal times, by the along-track uncertainty divided by the ground speed. |
| Amendment envelope | A new contract must inherit the envelope of the old one. The aircraft may be anywhere inside the old envelope when the amendment is committed. |
| Commit horizon | The horizon inside which an amendment must be conflict-free is shorter than the horizon over which windows are extended. Otherwise envelopes that are wide at 20 minutes reject most amendments. |
| Divergence point | An amendment takes effect at a stated point on the current path, far enough ahead to deliver the clearance. It may not lie inside a vertical transition. |
| Disagreement rule | Stated with a tolerance band (section 7, item 3). |
| Intruder re-contract | An intruder gets a new contract from its extrapolated state. It counts as a contract flight again only when the Monitor sees it inside the new envelope. |
| Safety Net and intent | A net that predicts from state alone gives false instructions near turns, and a false instruction can cause a loss. The net needs intent from the aircraft itself, or the Verifier must reject contracts that the net would misread, or the net must instruct only at a shorter look-ahead than it alerts. |
| Each implementation alone | The evidence ladder must run each Verifier implementation alone as well as together. |
| Arithmetic and form of the kernel | The design did not say how replicas come to agree on every bit. The kernel computes with integers only, and it is released as one program for one instruction set, run in a small machine (design, section 9.5). |
| Tolerance in time | A tolerance of one second must be a second: a vertical segment counts if it is in effect within one second of the time of the test (section 7, item 3). |

## 9. Limits

- **One simplified cell.** No terminal airspace, runways, weather, wind field, or neighboring cell.
- **Tests, not proofs.** The invariants are checked by tests and by a second statement of the rule in arbitrary-precision arithmetic. Nothing is formally verified. That second statement and the kernel share one definition of the tube, so the tests cannot find an error in that definition.
- **The aircraft model is kind.** Conforming aircraft stay inside the assumed bounds by construction. The runs say nothing about whether real aircraft do. That is assumption A2 of the design, and only recorded data can test it.
- **The fault model is small.** Three aircraft faults and three message faults. No surveillance fault, no wrong aircraft answering, no spoofing.
- **No rare-event method.** About 325,000 flight hours with no kernel-class loss is evidence about logic. It is far too little to support a collision-risk figure.
- **The planner is simple.** The density results describe this heuristic planner, not the limit of the design.
- **Speed changes are not modelled exactly.** An aircraft that is early by some seconds is early by a different distance on a faster leg. The model covers this with margin and does not treat it exactly.
- **Parameters are untuned.** Every value in section 4.2 is an initial value.
- **The record has one signer and no witness.** [record.md](record.md), section 8, lists these and its other limits.
- **The simulator computes in floating point.** The kernel gives the same bits everywhere. The simulator around it does not: on two processor types, one seed gives positions that differ by up to a few millimetres, and so two records that differ a little ([record.md](record.md), section 8). The tables of section 6 come from an arm64 machine. An amd64 build of the experiment command printed the same tables, every number of sections 6.1 to 6.6. Nothing guarantees that for another run.
- **Two kernels, one author.** The Rust kernel is a second writing of the Go kernel by the same author. It tests the specification for completeness less than a kernel by another author would.
- **The kernel program is not built by a second party, and the machine is not proved.** [vm.md](vm.md), section 6.

[gap-analysis.md](gap-analysis.md) sets these limits beside what a system that carries traffic would need, and says what closes each gap.

## 10. Viewer

`cmd/atcviz` replays any scenario in a browser. It runs the simulation on request, records it, and serves a page that draws the recording.

```bash
go run ./cmd/atcviz
```

Then open <http://127.0.0.1:8787>.

### 10.1 What it shows

| Part | Content |
|---|---|
| Map | Every aircraft at its true position, drawn by what the kernel believes about it. Shape and colour both carry the state, so colour is never the only signal. |
| Proposals | Each proposal at the moment the Verifier decides. A committed one flashes blue and becomes a dashed pending route. A rejected one flashes as a dashed line with a cross and the reason. |
| Predicted conflicts | A line from each of the two flights to the point where they would meet, with the time left. The line gets heavier as the time runs out. |
| Intruders | An orange diamond with the cone that the kernel protects around it. |
| Safety Net | An aqua ring on both aircraft of an alert, and the instruction beside the aircraft that receives one. |
| Selected flight | Its route ahead, the envelope that the kernel protects along it for the next 20 minutes, and a flight strip with its state as the kernel sees it and as it truly is. |
| Side view | Flight level against west-to-east position, aligned with the map. |
| Timeline | Proposals committed and rejected per interval, and marks for faults, intruders, alerts, instructions, and losses. Click or drag to move. |
| Right panel | Counts at the current time: the Verifier's verdicts by reason, Safety Net alerts by class, and losses by class. A loss of the kernel class is flagged. |

Three hues carry meaning: blue for a clearance in flight, orange for anything outside the kernel's guarantee, aqua for the Safety Net. The set passes a colour-vision check on the light and on the dark map.

Controls: space plays and pauses, the arrow keys step 10 seconds (60 with shift), the wheel zooms, a drag pans, a click selects a flight. A link can name a moment, for example `?scenario=chaos&t=2040&sel=12&paused=1&theme=dark`.

### 10.2 Scenarios

| Scenario | What it shows |
|---|---|
| Two aircraft, head-on | One predicted conflict from first sight to resolution. Speed changes are rejected; a level change is committed. |
| Two crossing streams | Eight aircraft that would meet at one point. Each amendment must also clear the amendments already committed. |
| One aircraft turns into a stream | The Monitor reports the deviation, the kernel draws a cone, the Safety Net instructs, and the intruder gets a new contract. |
| One aircraft leaves its level | A wrong selected altitude under traffic 1,000 ft above. The Monitor and the Safety Net act within one surveillance period. |
| Normal operations | 120 flights per hour with every layer on. |
| Dense traffic | 240 flights per hour. |
| A hostile planner | The chaos planner with the lower layers on, with one false Safety Net instruction near minute 34. |
| A hostile planner, kernel alone | The same run with no Fallback Resolver and a Safety Net that only alerts. |
| Aircraft that disobey | 8% leave the route, 8% leave the level, 3% have no radio. Two Safety Net instructions keep intruders clear. |
| Aircraft that disobey, net alerts only | The same run without instructions: two losses of the intruder class. |
| Lost messages | Clearances with no answer, and the Monitor settling which route is flown. |
| A faulty Verifier, alone | Losses of the kernel class: the one scenario that has them. |
| The same faulty Verifier, with a correct one | Same traffic and seed: no loss, and alarms. |

The seeds of the hostile-planner, disobeying-aircraft, and faulty-Verifier scenarios are chosen so that the event happens inside the default duration. `TestScenariosShowTheirPoint` holds every description in the list to what the run does. `TestTraceAgreesWithTheRun` checks that recording a run does not change it.

### 10.3 Limits of the viewer

- It shows true positions every five seconds and moves aircraft in straight lines between them.
- The envelope around the selected flight is drawn from its present speed along its path. It is a close picture of the kernel's tube, not the tube itself.
- A recording is held in memory. Runs longer than about four hours at high density become slow to load.
