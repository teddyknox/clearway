# Clearway kernel specification

| | |
|---|---|
| Version | 0.2, draft, 2026-11-15 |
| Purpose | To let a person who has not read this project's code build a Verifier or a kernel, and check it |
| Tested by | [conformance/](../conformance/README.md) for sections 2 to 5. For sections 6 and 7: the Go tests in `internal/kernel`, two recorded kernel vectors, and a second kernel in another language that must give the same bits |
| Reads with | [design.md](design.md) for why the kernel is built this way. [record.md](record.md) for the record of what a kernel decided. [vm.md](vm.md) for the machine that runs the kernel program |

**Words.** "Must" states a requirement. "May" states a permission. A dagger (†) marks an initial value that simulation must confirm.

**What changed from 0.1.**

- *Integers.* Version 0.1 computed in floating point. Two processor types then disagreed in the last digit of a result, and a record of one could not be replayed on the other. Version 0.2 computes in integers only. Every comparison that decides separation is a comparison of exact integers, so the rule has one answer on every machine.
- *No guard band.* The guard band of 0.1 kept the last digits of an implementation's arithmetic from deciding a test. There are no such digits now, and the guard band is gone.
- *The slack is a tolerance in time.* In 0.1 a slack moved the start and the end of each vertical segment. Where the radius of a tube grows, that gives less tolerance than the slack states, and a method that works in steps of one second could be up to some tens of milliseconds outside it. The guard band hid this. In 0.2 a segment counts under a slack `w` if it is in effect within `w` of the time of the test (section 3).

**What is settled.** Sections 2 to 5 define the Verifier, and the conformance suite tests them. Sections 6 and 7 define the kernel around the Verifier. Two kernels are built from them, in two languages, and they agree bit for bit on every record that was tried (section 9). Section 10 lists what is open.

## 1. Conventions

- **Integers.** Every value is a signed 64-bit integer. There is no floating point anywhere in a kernel or in a Verifier inside a kernel.
- **Units.** Length in micrometres (µm): horizontal position, radius, altitude. Time in microseconds (µs). Speed in micrometres per second (µm/s): ground velocity, and the growth of a radius or of an uncertainty.
- **Picometres.** One micrometre per second for one microsecond is one picometre (pm). So a position at a whole microsecond is an exact integer in picometres, and so is a radius. This document writes `M` for 1,000,000: the picometres in a micrometre, and the microseconds in a second.
- **Exact arithmetic.** A product of two values, and a sum of such products, is exact. It needs at most 128 bits. Where this document divides, it says how the quotient is rounded: `floor` is toward minus infinity, `ceil` is toward plus infinity.
- **Plane.** Positions are points in a flat plane with coordinates (x, y). The prototype has no map projection.
- **Time.** One clock per cell. Every time is a whole number of microseconds on that clock.
- **Aviation units.** 1 nm = 1,852,000,000 µm. 1 ft = 304,800 µm. Both are exact, so every separation standard and every flight level is an exact value.

**Limits.** A value outside these limits is not valid. Inside them, no operation of this document overflows.

| Value | Limit | About |
|---|---|---|
| A time of a trajectory | \|t\| ≤ 2^50 µs | 35 years |
| The clock of a kernel, and the time of a track | \|t\| ≤ 2^49 µs | |
| A coordinate or an altitude of a trajectory | \|x\| ≤ 2^41 µm | 2,200 km |
| A coordinate or an altitude of a track | \|x\| ≤ 2^40 µm | |
| A component of a velocity, a growth rate, a vertical rate | \|v\| ≤ 2^30 µm/s | 2,087 kt |
| A radius, an uncertainty, a separation standard | 0 ≤ r ≤ 2^40 µm | |
| A required distance that is compared exactly | ≤ 2^62 pm | section 4 |

## 2. Trajectory

A *trajectory* is a list of legs and a list of vertical segments.

**Leg.** A leg has a start time `t0`, an end time `t1`, a start position `p0` (µm), and a ground velocity `v` (µm/s), with `t1 > t0`.

- At a time t with `t0 ≤ t ≤ t1`, the position on the leg is `S(t) = M·p0 + v·(t − t0)`, in picometres. It is exact.

**Legs in a trajectory.**

