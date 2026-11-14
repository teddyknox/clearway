# Clearway kernel specification

| | |
|---|---|
| Version | 0.1, draft, 2026-11-14 |
| Purpose | To let a person who has not read this project's code build a Verifier, and check it |
| Tested by | [conformance/](../conformance/README.md) for sections 2 to 5. The Go tests in `internal/kernel` for sections 6 and 7 |
| Reads with | [design.md](design.md) for why the kernel is built this way |

**Words.** "Must" states a requirement. "May" states a permission. A dagger (†) marks an initial value that simulation must confirm.

**What is settled.** Sections 2 to 5 define the Verifier. They are complete enough to build from, and the conformance suite tests them. Sections 6 and 7 describe the kernel around the Verifier as the reference implementation behaves today. No conformance vectors exist for them yet. Section 10 lists what is open.

## 1. Conventions

- **Units.** Distance in nautical miles. Altitude in feet. Time in seconds. Speed in nautical miles per second.
- **Plane.** Positions are points in a flat plane with coordinates (x, y). The prototype has no map projection.
- **Numbers.** IEEE 754 double precision.
- **Time.** One clock per cell. Every time in a check is a value on that clock.

## 2. Trajectory

A *trajectory* is a list of legs and a list of vertical segments.

**Leg.** A leg has a start time `t0`, an end time `t1`, a start position `p0`, and a ground velocity `v`.

- `t1 > t0`.
- At time t with `t0 ≤ t ≤ t1`, the position on the leg is `p0 + v·(t − t0)`.

**Legs in a trajectory.**

- There is at least one leg.
- Each leg starts when the leg before it ends.
- Each leg starts where the leg before it ends. A turn is a change of velocity at a point; the model has no turn radius.

**Vertical segment.** A vertical segment has a start time `t0`, an end time `t1`, and two altitudes `a0` and `a1`, with `t1 > t0`.

- If `a0 = a1`, the segment is *level*.
- Otherwise it is a *transition*.
- The *band* of the segment is the closed range from `min(a0, a1)` to `max(a0, a1)`. A transition holds its whole band for its whole duration. The model does not assume a rate of climb.

**Vertical segments in a trajectory.**

- There is at least one segment.
- Each segment starts when the one before it ends, at the altitude where that one ends.
- The segments cover the same time span as the legs.

The *span* of the trajectory runs from the start of its first leg to the end of its last leg. `p(t)` is the position at a time t inside the span.

## 3. Tube

A *tube* is the protected envelope of one trajectory. It has these values.

| Field | Meaning | Unit |
|---|---|---|
| trajectory | The nominal path | |
| `anchor` | The time at which the radius equals `r0` | s |
| `r0` | Radius at the anchor | nm |
| `k` | Growth of the radius | nm/s |
| `vu0` | Vertical uncertainty at the anchor | ft |
| `vuRate` | Growth of the vertical uncertainty | ft/s |
| `vmin` | The lowest ground speed that the aircraft may fly on this trajectory. Zero means that the allowance below is zero. | nm/s |

Three functions of time follow from them.

| Function | Definition |
|---|---|
| Radius | `R(t) = r0 + k·max(0, t − anchor)` |
| Vertical uncertainty | `U(t) = vu0 + vuRate·max(0, t − anchor)` |
| Allowance | `σ(t) = R(t) / vmin` if `vmin > 0`, otherwise 0 |

**Segments in effect.** The aircraft may be early or late. So a vertical segment counts for longer than its nominal times. Under a slack `w`, a vertical segment `s` is *in effect* at time t if both of these hold:

- `s.t0 − w ≤ t + σ(t)`
- `t − σ(t) ≤ s.t1 + w`

`w` is the vertical slack of the separation standard (section 4). It is zero in a normal check.

**Extent.** The *vertical extent* of the tube at time t is the range from the lowest band limit to the highest band limit over all segments in effect at t. Write it `[lo(t), hi(t)]`.

**What a tube claims.** This paragraph explains and does not require anything. At time t the aircraft is within `R(t)` of `p(t)`, and its altitude is within `U(t)` of the vertical extent. One disc covers along-track and cross-track error: a point at along-track offset s on the path is at most |s| from the nominal point, on a straight path and on a bent one.

## 4. Separation

A *standard* has three values.

| Value | Meaning | Normal value |
|---|---|---|
| `sh` | Horizontal distance between the two discs | 5 nm† |
| `svTrue` | Vertical distance between the two extents, after each is widened by its uncertainty | 600 ft† |
| `vertSlack` | The slack `w` of section 3 | 0 s |

For two tubes a and b at one time t:

