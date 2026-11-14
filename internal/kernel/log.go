package kernel

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"sort"

	"github.com/teddyknox/clearway/internal/traj"
)

// InputKind names one kind of kernel input.
type InputKind uint8

const (
	InAdvance InputKind = iota
	InAdmit
	InPropose
	InRecontract
	InAck
	InRefuse
	InFail
	InTrack
	InMarkIntruder
	InReanchor
	InRemove
	InTick
	InSetAuthority
)

// Input is one entry of the kernel's input log. The kernel is a deterministic
// state machine: the same inputs in the same order produce the same state and
// the same verdicts.
type Input struct {
	Kind   InputKind
	T      float64
	Flight FlightID
	Traj   *traj.Trajectory
	Track  Track
	By     Authority
}

func (k *Kernel) record(in Input) {
	if k.Record {
		k.Log = append(k.Log, in)
	}
}

// Replay applies a recorded input log to a new kernel and returns it.
func Replay(p Params, d *Dual, log []Input) *Kernel {
	k := New(p, d)
	for _, in := range log {
		switch in.Kind {
		case InAdvance:
			k.Advance(in.T)
		case InAdmit:
			k.Admit(in.Flight, in.Traj)
		case InPropose:
			k.Propose(in.Flight, in.Traj, in.By)
		case InRecontract:
			k.ProposeRecontract(in.Flight, in.Traj, in.By)
		case InAck:
			k.Ack(in.Flight)
		case InRefuse:
			k.Refuse(in.Flight)
		case InFail:
			k.DeliveryFailed(in.Flight)
		case InTrack:
			k.UpdateTrack(in.Flight, in.Track)
		case InMarkIntruder:
			k.MarkIntruder(in.Flight)
		case InReanchor:
			k.ReanchorAll()
		case InRemove:
			k.Remove(in.Flight)
		case InTick:
			k.Tick()
		case InSetAuthority:
			k.SetAuthority(in.Flight, in.By)
		}
	}
	return k
}

// Digest returns a hash of the Store, the verified windows, and every verdict
// issued so far. Two kernels with equal digests reached the same state by the
// same decisions.
func (k *Kernel) Digest() uint64 {
	h := fnv.New64a()
	put := func(vals ...uint64) {
		var b [8]byte
		for _, v := range vals {
			binary.LittleEndian.PutUint64(b[:], v)
			h.Write(b[:])
		}
	}
	f64 := math.Float64bits
	tube := func(c *Contract) {
		if c == nil {
			put(0)
			return
		}
		put(c.ID, f64(c.Anchor), f64(c.R0), f64(c.DivergeAt), uint64(len(c.Traj.Legs)))
		for _, l := range c.Traj.Legs {
			put(f64(l.T0), f64(l.T1), f64(l.P0.X), f64(l.P0.Y), f64(l.V.X), f64(l.V.Y))
		}
		for _, s := range c.Traj.VSegs {
			put(f64(s.T0), f64(s.T1), f64(s.A0), f64(s.A1))
		}
	}
	put(f64(k.now), k.outHash, k.nextTube)
	for _, id := range k.order {
		f := k.flights[id]
		var flags uint64
		if f.Intruder {
			flags |= 1
		}
		put(uint64(id), uint64(f.Authority), uint64(f.PState), flags)
		tube(f.Active)
		tube(f.Pending)
	}
	keys := make([]pairKey, 0, len(k.windows))
	for key := range k.windows {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].a != keys[j].a {
			return keys[i].a < keys[j].a
		}
		return keys[i].b < keys[j].b
	})
	for _, key := range keys {
		put(key.a, key.b, f64(k.windows[key]))
	}
	return h.Sum64()
}