- There are from 1 to 256 legs.
- Each leg starts at the time at which the leg before it ends.
- Each leg starts within the *join tolerance* of the end of the leg before it, on each axis: `|M·p0 − S_before(t1)| ≤ M·18,520` on x and on y. The tolerance is 18,520 µm†, 10⁻⁵ nm. A velocity in whole micrometres per second cannot end a leg on an exact point, so an exact join is not possible.
- At the time where one leg ends and the next starts, the next leg gives the position.
- A turn is a change of velocity at a point. The model has no turn radius.

**Vertical segment.** A vertical segment has a start time `t0`, an end time `t1`, and two altitudes `a0` and `a1` (µm), with `t1 > t0`.

- If `a0 = a1`, the segment is *level*. Otherwise it is a *transition*.
- The *band* of the segment is the closed range from `min(a0, a1)` to `max(a0, a1)`. A transition holds its whole band for its whole duration. The model does not assume a rate of climb.

**Vertical segments in a trajectory.**

- There are from 1 to 256 segments.
- Each segment starts at the time at which the one before it ends, at the altitude at which that one ends. Both are exact.
- The first segment starts with the first leg, and the last segment ends with the last leg.

**Valid.** A trajectory is *valid* if it meets this section, every value is inside the limits of section 1, and every position on every leg is inside the limit for a coordinate. A kernel computes only with valid trajectories.

The *span* of the trajectory runs from the start of its first leg to the end of its last leg. `p(t)` is the position at a time t: `S(t)` of the leg in effect, with a time outside the span moved to the nearest end.

**Nominal altitude.** Inside a segment the nominal altitude is `a0 + floor((a1 − a0)·(t − t0) / (t1 − t0))`. Separation does not use it. The kernel uses it where an amendment cuts a trajectory.

## 3. Tube

A *tube* is the protected envelope of one trajectory. It has these values.

| Field | Meaning | Unit |
|---|---|---|
| trajectory | The nominal path | |
| `anchor` | The time at which the radius equals `r0` | µs |
| `r0` | Radius at the anchor | µm |
| `k` | Growth of the radius | µm/s |
| `vu0` | Vertical uncertainty at the anchor | µm |
| `vuRate` | Growth of the vertical uncertainty | µm/s |
| `vmin` | The lowest ground speed that the aircraft may fly on this trajectory. Zero means that there is no allowance (below). | µm/s |

A tube is *valid* if its trajectory is valid and its values are inside the limits of section 1, with `0 ≤ vmin ≤ 2^31`.

Two functions of time follow, both in picometres and both exact.

| Function | Definition |
|---|---|
| Radius | `R(t) = M·r0 + k·max(0, t − anchor)` |
| Vertical uncertainty | `U(t) = M·vu0 + vuRate·max(0, t − anchor)` |

**Segments in effect.** The aircraft may be early or late by the time in which it covers its own radius, `R(t) / vmin`. So a vertical segment counts for longer than its nominal times. A vertical segment `s` is *in effect* at time t if both of these hold:

- `vmin·(s.t0 − t) ≤ R(t)`
- `vmin·(t − s.t1) ≤ R(t)`

The rule has no division, so it has no rounding. With `vmin = 0` there is no allowance, and `s` is in effect when `s.t0 ≤ t ≤ s.t1`.

**Slack.** A separation standard has a *slack* `w`, a tolerance in time (section 4). Under a slack `w`, `s` is in effect at time t if it is in effect, by the rule above, at some whole microsecond t′ with `t − w ≤ t′ ≤ t + w`. The slack is zero in a normal check.

**Extent.** The *vertical extent* of the tube at time t is the range from the lowest band limit to the highest band limit over all segments in effect at t, under the slack of the standard. Write it `[lo(t), hi(t)]`.

**What a tube claims.** This paragraph explains and does not require anything. At time t the aircraft is within `R(t)` of `p(t)`, and its altitude is within `U(t)` of the vertical extent. One disc covers along-track and cross-track error: a point at along-track offset s on the path is at most |s| from the nominal point, on a straight path and on a bent one.

## 4. Separation

A *standard* has three values.

