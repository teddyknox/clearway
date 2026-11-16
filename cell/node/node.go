// Package node runs one replica of a cell: a CometBFT node with the kernel
// application of package app.
//
// Setup writes the files of a cell of several replicas on one computer.
// Start runs one of them. docs/replication.md describes the arrangement.
package node

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	dbm "github.com/cometbft/cometbft-db"
	cfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/crypto"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/libs/log"
	nm "github.com/cometbft/cometbft/node"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/proxy"
	rpclocal "github.com/cometbft/cometbft/rpc/client/local"
	"github.com/cometbft/cometbft/types"

	"github.com/teddyknox/clearway/cell/app"
	"github.com/teddyknox/clearway/cell/client"
	"github.com/teddyknox/clearway/internal/record"
)

// settings are the addresses of one replica. Setup writes them to the file
// cell.json in the home directory of the replica.
type settings struct {
	Name  string `json:"name"`
	P2P   string `json:"p2p"`   // where the replica listens for the other replicas
	RPC   string `json:"rpc"`   // where the replica answers queries
	Peers string `json:"peers"` // the other replicas: id@host:port, with commas between them
}

const settingsFile = "cell.json"

// config returns the CometBFT configuration of a replica on one computer
// with the other replicas. The timeouts suit a local network: a block takes
// some tens of milliseconds.
func config(home string, s settings) *cfg.Config {
	c := cfg.DefaultConfig()
	c.SetRoot(home)
	c.Moniker = s.Name
	c.P2P.ListenAddress = s.P2P
	c.P2P.PersistentPeers = s.Peers
	c.P2P.AllowDuplicateIP = true
	c.P2P.AddrBookStrict = false
	c.P2P.PexReactor = false
	c.RPC.ListenAddress = s.RPC
	c.RPC.TimeoutBroadcastTxCommit = 20 * time.Second
	c.Consensus.TimeoutPropose = 600 * time.Millisecond
	c.Consensus.TimeoutProposeDelta = 200 * time.Millisecond
	c.Consensus.TimeoutPrevote = 200 * time.Millisecond
	c.Consensus.TimeoutPrevoteDelta = 100 * time.Millisecond
	c.Consensus.TimeoutPrecommit = 200 * time.Millisecond
	c.Consensus.TimeoutPrecommitDelta = 100 * time.Millisecond
	c.Consensus.TimeoutCommit = 5 * time.Millisecond
	c.Consensus.PeerGossipSleepDuration = 5 * time.Millisecond
	// A block only when there is a transaction, and then one more, which
	// carries the application hash of the block before it.
	c.Consensus.CreateEmptyBlocks = false
	return c
}

func freeAddress() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String(), nil
}

// stdKey returns the Ed25519 key of a validator as a key of the standard
// library. The two have the same 64 bytes.
func stdKey(k crypto.PrivKey) (ed25519.PrivateKey, error) {
	ed, ok := k.(cmted25519.PrivKey)
	if !ok || len(ed) != ed25519.PrivateKeySize {
		return nil, errors.New("the key of the replica is not an Ed25519 key")
	}
	return ed25519.PrivateKey(ed), nil
}

