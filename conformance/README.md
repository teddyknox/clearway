# Verifier conformance suite

This directory lets you check a Verifier written in any language against the [kernel specification](../docs/spec.md), sections 2 to 5.

| File | Content |
|---|---|
| `verifier-0.2.json` | 628 test vectors for specification 0.2 |
| `examples/python/verifier.py` | A small Verifier in Python that shows the protocol |
| `kernel-session-0.2.rec` | A kernel vector: a short session with every kind of kernel input |
| `kernel-traffic-0.2.rec.gz` | A kernel vector: half an hour of dense traffic under a hostile planner |

The last two files test a whole kernel and are a different kind of vector. The section "Kernel vectors" below describes them.

## Run it

The runner is a Go program in this repository. It needs Go 1.26.

Run one of this repository's two implementations:

```bash
go run ./cmd/conform -builtin analytic
```

Run a Verifier in another process, in any language:

```bash
go run ./cmd/conform -exec "python3 conformance/examples/python/verifier.py"
```

A run prints the number of cases in each class, how many the Verifier called a conflict, and `PASS` or the failures by group.

## The protocol for a Verifier in another process

The runner starts your program once. Then, for each case:

1. The runner writes the case as **one line of JSON** to your standard input. The line has `id`, `a`, `b`, `from`, `to`, and `sep`. It does not have the expected answer.
2. Your program writes **one line of JSON** to its standard output and flushes it:

```json
{"id": "collision-007", "conflict": true, "t": 641702526}
```

- `id` must repeat the id of the case.
- `conflict` is true or false.
- `t` is the first conflict time in microseconds. It is read only when `conflict` is true. It must then be an integer. The runner stops at a time that has a fraction or an exponent, or that is absent.

Your program should exit when its input ends. Anything that it writes to standard error is shown and not judged.

## A vector

```json
{
  "id": "basic-005",
  "group": "basic",
  "note": "Parallel tracks 9 nm apart. The envelopes grow until 9 = 5 + 2(1.2 + k t). With k = 10 kt that is t = 288 s. The vector has k = 5144444 µm/s, which is a little less, and the first conflict is at 288000025 µs.",
  "a": {"legs": [{"t0": 0, "t1": 1500000000, "p0": [-185200000000, 0], "v": [246933333, 0]}],
        "vsegs": [{"t0": 0, "t1": 1500000000, "a0": 10668000000, "a1": 10668000000}],
        "anchor": 0, "r0": 2222400000, "k": 5144444, "vu0": 60960000, "vuRate": 0, "vmin": 246933333},
  "b": {"...": "the second tube, in the same form"},
  "from": 0,
  "to": 1500000000,
  "sep": {"sh": 9260000000, "svTrue": 182880000, "vertSlack": 0},
  "expect": {"class": "conflict", "firstMin": 153000014, "firstMax": 288000025, "exactFirst": 288000025}
}
```

These are the numbers of the file. In the file, a vector is one line.

| Field | Meaning | Specification |
|---|---|---|
| `legs` | Straight legs: start and end time, start position, ground velocity | Section 2 |
| `vsegs` | Vertical segments: start and end time, start and end altitude | Section 2 |
| `anchor`, `r0`, `k` | The radius of the tube and its growth | Section 3 |
| `vu0`, `vuRate` | The vertical uncertainty and its growth | Section 3 |
| `vmin` | The lowest ground speed. It sets the early and late allowance. Zero means no allowance | Section 3 |
| `from`, `to` | The interval to check | Section 4 |
| `sep` | The separation standard: the horizontal distance `sh`, the vertical distance `svTrue`, and the slack `vertSlack` | Section 4 |

Every number in a vector is an integer.

| Quantity | Unit | Fields |
|---|---|---|
| Time | microsecond (µs) | `t0`, `t1`, `anchor`, `from`, `to`, `vertSlack`, `firstMin`, `firstMax`, `exactFirst`, and the `t` of an answer |
| Length and altitude | micrometre (µm) | `p0`, `a0`, `a1`, `r0`, `vu0`, `sh`, `svTrue` |
| Speed and growth | micrometre per second (µm/s) | `v`, `k`, `vuRate`, `vmin` |

