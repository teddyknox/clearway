package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"os"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/teddyknox/clearway/internal/record"
)

const sessionVector = "../../conformance/kernel-session-0.2.rec"

// vector reads a published record: its header line, and its steps.
func vector(t testing.TB, name string) (header []byte, steps []Step) {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	header, steps, err = Steps(f)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return header, steps
}

func keys(t testing.TB, n int) (pub []string, priv []ed25519.PrivateKey) {
	t.Helper()
	for i := 0; i < n; i++ {
		seed := bytes.Repeat([]byte{byte(i + 1)}, ed25519.SeedSize)
		k := ed25519.NewKeyFromSeed(seed)
		priv = append(priv, k)
		pub = append(pub, record.FormatPublicKey(k.Public().(ed25519.PublicKey)))
	}
	return pub, priv
}

// start returns an application that has run InitChain.
func start(t testing.TB, a *App, header []byte, pub []string, every int64) *App {
	t.Helper()
	state, err := json.Marshal(Genesis{Header: string(header), Keys: pub, CheckpointEvery: every})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.InitChain(context.Background(), &abci.InitChainRequest{AppStateBytes: state}); err != nil {
		t.Fatal(err)
	}
	return a
}

// block gives one block to an application and commits it.
func block(t testing.TB, a *App, height int64, txs ...[]byte) *abci.FinalizeBlockResponse {
	t.Helper()
	res, err := a.FinalizeBlock(context.Background(), &abci.FinalizeBlockRequest{Height: height, Txs: txs})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Commit(context.Background(), &abci.CommitRequest{}); err != nil {
		t.Fatal(err)
	}
	return res
}

// Two replicas get the blocks of a published record. One runs the kernel
// program as translated code and one in the interpreter alone. After every
// block the two have the same application hash. The verdicts are those of
// the record, and at each checkpoint of the record the replicas have its
// number of lines, its kernel time, its chain value, and its state digest.
func TestReplicasReproduceTheRecord(t *testing.T) {
	header, steps := vector(t, sessionVector)
	pub, _ := keys(t, 4)
	a := start(t, &App{}, header, pub, 0)
	b := start(t, &App{Interpret: true}, header, pub, 0)
	height, n, verdicts := int64(0), uint64(0), 0
	for _, s := range steps {
		var got [][]byte
		for _, batch := range s.Batches {
			height++
			tx := InputTx(n, batch)
			n++
			ra, rb := block(t, a, height, tx), block(t, b, height, tx)
			if !bytes.Equal(ra.AppHash, rb.AppHash) || !bytes.Equal(ra.TxResults[0].Data, rb.TxResults[0].Data) {
				t.Fatalf("block %d: the two replicas differ", height)
			}
			if ra.TxResults[0].Code != CodeOK {
				t.Fatalf("block %d: code %d", height, ra.TxResults[0].Code)
			}
			if d := ra.TxResults[0].Data; len(d) > 0 {
				got = append(got, bytes.Split(d, []byte{'\n'})...)
			}
		}
		if !bytes.Equal(bytes.Join(got, []byte{'\n'}), bytes.Join(s.Verdicts, []byte{'\n'})) {
			t.Fatalf("before line %d: the replicas gave the verdicts %q; the record has %q", s.Want.Seq, got, s.Verdicts)
		}
		verdicts += len(got)
		c := a.Checkpoint()
		if c.Seq != s.Want.Seq || c.T != s.Want.T || c.Head != s.Want.Head || c.State != s.Want.State || c.Cell != s.Want.Cell {
			t.Fatalf("the checkpoint of the replica is %+v; the record has %+v", c, s.Want)
		}
		if !bytes.Equal(AppHash(s.Want), block(t, a, height+1).AppHash) {
			t.Fatal("the application hash is not the hash of the checkpoint of the record")
		}
		block(t, b, height+1)
		height++
	}
	if verdicts != 11 {
		t.Fatalf("%d verdicts, want the 11 of the vector", verdicts)
	}
}

