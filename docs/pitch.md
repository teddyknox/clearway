# The pitch we want to be able to make

| | |
|---|---|
| Status | A target. Written 2026-11-14 and set in November 2029. |
| Warning | **Nothing in section 1 has happened.** Every result, date, number, and endorsement in it is invented. |
| Reads with | [design.md](design.md), [gap-analysis.md](gap-analysis.md), and [plan.md](plan.md), which works back from this pitch |

This is the talk that would win adoption of Clearway as the standard for automated separation in United States airspace. We write it first and work backwards from it.

Read each sentence of the pitch as a goal. The shape of a claim matters: "three bodies outside the project replayed the record" is something we must make true. The digits are placeholders for results that we must earn and then measure. Section 2 lists what each claim needs. Section 3 lists where the pitch departs from the current design.

The plan finds that the operational claims cannot be true by November 2029. Section 6 of [plan.md](plan.md) states the pitch that can be earned by then. This one becomes true about two years after the FAA approves a first operational evaluation.

## 1. The pitch

*November 2029. Ten minutes with the FAA official who owns the investment decision for the Common Automation Platform.*

Thank you for the time. I will ask you for three decisions. I will name them now, so that you can test everything I say against them.

1. Require that every clearance in the United States, from a controller or from a machine, passes through a kernel that has shown it to be safe.
2. Let that kernel issue clearances itself in the airspace where it has already done so without a loss, under gates that your safety office sets.
3. Start the equipage rule for Class A airspace.

None of the three asks you to trust me, a vendor, or a machine. Let me show you why.

### The problem you already have

You are short of certified controllers, and you will be for years. A controller takes years to certify and must retire at 56. Each one separates aircraft by voice, one sector at a time. So the capacity of the system is a head count, and every staffing gap becomes a delay program.

The platform that replaces ERAM and STARS will serve for twenty-five years. If you build it around one controller per sector, you buy this problem for twenty-five more.

### What Clearway is

Each flight has one cleared trajectory. We call it a contract. A small kernel holds every contract, and it is the only path by which a clearance reaches an aircraft. It commits a clearance only when it has shown that all contracts stay separated. Planners propose, and that includes AI planners. The kernel decides. No learned model sits on the path that approves a clearance.

So the first decision is not about automation. Today a controller can speak a clearance that no system has checked. Over the oceans you ended that years ago: there, a clearance passes a conflict probe before it goes out. Decision one applies your own oceanic rule to the whole country.

### What three years have shown

**It is proved.** The specification of the kernel is forty pages long. A proof assistant has checked that it keeps aircraft separated, and two groups outside the project have checked the proof. Four teams that never met built four Verifiers from that specification. A clearance needs all four to agree. In 900 billion checks they have disagreed beyond tolerance eleven times. Each time the kernel refused the clearance and raised an alarm, and we published the defect.

**It has watched your traffic.** For twenty-four months the kernel has run in shadow on live data at all twenty en route centers: 30 million flights. Three bodies outside the project have replayed the public record on their own machines. They found no clearance that the kernel approved and that was not separated. In the same months your controllers recorded 1,700 losses of separation. In 1,160 of them the kernel would have refused the clearance. In the others an aircraft left its clearance, and the kernel's Monitor reported it a median of 31 seconds before the controller's first transmission.

**It has read your charts.** We loaded every published route and procedure in the country into one model and ran one check on it: are any two closer than the standard allows, with no rule that keeps them apart? The check found 312 such pairs. One was the helicopter route and the approach to runway 33 at Washington National. Your facilities have now reviewed all 312.

**The assumption has been measured.** Everything rests on one question: does an aircraft stay where it was cleared? We measured it over 2.6 billion minutes of flight. The envelope that the kernel assumes held in all but three minutes per million, and the Monitor caught each of those within ten seconds. The data and the method are public.

**It has worked real airspace.** In Oakland's oceanic airspace the kernel has held clearance authority for fourteen months: 190,000 flights, with 97 percent of clearances issued with no human action. At Salt Lake Center, controllers have worked on the platform for eighteen months, with the kernel checking every clearance they enter. Since February the kernel has held authority there at night in the high airspace: 41,000 flights. There has been no loss of separation.

Three times a monitor demoted a cell: for a radar outage, for a software release that failed in its first hour, and for a week of jet-stream winds that pushed aircraft toward the edge of the envelope. Each time the cell went back to its controllers in good order. I count those three among our best results. The system stopped itself.

**The people who must live with it want it.** Controllers wrote the supervisor's role. One hundred and sixty of them have worked it in simulation at your Technical Center. The controllers' union and the pilots' union both ask you to proceed, and their letters are in your folder. The agreement with the controllers has three lines. A supervisor is a certified controller. No certified controller loses a position to Clearway. Staffing follows measured workload.

