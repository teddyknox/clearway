package rvm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"slices"
	"testing"
)

// guestMem is the memory of the guest program in the tests. The program
// takes 1.5 MiB for its statics; the rest is stack.
const guestMem = 4 << 20

// guestStepLimit bounds a run of the guest program. The longest run of the
// tests and the benchmarks takes half of it. A machine that is wrong can
// keep the guest in a loop; the limit ends such a run in seconds.
const guestStepLimit = 1 << 29

// The operations and exit codes of the guest program; see
// testdata/guest/src/main.rs.
const (
	opArith64  = 1
	opArith128 = 2
	opSHA256   = 3
	opMem      = 4
	opRecurse  = 5
	opFntable  = 6
	opStatics  = 7
	opHeap     = 8
	opAtomic   = 9
	opExit     = 10
	opRawM     = 11

	exitShortHeader  = 0xE001
	exitShortPayload = 0xE003
	exitUnknownOp    = 0xE004

	guestHeapSize    = 1 << 20
	guestScratchSize = 4096
)

// A wr collects the bytes of a payload. Every number is little-endian.
type wr struct{ b []byte }

func (w *wr) u8(v uint8)   { w.b = append(w.b, v) }
func (w *wr) u16(v uint16) { w.b = binary.LittleEndian.AppendUint16(w.b, v) }
func (w *wr) u32(v uint32) { w.b = binary.LittleEndian.AppendUint32(w.b, v) }
func (w *wr) u64(v uint64) { w.b = binary.LittleEndian.AppendUint64(w.b, v) }

// u128 writes v modulo 2^128.
func (w *wr) u128(v *big.Int) {
	var b [16]byte
	wrap128(v).FillBytes(b[:])
	slices.Reverse(b[:])
	w.b = append(w.b, b[:]...)
}

var (
	two64  = new(big.Int).Lsh(big.NewInt(1), 64)
	two127 = new(big.Int).Lsh(big.NewInt(1), 127)
	two128 = new(big.Int).Lsh(big.NewInt(1), 128)
	max128 = new(big.Int).Sub(two128, big.NewInt(1))
)

// wrap128 returns v modulo 2^128, in [0, 2^128).
func wrap128(v *big.Int) *big.Int { return new(big.Int).And(v, max128) }

// signed128 returns the value of the 128 bits of v as a signed number.
func signed128(v *big.Int) *big.Int {
	if v.Cmp(two127) >= 0 {
		return new(big.Int).Sub(v, two128)
	}
	return new(big.Int).Set(v)
}

// fitsSigned reports whether v is in [-2^(n-1), 2^(n-1)).
func fitsSigned(v *big.Int, n uint) bool {
	limit := new(big.Int).Lsh(big.NewInt(1), n-1)
	return v.Cmp(limit) < 0 && v.Cmp(new(big.Int).Neg(limit)) >= 0
}

// A prng is the SplitMix64 generator. The tests do not use math/rand, so
// that a seed gives the same requests with every version of Go; the file
// guest.digest depends on that.
type prng struct{ state uint64 }

func (p *prng) Uint64() uint64 {
	p.state += 0x9e3779b97f4a7c15
	z := p.state
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}

func (p *prng) Uint32() uint32 { return uint32(p.Uint64() >> 32) }

// IntN returns a number in [0, n). The caller gives n > 0.
func (p *prng) IntN(n int) int { return int(p.Uint64() % uint64(n)) }

// A guestRun is a sequence of requests for the guest program, the responses
// that must follow, and a model of the statics that the guest keeps between
// requests.
type guestRun struct {
	rng *prng
	in  []byte // the input stream
	out []byte // the output stream that must follow
	ops []byte // the operation of each request

	heapUsed  uint32
	dataWords [8]uint32
	dataBytes [16]byte
	bssWords  [8]uint32
	atomU     uint32
	atomI     int32
}

var guestROWords = [8]uint32{
	0x9e3779b9, 0x7f4a7c15, 0xf39cc060, 0x5cedc834, 0x1082276b, 0xf3a27251, 0xf86c6a11, 0xd0c18e95,
}

func newGuestRun(seed uint64) *guestRun {
	g := &guestRun{
		rng:       &prng{state: seed},
		dataWords: [8]uint32{0x01234567, 0x89abcdef, 0xdeadbeef, 0x0badf00d, 0x13579bdf, 0x2468ace0, 0xffffffff, 0x00000001},
		atomU:     0x80000000,
		atomI:     -7,
	}
	copy(g.dataBytes[:], "clearway guest!\n")
	return g
}