| Value | Meaning | Normal value |
|---|---|---|
| `sh` | Horizontal distance between the two discs | 5 nm† = 9,260,000,000 µm |
| `svTrue` | Vertical distance between the two extents, after each is widened by its uncertainty | 600 ft† = 182,880,000 µm |
| `vertSlack` | The slack `w` of section 3, from 0 to 2^40 µs | 0 µs |

For two tubes a and b at one time t, write `req(t) = M·sh + R_a(t) + R_b(t)`: the *required distance*, in picometres.

- They are **horizontally separated** if `req(t) ≤ 2^62` and `|p_a(t) − p_b(t)|² ≥ req(t)²`. Both sides are exact integers below 2^127. A pair that needs more than 2^62 pm is not horizontally separated.
- They are **vertically separated** if `M·(lo_a(t) − hi_b(t) − svTrue) ≥ U_a(t) + U_b(t)`, or if the same holds with a and b exchanged.
- They are **separated** if they are horizontally separated or vertically separated.

No square root is taken, and nothing is rounded.

**The interval of a check.** A check has a start `from` and an end `to`. It covers every whole microsecond t with `lo ≤ t ≤ hi`, where

- `lo = max(from, start of a, start of b, anchor of a, anchor of b)`
- `hi = min(to, end of a, end of b)`

If `hi ≤ lo`, there is nothing to check, and the pair is separated.

**Conflict.** The pair is *in conflict* if it is not separated at some time of the interval. The *first conflict time* `t*` is the least of those times. It is one definite microsecond.

**Level aircraft.** Two contract tubes have a vertical uncertainty of 200 ft† each. With `svTrue` at 600 ft, two level aircraft are vertically separated when their assigned levels are at least 1,000 ft apart.

## 5. The Verifier

### 5.1 Function

A Verifier is one function.

```
FirstConflict(a, b, from, to, standard) → (conflict, t)
```

- `a`, `b`: two valid tubes.
- `conflict`: true or false.
- `t`: a time. It has meaning only when `conflict` is true.

The caller guarantees valid tubes. What a Verifier does with an invalid tube is not specified.

### 5.2 Requirements

A Verifier may be *conservative*: it may report a conflict for a pair that is separated. It must never be the other way round.

| | Requirement |
|---|---|
| V1 | **Sound.** If the pair is in conflict under the standard, the Verifier must report a conflict. |
| V2 | **Conservative within a bound.** If the pair is separated under the standard *tightened by the tolerance*, the Verifier must report no conflict. |
| V3 | **Never late.** A reported time `t` must not be later than the first conflict time `t*` under the standard. |
| V4 | **Never early beyond the bound.** A reported time `t` must not be earlier than the first conflict time under the tightened standard, and it must lie in the interval of the check. |
| V5 | **Deterministic.** The same inputs must give the same outputs, on every machine. A Verifier must not use random numbers, a clock, floating point, or state that outlives one call. |
| V6 | **Total and bounded.** For valid tubes a Verifier must return, in time and memory that depend only on the number of legs and segments and on the length of the interval. |

Each requirement is exact: it compares integers. An *exact* Verifier reports `t*` itself.

**The tightened standard.** For a standard `(sh, svTrue, vertSlack)` and a pair of tubes, the standard tightened by a distance `D` and a time `T` is:

- `sh + D`
- `vertSlack + T`
- `svTrue + ceil((vuRate_a + vuRate_b)·T / M)`

The second line is a tolerance of `T` in time (section 3): a vertical segment counts if it is in effect within `T` of the time of the test. The third line is there because a vertical uncertainty that grows with time is larger `T` later.

**The tolerance.** `D = 0.75 nm`† = 1,389,000,000 µm, and `T = 1 s`† = 1,000,000 µs.

The tolerance exists so that two different methods can both conform. An exact method reports the true answer. A method that proves separation over short intervals rejects a little more, a little earlier. Between the standard and the tightened standard, either answer conforms.

**What the tolerance covers.** Take a method that tests an interval of at most `T` as a whole and reports the start of an interval that it cannot prove.

- *Vertical.* It counts every segment that is in effect at some time of the interval. Each of those is in effect within `T` of the start. The largest uncertainty of the interval is at most `vuRate·T` above the uncertainty at the start. So the tightened standard covers the vertical part for every valid tube.
- *Horizontal.* Between the start and the end of the interval, the distance changes by at most the sum of the two ground speeds times `T`, and the required distance by `(k_a + k_b)·T`. So `D` covers the horizontal part if the two ground speeds and the two growth rates sum to at most `D / T` = 2,700 kt. Section 10 says what is open above that.

