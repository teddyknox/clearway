package kernel

import (
	"errors"
	"fmt"
	"sort"

	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/traj"
)

// Authority is the one actor that may issue clearances to a flight.
type Authority uint8

const (
	Automation Authority = iota
	Human
)

// PendingState is the delivery state of a pending amendment.
type PendingState uint8

const (
	PendingNone     PendingState = iota
	PendingAwaiting              // the Gateway has not reported yet
	PendingUnknown               // delivery failed: the aircraft may fly either contract
)

// Params are the design values of one cell, in the units of package fx.
// DefaultParams returns the initial values used in the design document.
type Params struct {
	Sh   int64 // horizontal separation standard (µm)
	Sv   int64 // vertical separation standard between assigned levels (µm)
	VTol int64 // altitude-keeping tolerance (µm)

	Horizon         int64 // verification horizon (µs)
	CommitHorizon   int64 // an amendment must be conflict-free this far ahead (µs)
	IntruderHorizon int64 // length of an intruder tube (µs)
	ExtendSlack     int64 // a verified window is extended when it is this short of the horizon (µs)

	W  int64 // lateral navigation tolerance (µm)
	E0 int64 // surveillance and along-track tolerance at the anchor (µm)
	Bh int64 // detection buffer: distance an aircraft can stray before the Monitor reacts (µm)
	K  int64 // growth of along-track uncertainty (µm/s)

	MinLead    int64 // least time from a proposal to its divergence point (µs)
	CellRadius int64 // the cell is a disc around the origin (µm)

	IntruderR0    int64 // radius of an intruder tube at the track time (µm)
	IntruderK     int64 // growth of an intruder tube (µm/s)
	IntruderVU    int64 // vertical uncertainty of a climbing or descending intruder (µm)
	RecontractTol int64 // a re-contract must start this close to the extrapolated track (µm)

	IntentCheck     bool  // compare broadcast selected altitude with the contract
	IntentLookahead int64 // a selected altitude may anticipate a transition by this much (µs)
}

// DefaultParams returns the initial design values.
func DefaultParams() Params {
	return Params{
		Sh: 5 * fx.NM, Sv: 1000 * fx.FT, VTol: 200 * fx.FT,
		Horizon: 1200 * fx.Sec, CommitHorizon: 900 * fx.Sec, IntruderHorizon: 300 * fx.Sec, ExtendSlack: 30 * fx.Sec,
		W: fx.NM / 2, E0: fx.NM / 5, Bh: fx.NM / 2, K: 10 * fx.NM / 3600,
		MinLead: 120 * fx.Sec, CellRadius: 150 * fx.NM,
		IntruderR0: 3 * fx.NM / 2, IntruderK: 2 * fx.NM / 60, IntruderVU: 300 * fx.FT, RecontractTol: fx.NM / 2,
		IntentCheck: true, IntentLookahead: 120 * fx.Sec,
	}
}

// The limits of the parameters. With them, no operation of the kernel can
// overflow on valid inputs.
const (
	maxHorizon         = 1 << 40 // µs: about 12 days
	maxIntruderHorizon = 1 << 29 // µs: about 9 minutes; an intruder tube must stay inside fx.MaxCoord
)

// Validate reports the first value that no cell can run with. A caller that
// reads parameters from a file must call it. New does not.
func (p Params) Validate() error {
	type named struct {
		name  string
		v, hi int64
	}
	for _, x := range []named{
		{"Sh", p.Sh, fx.MaxLen}, {"Sv", p.Sv, fx.MaxLen}, {"CellRadius", p.CellRadius, fx.MaxLen},
		{"Horizon", p.Horizon, maxHorizon}, {"CommitHorizon", p.CommitHorizon, maxHorizon},
		{"IntruderHorizon", p.IntruderHorizon, maxIntruderHorizon},
	} {
		if x.v <= 0 || x.v > x.hi {
			return fmt.Errorf("parameter %s must be from 1 to %d", x.name, x.hi)
		}
	}
	for _, x := range []named{
		{"VTol", p.VTol, fx.MaxLen}, {"W", p.W, fx.MaxLen}, {"E0", p.E0, fx.MaxLen}, {"Bh", p.Bh, fx.MaxLen},
		{"IntruderR0", p.IntruderR0, fx.MaxLen}, {"IntruderVU", p.IntruderVU, fx.MaxLen},
		{"RecontractTol", p.RecontractTol, fx.MaxLen},
		{"K", p.K, fx.MaxSpeed}, {"IntruderK", p.IntruderK, fx.MaxSpeed},
		{"ExtendSlack", p.ExtendSlack, maxHorizon}, {"MinLead", p.MinLead, maxHorizon},
		{"IntentLookahead", p.IntentLookahead, maxHorizon},
	} {
		if x.v < 0 || x.v > x.hi {
			return fmt.Errorf("parameter %s must be from 0 to %d", x.name, x.hi)
		}
	}
	if p.SvTrue() <= 0 {
		return errors.New("parameter Sv must be more than twice VTol")
	}
	if p.CommitHorizon > p.Horizon {
		return errors.New("parameter CommitHorizon must not exceed Horizon")
	}
	if p.BaseR0() > fx.MaxLen {
		return errors.New("parameters W, E0, and Bh are too large together")
	}
	return nil
}

