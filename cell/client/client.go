// Package client is what a party outside a cell uses: it gives inputs to the
// cell, reads the verdicts of the kernel, and gets a certificate for them.
//
// A Client talks to one replica. It does not have to trust that replica for
// a verdict: a Certificate carries the signatures of the replicas, and
// Certificate.Verify checks it with the public keys alone.
// docs/replication.md describes the arrangement.
package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	rpcclient "github.com/cometbft/cometbft/rpc/client"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cometbft/cometbft/types"

	"github.com/teddyknox/clearway/cell/app"
	"github.com/teddyknox/clearway/internal/record"
)

// A Client talks to one replica of a cell.
type Client struct {
	rpc rpcclient.Client
}

// New returns a client that talks to a replica through rpc.
func New(rpc rpcclient.Client) *Client { return &Client{rpc: rpc} }

// Dial returns a client that talks to the replica that answers queries at
// addr, for example "tcp://127.0.0.1:26657".
func Dial(addr string) (*Client, error) {
	rpc, err := rpchttp.New(addr)
	if err != nil {
		return nil, err
	}
	return New(rpc), nil
}

// poll is the time between two questions to the replica while a client
// waits for a block.
const poll = 2 * time.Millisecond

func wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(poll):
		return nil
	}
}

// Ready waits until the replica takes part in the consensus of the cell. A
// replica that has just started first gets the blocks that it does not
// have from the other replicas, and takes no input in that time.
func (c *Client) Ready(ctx context.Context) error {
	for {
		st, err := c.rpc.Status(ctx)
		if err != nil {
			return err
		}
		if !st.SyncInfo.CatchingUp {
			return nil
		}
		if err := wait(ctx); err != nil {
			return fmt.Errorf("the replica still gets old blocks: %w", err)
		}
	}
}

// A Result is what the cell did with one batch of inputs.
type Result struct {
	N        uint64   // the number of the batch
	Height   int64    // the block that holds the batch
	Index    int      // the place of the batch in that block
	Verdicts [][]byte // the verdict lines of the kernel for the proposals of the batch
}

// Next returns the number of the next batch of inputs that the cell applies,
// as the replica knows it.
func (c *Client) Next(ctx context.Context) (uint64, error) {
	res, err := c.rpc.ABCIQuery(ctx, "next", nil)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(string(res.Response.Value), 10, 64)
}

// Checkpoint returns the checkpoint of the record after the last block that
// the replica has applied.
func (c *Client) Checkpoint(ctx context.Context) (record.Checkpoint, error) {
	return c.checkpoint(ctx, "checkpoint")
}

// Signed returns the newest checkpoint that the replicas sign, with the
// signatures that the replica has for it. It returns an error if the
// replicas have no checkpoint to sign yet.
func (c *Client) Signed(ctx context.Context) (record.Checkpoint, error) {
	return c.checkpoint(ctx, "signed")
}

func (c *Client) checkpoint(ctx context.Context, path string) (record.Checkpoint, error) {
	res, err := c.rpc.ABCIQuery(ctx, path, nil)
	if err != nil {
		return record.Checkpoint{}, err
	}
	if res.Response.Code != app.CodeOK {
		return record.Checkpoint{}, fmt.Errorf("the replica has no answer: %s", res.Response.Log)
	}
	cs, err := record.ReadCheckpoints(bytes.NewReader(res.Response.Value))
	if err != nil {
		return record.Checkpoint{}, err
	}
	if len(cs) != 1 {
		return record.Checkpoint{}, errors.New("the replica did not answer with one checkpoint line")
	}
	return cs[0], nil
}

// Submit gives batch n of the inputs of the cell to the replica and waits
// until a block holds it.
//
// The batches of a cell are numbered from 0, and the cell applies each
// number once, in order. Submit returns an error if the replica refuses the
// batch, if the cell did not apply it, and if no block held it in time. In
// the last case the batch stays with the replica, and a later block can
// hold it.
func (c *Client) Submit(ctx context.Context, n uint64, lines [][]byte) (Result, error) {
	res, err := c.rpc.BroadcastTxCommit(ctx, app.InputTx(n, lines))
	if err != nil {
		return Result{}, err
	}
	if res.CheckTx.Code != app.CodeOK {
		return Result{}, fmt.Errorf("the replica refused batch %d: code %d", n, res.CheckTx.Code)
	}
	if res.TxResult.Code != app.CodeOK {
		return Result{}, fmt.Errorf("the cell did not apply batch %d: code %d", n, res.TxResult.Code)
	}
	// The place of the batch in its block is not in the answer.
	blk, err := c.rpc.Block(ctx, &res.Height)
	if err != nil {
		return Result{}, err
	}
	tx := app.InputTx(n, lines)
	for i, have := range blk.Block.Txs {
		if bytes.Equal(have, tx) {
			return Result{N: n, Height: res.Height, Index: i, Verdicts: splitLines(res.TxResult.Data)}, nil
		}
	}
	return Result{}, fmt.Errorf("block %d does not hold batch %d", res.Height, n)
}

// Find returns what the cell did with batch n, which a client gave to the
// cell before. It is for a batch whose Submit ended with no answer. Find
// returns an error if no block holds the batch yet, and if the cell did not
// apply it.
func (c *Client) Find(ctx context.Context, n uint64, lines [][]byte) (Result, error) {
	tx := types.Tx(app.InputTx(n, lines))
	res, err := c.rpc.Tx(ctx, tx.Hash(), false)
	if err != nil {
		return Result{}, err
	}
	if res.TxResult.Code != app.CodeOK {
		return Result{}, fmt.Errorf("the cell did not apply batch %d: code %d", n, res.TxResult.Code)
	}
	return Result{N: n, Height: res.Height, Index: int(res.Index), Verdicts: splitLines(res.TxResult.Data)}, nil
}