// The replicas sign checkpoints, and the signatures travel as transactions.
// The record that a replica writes then passes the check of docs/record.md
// with the keys of the replicas and a quorum of three, and fails it with a
// quorum that the signatures do not reach.
func TestSignedRecord(t *testing.T) {
	header, steps := vector(t, sessionVector)
	pub, priv := keys(t, 4)
	var pending []record.Checkpoint
	a := start(t, &App{Journal: &Journal{}}, header, pub, 0)
	a.OnCheckpoint = func(c record.Checkpoint) { pending = append(pending, c) }
	height, n := int64(0), uint64(0)
	results := map[uint32]int{}
	for _, s := range steps {
		for _, batch := range s.Batches {
			// The block carries the signatures of three replicas for the
			// checkpoints before it, and some that must not count.
			var txs [][]byte
			for _, c := range pending {
				for _, k := range priv[:3] {
					txs = append(txs, SignatureTx(c, k))
				}
				txs = append(txs, SignatureTx(c, priv[0]))                                  // a second signature of one replica
				txs = append(txs, SignatureTx(c, ed25519.NewKeyFromSeed(make([]byte, 32)))) // not a replica
				wrong := c
				wrong.T++
				txs = append(txs, SignatureTx(wrong, priv[3])) // a signature of another statement
			}
			pending = nil
			height++
			res := block(t, a, height, append(txs, InputTx(n, batch))...)
			n++
			for _, r := range res.TxResults {
				results[r.Code]++
			}
		}
	}
	var rec bytes.Buffer
	if _, err := a.Journal.WriteTo(&rec); err != nil {
		t.Fatal(err)
	}
	trusted := make([]ed25519.PublicKey, len(priv))
	for i, k := range priv {
		trusted[i] = k.Public().(ed25519.PublicKey)
	}
	rep, err := record.Verify(bytes.NewReader(rec.Bytes()), record.Options{Trusted: trusted, Quorum: 3})
	if err != nil {
		t.Fatal(err)
	}
	// The last checkpoint has no signature yet: no later block carried one.
	for _, f := range rep.Failures {
		if f.Class != record.Signature || f.Line < rep.Lines {
			t.Errorf("the record of the replica with a quorum of 3: %v", f)
		}
	}
	if rep.Checkpoints < 10 || !rep.ReplayDone || len(rep.Signers) != 3 {
		t.Fatalf("%d checkpoints, replay done %v, signers %+v", rep.Checkpoints, rep.ReplayDone, rep.Signers)
	}
	if rep4, _ := record.Verify(bytes.NewReader(rec.Bytes()), record.Options{Trusted: trusted, Quorum: 4, NoReplay: true}); len(rep4.Failures) < rep.Checkpoints-1 {
		t.Fatalf("a quorum of 4 failed %d checkpoints of %d, and three replicas signed", len(rep4.Failures), rep.Checkpoints)
	}
	if results[CodeNoSigner] != 3*(rep.Checkpoints-1) || results[CodeOK] < 3*(rep.Checkpoints-1) {
		t.Fatalf("results by code: %v, with %d checkpoints", results, rep.Checkpoints)
	}
	t.Logf("%d chained lines, %d checkpoints, each but the last signed by 3 replicas; results by code %v", rep.Lines, rep.Checkpoints, results)
}

// A batch that is not the next one, and a batch with a line that is not an
// input, change nothing: the replica has the application hash that it had.
func TestBadTransactions(t *testing.T) {
	header, steps := vector(t, sessionVector)
	pub, _ := keys(t, 4)
	a := start(t, &App{}, header, pub, 0)
	all := Batches(steps)
	good := all[0]
	first := block(t, a, 1, InputTx(0, good))
	for name, c := range map[string]struct {
		tx   []byte
		code uint32
	}{
		"a batch from the past":         {InputTx(0, good), CodeOrder},
		"a batch from the future":       {InputTx(2, good), CodeOrder},
		"a line that is not an input":   {InputTx(1, [][]byte{good[0], []byte(`{"k":"verdict","ok":true,"contract":9}`)}), CodeBad},
		"a line that is not JSON":       {InputTx(1, [][]byte{[]byte("advance")}), CodeBad},
		"a number with a leading zero":  {append([]byte("I01\n"), good[0]...), CodeBad},
		"no input":                      {[]byte("I1\n"), CodeBad},
		"an unknown kind":               {[]byte("X"), CodeBad},
		"an empty transaction":          {nil, CodeBad},
		"a signature that is not JSON":  {[]byte("S{"), CodeBad},
		"a signature for no checkpoint": {[]byte(`S{"seq":99999,"key":"` + pub[0] + `","sig":"00"}`), CodeNoSigner},
	} {
		check, _ := a.CheckTx(context.Background(), &abci.CheckTxRequest{Tx: c.tx})
		// A signature for a checkpoint that the replica has not reached may
		// wait in the mempool: the replica may be behind.
		if check.Code == CodeOK && name != "a batch from the future" && name != "a signature for no checkpoint" {
			t.Errorf("%s: CheckTx accepts it", name)
		}
		res := block(t, a, a.height+1, c.tx)
		if res.TxResults[0].Code != c.code || len(res.TxResults[0].Data) != 0 {
			t.Errorf("%s: code %d, want %d", name, res.TxResults[0].Code, c.code)
		}
		if !bytes.Equal(res.AppHash, first.AppHash) {
			t.Errorf("%s: the application hash changed", name)
		}
	}
	// The next batch still applies.
	if res := block(t, a, a.height+1, InputTx(1, all[1])); res.TxResults[0].Code != CodeOK || bytes.Equal(res.AppHash, first.AppHash) {
		t.Fatalf("the next batch: code %d", res.TxResults[0].Code)
	}
}

