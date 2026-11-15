package record

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/teddyknox/clearway/internal/kernel"
	"github.com/teddyknox/clearway/internal/traj"
)

// A Program is the kernel as a program outside this process: the kernel in
// Rust (guest/kernel), run in the machine of package rvm or as a native
// program. It reads one input stream and writes one stream of answers.
// docs/vm.md defines both streams.
//
// Run gives the program the whole input and returns its whole output and its
// exit code. steps is the number of instructions that it executed, or zero if
// the program does not count them.
type Program interface {
	Run(input []byte) (output []byte, exit uint32, steps uint64, err error)
}

// The first eight bytes of the input stream: "CLWK" and the version 2.
var wireMagic = []byte("CLWK\x02\x00\x00\x00")

// Kinds of message beyond the inputs of the kernel, and kinds of answer.
const (
	wireDigest = 250 // asks for the state digest
	wireEnd    = 255 // ends the stream

	answerVerdict = 1
	answerDigest  = 2
)

type wire struct{ bytes.Buffer }

func (w *wire) u8(v uint8)   { w.WriteByte(v) }
func (w *wire) u32(v uint32) { w.Write(binary.LittleEndian.AppendUint32(nil, v)) }
func (w *wire) i64(vals ...int64) {
	for _, v := range vals {
		w.Write(binary.LittleEndian.AppendUint64(nil, uint64(v)))
	}
}

func (w *wire) flag(b bool) {
	if b {
		w.u8(1)
	} else {
		w.u8(0)
	}
}

func (w *wire) trajectory(tr *traj.Trajectory) {
	if tr == nil {
		tr = &traj.Trajectory{}
	}
	w.u32(uint32(len(tr.Legs)))
	for _, l := range tr.Legs {
		w.i64(l.T0, l.T1, l.P0.X, l.P0.Y, l.V.X, l.V.Y)
	}
	w.u32(uint32(len(tr.VSegs)))
	for _, s := range tr.VSegs {
		w.i64(s.T0, s.T1, s.A0, s.A1)
	}
}

// header writes the start of the stream: the magic, the arrangement, and the
// parameters in the order of kernel.Params.
func (w *wire) header(h *Header) error {
	w.Write(wireMagic)
	switch h.Verifier {
	case "single":
		w.u8(0)
	case "dual":
		w.u8(1)
	default:
		return fmt.Errorf("the kernel program has no Verifier arrangement %q", h.Verifier)
	}
	p := h.Params
	w.i64(p.Sh, p.Sv, p.VTol, p.Horizon, p.CommitHorizon, p.IntruderHorizon, p.ExtendSlack,
		p.W, p.E0, p.Bh, p.K, p.MinLead, p.CellRadius, p.IntruderR0, p.IntruderK, p.IntruderVU, p.RecontractTol)
	w.flag(p.IntentCheck)
	w.i64(p.IntentLookahead)
	return nil
}

// input writes one input of the kernel. The kind is the number of
// kernel.InputKind.
func (w *wire) input(in kernel.Input) {
	w.u8(uint8(in.Kind))
	switch in.Kind {
	case kernel.InAdvance:
		w.i64(in.T)
	case kernel.InAdmit:
		w.i64(int64(in.Flight))
		w.trajectory(in.Traj)
	case kernel.InPropose, kernel.InRecontract:
		w.i64(int64(in.Flight))
		w.u8(uint8(in.By))
		w.trajectory(in.Traj)
	case kernel.InTrack:
		tk := in.Track
		w.i64(int64(in.Flight), tk.T, tk.Pos.X, tk.Pos.Y, tk.Vel.X, tk.Vel.Y, tk.Alt, tk.VRate)
		w.flag(tk.HasSel)
		w.i64(tk.SelAlt)
	case kernel.InSetAuthority:
		w.i64(int64(in.Flight))
		w.u8(uint8(in.By))
	case kernel.InReanchor, kernel.InTick:
	default:
		w.i64(int64(in.Flight))
	}
}

// answer is one answer that the record states, and the line that states it.
type answer struct {
	line    int
	verdict *verdict
	digest  [32]byte
}

// ProgramReport is the result of ReplayProgram.
type ProgramReport struct {
	Verdicts int    // verdicts that the program gave and the record states
	Digests  int    // state digests, one for each checkpoint
	Input    int    // bytes of the input stream
	Steps    uint64 // instructions executed, if the program counts them

	Failures []Failure
}

// OK reports whether the program reproduced the record.
func (r *ProgramReport) OK() bool { return len(r.Failures) == 0 }

