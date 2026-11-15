package record

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/teddyknox/clearway/internal/kernel"
)

// maxLine is the longest line that Verify reads.
const maxLine = 64 << 20

// Class names the check that a failure belongs to.
type Class string

const (
	Format    Class = "format"    // a line is not a line of a record, or is out of place
	Chain     Class = "chain"     // the lines do not match a checkpoint
	Signature Class = "signature" // a checkpoint is not signed as required
	Witness   Class = "witness"   // the record does not match a checkpoint that a witness holds
	Replay    Class = "replay"    // the kernel does not reproduce the record
)

// Failure is one reason for which a record does not pass.
type Failure struct {
	Class Class
	Line  int // line of the file, from 1; 0 if the failure has no line
	Msg   string
}

func (f Failure) String() string {
	if f.Line == 0 {
		return fmt.Sprintf("%-9s %s", f.Class, f.Msg)
	}
	return fmt.Sprintf("%-9s line %d: %s", f.Class, f.Line, f.Msg)
}

// Options says what Verify checks a record against.
type Options struct {
	// Trusted holds the public keys of the signers that the caller trusts.
	// If it is empty, Verify checks each signature against the key that the
	// record itself names. That shows that the record is consistent. It does
	// not show who made it.
	Trusted []ed25519.PublicKey

	// Quorum is the number of trusted keys that must sign each checkpoint.
	// Zero means one. It has no effect if Trusted is empty.
	Quorum int

	// Witnessed holds checkpoints that the caller got from a source other
	// than the record: a witness, or a publication. The record must contain
	// each one.
	Witnessed []Checkpoint

	// NoReplay skips the replay of the kernel.
	NoReplay bool
}

// Signer is one key that signed checkpoints of a record.
type Signer struct {
	Key         string // 64 hexadecimal digits
	Trusted     bool
	Checkpoints int
}

// Report is the result of Verify.
type Report struct {
	Header      Header
	Lines       int      // chained lines, the header included
	Inputs      int      // input lines
	Commits     int      // verdict lines that commit a contract
	Rejects     int      // verdict lines that reject a proposal
	Checkpoints int      // checkpoint lines
	Unsigned    int      // checkpoint lines with no signature
	End         int64    // kernel time of the last advance (µs)
	Head        [32]byte // value of the chain after the last chained line
	Tail        int      // chained lines after the last checkpoint: no signature covers them
	Unanswered  bool     // the record ends with a proposal that has no verdict
	Signers     []Signer // keys with a valid signature, most checkpoints first
	Witnessed   int      // witnessed checkpoints that the record contains
	Replayed    int      // chained lines that the replay applied
	ReplayDone  bool     // the replay reached the end of the record with no difference
	StateDiffs  int      // checkpoints whose state digest differs from the replay
	ReasonDiffs int      // proposals that both kernels rejected, for different reasons

	Failures []Failure
}

// OK reports whether the record passed every check that ran.
func (r *Report) OK() bool { return len(r.Failures) == 0 }

func (r *Report) fail(c Class, lineNo int, format string, args ...any) {
	r.Failures = append(r.Failures, Failure{Class: c, Line: lineNo, Msg: fmt.Sprintf(format, args...)})
}

// Verify reads a record and checks it.
//
//   - Format. Each line is a line of the format, in a valid order.
//   - Chain. At each checkpoint, the lines before it give the chain value
//     that the checkpoint states.
//   - Signature. Each signature on a checkpoint is valid. If opt.Trusted is
//     not empty, each checkpoint has signatures from opt.Quorum trusted keys.
//   - Witness. The record contains every checkpoint of opt.Witnessed.
//   - Replay. A new kernel, built from the header and from the Verifier
//     implementations of this module, applies every input. It must return
//     every verdict and reach every state digest that the record states.
//
// Verify returns an error only if it cannot read r. Every other finding is a
// Failure in the Report. After a format failure Verify stops. After a chain
// failure it continues from the value that the checkpoint states, so that it
// can check the later lines.
//
// The replay does not depend on what the record states about the kernel, only
// on the inputs. So it continues after a state digest that differs, and after
// a proposal that both kernels reject for different reasons. Verify reports
// the first of each and counts the rest. The replay stops at the first
// proposal that one kernel commits and the other rejects: from there the two
// hold different contracts. The other checks continue.
//
// A record is untrusted data. Verify does not trust any value in it.
func Verify(r io.Reader, opt Options) (*Report, error) {
	rep := &Report{}
	v := &verifier{rep: rep, opt: opt, signers: map[string]*Signer{}}
	v.trusted = map[string]bool{}
	for _, k := range opt.Trusted {
		v.trusted[hex.EncodeToString(k)] = true
	}
	v.quorum = max(opt.Quorum, 1)
	v.witnessed = append(v.witnessed, opt.Witnessed...)
	sort.SliceStable(v.witnessed, func(i, j int) bool { return v.witnessed[i].Seq < v.witnessed[j].Seq })

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), maxLine)
	sc.Split(splitLines)
	for sc.Scan() {
		v.lineNo++
		if !v.line(sc.Bytes()) {
			break
		}
	}
	if err := sc.Err(); err != nil {
		if !errors.Is(err, bufio.ErrTooLong) {
			return nil, err
		}
		rep.fail(Format, v.lineNo+1, "the line is longer than %d bytes", maxLine)
	}
	v.finish()
	return rep, nil
}