### 5.3 More than one implementation

The kernel uses two or more Verifier implementations together (design, section 9.5).

- They must be built from this document by different authors, and they must share no code for geometry.
- A pair is approved only if **every** implementation reports no conflict.
- If all report a conflict, the first conflict time is the earliest of the reported times.
- If they disagree, the pair is rejected, and the disagreement is classified. The approving implementation runs again with the standard tightened by the tolerance.
  - If it now reports a conflict, the disagreement is *marginal*. The pair lies between the two standards, where V2 permits both answers.
  - If it still reports no conflict, the disagreement is *hard*. At least one implementation breaks this specification. The kernel raises an alarm.

A record names the arrangement of its kernel. This project has two. *dual*: the exact implementation and the sampled implementation with a step of 1 s. *single*: the exact implementation alone.

Each implementation must also pass every test alone. A pair of implementations can hide a defect in one member: this project's exact implementation once reported the end of a conflict as its start, and the rule "earliest time wins" concealed it.

**The two methods of this project**, for information.

- *Exact.* It splits the interval where a leg or the set of segments in effect changes. Inside a piece, `q(t) = |p_a(t) − p_b(t)|² − req(t)²` is a quadratic polynomial in t with integer values, and the pair is in horizontal conflict exactly where `q(t) < 0`. Those times form one run, because the distance less the required distance is convex. A search on the sign of exact integers finds the first.
- *Sampled.* It takes the distance at the middle of an interval, rounded down, and subtracts the most that the pair can close on the way to either end, with speeds rounded up. If the result is at least the largest required distance of the interval, the interval is separated. For the vertical part it takes every segment that is in effect at some time of the interval. It finds them by testing the two inequalities of section 3 at the two ends of the interval, and it solves nothing. If it cannot prove the interval, it halves it. An interval of at most one second that it cannot prove is a conflict, at the start of that interval.

## 6. Kernel state

**Contract.** A contract is a tube with two more values: `divergeAt`, the time at which an amendment leaves the contract that it replaces, and a mark that says whether it was issued to an intruder. A contract tube has `r0 = W + E0 + Bh` when it is anchored, `k = K`, `vu0 = VTol`, `vuRate = 0`, and `vmin` equal to the lowest speed of its legs, where the speed of a leg is `floor(sqrt(vx² + vy²))` (section 8).

**Flight.** The Store holds, for each flight:

| Value | Meaning |
|---|---|
| authority | The one actor that may propose for this flight: automation or a human |
| active | The contract in force, if any |
| pending | An amendment that has been committed and not yet settled, if any |
| pending state | *none*, *awaiting* an answer, or *unknown* after a failed delivery |
| intruder | True when the flight has no conforming contract |
| intruder tube | A tube built from the last track, while the flight is an intruder |
| track | The last surveillance report |

**Windows.** For every two contract tubes of two different flights, the Store holds a *window*: the time until which the Verifier has shown them separated.

**Tube identifier.** The kernel numbers the tubes that it puts in the Store, from 1, in the order in which it makes them. Each committed contract, each accepted re-anchor, and each intruder tube takes the next number. A verdict that commits a contract gives the number of its tube.

**Invariants.** The design states them (section 6.2). In the terms of this document:

| | Invariant |
|---|---|
| I1 | Each flight has exactly one authority. A proposal from any other actor is rejected. |
| I2 | For every two contract tubes of different flights, the pair is separated at every time from the current time to the last microsecond before its window. |
| I3 | Every contract ends at or outside the cell boundary. |
| I4 | While an amendment is pending, the active tube and the pending tube are both in the Store, and I2 holds for each. |
| I5 | The kernel applies one input at a time. Each check sees the state that all earlier inputs left. |

## 7. Kernel operations

Each operation is one input. An operation that proposes a contract returns a verdict: committed, or rejected with one reason. The checks run in the order given, and the first that fails gives the reason. An input that names a flight that is not in the Store has no effect, except where a check below gives a reason.

### 7.1 Checking a new tube