// ReplayProgram gives the inputs of a record to a kernel program and compares
// every answer with the record: each verdict with its verdict line, and the
// state digest at each checkpoint with the digest that the checkpoint states.
// The comparison is of bits.
//
// ReplayProgram checks the form of each line. It does not check the chain or
// the signatures: Verify does. It returns an error if it cannot read r or
// cannot run the program. Every other finding is a Failure.
func ReplayProgram(r io.Reader, prog Program) (*ProgramReport, error) {
	rep := &ProgramReport{}
	var in wire
	var want []answer
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), maxLine)
	sc.Split(splitLines)
	lineNo, open := 0, false
	for sc.Scan() {
		lineNo++
		l, err := parseLine(sc.Bytes())
		if err != nil {
			rep.Failures = append(rep.Failures, Failure{Format, lineNo, err.Error()})
			return rep, nil
		}
		switch {
		case (lineNo == 1) != (l.K == kHeader):
			rep.Failures = append(rep.Failures, Failure{Format, lineNo, "the header must be the first line, and only the first"})
			return rep, nil
		case l.K == kHeader:
			h := Header{Format: l.Format, Spec: l.Spec, Cell: l.Cell, Verifier: l.Verifier, Params: *l.Params}
			if err := h.validate(); err == nil {
				err = in.header(&h)
			}
			if err != nil {
				rep.Failures = append(rep.Failures, Failure{Format, lineNo, err.Error()})
				return rep, nil
			}
		case l.K == kCheckpoint:
			c, err := l.checkpoint()
			if err != nil || open {
				rep.Failures = append(rep.Failures, Failure{Format, lineNo, "a checkpoint that is malformed or out of place"})
				return rep, nil
			}
			in.u8(wireDigest)
			want = append(want, answer{line: lineNo, digest: c.State})
		case l.K == kVerdict:
			v, err := l.verdict()
			if err != nil || !open {
				rep.Failures = append(rep.Failures, Failure{Format, lineNo, "a verdict that is malformed or out of place"})
				return rep, nil
			}
			open = false
			want = append(want, answer{line: lineNo, verdict: &v})
		default:
			input, err := l.input()
			if err != nil || open {
				rep.Failures = append(rep.Failures, Failure{Format, lineNo, "an input that is malformed or out of place"})
				return rep, nil
			}
			in.input(input)
			open = input.Kind.HasVerdict()
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if lineNo == 0 {
		rep.Failures = append(rep.Failures, Failure{Format, 0, "the record is empty"})
		return rep, nil
	}
	in.u8(wireEnd)
	rep.Input = in.Len()

	out, exit, steps, err := prog.Run(in.Bytes())
	if err != nil {
		return nil, err
	}
	rep.Steps = steps
	fail := func(line int, format string, args ...any) {
		rep.Failures = append(rep.Failures, Failure{Replay, line, fmt.Sprintf(format, args...)})
	}
	for _, w := range want {
		got, rest, ok := nextAnswer(out)
		if !ok {
			fail(w.line, "the kernel program gave no answer here: its output ends")
			break
		}
		out = rest
		switch {
		case w.verdict != nil && got.verdict == nil:
			fail(w.line, "the record states a verdict; the kernel program gave a state digest")
		case w.verdict == nil && got.verdict != nil:
			fail(w.line, "the record states a state digest; the kernel program gave a verdict")
		case w.verdict != nil && *w.verdict != *got.verdict:
			fail(w.line, "the record says %q; the kernel program says %q", w.verdict.String(), got.verdict.String())
		case w.verdict == nil && w.digest != got.digest:
			fail(w.line, "the state digest of the checkpoint is %s; the kernel program has %s",
				hex.EncodeToString(w.digest[:8]), hex.EncodeToString(got.digest[:8]))
		case w.verdict != nil:
			rep.Verdicts++
			continue
		default:
			rep.Digests++
			continue
		}
		// After a difference the two kernels are in different states, and
		// every later answer may differ. The first one is the finding.
		break
	}
	switch {
	case exit != 0:
		fail(0, "the kernel program stopped with exit code %d", exit)
	case rep.OK() && len(out) != 0:
		fail(0, "the kernel program gave %d bytes more than the record has answers for", len(out))
	}
	return rep, nil
}

// nextAnswer reads one answer from the output of the program.
func nextAnswer(out []byte) (a answer, rest []byte, ok bool) {
	if len(out) == 0 {
		return a, nil, false
	}
	switch out[0] {
	case answerDigest:
		if len(out) < 33 {
			return a, nil, false
		}
		copy(a.digest[:], out[1:33])
		return a, out[33:], true
	case answerVerdict:
		if len(out) < 2 {
			return a, nil, false
		}
		v := &verdict{OK: out[1] == 1}
		if v.OK {
			if len(out) < 10 {
				return a, nil, false
			}
			v.Contract = binary.LittleEndian.Uint64(out[2:10])
			a.verdict = v
			return a, out[10:], true
		}
		if len(out) < 19 || out[1] != 0 || kernel.RejectReason(out[2]) >= kernel.NumReasons {
			return a, nil, false
		}
		v.Reason = kernel.RejectReason(out[2])
		v.With = kernel.FlightID(binary.LittleEndian.Uint64(out[3:11]))
		v.At = int64(binary.LittleEndian.Uint64(out[11:19]))
		a.verdict = v
		return a, out[19:], true
	}
	return a, nil, false
}
