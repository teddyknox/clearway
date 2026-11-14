# Three-year plan, worked back from the pitch

| | |
|---|---|
| Status | Draft 0.1, 2026-11-14 |
| Covers | December 2026 to November 2029 |
| Reads with | [pitch.md](pitch.md) for the target, [gap-analysis.md](gap-analysis.md) for the starting point |

**Conventions.** A date is the last day on which a thing can become true and still serve the pitch. Staffing and reachability are my estimates, and they are rough. Section 9 lists the outside facts that were checked for this draft.

## 1. What working backwards shows

1. **The critical path is not code.** It is three relationships and one clock. The relationships are with a source of live traffic data, with working controllers, and with a sponsor inside the FAA. The clock is the FAA's choice of its Common Automation Platform, which we do not control.
2. **About half of the pitch is ours to make true.** The proof, the independent Verifiers, the replicas, the public record, the airspace check, the shadow run, and the envelope measurement need no one's permission. They need people and money.
3. **The operational claims are not reachable by November 2029.** The pitch says that the kernel has held clearance authority over real aircraft for fourteen months. No plan that we control puts that inside three years: it needs an FAA sponsor, an operational approval, and integration with a fielded system, in that order. This matches the design, which places first authority in the early 2030s.
4. **So there are two pitches.** The pitch as written is the one we can make about two years after the FAA approves a first operational evaluation. The pitch that we can earn by November 2029 asks for decision one, and for approval of that evaluation. Section 6 states it.
5. **Decision one may need to be won early.** The FAA has reportedly shortlisted vendors for the platform, and it wants the platform deployed by the end of 2028. If its requirements are fixed before we are heard, decision one becomes a change to a signed contract. The one-paragraph requirement must be in front of the program, the bidders, and the people who fund them in the first quarter of 2027.
6. **The first product is the airspace check.** It uses public data, needs no authority, and gives the FAA something useful in 2028. It is how the project earns the meetings that everything else depends on.

## 2. How the claims sort

Each claim in the pitch needs one of three things. *Ours*: work that we can do without permission. *Partner*: an organization that chooses to work with us. *FAA*: an act of the agency.

| Claim in the pitch | Needs | True by November 2029? | If not, the claim we can make |
|---|---|---|---|
| A machine-checked proof of the specification | Ours | Yes | — |
| The proof checked by two outside groups | Partner | Likely | One outside review complete, one in progress |
| Four Verifiers by four teams | Partner | Three teams is likely, four is a stretch | "Three teams", with a fourth under way |
| A year of running four Verifiers together | Ours, after the teams deliver | Only if all four exist by 2028-11 | The months actually run |
| Two years of shadow on live traffic | Ours, given data access | Yes, if access is granted by 2027-08 | The months actually run |
| Outsiders replayed the record | Partner | Likely | — |
| The kernel would have refused most recorded losses | FAA, for the loss records | Partly | Losses that the kernel itself observed in the tracks |
| The airspace check, and facilities that reviewed every finding | Ours for the check, FAA for the review | The check, yes. The review, partly | Findings delivered, with the reviews that were done |
| The envelope measured and published | Ours, given data | Yes, for what the data contains | Route and altitude keeping. Not responses to voice clearances |
| Authority over oceanic airspace for fourteen months | FAA | **No** | An approved plan for a first operational evaluation |
| Controllers work on the platform at a center | FAA | **No** | A display in shadow at one facility, if an agreement is signed by 2028-06 |
| Authority at night in domestic high airspace | FAA | **No** | The same operation in human-in-the-loop simulation on recorded traffic |
| Three demotions that worked | Ours | Yes, in shadow and in simulation | — |
| Controllers wrote the supervisor's role | Partner | Likely | — |
| 160 controllers at the Technical Center | Partner for the controllers, FAA for the venue | The number is a stretch. The venue needs an agreement | Fewer controllers, at a university laboratory |
| Letters from the controllers' and pilots' unions | Partner | A letter for decision one is possible. For decision two it is unlikely | Named participation, and a statement on decision one |
| A core site lost with no effect | Ours | Yes, in the shadow deployment | — |
| A year of attack survived | Partner | Likely | A university red team and a public bounty |
| Three bidders run on the kernel | Partner | One or two | The ones that do |
| Cost estimate and schedule reviewed by government auditors | FAA | **No** | An estimate reviewed by an independent estimator |
| A published kernel standard | FAA, to charter it | **No** | Specification 1.0, and a work item opened in a standards committee |
| A proposed rule ready to publish | FAA | No | The regulatory analysis, published |

