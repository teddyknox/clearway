package app

import (
	"io"
	"sync"

	"github.com/teddyknox/clearway/internal/record"
)

// A Journal keeps the record of a cell as one replica builds it: the header,
// every input line and verdict line in order, and the checkpoints that the
// replicas sign, with the signatures that have arrived.
//
// WriteTo writes it as a record of docs/record.md. A party outside the cell
// checks that record with `record verify`, the public keys of the replicas,
// and a quorum: it needs nothing of CometBFT.
//
// A Journal holds the record in memory. It is safe for concurrent use.
type Journal struct {
	mu    sync.Mutex
	lines [][]byte            // chained lines; lines[0] is the header
	marks []record.Checkpoint // in order of Seq
}

func (j *Journal) reset(header []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines, j.marks = [][]byte{append([]byte(nil), header...)}, nil
}

func (j *Journal) line(raw []byte) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, append([]byte(nil), raw...))
}

func (j *Journal) mark(c record.Checkpoint) {
	j.mu.Lock()
	defer j.mu.Unlock()
	c.Sigs = nil
	j.marks = append(j.marks, c)
}

// sign replaces the signatures of the checkpoint with the number of c.
func (j *Journal) sign(c record.Checkpoint) {
	j.mu.Lock()
	defer j.mu.Unlock()
	for i := len(j.marks) - 1; i >= 0; i-- {
		if j.marks[i].Seq == c.Seq {
			j.marks[i].Sigs = append([]record.Sig(nil), c.Sigs...)
			return
		}
	}
}

// Lines returns the number of chained lines.
func (j *Journal) Lines() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return len(j.lines)
}

// Checkpoints returns the checkpoints that the replicas sign, with the
// signatures so far.
func (j *Journal) Checkpoints() []record.Checkpoint {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]record.Checkpoint, len(j.marks))
	for i, c := range j.marks {
		c.Sigs = append([]record.Sig(nil), c.Sigs...)
		out[i] = c
	}
	return out
}

// WriteTo writes the record. A checkpoint stands after the chained line
// with its number.
func (j *Journal) WriteTo(w io.Writer) (int64, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var total int64
	put := func(b []byte) error {
		n, err := w.Write(append(b[:len(b):len(b)], '\n'))
		total += int64(n)
		return err
	}
	next := 0
	for i, l := range j.lines {
		if err := put(l); err != nil {
			return total, err
		}
		for next < len(j.marks) && j.marks[next].Seq == uint64(i) {
			if err := put(j.marks[next].MarshalLine()); err != nil {
				return total, err
			}
			next++
		}
	}
	return total, nil
}
