package record

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/teddyknox/clearway/internal/kernel"
)

// A Stepper is a kernel program that takes its input in parts.
//
// Step gives the program more bytes of its input stream and runs it until it
// has read them all. It returns the bytes that the program wrote in that
// time. If the program stops, Step returns an error, and so does every later
// call. docs/vm.md defines the two streams.
type Stepper interface {
	Step(input []byte) (output []byte, err error)
}

// A Session is one kernel, run as a kernel program, with the chain of its
// record. It applies one line of a record at a time.
//
// A replica of a cell holds a Session. The replicas agree on the lines and
// on their order. Each replica then applies the lines to its own Session, and
// the Sessions reach the same checkpoint: the same number of chained lines,
// the same value of the chain, the same kernel time, and the same state
// digest. A replica that differs in one bit of a verdict or of the state has
// another checkpoint.
//
// A Session is not safe for use by more than one goroutine at a time.
type Session struct {
	prog Stepper
	cell string
	head [32]byte
	seq  uint64 // the number of the last chained line; the header is line 0
	now  int64  // the clock of the kernel
}

// HeaderLine returns the header line of a record, without the newline. It
// fills the format and specification versions of h.
func HeaderLine(h Header) ([]byte, error) {
	h.Format, h.Spec = FormatVersion, SpecVersion
	if err := h.validate(); err != nil {
		return nil, err
	}
	return json.Marshal(&line{
		K: kHeader, Format: h.Format, Spec: h.Spec, Cell: h.Cell, Verifier: h.Verifier, Params: &h.Params, Note: h.Note,
	})
}

// ParseHeader reads the header line of a record.
func ParseHeader(raw []byte) (Header, error) {
	l, err := parseLine(raw)
	if err != nil {
		return Header{}, err
	}
	if l.K != kHeader {
		return Header{}, fmt.Errorf("a line of kind %q is not a header", l.K)
	}
	h := Header{Format: l.Format, Spec: l.Spec, Cell: l.Cell, Verifier: l.Verifier, Params: *l.Params, Note: l.Note}
	return h, h.validate()
}

// NewSession starts the kernel of a record. header is the header line of the
// record: it becomes chained line 0. prog is a kernel program that has read
// no input yet.
func NewSession(header []byte, prog Stepper) (*Session, error) {
	h, err := ParseHeader(header)
	if err != nil {
		return nil, err
	}
	var w wire
	if err := w.header(&h); err != nil {
		return nil, err
	}
	out, err := prog.Step(w.Bytes())
	if err != nil {
		return nil, err
	}
	if len(out) != 0 {
		return nil, errors.New("the kernel program answered the header")
	}
	return &Session{prog: prog, cell: h.Cell, head: link([32]byte{}, header)}, nil
}

// CheckInput reports whether raw is an input line of a record: a line that
// Apply applies. It does not look at the state of any kernel.
func CheckInput(raw []byte) error {
	_, err := parseInput(raw)
	return err
}

func parseInput(raw []byte) (kernel.Input, error) {
	l, err := parseLine(raw)
	if err != nil {
		return kernel.Input{}, err
	}
	if l.K == kHeader || l.K == kVerdict || l.K == kCheckpoint {
		return kernel.Input{}, fmt.Errorf("a line of kind %q is not an input", l.K)
	}
	return l.input()
}

// Apply applies one input line to the kernel and adds the line to the chain.
// raw is the bytes of the line, without the newline. If the input is a
// proposal, Apply returns the verdict line of the kernel, and adds that line
// to the chain too.
//
// If raw is not an input line, Apply returns an error and changes nothing.
// If the kernel program stops, Apply returns the error of the program, and
// the Session cannot continue.
func (s *Session) Apply(raw []byte) (verdictLine []byte, err error) {
	in, err := parseInput(raw)
	if err != nil {
		return nil, err
	}
	var w wire
	w.input(in)
	out, err := s.prog.Step(w.Bytes())
	if err != nil {
		return nil, err
	}
	s.head = link(s.head, raw)
	s.seq++
	if in.Kind == kernel.InAdvance && in.T > s.now && in.T <= kernel.MaxClock {
		s.now = in.T // the rule of the kernel for its clock
	}
	if !in.Kind.HasVerdict() {
		if len(out) != 0 {
			return nil, errors.New("the kernel program answered an input that has no verdict")
		}
		return nil, nil
	}
	a, rest, ok := nextAnswer(out)
	if !ok || a.verdict == nil || len(rest) != 0 {
		return nil, errors.New("the kernel program did not answer a proposal with one verdict")
	}
	verdictLine, err = json.Marshal(a.verdict.line())
	if err != nil {
		return nil, err
	}
	s.head = link(s.head, verdictLine)
	s.seq++
	return verdictLine, nil
}

// Checkpoint returns the checkpoint of the record so far, with no signature.
// It asks the kernel program for the state digest.
func (s *Session) Checkpoint() (Checkpoint, error) {
	out, err := s.prog.Step([]byte{wireDigest})
	if err != nil {
		return Checkpoint{}, err
	}
	a, rest, ok := nextAnswer(out)
	if !ok || a.verdict != nil || len(rest) != 0 {
		return Checkpoint{}, errors.New("the kernel program did not answer with one state digest")
	}
	return Checkpoint{Cell: s.cell, Seq: s.seq, T: s.now, Head: s.head, State: a.digest}, nil
}
