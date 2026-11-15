// Package conformance holds the conformance suite for Verifier
// implementations: the data format of a test vector, the reference that
// decides what each vector must answer, and the rules that judge an answer.
//
// The suite is for implementations in any language. Vectors are JSON. An
// implementation outside this module is run through a line protocol; see Run.
//
// Every number in a vector is an integer, in the units of package fx:
// micrometres, microseconds, micrometres per second.
package conformance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/traj"
)

const (
	SpecVersion = "0.2"
	Units       = "micrometres, microseconds, micrometres per second"

	// The tolerance. A Verifier may be conservative. It must approve a pair
	// that is still separated when the standard is tightened by Tol and
	// TimeTol.
	Tol     = kernel.Tol     // µm
	TimeTol = kernel.TimeTol // µs
)

// Classes of expected answer.
const (
	Conflict = "conflict" // the Verifier must report a conflict
	Clear    = "clear"    // the Verifier must report separation
	Either   = "either"   // inside the tolerance: both answers conform
)

// Leg is a straight horizontal segment at constant ground velocity.
type Leg struct {
	T0 int64    `json:"t0"` // µs
	T1 int64    `json:"t1"` // µs
	P0 [2]int64 `json:"p0"` // µm
	V  [2]int64 `json:"v"`  // µm/s
}

// VSeg is a vertical segment: level if a0 equals a1, otherwise a transition.
type VSeg struct {
	T0 int64 `json:"t0"` // µs
	T1 int64 `json:"t1"` // µs
	A0 int64 `json:"a0"` // µm
	A1 int64 `json:"a1"` // µm
}

// Tube is the protected envelope of one trajectory.
type Tube struct {
	Legs   []Leg  `json:"legs"`
	VSegs  []VSeg `json:"vsegs"`
	Anchor int64  `json:"anchor"` // µs
	R0     int64  `json:"r0"`     // µm
	K      int64  `json:"k"`      // µm/s
	VU0    int64  `json:"vu0"`    // µm
	VURate int64  `json:"vuRate"` // µm/s
	VMin   int64  `json:"vmin"`   // µm/s
}

// Sep is the separation standard of one check.
type Sep struct {
	Sh        int64 `json:"sh"`        // µm
	SvTrue    int64 `json:"svTrue"`    // µm
	VertSlack int64 `json:"vertSlack"` // µs
}

// Expect is what a conforming Verifier may answer.
type Expect struct {
	Class string `json:"class"`
	// A reported conflict time must be from FirstMin to FirstMax (µs).
	// Absent for class "clear".
	FirstMin *int64 `json:"firstMin,omitempty"`
	FirstMax *int64 `json:"firstMax,omitempty"`
	// For information: the first conflict time under the standard as given
	// (µs). Absent when there is no conflict under that standard.
	ExactFirst *int64 `json:"exactFirst,omitempty"`
}

// Case is one test vector.
type Case struct {
	ID     string `json:"id"`
	Group  string `json:"group"`
	Note   string `json:"note,omitempty"`
	A      Tube   `json:"a"`
	B      Tube   `json:"b"`
	From   int64  `json:"from"` // µs
	To     int64  `json:"to"`   // µs
	Sep    Sep    `json:"sep"`
	Expect Expect `json:"expect"`
}

// Tolerance records the tolerance that the expectations were built with.
type Tolerance struct {
	Distance int64 `json:"distance"` // µm
	Time     int64 `json:"time"`     // µs
}

// Suite is a set of vectors.
type Suite struct {
	Name      string    `json:"suite"`
	Spec      string    `json:"spec"`
	Units     string    `json:"units"`
	Tolerance Tolerance `json:"tolerance"`
	Cases     []Case    `json:"cases"`
}

// Answer is what a Verifier says about one case. T is the first conflict
// time (µs). It has meaning only when Conflict is true.
type Answer struct {
	ID       string `json:"id"`
	Conflict bool   `json:"conflict"`
	T        int64  `json:"t"`
}

// Kernel converts a vector tube to the kernel's form. A tube from a vector is
// untrusted: Kernel returns an error if the tube is not valid.
func (t Tube) Kernel(id uint64) (*kernel.Tube, error) {
	tr := &traj.Trajectory{}
	for _, l := range t.Legs {
		tr.Legs = append(tr.Legs, traj.Leg{T0: l.T0, T1: l.T1, P0: fx.Vec{X: l.P0[0], Y: l.P0[1]}, V: fx.Vec{X: l.V[0], Y: l.V[1]}})
	}
	for _, s := range t.VSegs {
		tr.VSegs = append(tr.VSegs, traj.VSeg{T0: s.T0, T1: s.T1, A0: s.A0, A1: s.A1})
	}
	k := &kernel.Tube{
		ID: id, Flight: kernel.FlightID(id), Traj: tr,
		Anchor: t.Anchor, R0: t.R0, K: t.K, VU0: t.VU0, VURate: t.VURate, VMin: t.VMin,
	}
	if err := k.Validate(); err != nil {
		return nil, err
	}
	return k, nil
}

// FromKernel converts a kernel tube to the vector form.
func FromKernel(k *kernel.Tube) Tube {
	t := Tube{Anchor: k.Anchor, R0: k.R0, K: k.K, VU0: k.VU0, VURate: k.VURate, VMin: k.VMin}
	for _, l := range k.Traj.Legs {
		t.Legs = append(t.Legs, Leg{T0: l.T0, T1: l.T1, P0: [2]int64{l.P0.X, l.P0.Y}, V: [2]int64{l.V.X, l.V.Y}})
	}
	for _, s := range k.Traj.VSegs {
		t.VSegs = append(t.VSegs, VSeg{T0: s.T0, T1: s.T1, A0: s.A0, A1: s.A1})
	}
	return t
}

