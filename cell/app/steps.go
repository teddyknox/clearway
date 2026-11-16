package app

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/teddyknox/clearway/internal/record"
)

// A Step is the lines of a record from one of its checkpoints to the next:
// the input lines in batches, the verdict lines that the kernel gave, and
// the checkpoint that ends the step.
type Step struct {
	Batches  [][][]byte
	Verdicts [][]byte
	Want     record.Checkpoint
}

// Steps reads a record of docs/record.md and returns its header line and its
// steps. A batch ends before each line that moves the clock, so a batch is
// the inputs of one instant.
//
// Steps does not check the record. It returns an error if the record has no
// checkpoint, and if lines follow the last checkpoint.
func Steps(r io.Reader) (header []byte, steps []Step, err error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	checkpoints, err := record.ReadCheckpoints(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 64<<20)
	var cur Step
	var batch [][]byte
	flush := func() {
		if len(batch) > 0 {
			cur.Batches, batch = append(cur.Batches, batch), nil
		}
	}
	for first := true; sc.Scan(); first = false {
		l := bytes.Clone(sc.Bytes())
		switch {
		case first:
			header = l
		case bytes.HasPrefix(l, []byte(`{"k":"verdict"`)):
			cur.Verdicts = append(cur.Verdicts, l)
		case bytes.HasPrefix(l, []byte(`{"k":"checkpoint"`)):
			flush()
			if len(steps) == len(checkpoints) {
				return nil, nil, errors.New("the record has a checkpoint line that is not a checkpoint")
			}
			cur.Want = checkpoints[len(steps)]
			steps, cur = append(steps, cur), Step{}
		default:
			if bytes.HasPrefix(l, []byte(`{"k":"advance"`)) {
				flush()
			}
			batch = append(batch, l)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	if len(steps) == 0 || len(steps) != len(checkpoints) {
		return nil, nil, fmt.Errorf("the record has %d steps and %d checkpoints", len(steps), len(checkpoints))
	}
	if len(batch) > 0 || len(cur.Verdicts) > 0 {
		return nil, nil, errors.New("the record has lines after its last checkpoint")
	}
	return header, steps, nil
}

// Batches returns the batches of all the steps, in order.
func Batches(steps []Step) (out [][][]byte) {
	for _, s := range steps {
		out = append(out, s.Batches...)
	}
	return out
}