- They are **horizontally separated** if `|p_a(t) − p_b(t)| ≥ sh + R_a(t) + R_b(t)`.
- They are **vertically separated** if `lo_a(t) − hi_b(t) − (U_a(t) + U_b(t)) ≥ svTrue`, or if the same holds with a and b exchanged.
- They are **separated** if they are horizontally separated or vertically separated.

**The interval of a check.** A check has a start `from` and an end `to`. It covers the times t with `lo ≤ t ≤ hi`, where

- `lo = max(from, start of a, start of b, anchor of a, anchor of b)`
- `hi = min(to, end of a, end of b)`

If `hi ≤ lo`, there is nothing to check, and the pair is separated.

**Conflict.** The pair is *in conflict* if it is not separated at some time in the interval. The *first conflict time* `t*` is the greatest lower bound of those times.

**Margin.** For information only: the least value of `|p_a(t) − p_b(t)| − (sh + R_a(t) + R_b(t))` over the times in the interval at which the pair is not vertically separated. The pair is in conflict exactly when the margin is negative.

**Level aircraft.** Two contract tubes have a vertical uncertainty of 200 ft† each. With `svTrue` at 600 ft, two level aircraft are vertically separated when their assigned levels are at least 1,000 ft apart.

## 5. The Verifier

### 5.1 Function

A Verifier is one function.

```
FirstConflict(a, b, from, to, standard) → (conflict, t)
```

- `a`, `b`: two tubes that meet sections 2 and 3.
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
| V5 | **Deterministic.** The same inputs must give the same outputs. A Verifier must not use random numbers, a clock, or state that outlives one call. |
| V6 | **Total and bounded.** For valid tubes a Verifier must return, in time and memory that depend only on the number of legs and segments and on the length of the interval. |

**The tightened standard.** For a standard `(sh, svTrue, vertSlack)` and a pair of tubes, the standard tightened by a distance `D` and a time `T` is:

- `sh + D`
- `vertSlack + T`
- `svTrue + (vuRate_a + vuRate_b)·T`

The third line is there because a shift in time also moves a vertical uncertainty that grows with time.

**The tolerance.** `D = 0.75 nm`† and `T = 1 s`†.

The tolerance exists so that two different methods can both conform. An exact method reports the true answer. A method that proves separation over short intervals rejects a little more, a little earlier. Between the standard and the tightened standard, either answer conforms.

### 5.3 More than one implementation

The kernel uses two or more Verifier implementations together (design, section 9.5).

- They must be built from this document by different authors, and they must share no code for geometry.
- A pair is approved only if **every** implementation reports no conflict.
- If all report a conflict, the first conflict time is the earliest of the reported times.
- If they disagree, the pair is rejected, and the disagreement is classified. The approving implementation runs again with the standard tightened by the tolerance.
  - If it now reports a conflict, the disagreement is *marginal*. The pair lies between the two standards, where V2 permits both answers.
  - If it still reports no conflict, the disagreement is *hard*. At least one implementation breaks this specification. The kernel raises an alarm.

Each implementation must also pass every test alone. A pair of implementations can hide a defect in one member: this project's exact implementation once reported the end of a conflict as its start, and the rule "earliest time wins" concealed it.

## 6. Kernel state

This section and the next describe the reference implementation in `internal/kernel`.

**Contract.** A contract is a tube with two more values: `divergeAt`, the time at which an amendment leaves the contract that it replaces, and a mark that says whether it was issued to an intruder. A contract tube has `r0 = W + E0 + Bh` when it is anchored, `k = K`, `vu0 = VTol`, `vuRate = 0`, and `vmin` equal to the lowest speed of its legs (section 8).

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

**Invariants.** The design states them (section 6.2). In the terms of this document:

| | Invariant |
|---|---|
| I1 | Each flight has exactly one authority. A proposal from any other actor is rejected. |
| I2 | For every two contract tubes of different flights, the pair is separated from the current time to its window. |
| I3 | Every contract ends at or outside the cell boundary. |
| I4 | While an amendment is pending, the active tube and the pending tube are both in the Store, and I2 holds for each. |
| I5 | The kernel applies one input at a time. Each check sees the state that all earlier inputs left. |

## 7. Kernel operations

Each operation is one input. An operation that proposes a contract returns a verdict: committed, or rejected with one reason. The checks run in the order given, and the first that fails gives the reason.

### 7.1 Checking a new tube

Used by Admit, Propose, and ProposeRecontract. The new tube is checked against every tube of every other flight: active, pending, and intruder. Each check runs from the current time to the current time plus the horizon.