## 3. The critical path

**The clock: the platform decision.** The FAA asked industry for information in November 2025 and ran a readiness assessment in early 2026. Trade press reports a shortlist of five vendors. No award date is public, and the program is not fully funded. The plan assumes an award in 2027 or 2028. Every month before it is worth more than any month after.

**Three relationships, each with a last date.**

| Relationship | Why it is on the critical path | Last date | If the date passes |
|---|---|---|---|
| Live traffic data | Shadow, the envelope, and the public record all need it. The FAA offers near-real-time data to outside users through its SWIM cloud service. | Access by 2027-08 | "Two years of shadow" becomes fewer months, or moves to public ADS-B data with no flight plans |
| Working controllers | The supervisor's role, the simulation results, and the unions' support all need them. No official will act against the controllers. | A controller on the project by 2027-03. Union contact by 2027-06 | The human claims become claims about retired controllers and students |
| A sponsor inside the FAA | Every claim marked FAA needs one. A research agreement takes six to twelve months to sign. | Agreement signed by 2028-06 | No claim in 2029 is set in an FAA facility |

**The chain behind the earliest deadlines.**

- Shadow by 2027-11 needs data access by 2027-08, which needs the application in 2026-12.
- Shadow also needs the kernel to read real flight plans, which needs the Airspace Model built from the FAA's public data by 2027-08.
- The pitch counts the project as one team, so four teams means three outside ones. Their Verifiers running together for a year needs the last of them by 2028-11. That needs outside teams at work by 2027-11, which needs a specification and a conformance suite by 2027-06. University teams form in the spring for a start in September.
- A proof reviewed by 2029-03 needs the proof done by 2028-06, which needs the formal specification by 2027-06.

## 4. The plan

Six streams run in parallel.

| Stream | Aim |
|---|---|
| A. Proof | A specification that others can build from, a proof, and Verifiers by other authors |
| B. Kernel | Replicas, the signed record, monitors with demotion, oceanic standards |
| C. Data | The Airspace Model, the airspace check, shadow, the envelope |
| D. People | Controllers, pilots, the supervisor's role, simulation with people in it |
| E. Institution | The requirement, a sponsor, a standard, a cost estimate |
| F. Project | A steward, money, contributors, vendors |

### 2026-12 to 2027-06: buildable by others, and the data requested

| Stream | Work | Output |
|---|---|---|
| A | Write the kernel specification in prose and in a formal language. Turn the brute-force oracle and the test pairs into a conformance suite that any language can run. | Specification 0.9. Conformance suite 1.0 |
| B | Chain the input log by hash. Sign checkpoints. Publish the replay tool. Build the assumption monitors and automatic demotion in the simulator. | A record that an outsider can check |
| C | Apply for data access. Load the FAA's public airspace and procedure data. Parse filed routes. Replay one recorded day. | Airspace Model 0. An answer on data access |
| D | Bring one experienced controller onto the project. Hold first talks with the controllers' union, two universities that train controllers, and the pilots' union. | A named controller. Three open doors |
| E | Publish the one-paragraph requirement with a four-page case for it. Send it to the platform program, to each shortlisted vendor, and to the committees that fund the program. | The requirement on the record |
| F | Publish the repository. Choose a neutral steward. Apply for the first grants. | A public project with a home |

**Gate 1, 2027-06-30.** Has the data request been answered? Is the specification published? Has one outside author agreed to build a Verifier? Is a controller on the project? Each "no" is a warning that a last date in section 3 is at risk.