Used by Admit, Propose, and ProposeRecontract. The new tube is checked against every tube of every other flight, in ascending order of flight, and for each flight in the order active, pending, intruder. Each check runs from the current time to the current time plus the horizon.

1. A hard disagreement rejects the tube: *disagree*.
2. A conflict with an intruder tube rejects it: *conflict*.
3. A conflict with a contract tube before the current time plus the commit horizon rejects it: *conflict*.
4. Otherwise the tube is committed. Its window with each contract tube is the first conflict time if there is one, and otherwise the earliest of: the current time plus the horizon, and the ends of the two tubes.

A rejection for *conflict* or *disagree* names the other flight and the time of the conflict.

### 7.2 Admit(flight, trajectory)

Commits the first contract of a new flight. The contract may start in the future, and it is active at once. Automation holds authority for a new flight.

| Check | Reason |
|---|---|
| The flight is not in the Store. | *duplicate* |
| The trajectory is valid and ends after the current time. | *malformed* |
| The trajectory ends at or outside the cell boundary: `|p(end)|² ≥ (M·(CellRadius − 18,520))²`. An end that is inside by the join tolerance counts as on the boundary. | *open* |
| Section 7.1, with the tube anchored at the current time. | *conflict* or *disagree* |

### 7.3 Propose(flight, tail, proposer)

Proposes an amendment. `tail` is the new trajectory from the divergence time `td`, which is the start of the tail. The new contract is the active trajectory up to `td`, followed by the tail.

| Check | Reason |
|---|---|
| The flight is in the Store. | *unknown-flight* |
| The proposer holds authority for the flight. | *authority* |
| The flight is not an intruder and has an active contract. | *intruder* |
| No amendment is pending. | *busy* |
| The tail is valid. | *malformed* |
| `td` is at least the minimum lead after the current time. | *lead* |
| `td` lies strictly inside the active contract, and the tail starts within the join tolerance of the active position at `td`, on each axis. | *malformed* |
| The vertical extent of the active tube at `td` is one level. | *vertical* |
| The tail starts at that level. | *malformed* |
| The tail ends at or outside the cell boundary, as in section 7.2. | *open* |
| The active trajectory up to `td`, followed by the tail, is valid. | *malformed* |
| Section 7.1. | *conflict* or *disagree* |

The new tube takes the anchor and the `r0` of the active tube. The aircraft may be anywhere inside the old envelope when the amendment is committed, so a fresh envelope would be too small.

On success the amendment is pending, in state *awaiting*.

### 7.4 ProposeRecontract(flight, trajectory, proposer)

Proposes a new contract for an intruder.

| Check | Reason |
|---|---|
| The flight is in the Store, and the proposer holds authority. | *unknown-flight*, *authority* |
| The flight is an intruder and has a track. | *intruder* |
| It has no pending amendment and no contract. | *busy* |
| The trajectory is valid. | *malformed* |
| It starts at least the minimum lead after the current time. | *lead* |
| It starts within the re-contract tolerance of the position that the track reaches at that time, at its present velocity. The distance is exact: in picometres, the start `M·p0` against `M·pos + vel·(start − track time)`. | *malformed* |
| It ends at or outside the cell boundary. | *open* |
| Section 7.1, with the tube anchored at the current time. | *conflict* or *disagree* |

On success the new contract is pending, in state *awaiting*, and an Ack makes it active (section 7.5). Its `divergeAt` is its start. The intruder tube stays in force. The flight stops being an intruder only when the Monitor sees a track inside the new contract (section 7.6).

### 7.5 Answers from the Gateway

| Input | Effect |
|---|---|
| Ack | The pending contract becomes active. The old tube and its windows are removed. |
| Refuse | The pending contract and its windows are removed. |
| DeliveryFailed | For an intruder: as Refuse. Otherwise the pending state becomes *unknown*: the kernel does not know which contract the aircraft flies, and both tubes stay protected. |

Each has no effect if the flight has no pending contract.

### 7.6 The Monitor: UpdateTrack(flight, track)

A track has a time, a position, a velocity, an altitude, a vertical rate, and a selected altitude that may be absent. A track with a value outside the limits of section 1 has no effect.

**Inside.** A track is *inside* a tube if all of these hold. A track later than the end of the tube counts as inside.