// request appends one request and the response that must follow.
func (g *guestRun) request(op byte, payload, response []byte) {
	g.ops = append(g.ops, op)
	g.in = append(g.in, op)
	g.in = binary.LittleEndian.AppendUint32(g.in, uint32(len(payload)))
	g.in = append(g.in, payload...)
	g.out = binary.LittleEndian.AppendUint32(g.out, uint32(len(response)))
	g.out = append(g.out, response...)
}

// word64 returns a 64-bit operand: often a value at an edge of a range.
func (g *guestRun) word64() uint64 {
	edges := []uint64{0, 1, 2, 3, math.MaxUint64, math.MaxUint64 - 1, 1 << 63, 1<<63 - 1, 1<<63 + 1,
		1 << 32, 1<<32 - 1, 1<<32 + 1, 1 << 31, 1<<31 - 1, 0xffffffff00000000, 0x00000000ffffffff}
	switch g.rng.IntN(4) {
	case 0:
		return edges[g.rng.IntN(len(edges))]
	case 1:
		return g.rng.Uint64() >> g.rng.IntN(64)
	case 2:
		return -(g.rng.Uint64() >> g.rng.IntN(64))
	}
	return g.rng.Uint64()
}

// word128 returns a 128-bit operand.
func (g *guestRun) word128() *big.Int {
	v := new(big.Int).SetUint64(g.word64())
	v.Lsh(v, 64)
	v.Or(v, new(big.Int).SetUint64(g.word64()))
	switch g.rng.IntN(4) {
	case 0:
		v.Rsh(v, uint(g.rng.IntN(128)))
	case 1:
		v = wrap128(new(big.Int).Neg(new(big.Int).Rsh(v, uint(g.rng.IntN(128)))))
	}
	return v
}

func (g *guestRun) arith64(n int) {
	var p, r wr
	for range n {
		a, b, s := g.word64(), g.word64(), uint8(g.rng.Uint32())
		p.u64(a)
		p.u64(b)
		p.u8(s)

		sa, sb := int64(a), int64(b)
		ba, bb := big.NewInt(sa), big.NewInt(sb)
		sh := uint(s & 63)
		r.u64(a + b)
		r.u64(a - b)
		r.u64(a * b)
		if b != 0 {
			r.u64(a / b)
			r.u64(a % b)
			// Go defines the quotient of the least integer by -1 as
			// the least integer, with the remainder 0, as Rust's
			// wrapping_div and wrapping_rem do.
			r.u64(uint64(sa / sb))
			r.u64(uint64(sa % sb))
		} else {
			r.u64(math.MaxUint64)
			r.u64(a)
			r.u64(math.MaxUint64)
			r.u64(a)
		}
		r.u64(a << sh)
		r.u64(a >> sh)
		r.u64(uint64(sa >> sh))
		r.u64(bits.RotateLeft64(a, int(sh)))
		r.u32(uint32(bits.LeadingZeros64(a)))
		r.u32(uint32(bits.TrailingZeros64(a)))
		r.u32(uint32(bits.OnesCount64(a)))
		hi, _ := bits.Mul64(a, b)
		r.u64(hi)
		prod := new(big.Int).Mul(ba, bb)
		r.u64(new(big.Int).And(new(big.Int).Rsh(prod, 64), new(big.Int).SetUint64(math.MaxUint64)).Uint64())
		switch {
		case fitsSigned(prod, 64):
			r.u64(uint64(prod.Int64()))
		case prod.Sign() < 0:
			r.u64(1 << 63)
		default:
			r.u64(1<<63 - 1)
		}
		_, carry := bits.Add64(a, b, 0)
		r.u8(uint8(flag(a < b) | flag(sa < sb)<<1 | uint32(carry)<<2 |
			flag(!fitsSigned(new(big.Int).Sub(ba, bb), 64))<<3 | flag(!fitsSigned(prod, 64))<<4))

		x, y := uint32(a), uint32(b)
		r.u32(x * y)
		r.u32(quotient(x, y, false, false))
		r.u32(quotient(x, y, false, true))
		r.u32(quotient(x, y, true, false))
		r.u32(quotient(x, y, true, true))
		r.u32(product(x, y, false, false, 32))
		r.u32(product(x, y, true, true, 32))
		r.u32(uint32(bits.LeadingZeros32(x)))
	}
	g.request(opArith64, p.b, r.b)
}