// Feed gives batches to the cell in order, numbered from first, and calls
// got for each of them, in order, when a block holds it. At most window
// batches wait for a block at one time, so one block can hold several.
//
// Feed returns the error of got, an error of the replica, and the error of
// ctx when ctx ends before a block holds the last batch.
func (c *Client) Feed(ctx context.Context, first uint64, batches [][][]byte, window int, got func(Result) error) error {
	if window < 1 {
		return errors.New("the window must be 1 or more")
	}
	st, err := c.rpc.Status(ctx)
	if err != nil {
		return err
	}
	scanned := st.SyncInfo.LatestBlockHeight
	sent, done := 0, 0
	for done < len(batches) {
		for sent < len(batches) && sent-done < window {
			n := first + uint64(sent)
			res, err := c.rpc.BroadcastTxSync(ctx, app.InputTx(n, batches[sent]))
			if err != nil {
				return fmt.Errorf("batch %d: %w", n, err)
			}
			if res.Code != app.CodeOK {
				return fmt.Errorf("the replica refused batch %d: code %d", n, res.Code)
			}
			sent++
		}
		if st, err = c.rpc.Status(ctx); err != nil {
			return err
		}
		if st.SyncInfo.LatestBlockHeight == scanned {
			if err := wait(ctx); err != nil {
				return fmt.Errorf("no block holds batch %d: %w", first+uint64(done), err)
			}
			continue
		}
		latest := st.SyncInfo.LatestBlockHeight
		for h := scanned + 1; h <= latest; h++ {
			results, err := c.results(ctx, h)
			if err != nil && h == latest {
				// The replica has stored the last block and has not applied
				// it yet: its results come a moment later.
				if err := wait(ctx); err != nil {
					return fmt.Errorf("no results for block %d: %w", h, err)
				}
				break
			}
			if err != nil {
				return err
			}
			for _, r := range results {
				if r.N < first+uint64(done) {
					continue // a batch from before this call
				}
				if r.N != first+uint64(done) || done == sent {
					return fmt.Errorf("block %d holds batch %d, and the next batch of this client is %d: another client feeds the cell", h, r.N, first+uint64(done))
				}
				done++
				if err := got(r); err != nil {
					return err
				}
			}
			scanned = h
		}
	}
	return nil
}

// results returns what the cell did with the batches of block h that it
// applied.
func (c *Client) results(ctx context.Context, h int64) ([]Result, error) {
	blk, err := c.rpc.Block(ctx, &h)
	if err != nil {
		return nil, err
	}
	res, err := c.rpc.BlockResults(ctx, &h)
	if err != nil {
		return nil, err
	}
	if len(res.TxResults) != len(blk.Block.Txs) {
		return nil, fmt.Errorf("block %d has %d transactions and %d results", h, len(blk.Block.Txs), len(res.TxResults))
	}
	var out []Result
	for i, tx := range blk.Block.Txs {
		n, _, err := app.ParseInputTx(tx)
		if err != nil || res.TxResults[i].Code != app.CodeOK {
			continue
		}
		out = append(out, Result{N: n, Height: h, Index: i, Verdicts: splitLines(res.TxResults[i].Data)})
	}
	return out, nil
}

func splitLines(b []byte) [][]byte {
	if len(b) == 0 {
		return nil
	}
	return bytes.Split(b, []byte{'\n'})
}

// Replayed says what Replay did.
type Replayed struct {
	Batches  int   // the batches of inputs that the cell applied
	Verdicts int   // the verdict lines that the cell gave
	First    int64 // the block that holds the first batch
	Last     int64 // the block that holds the last batch
}

// Replay gives the inputs of a record to a cell that has no input yet, and
// checks the cell against the record. steps are the steps of the record, as
// app.Steps returns them.
//
// Replay returns an error if the cell has an input already, if the cell
// gives a verdict line that the record does not have at that place, and if,
// at a checkpoint of the record, the replica does not have its number of
// lines, kernel time, chain value, and state digest.
func (c *Client) Replay(ctx context.Context, steps []app.Step, window int) (Replayed, error) {
	var out Replayed
	next, err := c.Next(ctx)
	if err != nil {
		return out, err
	}
	if next != 0 {
		return out, fmt.Errorf("the cell has %d batches of inputs already: a record replays from its first line", next)
	}
	for _, s := range steps {
		var got [][]byte
		err := c.Feed(ctx, uint64(out.Batches), s.Batches, window, func(r Result) error {
			if out.First == 0 {
				out.First = r.Height
			}
			out.Last = r.Height
			got = append(got, r.Verdicts...)
			return nil
		})
		if err != nil {
			return out, err
		}
		out.Batches += len(s.Batches)
		out.Verdicts += len(got)
		if !bytes.Equal(bytes.Join(got, []byte{'\n'}), bytes.Join(s.Verdicts, []byte{'\n'})) {
			return out, fmt.Errorf("before line %d: the cell gave the verdicts %q; the record has %q", s.Want.Seq, got, s.Verdicts)
		}
		have, err := c.Checkpoint(ctx)
		if err != nil {
			return out, err
		}
		w := s.Want
		if have.Cell != w.Cell || have.Seq != w.Seq || have.T != w.T || have.Head != w.Head || have.State != w.State {
			return out, fmt.Errorf("at line %d of the record: the replica has the checkpoint %s; the record has %s",
				w.Seq, have.MarshalLine(), unsigned(w).MarshalLine())
		}
	}
	return out, nil
}

func unsigned(c record.Checkpoint) *record.Checkpoint {
	c.Sigs = nil
	return &c
}