// SvTrue is the least vertical distance between two true altitudes that the
// standard assures: two aircraft at adjacent levels, each off by VTol.
func (p Params) SvTrue() int64 { return p.Sv - 2*p.VTol }

// BaseR0 is the protected radius of a freshly anchored contract.
func (p Params) BaseR0() int64 { return p.W + p.E0 + p.Bh }

// Track is one surveillance report from Fusion.
type Track struct {
	T      int64  // µs
	Pos    fx.Vec // µm
	Vel    fx.Vec // µm/s
	Alt    int64  // µm
	VRate  int64  // µm/s
	HasSel bool   // the aircraft broadcasts a selected altitude
	SelAlt int64  // the selected altitude (µm); zero if HasSel is false
}

// Valid reports whether every value of the track is inside the limits of
// package fx. The kernel ignores a track that is not valid.
func (tr Track) Valid() bool {
	in := func(limit int64, vals ...int64) bool {
		for _, v := range vals {
			if v < -limit || v > limit {
				return false
			}
		}
		return true
	}
	return in(maxClock, tr.T) && in(fx.MaxTrack, tr.Pos.X, tr.Pos.Y, tr.Alt, tr.SelAlt) &&
		in(fx.MaxSpeed, tr.Vel.X, tr.Vel.Y, tr.VRate) && (tr.HasSel || tr.SelAlt == 0)
}

// maxClock is the latest time of the kernel's clock and of a track (µs). It
// leaves room for a horizon below fx.MaxTime.
const maxClock = fx.MaxTime / 2

// MaxClock is the latest time of the kernel's clock (µs): Advance ignores a
// later time.
const MaxClock = maxClock

// Contract is a cleared trajectory with its tube.
type Contract struct {
	Tube
	DivergeAt  int64 // time at which an amendment leaves the previous contract (µs)
	Recontract bool  // issued to an intruder; starts from its extrapolated track
}

// Flight is the Store's record of one flight.
type Flight struct {
	ID        FlightID
	Authority Authority
	Active    *Contract
	Pending   *Contract
	PState    PendingState
	Intruder  bool
	ITube     *Tube
	Track     Track
	HasTrack  bool
}

// RejectReason says why the Verifier did not commit a proposal.
type RejectReason uint8

const (
	RejNone RejectReason = iota
	RejUnknownFlight
	RejDuplicate
	RejAuthority // the proposer does not hold authority for the flight
	RejIntruder  // the flight has no contract to amend, or is not an intruder
	RejBusy      // an amendment is already pending
	RejMalformed // invalid trajectory, or not continuous with the active contract
	RejLead      // the divergence point is too close to deliver the clearance
	RejVertical  // the divergence point lies inside a vertical transition
	RejOpen      // I3: the contract does not end in a safe end state
	RejConflict  // I2 or I4: not separated from another tube
	RejDisagree  // the two Verifier implementations disagree beyond tolerance
	NumReasons
)

var reasonNames = [NumReasons]string{
	"none", "unknown-flight", "duplicate", "authority", "intruder", "busy",
	"malformed", "lead", "vertical", "open", "conflict", "disagree",
}

func (r RejectReason) String() string { return reasonNames[r] }

// Verdict is the Verifier's answer to one proposal.
type Verdict struct {
	OK       bool
	Reason   RejectReason
	With     FlightID // the other flight, for RejConflict and RejDisagree
	At       int64    // time of the conflict (µs)
	Contract *Contract
}

// ExitCause says why the Monitor marked a flight as an intruder.
type ExitCause uint8

const (
	ExitHorizontal ExitCause = iota
	ExitVertical
	ExitIntent
	ExitExternal
	numCauses
)

// Conflict is a pair of flights that the Verifier cannot show to be separated
// from time T on.
type Conflict struct {
	A, B     FlightID
	T        int64 // µs
	Intruder bool  // B is an intruder; A holds the contract
}

// PairStatus is what the kernel currently claims about two flights.
type PairStatus uint8

const (
	PairAbsent   PairStatus = iota // one of the flights is not in the Store
	PairIntruder                   // at least one flight has no conforming contract
	PairExpired                    // contracts exist, but a verified window has run out
	PairVerified                   // the kernel asserts separation now
)

// Stats counts kernel events.
type Stats struct {
	Proposals        uint64
	Commits          uint64
	Rejects          [NumReasons]uint64
	ReanchorOK       uint64
	ReanchorRejected uint64
	Exits            [numCauses]uint64
	UnknownPromoted  uint64
	UnknownDropped   uint64
	RecontractOK     uint64
	RecontractFailed uint64
	Alarms           uint64 // hard disagreements between Verifier implementations
}

type pairKey struct{ a, b uint64 }

func mkKey(x, y uint64) pairKey {
	if x > y {
		x, y = y, x
	}
	return pairKey{x, y}
}

// Kernel holds the Store of one cell and applies every change in one total
// order (I5). All methods must be called from one goroutine.
type Kernel struct {
	P Params
	D *Dual

	now      int64 // µs
	flights  map[FlightID]*Flight
	order    []FlightID // ascending; fixes iteration order
	nextTube uint64

	// windows holds, for each pair of contract tubes of two flights, the time
	// until which the Verifier has shown them separated (I2).
	windows   map[pairKey]int64
	conflicts []Conflict
	coast     int64

	Stats Stats

	// Rec, if set, receives every input and every verdict (see Recorder).
	Rec Recorder

	scratch []*Tube
}