1. A hard disagreement rejects the tube: *disagree*.
2. A conflict with an intruder tube rejects it: *conflict*.
3. A conflict with a contract tube before the current time plus the commit horizon rejects it: *conflict*.
4. Otherwise the tube is committed. Its window with each contract tube is the first conflict time if there is one, and otherwise the earliest of: the current time plus the horizon, and the ends of the two tubes.

### 7.2 Admit(flight, trajectory)

Commits the first contract of a new flight. The contract may start in the future, and it is active at once.

| Check | Reason |
|---|---|
| The flight is not in the Store. | *duplicate* |
| The trajectory meets section 2 and ends after the current time. | *malformed* |
| The trajectory ends at or outside the cell boundary. | *open* |
| Section 7.1, with the tube anchored at the current time. | *conflict* or *disagree* |

### 7.3 Propose(flight, tail, proposer)

Proposes an amendment. `tail` is the new trajectory from the divergence time `td`, which is the start of the tail. The new contract is the active trajectory up to `td`, followed by the tail.

| Check | Reason |
|---|---|
| The flight is in the Store. | *unknown-flight* |
| The proposer holds authority for the flight. | *authority* |
| The flight is not an intruder and has an active contract. | *intruder* |
| No amendment is pending. | *busy* |
| The tail meets section 2. | *malformed* |
| `td` is at least the minimum lead after the current time. | *lead* |
| `td` lies strictly inside the active contract, and the tail starts within 10⁻⁵ nm of the active position at `td`. | *malformed* |
| The active contract has one level in effect at `td`, with the allowance of section 3. | *vertical* |
| The tail starts at that level. | *malformed* |
| The tail ends at or outside the cell boundary. | *open* |
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
| The trajectory meets section 2. | *malformed* |
| It starts at least the minimum lead after the current time. | *lead* |
| It starts within the re-contract tolerance of the position that the track reaches at that time, at its present velocity. | *malformed* |
| It ends at or outside the cell boundary. | *open* |
| Section 7.1, with the tube anchored at the current time. | *conflict* or *disagree* |

The intruder tube stays in force. The flight stops being an intruder only when the Monitor sees a track inside the new contract (section 7.6).

### 7.5 Answers from the Gateway

| Input | Effect |
|---|---|
| Ack | The pending contract becomes active. The old tube and its windows are removed. |
| Refuse | The pending contract and its windows are removed. |
| DeliveryFailed | For an intruder: as Refuse. Otherwise the pending state becomes *unknown*: the kernel does not know which contract the aircraft flies, and both tubes stay protected. |

### 7.6 The Monitor: UpdateTrack(flight, track)

A track has a time, a position, a velocity, an altitude, a vertical rate, and a selected altitude that may be absent.

**Inside.** A track is *inside* a tube if all of these hold. A track later than the end of the tube counts as inside.

- Its distance from `p(t)` is at most `R(t) − Bh`. The detection buffer `Bh` makes the conformance envelope smaller than the tube.
- Its altitude is at least `lo(t) − VTol` and at most `hi(t) + VTol`.

**Intent.** If the intent check is on and the track has a selected altitude, that altitude must lie within 50 ft of the range of all segments in effect from t to t plus the intent look-ahead.

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
| | Still outside 30 s after its start | The new contract is removed |

When a flight becomes an intruder, its contracts and their windows are removed.

### 7.7 Intruder tube

Built from the last track on every update. It has one leg: from the track position, at the track velocity, for the intruder horizon. It has one vertical segment over the same time.

| Track | Band | `vu0` |
|---|---|---|
| A selected altitude more than 150 ft from the altitude | From the altitude to the selected altitude | `IntruderVU` |
| Otherwise, a vertical rate under 5 ft/s | The nearest multiple of 1,000 ft if it is within 150 ft, else the altitude | `VTol` |
| Otherwise | From the altitude to the altitude that the rate reaches in the intruder horizon | `IntruderVU` |

`r0 = IntruderR0`, `k = IntruderK`, `vuRate = 0`, `vmin` is the track speed, and the anchor is the track time.

### 7.8 Re-anchor

Without re-anchoring, the radius of a contract grows without limit. A re-anchor moves the times of a contract to match where the aircraft is. It does not change the path, and no clearance is sent.

A flight is eligible if it is not an intruder, has an active contract that covers the current time, has no pending amendment, and has a track taken at the current time.

1. Measure the along-track offset of the track from `p(now)`, along the current leg. Divide by the speed of the leg to get a time `dt`.
2. Subtract `dt` from every time in the trajectory.
3. Anchor the new tube at the current time, with `r0 = W + E0 + Bh`.
4. Check the new tube against every contract tube of every other flight, from the current time to the current time plus the horizon. A conflict is acceptable only if it is no earlier than the window that the old tube had with that tube.
5. If every check is acceptable, replace the active contract. Otherwise keep the old one: its guarantee for that pair is the better one.

