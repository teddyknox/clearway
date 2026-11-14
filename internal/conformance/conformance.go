// Package conformance holds the conformance suite for Verifier
// implementations: the data format of a test vector, the reference that
// decides what each vector must answer, and the rules that judge an answer.
//
// The suite is for implementations in any language. Vectors are JSON. An
// implementation outside this module is run through a line protocol; see Run.
//
// Units: nautical miles, feet, seconds.
package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/teddyknox/clearway/internal/geom"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/traj"
)

// The tolerances of specification 0.1.
const (
	SpecVersion = "0.1"

	// A Verifier may be conservative. It must approve a pair that is still
	// separated when the standard is tightened by Tol and TimeTol.
	Tol     = 0.75 // nm
	TimeTol = 1.0  // s

	// A Verifier must reject a pair that is still in conflict when the
	// standard is relaxed by the guard. The guard keeps the suite from
	// depending on the last digits of anyone's arithmetic.
	GuardD = 0.02 // nm
	GuardT = 0.1  // s
)

// Classes of expected answer.
const (
	Conflict = "conflict" // the Verifier must report a conflict
	Clear    = "clear"    // the Verifier must report separation
	Either   = "either"   // inside the tolerance band: both answers conform
)

// Leg is a straight horizontal segment at constant ground velocity.
type Leg struct {
	T0 float64    `json:"t0"`
	T1 float64    `json:"t1"`
	P0 [2]float64 `json:"p0"`
	V  [2]float64 `json:"v"`
}

// VSeg is a vertical segment: level if a0 equals a1, otherwise a transition.
type VSeg struct {
	T0 float64 `json:"t0"`
	T1 float64 `json:"t1"`
	A0 float64 `json:"a0"`
	A1 float64 `json:"a1"`
}

// Tube is the protected envelope of one trajectory.
type Tube struct {
	Legs   []Leg   `json:"legs"`
	VSegs  []VSeg  `json:"vsegs"`
	Anchor float64 `json:"anchor"`
	R0     float64 `json:"r0"`
	K      float64 `json:"k"`
	VU0    float64 `json:"vu0"`
	VURate float64 `json:"vuRate"`
	VMin   float64 `json:"vmin"`
}

// Sep is the separation standard of one check.
type Sep struct {
	Sh        float64 `json:"sh"`
	SvTrue    float64 `json:"svTrue"`
	VertSlack float64 `json:"vertSlack"`
}

// Expect is what a conforming Verifier may answer.
type Expect struct {
	Class string `json:"class"`
	// A reported conflict time must lie in [FirstMin, FirstMax]. Absent for
	// class "clear".
	FirstMin *float64 `json:"firstMin,omitempty"`
	FirstMax *float64 `json:"firstMax,omitempty"`
	// For information: the exact first conflict time and the exact least
	// margin under the standard as given. The margin is absent when the pair
	// is vertically separated throughout.
	ExactFirst  *float64 `json:"exactFirst,omitempty"`
	ExactMargin *float64 `json:"exactMargin,omitempty"`
}

// Case is one test vector.
type Case struct {
	ID     string  `json:"id"`
	Group  string  `json:"group"`
	Note   string  `json:"note,omitempty"`
	A      Tube    `json:"a"`
	B      Tube    `json:"b"`
	From   float64 `json:"from"`
	To     float64 `json:"to"`
	Sep    Sep     `json:"sep"`
	Expect Expect  `json:"expect"`
}

// Tolerance records the tolerances that the expectations were built with.
type Tolerance struct {
	Distance      float64 `json:"distance"`
	Time          float64 `json:"time"`
	GuardDistance float64 `json:"guardDistance"`
	GuardTime     float64 `json:"guardTime"`
}

// Suite is a set of vectors.
type Suite struct {
	Name      string    `json:"suite"`
	Spec      string    `json:"spec"`
	Units     string    `json:"units"`
	Tolerance Tolerance `json:"tolerance"`
	Cases     []Case    `json:"cases"`
}

// Answer is what a Verifier says about one case.
type Answer struct {
	ID       string  `json:"id"`
	Conflict bool    `json:"conflict"`
	T        float64 `json:"t"`
}