func (g *guestRun) arith128(n int) {
	var p, r wr
	for range n {
		a, b, s := g.word128(), g.word128(), uint8(g.rng.Uint32())
		p.u128(a)
		p.u128(b)
		p.u8(s)

		sa, sb := signed128(a), signed128(b)
		sh := uint(s & 127)
		r.u128(new(big.Int).Add(a, b))
		r.u128(new(big.Int).Sub(a, b))
		r.u128(new(big.Int).Mul(a, b))
		if b.Sign() != 0 {
			r.u128(new(big.Int).Quo(a, b))
			r.u128(new(big.Int).Rem(a, b))
			// Quo and Rem round to zero. The quotient -2^127 / -1 is
			// 2^127, which is -2^127 modulo 2^128, as wrapping_div
			// gives.
			r.u128(new(big.Int).Quo(sa, sb))
			r.u128(new(big.Int).Rem(sa, sb))
		} else {
			r.u128(max128)
			r.u128(a)
			r.u128(max128)
			r.u128(a)
		}
		r.u128(new(big.Int).Lsh(a, sh))
		r.u128(new(big.Int).Rsh(a, sh))
		r.u128(new(big.Int).Rsh(sa, sh)) // Rsh rounds down, as an arithmetic shift does
		r.u128(new(big.Int).Or(new(big.Int).Lsh(a, sh), new(big.Int).Rsh(a, 128-sh)))
		r.u32(uint32(128 - a.BitLen()))
		if a.Sign() == 0 {
			r.u32(128)
		} else {
			r.u32(uint32(a.TrailingZeroBits()))
		}
		ones := 0
		for _, byt := range a.Bytes() {
			ones += bits.OnesCount8(byt)
		}
		r.u32(uint32(ones))
		root := new(big.Int).Sqrt(a).Uint64()
		r.u64(root)
		r.u64(root)
		lowA, lowB := new(big.Int).Mod(a, two64), new(big.Int).Mod(b, two64)
		r.u128(new(big.Int).Mul(lowA, lowB))
		fixed := new(big.Int).Mul(big.NewInt(int64(lowA.Uint64())), big.NewInt(int64(lowB.Uint64())))
		r.u128(fixed.Rsh(fixed, 32))
		r.u8(uint8(flag(a.Cmp(b) < 0) | flag(sa.Cmp(sb) < 0)<<1 |
			flag(new(big.Int).Add(a, b).Cmp(two128) >= 0)<<2 | flag(!fitsSigned(new(big.Int).Mul(sa, sb), 128))<<3))
	}
	g.request(opArith128, p.b, r.b)
}

func (g *guestRun) sha256(size int) {
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(g.rng.Uint32())
	}
	sum := sha256.Sum256(payload)
	g.request(opSHA256, payload, sum[:])
}

// span returns a start and a length inside the scratch buffer. Most spans
// are short, and their starts have every alignment.
func (g *guestRun) span() (start, length int) {
	switch g.rng.IntN(4) {
	case 0:
		length = g.rng.IntN(guestScratchSize + 1)
	case 1:
		length = g.rng.IntN(9)
	default:
		length = g.rng.IntN(200)
	}
	return g.rng.IntN(guestScratchSize - length + 1), length
}

func (g *guestRun) mem(n int) {
	var p, r wr
	seed := g.rng.Uint32()
	p.u32(seed)
	var scratch [guestScratchSize]byte
	x := seed | 1
	for i := range scratch {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		scratch[i] = byte(x >> 24)
	}
	op := func(kind, value uint8, dst, src, length int) {
		p.u8(kind)
		p.u8(value)
		p.u16(uint16(dst))
		p.u16(uint16(src))
		p.u16(uint16(length))
		d, s := scratch[dst:dst+length], scratch[src:src+length]
		switch kind {
		case 0:
			for i := range d {
				d[i] = value
			}
		case 1, 2:
			copy(d, s) // copy moves the bytes as memmove does
		case 3:
			r.u8(uint8(bytes.Compare(d, s)))
		case 4:
			r.u8(uint8(flag(bytes.Equal(d, s))))
		}
	}
	for range n {
		dst, length := g.span()
		src := g.rng.IntN(guestScratchSize - length + 1)
		value := uint8(g.rng.Uint32())
		switch kind := uint8(g.rng.IntN(5)); kind {
		case 1:
			// memcpy needs ranges that do not overlap.
			if dst < src+length && src < dst+length {
				kind = 2
			}
			op(kind, value, dst, src, length)
		case 3, 4:
			// Make the ranges equal first in half of the cases, and
			// then different in one byte in half of those.
			if g.rng.IntN(2) == 0 {
				op(2, 0, dst, src, length)
				if length > 0 && g.rng.IntN(2) == 0 {
					op(0, value, dst+g.rng.IntN(length), 0, 1)
				}
			}
			op(kind, 0, dst, src, length)
		default:
			op(kind, value, dst, src, length)
		}
	}
	r.b = append(r.b, scratch[:]...)
	g.request(opMem, p.b, r.b)
}

