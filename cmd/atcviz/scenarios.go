package main

import "autoatc/internal/sim"

// Scenario is one world that the viewer can run.
type Scenario struct {
	ID      string  `json:"id"`
	Group   string  `json:"group"`
	Name    string  `json:"name"`
	Blurb   string  `json:"blurb"` // one line for the list
	Watch   string  `json:"watch"` // what to look for
	Minutes float64 `json:"minutes"`
	Seed    uint64  `json:"seed"`

	mut func(*sim.Config)
}

func flight(at, entry, exit, level float64) sim.ScriptedFlight {
	return sim.ScriptedFlight{At: at, EntryDeg: entry, ExitDeg: exit, Level: level, SpeedKt: 460}
}

func faults(blunder, bust, nordo float64) func(*sim.Config) {
	return func(c *sim.Config) { c.PBlunder, c.PLevelBust, c.PNordo = blunder, bust, nordo }
}

var scenarios = []Scenario{
	// Small scripted worlds that show one mechanism each.
	{
		ID: "head-on", Group: "One mechanism at a time", Name: "Two aircraft, head-on",
		Blurb:   "One predicted conflict, start to finish.",
		Watch:   "The conflict appears about 20 minutes ahead. At 12 minutes the planner starts to propose. Speed changes cannot fix a head-on course, so the kernel rejects them; it commits a level change. The amendment stays pending until the crew answers.",
		Minutes: 55, Seed: 1,
		mut: func(c *sim.Config) {
			c.Scripted = []sim.ScriptedFlight{flight(0, 180, 0, 35000), flight(0, 0, 180, 35000)}
		},
	},
	{
		ID: "crossing", Group: "One mechanism at a time", Name: "Two crossing streams",
		Blurb:   "Eight aircraft meet at one point, at one level.",
		Watch:   "Each pair would meet at the centre. The planner resolves them one at a time, and every amendment must also stay clear of the amendments already committed.",
		Minutes: 70, Seed: 1,
		mut: func(c *sim.Config) {
			for i := 0.0; i < 4; i++ {
				c.Scripted = append(c.Scripted, flight(i*180, 180, 0, 35000), flight(i*180, 270, 90, 35000))
			}
		},
	},
	{
		ID: "blunder", Group: "One mechanism at a time", Name: "One aircraft turns into a stream",
		Blurb:   "A crew leaves its route. The kernel cannot stop that.",
		Watch:   "Flight 2 turns toward the stream. The Monitor reports it within seconds and the kernel treats it as an intruder with a wide cone. The Safety Net instructs, and the planner then offers the intruder a new contract.",
		Minutes: 50, Seed: 1,
		mut: func(c *sim.Config) {
			// A track 20 nm north of the stream, and a 40 degree turn toward it.
			x := flight(120, 172.36, 7.64, 35000)
			x.BlunderFrac, x.BlunderDeg = 0.35, -40
			c.Scripted = []sim.ScriptedFlight{flight(0, 180, 0, 35000), x, flight(200, 180, 0, 35000), flight(400, 180, 0, 35000)}
		},
	},
	{
		ID: "level-bust", Group: "One mechanism at a time", Name: "One aircraft leaves its level",
		Blurb:   "A wrong altitude is selected under traffic 1,000 ft above.",
		Watch:   "Near minute 31, flight 1 selects the level of flight 2, which is right above it. The Monitor sees the wrong selected altitude at once and withdraws the contract. The Safety Net predicts a loss in ten seconds and tells flight 1 to stay below. The planner then gives it a new contract.",
		Minutes: 50, Seed: 1,
		mut: func(c *sim.Config) {
			low := flight(0, 180, 0, 35000)
			low.BustFrac, low.BustBy = 0.4, 1000
			c.Scripted = []sim.ScriptedFlight{low, flight(0, 180, 0, 36000), flight(240, 180, 0, 36000)}
		},
	},

	// Random traffic.
	{
		ID: "normal", Group: "Random traffic", Name: "Normal operations",
		Blurb:   "120 flights per hour, every layer on.",
		Watch:   "Most flights cross untouched. Watch the dashed conflict lines appear far ahead and vanish as the planner amends one of the two flights.",
		Minutes: 60, Seed: 1, mut: func(c *sim.Config) {},
	},
	{
		ID: "dense", Group: "Random traffic", Name: "Dense traffic",
		Blurb:   "240 flights per hour.",
		Watch:   "The kernel still holds. The planner is the limit: more proposals are rejected, and now and then a conflict is still open a few minutes ahead.",
		Minutes: 60, Seed: 1, mut: func(c *sim.Config) { c.FlightsPerHour = 240 },
	},
	{
		ID: "chaos", Group: "Random traffic", Name: "A hostile planner",
		Blurb:   "Random and hostile proposals, with the lower layers on.",
		Watch:   "The planner aims aircraft at each other, ends routes inside the cell, and sends tails that do not connect. Rejected proposals flash and vanish. The ones that the kernel commits are safe, and absurd. Near minute 34 the Safety Net misreads a turn and instructs an aircraft that was in no danger.",
		Minutes: 60, Seed: chaosSeed, mut: func(c *sim.Config) { c.Planner = "chaos" },
	},
	{
		ID: "chaos-alone", Group: "Random traffic", Name: "A hostile planner, kernel alone",
		Blurb:   "No Fallback Resolver. The Safety Net only alerts.",
		Watch:   "Nothing but the kernel stands between the planner and the traffic. Losses of the kernel class stay at zero. The Safety Net alert near minute 34 is false: the net does not know that the aircraft is about to turn, and here it may not act on its mistake.",
		Minutes: 60, Seed: chaosSeed,
		mut: func(c *sim.Config) { c.Planner, c.Fallback, c.SafetyNet = "chaos", false, false },
	},

	// Things that go wrong outside the kernel, and inside it.
	{
		ID: "faults", Group: "Failures", Name: "Aircraft that disobey",
		Blurb:   "8% leave the route, 8% leave the level, 3% have no radio.",
		Watch:   "Orange aircraft are outside the kernel's guarantee. Each one gets a cone, its neighbours are moved, and it is brought back under contract when it answers. Twice, near minutes 44 and 58, only a Safety Net instruction keeps an intruder clear of another aircraft.",
		Minutes: 60, Seed: faultsSeed, mut: faults(0.08, 0.08, 0.03),
	},
	{
		ID: "faults-alerts-only", Group: "Failures", Name: "Aircraft that disobey, net alerts only",
		Blurb:   "The same faults. The Safety Net may not instruct.",
		Watch:   "The same traffic and the same faults. Without instructions from the Safety Net, two intruders reach other aircraft, near minutes 44 and 58. These losses are of the intruder class: the kernel had already withdrawn its guarantee.",
		Minutes: 60, Seed: faultsSeed,
		mut: func(c *sim.Config) { faults(0.08, 0.08, 0.03)(c); c.SafetyNet = false },
	},
	{
		ID: "lost-messages", Group: "Failures", Name: "Lost messages",
		Blurb:   "15% of aircraft have no radio. Replies are lost often.",
		Watch:   "A clearance with no answer leaves the kernel unsure which route the aircraft flies. It protects both routes until the track settles the question.",
		Minutes: 60, Seed: 1,
		mut: func(c *sim.Config) { c.PNordo, c.PLostUplink, c.PLostReply = 0.15, 0.1, 0.3 },
	},
	{
		ID: "faulty-verifier", Group: "Failures", Name: "A faulty Verifier, alone",
		Blurb:   "One implementation that misreads climbs. No second opinion.",
		Watch:   "This Verifier reads a climb as level flight. It approves climbs through occupied levels, and conforming aircraft lose separation. This is the one scenario with losses of the kernel class.",
		Minutes: 90, Seed: faultySeed,
		mut: func(c *sim.Config) { faultyTraffic(c); c.Verifier = "broken-single" },
	},
	{
		ID: "faulty-verifier-pair", Group: "Failures", Name: "The same faulty Verifier, with a correct one",
		Blurb:   "Same traffic and seed. A commit needs both to approve.",
		Watch:   "The correct implementation outvotes the faulty one. No loss occurs, and each disagreement beyond tolerance raises an alarm.",
		Minutes: 90, Seed: faultySeed,
		mut: func(c *sim.Config) { faultyTraffic(c); c.Verifier = "broken-dual" },
	},
}

// Seeds chosen so that each scenario shows what it is for inside its default
// duration. TestScenariosShowTheirPoint keeps the descriptions honest.
const (
	chaosSeed  = 12 // one false Safety Net alert, near minute 34
	faultsSeed = 14 // two intruders reach other aircraft unless the net instructs
	faultySeed = 4  // the faulty Verifier alone loses separation four times
)

// faultyTraffic is traffic with many level changes, so that a Verifier that
// misreads climbs has many chances to be wrong. The Safety Net only alerts, so
// that the fault is not hidden.
func faultyTraffic(c *sim.Config) {
	c.FlightsPerHour, c.StepFraction, c.SafetyNet = 160, 0.7, false
}

func scenarioByID(id string) *Scenario {
	for i := range scenarios {
		if scenarios[i].ID == id {
			return &scenarios[i]
		}
	}
	return nil
}

// config returns the simulation configuration of s.
func (s *Scenario) config(seed uint64, minutes float64) sim.Config {
	cfg := sim.DefaultConfig()
	cfg.Seed, cfg.Duration = seed, minutes*60
	s.mut(&cfg)
	return cfg
}