Every tube and every standard in the file is valid: each value is inside the limits of the specification, sections 1 to 4. The runner does not run a file with a vector that is not valid.

One nautical mile is 1,852,000,000 µm. One foot is 304,800 µm. One knot is about 514,444 µm/s. In the example, each track starts 100 nm west of the origin and goes east at 480 kt, `r0` is 1.2 nm, `sh` is 5 nm, and `svTrue` is 600 ft.

## How an answer is judged

The rule is exact. A check covers every whole microsecond of its interval. At a whole microsecond, a position is a whole number of picometres: `1,000,000·p0 + v·(t − t0)`. The rule compares integers, so an expected answer does not depend on rounding. The squares of distances that it compares can need more than 64 bits.

| Class | A conforming answer |
|---|---|
| `conflict` | `conflict` is true, and `t` is from `firstMin` to `firstMax`. |
| `clear` | `conflict` is false. |
| `either` | `conflict` is false, or it is true with `t` from `firstMin` to `firstMax`. |

A Verifier may be conservative, within a tolerance of 1,389,000,000 µm (0.75 nm) and 1,000,000 µs (1 s). Section 5.2 of the specification defines the standard tightened by the tolerance.

The tolerance in time is a slack. Under a slack `w`, a vertical segment counts at a time t if it is in effect at some whole microsecond from `t − w` to `t + w` (specification, section 3). A slack does not move the start and the end of a segment: with a radius that grows, that is a different rule. Most vectors have a `vertSlack` of zero. The tightened standard always has a slack: 1,000,000 µs more than `vertSlack`.

- A vector is of class `conflict` if the pair is in conflict under the standard as given. `firstMax` is the first conflict time under that standard: a Verifier must not be late. `firstMin` is the first conflict time under the tightened standard: a Verifier must not be early by more than the tolerance.
- A vector is of class `clear` if the pair is separated under the tightened standard.
- A vector is of class `either` in all other cases: the pair lies inside the tolerance. `firstMin` is the first conflict time under the tightened standard, and `firstMax` is the end of the interval.

There is no guard around these times. A time that is one microsecond before `firstMin` or one microsecond after `firstMax` does not conform.

`exactFirst` is for information. It is present for class `conflict` and equals `firstMax`: an exact Verifier reports it.

## The groups

Each group aims at one way in which a Verifier can be wrong.

| Group | Vectors | What it is for |
|---|---|---|
| `basic` | 13 | Cases with an answer that can be worked out by hand |
| `random` | 40 | Unrelated trajectories. All are clear |
| `dense` | 200 | Pairs that start close together, with turns and level changes |
| `collision` | 80 | Two aircraft aimed at one point at one time. The second misses the point by nothing, by a micrometre or two, or by up to a few hundredths of a mile. Closed-form roots in floating point lose precision here |
| `parallel` | 30 | Equal velocities. A conflict can arise only from the growth of the envelopes |
| `vertical-timing` | 80 | One aircraft leaves a level as another enters it, within seconds |
| `restart` | 45 | A check that starts at the exact start of a conflict, or from one microsecond to five seconds before or after it |
| `intruder` | 50 | One tube built from a track, with a fast-growing radius and a vertical uncertainty that may grow |
| `tightened` | 50 | A standard with a wider distance and a vertical slack |
| `slack` | 40 | One aircraft enters or leaves the level of another, at a time that the note states to the microsecond. The answer, or its time window, depends on what a slack covers. In 20 of the vectors the radius grows at 0.7 to 0.8 of the speed of the aircraft |

The `collision` and `restart` groups exist because this project's own exact implementation once failed in each of those two ways.

The `slack` group exists because this project's sampled implementation was once earlier than the tolerance allowed. It tests one second at a time and reports the start of a second that it cannot prove. The rule then applied a slack by moving the start and the end of a segment. With a radius that grows, that moves the first conflict by less than the slack, so the time window started less than one second before the first conflict. `slack-001` is the pair that showed it. The rule changed: a slack is now a tolerance in time.