// splitLines splits at each newline and keeps every other byte of a line.
func splitLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

type verifier struct {
	rep *Report
	opt Options

	trusted map[string]bool
	quorum  int
	signers map[string]*Signer

	lineNo    int // line of the file
	stopped   bool
	hasHeader bool
	head      [32]byte
	seq       uint64 // number of the last chained line
	sinceCP   int    // chained lines since the last checkpoint
	firstLine int    // file line of the first chained line since the last checkpoint

	open     bool // the last chained line is a proposal with no verdict yet
	lastIn   kernel.Input
	lastLine int

	witnessed []Checkpoint // ascending by Seq
	nextW     int

	k        *kernel.Kernel // nil when the replay is off or has stopped
	replayed int
	answer   verdict // verdict of the replay for the open proposal
}

// line handles one line. It returns false if Verify must stop.
func (v *verifier) line(raw []byte) bool {
	l, err := parseLine(raw)
	if err != nil {
		v.rep.fail(Format, v.lineNo, "%v", err)
		return false
	}
	switch {
	case !v.hasHeader:
		if l.K != kHeader {
			v.rep.fail(Format, v.lineNo, "the first line must be the header")
			return false
		}
		return v.header(l, raw)
	case l.K == kHeader:
		v.rep.fail(Format, v.lineNo, "a second header")
		return false
	case l.K == kCheckpoint:
		return v.checkpoint(l)
	}

	// A chained line.
	v.head = link(v.head, raw)
	v.seq++
	v.rep.Lines++
	if v.sinceCP == 0 {
		v.firstLine = v.lineNo
	}
	v.sinceCP++
	ok := true
	if l.K == kVerdict {
		ok = v.verdict(l)
	} else {
		ok = v.input(l)
	}
	if ok {
		v.witness()
		if v.k != nil {
			v.replayed++
		}
	}
	return ok
}

func (v *verifier) header(l *line, raw []byte) bool {
	h := Header{Format: l.Format, Spec: l.Spec, Cell: l.Cell, Verifier: l.Verifier, Params: *l.Params, Note: l.Note}
	if err := h.validate(); err != nil {
		v.rep.fail(Format, v.lineNo, "%v", err)
		return false
	}
	v.rep.Header = h
	v.hasHeader = true
	v.head = link(v.head, raw)
	v.rep.Lines = 1
	v.sinceCP, v.firstLine = 1, v.lineNo
	if !v.opt.NoReplay {
		d, err := kernel.Arrangement(h.Verifier)
		if err != nil {
			v.rep.fail(Replay, v.lineNo, "no replay: %v", err)
		} else {
			v.k = kernel.New(h.Params, d)
			v.replayed = 1
		}
	}
	v.witness()
	return true
}

func (v *verifier) input(l *line) bool {
	if v.open {
		v.rep.fail(Format, v.lineNo, "the proposal on line %d has no verdict", v.lastLine)
		return false
	}
	in, err := l.input()
	if err != nil {
		v.rep.fail(Format, v.lineNo, "%v", err)
		return false
	}
	v.rep.Inputs++
	if in.Kind == kernel.InAdvance {
		v.rep.End = in.T
	}
	v.open = in.Kind.HasVerdict()
	v.lastIn, v.lastLine = in, v.lineNo
	if v.k != nil {
		if out, has, err := apply(v.k, in); err != nil {
			v.stopReplay("the kernel stopped on this input: %v", err)
		} else if has {
			v.answer = verdictOf(out)
		}
	}
	return true
}