// New returns an empty kernel.
func New(p Params, d *Dual) *Kernel {
	return &Kernel{
		P: p, D: d,
		flights: map[FlightID]*Flight{},
		windows: map[pairKey]int64{},
		coast:   fx.NoLimit,
	}
}

func (k *Kernel) Now() int64 { return k.now }

// Flight returns the record of id, or nil.
func (k *Kernel) Flight(id FlightID) *Flight { return k.flights[id] }

// IDs returns the flight identifiers in ascending order. The caller must not
// modify the slice.
func (k *Kernel) IDs() []FlightID { return k.order }

// Conflicts returns the predicted conflicts found by the last Tick, soonest
// first.
func (k *Kernel) Conflicts() []Conflict { return k.conflicts }

// Coast returns the coast time found by the last Tick: the shortest time for
// which every pair of contracts stays verified with no further clearance.
// It is fx.NoLimit when no pair limits it.
func (k *Kernel) Coast() int64 { return k.coast }

// Advance moves the kernel clock to t, if t is later than the clock and not
// later than the last valid time of the clock.
func (k *Kernel) Advance(t int64) {
	k.record(Input{Kind: InAdvance, T: t})
	if t > k.now && t <= maxClock {
		k.now = t
	}
}

func (k *Kernel) sep() SepParams { return SepParams{Sh: k.P.Sh, SvTrue: k.P.SvTrue()} }

func (k *Kernel) nextID() uint64 { k.nextTube++; return k.nextTube }

// closed reports whether tr ends at or outside the cell boundary. An end that
// is inside by at most traj.Join counts as on the boundary.
func (k *Kernel) closed(tr *traj.Trajectory) bool {
	end := tr.Pos(tr.End())
	return fx.SqSum(end.X, end.Y).Cmp(fx.Sq((k.P.CellRadius-traj.Join)*fx.M)) >= 0
}

func (k *Kernel) contractTube(id FlightID, tr *traj.Trajectory, anchor, r0 int64) Tube {
	return Tube{Flight: id, Traj: tr, Anchor: anchor, R0: r0, K: k.P.K, VU0: k.P.VTol, VMin: tr.MinSpeed()}
}

// contractTubes appends the contract tubes of f to buf.
func contractTubes(buf []*Tube, f *Flight) []*Tube {
	if f.Active != nil {
		buf = append(buf, &f.Active.Tube)
	}
	if f.Pending != nil {
		buf = append(buf, &f.Pending.Tube)
	}
	return buf
}

func (k *Kernel) insertOrder(id FlightID) {
	i := sort.Search(len(k.order), func(i int) bool { return k.order[i] >= id })
	k.order = append(k.order, 0)
	copy(k.order[i+1:], k.order[i:])
	k.order[i] = id
}

// removeTube forgets every verified window of tb.
func (k *Kernel) removeTube(tb *Tube) {
	for _, id := range k.order {
		g := k.flights[id]
		if g.Active != nil {
			delete(k.windows, mkKey(tb.ID, g.Active.ID))
		}
		if g.Pending != nil {
			delete(k.windows, mkKey(tb.ID, g.Pending.ID))
		}
	}
}

type window struct {
	other uint64
	until int64
}

type verifyOut struct {
	ok     bool
	reason RejectReason
	with   FlightID
	at     int64
	wins   []window
}

// verifyAmend checks a new tube against every protected tube of every other
// flight: active contracts, pending contracts (I4), and intruder tubes.
//
// It approves only if no conflict starts before now + CommitHorizon. For each
// contract tube it returns the time until which the pair is verified.
func (k *Kernel) verifyAmend(n *Tube, self FlightID) verifyOut {
	sp := k.sep()
	to := k.now + k.P.Horizon
	limit := k.now + k.P.CommitHorizon
	out := verifyOut{ok: true}
	for _, id := range k.order {
		if id == self {
			continue
		}
		g := k.flights[id]
		k.scratch = contractTubes(k.scratch[:0], g)
		if g.ITube != nil {
			k.scratch = append(k.scratch, g.ITube)
		}
		for _, x := range k.scratch {
			r, hard := k.D.Check(n, x, k.now, to, sp)
			if hard {
				k.Stats.Alarms++
				return verifyOut{reason: RejDisagree, with: id, at: r.T}
			}
			if x.Intruder {
				if r.Conflict {
					return verifyOut{reason: RejConflict, with: id, at: r.T}
				}
				continue
			}
			until := min(to, n.End(), x.End())
			if r.Conflict {
				if r.T < limit {
					return verifyOut{reason: RejConflict, with: id, at: r.T}
				}
				until = r.T
			}
			out.wins = append(out.wins, window{x.ID, until})
		}
	}
	return out
}