- `R(t) − M·Bh ≥ 0`, and the square of its distance from `p(t)`, in picometres, is at most `(R(t) − M·Bh)²`. The detection buffer `Bh` makes the conformance envelope smaller than the tube. If `R(t) − M·Bh` is above 2^62 pm, the distance is not tested.
- Its altitude is at least `lo(t) − VTol` and at most `hi(t) + VTol`.

**Intent.** If the intent check is on and the track has a selected altitude, that altitude must lie within 50 ft of the range of all segments that are in effect at t when the start of each segment is moved earlier by the intent look-ahead: the first inequality of section 3 becomes `vmin·(s.t0 − IntentLookahead − t) ≤ R(t)`. The intent of a track later than the end of the tube agrees with the tube.

**Rules.**

| Flight state | Track | Effect |
|---|---|---|
| Contract, no amendment past its divergence time | Outside the active tube | Becomes an intruder |
| | Inside, and intent disagrees with the active tube and with any pending tube | Becomes an intruder |
| Amendment pending, past its divergence time | Inside both tubes | None |
| | Inside the pending tube only | If the state is *unknown*: the pending contract becomes active |
| | Inside the active tube only | If the state is *unknown*: the pending contract is removed |
| | Outside both | Becomes an intruder |
| Intruder with a new contract, at or after its start | Inside it, and intent agrees | Stops being an intruder |
| | Still outside more than 30 s after its start | The new contract is removed |

A track before the start of the active contract changes nothing but the stored track. When a flight becomes an intruder, its contracts and their windows are removed. An intruder gets a new intruder tube from each track.

### 7.7 Intruder tube

Built from the last track. It has one leg: from the track position, at the track velocity, for the intruder horizon. It has one vertical segment over the same time.

| Track | Band | `vu0` |
|---|---|---|
| A selected altitude more than 150 ft from the altitude | From the altitude to the selected altitude | `IntruderVU` |
| Otherwise, a vertical rate under 5 ft/s in size | The nearest multiple of 1,000 ft if it is within 150 ft, else the altitude. Half-way between two multiples rounds up. | `VTol` |
| Otherwise | From the altitude to `altitude + floor(rate·IntruderHorizon / M)` | `IntruderVU` |

`r0 = IntruderR0`, `k = IntruderK`, `vuRate = 0`, `vmin = floor(sqrt(vx² + vy²))` of the track, and the anchor is the track time.

### 7.8 Re-anchor

Without re-anchoring, the radius of a contract grows without limit. A re-anchor moves the times of a contract to match where the aircraft is. It does not change the path, and no clearance is sent.

A flight is eligible if it is not an intruder, has an active contract with `start ≤ now < end`, has no pending amendment, and has a track taken at the current time. Flights are taken in ascending order.

1. Take the offset of the track from `p(now)`, in picometres, and the velocity `v` of the current leg. If `v` is zero, stop. The shift is `dt = floor((offset · v) / |v|²)`, in microseconds: the along-track offset as a time.
2. If `|dt| > 2^50`, stop. Subtract `dt` from every time in the trajectory. If the result is not valid, or does not have `start ≤ now < end`, stop: the flight keeps its contract. A leg with almost no speed turns a small offset into a long time.
3. Anchor the new tube at the current time, with `r0 = W + E0 + Bh`. The new `divergeAt` is the old one less `dt`.
4. Check the new tube against every contract tube of every other flight, from the current time to the current time plus the horizon. A hard disagreement stops the re-anchor. A conflict is acceptable only if the old tube had a window with that tube, and the conflict is no earlier than that window.
5. If every check is acceptable, replace the active contract, with a new tube identifier. Otherwise keep the old one: its guarantee for that pair is the better one.

The shift is a whole number of microseconds, so the new contract has exactly the path of the old one.

### 7.9 Tick

Extends the windows and lists the predicted conflicts.

For every two contract tubes of different flights, let `target` be the earliest of: the current time plus the horizon, and the ends of the two tubes. A pair with no window has the window 0.

- If the window is at or after the current time and within `ExtendSlack` of `target`, nothing is done.
- Otherwise the pair is checked from the later of its window and the current time, to `target`. If that start is at or after `target`, nothing is done. With no conflict, the window becomes `target`. With a conflict, the window becomes the first conflict time, and the pair is a *predicted conflict*.