### 2027-07 to 2027-12: shadow starts

| Stream | Work | Output |
|---|---|---|
| A | Prove the invariants for the geometry at the heart of the Verifier. Support the first outside Verifier team. | Proof in progress. First outside Verifier in progress |
| B | Run the kernel as four replicas with quorum certificates, on machines in four places. | The replication of design section 9.5, working |
| C | Start the shadow run on live en route data by 2027-11. Run the first airspace check. Review its findings with the controller on the project. | Shadow running. First findings |
| D | Hold the first workshop on the supervisor's role. Design the workstation for simulation with people in it. | Role draft 0 |
| E | Start a research agreement with the FAA's Technical Center or with NASA. | Paperwork under way |
| F | First paid contributors. A public test bench for planners. | A bench that a vendor can use |

**Gate 2, 2027-12-31.** Is the shadow run live? If not, the pitch claims the months that will exist.

### 2028-01 to 2028-06: evidence that others can check

| Stream | Work | Output |
|---|---|---|
| A | Finish the proof. Send it to two outside reviewers. The first outside Verifier passes the suite. | Proof complete. Two teams |
| B | Add oceanic separation standards and position reporting. Agree a year of attack with a laboratory. | Oceanic cell in simulation |
| C | Publish the airspace report, signed by people who design procedures. Deliver it to the FAA's safety office, the accident investigator, and the unions. Publish six months of shadow results with the signed record. | **The airspace report**: the project's first product |
| D | Build the simulation workstation. Run first sessions with retired controllers. Bring in line pilots. | Workstation 1 |
| E | Sign the research agreement. Present the specification at a standards forum and ask a member to open a work item. | A signed agreement |
| F | Hold a workshop for vendors on the planner bench. | Planners from outside |

**Gate 3, 2028-06-30.** Is a research agreement signed? If not, no 2029 claim is set in an FAA facility, and simulation moves to a university laboratory.

### 2028-07 to 2028-12: many hands

| Stream | Work | Output |
|---|---|---|
| A | The second and third outside Verifiers pass the suite. All run together on the shadow traffic from 2028-11. | Verifiers from four teams |
| B | Begin the year of attack. Cut power to a site of the shadow deployment, by plan and without notice. | Drill reports |
| C | Publish the envelope measurement after twelve months of shadow. Study every loss that the kernel observed. | The envelope paper |
| D | Run the first simulation campaign with 40 controllers. Controllers write version 1 of the supervisor's role. | Role 1.0, written by controllers |
| E | Build the cost estimate and the schedule for decision one across the country. Commission an independent review. | Cost estimate 1 |

### 2029-01 to 2029-06: rehearse the real thing

| Stream | Work | Output |
|---|---|---|
| A | Close the outside reviews of the proof and publish them. | A reviewed proof |
| C | Have two outside bodies replay eighteen months of the record on their own machines. | Two replays |
| D | Run the second campaign: automation with authority at night, and an oceanic cell, in simulation on recorded traffic. Use the Technical Center if the agreement allows it. | The operation in the pitch, in simulation |
| E | Place a shadow display at one facility if the agreement allows it. Write the plan for a first operational evaluation in oceanic airspace, with its safety case in the FAA's own form. Publish the analysis for the equipage rule. | The evaluation plan |

### 2029-07 to 2029-11: the pitch

Final reports from the red team, the reviewers, and the cost estimator. Statements from the unions. Rehearsal. The pitch of section 6 in November.

## 5. What the project needs and does not have

| Need | Why | When |
|---|---|---|
| A formal methods lead | The proof, and the specification that outside authors build from | 2027-01 |
| An experienced controller | The airspace check, the supervisor's role, and credibility with every other controller | 2027-03 |
| A data engineer | Ingest of live data and two years of shadow without a break | 2027-04 |
| Three outside Verifier authors | The claim cannot be met from inside | 2027-09 to 2028-06 |
| A human factors researcher | Simulation campaigns that a regulator will accept | 2028-01 |
| A person for government and standards work | Stream E is a job, not a side task | 2027-06 |
| A neutral steward | Unions, vendors, and the FAA will each work with a neutral body more readily than with one person | 2027-06 |
| Money | About 4 people in 2027, 8 in 2028, and 12 in 2029: about 25 person-years | From 2027-01 |