// verifyReanchor checks a re-anchored tube. A re-anchor changes the kernel's
// model of a flight, not the flight's clearance. It is accepted only if no
// verified window becomes shorter than it was for the old tube.
func (k *Kernel) verifyReanchor(n, old *Tube, self FlightID) verifyOut {
	sp := k.sep()
	to := k.now + k.P.Horizon
	out := verifyOut{ok: true}
	for _, id := range k.order {
		if id == self {
			continue
		}
		g := k.flights[id]
		k.scratch = contractTubes(k.scratch[:0], g)
		for _, x := range k.scratch {
			r, hard := k.D.Check(n, x, k.now, to, sp)
			if hard {
				k.Stats.Alarms++
				return verifyOut{reason: RejDisagree, with: id, at: r.T}
			}
			until := min(to, n.End(), x.End())
			if r.Conflict {
				prev, ok := k.windows[mkKey(old.ID, x.ID)]
				if !ok || r.T < prev {
					return verifyOut{reason: RejConflict, with: id, at: r.T}
				}
				until = r.T
			}
			out.wins = append(out.wins, window{x.ID, until})
		}
	}
	return out
}

func (k *Kernel) install(n *Tube, wins []window) {
	for _, w := range wins {
		k.windows[mkKey(n.ID, w.other)] = w.until
	}
}

// reject and accept build every verdict that the kernel returns. Each passes
// the verdict to the Recorder.
func (k *Kernel) reject(reason RejectReason, with FlightID, at int64) Verdict {
	k.Stats.Rejects[reason]++
	return k.verdict(Verdict{Reason: reason, With: with, At: at})
}

func (k *Kernel) accept(c *Contract) Verdict {
	k.Stats.Commits++
	return k.verdict(Verdict{OK: true, Contract: c})
}

func (k *Kernel) verdict(v Verdict) Verdict {
	if k.Rec != nil {
		k.Rec.Verdict(v)
	}
	return v
}

// Admit commits the entry contract of a new flight. The contract may start in
// the future. The flight is active at once: the neighbor that hands the flight
// over has already delivered the clearance.
func (k *Kernel) Admit(id FlightID, tr *traj.Trajectory) Verdict {
	k.record(Input{Kind: InAdmit, Flight: id, Traj: tr})
	k.Stats.Proposals++
	if k.flights[id] != nil {
		return k.reject(RejDuplicate, 0, 0)
	}
	if tr.Validate() != nil || tr.End() <= k.now {
		return k.reject(RejMalformed, 0, 0)
	}
	if !k.closed(tr) {
		return k.reject(RejOpen, 0, 0)
	}
	c := &Contract{Tube: k.contractTube(id, tr, k.now, k.P.BaseR0())}
	out := k.verifyAmend(&c.Tube, id)
	if !out.ok {
		return k.reject(out.reason, out.with, out.at)
	}
	c.ID = k.nextID()
	k.install(&c.Tube, out.wins)
	k.flights[id] = &Flight{ID: id, Authority: Automation, Active: c}
	k.insertOrder(id)
	return k.accept(c)
}

// SetAuthority records a transfer of authority for a flight (I1).
func (k *Kernel) SetAuthority(id FlightID, a Authority) {
	k.record(Input{Kind: InSetAuthority, Flight: id, By: a})
	if f := k.flights[id]; f != nil {
		f.Authority = a
	}
}

// Propose asks the Verifier to commit an amendment.
//
// tail is the new trajectory from the divergence point on. The new contract is
// the active contract up to tail.Start(), followed by tail. The caller must
// hold authority for the flight and must make tail continuous with the active
// contract. On success the amendment is pending, and both the old and the new
// tube stay protected (I4) until Ack, Refuse, or the Monitor settles it.
func (k *Kernel) Propose(id FlightID, tail *traj.Trajectory, by Authority) Verdict {
	k.record(Input{Kind: InPropose, Flight: id, Traj: tail, By: by})
	k.Stats.Proposals++
	f := k.flights[id]
	if f == nil {
		return k.reject(RejUnknownFlight, 0, 0)
	}
	if f.Authority != by {
		return k.reject(RejAuthority, 0, 0)
	}
	if f.Intruder || f.Active == nil {
		return k.reject(RejIntruder, 0, 0)
	}
	if f.PState != PendingNone {
		return k.reject(RejBusy, 0, 0)
	}
	if tail.Validate() != nil {
		return k.reject(RejMalformed, 0, 0)
	}
	act := f.Active
	td := tail.Start()
	if td < k.now+k.P.MinLead {
		return k.reject(RejLead, 0, 0)
	}
	if td <= act.Start() || td >= act.End() || !joins(tail.Legs[0].P0, act.Traj.Pos(td), traj.Join) {
		return k.reject(RejMalformed, 0, 0)
	}
	lo, hi := act.Band(td, 0)
	if lo != hi {
		return k.reject(RejVertical, 0, 0)
	}
	if tail.VSegs[0].A0 != lo {
		return k.reject(RejMalformed, 0, 0)
	}
	if !k.closed(tail) {
		return k.reject(RejOpen, 0, 0)
	}
	tr, err := traj.Splice(act.Traj, td, tail)
	if err != nil {
		return k.reject(RejMalformed, 0, 0)
	}
	// The aircraft may be anywhere inside the old envelope now, so the new
	// contract inherits the old anchor and radius.
	c := &Contract{Tube: k.contractTube(id, tr, act.Anchor, act.R0), DivergeAt: td}
	out := k.verifyAmend(&c.Tube, id)
	if !out.ok {
		return k.reject(out.reason, out.with, out.at)
	}
	c.ID = k.nextID()
	k.install(&c.Tube, out.wins)
	f.Pending, f.PState = c, PendingAwaiting
	return k.accept(c)
}