In every vector of the group the two aircraft fly one nautical mile apart with one velocity. So the pair is never horizontally separated, and the vertical part alone decides. Where the class is `conflict`, `firstMin` is exactly 1,000,000 µs before `firstMax`. The group holds four kinds of vector.

| Vectors | Slack | What happens |
|---|---|---|
| `slack-001` to `slack-012` | 0 | b descends to the level of a. The check starts far before the first conflict, exactly 1,000,000 µs before it, or 1,000,001 µs before it. In the last case a conflict at the start of the check does not conform |
| `slack-013` to `slack-020` | 1 s or 10 s | The same under a slack. The first conflict is earlier by exactly the slack. In four vectors the check ends 1,000 µs after the first conflict |
| `slack-021` to `slack-032` | 0 | a descends out of the level that b descends into, and the radius of a grows almost as fast as a flies. The transition of b is first in effect when the transition of a is last in effect, or 1 µs, 2,000,000 µs, or 2,000,001 µs later. The classes are `conflict`, `either`, `either`, and `clear` |
| `slack-033` to `slack-040` | 1 s | The same under a slack: 2,000,000 µs, 2,000,001 µs, 4,000,000 µs, or 4,000,001 µs later |

## What a pass means

Your implementation agrees with the specification on 628 pairs. That is evidence, and it is not proof. Please also read the limits in section 9 of the specification.

If your implementation fails a vector and you believe that the vector is wrong, open an issue. The expected answers come from one implementation and one enumeration by the same author. A disagreement with a careful independent implementation is exactly what this suite is for.

## Kernel vectors

A kernel vector is a record ([docs/record.md](../docs/record.md)): the inputs of a kernel, with every verdict and every state digest that the kernel must give. The kernel of this repository, in Go, wrote both vectors on an arm64 machine. Neither is signed.

| Vector | Chained lines | Content |
|---|---|---|
| `kernel-session-0.2.rec` | 816 | Three flights and every kind of input: amendments that are acknowledged, refused, and lost, a change of authority, an intruder that gets a new contract, and a flight that leaves. 7 commits and 4 rejections, and 16 checkpoints |
| `kernel-traffic-0.2.rec.gz` | 13,742 | 30 minutes at 240 flights an hour, with a planner that proposes at random and aircraft that leave their route or their level. 203 commits and 225 rejections, and 30 checkpoints |

Check one with the Go kernel of this repository:

```bash
go run ./cmd/record verify conformance/kernel-traffic-0.2.rec.gz
```

Check one with the kernel program, in the machine of [docs/vm.md](../docs/vm.md):

```bash
go run ./cmd/record verify -vm conformance/kernel-traffic-0.2.rec.gz
```

**What they test.** That a kernel gives the bits of the specification. The replay compares bits: each verdict, each conflict time to the microsecond, and each state digest must be equal. Every value of a kernel is an integer (specification, section 1), so there is one right answer, on every machine and in every language.

- `go test ./...` replays both vectors with the Go kernel. So a run of the tests on any machine is a check that this machine computes the same bits.
- The tests also give both vectors to the kernel in Rust, as the kernel program in the machine. It reproduces every verdict and every state digest.
- The first kernel of this project computed in floating point. A replay of this traffic on an amd64 processor then failed at the checkpoint for the fourth minute.

**What they do not test.** That the two kernels are right. One author wrote both from one specification, and both can share one misreading. They test agreement.

**For a kernel by another author.** These vectors are the test. [docs/vm.md](../docs/vm.md), section 4, defines a binary form of the same inputs and answers, which a kernel can read without a JSON parser.

## For maintainers

A change to what the kernel does, made on purpose, needs new kernel vectors:

```bash
go test ./internal/record -run TestKernelVectorSession -update
```

```bash
go test ./internal/sim -run TestKernelVectorTraffic -update
```

`go run ./cmd/conform -generate` rebuilds the vectors from a seed. Do this only when the specification changes, and give the new file a new version in its name. The tests in `internal/conformance` check the file in the repository: its expectations against an enumeration, both built-in implementations, the Python example, and sixteen deliberately faulty Verifiers that the suite must catch. Two of the sixteen are the slack and the sampled method as they were before the rule changed.
