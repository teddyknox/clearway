package sim

import (
	"fmt"
	"strings"
	"time"

	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/planner"
)

// Report is the result of one run.
//
// A loss of separation is two aircraft inside the cell closer than the
// horizontal standard and closer than the least true vertical distance that
// the standard assures. Each loss is counted once and classified by what the
// kernel claimed when it began:
//
//   - LossKernel: both aircraft were inside their contracts and the pair was
//     inside its verified window. The kernel's guarantee failed.
//   - LossLatency: the pair was verified, and one aircraft had left its
//     contract and the Monitor had not reported it yet.
//   - LossUnresolved: both aircraft conformed, and the verified window had
//     run out with the predicted conflict unresolved.
//   - LossIntruder: at least one aircraft was a known intruder.
type Report struct {
	Config Config

	Admitted          int
	Completed         int
	EntryDenied       int
	EntryLevelChanged int
	EntryDelaySum     float64
	EntryDelayMax     float64
	PeakInCell        int
	FlightSeconds     float64
	TransitDelaySum   float64
	AmendmentsFlown   int

	ClearancesSent     int
	ClearancesAcked    int
	ClearancesRefused  int
	ClearancesFailed   int
	ClearancesRepeated int
	RepliesStale       int

	Conflicts         int // predicted conflicts between two contracts
	ConflictsTo10     int // of those, still open 10 minutes before the conflict
	ConflictsTo5      int
	ConflictsTo2      int
	IntruderConflicts int

	CoastMin    float64
	CoastP1     float64
	CoastMedian float64

	// Safety Net alerts, classified by what the kernel claimed. An alert is
	// "verified" if the kernel held the pair as separated beyond the net's
	// look-ahead, "unresolved" if the kernel also predicted the conflict, and
	// "intruder" if one aircraft had no conforming contract.
	AlertsVerified   int
	AlertsUnresolved int
	AlertsIntruder   int
	Instructions     int
	// VerifiedAlertClosest is the least true distance (nm) that any pair
	// reached after a "verified" alert while inside the vertical threshold.
	// Zero if there was no such alert. +Inf if no pair came inside the
	// vertical threshold.
	VerifiedAlertClosest float64

	// ClosestVerified is the least true distance (nm) between two aircraft
	// at conflicting altitudes while the kernel held the pair as verified.
	// Zero if no such pair came within 12 nm.
	ClosestVerified float64

	LossKernel     int
	LossLatency    int
	LossUnresolved int
	LossIntruder   int
	LossDetail     []string
	// Losses in which one of the aircraft had received a Safety Net
	// instruction in the 5 minutes before. LossNetNotPrevented: the loss is
	// with the aircraft the instruction was meant to avoid. LossNetCaused:
	// the loss is with another aircraft, or the alert was a nuisance alert.
	LossNetNotPrevented int
	LossNetCaused       int

	SimInconsistencies int
	Kernel             kernel.Stats
	Marginal           uint64
	Hard               uint64
	Sources            map[string]planner.Counter
	Digest             uint64
	Wall               time.Duration
}

// FlightHours returns the hours flown inside the cell.
func (r *Report) FlightHours() float64 { return r.FlightSeconds / 3600 }

// Losses returns the number of losses of separation of every class.
func (r *Report) Losses() int {
	return r.LossKernel + r.LossLatency + r.LossUnresolved + r.LossIntruder
}

func per(n int, d float64) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / d
}