// ProposeRecontract asks the Verifier to commit a new contract for an
// intruder. tr must start at least MinLead ahead, at the position where the
// extrapolated track will be. The intruder tube stays in force until the
// Monitor sees the aircraft inside the new contract.
func (k *Kernel) ProposeRecontract(id FlightID, tr *traj.Trajectory, by Authority) Verdict {
	k.record(Input{Kind: InRecontract, Flight: id, Traj: tr, By: by})
	k.Stats.Proposals++
	f := k.flights[id]
	if f == nil {
		return k.reject(RejUnknownFlight, 0, 0)
	}
	if f.Authority != by {
		return k.reject(RejAuthority, 0, 0)
	}
	if !f.Intruder || !f.HasTrack {
		return k.reject(RejIntruder, 0, 0)
	}
	if f.PState != PendingNone || f.Active != nil {
		return k.reject(RejBusy, 0, 0)
	}
	if tr.Validate() != nil {
		return k.reject(RejMalformed, 0, 0)
	}
	ts := tr.Start()
	if ts < k.now+k.P.MinLead {
		return k.reject(RejLead, 0, 0)
	}
	if !k.startsAtTrack(tr.Legs[0].P0, f.Track, ts) {
		return k.reject(RejMalformed, 0, 0)
	}
	if !k.closed(tr) {
		return k.reject(RejOpen, 0, 0)
	}
	c := &Contract{Tube: k.contractTube(id, tr, k.now, k.P.BaseR0()), DivergeAt: ts, Recontract: true}
	out := k.verifyAmend(&c.Tube, id)
	if !out.ok {
		return k.reject(out.reason, out.with, out.at)
	}
	c.ID = k.nextID()
	k.install(&c.Tube, out.wins)
	f.Pending, f.PState = c, PendingAwaiting
	return k.accept(c)
}

// Ack records that the aircraft acknowledged the pending clearance. The
// pending contract becomes active, and the old tube is released.
func (k *Kernel) Ack(id FlightID) {
	k.record(Input{Kind: InAck, Flight: id})
	f := k.flights[id]
	if f == nil || f.Pending == nil {
		return
	}
	k.promote(f)
}

func (k *Kernel) promote(f *Flight) {
	old := f.Active
	f.Active, f.Pending, f.PState = f.Pending, nil, PendingNone
	if old != nil {
		k.removeTube(&old.Tube)
	}
}

// Refuse records that the aircraft refused the pending clearance. The active
// contract stays in force.
func (k *Kernel) Refuse(id FlightID) {
	k.record(Input{Kind: InRefuse, Flight: id})
	f := k.flights[id]
	if f == nil || f.Pending == nil {
		return
	}
	k.dropPending(f)
}

func (k *Kernel) dropPending(f *Flight) {
	k.removeTube(&f.Pending.Tube)
	f.Pending, f.PState = nil, PendingNone
}

// DeliveryFailed records that the Gateway got no acknowledgement. The kernel
// does not know which contract the aircraft flies. It keeps both tubes
// protected until the Monitor sees the aircraft leave one of them.
//
// For an intruder the pending re-contract is dropped: the intruder tube
// already protects the aircraft whatever it flies.
func (k *Kernel) DeliveryFailed(id FlightID) {
	k.record(Input{Kind: InFail, Flight: id})
	f := k.flights[id]
	if f == nil || f.Pending == nil {
		return
	}
	if f.Intruder {
		k.dropPending(f)
		return
	}
	f.PState = PendingUnknown
}

// joins reports whether the point p (µm) is within tol (µm) of the position
// at (pm), on each axis.
func joins(p, at fx.Vec, tol int64) bool {
	dx := fx.Mul(p.X, fx.M).Sub(fx.Int(at.X))
	dy := fx.Mul(p.Y, fx.M).Sub(fx.Int(at.Y))
	lim := fx.Int(tol * fx.M)
	return dx.Cmp(lim) <= 0 && dx.Cmp(lim.Neg()) >= 0 && dy.Cmp(lim) <= 0 && dy.Cmp(lim.Neg()) >= 0
}

// startsAtTrack reports whether the point p is within RecontractTol of the
// position that the track reaches at ts, at its present velocity.
func (k *Kernel) startsAtTrack(p fx.Vec, tk Track, ts int64) bool {
	dt := ts - tk.T
	dx := fx.Mul(p.X-tk.Pos.X, fx.M).Sub(fx.Mul(tk.Vel.X, dt))
	dy := fx.Mul(p.Y-tk.Pos.Y, fx.M).Sub(fx.Mul(tk.Vel.Y, dt))
	lim := fx.Int(k.P.RecontractTol * fx.M)
	if dx.Cmp(lim) > 0 || dx.Cmp(lim.Neg()) < 0 || dy.Cmp(lim) > 0 || dy.Cmp(lim.Neg()) < 0 {
		return false
	}
	return fx.SqSum(dx.Int64(), dy.Int64()).Cmp(fx.Sq(k.P.RecontractTol*fx.M)) <= 0
}

