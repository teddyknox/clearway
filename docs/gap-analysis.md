# Gap analysis: from this prototype to a system that could carry traffic

| | |
|---|---|
| Status | 2026-11-14 |
| Reads with | [design.md](design.md) for the system, [prototype.md](prototype.md) for what is built |

The prototype shows that the idea is coherent in a kind, simplified world. A system that carries traffic must show that the real world behaves as the idea assumes. It must then earn the right to act on it. Almost all of the remaining work lies in that gap, and little of it is algorithmic.

## 1. The gaps

Section references are to the design document.

| Area | In the prototype | In a serious system | What closes the gap |
|---|---|---|---|
| Aircraft behaviour | A conforming aircraft stays inside its envelope because the simulator makes it: drift is at most 0.8 of the assumed rate. | Real aircraft, winds, and crews decide how wide the envelope must be. A wide envelope costs capacity. | Assumption A2 (6.2). Measure how far aircraft stray from assigned trajectories in recorded traffic. Set the envelope parameters from that data. Monitor them in operation. |
| The world | A flat disc of 150 nm radius. Straight routes at constant ground speed. No wind and no weather. | Wind fields with forecast error, performance that varies by type and weight, weather that crews fly around. | A Digital Twin that reproduces recorded days within a stated error (phase 0). |
| Surveillance | Exact positions every five seconds. It never fails. | Radar and ADS-B with error, delay, and gaps. Satellite navigation that can be jammed or spoofed. | Surveillance Fusion with integrity bounds, assumption A1 (6.2 and 6.3). Sensor faults injected in the Twin (gate E3). |
| Proof | Tests, and a brute-force check that shares the kernel's own definition of an envelope. One author wrote both Verifier implementations. | A formal specification, machine-checked proofs, and implementations by separate teams under a certification process. | Gate E1 (11.3). The two defects found in about 280 lines of Verifier geometry show why tests are not enough. An open kernel invites a Verifier from another author (13). |
| Scope | One en route cell. | Terminal airspace, runways, wake spacing, the airport surface, many cells with boundaries, oceanic airspace. Most delay and much of the risk are near airports. | Phases 4 to 6, in that order. Closed arrival contracts and runway reservations are not prototyped. |
| Flow | No Flow Manager. No run that was checked refused an admission, so back-pressure was never exercised. | Demand held below capacity hours ahead, by published rules for who takes delay. | The Flow Manager (6.3). In the density runs the planner saturated before the kernel refused anything. |
| Talking to aircraft | A data link with delay, loss, refusal, and one repeat. | Voice for most aircraft: synthesis, readback checks, crowded frequencies, callsign confusion. Aircraft with no data link, and aircraft with no contract at all. | The Delivery Gateway (6.3). Capture of controller speech at level L1 is the first real test of recognition. An equipage rule covers Class A (13). |
| People | None. The authority rule exists and is tested, and no one exercises it. | Supervisors who decide escalations and handle emergencies. Pilots who make requests. Controllers who must trust the system through a long transition. | Levels L1 and L2 (8.1), and human-in-the-loop simulation (gate E4). |
| Safety Net | It predicts from state alone and instructs only in the vertical. It caused one loss of separation. | A last ground layer that creates no risk of its own and that works with airborne collision avoidance. | Open (15.1). The net needs intent from the aircraft, or a Verifier rule, or a shorter look-ahead for instructions than for alerts. |
| Planner | Twelve manoeuvres, tried in a fixed order. | Optimization for fuel, delay, and fairness, with operator preferences. | The gap with the least risk. Any planner can be used, because the kernel decides. |
| Platform | One process, one thread, in memory. | Replicas across sites, island mode, staged releases, security, the availability of section 16, and years of running beside the legacy systems. | Section 9. The deterministic input log and its replay carry over. Replicas, quorum certificates, and the signed record of section 9.5 are not prototyped. |
| Airspace data | None: no terrain, no restricted airspace, no procedures. | Every procedure, restricted area, obstacle, and local agreement, including knowledge that only controllers hold. | The Airspace Model, written with controllers (phase 2). |
| Evidence | About 325,000 simulated flight hours. | An argument for a collision risk on the order of one in a billion flight hours. | The argument of section 11.1: proof, monitored assumptions, independent layers, and operation in shadow. Simulation alone cannot supply it. |
| Institutions | Not addressed. | A method to approve the system, a law that says who is accountable for a clearance, a migration with no day of outage, and a workforce that is part of it. | The migration plan and the rulemaking track (12 and 13). |

## 2. The three gaps that decide the outcome

**The envelope assumption.** Everything the kernel guarantees rests on aircraft staying inside their envelopes. In the simulator that is true by construction. In reality it is an empirical claim.

- *What settles it.* Recorded tracks compared with the trajectories that the aircraft were cleared to fly, over years and seasons.
- *If it goes badly.* The envelope must be wide. The prototype shows the cost: as the assumed drift rose from 3 kt to 15 kt, clearances per flight rose from 0.29 to 0.46, and conflicts still open ten minutes ahead rose from 2 to 49. The system stays safe and loses capacity.

**Voice and mixed equipage.** The prototype leaves out the hardest interface. Most aircraft receive clearances by voice.

- *What settles it.* Measured error rates for recognizing readbacks on real frequencies, and for crews understanding a synthetic voice.
- *If it goes badly.* Automation covers only aircraft with data link. Human sectors stay, and the staffing problem that motivates the design stays with them.

**Institutions.** No regulator has a method to approve this. The law does not say who is accountable for a clearance that a machine issues.

- *What settles it.* Rulemaking, and a programme that can replace the en route and terminal systems without a day of outage.
- *If it goes badly.* The system is built and is never given authority. It remains an advisory tool beside the controllers.

## 3. What carries over

- **The architecture claim.** The planner can be untrusted if the kernel decides. The hostile planner tested this directly.
- **The rules that the prototype forced.** Re-anchoring, the commit horizon, the inherited envelope, vertical timing, and the divergence point are now in the design (6.2).
- **The test method.** A hostile planner and a deliberately faulty Verifier test the claim instead of assuming it. The same method applies at any scale.
- **The kernel as a deterministic state machine.** One input log, replayed, gives one state. Replication, audit, and investigation all need this property.
- **The Safety Net finding.** An independent net that cannot see intent can cause harm. A paper design would not have shown this.

## 4. What the prototype cannot answer at all

These questions need data or people that a simulator of this kind does not have.

- The capacity of real airspace under this design.
- How many aircraft one supervisor can oversee.
- What the system costs to build and to run.
- Whether pilots and controllers accept it.
