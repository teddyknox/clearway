package node

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/types"

	"github.com/teddyknox/clearway/cell/app"
	"github.com/teddyknox/clearway/cell/client"
)

// copyOf returns a certificate that shares nothing with c.
func copyOf(t testing.TB, c *client.Certificate) *client.Certificate {
	t.Helper()
	wire, err := c.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	back, err := client.UnmarshalCertificate(wire)
	if err != nil {
		t.Fatal(err)
	}
	return back
}

// forge returns a certificate that states data as the verdicts of the batch
// of good. The replicas with the keys sign it, and no other replica does: it
// is what faulty replicas can make when they work together.
func forge(t testing.TB, r *Replica, trust *client.Trust, good *client.Certificate, data []byte, keys ...crypto.PrivKey) *client.Certificate {
	t.Helper()
	c := copyOf(t, good)
	h := c.Header.Height
	res, err := r.rpc.BlockResults(context.Background(), &h)
	if err != nil {
		t.Fatal(err)
	}
	index := int(c.Tx.Proof.Index)
	results := make([]*abci.ExecTxResult, len(res.TxResults))
	for i, x := range res.TxResults {
		results[i] = abci.DeterministicExecTxResult(x)
	}
	results[index].Data = data
	c.Result = results[index]
	c.ResultProof = types.NewResults(results).ProveResult(index)
	c.Next.LastResultsHash = types.NewResults(results).Hash()
	c.Commit = &types.Commit{
		Height:     c.Next.Height,
		BlockID:    types.BlockID{Hash: c.Next.Hash(), PartSetHeader: good.Commit.BlockID.PartSetHeader},
		Signatures: make([]types.CommitSig, trust.Replicas.Size()),
	}
	for i := range c.Commit.Signatures {
		c.Commit.Signatures[i] = types.NewCommitSigAbsent()
	}
	for _, k := range keys {
		i, v := trust.Replicas.GetByAddress(k.PubKey().Address())
		if v == nil {
			t.Fatal("the key is not the key of a replica")
		}
		c.Commit.Signatures[i] = types.CommitSig{
			BlockIDFlag: types.BlockIDFlagCommit, ValidatorAddress: v.Address, Timestamp: good.Next.Time,
		}
		sig, err := k.Sign(c.Commit.VoteSignBytes(trust.ChainID, i))
		if err != nil {
			t.Fatal(err)
		}
		c.Commit.Signatures[i].Signature = sig
	}
	return c
}