// inside reports whether tr lies inside the conformance envelope of tb: the
// protected tube less the detection buffer.
func (k *Kernel) inside(tb *Tube, tr Track) (bool, ExitCause) {
	if tr.T > tb.End() {
		return true, 0
	}
	at := tb.Traj.Pos(tr.T)
	// A track is inside ±2^60 pm and a position inside ±2^61 pm.
	dx, dy := tr.Pos.X*fx.M-at.X, tr.Pos.Y*fx.M-at.Y
	lim := tb.RadiusPM(tr.T).Sub(fx.Mul(k.P.Bh, fx.M))
	switch {
	case lim.Sign() < 0:
		return false, ExitHorizontal
	case lim.Cmp(fx.Int(fx.MaxReq)) <= 0 && fx.SqSum(dx, dy).Cmp(fx.Sq(lim.Int64())) > 0:
		return false, ExitHorizontal
	}
	lo, hi := tb.Band(tr.T, 0)
	if tr.Alt < lo-k.P.VTol || tr.Alt > hi+k.P.VTol {
		return false, ExitVertical
	}
	return true, 0
}

// intentSlack is how far a selected altitude may lie outside the levels of
// the contract (µm).
const intentSlack = 50 * fx.FT

// intentOK reports whether the broadcast selected altitude agrees with tb.
func (k *Kernel) intentOK(tb *Tube, tr Track) bool {
	if !k.P.IntentCheck || !tr.HasSel || tr.T > tb.End() {
		return true
	}
	lo, hi := tb.Band(tr.T, k.P.IntentLookahead)
	return tr.SelAlt >= lo-intentSlack && tr.SelAlt <= hi+intentSlack
}

// InsideAny reports whether tr lies inside the conformance envelope of any
// contract of the flight. The simulator uses it with a true position to
// classify a loss of separation.
func (k *Kernel) InsideAny(id FlightID, tr Track) bool {
	f := k.flights[id]
	if f == nil {
		return false
	}
	if f.Active != nil {
		if ok, _ := k.inside(&f.Active.Tube, tr); ok {
			return true
		}
	}
	if f.Pending != nil {
		if ok, _ := k.inside(&f.Pending.Tube, tr); ok {
			return true
		}
	}
	return false
}

// recontractGrace is how long after the start of its new contract an intruder
// may still be outside it (µs).
const recontractGrace = 30 * fx.Sec

// UpdateTrack is the Conformance Monitor. It compares one surveillance report
// with the contracts of the flight. A report for a flight that is not in the
// Store, or a report that is not valid, has no effect.
//
//   - An aircraft outside every contract it may fly becomes an intruder.
//   - With a pending amendment in state unknown, the first report that lies
//     inside one tube and outside the other settles which contract is flown.
//   - A re-contracted intruder becomes a contract flight again when a report
//     lies inside the new contract.
func (k *Kernel) UpdateTrack(id FlightID, tr Track) {
	k.record(Input{Kind: InTrack, Flight: id, Track: tr})
	f := k.flights[id]
	if f == nil || !tr.Valid() {
		return
	}
	f.Track, f.HasTrack = tr, true
	if f.Intruder {
		if f.Active != nil && tr.T >= f.Active.Start() {
			if ok, _ := k.inside(&f.Active.Tube, tr); ok && k.intentOK(&f.Active.Tube, tr) {
				f.Intruder, f.ITube = false, nil
				k.Stats.RecontractOK++
				return
			}
			if tr.T > f.Active.Start()+recontractGrace {
				k.removeTube(&f.Active.Tube)
				f.Active = nil
				k.Stats.RecontractFailed++
			}
		}
		f.ITube = k.intruderTube(f)
		return
	}
	if f.Active == nil || tr.T < f.Active.Start() {
		return
	}
	inA, cause := k.inside(&f.Active.Tube, tr)
	if f.Pending == nil || tr.T < f.Pending.DivergeAt {
		// Before the divergence point the two tubes are the same.
		if !inA {
			k.markIntruder(f, cause)
		} else if !k.intentOK(&f.Active.Tube, tr) && (f.Pending == nil || !k.intentOK(&f.Pending.Tube, tr)) {
			k.markIntruder(f, ExitIntent)
		}
		return
	}
	inP, _ := k.inside(&f.Pending.Tube, tr)
	switch {
	case inA && inP:
	case inP:
		if f.PState == PendingUnknown {
			k.promote(f)
			k.Stats.UnknownPromoted++
		}
	case inA:
		if f.PState == PendingUnknown {
			k.dropPending(f)
			k.Stats.UnknownDropped++
		}
	default:
		k.markIntruder(f, cause)
	}
}

// MarkIntruder records that a flight no longer follows its contract for a
// reason outside the kernel, for example a Safety Net instruction.
func (k *Kernel) MarkIntruder(id FlightID) {
	k.record(Input{Kind: InMarkIntruder, Flight: id})
	f := k.flights[id]
	if f == nil || f.Intruder {
		return
	}
	k.markIntruder(f, ExitExternal)
}

func (k *Kernel) markIntruder(f *Flight, cause ExitCause) {
	k.Stats.Exits[cause]++
	if f.Pending != nil {
		k.removeTube(&f.Pending.Tube)
	}
	if f.Active != nil {
		k.removeTube(&f.Active.Tube)
	}
	f.Active, f.Pending, f.PState = nil, nil, PendingNone
	f.Intruder = true
	if f.HasTrack {
		f.ITube = k.intruderTube(f)
	}
}