func (r *Report) String() string {
	var b strings.Builder
	c := r.Config
	p := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }
	p("run        seed %d, %.1f h, %.0f flights/h, planner %s, verifier %s, fallback %v, safety net %v",
		c.Seed, c.Duration/3600, c.FlightsPerHour, c.Planner, c.Verifier, c.Fallback, c.SafetyNet)
	p("faults     blunder %.3f, level bust %.3f, no radio %.3f per flight; lost uplink %.3f, lost reply %.3f, refusal %.3f per clearance",
		c.PBlunder, c.PLevelBust, c.PNordo, c.PLostUplink, c.PLostReply, c.PRefuse)
	p("traffic    %d admitted, %d completed, peak %d in cell, %.0f flight hours in cell",
		r.Admitted, r.Completed, r.PeakInCell, r.FlightHours())
	p("entry      mean delay %.0f s, max %.0f s, %d requests denied, %d admitted at another level",
		r.EntryDelaySum/float64(max(r.Admitted, 1)), r.EntryDelayMax, r.EntryDenied, r.EntryLevelChanged)
	p("clearances %d sent, %d acknowledged, %d refused, %d failed, %d repeated; %.2f flown per flight",
		r.ClearancesSent, r.ClearancesAcked, r.ClearancesRefused, r.ClearancesFailed, r.ClearancesRepeated,
		per(r.AmendmentsFlown, float64(max(r.Completed, 1))))
	var src []string
	for _, name := range []string{"planner", "chaos", "fallback", "recontract"} {
		if s, ok := r.Sources[name]; ok {
			src = append(src, fmt.Sprintf("%s %d/%d", name, s.Committed, s.Proposed))
		}
	}
	p("proposals  committed/proposed: %s", strings.Join(src, ", "))
	k := r.Kernel
	var rej []string
	for i := kernel.RejectReason(1); i < kernel.NumReasons; i++ {
		if k.Rejects[i] > 0 {
			rej = append(rej, fmt.Sprintf("%s %d", i, k.Rejects[i]))
		}
	}
	p("verifier   %d proposals, %d commits; rejected: %s", k.Proposals, k.Commits, strings.Join(rej, ", "))
	p("           disagreements: %d marginal, %d hard", r.Marginal, r.Hard)
	p("re-anchor  %d accepted, %d rejected", k.ReanchorOK, k.ReanchorRejected)
	p("conflicts  %d predicted; open at 10 min: %d, at 5 min: %d, at 2 min: %d; with intruders: %d",
		r.Conflicts, r.ConflictsTo10, r.ConflictsTo5, r.ConflictsTo2, r.IntruderConflicts)
	p("coast time min %.1f min, 1st percentile %.1f min, median %.1f min",
		r.CoastMin/60, r.CoastP1/60, r.CoastMedian/60)
	p("monitor    exits: %d horizontal, %d vertical, %d selected altitude, %d by instruction; lost acks settled: %d new, %d old; re-contracts: %d ok, %d failed",
		k.Exits[kernel.ExitHorizontal], k.Exits[kernel.ExitVertical], k.Exits[kernel.ExitIntent], k.Exits[kernel.ExitExternal],
		k.UnknownPromoted, k.UnknownDropped, k.RecontractOK, k.RecontractFailed)
	p("safety net alerts: %d on verified pairs, %d on unresolved conflicts, %d with intruders; %d instructions",
		r.AlertsVerified, r.AlertsUnresolved, r.AlertsIntruder, r.Instructions)
	if r.AlertsVerified > 0 {
		p("           closest true approach after an alert on a verified pair: %.2f nm", r.VerifiedAlertClosest)
	}
	p("losses     %d total: kernel %d, monitor latency %d, unresolved %d, intruder %d",
		r.Losses(), r.LossKernel, r.LossLatency, r.LossUnresolved, r.LossIntruder)
	p("           of these, %d caused by a Safety Net instruction, %d not prevented by one",
		r.LossNetCaused, r.LossNetNotPrevented)
	if r.ClosestVerified > 0 {
		p("           closest true approach of a verified pair at conflicting altitudes: %.2f nm (standard %.0f nm)",
			r.ClosestVerified, c.Kernel.Sh)
	} else {
		p("           no verified pair at conflicting altitudes came within 12 nm")
	}
	for _, d := range r.LossDetail {
		p("           %s", d)
	}
	p("efficiency mean transit delay %.1f s", r.TransitDelaySum/float64(max(r.Completed, 1)))
	if r.SimInconsistencies > 0 {
		p("WARNING    %d simulator inconsistencies", r.SimInconsistencies)
	}
	if r.Wall > 0 {
		p("wall time  %.1f s (%.0f times real time)", r.Wall.Seconds(), c.Duration/r.Wall.Seconds())
	}
	return b.String()
}