// Setup writes the files of a cell of n replicas on this computer, each in
// its own home directory under dir, and returns the home directories.
//
// header is the header line of the record of the cell. checkpointEvery is
// the kernel time between two checkpoints that the replicas sign (µs); with
// 0 they sign one after every block that has an input. Each replica gets a
// new Ed25519 key. It signs its votes and the checkpoints of the record with
// that key, and the genesis file names the public keys.
func Setup(dir string, n int, header []byte, checkpointEvery int64) ([]string, error) {
	if _, err := record.ParseHeader(header); err != nil {
		return nil, err
	}
	homes := make([]string, n)
	sets := make([]settings, n)
	ids := make([]string, n)
	gen := app.Genesis{Header: string(header), CheckpointEvery: checkpointEvery}
	var validators []types.GenesisValidator
	for i := range homes {
		home := filepath.Join(dir, fmt.Sprintf("replica%d", i))
		homes[i] = home
		c := config(home, settings{})
		cfg.EnsureRoot(home)
		pv, err := privval.GenFilePV(c.PrivValidatorKeyFile(), c.PrivValidatorStateFile(),
			func() (crypto.PrivKey, error) { return cmted25519.GenPrivKey(), nil })
		if err != nil {
			return nil, err
		}
		pv.Save()
		key, err := stdKey(pv.Key.PrivKey)
		if err != nil {
			return nil, err
		}
		gen.Keys = append(gen.Keys, record.FormatPublicKey(key.Public().(ed25519.PublicKey)))
		validators = append(validators, types.GenesisValidator{
			Address: pv.Key.Address, PubKey: pv.Key.PubKey, Power: 1, Name: fmt.Sprintf("replica%d", i),
		})
		nodeKey, err := p2p.LoadOrGenNodeKey(c.NodeKeyFile())
		if err != nil {
			return nil, err
		}
		p2pAddr, err := freeAddress()
		if err != nil {
			return nil, err
		}
		rpcAddr, err := freeAddress()
		if err != nil {
			return nil, err
		}
		sets[i] = settings{Name: fmt.Sprintf("replica%d", i), P2P: "tcp://" + p2pAddr, RPC: "tcp://" + rpcAddr}
		ids[i] = string(nodeKey.ID()) + "@" + p2pAddr
	}
	state, err := json.Marshal(gen)
	if err != nil {
		return nil, err
	}
	h, _ := record.ParseHeader(header)
	doc := &types.GenesisDoc{
		ChainID:         "clearway-" + h.Cell,
		GenesisTime:     time.Now().UTC().Truncate(time.Second),
		ConsensusParams: types.DefaultConsensusParams(),
		Validators:      validators,
		AppState:        state,
	}
	if err := doc.ValidateAndComplete(); err != nil {
		return nil, err
	}
	for i, home := range homes {
		var peers []string
		for j, id := range ids {
			if j != i {
				peers = append(peers, id)
			}
		}
		sets[i].Peers = strings.Join(peers, ",")
		if err := doc.SaveAs(config(home, sets[i]).GenesisFile()); err != nil {
			return nil, err
		}
		b, _ := json.MarshalIndent(sets[i], "", "  ")
		if err := os.WriteFile(filepath.Join(home, settingsFile), b, 0o644); err != nil {
			return nil, err
		}
	}
	return homes, nil
}

// RPCAddress returns the address at which the replica with the files in
// home answers queries. client.Dial takes it.
func RPCAddress(home string) (string, error) {
	s, err := readSettings(home)
	return s.RPC, err
}

func readSettings(home string) (settings, error) {
	var s settings
	b, err := os.ReadFile(filepath.Join(home, settingsFile))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%s: %w", settingsFile, err)
	}
	return s, nil
}

// databases opens a database of a node. The database stays open when the
// node closes it.
//
// A node of CometBFT 1.0 that stops closes its databases while goroutines of
// its consensus reactor can still read them (queryMaj23Routine sleeps for
// seconds between its check that the reactor runs and its read), and a read
// of a closed database ends the process. A write is on the disk before the
// database returns from it, so a process that ends loses nothing without the
// close.
func databases(ctx *cfg.DBContext) (dbm.DB, error) {
	db, err := cfg.DefaultDBProvider(ctx)
	if err != nil {
		return nil, err
	}
	return keptOpen{db}, nil
}

type keptOpen struct{ dbm.DB }

func (keptOpen) Close() error { return nil }

// Options change how a replica runs.
type Options struct {
	// Interpret makes the machine of the replica interpret every instruction
	// of the kernel program.
	Interpret bool
	// Lie makes the replica faulty: see app.App.
	Lie func(height int64) bool
	// Log, if not nil, receives the errors that CometBFT logs. With Verbose,
	// it receives every message.
	Log     io.Writer
	Verbose bool
	// CommitTimeout, if not zero, is how long client.Submit waits for a
	// block when it talks to this replica.
	CommitTimeout time.Duration
}