// The altitude rules of an intruder tube (µm, µm/s).
const (
	levelRate  = 5 * fx.FT    // below this vertical rate the aircraft holds a level
	levelSnap  = 150 * fx.FT  // a level this close to a cardinal level counts as that level
	levelGrid  = 1000 * fx.FT // cardinal levels
	selAltGate = 150 * fx.FT  // a selected altitude further than this from the altitude is a target
)

// intruderTube builds a state-based tube from the last track: a straight line
// at the current velocity, with a radius that grows fast. The vertical extent
// runs from the current altitude to the altitude the aircraft is heading for.
//
// The track is valid, and the intruder horizon is at most maxIntruderHorizon.
// So the tube is inside the limits of a valid trajectory.
func (k *Kernel) intruderTube(f *Flight) *Tube {
	tr := f.Track
	end := tr.T + k.P.IntruderHorizon
	lo, hi := tr.Alt, tr.Alt
	vu := k.P.VTol
	switch {
	case tr.HasSel && fx.Abs(tr.SelAlt-tr.Alt) > selAltGate:
		lo, hi = min(tr.Alt, tr.SelAlt), max(tr.Alt, tr.SelAlt)
		vu = k.P.IntruderVU
	case fx.Abs(tr.VRate) < levelRate:
		// Holding a level: snap to it, so that adjacent levels stay usable.
		// The nearest cardinal level; half-way rounds up.
		n, _ := fx.Int(tr.Alt + levelGrid/2).DivFloor(levelGrid)
		if n *= levelGrid; fx.Abs(n-tr.Alt) <= levelSnap {
			lo, hi = n, n
		}
	default:
		climb, _ := fx.Mul(tr.VRate, k.P.IntruderHorizon).DivFloor(fx.M)
		tgt := tr.Alt + climb
		lo, hi = min(tr.Alt, tgt), max(tr.Alt, tgt)
		vu = k.P.IntruderVU
	}
	t := &traj.Trajectory{
		Legs:  []traj.Leg{{T0: tr.T, T1: end, P0: tr.Pos, V: tr.Vel}},
		VSegs: []traj.VSeg{{T0: tr.T, T1: end, A0: lo, A1: hi}},
	}
	return &Tube{
		ID: k.nextID(), Flight: f.ID, Traj: t, Anchor: tr.T,
		R0: k.P.IntruderR0, K: k.P.IntruderK, VU0: vu, VMin: fx.Speed(tr.Vel), Intruder: true,
	}
}

// Reanchored reports one accepted re-anchor.
type Reanchored struct {
	ID       FlightID
	Dt       int64 // the contract times moved by −Dt (µs)
	Old, New *traj.Trajectory
}

// ReanchorAll re-anchors every eligible contract on the latest track.
//
// A contract is eligible if the flight conforms, has no pending amendment,
// and has a track taken at the current time. The new contract has the same
// path. Its times move so that the nominal position matches the along-track
// position of the aircraft, and its radius restarts from the base value.
// The shift is a whole number of microseconds, so the new contract has
// exactly the path of the old one.
func (k *Kernel) ReanchorAll() []Reanchored {
	k.record(Input{Kind: InReanchor})
	var done []Reanchored
	for _, id := range k.order {
		f := k.flights[id]
		if f.Intruder || f.Active == nil || f.PState != PendingNone || !f.HasTrack ||
			f.Track.T != k.now || k.now < f.Active.Start() || k.now >= f.Active.End() {
			continue
		}
		act := f.Active
		leg := act.Traj.LegAt(k.now)
		vsq := leg.V.X*leg.V.X + leg.V.Y*leg.V.Y // at most 2^61
		if vsq == 0 {
			continue
		}
		// The along-track offset of the track from the nominal position, as
		// a time: (offset · v) / |v|², rounded down. In picometres times
		// micrometres per second, over micrometres squared per second
		// squared, it is in microseconds.
		at := act.Traj.Pos(k.now)
		dx, dy := f.Track.Pos.X*fx.M-at.X, f.Track.Pos.Y*fx.M-at.Y
		dt, ok := fx.Mul(dx, leg.V.X).Add(fx.Mul(dy, leg.V.Y)).DivFloor(vsq)
		if !ok || dt < -fx.MaxTime || dt > fx.MaxTime {
			continue
		}
		tr := act.Traj.Shift(-dt)
		if tr.Validate() != nil || k.now < tr.Start() || k.now >= tr.End() {
			// The shift moved the contract outside the valid times, or away
			// from the current time. A leg with almost no speed can do that:
			// a small offset is then a long time. The old contract stays.
			continue
		}
		c := &Contract{Tube: k.contractTube(id, tr, k.now, k.P.BaseR0()), DivergeAt: act.DivergeAt - dt}
		out := k.verifyReanchor(&c.Tube, &act.Tube, id)
		if !out.ok {
			k.Stats.ReanchorRejected++
			continue
		}
		c.ID = k.nextID()
		k.removeTube(&act.Tube)
		k.install(&c.Tube, out.wins)
		f.Active = c
		k.Stats.ReanchorOK++
		done = append(done, Reanchored{ID: id, Dt: dt, Old: act.Traj, New: tr})
	}
	return done
}