// A party outside the cell gets a certificate for a batch from one replica,
// and checks it with the genesis file of the cell and nothing else. The
// certificate gives the batch and the verdicts of the record.
//
// A certificate that states another verdict, or another batch, fails the
// check. So does a certificate that one replica, or two, made up and
// signed. Three replicas of four can make one that passes: that is the
// limit of a cell of four, which tolerates one faulty replica.
func TestCertificate(t *testing.T) {
	header, steps := vector(t, sessionVector)
	homes, rs := cell(t, header, 0, Options{}, Options{}, Options{}, Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	trust, err := client.TrustFromGenesis(rs[0].GenesisFile())
	if err != nil {
		t.Fatal(err)
	}
	batches := app.Batches(steps)
	var want [][]byte
	for _, s := range steps {
		want = append(want, s.Verdicts...)
	}
	var results []client.Result
	if err := rs[1].Client().Feed(ctx, 0, batches, 8, func(r client.Result) error {
		results = append(results, r)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// The certificates come from another replica than the one that took
	// the inputs.
	var certs []*client.Certificate
	var got [][]byte
	for _, r := range results {
		if len(r.Verdicts) == 0 && r.N%40 != 0 {
			continue
		}
		cert, err := rs[2].Client().Certificate(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		cert = copyOf(t, cert) // as the party gets it: in JSON
		n, lines, verdicts, err := cert.Verify(trust)
		if err != nil {
			t.Fatalf("batch %d: %v", r.N, err)
		}
		if n != r.N || !bytes.Equal(bytes.Join(lines, []byte{'\n'}), bytes.Join(batches[r.N], []byte{'\n'})) {
			t.Fatalf("batch %d: the certificate is for batch %d with the lines %q", r.N, n, lines)
		}
		if len(verdicts) > 0 {
			got = append(got, verdicts...)
			certs = append(certs, cert)
		}
	}
	if !bytes.Equal(bytes.Join(got, []byte{'\n'}), bytes.Join(want, []byte{'\n'})) || len(certs) < 2 {
		t.Fatalf("the certificates give the verdicts %q; the record has %q", got, want)
	}

	good, other := certs[0], certs[len(certs)-1]
	if good.Header.Height == other.Header.Height {
		t.Fatal("the two certificates are for one block")
	}
	lie := bytes.Replace(good.Result.Data, []byte(`"ok":false`), []byte(`"ok":true`), 1)
	if bytes.Equal(lie, good.Result.Data) {
		lie = bytes.Replace(good.Result.Data, []byte(`"ok":true`), []byte(`"ok":false`), 1)
	}
	if bytes.Equal(lie, good.Result.Data) {
		t.Fatalf("the verdict %q has no field to change", good.Result.Data)
	}
	keys := make([]crypto.PrivKey, len(homes))
	for i, home := range homes {
		c := config(home, settings{})
		keys[i] = privval.LoadFilePV(c.PrivValidatorKeyFile(), c.PrivValidatorStateFile()).Key.PrivKey
	}
	elsewhere, err := Setup(t.TempDir(), 4, header, 0)
	if err != nil {
		t.Fatal(err)
	}
	strangers, err := client.TrustFromGenesis(config(elsewhere[0], settings{}).GenesisFile())
	if err != nil {
		t.Fatal(err)
	}

	change := func(f func(c *client.Certificate)) *client.Certificate {
		c := copyOf(t, good)
		f(c)
		return c
	}
	for name, c := range map[string]struct {
		cert  *client.Certificate
		trust *client.Trust
		why   string // a part of the error
	}{
		"another verdict": {
			change(func(c *client.Certificate) { c.Result.Data = lie }), trust, "the result is not in the second header"},
		"the verdict of another batch": {
			change(func(c *client.Certificate) { c.Result, c.ResultProof = other.Result, other.ResultProof }), trust, "the result is not in the second header"},
		"another batch": {
			change(func(c *client.Certificate) { c.Tx = other.Tx }), trust, "the batch is not in the block"},
		"a block that is not the block before": {
			change(func(c *client.Certificate) { c.Header = other.Header }), trust, "the second header does not follow the first"},
		"signatures for another block": {
			change(func(c *client.Certificate) { c.Commit = other.Commit }), trust, "the signatures are not for the second header"},
		"another chain": {
			good, &client.Trust{ChainID: "clearway-other", Replicas: trust.Replicas}, "it is for the chain"},
		"the replicas of another cell": {
			good, &client.Trust{ChainID: trust.ChainID, Replicas: strangers.Replicas}, "the signatures of the replicas"},
		"a verdict that one replica made up": {
			forge(t, rs[0], trust, good, lie, keys[3]), trust, "the signatures of the replicas"},
		"a verdict that two replicas made up": {
			forge(t, rs[0], trust, good, lie, keys[3], keys[1]), trust, "the signatures of the replicas"},
	} {
		_, _, _, err := c.cert.Verify(c.trust)
		if err == nil || !strings.Contains(err.Error(), c.why) {
			t.Errorf("%s: the check gives %v, want an error with %q", name, err, c.why)
		}
	}
	// Three replicas are a quorum. The check cannot tell their forgery from
	// a verdict of the cell: it passes, with the made-up verdict.
	_, _, verdicts, err := forge(t, rs[0], trust, good, lie, keys[0], keys[1], keys[3]).Verify(trust)
	if err != nil || !bytes.Equal(bytes.Join(verdicts, []byte{'\n'}), lie) {
		t.Fatalf("a certificate that three replicas made up: %v, verdicts %q", err, verdicts)
	}
	t.Logf("%d certificates checked; one is %d bytes of JSON", len(certs), func() int { b, _ := good.Marshal(); return len(b) }())
}