// A proposer puts the batches in the order of their numbers and leaves out
// what cannot follow. A replica refuses a block that does otherwise.
func TestProposals(t *testing.T) {
	header, steps := vector(t, sessionVector)
	pub, priv := keys(t, 4)
	a := start(t, &App{}, header, pub, 0)
	b := Batches(steps)
	var marks []record.Checkpoint
	a.OnCheckpoint = func(c record.Checkpoint) { marks = append(marks, c) }
	block(t, a, 1, InputTx(0, b[0]))
	sig := SignatureTx(marks[0], priv[1])
	tx1, tx2, tx3 := InputTx(1, b[1]), InputTx(2, b[2]), InputTx(3, b[3])

	prep, err := a.PrepareProposal(context.Background(), &abci.PrepareProposalRequest{
		MaxTxBytes: 1 << 20,
		Txs:        [][]byte{tx2, InputTx(0, b[0]), sig, tx1, tx1, InputTx(4, b[4]) /* 3 is missing */},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]byte{sig, tx1, tx2}; !equal(prep.Txs, want) {
		t.Fatalf("the proposal has %d transactions, want the signature and batches 1 and 2", len(prep.Txs))
	}
	for name, c := range map[string]struct {
		txs    [][]byte
		accept bool
	}{
		"its own proposal":            {prep.Txs, true},
		"an empty block":              {nil, true},
		"the next batches in order":   {[][]byte{tx1, sig, tx2, tx3}, true},
		"batches out of order":        {[][]byte{tx2, tx1}, false},
		"a batch from the past":       {[][]byte{InputTx(0, b[0])}, false},
		"a gap":                       {[][]byte{tx1, tx3}, false},
		"a line that is not an input": {[][]byte{InputTx(1, [][]byte{[]byte("{}")})}, false},
		"an unknown transaction":      {[][]byte{[]byte("X")}, false},
	} {
		res, err := a.ProcessProposal(context.Background(), &abci.ProcessProposalRequest{Txs: c.txs})
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Status == abci.PROCESS_PROPOSAL_STATUS_ACCEPT; got != c.accept {
			t.Errorf("%s: accepted %v, want %v", name, got, c.accept)
		}
	}
}

func equal(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// A faulty replica reports another state and other verdicts than it has. Its
// application hash and its results then differ from those of a correct
// replica, which is what the other replicas vote on.
func TestLyingReplica(t *testing.T) {
	header, steps := vector(t, sessionVector)
	pub, _ := keys(t, 4)
	honest := start(t, &App{}, header, pub, 0)
	liar := start(t, &App{Lie: func(h int64) bool { return h >= 2 }}, header, pub, 0)
	verdicts := 0
	for h, batch := range Batches(steps)[:40] {
		tx := InputTx(uint64(h), batch)
		rh, rl := block(t, honest, int64(h+1), tx), block(t, liar, int64(h+1), tx)
		lies := h+1 >= 2
		if bytes.Equal(rh.AppHash, rl.AppHash) == lies {
			t.Fatalf("block %d: the application hashes are equal: %v", h+1, !lies)
		}
		if d := rh.TxResults[0].Data; len(d) > 0 && lies {
			verdicts++
			if bytes.Equal(d, rl.TxResults[0].Data) {
				t.Fatalf("block %d: the faulty replica reports the verdict of the correct one", h+1)
			}
		}
	}
	if verdicts == 0 {
		t.Fatal("no block had a verdict to lie about")
	}
}
