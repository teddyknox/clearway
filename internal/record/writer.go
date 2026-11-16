package record

import (
	"bufio"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/teddyknox/clearway/internal/kernel"
)

// Writer writes the record of one kernel. It is a kernel.Recorder.
//
// The caller sets the Writer as the Recorder of a new kernel, before the
// kernel has an input. The kernel then passes every input and every verdict.
// The caller calls Checkpoint between kernel operations, and Close at the end.
//
// Failure. If a line cannot be encoded or written, the Writer keeps the
// first error and writes nothing more. The file is then a valid record that
// ends early. Err and Close return the error. The Writer does not stop the
// kernel: the caller must check Err or Close before it relies on the record.
type Writer struct {
	out     *bufio.Writer
	cell    string
	signers []ed25519.PrivateKey

	head [32]byte
	seq  uint64 // number of the last chained line; the header is line 0
	open bool   // the last input has a verdict that is not written yet
	err  error
}

// NewWriter writes the header line and returns the Writer. It fills the
// format and specification versions of h. Each key in signers signs every
// checkpoint. With no signer, checkpoints carry no signature.
func NewWriter(out io.Writer, h Header, signers ...ed25519.PrivateKey) (*Writer, error) {
	h.Format, h.Spec = FormatVersion, SpecVersion
	if err := h.validate(); err != nil {
		return nil, err
	}
	for _, k := range signers {
		if len(k) != ed25519.PrivateKeySize {
			return nil, errors.New("a signing key is not an Ed25519 private key")
		}
	}
	w := &Writer{out: bufio.NewWriterSize(out, 1<<16), cell: h.Cell, signers: signers}
	w.chain(&line{
		K: kHeader, Format: h.Format, Spec: h.Spec, Cell: h.Cell, Verifier: h.Verifier, Params: &h.Params, Note: h.Note,
	})
	w.seq = 0
	return w, w.err
}

// chain encodes one line, adds it to the chain, and writes it.
func (w *Writer) chain(l *line) {
	b, err := json.Marshal(l)
	if err != nil {
		w.err = fmt.Errorf("chained line %d (%s) cannot be written: %v", w.seq+1, l.K, err)
		return
	}
	w.head = link(w.head, b)
	w.seq++
	w.write(b)
}

func (w *Writer) write(b []byte) {
	if _, err := w.out.Write(b); err != nil {
		w.err = err
		return
	}
	if err := w.out.WriteByte('\n'); err != nil {
		w.err = err
	}
}

// Input writes one input. The kernel calls it before it applies the input.
func (w *Writer) Input(in kernel.Input) {
	if w.err != nil {
		return
	}
	if w.open {
		w.err = fmt.Errorf("chained line %d: an input arrived before the verdict of the proposal before it", w.seq+1)
		return
	}
	l, err := inputLine(in)
	if err != nil {
		w.err = fmt.Errorf("chained line %d: %v", w.seq+1, err)
		return
	}
	w.chain(l)
	w.open = in.Kind.HasVerdict()
}

// Verdict writes the verdict of the last input.
func (w *Writer) Verdict(v kernel.Verdict) {
	if w.err != nil {
		return
	}
	if !w.open {
		w.err = fmt.Errorf("chained line %d: a verdict arrived with no proposal before it", w.seq+1)
		return
	}
	w.chain(verdictOf(v).line())
	w.open = false
}

// Checkpoint writes a signed checkpoint for the record so far, and flushes
// the record to the underlying writer.
//
// t is the kernel time and state is the state digest of the kernel. The
// caller must take both after the last input that the Writer received, and
// must not call Checkpoint from inside a kernel operation.
func (w *Writer) Checkpoint(t int64, state [32]byte) {
	if w.err != nil {
		return
	}
	if w.open {
		w.err = fmt.Errorf("checkpoint after chained line %d: the proposal has no verdict yet", w.seq)
		return
	}
	c := Checkpoint{Cell: w.cell, Seq: w.seq, T: t, Head: w.head, State: state}
	for _, k := range w.signers {
		c.Sign(k)
	}
	w.write(c.MarshalLine())
	if w.err == nil {
		w.err = w.out.Flush()
	}
}

// Head returns the number of the last chained line and the value of the
// chain after it.
func (w *Writer) Head() (seq uint64, head [32]byte) { return w.seq, w.head }

// Err returns the first error, if any.
func (w *Writer) Err() error { return w.err }

// Close flushes the record and returns the first error, if any. It does not
// close the underlying writer.
func (w *Writer) Close() error {
	if err := w.out.Flush(); err != nil && w.err == nil {
		w.err = err
	}
	return w.err
}