// A Replica is one running replica of a cell.
type Replica struct {
	Home    string
	Node    *nm.Node
	App     *app.App
	Journal *app.Journal // the record of the cell, as this replica builds it
	Key     string       // the public key of the replica, in hexadecimal

	rpc     *rpclocal.Local
	key     ed25519.PrivateKey
	running atomic.Bool
}

// Client returns a client that talks to this replica without the network.
func (r *Replica) Client() *client.Client { return client.New(r.rpc) }

// GenesisFile returns the name of the genesis file of the cell.
func (r *Replica) GenesisFile() string { return r.Node.Config().GenesisFile() }

// Start runs the replica whose files are in home. The replica connects to
// the other replicas, applies the blocks that it has stored, and takes part
// in consensus. The kernel of a replica is in memory: a replica that starts
// again applies every block of the cell from the first.
func Start(home string, opt Options) (*Replica, error) {
	s, err := readSettings(home)
	if err != nil {
		return nil, err
	}
	c := config(home, s)
	if opt.CommitTimeout > 0 {
		c.RPC.TimeoutBroadcastTxCommit = opt.CommitTimeout
	}
	pv := privval.LoadFilePV(c.PrivValidatorKeyFile(), c.PrivValidatorStateFile())
	key, err := stdKey(pv.Key.PrivKey)
	if err != nil {
		return nil, err
	}
	nodeKey, err := p2p.LoadNodeKey(c.NodeKeyFile())
	if err != nil {
		return nil, err
	}
	r := &Replica{Home: home, Journal: &app.Journal{}, key: key, Key: record.FormatPublicKey(key.Public().(ed25519.PublicKey))}
	r.App = &app.App{Interpret: opt.Interpret, Lie: opt.Lie, Journal: r.Journal, OnCheckpoint: r.sign}
	logger := log.NewNopLogger()
	if opt.Log != nil {
		logger = log.NewTMLogger(log.NewSyncWriter(opt.Log))
		if !opt.Verbose {
			logger = log.NewFilter(logger, log.AllowError())
		}
	}
	r.Node, err = nm.NewNode(context.Background(), c, pv, nodeKey, proxy.NewLocalClientCreator(r.App),
		nm.DefaultGenesisDocProviderFunc(c), databases, nm.DefaultMetricsProvider(c.Instrumentation), logger)
	if err != nil {
		return nil, err
	}
	// The replica has applied the blocks that it had stored. From here it
	// signs the checkpoints of new blocks.
	r.rpc = rpclocal.New(r.Node)
	r.running.Store(true)
	if err := r.Node.Start(); err != nil {
		r.running.Store(false)
		return nil, err
	}
	return r, nil
}

// sign signs a checkpoint of the record with the key of the replica and
// sends the signature to the cell as a transaction. The application calls it
// from inside the commit of a block, so the transaction goes out later.
func (r *Replica) sign(c record.Checkpoint) {
	if !r.running.Load() {
		return // the replica is applying old blocks: the cell has these signatures
	}
	tx := app.SignatureTx(c, r.key)
	go func() {
		if r.running.Load() {
			_, _ = r.rpc.BroadcastTxAsync(context.Background(), tx)
		}
	}()
}

// Stop stops the replica. Its databases stay open until the process ends:
// see databases.
//
// A replica that stopped cannot start again in the same process: its
// databases are open, and a node of CometBFT 1.0 that stops can leave a
// connection to another replica behind, and that replica then refuses the
// new node. Start the replica again in a new process.
func (r *Replica) Stop() error {
	if !r.running.Swap(false) {
		return nil
	}
	if err := r.Node.Stop(); err != nil {
		return err
	}
	r.Node.Wait()
	return nil
}

// Height returns the number of the last block that the replica has stored.
func (r *Replica) Height() int64 { return r.Node.BlockStore().Height() }