**It has no switch that turns the country off.** We cut power to a core site on a Tuesday afternoon during the Salt Lake trial. No one on a frequency noticed. A national laboratory attacked the system for a year with the source code in hand. It could not make a radio site accept a clearance from fewer than three of four replicas. And if every computer we run stops at once, aircraft continue on contracts that are already separated for at least ten minutes, and the cell drains.

### Who owns it, and what it costs

You do. The specification, the proofs, and the kernel are public under an open licence. There is no licence fee, and there is no vendor that you cannot replace. Three of the companies that bid for your automation platform already run their planners and their workstations on the kernel.

You have a lifecycle cost estimate and an integrated master schedule, built to the GAO's guides and reviewed by its auditors. The kernel changes the price of the platform by less than four percent. It removes the second replacement program that automation would otherwise need.

### What happens if we are wrong

- If aircraft stray more than we measured, the kernel spaces them farther apart. You lose capacity. You do not lose separation.
- If one Verifier is wrong, the others refuse, and the cell stops issuing new clearances.
- If the automation disappoints at any level, the cell stays at the level below. At the first level you already have something that you lack today: a platform on which no controller's clearance goes out unchecked.
- You can stop at any gate. Nothing that you decide today commits you to the next one.

### What I am not asking for

Not a national switch-on. Not fewer controllers. Not towers. Not trust in AI.

### The three decisions

1. Write the kernel standard, which the industry standards committee published this spring, into the requirements of the Common Automation Platform. Every clearance commits through a conforming kernel.
2. Authorize your safety oversight office to promote cells by the published gates. The first cells are the remaining oceanic airspace and the high airspace of four centers.
3. Publish the proposed rule for data link and selected-altitude broadcast in Class A airspace.

The first decision is one paragraph in a requirements document. It makes every later choice possible and forces none of them.

I cannot promise you a system that never fails. I can promise that every clearance it issues has been shown to be separated before a pilot hears it, that anyone can check the record, and that when the system is not sure, it slows down and says so.

I would like to leave this room with decision one.

## 2. What the pitch takes for granted

Each row is a claim in the pitch, what must exist for it to be true, and where the project stands on 2026-11-14.

| Claim | What must exist | Today |
|---|---|---|
| A machine-checked proof, checked again by outsiders | A formal specification of the kernel, a proof of its invariants, and two reviewers outside the project | Tests and a brute-force oracle |
| Four Verifiers by four teams that never met | A specification that others can build from, a conformance suite, and three more authors | Specification 0.1 and a suite of 588 vectors. Two implementations by one author |
| Two years of shadow at twenty centers | Access to live surveillance and flight data, an agreement with the FAA, and a recorder that runs without a break | A simulator. No access to data |
| Outsiders replayed the public record | A hash-chained log, signed checkpoints, a public replay tool, and witnesses who take it up | Replay and a state digest |
| The kernel would have refused most recorded losses | Recorded losses of separation, matched with the clearances that led to them | None |
| The airspace check found 312 pairs | The national route and procedure data in the Airspace Model, and the static check | Neither |
| The envelope was measured | Recorded tracks matched with what each aircraft was cleared to fly, and a published method | The simulator makes it true |
| Authority over oceanic airspace | Oceanic separation standards in the kernel, a link to the oceanic system, and an operational approval | Not designed |
| Controllers work on the platform at a center | The platform as a controller workstation, data link, a safety approval, and a facility that wants it | One simulated cell |
| Authority at night in domestic high airspace | Everything above, and gates E1 to E6 of the design passed for that cell | The gates are defined |
| Three demotions that worked | Monitors for every assumption, and automatic demotion | Designed, not built |
| Controllers wrote the supervisor's role | A working relationship with the controllers' union from the first year | No contact |
| 160 controllers in simulation | A human-in-the-loop simulator, and time at the Technical Center | A viewer |
| A core site lost with no effect; a year of attack survived | Four replicas, quorum certificates, and an outside red team | One process |
| Three bidders run on the kernel | Stable public interfaces, a public test bench, and vendors who see a reason | Internal interfaces |
| A cost estimate and a schedule that auditors reviewed | Someone whose job that is | No estimate |
| A kernel standard to cite | A standards committee, and years of its time | None |
| A proposed rule ready to publish | The regulatory analysis behind it | A line in the design |

## 3. Where the pitch departs from the current design

- **Oceanic airspace comes first, not last.** The design leaves oceanic airspace to phase 6. The pitch makes it the first place where the kernel holds authority: most aircraft there already use data link, traffic is thin, and the controller already relies on a conflict probe.
- **The first decision is the kernel under human controllers.** The design treats level L1 as a step on the way. The pitch makes it the main request: every clearance is checked, whoever issues it. Autonomy is then a separate and smaller decision.
- **Four Verifier implementations, not two.** With the rule that all must agree, more implementations add protection against a shared defect and cost liveness.
- **A published standard.** The design speaks of a government-owned specification. The pitch needs a standard that an official can cite in a requirements document.
- **A check of the national airspace data as an early product.** It needs no authority and no change to operations, and it gives the FAA something useful before any clearance is at stake.