func (g *guestRun) recurse(values []uint32) {
	var p, r wr
	for _, v := range values {
		p.u32(v)
	}
	acc := uint64(0x9e3779b97f4a7c15)
	for i := len(values) - 1; i >= 0; i-- {
		acc = (bits.RotateLeft64(acc, 5) ^ uint64(values[i])) * 0x00000100000001b3
	}
	r.u64(acc)
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	for _, v := range sorted {
		r.u32(v)
	}
	g.request(opRecurse, p.b, r.b)
}

func (g *guestRun) fntable(n int) {
	funcs := [8]func(a, b uint32) uint32{
		func(a, b uint32) uint32 { return a + b },
		func(a, b uint32) uint32 { return a - b },
		func(a, b uint32) uint32 { return a * (b | 1) },
		func(a, b uint32) uint32 { return bits.RotateLeft32(a^b, int(b&31)) },
		func(a, b uint32) uint32 {
			if b == 0 {
				return ^a
			}
			return a / b
		},
		func(a, b uint32) uint32 { return quotient(a, b, true, true) },
		func(a, b uint32) uint32 { return a*0x9e3779b1 ^ bits.ReverseBytes32(b) },
		func(a, b uint32) uint32 { return uint32(int32(a)>>(b&31)) ^ b },
	}
	var p, r wr
	acc := uint32(0x12345678)
	for range n {
		index, operand := uint8(g.rng.Uint32()), uint32(g.word64())
		p.u8(index)
		p.u32(operand)
		acc = funcs[index&7](acc, operand)
		r.u32(acc)
	}
	g.request(opFntable, p.b, r.b)
}

func (g *guestRun) statics(n int) {
	var p, r wr
	for range n {
		kind, v := uint8(g.rng.Uint32()), g.rng.Uint32()
		p.u8(kind)
		p.u32(v)
		i, j := v>>29, v>>26&7
		switch kind & 3 {
		case 0:
			g.dataWords[i] = (g.dataWords[i] + v) ^ guestROWords[j]
		case 1:
			g.bssWords[i] += v ^ guestROWords[j]
		case 2:
			g.dataBytes[v&15] ^= uint8(v >> 8)
		case 3:
			g.dataWords[i], g.bssWords[j] = g.bssWords[j], g.dataWords[i]
		}
	}
	for _, v := range g.dataWords {
		r.u32(v)
	}
	r.b = append(r.b, g.dataBytes[:]...)
	for _, v := range g.bssWords {
		r.u32(v)
	}
	for _, v := range guestROWords {
		r.u32(v)
	}
	g.request(opStatics, p.b, r.b)
}

// heap asks for blocks of the given sizes. The alignment of a block is a
// random power of two up to 128.
func (g *guestRun) heap(sizes []uint32) {
	var p, r wr
	type block struct{ offset, size uint32 }
	var blocks []block
	for _, size := range sizes {
		exp := uint8(g.rng.IntN(8))
		p.u8(exp)
		p.u32(size)
		align := uint32(1) << exp
		offset := (g.heapUsed + align - 1) &^ (align - 1)
		if offset > guestHeapSize || size > guestHeapSize-offset {
			r.u32(math.MaxUint32)
			continue
		}
		g.heapUsed = offset + size
		r.u32(offset)
		r.u32(0) // every byte of a new block is zero
		blocks = append(blocks, block{offset, size})
	}
	var sum uint64
	for _, b := range slices.Backward(blocks) {
		for k := uint32(0); k < b.size; k++ {
			sum = bits.RotateLeft64(sum, 1) ^ uint64(uint8(k*31+b.offset))*(uint64(k)+1)
		}
	}
	r.u64(sum)
	g.request(opHeap, p.b, r.b)
}