// apply applies one input and turns a panic of the kernel into an error. A
// record is untrusted data, and Verify must report on it whatever it holds.
func apply(k *kernel.Kernel, in kernel.Input) (out kernel.Verdict, has bool, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("%v", p)
		}
	}()
	out, has = k.Apply(in)
	return out, has, nil
}

func (v *verifier) verdict(l *line) bool {
	if !v.open {
		v.rep.fail(Format, v.lineNo, "a verdict with no proposal on the line before it")
		return false
	}
	got, err := l.verdict()
	if err != nil {
		v.rep.fail(Format, v.lineNo, "%v", err)
		return false
	}
	v.open = false
	if got.OK {
		v.rep.Commits++
	} else {
		v.rep.Rejects++
	}
	if v.k == nil || got == v.answer {
		return true
	}
	what := fmt.Sprintf("%s of flight %d on line %d: the record says %q; the replay says %q",
		kindNames[v.lastIn.Kind], v.lastIn.Flight, v.lastLine, got.String(), v.answer.String())
	if got.OK || v.answer.OK {
		// One kernel committed a contract that the other does not hold.
		v.stopReplay("%s", what)
		return true
	}
	// Both kernels rejected the proposal, for different reasons. They hold
	// the same contracts, and the replay continues.
	v.rep.ReasonDiffs++
	if v.rep.ReasonDiffs == 1 {
		v.rep.fail(Replay, v.lineNo, "%s", what)
	}
	return true
}

func (v *verifier) stopReplay(format string, args ...any) {
	v.rep.fail(Replay, v.lineNo, format, args...)
	v.k = nil
}

func (v *verifier) checkpoint(l *line) bool {
	if v.open {
		v.rep.fail(Format, v.lineNo, "a checkpoint between the proposal on line %d and its verdict", v.lastLine)
		return false
	}
	c, err := l.checkpoint()
	if err != nil {
		v.rep.fail(Format, v.lineNo, "%v", err)
		return false
	}
	v.rep.Checkpoints++

	// Signatures.
	keys, sigErr := c.signers()
	trustedSigs := 0
	for _, key := range keys {
		s := v.signers[key]
		if s == nil {
			s = &Signer{Key: key, Trusted: v.trusted[key]}
			v.signers[key] = s
		}
		s.Checkpoints++
		if s.Trusted {
			trustedSigs++
		}
	}
	signed := sigErr == nil && len(keys) > 0 && (len(v.trusted) == 0 || trustedSigs >= v.quorum)

	// Chain.
	switch {
	case c.Cell != v.rep.Header.Cell:
		v.rep.fail(Chain, v.lineNo, "the checkpoint is for cell %q; the record is for cell %q", c.Cell, v.rep.Header.Cell)
	case c.Seq != v.seq:
		v.rep.fail(Chain, v.lineNo, "the checkpoint is for chained line %d, but %d chained lines come before it: "+
			"lines were added or removed between line %d and here", c.Seq, v.seq+1, v.firstLine)
	case c.Head != v.head:
		v.rep.fail(Chain, v.lineNo, "lines %d to %d do not give the chain value of this checkpoint: "+
			"a line was changed, added, or removed", v.firstLine, v.lineNo-1)
	}
	if signed {
		// Continue from what the signer stated, so that a difference in one
		// interval does not make every later checkpoint fail. A checkpoint
		// that is not signed as required gets no such trust.
		v.head, v.seq = c.Head, c.Seq
	}
	v.sinceCP = 0

	switch {
	case sigErr != nil:
		v.rep.fail(Signature, v.lineNo, "%v", sigErr)
	case len(keys) == 0:
		v.rep.Unsigned++
		if len(v.trusted) > 0 {
			v.rep.fail(Signature, v.lineNo, "the checkpoint has no signature")
		}
	case !signed:
		v.rep.fail(Signature, v.lineNo, "the checkpoint has %d of the %d trusted signatures that it needs", trustedSigs, v.quorum)
	}

	// Replay.
	if v.k != nil {
		switch {
		case c.T != v.k.Now():
			v.stopReplay("the checkpoint states kernel time %v; the replay is at %v", c.T, v.k.Now())
		case c.State != v.k.StateDigest():
			// The replay does not depend on the state that the record states.
			// It continues, to find the first verdict that differs.
			v.rep.StateDiffs++
			if v.rep.StateDiffs == 1 {
				v.rep.fail(Replay, v.lineNo, "the state digest of the checkpoint differs from the state of the replay: "+
					"the two kernels hold different windows, predicted conflicts, or contracts")
			}
		}
	}
	return true
}

