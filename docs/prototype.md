# Kernel prototype and simulation results

| | |
|---|---|
| Status | Prototype 0.1, 2026-11-14 |
| Scope | The kernel of [design.md](design.md) section 6 for one en route cell, with a fast-time simulator around it |
| Code | Go 1.26, standard library only. About 3,900 lines plus 1,200 lines of tests. |

The prototype exists to answer two questions.

1. Does the central claim of the design hold when it is built? The claim: two aircraft that conform to verified contracts do not lose separation, whatever the planner proposes.
2. What did the design leave out? A design that has not been built hides its gaps.

## 1. Results in brief

All numbers come from 22 configurations, each run for 8 seeds of 24 simulated hours. Section 6 has the tables.

- **The kernel held.** The 21 configurations with a correct Verifier arrangement cover about 325,000 simulated flight hours. No loss of separation occurred between two aircraft that conformed to verified contracts. The closest such pair came to 7.4 nm against a standard of 5 nm.
- **Planner quality changed efficiency, not kernel safety.** With a deliberately hostile planner, and with the Fallback Resolver and the Safety Net switched off, the Verifier received 218,000 proposals and rejected 96,000. Flights received 4.4 clearances each in place of 0.4, and the mean delay rose from 5 s to more than 630 s. No kernel-class loss occurred.
- **The second Verifier implementation earned its place.** A faulty implementation alone produced 99 losses between conforming aircraft. The same faulty implementation beside a correct one produced none, and raised 37,512 alarms.
- **The second implementation also found two real defects in the first** during this work. One of them approved about a third of near-exact collision courses (section 7).
- **The Safety Net prevented every loss caused by aircraft faults** in these runs: 41 losses with alerts only, none with instructions.
- **The Safety Net also caused a loss.** It predicts from state alone, so it does not know that an aircraft will turn. Under the hostile planner it issued 161 instructions to aircraft that were in no danger, and one of those instructions led to a loss.
- **The growth rate of along-track uncertainty drives workload.** From 3 kt to 15 kt, clearances per flight rose from 0.29 to 0.46, and conflicts still open ten minutes ahead rose from 2 to 49.

These are results of a simplified model (section 9). They test the logic of the design. They are not a safety rate.

## 2. What the prototype contains

| Package | Tier | Content |
|---|---|---|
| `internal/kernel` | A | Contract Store, Clearance Verifier with two implementations, Conformance Monitor, verified windows, re-anchoring, input log and replay |
| `internal/safetynet` | A, independent | Tactical Safety Net. It imports nothing from this module. |
| `internal/traj`, `internal/geom` | A | Trajectories and plane geometry |
| `internal/planner` | B | Heuristic Trajectory Planner, Fallback Resolver, re-contracting of intruders, and a hostile "chaos" planner |
| `internal/sim` | — | True aircraft states, traffic, clearance delivery, faults, measurement |
| `internal/arch` | — | Tests that enforce the dependency rules of the design |
| `cmd/atcsim` | — | Command-line tool and experiment runner |

| Modelled | Not modelled |
|---|---|
| Invariants I1 to I5 and assumptions A1 to A3 of the design | Surface, terminal airspace, runways, wake |
| One cell with entry and exit contracts | Several cells, boundary agreements, island mode |
| Level flight, climbs and descents, speed changes, lateral detours | Holding, weather, wind fields |
| Data link delay, lost uplinks, lost replies, refusals, one repeat | Voice, speech recognition |
| Aircraft that leave their route, leave their level, or have no radio | Surveillance faults, spoofing |
| The Safety Net with alerts and vertical instructions | Airborne collision avoidance |
| Automation authority; the authority rule itself | Supervisors and their decisions |

## 3. How to run

Run the tests. They take about 20 seconds.

```bash
go test ./...
```

Run one six-hour simulation with default settings.

```bash
go run ./cmd/atcsim
```

Run every experiment of section 6. This takes about four minutes on 16 cores.