// Kernel converts a vector tube to the kernel's form.
func (t Tube) Kernel(id uint64) *kernel.Tube {
	tr := &traj.Trajectory{}
	for _, l := range t.Legs {
		tr.Legs = append(tr.Legs, traj.Leg{T0: l.T0, T1: l.T1, P0: geom.Vec2{X: l.P0[0], Y: l.P0[1]}, V: geom.Vec2{X: l.V[0], Y: l.V[1]}})
	}
	for _, s := range t.VSegs {
		tr.VSegs = append(tr.VSegs, traj.VSeg{T0: s.T0, T1: s.T1, A0: s.A0, A1: s.A1})
	}
	return &kernel.Tube{
		ID: id, Flight: kernel.FlightID(id), Traj: tr,
		Anchor: t.Anchor, R0: t.R0, K: t.K, VU0: t.VU0, VURate: t.VURate, VMin: t.VMin,
	}
}

// FromKernel converts a kernel tube to the vector form.
func FromKernel(k *kernel.Tube) Tube {
	t := Tube{Anchor: k.Anchor, R0: k.R0, K: k.K, VU0: k.VU0, VURate: k.VURate, VMin: k.VMin}
	for _, l := range k.Traj.Legs {
		t.Legs = append(t.Legs, Leg{T0: l.T0, T1: l.T1, P0: [2]float64{l.P0.X, l.P0.Y}, V: [2]float64{l.V.X, l.V.Y}})
	}
	for _, s := range k.Traj.VSegs {
		t.VSegs = append(t.VSegs, VSeg{T0: s.T0, T1: s.T1, A0: s.A0, A1: s.A1})
	}
	return t
}

func (s Sep) params() kernel.SepParams {
	return kernel.SepParams{Sh: s.Sh, SvTrue: s.SvTrue, VertSlack: s.VertSlack}
}

// span is the interval that a check covers: inside [from, to], inside both
// trajectories, and not before either anchor.
func span(a, b *kernel.Tube, from, to float64) (lo, hi float64) {
	lo = math.Max(math.Max(from, a.Start()), math.Max(b.Start(), math.Max(a.Anchor, b.Anchor)))
	hi = math.Min(to, math.Min(a.End(), b.End()))
	return lo, hi
}

// Dense states the separation rule by enumeration: it tests the predicate at
// every multiple of dt. It shares no geometry with either Verifier
// implementation. It returns whether any sample is in conflict, the first
// such sample, and the least margin seen.
func Dense(a, b *kernel.Tube, from, to float64, sp kernel.SepParams, dt float64) (conflict bool, first, margin float64) {
	margin = math.Inf(1)
	lo, hi := span(a, b, from, to)
	if hi <= lo {
		return false, 0, margin
	}
	n := int(math.Ceil((hi - lo) / dt))
	for i := 0; i <= n; i++ {
		t := math.Min(hi, lo+float64(i)*dt)
		sa, sb := a.Sigma(t)+sp.VertSlack, b.Sigma(t)+sp.VertSlack
		aLo, aHi := a.Traj.BandOver(t-sa, t+sa)
		bLo, bHi := b.Traj.BandOver(t-sb, t+sb)
		u := a.VU(t) + b.VU(t)
		if aLo-bHi-u >= sp.SvTrue || bLo-aHi-u >= sp.SvTrue {
			continue
		}
		m := a.Traj.PosAt(t).Dist(b.Traj.PosAt(t)) - (sp.Sh + a.Radius(t) + b.Radius(t))
		margin = math.Min(margin, m)
		if m < 0 && !conflict {
			conflict, first = true, t
		}
	}
	return conflict, first, margin
}

