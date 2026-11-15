package kernel

import (
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"sort"
)

// stateTag starts the input of every state digest.
const stateTag = "clearway state 0.2\n"

// StateDigest returns the SHA-256 digest of the Store: the clock, every
// flight with its track and its tubes, every window, and the result of the
// last Tick. It covers every value that a later operation can read.
//
// The encoding is defined in docs/record.md, section 5: the tag, and then a
// sequence of 64-bit words, least significant byte first. Every value of the
// Store is an integer, so the digest has one form on every machine. The
// kernel in Rust must return the same digest after the same inputs.
//
// StateDigest does not change the kernel.
func (k *Kernel) StateDigest() [32]byte {
	e := enc{h: sha256.New()}
	e.h.Write([]byte(stateTag))
	e.i64(k.now)
	e.u64(k.nextTube)

	e.u64(uint64(len(k.order)))
	for _, id := range k.order {
		f := k.flights[id]
		e.i64(int64(id))
		e.u64(uint64(f.Authority))
		e.u64(uint64(f.PState))
		e.flag(f.Intruder)
		e.flag(f.HasTrack)
		if f.HasTrack {
			t := f.Track
			e.i64(t.T, t.Pos.X, t.Pos.Y, t.Vel.X, t.Vel.Y, t.Alt, t.VRate)
			e.flag(t.HasSel)
			e.i64(t.SelAlt)
		}
		e.contract(f.Active)
		e.contract(f.Pending)
		e.tube(f.ITube)
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
	e.u64(uint64(len(keys)))
	for _, key := range keys {
		e.u64(key.a, key.b)
		e.i64(k.windows[key])
	}

	e.u64(uint64(len(k.conflicts)))
	for _, c := range k.conflicts {
		e.i64(int64(c.A), int64(c.B), c.T)
		e.flag(c.Intruder)
	}
	e.i64(k.coast)

	var out [32]byte
	e.h.Sum(out[:0])
	return out
}

// enc writes 64-bit little-endian words to a hash.
type enc struct {
	h hash.Hash
	b [8]byte
}

func (e *enc) u64(vals ...uint64) {
	for _, v := range vals {
		binary.LittleEndian.PutUint64(e.b[:], v)
		e.h.Write(e.b[:])
	}
}

// i64 writes signed values in two's complement.
func (e *enc) i64(vals ...int64) {
	for _, v := range vals {
		e.u64(uint64(v))
	}
}

func (e *enc) flag(b bool) {
	if b {
		e.u64(1)
	} else {
		e.u64(0)
	}
}

func (e *enc) tube(t *Tube) {
	if t == nil {
		e.u64(0)
		return
	}
	e.u64(1, t.ID)
	e.i64(t.Anchor, t.R0, t.K, t.VU0, t.VURate, t.VMin)
	e.u64(uint64(len(t.Traj.Legs)))
	for _, l := range t.Traj.Legs {
		e.i64(l.T0, l.T1, l.P0.X, l.P0.Y, l.V.X, l.V.Y)
	}
	e.u64(uint64(len(t.Traj.VSegs)))
	for _, s := range t.Traj.VSegs {
		e.i64(s.T0, s.T1, s.A0, s.A1)
	}
}

func (e *enc) contract(c *Contract) {
	if c == nil {
		e.u64(0)
		return
	}
	e.tube(&c.Tube)
	e.i64(c.DivergeAt)
	e.flag(c.Recontract)
}