// maxSlack is the largest valid vertical slack (µs): specification, section 4.
const maxSlack = 1 << 40

// validate reports the first value of the standard that is outside its
// limits: sh and svTrue are lengths, and vertSlack is from 0 to maxSlack.
func (s Sep) validate() error {
	switch {
	case s.Sh < 0 || s.Sh > fx.MaxLen:
		return errors.New("sh outside the valid lengths")
	case s.SvTrue < 0 || s.SvTrue > fx.MaxLen:
		return errors.New("svTrue outside the valid lengths")
	case s.VertSlack < 0 || s.VertSlack > maxSlack:
		return errors.New("vertSlack outside the valid slacks")
	}
	return nil
}

// params returns the standard in the kernel's form.
func (s Sep) params() kernel.SepParams {
	return kernel.SepParams{Sh: s.Sh, SvTrue: s.SvTrue, VertSlack: s.VertSlack}
}

// kernel returns the two tubes and the standard of c in the kernel's form. A
// vector is untrusted: kernel returns an error if a tube or the standard is
// not valid.
func (c *Case) kernel() (a, b *kernel.Tube, sp kernel.SepParams, err error) {
	if a, err = c.A.Kernel(1); err != nil {
		return nil, nil, sp, fmt.Errorf("tube a: %w", err)
	}
	if b, err = c.B.Kernel(2); err != nil {
		return nil, nil, sp, fmt.Errorf("tube b: %w", err)
	}
	if err = c.Sep.validate(); err != nil {
		return nil, nil, sp, fmt.Errorf("standard: %w", err)
	}
	return a, b, c.Sep.params(), nil
}

// tightened returns the standard tightened by the tolerance, for two valid
// tubes (specification, section 5.2): Sh wider by Tol, VertSlack wider by
// TimeTol, and SvTrue wider by the growth of both vertical uncertainties in
// TimeTol, rounded up. A test holds it to kernel.Tighten.
func (s Sep) tightened(a, b *Tube) Sep {
	// Both rates are at most fx.MaxSpeed: the quotient fits.
	grow, _ := fx.Mul(a.VURate+b.VURate, TimeTol).DivCeil(fx.M)
	return Sep{Sh: s.Sh + Tol, SvTrue: s.SvTrue + grow, VertSlack: s.VertSlack + TimeTol}
}

// Reference decides what a conforming Verifier may answer for c.
//
// It runs the exact implementation twice: with the standard as given, and
// with the standard tightened by the tolerance.
//
//   - In conflict under the standard as given: class "conflict". A reported
//     time must be from the first conflict time under the tightened standard
//     to the first conflict time under the standard as given.
//   - Separated under the tightened standard: class "clear".
//   - Otherwise: class "either". A reported time must be from the first
//     conflict time under the tightened standard to the end of the interval.
//
// Reference returns an error if a tube or the standard is not valid, if the
// two runs are not ordered, or if Confirm does not find the same by
// enumeration. The last two are defects in the exact implementation or in
// the enumeration.
func Reference(c *Case) (Expect, error) {
	a, b, sp, err := c.kernel()
	if err != nil {
		return Expect{}, fmt.Errorf("%s: %w", c.ID, err)
	}
	var ac kernel.AnalyticChecker
	exact := ac.FirstConflict(a, b, c.From, c.To, sp)
	tight := ac.FirstConflict(a, b, c.From, c.To, c.Sep.tightened(&c.A, &c.B).params())
	if exact.Conflict && !tight.Conflict {
		return Expect{}, fmt.Errorf("%s: in conflict under the standard, and separated under the tightened standard", c.ID)
	}
	if exact.Conflict && tight.T > exact.T {
		return Expect{}, fmt.Errorf("%s: first conflict at %d under the standard, and later, at %d, under the tightened standard", c.ID, exact.T, tight.T)
	}

	var e Expect
	ptr := func(v int64) *int64 { return &v }
	switch {
	case exact.Conflict:
		e = Expect{Class: Conflict, FirstMin: ptr(tight.T), FirstMax: ptr(exact.T), ExactFirst: ptr(exact.T)}
	case !tight.Conflict:
		e = Expect{Class: Clear}
	default:
		_, hi := c.interval()
		e = Expect{Class: Either, FirstMin: ptr(tight.T), FirstMax: ptr(hi)}
	}
	confirmed := *c
	confirmed.Expect = e
	return e, Confirm(&confirmed)
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
	if ans.T < *e.FirstMin || ans.T > *e.FirstMax {
		return fmt.Errorf("conflict time %d is outside %d to %d", ans.T, *e.FirstMin, *e.FirstMax)
	}
	return nil
}

// Load reads a suite from a file. A file is untrusted: Load returns an error
// if a tube or a standard of any vector is not valid. So a Verifier, in this
// module or outside it, gets only valid vectors from a loaded suite.
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
	for i := range s.Cases {
		if _, _, _, err := s.Cases[i].kernel(); err != nil {
			return nil, fmt.Errorf("%s: %s: %w", path, s.Cases[i].ID, err)
		}
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