// Reference decides what a conforming Verifier may answer for c.
//
// It runs the exact check three times: with the standard as given, with the
// standard relaxed by the guard, and with the standard tightened by the
// tolerance.
//
//   - In conflict under the relaxed standard: class "conflict".
//   - Separated under the tightened standard: class "clear".
//   - Otherwise: class "either".
//
// It then confirms the class with Dense at step dt. An error means that the
// exact check and the enumeration disagree, which is a defect in one of them.
func Reference(c *Case, dt float64) (Expect, error) {
	a, b := c.A.Kernel(1), c.B.Kernel(2)
	base := c.Sep.params()
	// A shift in time also moves a vertical uncertainty that grows with
	// time, so the vertical distance changes with the two growth rates.
	growth := a.VURate + b.VURate
	relaxedP, tightP := base, base
	relaxedP.Sh -= GuardD
	relaxedP.VertSlack -= GuardT
	relaxedP.SvTrue -= growth * GuardT
	tightP.Sh += Tol
	tightP.VertSlack += TimeTol
	tightP.SvTrue += growth * TimeTol

	var ac kernel.AnalyticChecker
	exact := ac.FirstConflict(a, b, c.From, c.To, base)
	relaxed := ac.FirstConflict(a, b, c.From, c.To, relaxedP)
	tight := ac.FirstConflict(a, b, c.From, c.To, tightP)
	lo, hi := span(a, b, c.From, c.To)

	if (relaxed.Conflict && !exact.Conflict) || (exact.Conflict && !tight.Conflict) {
		return Expect{}, fmt.Errorf("%s: relaxed, exact, and tightened checks are not ordered: %v %v %v", c.ID, relaxed.Conflict, exact.Conflict, tight.Conflict)
	}
	if exact.Conflict && (tight.T > exact.T+1e-6 || (relaxed.Conflict && exact.T > relaxed.T+1e-6)) {
		return Expect{}, fmt.Errorf("%s: conflict times are not ordered: tightened %.6f, exact %.6f, relaxed %.6f", c.ID, tight.T, exact.T, relaxed.T)
	}

	var e Expect
	ptr := func(v float64) *float64 { return &v }
	switch {
	case relaxed.Conflict:
		e.Class = Conflict
		e.FirstMin, e.FirstMax = ptr(math.Max(lo, tight.T-GuardT)), ptr(relaxed.T+GuardT)
	case !tight.Conflict:
		e.Class = Clear
	default:
		e.Class = Either
		e.FirstMin, e.FirstMax = ptr(math.Max(lo, tight.T-GuardT)), ptr(hi)
	}
	if exact.Conflict {
		e.ExactFirst = ptr(exact.T)
	}
	if !math.IsInf(exact.Margin, 1) {
		e.ExactMargin = ptr(exact.Margin)
	}

	found, _, _ := Dense(a, b, c.From, c.To, base, dt)
	if e.Class == Conflict && !found {
		return e, fmt.Errorf("%s: the exact check reports a conflict and the enumeration finds none", c.ID)
	}
	if e.Class == Clear && found {
		return e, fmt.Errorf("%s: the exact check reports separation and the enumeration finds a conflict", c.ID)
	}
	return e, nil
}

// Judge returns nil if the answer conforms, or the reason why it does not.
func Judge(c *Case, ans Answer) error {
	e := c.Expect
	switch e.Class {
	case Conflict:
		if !ans.Conflict {
			return errors.New("approved a pair that is in conflict")
		}
	case Clear:
		if ans.Conflict {
			return errors.New("rejected a pair that is separated beyond the tolerance")
		}
		return nil
	case Either:
		if !ans.Conflict {
			return nil
		}
	default:
		return fmt.Errorf("unknown class %q", e.Class)
	}
	if math.IsNaN(ans.T) || ans.T < *e.FirstMin || ans.T > *e.FirstMax {
		return fmt.Errorf("conflict time %.3f is outside [%.3f, %.3f]", ans.T, *e.FirstMin, *e.FirstMax)
	}
	return nil
}

// Load reads a suite from a file.
func Load(path string) (*Suite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Suite
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(s.Cases) == 0 {
		return nil, fmt.Errorf("%s: no cases", path)
	}
	return &s, nil
}

// Write writes the suite as JSON, with one case on each line so that a change
// to one vector is a change to one line.
func (s *Suite) Write(w io.Writer) error {
	head := *s
	head.Cases = nil
	meta, err := json.Marshal(head)
	if err != nil {
		return err
	}
	meta = bytes.TrimSuffix(meta, []byte(`,"cases":null}`))
	if _, err := fmt.Fprintf(w, "%s,\"cases\":[\n", meta); err != nil {
		return err
	}
	for i, c := range s.Cases {
		line, err := json.Marshal(c)
		if err != nil {
			return err
		}
		sep := ","
		if i == len(s.Cases)-1 {
			sep = ""
		}
		if _, err := fmt.Fprintf(w, "%s%s\n", line, sep); err != nil {
			return err
		}
	}
	_, err = io.WriteString(w, "]}\n")
	return err
}