func (g *guestRun) atomic(n int) {
	var p, r wr
	for range n {
		kind, v, x := uint8(g.rng.IntN(14)), uint32(g.word64()), g.rng.Uint32()
		if kind == 10 && g.rng.IntN(2) == 0 {
			v = g.atomU // the exchange succeeds
		}
		p.u8(kind)
		p.u32(v)
		p.u32(x)
		oldU, oldI := g.atomU, g.atomI
		switch kind {
		case 0:
			g.atomU += v
		case 1:
			g.atomU -= v
		case 2:
			g.atomU = v
		case 3:
			g.atomU &= v
		case 4:
			g.atomU |= v
		case 5:
			g.atomU ^= v
		case 6:
			g.atomU = max(g.atomU, v)
		case 7:
			g.atomU = min(g.atomU, v)
		case 8:
			g.atomI = max(g.atomI, int32(v))
		case 9:
			g.atomI = min(g.atomI, int32(v))
		case 10:
			if oldU == v {
				g.atomU = x
				r.u8(1)
			} else {
				r.u8(0)
			}
		case 11:
			g.atomU = oldU*3 + v
		case 12:
			g.atomI += int32(v)
		}
		switch kind {
		case 8, 9, 12:
			r.u32(uint32(oldI))
		default:
			r.u32(oldU)
		}
	}
	g.request(opAtomic, p.b, r.b)
}

func (g *guestRun) rawM(n int) {
	edges := []uint32{0, 1, 2, 0xffffffff, 0xfffffffe, 0x80000000, 0x7fffffff, 0x80000001, 0x10000, 0xffff}
	var p, r wr
	pair := func(a, b uint32) {
		p.u32(a)
		p.u32(b)
		r.u32(product(a, b, false, false, 0))
		r.u32(product(a, b, true, true, 32))
		r.u32(product(a, b, true, false, 32))
		r.u32(product(a, b, false, false, 32))
		r.u32(quotient(a, b, true, false))
		r.u32(quotient(a, b, false, false))
		r.u32(quotient(a, b, true, true))
		r.u32(quotient(a, b, false, true))
	}
	for _, a := range edges {
		for _, b := range edges {
			pair(a, b)
		}
	}
	for range n {
		pair(uint32(g.word64()), uint32(g.word64()))
	}
	g.request(opRawM, p.b, r.b)
}

// values returns n random u32 values.
func (g *guestRun) values(n int) []uint32 {
	v := make([]uint32, n)
	for i := range v {
		v[i] = uint32(g.word64())
	}
	return v
}

// exit appends the request that halts the guest with the given exit code.
func (g *guestRun) exit(code uint32) {
	g.ops = append(g.ops, opExit)
	g.in = append(g.in, opExit, 4, 0, 0, 0)
	g.in = binary.LittleEndian.AppendUint32(g.in, code)
}

// guestExit is the exit code that the test asks of the guest.
const guestExit = 0x5eed

// newGuestWorkload returns requests that use every operation of the guest
// program many times, in an order that depends on the seed alone. The last
// request halts the guest with guestExit.
func newGuestWorkload(seed uint64) *guestRun {
	g := newGuestRun(seed)
	g.statics(0) // the start values of .data and .bss
	g.heap(nil)
	g.sha256(0)
	g.recurse(nil)
	g.rawM(64)
	for _, size := range []int{1, 55, 56, 57, 63, 64, 65, 119, 120, 127, 128, 1 << 16} {
		g.sha256(size)
	}
	sorted := g.values(500)
	slices.Sort(sorted)
	g.recurse(sorted)
	slices.Reverse(sorted)
	g.recurse(sorted)
	g.recurse(make([]uint32, 300))
	g.recurse([]uint32{7})
	g.recurse([]uint32{9, 7})
	g.recurse(g.values(16000)) // a recursion 16000 calls deep
	g.heap([]uint32{0, 1, guestHeapSize + 1, 100})

	for round := 0; round < 40; round++ {
		switch round % 10 {
		case 0:
			g.arith64(100)
		case 1:
			g.arith128(40)
		case 2:
			g.sha256(g.rng.IntN(3000))
		case 3:
			g.mem(200)
		case 4:
			g.recurse(g.values(g.rng.IntN(2000)))
		case 5:
			g.fntable(500)
		case 6:
			g.statics(200)
		case 7:
			sizes := make([]uint32, 1+g.rng.IntN(16))
			for i := range sizes {
				sizes[i] = uint32(g.rng.IntN(6000))
			}
			g.heap(sizes)
		case 8:
			g.atomic(300)
		case 9:
			g.rawM(100)
		}
	}
	g.heap([]uint32{guestHeapSize, 8}) // the first block does not fit
	g.exit(guestExit)
	return g
}