Every contract tube is also checked against every intruder tube of another flight, from the current time to the current time plus the horizon. A conflict is a predicted conflict with an intruder.

**The list.** Each predicted conflict names two flights, A and B, and a time. For two contracts, A is the flight with the lower identifier. For a contract and an intruder tube, A is the flight with the contract. The list keeps one entry for each ordered pair (A, B): the earliest, and of two at the same time the one between two contracts. The entries are in order of time. Entries with the same time are in order of A, then of B.

The *coast time* is the time from now to the earliest predicted conflict between two contracts. If there is none, it is "no limit", written as the largest 64-bit integer.

### 7.10 Other inputs

| Input | Effect |
|---|---|
| Advance(t) | The clock moves to t, if t is later than the clock and at most 2^49. The clock starts at 0. |
| SetAuthority(flight, actor) | The actor holds authority for the flight (I1). |
| MarkIntruder(flight) | The flight becomes an intruder, as in section 7.6, for a reason outside the kernel: for example, it follows an instruction of the Safety Net. No effect if it is one already. |
| Remove(flight) | The flight leaves the Store, with its tubes and their windows. |

### 7.11 Record, replay, and the kernel program

The kernel is a deterministic state machine. Its state is the Store, and its inputs are the operations of this section. The same inputs in the same order give the same verdicts and the same state.

The kernel passes each input to a recorder before it applies the input, and passes each verdict before the operation returns. [record.md](record.md) defines the record that results: the inputs and verdicts in order, chained by hash, with signed checkpoints. It also defines the *state digest*, a SHA-256 value that covers every value of the Store.

A party that holds a record can apply its inputs to a new kernel. That replay must return each verdict of the record and reach each state digest, to the last bit.

**The kernel program.** The published form of the kernel is a program for one fixed instruction set: RISC-V RV32IMAC, which has no floating-point instruction. [vm.md](vm.md) defines the machine that runs it, the program's input, and its answers. The SHA-256 of the program names it. Every replica and every checker runs the same bytes on the same machine. A second kernel, in Go, is in this repository for the simulator; the two must agree on every bit.

## 8. Parameters

All are initial values†. The kernel holds them as integers.

| Name | Value | In kernel units | Meaning |
|---|---|---|---|
| `Sh` | 5 nm | 9,260,000,000 µm | Horizontal standard |
| `Sv` | 1,000 ft | 304,800,000 µm | Vertical standard between assigned levels |
| `VTol` | 200 ft | 60,960,000 µm | Altitude-keeping tolerance. `svTrue = Sv − 2·VTol` |
| `Horizon` | 1,200 s | 1,200,000,000 µs | How far ahead windows are extended |
| `CommitHorizon` | 900 s | 900,000,000 µs | How far ahead a new contract must be free of conflict |
| `ExtendSlack` | 30 s | 30,000,000 µs | How short of the horizon a window may fall before it is extended |
| `W` | 0.5 nm | 926,000,000 µm | Lateral navigation tolerance |
| `E0` | 0.2 nm | 370,400,000 µm | Surveillance and along-track tolerance at the anchor |
| `Bh` | 0.5 nm | 926,000,000 µm | Detection buffer |
| `K` | 10 kt | 5,144,444 µm/s | Growth of along-track uncertainty |
| `MinLead` | 120 s | 120,000,000 µs | Least time from a proposal to its divergence time |
| `CellRadius` | 150 nm | 277,800,000,000 µm | The prototype's cell is a disc |
| `IntruderHorizon` | 300 s | 300,000,000 µs | Length of an intruder tube. At most 2^29 µs |
| `IntruderR0` | 1.5 nm | 2,778,000,000 µm | Radius of an intruder tube at the track time |
| `IntruderK` | 2 nm/min | 61,733,333 µm/s | Growth of an intruder tube |
| `IntruderVU` | 300 ft | 91,440,000 µm | Vertical uncertainty of an intruder that is changing level |
| `RecontractTol` | 0.5 nm | 926,000,000 µm | Section 7.4 |
| `IntentLookahead` | 120 s | 120,000,000 µs | Section 7.6 |
| Tolerance | 0.75 nm, 1 s | 1,389,000,000 µm, 1,000,000 µs | Section 5.2 |
| Join tolerance | 10⁻⁵ nm | 18,520 µm | Section 2 |