// witness compares the chain with each witnessed checkpoint for the chained
// line that was just read.
func (v *verifier) witness() {
	for ; v.nextW < len(v.witnessed) && v.witnessed[v.nextW].Seq <= v.seq; v.nextW++ {
		w := &v.witnessed[v.nextW]
		if !v.witnessUsable(w) {
			continue
		}
		if w.Seq < v.seq {
			// Possible only after a chain failure moved the line numbers.
			v.rep.fail(Witness, v.lineNo, "a witness holds a checkpoint for chained line %d; "+
				"the checkpoints of the record skip that line", w.Seq)
			continue
		}
		if w.Head != v.head {
			v.rep.fail(Witness, v.lineNo, "a witness holds a checkpoint for chained line %d with another chain value: "+
				"the record is not the history that the witness saw", w.Seq)
			continue
		}
		v.rep.Witnessed++
	}
}

// witnessUsable checks that a witnessed checkpoint is for this cell and is
// signed as the options require.
func (v *verifier) witnessUsable(w *Checkpoint) bool {
	if w.Cell != v.rep.Header.Cell {
		v.rep.fail(Witness, 0, "a witnessed checkpoint is for cell %q; the record is for cell %q", w.Cell, v.rep.Header.Cell)
		return false
	}
	keys, err := w.signers()
	if err != nil {
		v.rep.fail(Witness, 0, "the witnessed checkpoint for chained line %d: %v", w.Seq, err)
		return false
	}
	if len(v.trusted) > 0 {
		n := 0
		for _, key := range keys {
			if v.trusted[key] {
				n++
			}
		}
		if n < v.quorum {
			v.rep.fail(Witness, 0, "the witnessed checkpoint for chained line %d has %d of the %d trusted signatures that it needs",
				w.Seq, n, v.quorum)
			return false
		}
	}
	return true
}

func (v *verifier) finish() {
	rep := v.rep
	rep.Head = v.head
	if !v.hasHeader {
		if rep.OK() {
			rep.fail(Format, 0, "the record is empty")
		}
		return
	}
	rep.Tail = v.sinceCP
	rep.Unanswered = v.open
	rep.Replayed = v.replayed
	formatFailed := false
	for _, f := range rep.Failures {
		formatFailed = formatFailed || f.Class == Format
	}
	rep.ReplayDone = v.k != nil && rep.StateDiffs == 0 && rep.ReasonDiffs == 0 && !formatFailed
	if !formatFailed {
		for ; v.nextW < len(v.witnessed); v.nextW++ {
			rep.fail(Witness, 0, "a witness holds a checkpoint for chained line %d; the record ends at chained line %d: "+
				"the end of the record is missing", v.witnessed[v.nextW].Seq, v.seq)
		}
	}
	for _, s := range v.signers {
		rep.Signers = append(rep.Signers, *s)
	}
	sort.Slice(rep.Signers, func(i, j int) bool {
		a, b := rep.Signers[i], rep.Signers[j]
		if a.Checkpoints != b.Checkpoints {
			return a.Checkpoints > b.Checkpoints
		}
		return a.Key < b.Key
	})
}

// ReadCheckpoints returns every checkpoint line of r, in order. r may be a
// record, or a file that holds only checkpoint lines. ReadCheckpoints checks
// the form of each checkpoint line. It does not check signatures.
func ReadCheckpoints(r io.Reader) ([]Checkpoint, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), maxLine)
	sc.Split(splitLines)
	var out []Checkpoint
	for n := 1; sc.Scan(); n++ {
		var kind struct {
			K string `json:"k"`
		}
		if err := json.Unmarshal(sc.Bytes(), &kind); err != nil {
			return nil, fmt.Errorf("line %d: not a line of a record: %v", n, err)
		}
		if kind.K != kCheckpoint {
			continue
		}
		l, err := parseLine(sc.Bytes())
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", n, err)
		}
		c, err := l.checkpoint()
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", n, err)
		}
		out = append(out, *c)
	}
	return out, sc.Err()
}
