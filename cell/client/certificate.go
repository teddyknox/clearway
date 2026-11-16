package client

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/merkle"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/types"

	"github.com/teddyknox/clearway/cell/app"
)

// A Certificate proves that a cell applied one batch of inputs and gave
// these verdicts for it. The proof is the signatures of the replicas: a
// party checks it with the public keys of the replicas, and needs no
// replica to do so.
//
// The certificate holds two block headers. The block of the first header
// holds the batch. The second header is the header of the next block: it
// holds the hash of the first header and the hash of the results of the
// first block. The commit is the signatures of the replicas for the second
// header. So a quorum of the replicas signed one value that fixes the batch,
// its place in the order of all batches, and the verdicts.
type Certificate struct {
	Header      *types.Header      `json:"header"`       // of the block that holds the batch
	Next        *types.Header      `json:"next"`         // of the block after it
	Commit      *types.Commit      `json:"commit"`       // the signatures of the replicas for Next
	Tx          types.TxProof      `json:"tx"`           // the batch, and the proof that the block of Header holds it
	Result      *abci.ExecTxResult `json:"result"`       // the result of the batch: its data is the verdict lines
	ResultProof merkle.Proof       `json:"result_proof"` // the proof that Next holds the hash of Result
}

// Trust is what a party must know of a cell to check a certificate: the
// name of the chain of the cell, and the replicas with their public keys.
type Trust struct {
	ChainID  string
	Replicas *types.ValidatorSet
}

// TrustFromGenesis returns the Trust that the genesis file of a cell gives.
// The replicas of a cell do not change after the genesis file.
func TrustFromGenesis(file string) (*Trust, error) {
	doc, err := types.GenesisDocFromFile(file)
	if err != nil {
		return nil, err
	}
	vals := make([]*types.Validator, len(doc.Validators))
	for i, v := range doc.Validators {
		vals[i] = types.NewValidator(v.PubKey, v.Power)
	}
	return &Trust{ChainID: doc.ChainID, Replicas: types.NewValidatorSet(vals)}, nil
}

// Certificate returns the certificate for the batch of r. It waits for the
// block after the block that holds the batch. The cell makes that block
// without a new input, because the batch changed the state of the cell.
func (c *Client) Certificate(ctx context.Context, r Result) (*Certificate, error) {
	h, next := r.Height, r.Height+1
	for {
		st, err := c.rpc.Status(ctx)
		if err != nil {
			return nil, err
		}
		if st.SyncInfo.LatestBlockHeight >= next {
			break
		}
		if err := wait(ctx); err != nil {
			return nil, fmt.Errorf("no block after block %d: %w", h, err)
		}
	}
	blk, err := c.rpc.Block(ctx, &h)
	if err != nil {
		return nil, err
	}
	res, err := c.rpc.BlockResults(ctx, &h)
	if err != nil {
		return nil, err
	}
	signed, err := c.rpc.Commit(ctx, &next)
	if err != nil {
		return nil, err
	}
	if r.Index < 0 || r.Index >= len(blk.Block.Txs) || len(res.TxResults) != len(blk.Block.Txs) {
		return nil, fmt.Errorf("block %d has no transaction %d", h, r.Index)
	}
	return &Certificate{
		Header:      &blk.Block.Header,
		Next:        signed.Header,
		Commit:      signed.Commit,
		Tx:          blk.Block.Txs.Proof(r.Index),
		Result:      abci.DeterministicExecTxResult(res.TxResults[r.Index]),
		ResultProof: types.NewResults(res.TxResults).ProveResult(r.Index),
	}, nil
}

// Verify checks the certificate against what the party knows of the cell,
// and returns the batch and its verdicts.
//
// Verify returns an error unless all of these hold: replicas with more than
// two thirds of the voting power signed the second header; the second
// header follows the first; the block of the first header holds the batch;
// the second header holds the hash of the result, at the place of the
// batch; and the cell applied the batch.
func (c *Certificate) Verify(t *Trust) (n uint64, lines, verdicts [][]byte, err error) {
	fail := func(format string, a ...any) (uint64, [][]byte, [][]byte, error) {
		return 0, nil, nil, fmt.Errorf("certificate: "+format, a...)
	}
	if c.Header == nil || c.Next == nil || c.Commit == nil || c.Result == nil {
		return fail("a part is missing")
	}
	if c.Header.ChainID != t.ChainID || c.Next.ChainID != t.ChainID {
		return fail("it is for the chain %q, not for %q", c.Next.ChainID, t.ChainID)
	}
	if c.Commit.Height != c.Next.Height || !bytes.Equal(c.Commit.BlockID.Hash, c.Next.Hash()) {
		return fail("the signatures are not for the second header")
	}
	if err := t.Replicas.VerifyCommitLight(t.ChainID, c.Commit.BlockID, c.Next.Height, c.Commit); err != nil {
		return fail("the signatures of the replicas: %v", err)
	}
	if c.Next.Height != c.Header.Height+1 || !bytes.Equal(c.Next.LastBlockID.Hash, c.Header.Hash()) {
		return fail("the second header does not follow the first")
	}
	if err := c.Tx.Validate(c.Header.DataHash); err != nil {
		return fail("the batch is not in the block: %v", err)
	}
	leaf, err := abci.DeterministicExecTxResult(c.Result).Marshal()
	if err != nil {
		return fail("the result: %v", err)
	}
	if err := c.ResultProof.Verify(c.Next.LastResultsHash, leaf); err != nil {
		return fail("the result is not in the second header: %v", err)
	}
	if c.ResultProof.Index != c.Tx.Proof.Index || c.ResultProof.Total != c.Tx.Proof.Total {
		return fail("the result is not the result of the batch")
	}
	if c.Result.Code != app.CodeOK {
		return fail("the cell did not apply the batch: code %d", c.Result.Code)
	}
	n, lines, err = app.ParseInputTx(c.Tx.Data)
	if err != nil {
		return fail("%v", err)
	}
	return n, lines, splitLines(c.Result.Data), nil
}

// Marshal returns the certificate as JSON.
func (c *Certificate) Marshal() ([]byte, error) { return cmtjson.Marshal(c) }

// UnmarshalCertificate reads a certificate that Marshal wrote. It does not
// check it: see Verify.
func UnmarshalCertificate(b []byte) (*Certificate, error) {
	var c Certificate
	if err := cmtjson.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.Header == nil || c.Next == nil || c.Commit == nil || c.Result == nil {
		return nil, errors.New("certificate: a part is missing")
	}
	return &c, nil
}