Today the project has one author and no funding. The first quarter decides whether the plan is real: it needs the repository public, the data request sent, and the first two people.

## 6. The pitch we can earn by November 2029

It keeps the three decisions and changes the second one.

1. **Decision one, unchanged.** Require that every clearance passes through a conforming kernel.
2. **Decision two, smaller.** Approve a first operational evaluation: oceanic airspace at night, under gates that the safety office sets.
3. **Decision three, unchanged.** Start the equipage rule.

The evidence changes in four places.

| The pitch says | The 2029 pitch says |
|---|---|
| "It has worked real airspace." | "It has run beside your airspace for two years, and controllers have run it with authority in simulation on your recorded traffic. We ask you to let it work real airspace, at night, over the ocean, where your own rule already requires a probe." |
| "The kernel standard, which the industry standards committee published this spring." | "Specification 1.0 is public, three teams have built from it, and a standards committee has opened a work item." |
| "Reviewed by the GAO's auditors." | "Reviewed by an independent estimator. We ask you to have your own auditors review it." |
| "Both unions ask you to proceed." | "Controllers wrote the supervisor's role, and their union supports decision one." |

The pitch as written then becomes true about two years after the evaluation is approved: 2031 or 2032.

## 7. What the plan leaves out on purpose

- **Voice.** The gap analysis calls it one of three gaps that decide the outcome. The 2029 pitch does not depend on it, because oceanic airspace uses data link. One small study of readback recognition keeps the question alive.
- **Terminal airspace, runways, and the surface.** They come after first authority.
- **A learned planner.** Any planner works with the kernel. The plan spends nothing on making one.

## 8. Risks to the plan

| Risk | Sign that it is happening | Response |
|---|---|---|
| The platform is awarded before we are heard. | An award notice in 2027. | Decision one becomes a requirement on the next release. The kernel is offered to the winner as a free component. |
| Data terms do not allow the use we need. | The access agreement limits use or publication. | Publish checkpoints and results, not data. Witnesses get their own access. |
| No sponsor inside the FAA appears. | Gate 3 passes with no agreement. | Work through NASA, a university, and the unions. The 2029 pitch loses every facility claim. |
| The controllers' union declines. | No reply by Gate 1. | Work with retired controllers and say so. Do not claim support that we do not have. |
| The measured envelope is wide. | Twelve months of data, in 2028. | Publish it. The design loses capacity, and the pitch says by how much. |
| An accident elsewhere is blamed on automated control. | News. | The record and the proof are the answer. Nothing else is. |
| One author. | Today. | The first two hires, and the steward. |

## 9. Outside facts checked for this draft

- The FAA's SWIM Cloud Distribution Service gives outside users near-real-time data, and its flight data service publishes tracks from the twenty en route centers. Its 2024 rules limit use to purposes that do not affect operations. Whether track data is available under the current terms was not confirmed, and the service desk must be asked. See [Getting Access to SWIM](https://www.faa.gov/air_traffic/technology/swim/products/get_connected) and the [SWIM questions and answers](https://www.faa.gov/air_traffic/technology/swim/questions_answers).
- The Common Automation Platform: a request for information in November 2025, a readiness assessment with responses due in January 2026, a reported shortlist of five vendors, a stated aim of deployment by the end of 2028, and no funding or award date found. See the [FAA announcement](https://www.faa.gov/newsroom/faa-seeks-solutions-new-air-traffic-automation-system) and [Flying](https://www.flyingmag.com/faa-billions-short-atc-data-automation-thales/), August 2026.
- Not checked: the lead time of an FAA research agreement, the time a standards committee takes, and every staffing figure. These are estimates from general knowledge.