// Remove deletes a flight that has left the cell.
func (k *Kernel) Remove(id FlightID) {
	k.record(Input{Kind: InRemove, Flight: id})
	f := k.flights[id]
	if f == nil {
		return
	}
	if f.Pending != nil {
		k.removeTube(&f.Pending.Tube)
	}
	if f.Active != nil {
		k.removeTube(&f.Active.Tube)
	}
	delete(k.flights, id)
	i := sort.Search(len(k.order), func(i int) bool { return k.order[i] >= id })
	k.order = append(k.order[:i], k.order[i+1:]...)
}

// Tick extends every verified window toward the horizon and rebuilds the list
// of predicted conflicts.
//
// A pair whose window cannot be extended has a predicted conflict at the end
// of its window. Each contract tube is also checked against each intruder
// tube, from the current time.
func (k *Kernel) Tick() {
	k.record(Input{Kind: InTick})
	sp := k.sep()
	to := k.now + k.P.Horizon
	k.conflicts = k.conflicts[:0]
	k.coast = fx.NoLimit

	tubes := k.scratch[:0]
	for _, id := range k.order {
		tubes = contractTubes(tubes, k.flights[id])
	}
	for i, a := range tubes {
		for _, b := range tubes[i+1:] {
			if a.Flight == b.Flight {
				continue
			}
			target := min(to, a.End(), b.End())
			key := mkKey(a.ID, b.ID)
			u := k.windows[key]
			if u >= target-k.P.ExtendSlack && u >= k.now {
				continue
			}
			from := max(u, k.now)
			if from >= target {
				continue
			}
			r, hard := k.D.Check(a, b, from, target, sp)
			if hard {
				k.Stats.Alarms++
			}
			if !r.Conflict {
				k.windows[key] = target
				continue
			}
			k.windows[key] = r.T
			k.conflicts = append(k.conflicts, Conflict{A: a.Flight, B: b.Flight, T: r.T})
			k.coast = min(k.coast, r.T-k.now)
		}
	}
	for _, gid := range k.order {
		g := k.flights[gid]
		if g.ITube == nil {
			continue
		}
		for _, a := range tubes {
			if a.Flight == gid {
				continue
			}
			r, hard := k.D.Check(a, g.ITube, k.now, to, sp)
			if hard {
				k.Stats.Alarms++
			}
			if r.Conflict {
				k.conflicts = append(k.conflicts, Conflict{A: a.Flight, B: gid, T: r.T, Intruder: true})
			}
		}
	}
	k.scratch = tubes[:0]

	// One entry for each ordered pair: the earliest, and of two at the same
	// time the one between two contracts. Then by time; entries with the same
	// time stay in order of A, then of B. Each sort is stable and has a total
	// order, so that every implementation of the kernel gives the same list.
	sort.SliceStable(k.conflicts, func(i, j int) bool {
		a, b := k.conflicts[i], k.conflicts[j]
		switch {
		case a.A != b.A:
			return a.A < b.A
		case a.B != b.B:
			return a.B < b.B
		case a.T != b.T:
			return a.T < b.T
		}
		return !a.Intruder && b.Intruder
	})
	out := k.conflicts[:0]
	for i, c := range k.conflicts {
		if i > 0 && c.A == k.conflicts[i-1].A && c.B == k.conflicts[i-1].B {
			continue
		}
		out = append(out, c)
	}
	k.conflicts = out
	sort.SliceStable(k.conflicts, func(i, j int) bool { return k.conflicts[i].T < k.conflicts[j].T })
}

// PairState reports what the kernel claims about two flights at the current
// time.
func (k *Kernel) PairState(a, b FlightID) PairStatus {
	fa, fb := k.flights[a], k.flights[b]
	if fa == nil || fb == nil {
		return PairAbsent
	}
	if fa.Intruder || fb.Intruder || fa.Active == nil || fb.Active == nil {
		return PairIntruder
	}
	var ta, tb [2]*Tube
	na := copy(ta[:], contractTubes(nil, fa))
	nb := copy(tb[:], contractTubes(nil, fb))
	for _, x := range ta[:na] {
		for _, y := range tb[:nb] {
			if k.now < max(x.Start(), y.Start()) || k.now > min(x.End(), y.End()) {
				continue
			}
			if u, ok := k.windows[mkKey(x.ID, y.ID)]; !ok || u < k.now {
				return PairExpired
			}
		}
	}
	return PairVerified
}

// PairWindow returns the earliest end of a verified window between the
// contracts of two flights: the time until which the kernel asserts that the
// two are separated. It returns fx.NoLimit if no window applies.
func (k *Kernel) PairWindow(a, b FlightID) int64 {
	fa, fb := k.flights[a], k.flights[b]
	least := int64(fx.NoLimit)
	if fa == nil || fb == nil {
		return least
	}
	for _, x := range contractTubes(nil, fa) {
		for _, y := range contractTubes(nil, fb) {
			end := min(x.End(), y.End())
			if k.now < max(x.Start(), y.Start()) || k.now > end {
				continue
			}
			// A window that reaches the end of either contract has no limit.
			if u, ok := k.windows[mkKey(x.ID, y.ID)]; !ok {
				return k.now
			} else if u < end {
				least = min(least, u)
			}
		}
	}
	return least
}
