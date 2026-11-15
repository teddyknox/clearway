package kernel

import (
	"fmt"

	"github.com/teddyknox/clearway/internal/fx"
)

// The tolerance of the specification, section 8: how far a conservative
// implementation may be from the exact answer.
const (
	Tol     = 3 * fx.NM / 4 // µm
	TimeTol = fx.Sec        // µs
)

// Dual runs two independent checkers and approves only if both approve.
//
// The two implementations need not agree exactly: one may be conservative.
// They must agree outside a tolerance band around the standard. When one
// approves and the other rejects, Dual runs the approving checker again with
// the standard tightened by Tol in distance and TimeTol in time.
//
//   - If the approver now reports a conflict, the pair is inside the band.
//     The disagreement is marginal.
//   - If the approver still approves, the two differ by more than the
//     tolerance. One implementation is wrong. The disagreement is hard.
//
// Dual rejects in both cases and counts them separately.
type Dual struct {
	A, B    PairChecker // B may be nil: single-implementation mode
	Tol     int64       // µm
	TimeTol int64       // µs

	Marginal uint64
	Hard     uint64

	// OnHard, if set, receives each hard disagreement. An operational system
	// raises an alarm here and keeps the inputs for analysis.
	OnHard func(a, b *Tube, from, to int64, ra, rb Result)
}

// Tighten returns the standard sp tightened by tol in distance and by timeTol
// in time, for one pair of tubes (specification, section 5.2). A tolerance in
// time lets a vertical segment count if it is in effect within timeTol of the
// time of the test. It also covers a vertical uncertainty that grows in that
// time.
func Tighten(a, b *Tube, sp SepParams, tol, timeTol int64) SepParams {
	grow, _ := fx.Mul(a.VURate+b.VURate, timeTol).DivCeil(fx.M)
	return SepParams{Sh: sp.Sh + tol, SvTrue: sp.SvTrue + grow, VertSlack: sp.VertSlack + timeTol}
}

// Check returns the merged result. hard is true on a hard disagreement.
func (d *Dual) Check(a, b *Tube, from, to int64, sp SepParams) (res Result, hard bool) {
	ra := d.A.FirstConflict(a, b, from, to, sp)
	if d.B == nil {
		return ra, false
	}
	rb := d.B.FirstConflict(a, b, from, to, sp)
	if ra.Conflict == rb.Conflict {
		res = ra
		if ra.Conflict {
			res.T = min(ra.T, rb.T)
		}
		return res, false
	}
	approver, rej := d.A, rb
	if ra.Conflict {
		approver, rej = d.B, ra
	}
	if approver.FirstConflict(a, b, from, to, Tighten(a, b, sp, d.Tol, d.TimeTol)).Conflict {
		d.Marginal++
		return rej, false
	}
	d.Hard++
	if d.OnHard != nil {
		d.OnHard(a, b, from, to, ra, rb)
	}
	return rej, true
}

// Arrangement returns a Verifier arrangement by the name that a record gives
// it, built from the implementations in this package.
//
//	"dual"    the exact and the sampled implementation; both must approve
//	"single"  the exact implementation alone
//
// The step of the sampled implementation is one second.
func Arrangement(name string) (*Dual, error) {
	switch name {
	case "dual":
		return &Dual{A: AnalyticChecker{}, B: SampledChecker{Step: fx.Sec}, Tol: Tol, TimeTol: TimeTol}, nil
	case "single":
		return &Dual{A: AnalyticChecker{}}, nil
	}
	return nil, fmt.Errorf("unknown verifier arrangement %q", name)
}
