package kernel

import (
	"math"
	"sort"

	"github.com/teddyknox/clearway/internal/geom"
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

// Params are the design values of one cell. DefaultParams returns the initial
// values used in the design document.
type Params struct {
	Sh   float64 // horizontal separation standard (nm)
	Sv   float64 // vertical separation standard between assigned levels (ft)
	VTol float64 // altitude-keeping tolerance (ft)

	Horizon         float64 // verification horizon (s)
	CommitHorizon   float64 // an amendment must be conflict-free this far ahead (s)
	IntruderHorizon float64 // length of an intruder tube (s)
	ExtendSlack     float64 // a verified window is extended when it is this short of the horizon (s)

	W  float64 // lateral navigation tolerance (nm)
	E0 float64 // surveillance and along-track tolerance at the anchor (nm)
	Bh float64 // detection buffer: distance an aircraft can stray before the Monitor reacts (nm)
	K  float64 // growth of along-track uncertainty (nm/s)

	MinLead    float64 // least time from a proposal to its divergence point (s)
	CellRadius float64 // the cell is a disc around the origin (nm)

	IntruderR0    float64 // radius of an intruder tube at the track time (nm)
	IntruderK     float64 // growth of an intruder tube (nm/s)
	IntruderVU    float64 // vertical uncertainty of a climbing or descending intruder (ft)
	RecontractTol float64 // a re-contract must start this close to the extrapolated track (nm)

	IntentCheck     bool    // compare broadcast selected altitude with the contract
	IntentLookahead float64 // a selected altitude may anticipate a transition by this much (s)
}

// DefaultParams returns the initial design values.
func DefaultParams() Params {
	return Params{
		Sh: 5, Sv: 1000, VTol: 200,
		Horizon: 1200, CommitHorizon: 900, IntruderHorizon: 300, ExtendSlack: 30,
		W: 0.5, E0: 0.2, Bh: 0.5, K: 10 * geom.Kt,
		MinLead: 120, CellRadius: 150,
		IntruderR0: 1.5, IntruderK: 2.0 / 60, IntruderVU: 300, RecontractTol: 0.5,
		IntentCheck: true, IntentLookahead: 120,
	}
}

// SvTrue is the least vertical distance between two true altitudes that the
// standard assures: two aircraft at adjacent levels, each off by VTol.
func (p Params) SvTrue() float64 { return p.Sv - 2*p.VTol }

// BaseR0 is the protected radius of a freshly anchored contract.
func (p Params) BaseR0() float64 { return p.W + p.E0 + p.Bh }

// Track is one surveillance report from Fusion.
type Track struct {
	T      float64
	Pos    geom.Vec2
	Vel    geom.Vec2 // nm/s
	Alt    float64   // ft
	VRate  float64   // ft/s
	SelAlt float64   // broadcast selected altitude (ft); NaN if not broadcast
}

// Contract is a cleared trajectory with its tube.
type Contract struct {
	Tube
	DivergeAt  float64 // time at which an amendment leaves the previous contract
	Recontract bool    // issued to an intruder; starts from its extrapolated track
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
	At       float64  // time of the conflict
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
	T        float64
	Intruder bool // B is an intruder; A holds the contract
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

	now      float64
	flights  map[FlightID]*Flight
	order    []FlightID // ascending; fixes iteration order
	nextTube uint64

	// windows holds, for each pair of contract tubes of two flights, the time
	// until which the Verifier has shown them separated (I2).
	windows   map[pairKey]float64
	conflicts []Conflict
	coast     float64

	Stats   Stats
	Record  bool
	Log     []Input
	outHash uint64
	scratch []*Tube
}

// New returns an empty kernel.
func New(p Params, d *Dual) *Kernel {
	return &Kernel{
		P: p, D: d,
		flights: map[FlightID]*Flight{},
		windows: map[pairKey]float64{},
		coast:   math.Inf(1),
		outHash: 14695981039346656037,
	}
}

func (k *Kernel) Now() float64 { return k.now }

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
// It is +Inf when no pair limits it.
func (k *Kernel) Coast() float64 { return k.coast }

// Advance moves the kernel clock to t.
func (k *Kernel) Advance(t float64) {
	k.record(Input{Kind: InAdvance, T: t})
	if t > k.now {
		k.now = t
	}
}

func (k *Kernel) sep() SepParams { return SepParams{Sh: k.P.Sh, SvTrue: k.P.SvTrue()} }

func (k *Kernel) nextID() uint64 { k.nextTube++; return k.nextTube }

func (k *Kernel) closed(tr *traj.Trajectory) bool {
	return tr.PosAt(tr.End()).Norm() >= k.P.CellRadius-1e-6
}

func (k *Kernel) contractTube(id FlightID, tr *traj.Trajectory, anchor, r0 float64) Tube {
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
	until float64
}

type verifyOut struct {
	ok     bool
	reason RejectReason
	with   FlightID
	at     float64
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
			until := math.Min(to, math.Min(n.End(), x.End()))
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
			until := math.Min(to, math.Min(n.End(), x.End()))
			if r.Conflict {
				prev, ok := k.windows[mkKey(old.ID, x.ID)]
				if !ok || r.T < prev-1e-6 {
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

func (k *Kernel) reject(reason RejectReason, id, with FlightID, at float64) Verdict {
	k.Stats.Rejects[reason]++
	k.mix(uint64(reason), uint64(id), uint64(with), math.Float64bits(at))
	return Verdict{Reason: reason, With: with, At: at}
}

func (k *Kernel) accept(id FlightID, c *Contract) Verdict {
	k.Stats.Commits++
	k.mix(0, uint64(id), c.ID, math.Float64bits(c.End()))
	return Verdict{OK: true, Contract: c}
}

// Admit commits the entry contract of a new flight. The contract may start in
// the future. The flight is active at once: the neighbor that hands the flight
// over has already delivered the clearance.
func (k *Kernel) Admit(id FlightID, tr *traj.Trajectory) Verdict {
	k.record(Input{Kind: InAdmit, Flight: id, Traj: tr})
	k.Stats.Proposals++
	if k.flights[id] != nil {
		return k.reject(RejDuplicate, id, 0, 0)
	}
	if tr.Validate() != nil || tr.End() <= k.now {
		return k.reject(RejMalformed, id, 0, 0)
	}
	if !k.closed(tr) {
		return k.reject(RejOpen, id, 0, 0)
	}
	c := &Contract{Tube: k.contractTube(id, tr, k.now, k.P.BaseR0())}
	out := k.verifyAmend(&c.Tube, id)
	if !out.ok {
		return k.reject(out.reason, id, out.with, out.at)
	}
	c.ID = k.nextID()
	k.install(&c.Tube, out.wins)
	k.flights[id] = &Flight{ID: id, Authority: Automation, Active: c}
	k.insertOrder(id)
	return k.accept(id, c)
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
		return k.reject(RejUnknownFlight, id, 0, 0)
	}
	if f.Authority != by {
		return k.reject(RejAuthority, id, 0, 0)
	}
	if f.Intruder || f.Active == nil {
		return k.reject(RejIntruder, id, 0, 0)
	}
	if f.PState != PendingNone {
		return k.reject(RejBusy, id, 0, 0)
	}
	if tail.Validate() != nil {
		return k.reject(RejMalformed, id, 0, 0)
	}
	act := f.Active
	td := tail.Start()
	if td < k.now+k.P.MinLead {
		return k.reject(RejLead, id, 0, 0)
	}
	if td <= act.Start() || td >= act.End() ||
		tail.PosAt(td).Dist(act.Traj.PosAt(td)) > 1e-5 {
		return k.reject(RejMalformed, id, 0, 0)
	}
	sg := act.Sigma(td)
	lo, hi := act.Traj.BandOver(td-sg, td+sg)
	if lo != hi {
		return k.reject(RejVertical, id, 0, 0)
	}
	if tail.VSegs[0].A0 != lo {
		return k.reject(RejMalformed, id, 0, 0)
	}
	if !k.closed(tail) {
		return k.reject(RejOpen, id, 0, 0)
	}
	tr, err := traj.Splice(act.Traj, td, tail)
	if err != nil {
		return k.reject(RejMalformed, id, 0, 0)
	}
	// The aircraft may be anywhere inside the old envelope now, so the new
	// contract inherits the old anchor and radius.
	c := &Contract{Tube: k.contractTube(id, tr, act.Anchor, act.R0), DivergeAt: td}
	out := k.verifyAmend(&c.Tube, id)
	if !out.ok {
		return k.reject(out.reason, id, out.with, out.at)
	}
	c.ID = k.nextID()
	k.install(&c.Tube, out.wins)
	f.Pending, f.PState = c, PendingAwaiting
	return k.accept(id, c)
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
		return k.reject(RejUnknownFlight, id, 0, 0)
	}
	if f.Authority != by {
		return k.reject(RejAuthority, id, 0, 0)
	}
	if !f.Intruder || !f.HasTrack {
		return k.reject(RejIntruder, id, 0, 0)
	}
	if f.PState != PendingNone || f.Active != nil {
		return k.reject(RejBusy, id, 0, 0)
	}
	if tr.Validate() != nil {
		return k.reject(RejMalformed, id, 0, 0)
	}
	ts := tr.Start()
	if ts < k.now+k.P.MinLead {
		return k.reject(RejLead, id, 0, 0)
	}
	want := f.Track.Pos.Add(f.Track.Vel.Scale(ts - f.Track.T))
	if tr.PosAt(ts).Dist(want) > k.P.RecontractTol {
		return k.reject(RejMalformed, id, 0, 0)
	}
	if !k.closed(tr) {
		return k.reject(RejOpen, id, 0, 0)
	}
	c := &Contract{Tube: k.contractTube(id, tr, k.now, k.P.BaseR0()), DivergeAt: ts, Recontract: true}
	out := k.verifyAmend(&c.Tube, id)
	if !out.ok {
		return k.reject(out.reason, id, out.with, out.at)
	}
	c.ID = k.nextID()
	k.install(&c.Tube, out.wins)
	f.Pending, f.PState = c, PendingAwaiting
	return k.accept(id, c)
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

// inside reports whether tr lies inside the conformance envelope of tb: the
// protected tube less the detection buffer.
func (k *Kernel) inside(tb *Tube, tr Track) (bool, ExitCause) {
	if tr.T > tb.End() {
		return true, 0
	}
	if tr.Pos.Dist(tb.Traj.PosAt(tr.T)) > tb.Radius(tr.T)-k.P.Bh {
		return false, ExitHorizontal
	}
	sg := tb.Sigma(tr.T)
	lo, hi := tb.Traj.BandOver(tr.T-sg, tr.T+sg)
	if tr.Alt < lo-k.P.VTol || tr.Alt > hi+k.P.VTol {
		return false, ExitVertical
	}
	return true, 0
}

// intentOK reports whether the broadcast selected altitude agrees with tb.
func (k *Kernel) intentOK(tb *Tube, tr Track) bool {
	if !k.P.IntentCheck || math.IsNaN(tr.SelAlt) || tr.T > tb.End() {
		return true
	}
	sg := tb.Sigma(tr.T)
	lo, hi := tb.Traj.BandOver(tr.T-sg, tr.T+sg+k.P.IntentLookahead)
	return tr.SelAlt >= lo-50 && tr.SelAlt <= hi+50
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

// UpdateTrack is the Conformance Monitor. It compares one surveillance report
// with the contracts of the flight.
//
//   - An aircraft outside every contract it may fly becomes an intruder.
//   - With a pending amendment in state unknown, the first report that lies
//     inside one tube and outside the other settles which contract is flown.
//   - A re-contracted intruder becomes a contract flight again when a report
//     lies inside the new contract.
func (k *Kernel) UpdateTrack(id FlightID, tr Track) {
	k.record(Input{Kind: InTrack, Flight: id, Track: tr})
	f := k.flights[id]
	if f == nil {
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
			if tr.T > f.Active.Start()+30 {
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

// intruderTube builds a state-based tube from the last track: a straight line
// at the current velocity, with a radius that grows fast. The vertical extent
// runs from the current altitude to the altitude the aircraft is heading for.
func (k *Kernel) intruderTube(f *Flight) *Tube {
	tr := f.Track
	end := tr.T + k.P.IntruderHorizon
	lo, hi := tr.Alt, tr.Alt
	vu := k.P.VTol
	level := math.Abs(tr.VRate) < 5
	sel := tr.SelAlt
	switch {
	case !math.IsNaN(sel) && math.Abs(sel-tr.Alt) > 150:
		lo, hi = math.Min(tr.Alt, sel), math.Max(tr.Alt, sel)
		vu = k.P.IntruderVU
	case level:
		// Holding a level: snap to it, so that adjacent levels stay usable.
		if n := math.Round(tr.Alt/1000) * 1000; math.Abs(n-tr.Alt) <= 150 {
			lo, hi = n, n
		}
	default:
		tgt := tr.Alt + tr.VRate*k.P.IntruderHorizon
		lo, hi = math.Min(tr.Alt, tgt), math.Max(tr.Alt, tgt)
		vu = k.P.IntruderVU
	}
	t := &traj.Trajectory{
		Legs:  []traj.Leg{{T0: tr.T, T1: end, P0: tr.Pos, V: tr.Vel}},
		VSegs: []traj.VSeg{{T0: tr.T, T1: end, A0: lo, A1: hi}},
	}
	return &Tube{
		ID: k.nextID(), Flight: f.ID, Traj: t, Anchor: tr.T,
		R0: k.P.IntruderR0, K: k.P.IntruderK, VU0: vu, VMin: tr.Vel.Norm(), Intruder: true,
	}
}

// Reanchored reports one accepted re-anchor.
type Reanchored struct {
	ID       FlightID
	Dt       float64 // the contract times moved by -Dt
	Old, New *traj.Trajectory
}

// ReanchorAll re-anchors every eligible contract on the latest track.
//
// A contract is eligible if the flight conforms, has no pending amendment,
// and has a track taken at the current time. The new contract has the same
// path. Its times move so that the nominal position matches the along-track
// position of the aircraft, and its radius restarts from the base value.
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
		sp := leg.Speed()
		if sp <= 0 {
			continue
		}
		along := f.Track.Pos.Sub(act.Traj.PosAt(k.now)).Dot(leg.V.Scale(1 / sp))
		dt := along / sp
		tr := act.Traj.Shift(-dt)
		c := &Contract{Tube: k.contractTube(id, tr, k.now, k.P.BaseR0()), DivergeAt: act.DivergeAt - dt}
		out := k.verifyReanchor(&c.Tube, &act.Tube, id)
		if !out.ok {
			k.Stats.ReanchorRejected++
			k.mix(1, uint64(id), uint64(out.with), math.Float64bits(out.at))
			continue
		}
		c.ID = k.nextID()
		k.removeTube(&act.Tube)
		k.install(&c.Tube, out.wins)
		f.Active = c
		k.Stats.ReanchorOK++
		k.mix(2, uint64(id), c.ID, math.Float64bits(dt))
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
	k.coast = math.Inf(1)

	tubes := k.scratch[:0]
	for _, id := range k.order {
		tubes = contractTubes(tubes, k.flights[id])
	}
	for i, a := range tubes {
		for _, b := range tubes[i+1:] {
			if a.Flight == b.Flight {
				continue
			}
			target := math.Min(to, math.Min(a.End(), b.End()))
			key := mkKey(a.ID, b.ID)
			u := k.windows[key]
			if u >= target-k.P.ExtendSlack && u >= k.now {
				continue
			}
			from := math.Max(u, k.now)
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
			k.coast = math.Min(k.coast, r.T-k.now)
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

	// One entry per ordered pair, soonest first.
	sort.Slice(k.conflicts, func(i, j int) bool {
		a, b := k.conflicts[i], k.conflicts[j]
		if a.A != b.A {
			return a.A < b.A
		}
		if a.B != b.B {
			return a.B < b.B
		}
		return a.T < b.T
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
			if k.now < math.Max(x.Start(), y.Start()) || k.now > math.Min(x.End(), y.End()) {
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
// two are separated. It returns +Inf if no window applies.
func (k *Kernel) PairWindow(a, b FlightID) float64 {
	fa, fb := k.flights[a], k.flights[b]
	least := math.Inf(1)
	if fa == nil || fb == nil {
		return least
	}
	for _, x := range contractTubes(nil, fa) {
		for _, y := range contractTubes(nil, fb) {
			end := math.Min(x.End(), y.End())
			if k.now < math.Max(x.Start(), y.Start()) || k.now > end {
				continue
			}
			// A window that reaches the end of either contract has no limit.
			if u, ok := k.windows[mkKey(x.ID, y.ID)]; !ok {
				return k.now
			} else if u < end {
				least = math.Min(least, u)
			}
		}
	}
	return least
}

func (k *Kernel) mix(vals ...uint64) {
	for _, v := range vals {
		for i := 0; i < 8; i++ {
			k.outHash ^= v & 0xff
			k.outHash *= 1099511628211
			v >>= 8
		}
	}
}
