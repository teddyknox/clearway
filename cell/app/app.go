// Package app is the kernel of one cell as an application of CometBFT.
//
// CometBFT gives every replica the same transactions in the same order. The
// application gives them to the kernel program (docs/vm.md), which every
// replica runs in the same machine. So the replicas reach the same state, and
// the application hash that they vote on says so: it is a hash of the
// checkpoint of the record (docs/record.md), which covers every chained line
// and the state digest of the kernel.
//
// docs/replication.md describes the whole arrangement.
package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/teddyknox/clearway/internal/kimage"
	"github.com/teddyknox/clearway/internal/record"
)

// Genesis is the state of the application before the first block: the
// app_state of the genesis file of a cell.
type Genesis struct {
	// Header is the header line of the record of the cell. It names the cell
	// and gives the parameters of the kernel.
	Header string `json:"header"`
	// Keys are the Ed25519 public keys of the replicas, in hexadecimal. A
	// replica signs checkpoints of the record with its key.
	Keys []string `json:"keys"`
	// CheckpointEvery is the kernel time between two checkpoints that the
	// replicas sign (µs). With 0, they sign a checkpoint after every block
	// that has an input.
	CheckpointEvery int64 `json:"checkpoint_every"`
}

// GenesisKeys returns the public keys of the replicas that the genesis file
// of a cell names. They are the keys that `record verify` takes for the
// record of the cell.
func GenesisKeys(file string) ([]ed25519.PublicKey, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var doc struct {
		AppState Genesis `json:"app_state"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	keys := make([]ed25519.PublicKey, len(doc.AppState.Keys))
	for i, k := range doc.AppState.Keys {
		if keys[i], err = record.ParsePublicKey(k); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
	}
	return keys, nil
}

// The codes of a transaction result. A code other than CodeOK means that the
// transaction changed nothing.
const (
	CodeOK       uint32 = 0
	CodeBad      uint32 = 1 // not a transaction of this application, or an input that is not a line of a record
	CodeOrder    uint32 = 2 // a batch of inputs that is not the next one
	CodeNoSigner uint32 = 3 // a signature that does not count: wrong key, wrong checkpoint, or a repeat
)

// A transaction is one of two kinds, by its first byte.
const (
	tagInputs    = 'I' // "I", the number of the batch in decimal, a newline, and input lines of a record with a newline between them
	tagSignature = 'S' // "S", and a JSON object: see Signature
)

// InputTx returns the transaction for batch n of the inputs of a cell. The
// batches of a cell are numbered from 0, and the application applies them in
// that order. Each line is an input line of a record, without a newline.
func InputTx(n uint64, lines [][]byte) []byte {
	tx := append([]byte{tagInputs}, strconv.FormatUint(n, 10)...)
	tx = append(tx, '\n')
	return append(tx, bytes.Join(lines, []byte{'\n'})...)
}

// ParseInputTx reads a transaction of InputTx: the number of the batch and
// its lines. It does not check the lines.
func ParseInputTx(tx []byte) (n uint64, lines [][]byte, err error) {
	if len(tx) == 0 || tx[0] != tagInputs {
		return 0, nil, errors.New("not a batch of inputs")
	}
	head, rest, found := bytes.Cut(tx[1:], []byte{'\n'})
	if !found || len(rest) == 0 {
		return 0, nil, errors.New("a batch of inputs with no input")
	}
	if n, err = strconv.ParseUint(string(head), 10, 64); err != nil || strconv.FormatUint(n, 10) != string(head) {
		return 0, nil, errors.New("a batch of inputs with no number")
	}
	return n, bytes.Split(rest, []byte{'\n'}), nil
}

// Signature is the signature of one replica for one checkpoint of the record.
type Signature struct {
	Seq uint64 `json:"seq"` // the number of the last chained line that the checkpoint covers
	Key string `json:"key"` // the public key of the replica, in hexadecimal
	Sig string `json:"sig"` // the Ed25519 signature of the checkpoint, in hexadecimal
}

// SignatureTx returns the transaction that carries the signature of key for
// the checkpoint c.
func SignatureTx(c record.Checkpoint, key ed25519.PrivateKey) []byte {
	signed := c
	signed.Sigs = nil
	signed.Sign(key)
	b, _ := json.Marshal(Signature{Seq: c.Seq, Key: signed.Sigs[0].Key, Sig: signed.Sigs[0].Sig})
	return append([]byte{tagSignature}, b...)
}

func parseSignatureTx(tx []byte) (Signature, error) {
	var s Signature
	if len(tx) == 0 || tx[0] != tagSignature {
		return s, errors.New("not a signature")
	}
	dec := json.NewDecoder(bytes.NewReader(tx[1:]))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		return s, err
	}
	return s, nil
}

// App is the application of one replica. It holds one kernel, as the kernel
// program in its machine, and the chain of the record of the cell.
//
// The methods of the ABCI interface are safe for concurrent use: CometBFT
// calls them from several goroutines.
type App struct {
	abci.BaseApplication

	// Interpret makes the machine interpret every instruction of the kernel
	// program, with no translated code. Set it before the first call.
	Interpret bool
	// Journal, if not nil, receives the record of the cell as the application
	// builds it. Set it before the first call.
	Journal *Journal
	// OnCheckpoint, if not nil, receives each checkpoint that the replicas
	// are to sign, after the block that made it is committed. The replica
	// answers with a SignatureTx.
	OnCheckpoint func(record.Checkpoint)
	// Lie, if not nil, makes the replica faulty: for a block at which Lie
	// returns true, the application reports a state that it does not have.
	// It is for tests of a cell with a faulty replica.
	Lie func(height int64) bool

	mu      sync.Mutex
	sess    *record.Session
	keys    map[string]ed25519.PublicKey
	every   int64
	next    uint64            // the number of the next batch of inputs
	height  int64             // the last block
	appHash []byte            // after the last block
	last    record.Checkpoint // after the last block
	marked  int64             // the kernel time of the last checkpoint to sign
	marks   []*record.Checkpoint
	fresh   *record.Checkpoint // a checkpoint to sign that the last block made
}

// keep is the number of checkpoints for which the application still takes
// signatures.
const keep = 64

// hashOf returns the application hash for a checkpoint of the record: the
// SHA-256 of its line, with no signature.
func hashOf(c record.Checkpoint) []byte {
	c.Sigs = nil
	sum := sha256.Sum256(c.MarshalLine())
	return sum[:]
}

// AppHash returns the application hash that a replica has after it reached
// the checkpoint c. A party that holds c can compare it with the application
// hash in a block header of the cell.
func AppHash(c record.Checkpoint) []byte { return hashOf(c) }

func (a *App) Info(context.Context, *abci.InfoRequest) (*abci.InfoResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return &abci.InfoResponse{
		Data: "clearway cell", Version: record.SpecVersion,
		LastBlockHeight: a.height, LastBlockAppHash: a.appHash,
	}, nil
}

// InitChain starts the kernel of the cell from the genesis state.
func (a *App) InitChain(_ context.Context, req *abci.InitChainRequest) (*abci.InitChainResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var g Genesis
	if err := json.Unmarshal(req.AppStateBytes, &g); err != nil {
		return nil, fmt.Errorf("the application state of the genesis file: %w", err)
	}
	if g.CheckpointEvery < 0 {
		return nil, errors.New("the application state of the genesis file: checkpoint_every is negative")
	}
	keys := map[string]ed25519.PublicKey{}
	for _, k := range g.Keys {
		pub, err := record.ParsePublicKey(k)
		if err != nil {
			return nil, fmt.Errorf("the application state of the genesis file: %w", err)
		}
		keys[record.FormatPublicKey(pub)] = pub
	}
	proc, err := kimage.Start(a.Interpret)
	if err != nil {
		return nil, err
	}
	sess, err := record.NewSession([]byte(g.Header), proc)
	if err != nil {
		return nil, fmt.Errorf("the header of the record in the genesis file: %w", err)
	}
	c, err := sess.Checkpoint()
	if err != nil {
		return nil, err
	}
	a.sess, a.keys, a.every = sess, keys, g.CheckpointEvery
	a.next, a.height, a.last, a.appHash = 0, 0, c, hashOf(c)
	a.marked, a.marks, a.fresh = 0, nil, nil
	if a.Journal != nil {
		a.Journal.reset([]byte(g.Header))
	}
	return &abci.InitChainResponse{AppHash: a.appHash}, nil
}

// checkSignature reports whether a signature would count now. A signature
// for a checkpoint that this replica has not reached yet may count later.
func (a *App) checkSignature(s Signature) (mark *record.Checkpoint, later bool, err error) {
	pub, known := a.keys[s.Key]
	if !known {
		return nil, false, errors.New("the key is not the key of a replica")
	}
	sig, err := hex.DecodeString(s.Sig)
	if err != nil {
		return nil, false, err
	}
	for _, m := range a.marks {
		if m.Seq != s.Seq {
			continue
		}
		for _, have := range m.Sigs {
			if have.Key == s.Key {
				return nil, false, errors.New("the replica has signed this checkpoint")
			}
		}
		if !m.SignedBy(pub, sig) {
			return nil, false, errors.New("the signature is not valid for the checkpoint")
		}
		return m, false, nil
	}
	if s.Seq > a.last.Seq {
		return nil, true, nil
	}
	return nil, false, errors.New("no checkpoint to sign has this number")
}

// CheckTx decides whether a transaction may wait in the mempool.
func (a *App) CheckTx(_ context.Context, req *abci.CheckTxRequest) (*abci.CheckTxResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	code := CodeBad
	switch {
	case len(req.Tx) == 0:
	case req.Tx[0] == tagInputs:
		n, lines, err := ParseInputTx(req.Tx)
		switch {
		case err != nil:
		case n < a.next:
			code = CodeOrder
		default:
			code = CodeOK
			for _, l := range lines {
				if record.CheckInput(l) != nil {
					code = CodeBad
				}
			}
		}
	case req.Tx[0] == tagSignature:
		if s, err := parseSignatureTx(req.Tx); err == nil {
			code = CodeNoSigner
			if _, _, err := a.checkSignature(s); err == nil {
				code = CodeOK
			}
		}
	}
	return &abci.CheckTxResponse{Code: code}, nil
}

// PrepareProposal chooses the transactions of a block that this replica
// proposes: every signature, and the batches of inputs in the order of their
// numbers, from the next one, with no gap.
func (a *App) PrepareProposal(_ context.Context, req *abci.PrepareProposalRequest) (*abci.PrepareProposalResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	type batch struct {
		n  uint64
		tx []byte
	}
	var out [][]byte
	var batches []batch
	size := int64(0)
	for _, tx := range req.Txs {
		if len(tx) == 0 {
			continue
		}
		if tx[0] == tagInputs {
			if n, _, err := ParseInputTx(tx); err == nil && n >= a.next {
				batches = append(batches, batch{n, tx})
			}
			continue
		}
		if size+int64(len(tx)) <= req.MaxTxBytes {
			out, size = append(out, tx), size+int64(len(tx))
		}
	}
	sort.SliceStable(batches, func(i, j int) bool { return batches[i].n < batches[j].n })
	want := a.next
	for _, b := range batches {
		if b.n < want {
			continue // the same batch twice
		}
		if b.n > want || size+int64(len(b.tx)) > req.MaxTxBytes {
			break
		}
		out, size, want = append(out, b.tx), size+int64(len(b.tx)), want+1
	}
	return &abci.PrepareProposalResponse{Txs: out}, nil
}

// ProcessProposal refuses a block whose batches of inputs are not the next
// ones in order. So a faulty proposer cannot change the order of the inputs.
func (a *App) ProcessProposal(_ context.Context, req *abci.ProcessProposalRequest) (*abci.ProcessProposalResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	want := a.next
	status := abci.PROCESS_PROPOSAL_STATUS_ACCEPT
	for _, tx := range req.Txs {
		if len(tx) == 0 || (tx[0] != tagInputs && tx[0] != tagSignature) {
			status = abci.PROCESS_PROPOSAL_STATUS_REJECT
			break
		}
		if tx[0] != tagInputs {
			continue
		}
		n, lines, err := ParseInputTx(tx)
		if err != nil || n != want {
			status = abci.PROCESS_PROPOSAL_STATUS_REJECT
			break
		}
		for _, l := range lines {
			if record.CheckInput(l) != nil {
				status = abci.PROCESS_PROPOSAL_STATUS_REJECT
			}
		}
		want++
	}
	return &abci.ProcessProposalResponse{Status: status}, nil
}

// FinalizeBlock applies the transactions of a block that the replicas have
// agreed on. The result of a batch of inputs is the verdict lines of the
// kernel for the proposals in it, with a newline between them.
//
// If the kernel program stops, FinalizeBlock returns its error, and the
// replica cannot continue. Every correct replica stops at the same block.
func (a *App) FinalizeBlock(_ context.Context, req *abci.FinalizeBlockRequest) (*abci.FinalizeBlockResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sess == nil {
		return nil, errors.New("the application has no kernel: InitChain did not run")
	}
	results := make([]*abci.ExecTxResult, len(req.Txs))
	inputs := false
	for i, tx := range req.Txs {
		res, err := a.deliver(tx)
		if err != nil {
			return nil, err
		}
		results[i] = res
		inputs = inputs || (res.Code == CodeOK && tx[0] == tagInputs)
	}
	c, err := a.sess.Checkpoint()
	if err != nil {
		return nil, err
	}
	a.height, a.last, a.appHash, a.fresh = req.Height, c, hashOf(c), nil
	due := inputs
	if a.every > 0 {
		due = c.T/a.every > a.marked/a.every
	}
	if due {
		m := c
		a.marked = c.T
		a.marks = append(a.marks, &m)
		if len(a.marks) > keep {
			a.marks = a.marks[len(a.marks)-keep:]
		}
		a.fresh = &m
		if a.Journal != nil {
			a.Journal.mark(c)
		}
	}
	hash := a.appHash
	if a.Lie != nil && a.Lie(req.Height) {
		// A faulty replica: it reports another state and other verdicts.
		hash = append([]byte(nil), hash...)
		hash[0] ^= 1
		for _, r := range results {
			if len(r.Data) > 0 {
				r.Data = append([]byte(nil), r.Data...)
				r.Data[len(r.Data)/2] ^= 1
			}
		}
		a.appHash = hash
	}
	return &abci.FinalizeBlockResponse{TxResults: results, AppHash: hash}, nil
}

func (a *App) deliver(tx []byte) (*abci.ExecTxResult, error) {
	fail := func(code uint32) (*abci.ExecTxResult, error) { return &abci.ExecTxResult{Code: code}, nil }
	if len(tx) == 0 {
		return fail(CodeBad)
	}
	switch tx[0] {
	case tagInputs:
		n, lines, err := ParseInputTx(tx)
		if err != nil {
			return fail(CodeBad)
		}
		if n != a.next {
			return fail(CodeOrder)
		}
		for _, l := range lines {
			if record.CheckInput(l) != nil {
				return fail(CodeBad)
			}
		}
		var verdicts [][]byte
		for _, l := range lines {
			v, err := a.sess.Apply(l)
			if err != nil {
				return nil, fmt.Errorf("the kernel program of this replica stopped: %w", err)
			}
			if a.Journal != nil {
				a.Journal.line(l)
			}
			if v != nil {
				verdicts = append(verdicts, v)
				if a.Journal != nil {
					a.Journal.line(v)
				}
			}
		}
		a.next++
		return &abci.ExecTxResult{Code: CodeOK, Data: bytes.Join(verdicts, []byte{'\n'})}, nil
	case tagSignature:
		s, err := parseSignatureTx(tx)
		if err != nil {
			return fail(CodeBad)
		}
		m, _, err := a.checkSignature(s)
		if err != nil || m == nil {
			return fail(CodeNoSigner)
		}
		m.Sigs = append(m.Sigs, record.Sig{Key: s.Key, Sig: s.Sig})
		if a.Journal != nil {
			a.Journal.sign(*m)
		}
		return &abci.ExecTxResult{Code: CodeOK}, nil
	}
	return fail(CodeBad)
}

// Commit ends a block. It hands a new checkpoint to the replica for its
// signature.
func (a *App) Commit(context.Context, *abci.CommitRequest) (*abci.CommitResponse, error) {
	a.mu.Lock()
	fresh, notify := a.fresh, a.OnCheckpoint
	a.fresh = nil
	a.mu.Unlock()
	if fresh != nil && notify != nil {
		c := *fresh
		c.Sigs = nil
		notify(c)
	}
	return &abci.CommitResponse{}, nil
}

// Query answers "checkpoint" with the line of the checkpoint after the last
// block, with no signature, "signed" with the line of the newest checkpoint
// that the replicas sign, with the signatures so far, and "next" with the
// number of the next batch of inputs, in decimal.
func (a *App) Query(_ context.Context, req *abci.QueryRequest) (*abci.QueryResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch req.Path {
	case "checkpoint":
		return &abci.QueryResponse{Value: a.last.MarshalLine(), Height: a.height}, nil
	case "next":
		return &abci.QueryResponse{Value: strconv.AppendUint(nil, a.next, 10), Height: a.height}, nil
	case "signed":
		if len(a.marks) == 0 {
			return &abci.QueryResponse{Code: CodeBad, Log: "no checkpoint yet"}, nil
		}
		return &abci.QueryResponse{Value: a.marks[len(a.marks)-1].MarshalLine(), Height: a.height}, nil
	}
	return &abci.QueryResponse{Code: CodeBad, Log: "unknown path"}, nil
}

// Checkpoint returns the checkpoint of the record after the last block.
func (a *App) Checkpoint() record.Checkpoint {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.last
}
