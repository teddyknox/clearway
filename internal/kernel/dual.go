package kernel

import "math"

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
	Tol     float64     // nm
	TimeTol float64     // s

	Marginal uint64
	Hard     uint64

	// OnHard, if set, receives each hard disagreement. An operational system
	// raises an alarm here and keeps the inputs for analysis.
	OnHard func(a, b *Tube, from, to float64, ra, rb Result)
}

// Check returns the merged result. hard is true on a hard disagreement.
func (d *Dual) Check(a, b *Tube, from, to float64, sp SepParams) (res Result, hard bool) {
	ra := d.A.FirstConflict(a, b, from, to, sp)
	if d.B == nil {
		return ra, false
	}
	rb := d.B.FirstConflict(a, b, from, to, sp)
	if ra.Conflict == rb.Conflict {
		res = ra
		res.Margin = math.Min(ra.Margin, rb.Margin)
		if ra.Conflict {
			res.T = math.Min(ra.T, rb.T)
		}
		return res, false
	}
	approver, rej := d.A, rb
	if ra.Conflict {
		approver, rej = d.B, ra
	}
	tight := sp
	tight.Sh += d.Tol
	tight.VertSlack += d.TimeTol
	if approver.FirstConflict(a, b, from, to, tight).Conflict {
		d.Marginal++
		return rej, false
	}
	d.Hard++
	if d.OnHard != nil {
		d.OnHard(a, b, from, to, ra, rb)
	}
	return rej, true
}