// frames splits an output stream into responses. A response that the stream
// cuts short is the last frame.
func frames(out []byte) [][]byte {
	var fs [][]byte
	for len(out) >= 4 {
		n := int(binary.LittleEndian.Uint32(out))
		if n > len(out)-4 {
			n = len(out) - 4
		}
		fs = append(fs, out[4:4+n])
		out = out[4+n:]
	}
	return fs
}

// checkGuestOutput compares the output of the guest with the output that
// must follow, and names the first response that differs.
func checkGuestOutput(t *testing.T, g *guestRun, got []byte) {
	t.Helper()
	if bytes.Equal(got, g.out) {
		return
	}
	gf, wf := frames(got), frames(g.out)
	for i := range wf {
		if i >= len(gf) {
			t.Fatalf("output ends after %d responses, want %d", len(gf), len(wf))
		}
		if !bytes.Equal(gf[i], wf[i]) {
			at := 0
			for at < len(gf[i]) && at < len(wf[i]) && gf[i][at] == wf[i][at] {
				at++
			}
			t.Fatalf("response %d (operation %d) differs at byte %d of %d:\n got  %x\n want %x",
				i, g.ops[i], at, len(wf[i]), gf[i][at:min(at+32, len(gf[i]))], wf[i][at:min(at+32, len(wf[i]))])
		}
	}
	t.Fatalf("output has %d bytes, want %d", len(got), len(g.out))
}

// runGuest runs the guest program on an input to its halt. It returns the
// machine, the output, and the exit code.
func runGuest(t testing.TB, input []byte) (*Machine, []byte, uint32) {
	t.Helper()
	m, err := Load(loadGuest(t), guestMem)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	m.In, m.Out = bytes.NewReader(input), &out
	exit, err := m.Run(guestStepLimit)
	if err != nil {
		t.Fatalf("guest: %v after %d steps", err, m.Steps)
	}
	return m, out.Bytes(), exit
}

// TestGuest runs the Rust guest program on requests with pseudo-random
// operands and compares every byte of its output with values that Go
// computes natively.
func TestGuest(t *testing.T) {
	for _, seed := range []uint64{1, 2} {
		g := newGuestWorkload(seed)
		m, out, exit := runGuest(t, g.in)
		checkGuestOutput(t, g, out)
		if exit != guestExit {
			t.Errorf("seed %d: exit code %#x, want %#x", seed, exit, guestExit)
		}
		if halted, code := m.Halted(); !halted || code != guestExit {
			t.Errorf("seed %d: Halted() = %v, %#x", seed, halted, code)
		}
		t.Logf("seed %d: %d requests, %d input bytes, %d output bytes, %d instructions",
			seed, len(g.ops), len(g.in), len(g.out), m.Steps)
	}
}

// TestGuestEndOfInput checks what the guest sees at the end of the input: a
// read of 0 bytes at a request boundary, and a short read inside a request.
func TestGuestEndOfInput(t *testing.T) {
	g := newGuestRun(1)
	g.fntable(10)
	whole := g.in
	for _, c := range []struct {
		name  string
		input []byte
		exit  uint32
		out   []byte
	}{
		{"empty input", nil, 0, nil},
		{"one request", whole, 0, g.out},
		{"short header", whole[:3], exitShortHeader, nil},
		{"short payload", whole[:len(whole)-1], exitShortPayload, nil},
		{"header after a request", append(slices.Clone(whole), opFntable, 0), exitShortHeader, g.out},
		{"unknown operation", []byte{200, 0, 0, 0, 0}, exitUnknownOp, nil},
	} {
		m, out, exit := runGuest(t, c.input)
		if exit != c.exit || !bytes.Equal(out, c.out) {
			t.Errorf("%s: exit code %#x and %d bytes of output, want %#x and %d bytes", c.name, exit, len(out), c.exit, len(c.out))
		}
		// A later Run returns the same exit code and executes nothing.
		steps := m.Steps
		if again, err := m.Run(100); err != nil || again != exit || m.Steps != steps {
			t.Errorf("%s: second Run = %#x, %v with %d more steps", c.name, again, err, m.Steps-steps)
		}
		if err := m.Step(); !errors.Is(err, ErrHalted) {
			t.Errorf("%s: Step after the halt = %v, want ErrHalted", c.name, err)
		}
	}
}

