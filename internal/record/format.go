// Package record writes and checks the record of one kernel: every input and
// every verdict in order, chained by hash, with signed checkpoints.
//
// A record is a text file with one JSON object on each line. docs/record.md
// defines the format. Writer produces a record. Verify checks one: the chain,
// the signatures, the checkpoints that a witness holds, and a replay of the
// kernel.
//
// Every number in a record is an integer in the units of the kernel:
// micrometres, microseconds, micrometres per second.
package record

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/teddyknox/clearway/internal/fx"
	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/traj"
)

const (
	// FormatVersion is the version of the record format that this package
	// writes and reads.
	FormatVersion = "0.2"
	// SpecVersion is the version of the kernel specification that a record
	// of this package follows.
	SpecVersion = "0.2"

	checkpointTag = "clearway checkpoint 0.2\n"

	kHeader     = "header"
	kVerdict    = "verdict"
	kCheckpoint = "checkpoint"
)

// The name of each kind of input, as a line gives it in "k".
var kindNames = [kernel.NumInputKinds]string{
	kernel.InAdvance:      "advance",
	kernel.InAdmit:        "admit",
	kernel.InPropose:      "propose",
	kernel.InRecontract:   "recontract",
	kernel.InAck:          "ack",
	kernel.InRefuse:       "refuse",
	kernel.InFail:         "fail",
	kernel.InTrack:        "track",
	kernel.InMarkIntruder: "mark-intruder",
	kernel.InReanchor:     "reanchor",
	kernel.InRemove:       "remove",
	kernel.InTick:         "tick",
	kernel.InSetAuthority: "authority",
}

var (
	kindByName   = map[string]kernel.InputKind{}
	reasonByName = map[string]kernel.RejectReason{}
)

func init() {
	for kd, name := range kindNames {
		kindByName[name] = kernel.InputKind(kd)
	}
	for r := kernel.RejNone + 1; r < kernel.NumReasons; r++ {
		reasonByName[r.String()] = r
	}
}

var authorityNames = [...]string{kernel.Automation: "automation", kernel.Human: "human"}

// Header is the first line of a record. It states what a replay needs.
type Header struct {
	Format   string        // version of the record format
	Spec     string        // version of the kernel specification
	Cell     string        // name of the cell: 1 to 64 of the characters A-Z a-z 0-9 . _ -
	Verifier string        // Verifier arrangement that the kernel declares: "dual" or "single"
	Params   kernel.Params // parameters of the cell
	Note     string        // free text
}

func (h *Header) validate() error {
	if h.Format != FormatVersion {
		return fmt.Errorf("record format %q: this program reads format %s", h.Format, FormatVersion)
	}
	if h.Spec == "" || h.Verifier == "" {
		return errors.New("the header must name a specification and a Verifier arrangement")
	}
	if !validCell(h.Cell) {
		return fmt.Errorf("cell name %q: use 1 to 64 of the characters A-Z a-z 0-9 . _ -", h.Cell)
	}
	return h.Params.Validate()
}