### 7.9 Tick

Extends the windows and lists the predicted conflicts.

For every two contract tubes of different flights, let `target` be the earliest of: the current time plus the horizon, and the ends of the two tubes.

- If the window is in the future and within `ExtendSlack` of `target`, nothing is done.
- Otherwise the pair is checked from the later of its window and the current time, to `target`. With no conflict, the window becomes `target`. With a conflict, the window becomes the first conflict time, and the pair is a *predicted conflict*.

Every contract tube is also checked against every intruder tube of another flight, from the current time. A conflict is a predicted conflict with an intruder.

The *coast time* is the time from now to the earliest predicted conflict between two contracts.

### 7.10 Log and replay

The kernel can record every input. Replaying a recorded log into a new kernel must produce the same state and the same verdicts. `Digest` returns a hash of the Store, the windows, and all verdicts so far. Two kernels with equal digests reached the same state by the same decisions.

## 8. Parameters

All are initial values†.

| Name | Value | Meaning |
|---|---|---|
| `Sh` | 5 nm | Horizontal standard |
| `Sv` | 1,000 ft | Vertical standard between assigned levels |
| `VTol` | 200 ft | Altitude-keeping tolerance. `svTrue = Sv − 2·VTol` |
| `Horizon` | 1,200 s | How far ahead windows are extended |
| `CommitHorizon` | 900 s | How far ahead a new contract must be free of conflict |
| `ExtendSlack` | 30 s | How short of the horizon a window may fall before it is extended |
| `W` | 0.5 nm | Lateral navigation tolerance |
| `E0` | 0.2 nm | Surveillance and along-track tolerance at the anchor |
| `Bh` | 0.5 nm | Detection buffer |
| `K` | 10 kt | Growth of along-track uncertainty |
| `MinLead` | 120 s | Least time from a proposal to its divergence time |
| `CellRadius` | 150 nm | The prototype's cell is a disc |
| `IntruderHorizon` | 300 s | Length of an intruder tube |
| `IntruderR0` | 1.5 nm | Radius of an intruder tube at the track time |
| `IntruderK` | 2 nm/min | Growth of an intruder tube |
| `IntruderVU` | 300 ft | Vertical uncertainty of an intruder that is changing level |
| `RecontractTol` | 0.5 nm | Section 7.4 |
| `IntentLookahead` | 120 s | Section 7.6 |
| Tolerance | 0.75 nm, 1 s | Section 5.2 |

## 9. Conformance

[conformance/README.md](../conformance/README.md) describes the suite, the file format, and how to run a Verifier written in any language.

- The suite has 588 vectors in nine groups. Each group aims at one way in which a Verifier can be wrong.
- Each vector has a class. *Conflict*: the Verifier must report a conflict (V1), at a time inside a stated range (V3, V4). *Clear*: it must report none (V2). *Either*: the pair lies between the standard and the tightened standard.
- **The guard.** V1 and V3 are strict. The suite tests them with a guard of 0.02 nm and 0.1 s: a vector is of class *conflict* only if the pair is still in conflict when the standard is relaxed by the guard. So the last digits of an implementation's arithmetic do not decide a test.
- The expected answers come from the exact implementation in this repository. Each is confirmed by a separate enumeration of the rule of section 4 at steps of 0.02 s.
- The suite is itself tested. Fourteen Verifiers with one deliberate defect each must all fail it. Two of those defects were real ones in this project.

**What passing shows.** The implementation agrees with this specification on 588 pairs.

**What passing does not show.** That the implementation is correct on other pairs, that it terminates within a bound, or that this specification says the right thing. A proof and a review answer those questions. The suite does not.

## 10. Open points

- **No formal statement and no proof.** This prose is the source for both.
- **Arithmetic across machines.** V5 holds on one machine. Two machines with different processors can differ in the last digit of a result, and so in a decision that lies exactly on a limit. Replicas of the kernel must agree bit for bit (design, section 9.5). The specification does not yet say how: fixed-point arithmetic and a single required instruction set are the candidates.
- **The tolerance values are provisional.** They fit a method that works in steps of one second. A different conservative method may need different values.
- **The allowance uses the lowest speed.** An aircraft that is early by some seconds is early by a different distance on a faster leg. The model covers this with margin and does not treat it exactly.
- **The disc is a simple shape.** It is larger than the true envelope, which is long and narrow. A tighter shape would give more capacity and a harder proof.
- **The kernel around the Verifier has no vectors.** A second kernel would need recorded input logs with the verdicts that they must produce.
- **Straight legs and a flat plane.** No turn radius, no wind, no map projection.
