# Verifier conformance suite

This directory lets you check a Verifier written in any language against the [kernel specification](../docs/spec.md), sections 2 to 5.

| File | Content |
|---|---|
| `verifier-0.1.json` | 588 test vectors for specification 0.1 |
| `examples/python/verifier.py` | A small Verifier in Python that shows the protocol |

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
{"id": "collision-007", "conflict": true, "t": 512.25}
```

- `id` must repeat the id of the case.
- `conflict` is true or false.
- `t` is the first conflict time in seconds. It is read only when `conflict` is true.

Your program should exit when its input ends. Anything that it writes to standard error is shown and not judged.

## A vector

```json
{
  "id": "basic-005",
  "group": "basic",
  "note": "Parallel tracks 9 nm apart. The envelopes grow until 9 = 5 + 2(1.2 + k t), at t = 288 s.",
  "a": {"legs": [{"t0": 0, "t1": 1500, "p0": [-100, 0], "v": [0.1333, 0]}],
        "vsegs": [{"t0": 0, "t1": 1500, "a0": 35000, "a1": 35000}],
        "anchor": 0, "r0": 1.2, "k": 0.002778, "vu0": 200, "vuRate": 0, "vmin": 0.1333},
  "b": {"...": "the second tube, in the same form"},
  "from": 0,
  "to": 1500,
  "sep": {"sh": 5, "svTrue": 600, "vertSlack": 0},
  "expect": {"class": "conflict", "firstMin": 152.9, "firstMax": 291.7,
             "exactFirst": 288.0, "exactMargin": -6.73}
}
```

Numbers above are shortened. The file holds them in full.

| Field | Meaning | Specification |
|---|---|---|
| `legs` | Straight legs: start and end time, start position, ground velocity | Section 2 |
| `vsegs` | Vertical segments: start and end time, start and end altitude | Section 2 |
| `anchor`, `r0`, `k` | The radius of the tube and its growth | Section 3 |
| `vu0`, `vuRate` | The vertical uncertainty and its growth | Section 3 |
| `vmin` | The lowest ground speed. It sets the early and late allowance | Section 3 |
| `from`, `to` | The interval to check | Section 4 |
| `sep` | The separation standard | Section 4 |

Units are nautical miles, feet, and seconds. Velocities are in nautical miles per second.

## How an answer is judged

| Class | A conforming answer |
|---|---|
| `conflict` | `conflict` is true, and `t` is from `firstMin` to `firstMax`. |
| `clear` | `conflict` is false. |
| `either` | `conflict` is false, or it is true with `t` from `firstMin` to `firstMax`. |

A Verifier may be conservative, within a tolerance of 0.75 nm and 1 s. The class `either` holds the pairs that lie inside that tolerance. `exactFirst` and `exactMargin` are for information: an exact Verifier reports `exactFirst`.

`firstMin` is the first conflict time under the tightened standard. `firstMax` is the first conflict time under the standard relaxed by the guard. Section 9 of the specification explains both.

## The groups

Each group aims at one way in which a Verifier can be wrong.

| Group | Vectors | What it is for |
|---|---|---|
| `basic` | 13 | Cases with an answer that can be worked out by hand |
| `random` | 40 | Unrelated trajectories. All are clear |
| `dense` | 200 | Pairs that start close together, with turns and level changes |
| `collision` | 80 | Two aircraft aimed at one point at one time, or within a few hundredths of a mile. Closed-form roots lose precision here |
| `parallel` | 30 | Equal velocities. A conflict can arise only from the growth of the envelopes |
| `vertical-timing` | 80 | One aircraft leaves a level as another enters it, within seconds |
| `restart` | 45 | A check that starts at, just before, or just after the exact start of a conflict |
| `intruder` | 50 | One tube built from a track, with a fast-growing radius and a vertical uncertainty that may grow |
| `tightened` | 50 | A standard with a wider distance and a vertical slack |

The `collision` and `restart` groups exist because this project's own exact implementation once failed in each of those two ways.

## What a pass means

Your implementation agrees with the specification on 588 pairs. That is evidence, and it is not proof. Please also read the limits in section 9 of the specification.

If your implementation fails a vector and you believe that the vector is wrong, open an issue. The expected answers come from one implementation and one enumeration by the same author. A disagreement with a careful independent implementation is exactly what this suite is for.

## For maintainers

`go run ./cmd/conform -generate` rebuilds the vectors from a seed. Do this only when the specification changes, and give the new file a new version in its name. The tests in `internal/conformance` check the file in the repository: its expectations against an enumeration, both built-in implementations, the Python example, and fourteen deliberately faulty Verifiers that the suite must catch.