// TestGuestNilStreams runs the guest with no input stream and no output
// stream.
func TestGuestNilStreams(t *testing.T) {
	m, err := Load(loadGuest(t), guestMem)
	if err != nil {
		t.Fatal(err)
	}
	if exit, err := m.Run(guestStepLimit); err != nil || exit != 0 {
		t.Fatalf("Run = %#x, %v, want 0, nil", exit, err)
	}

	// With input and no output stream, the guest still runs to its halt.
	g := newGuestRun(1)
	g.fntable(10)
	g.exit(guestExit)
	m, err = Load(loadGuest(t), guestMem)
	if err != nil {
		t.Fatal(err)
	}
	m.In = bytes.NewReader(g.in)
	if exit, err := m.Run(guestStepLimit); err != nil || exit != guestExit {
		t.Fatalf("Run = %#x, %v, want %#x, nil", exit, err, guestExit)
	}
}

// TestGuestCoverage counts the operations that the guest workload executes.
// The workload must execute every operation of the machine but ebreak and
// the operations that fault, so that TestGuest tests each of them.
func TestGuestCoverage(t *testing.T) {
	g := newGuestWorkload(1)
	m, err := Load(loadGuest(t), guestMem)
	if err != nil {
		t.Fatal(err)
	}
	m.In = bytes.NewReader(g.in)
	var count [numOps]uint64
	var compressed uint64
	for {
		if halted, _ := m.Halted(); halted {
			break
		}
		d := decode(binary.LittleEndian.Uint32(m.Mem[m.PC:]))
		count[d.op]++
		if d.size == 2 {
			compressed++
		}
		if err := m.Step(); err != nil {
			t.Fatal(err)
		}
	}
	names := [numOps]string{
		opLui: "lui", opAuipc: "auipc", opJal: "jal", opJalr: "jalr",
		opBeq: "beq", opBne: "bne", opBlt: "blt", opBge: "bge", opBltu: "bltu", opBgeu: "bgeu",
		opLb: "lb", opLh: "lh", opLw: "lw", opLbu: "lbu", opLhu: "lhu", opSb: "sb", opSh: "sh", opSw: "sw",
		opAddi: "addi", opSlti: "slti", opSltiu: "sltiu", opXori: "xori", opOri: "ori", opAndi: "andi",
		opSlli: "slli", opSrli: "srli", opSrai: "srai",
		opAdd: "add", opSub: "sub", opSll: "sll", opSlt: "slt", opSltu: "sltu", opXor: "xor", opSrl: "srl",
		opSra: "sra", opOr: "or", opAnd: "and",
		opMul: "mul", opMulh: "mulh", opMulhsu: "mulhsu", opMulhu: "mulhu",
		opDiv: "div", opDivu: "divu", opRem: "rem", opRemu: "remu",
		opFence: "fence", opEcall: "ecall", opEbreak: "ebreak",
		opLr: "lr.w", opSc: "sc.w", opAmoswap: "amoswap.w", opAmoadd: "amoadd.w", opAmoxor: "amoxor.w",
		opAmoand: "amoand.w", opAmoor: "amoor.w", opAmomin: "amomin.w", opAmomax: "amomax.w",
		opAmominu: "amominu.w", opAmomaxu: "amomaxu.w",
	}
	// The compiler has no use for these in the guest program; the
	// instruction tests cover them.
	absent := map[uint8]bool{opIllegal: true, opEbreak: true, opLb: true, opLhu: true, opSlti: true}
	var report bytes.Buffer
	for op, n := range count {
		if names[op] != "" {
			fmt.Fprintf(&report, " %s=%d", names[op], n)
		}
		if n == 0 && !absent[uint8(op)] {
			t.Errorf("the guest workload does not execute %s", names[op])
		}
	}
	t.Logf("%d instructions, %d of them compressed:%s", m.Steps, compressed, report.String())
}