func validCell(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// Sig is one signature on a checkpoint.
type Sig struct {
	Key string `json:"key"` // Ed25519 public key: 64 hexadecimal digits
	Sig string `json:"sig"` // Ed25519 signature: 128 hexadecimal digits
}

// Checkpoint is a statement about a record: after the line with number Seq,
// the chain has the value Head, and the kernel has the state digest State at
// kernel time T.
type Checkpoint struct {
	Cell  string
	Seq   uint64
	T     int64 // µs
	Head  [32]byte
	State [32]byte
	Sigs  []Sig
}

// message returns the bytes that a signer of the checkpoint signs.
func (c *Checkpoint) message() []byte {
	var b bytes.Buffer
	b.WriteString(checkpointTag)
	b.WriteString(c.Cell)
	b.WriteByte('\n')
	b.WriteString(strconv.FormatUint(c.Seq, 10))
	b.WriteByte('\n')
	b.WriteString(strconv.FormatInt(c.T, 10))
	b.WriteByte('\n')
	b.WriteString(hex.EncodeToString(c.Head[:]))
	b.WriteByte('\n')
	b.WriteString(hex.EncodeToString(c.State[:]))
	b.WriteByte('\n')
	return b.Bytes()
}

func (c *Checkpoint) sign(key ed25519.PrivateKey) {
	pub := key.Public().(ed25519.PublicKey)
	c.Sigs = append(c.Sigs, Sig{Key: hex.EncodeToString(pub), Sig: hex.EncodeToString(ed25519.Sign(key, c.message()))})
}

// MarshalLine returns the checkpoint as one line of a record, without the
// newline.
func (c *Checkpoint) MarshalLine() []byte {
	t, seq := c.T, c.Seq
	b, _ := json.Marshal(line{
		K: kCheckpoint, Cell: c.Cell, T: &t, Seq: &seq,
		Head: hex.EncodeToString(c.Head[:]), State: hex.EncodeToString(c.State[:]), Sigs: c.Sigs,
	})
	return b
}

// signers checks every signature on the checkpoint. It returns the keys with
// a valid signature, each once. It returns an error if a signature is not
// well formed or does not verify.
func (c *Checkpoint) signers() ([]string, error) {
	msg := c.message()
	seen := map[string]bool{}
	var keys []string
	for _, s := range c.Sigs {
		pub, err := hex.DecodeString(s.Key)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("a signature names a key that is not 64 hexadecimal digits")
		}
		sig, err := hex.DecodeString(s.Sig)
		if err != nil || len(sig) != ed25519.SignatureSize {
			return nil, fmt.Errorf("the signature of key %s is not 128 hexadecimal digits", short(s.Key))
		}
		if !ed25519.Verify(pub, msg, sig) {
			return nil, fmt.Errorf("the signature of key %s is not valid for this checkpoint", short(s.Key))
		}
		key := hex.EncodeToString(pub)
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func short(hexKey string) string {
	if len(hexKey) > 12 {
		return hexKey[:12]
	}
	return hexKey
}

// vec is a point or a velocity: exactly two numbers.
type vec [2]int64

func (v *vec) UnmarshalJSON(b []byte) error {
	var s []int64
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if len(s) != 2 {
		return errors.New("a point or a velocity must have two numbers")
	}
	v[0], v[1] = s[0], s[1]
	return nil
}

// Leg, VSeg, and Trajectory have the same JSON form as in the conformance
// suite.
type Leg struct {
	T0 int64 `json:"t0"`
	T1 int64 `json:"t1"`
	P0 vec   `json:"p0"`
	V  vec   `json:"v"`
}

type VSeg struct {
	T0 int64 `json:"t0"`
	T1 int64 `json:"t1"`
	A0 int64 `json:"a0"`
	A1 int64 `json:"a1"`
}

type Trajectory struct {
	Legs  []Leg  `json:"legs"`
	VSegs []VSeg `json:"vsegs"`
}

func fromTraj(tr *traj.Trajectory) *Trajectory {
	out := &Trajectory{Legs: []Leg{}, VSegs: []VSeg{}}
	if tr == nil {
		return out
	}
	for _, l := range tr.Legs {
		out.Legs = append(out.Legs, Leg{T0: l.T0, T1: l.T1, P0: vec{l.P0.X, l.P0.Y}, V: vec{l.V.X, l.V.Y}})
	}
	for _, s := range tr.VSegs {
		out.VSegs = append(out.VSegs, VSeg(s))
	}
	return out
}

func (t *Trajectory) toTraj() *traj.Trajectory {
	out := &traj.Trajectory{}
	for _, l := range t.Legs {
		out.Legs = append(out.Legs, traj.Leg{T0: l.T0, T1: l.T1, P0: fx.Vec{X: l.P0[0], Y: l.P0[1]}, V: fx.Vec{X: l.V[0], Y: l.V[1]}})
	}
	for _, s := range t.VSegs {
		out.VSegs = append(out.VSegs, traj.VSeg(s))
	}
	return out
}

// line is the JSON form of every kind of line. Each kind uses some of the
// names; fields states which.
type line struct {
	K string `json:"k"`

	// header
	Format   string         `json:"clearway-record,omitempty"`
	Spec     string         `json:"spec,omitempty"`
	Cell     string         `json:"cell,omitempty"` // also checkpoint
	Verifier string         `json:"verifier,omitempty"`
	Params   *kernel.Params `json:"params,omitempty"`
	Note     string         `json:"note,omitempty"`

	// inputs
	F    *int64      `json:"f,omitempty"`
	By   string      `json:"by,omitempty"`
	T    *int64      `json:"t,omitempty"` // also checkpoint
	Traj *Trajectory `json:"traj,omitempty"`
	P    *vec        `json:"p,omitempty"`
	V    *vec        `json:"v,omitempty"`
	Alt  *int64      `json:"alt,omitempty"`
	VR   *int64      `json:"vr,omitempty"`
	Sel  *int64      `json:"sel,omitempty"`

	// verdict
	OK       *bool   `json:"ok,omitempty"`
	Reason   string  `json:"reason,omitempty"`
	With     *int64  `json:"with,omitempty"`
	At       *int64  `json:"at,omitempty"`
	Contract *uint64 `json:"contract,omitempty"`

	// checkpoint
	Seq   *uint64 `json:"seq,omitempty"`
	Head  string  `json:"head,omitempty"`
	State string  `json:"state,omitempty"`
	Sigs  []Sig   `json:"sigs,omitempty"`
}

// One bit for each name of a line, except "k".
const (
	nFormat uint32 = 1 << iota
	nSpec
	nCell
	nVerifier
	nParams
	nNote
	nF
	nBy
	nT
	nTraj
	nP
	nV
	nAlt
	nVR
	nSel
	nOK
	nReason
	nWith
	nAt
	nContract
	nSeq
	nHead
	nState
	nSigs
)

var nameOfBit = []string{
	"clearway-record", "spec", "cell", "verifier", "params", "note", "f", "by", "t", "traj", "p", "v", "alt", "vr",
	"sel", "ok", "reason", "with", "at", "contract", "seq", "head", "state", "sigs",
}

// present returns the names that the line has.
func (l *line) present() uint32 {
	var m uint32
	set := func(bit uint32, has bool) {
		if has {
			m |= bit
		}
	}
	set(nFormat, l.Format != "")
	set(nSpec, l.Spec != "")
	set(nCell, l.Cell != "")
	set(nVerifier, l.Verifier != "")
	set(nParams, l.Params != nil)
	set(nNote, l.Note != "")
	set(nF, l.F != nil)
	set(nBy, l.By != "")
	set(nT, l.T != nil)
	set(nTraj, l.Traj != nil)
	set(nP, l.P != nil)
	set(nV, l.V != nil)
	set(nAlt, l.Alt != nil)
	set(nVR, l.VR != nil)
	set(nSel, l.Sel != nil)
	set(nOK, l.OK != nil)
	set(nReason, l.Reason != "")
	set(nWith, l.With != nil)
	set(nAt, l.At != nil)
	set(nContract, l.Contract != nil)
	set(nSeq, l.Seq != nil)
	set(nHead, l.Head != "")
	set(nState, l.State != "")
	set(nSigs, l.Sigs != nil)
	return m
}

// fields returns the names that a line of kind k must have and the names
// that it may have. ok is false if k is not a kind of line.
func fields(l *line) (must, may uint32, ok bool) {
	switch l.K {
	case kHeader:
		return nFormat | nSpec | nCell | nVerifier | nParams, nNote, true
	case kCheckpoint:
		return nCell | nT | nSeq | nHead | nState, nSigs, true
	case kVerdict:
		switch {
		case l.OK != nil && *l.OK:
			return nOK | nContract, 0, true
		case l.Reason == kernel.RejConflict.String() || l.Reason == kernel.RejDisagree.String():
			return nOK | nReason | nWith | nAt, 0, true
		}
		return nOK | nReason, 0, true
	}
	kd, known := kindByName[l.K]
	if !known {
		return 0, 0, false
	}
	switch kd {
	case kernel.InAdvance:
		return nT, 0, true
	case kernel.InAdmit:
		return nF | nTraj, 0, true
	case kernel.InPropose, kernel.InRecontract:
		return nF | nBy | nTraj, 0, true
	case kernel.InTrack:
		return nF | nT | nP | nV | nAlt | nVR, nSel, true
	case kernel.InSetAuthority:
		return nF | nBy, 0, true
	case kernel.InReanchor, kernel.InTick:
		return 0, 0, true
	}
	return nF, 0, true
}

// parseLine decodes one line and checks that it has the names of its kind
// and no others.
func parseLine(raw []byte) (*line, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var l line
	if err := dec.Decode(&l); err != nil {
		return nil, fmt.Errorf("not a line of a record: %v", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("text after the JSON object")
	}
	must, may, ok := fields(&l)
	if !ok {
		return nil, fmt.Errorf("unknown kind %q", l.K)
	}
	has := l.present()
	for i, name := range nameOfBit {
		bit := uint32(1) << i
		switch {
		case must&bit != 0 && has&bit == 0:
			return nil, fmt.Errorf("a line of kind %q must have %q", l.K, name)
		case (must|may)&bit == 0 && has&bit != 0:
			return nil, fmt.Errorf("a line of kind %q must not have %q", l.K, name)
		}
	}
	return &l, nil
}

// inputLine returns the line for one input.
func inputLine(in kernel.Input) (*line, error) {
	if in.Kind >= kernel.NumInputKinds {
		return nil, fmt.Errorf("input of unknown kind %d", in.Kind)
	}
	l := &line{K: kindNames[in.Kind]}
	f := int64(in.Flight)
	by := func() error {
		if int(in.By) >= len(authorityNames) {
			return fmt.Errorf("unknown authority %d", in.By)
		}
		l.By = authorityNames[in.By]
		return nil
	}
	switch in.Kind {
	case kernel.InAdvance:
		l.T = &in.T
	case kernel.InAdmit:
		l.F, l.Traj = &f, fromTraj(in.Traj)
	case kernel.InPropose, kernel.InRecontract:
		l.F, l.Traj = &f, fromTraj(in.Traj)
		return l, by()
	case kernel.InTrack:
		tk := in.Track
		l.F, l.T, l.Alt, l.VR = &f, &tk.T, &tk.Alt, &tk.VRate
		l.P, l.V = &vec{tk.Pos.X, tk.Pos.Y}, &vec{tk.Vel.X, tk.Vel.Y}
		if tk.HasSel {
			l.Sel = &tk.SelAlt
		}
	case kernel.InSetAuthority:
		l.F = &f
		return l, by()
	case kernel.InReanchor, kernel.InTick:
	default:
		l.F = &f
	}
	return l, nil
}

// input returns the input that a line of an input kind states.
func (l *line) input() (kernel.Input, error) {
	in := kernel.Input{Kind: kindByName[l.K]}
	if l.F != nil {
		in.Flight = kernel.FlightID(*l.F)
	}
	if l.By != "" {
		found := false
		for a, name := range authorityNames {
			if name == l.By {
				in.By, found = kernel.Authority(a), true
			}
		}
		if !found {
			return in, fmt.Errorf("unknown authority %q", l.By)
		}
	}
	switch in.Kind {
	case kernel.InAdvance:
		in.T = *l.T
	case kernel.InAdmit, kernel.InPropose, kernel.InRecontract:
		in.Traj = l.Traj.toTraj()
	case kernel.InTrack:
		in.Track = kernel.Track{
			T: *l.T, Pos: fx.Vec{X: l.P[0], Y: l.P[1]}, Vel: fx.Vec{X: l.V[0], Y: l.V[1]},
			Alt: *l.Alt, VRate: *l.VR,
		}
		if l.Sel != nil {
			in.Track.HasSel, in.Track.SelAlt = true, *l.Sel
		}
	}
	return in, nil
}

// verdict is what a verdict line states.
type verdict struct {
	OK       bool
	Reason   kernel.RejectReason
	With     kernel.FlightID
	At       int64
	Contract uint64
}

func verdictOf(v kernel.Verdict) verdict {
	out := verdict{OK: v.OK, Reason: v.Reason, With: v.With, At: v.At}
	if v.OK && v.Contract != nil {
		out.Contract = v.Contract.ID
	}
	return out
}

func (v verdict) line() *line {
	l := &line{K: kVerdict, OK: &v.OK}
	if v.OK {
		l.Contract = &v.Contract
		return l
	}
	l.Reason = v.Reason.String()
	if v.Reason == kernel.RejConflict || v.Reason == kernel.RejDisagree {
		with := int64(v.With)
		l.With, l.At = &with, &v.At
	}
	return l
}

func (l *line) verdict() (verdict, error) {
	if *l.OK {
		return verdict{OK: true, Contract: *l.Contract}, nil
	}
	r, known := reasonByName[l.Reason]
	if !known {
		return verdict{}, fmt.Errorf("unknown reason %q", l.Reason)
	}
	v := verdict{Reason: r}
	if l.With != nil {
		v.With, v.At = kernel.FlightID(*l.With), *l.At
	}
	return v, nil
}

func (v verdict) String() string {
	switch {
	case v.OK:
		return fmt.Sprintf("committed as contract %d", v.Contract)
	case v.Reason == kernel.RejConflict || v.Reason == kernel.RejDisagree:
		return fmt.Sprintf("rejected: %s with flight %d at %d µs", v.Reason, v.With, v.At)
	}
	return "rejected: " + v.Reason.String()
}

func (l *line) checkpoint() (*Checkpoint, error) {
	c := &Checkpoint{Cell: l.Cell, Seq: *l.Seq, T: *l.T, Sigs: l.Sigs}
	if !validCell(c.Cell) {
		return nil, fmt.Errorf("cell name %q is not valid", c.Cell)
	}
	if err := hex32(c.Head[:], l.Head); err != nil {
		return nil, fmt.Errorf(`"head": %v`, err)
	}
	if err := hex32(c.State[:], l.State); err != nil {
		return nil, fmt.Errorf(`"state": %v`, err)
	}
	return c, nil
}

func hex32(dst []byte, s string) error {
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != sha256.Size {
		return errors.New("not 64 hexadecimal digits")
	}
	copy(dst, b)
	return nil
}

// link returns the value of the chain after one more line.
func link(head [32]byte, raw []byte) [32]byte {
	h := sha256.New()
	h.Write(head[:])
	h.Write(raw)
	var out [32]byte
	h.Sum(out[:0])
	return out
}