A kernel refuses a set of parameters with any of these:

- a negative value, or a length or a speed above the limit of section 1;
- `Sh`, `Sv`, `CellRadius`, `Horizon`, `CommitHorizon`, or `IntruderHorizon` of zero;
- a time above 2^40 µs, or `IntruderHorizon` above 2^29 µs;
- `CommitHorizon` above `Horizon`;
- `Sv` that is not more than twice `VTol`;
- `W + E0 + Bh` above 2^40 µm.

## 9. Conformance

**The Verifier suite.** [conformance/README.md](../conformance/README.md) describes the suite, the file format, and how to run a Verifier written in any language.

- The suite has 628 vectors in ten groups. Each group aims at one way in which a Verifier can be wrong. Every number in a vector is an integer.
- Each vector has a class. *Conflict*: the Verifier must report a conflict (V1), at a time from the first conflict under the tightened standard to the first conflict under the standard (V3, V4). *Clear*: it must report none (V2). *Either*: the pair lies between the standard and the tightened standard.
- The expected answers come from the exact implementation in this repository. Each is confirmed by a statement of the rule of section 4 in arbitrary-precision arithmetic, which shares no code with the implementation.
- The suite is itself tested. Sixteen Verifiers with one deliberate defect each must all fail it. Four of those defects were real ones in this project: two in the exact implementation, the slack as version 0.1 defined it, and the sampled implementation as it was under that definition.

**Kernel vectors.** The same directory holds two records of a whole kernel ([record.md](record.md)): a session with every kind of input, and half an hour of dense traffic. A replay must give every verdict and every state digest, to the last bit.

**Two kernels.** The kernel exists twice: in Go (`internal/kernel`) and in Rust (`guest/kernel`), the second as a program for the machine of [vm.md](vm.md). One author wrote both from this document. The tests give the inputs of a record that the first wrote to the second, and compare every answer. They agree on both kernel vectors, and on records of two and of six hours, in the machine. As native programs they also agree on three records of four to six hours each: 21,788 verdicts and 960 state digests. And they agree on more than 700 random sessions of inputs that traffic does not have: very slow aircraft, values at the limits, and proposals that a kernel must refuse.

**What passing shows.** A Verifier agrees with this specification on 628 pairs. A kernel agrees with another kernel on the records that were tried.

**What passing does not show.** That an implementation is correct on other inputs, that it terminates within a bound, or that this specification says the right thing. A proof and a review answer those questions. Two kernels by one author can share one misreading.

## 10. Open points

- **No formal statement and no proof.** This prose is the source for both. Integer arithmetic makes the statement easier: every rule is a comparison of integers.
- **The distance tolerance has a speed limit.** `D` fits a method that works in steps of one second, for a pair whose two ground speeds and two growth rates sum to at most 2,700 kt (section 5.2). A valid tube may be faster: the limit of section 1 is about 2,950 kt for one aircraft. For such a pair the sampled method of this project can report a conflict that the tightened standard does not have, and the kernel would call that a hard disagreement. No traffic of the prototype is near the limit. A tolerance that grows with the speeds of the pair would remove it.
- **The tolerance values are provisional.** A different conservative method may need different values.
- **The allowance uses the lowest speed.** An aircraft that is early by some seconds is early by a different distance on a faster leg. The model covers this with margin and does not treat it exactly.
- **The disc is a simple shape.** It is larger than the true envelope, which is long and narrow. A tighter shape would give more capacity and a harder proof.
- **Time is a whole number of microseconds.** The rule says nothing about the instants between two microseconds. In one microsecond two aircraft close by less than a millimetre.
- **A join can move a position by up to 18.5 mm.** The exact Verifier computes with the positions as they are. The sampled one allows for the step.
- **The second kernel is not independent.** One author wrote both. A kernel by another author, from this document alone, is the test that this document is complete.
- **The kernel program is not yet reproducible by others.** Its bytes depend on the version of the compiler that built it. A pinned toolchain and a second builder would show that the published bytes come from the published source.
- **The machine is tested, and not proved.** [vm.md](vm.md) lists what its tests cover.
- **Straight legs and a flat plane.** No turn radius, no wind, no map projection.