```bash
go run ./cmd/atcsim -exp all -hours 24 -seeds 8
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
| `TestCheckersAgainstOracle` | Both Verifier implementations against a third, brute-force statement of the rule, on 6,000 random pairs. The analytic one matches it. The sampled one never approves a conflict. |
| `TestCollisionCourses` | Both implementations report 4,000 exact and near-exact collision courses, for every growth rate. |
| `TestSerializedCommit` | I5: two amendments that are each safe alone and unsafe together. The second is rejected. |
| `TestPendingProtection` | I4: while an amendment is pending, a third flight may conflict with neither the old tube nor the new one. |
| `TestProposalRules` | I1 and I3, and the rules for lead time, continuity, and vertical transitions. |
| `TestUnknownIsSettledByTheMonitor` | After a lost acknowledgement, the track decides which contract is active. |
| `TestMonitorMarksIntruder`, `TestRecontractIntruder`, `TestReanchor` | The Conformance Monitor, the return of an intruder to a contract, and re-anchoring. |
| `TestRandomProposalsKeepInvariants` | One hour of random admissions and random amendments. After every step, the brute-force check confirms I2 for every pair inside its verified window. A replay of the input log reaches the same state. |
| `TestKernelHoldsUnderEveryPlanner` | No kernel-class loss with the heuristic planner, no planner, the chaos planner, the chaos planner alone, and dense traffic. |
| `TestSecondImplementationCatchesFaultyVerifier` | A faulty implementation alone loses separation. Beside a correct one it does not, and alarms are raised. |
| `TestAircraftFaults`, `TestLostCommunications` | Faults and lost messages never become kernel-class losses. |
| `TestDeterminismAndReplay` | One seed gives one result. The kernel input log replays to the same state. |
| `internal/arch` | The kernel does not import the planner, the simulator, the Safety Net, random numbers, or the clock. The Safety Net imports nothing from the module. The two implementations do not call each other's geometry. |

## 6. Results

Each row is 8 seeds of 24 simulated hours at 120 flights per hour unless the row says otherwise. Communication faults are on in every run: 1% lost uplinks, 1% lost replies, 2% refusals.

### 6.1 Planner quality

| Planner | Flight hours | Kernel-class losses | Other losses | Clearances per flight | Mean delay (s) | Proposals rejected | Conflicts open at 10 / 5 / 2 min | Net instructions |
|---|---|---|---|---|---|---|---|---|
| Heuristic | 13,897 | 0 | 0 | 0.39 | 5 | 43,868 of 75,316 | 22 / 3 / 0 | 0 |
| None: Fallback Resolver only | 13,876 | 0 | 0 | 0.41 | 1 | 38,680 of 70,478 | 7,242 / 5,191 / 9 | 13 |
| Chaos | 17,807 | 0 | 1 intruder | 4.42 | 641 | 100,049 of 223,061 | 60 / 3 / 0 | 161 |
| Chaos, no Fallback Resolver | 17,794 | 0 | 0 | 4.33 | 641 | 96,562 of 217,552 | 51 / 2 / 2 | 160 |
| Chaos, no Fallback Resolver, Safety Net alerts only | 17,760 | 0 | 1 unresolved | 4.37 | 634 | 96,290 of 217,971 | 57 / 2 / 1 | 0 |

The chaos planner proposes random levels, random detours, routes aimed at other aircraft, contracts that end inside the cell, and tails that do not connect. In one 24-hour run the Verifier rejected its proposals for a conflict (5,866), a malformed trajectory (2,589), an open contract (2,337), too little lead time (820), a divergence inside a transition (523), and a pending amendment (466).

Findings:

- No planner produced a kernel-class loss.
- The last row has nothing but the kernel between the chaos planner and the traffic. Its one loss is of the unresolved class: the kernel had predicted the conflict, and no layer was left to act on it. This is a failure of liveness, as the design expects, and the reason for the escalation ladder.
- The Fallback Resolver alone handled this traffic with less delay than the heuristic planner. It prefers level changes, which cost no time. It acts late: 5,191 conflicts were still open at five minutes, and nine were still open at two.
- The one intruder-class loss under the chaos planner was caused by a Safety Net instruction (section 6.4).

### 6.2 A second Verifier implementation

In these runs the Safety Net alerts only, so that a Verifier fault shows as a loss. The faulty implementation reads a climb or descent as level flight at the starting altitude.

| Verifier arrangement | Flight hours | Kernel-class losses | Closest verified pair (nm) | Alarms |
|---|---|---|---|---|
| Two correct implementations | 13,897 | 0 | 8.3 | 0 |
| One correct implementation | 13,897 | 0 | 8.2 | — |
| One faulty implementation | 13,890 | 99 | 0.1 | — |
| One faulty, one correct | 13,899 | 0 | 8.5 | 37,512 |

A commit needs both implementations to approve. So a fault that makes one implementation too permissive cannot reach an aircraft. It shows as alarms.

Two correct implementations raised no alarm in any configuration, in about 2.5 million proposals. Reaching that took three corrections (section 7).

### 6.3 Aircraft faults

Fault rates per flight: 2% leave the route, 2% leave the level, 1% receive no clearance.

| Run | Flight hours | Kernel-class losses | Intruder-class losses | Net alerts: verified / unresolved / intruder | Net instructions |
|---|---|---|---|---|---|
| No aircraft faults | 13,897 | 0 | 0 | 0 / 0 / 0 | 0 |
| Faults | 13,901 | 0 | 0 | 5 / 0 / 49 | 55 |
| Faults, selected altitude not checked | 13,902 | 0 | 0 | 21 / 0 / 36 | 58 |
| Faults, Safety Net alerts only | 13,901 | 0 | 41 | 5 / 0 / 50 | 0 |

In one 24-hour run with faults, the Monitor reported 61 route deviations and 45 wrong selected altitudes, the planner brought 110 intruders back under contract, and none of those re-contracts failed.

Findings:

- An aircraft that leaves its contract is outside the kernel's guarantee. In these runs the Safety Net's instructions prevented all 41 losses that occurred without them.
- The selected-altitude check catches a level bust before the aircraft moves. Without it, the Safety Net raised 21 alerts on pairs that the kernel still held as verified. With it, 5.
- No loss of the latency class occurred. The detection buffer of 0.5 nm was enough for a 5-second surveillance period in this model.

### 6.4 The Safety Net and aircraft that turn

The Safety Net extrapolates each track in a straight line. A contract with a turn in the next two minutes can look like a conflict to it. The kernel knows better, but the net is independent of the kernel by design.

| Run | Net alerts on verified pairs | Net instructions | Losses caused by a net instruction |
|---|---|---|---|
| Chaos planner, look-ahead 120 s | 152 | 161 | 1 |
| Chaos planner, 90 s | 94 | 97 | 0 |
| Chaos planner, 60 s | 35 | 36 | 0 |
| Aircraft faults, 120 s | 5 | 55 | 0 |
| Aircraft faults, 90 s | 2 | 47 | 0 |
| Aircraft faults, 60 s | 1 | 49 | 0 |

Findings:

- After the 161 alerts on verified pairs in the last row of section 6.1, where the net could not instruct, no pair came closer than 9.3 nm. The alerts were false.
- With instructions on, one false alert sent an aircraft out of its contract and into a loss. The closest approach after a false alert was 3.3 nm.
- A shorter look-ahead removed most false alerts and, in this model, still prevented every loss from aircraft faults.
- With the heuristic planner the net raised no false alert at 120 flights per hour, one at 180, and three at 240. The chaos planner makes the problem visible because it issues many detours.

This is a design finding, not a tuning result. Section 8 states the change.

### 6.5 Growth of along-track uncertainty

K is how fast the kernel must assume an aircraft can drift from its contract between re-anchors.

| K | Clearances per flight | Proposals rejected | Conflicts open at 10 / 5 / 2 min |
|---|---|---|---|
| 3 kt | 0.29 | 24,490 of 53,640 | 2 / 0 / 0 |
| 6 kt | 0.33 | 32,407 of 62,541 | 12 / 2 / 0 |
| 10 kt | 0.39 | 43,868 of 75,316 | 22 / 3 / 0 |
| 15 kt | 0.46 | 62,311 of 95,249 | 49 / 10 / 2 |

No run had a kernel-class loss. A larger K costs clearances and planning effort, and it leaves less time: at 15 kt two conflicts reached the Safety Net. This supports principle P1 of the design. How well aircraft hold an assigned trajectory sets the capacity of the system.

### 6.6 Traffic density

| Flights per hour | Flight hours | Clearances per flight | Proposals rejected | Conflicts open at 10 / 5 / 2 min | Net instructions | Losses |
|---|---|---|---|---|---|---|
| 60 | 7,016 | 0.21 | 9,917 of 23,667 | 1 / 0 / 0 | 0 | 0 |
| 120 | 13,897 | 0.39 | 43,868 of 75,316 | 22 / 3 / 0 | 0 | 0 |
| 180 | 20,806 | 0.56 | 121,201 of 173,970 | 154 / 31 / 3 | 7 | 0 |
| 240 | 27,825 | 0.71 | 282,042 of 359,837 | 548 / 136 / 23 | 44 | 1 intruder |

At 240 flights per hour the cell held up to 185 aircraft at once. The kernel still had no loss. The heuristic planner is the limit: 23 conflicts were still open at two minutes, the Safety Net issued 44 instructions, and one of them did not prevent the loss it was issued for. The kernel admitted every flight on its first request, at most 130 s late or at a nearby level, so back-pressure did not act. A real Flow Manager must act before the planner saturates.

## 7. Defects that the prototype found in itself

Three defects appeared during this work. Each one is a small case study for the design.

**1. The analytic implementation reported the end of a conflict as its start.**
When a check started exactly at the moment a conflict began, a root that should have been zero came out negative by rounding error. The code then took the other root, the time at which the conflict ends. The kernel recorded the pair as verified during the conflict, and re-anchoring stopped. A run with the analytic implementation alone showed one loss between conforming aircraft at 4.99 nm. With both implementations the defect was invisible, because the kernel takes the earlier of the two conflict times.

**2. The analytic implementation approved near-exact collision courses.**
For two aircraft aimed at one point at one time, the equation for the closest approach has a discriminant that cancels to zero. Rounding made it negative, the code found no minimum, and it approved the pair. The closed form missed 1,316 of 4,000 such courses in a direct test. The first random test did not contain this geometry. After the rule in item 3 was corrected, two hard disagreements remained in the full set of runs: the sampled implementation had rejected a pair and the alarm had fired. The one that was examined was this defect, and both disappeared with the fix.

Both defects were fixed by one change: the closed-form roots were replaced by bisection on a convex function, which has no cancellation. `TestCollisionCourses` and a re-check test now cover both.

**3. The rule for "the two implementations disagree" was wrong twice.**
The sampled implementation works in steps of one second. The analytic one is exact. Two altitude changes 0.4 s apart overlap for one and not for the other. The first rule compared distance margins only and raised false alarms. The rule is now: run the approving implementation again with the standard tightened by 0.75 nm and by one second. If it then rejects, the pair is inside the tolerance band and the disagreement is marginal. If it still approves, one implementation is wrong.

Lessons for the design:

- A second implementation of a different kind finds defects that tests of one implementation do not. Both real defects were in closed-form geometry, the part that looks most trustworthy.
- A second implementation can also hide a defect in the first. Each implementation must also be run alone in test.
- The most dangerous geometry, the exact collision course, is the one where exact arithmetic is weakest. It needs its own tests.
- "Disagreement" between a conservative and an exact implementation needs a stated tolerance in distance and in time. Without one, the alarm is noise.

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

## 9. Limits

- **One simplified cell.** No terminal airspace, runways, weather, wind field, or neighboring cell.
- **Tests, not proofs.** The invariants are checked by tests and by a brute-force oracle. Nothing is formally verified. The oracle shares the definition of the tube with the kernel, so it cannot find an error in that definition.
- **The aircraft model is kind.** Conforming aircraft stay inside the assumed bounds by construction. The runs say nothing about whether real aircraft do. That is assumption A2 of the design, and only recorded data can test it.
- **The fault model is small.** Three aircraft faults and three message faults. No surveillance fault, no wrong aircraft answering, no spoofing.
- **No rare-event method.** About 325,000 flight hours with no kernel-class loss is evidence about logic. It is far too little to support a collision-risk figure.
- **The planner is simple.** The density results describe this heuristic planner, not the limit of the design.
- **Speed changes are not modelled exactly.** An aircraft that is early by some seconds is early by a different distance on a faster leg. The model covers this with margin and does not treat it exactly.
- **Parameters are untuned.** Every value in section 4.2 is an initial value.
