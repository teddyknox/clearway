// Package fx is the arithmetic of the kernel: signed 64-bit integers in fixed
// units, with exact 128-bit intermediate values. It has no floating point.
//
// Units (specification, section 1):
//
//	length   micrometre (µm)          horizontal position, radius, altitude
//	time     microsecond (µs)
//	speed    micrometre per second    also growth of a radius
//
// One micrometre per second for one microsecond is one picometre (pm). So a
// position at an integer time is an exact integer in picometres:
// M·p0 + v·(t − t0), with M = 1,000,000.
//
// Every function here is total and exact. A function states the range of its
// arguments; inside that range it cannot overflow. The kernel in Rust
// (guest/kernel) does the same operations with i128, and the two must give
// the same bits.
package fx

import "math/bits"

const (
	M = 1_000_000 // picometres per micrometre, and microseconds per second

	Sec = 1_000_000     // one second (µs)
	NM  = 1_852_000_000 // one nautical mile (µm)
	FT  = 304_800       // one foot (µm)
	Kt  = NM / 3600     // one knot, rounded down (µm/s)

	// Limits of valid values. A trajectory, a tube, and a track are checked
	// against them before the kernel computes with them.
	MaxTime  = 1 << 50 // |t| (µs): about 35 years
	MaxCoord = 1 << 41 // |x|, |y|, |altitude| of a trajectory (µm): about 2,200 km
	MaxTrack = 1 << 40 // |x|, |y|, |altitude| of a track (µm)
	MaxSpeed = 1 << 30 // |vx|, |vy|, a growth rate (µm/s): about 2,087 kt
	MaxLen   = 1 << 40 // a radius, an uncertainty, a separation standard (µm)

	// MaxReq is the largest required distance that is compared exactly (pm).
	// A pair that needs more is not horizontally separated.
	MaxReq = 1 << 62

	// NoLimit is the time that stands for "no limit": no conflict, or a
	// window with no end.
	NoLimit = 1<<63 - 1
)

// Vec is a point (µm) or a velocity (µm/s) in the horizontal plane.
type Vec struct{ X, Y int64 }

func (a Vec) Add(b Vec) Vec { return Vec{a.X + b.X, a.Y + b.Y} }
func (a Vec) Sub(b Vec) Vec { return Vec{a.X - b.X, a.Y - b.Y} }

// Abs returns |a|. The caller must not pass the least int64.
func Abs(a int64) int64 {
	if a < 0 {
		return -a
	}
	return a
}

// I128 is a signed 128-bit integer in two's complement: Hi·2^64 + Lo.
type I128 struct {
	Hi int64
	Lo uint64
}

// Int returns a as an I128.
func Int(a int64) I128 { return I128{a >> 63, uint64(a)} }

// Mul returns the exact product a·b.
func Mul(a, b int64) I128 {
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	// Correct the unsigned product for the signs of the operands.
	if a < 0 {
		hi -= uint64(b)
	}
	if b < 0 {
		hi -= uint64(a)
	}
	return I128{int64(hi), lo}
}

func (a I128) Add(b I128) I128 {
	lo, c := bits.Add64(a.Lo, b.Lo, 0)
	hi, _ := bits.Add64(uint64(a.Hi), uint64(b.Hi), c)
	return I128{int64(hi), lo}
}

func (a I128) Sub(b I128) I128 {
	lo, c := bits.Sub64(a.Lo, b.Lo, 0)
	hi, _ := bits.Sub64(uint64(a.Hi), uint64(b.Hi), c)
	return I128{int64(hi), lo}
}

func (a I128) Neg() I128 { return I128{}.Sub(a) }

// Sign returns −1, 0, or 1.
func (a I128) Sign() int {
	switch {
	case a.Hi < 0:
		return -1
	case a.Hi == 0 && a.Lo == 0:
		return 0
	}
	return 1
}

// Cmp returns −1, 0, or 1 as a is less than, equal to, or greater than b.
func (a I128) Cmp(b I128) int {
	switch {
	case a.Hi != b.Hi:
		if a.Hi < b.Hi {
			return -1
		}
		return 1
	case a.Lo != b.Lo:
		if a.Lo < b.Lo {
			return -1
		}
		return 1
	}
	return 0
}

// Fits reports whether a is an int64.
func (a I128) Fits() bool { return a.Hi == int64(a.Lo)>>63 }

// Int64 returns a as an int64. The caller must know that it fits.
func (a I128) Int64() int64 { return int64(a.Lo) }

// DivFloor returns floor(a / d) for d > 0. ok is false if the quotient is
// not an int64; q is then meaningless.
func (a I128) DivFloor(d int64) (q int64, ok bool) {
	neg := a.Hi < 0
	mag := a
	if neg {
		mag = a.Neg()
	}
	// mag = uq·d + ur, with 0 <= ur < d.
	hi, lo := uint64(mag.Hi), mag.Lo
	if hi >= uint64(d) {
		return 0, false // the quotient needs more than 64 bits
	}
	uq, ur := bits.Div64(hi, lo, uint64(d))
	if !neg {
		return int64(uq), uq <= 1<<63-1
	}
	// floor(−x/d) = −ceil(x/d).
	if ur != 0 {
		uq++
		if uq == 0 {
			return 0, false
		}
	}
	if uq > 1<<63 {
		return 0, false
	}
	return int64(-uq), true
}

// DivCeil returns ceil(a / d) for d > 0, with ok as for DivFloor.
func (a I128) DivCeil(d int64) (q int64, ok bool) {
	// ceil(a/d) = −floor(−a/d).
	q, ok = a.Neg().DivFloor(d)
	if !ok || q == -1<<63 {
		return 0, false
	}
	return -q, true
}

// U128 is an unsigned 128-bit integer: Hi·2^64 + Lo.
type U128 struct{ Hi, Lo uint64 }

// Sq returns a². The caller must not pass the least int64.
func Sq(a int64) U128 {
	hi, lo := bits.Mul64(uint64(Abs(a)), uint64(Abs(a)))
	return U128{hi, lo}
}

// SqSum returns x² + y². It requires |x|, |y| <= 2^63 − 1 and a sum below
// 2^128; |x|, |y| <= 2^62 is enough.
func SqSum(x, y int64) U128 {
	a, b := Sq(x), Sq(y)
	lo, c := bits.Add64(a.Lo, b.Lo, 0)
	hi, _ := bits.Add64(a.Hi, b.Hi, c)
	return U128{hi, lo}
}

func (a U128) Cmp(b U128) int {
	switch {
	case a.Hi != b.Hi:
		if a.Hi < b.Hi {
			return -1
		}
		return 1
	case a.Lo != b.Lo:
		if a.Lo < b.Lo {
			return -1
		}
		return 1
	}
	return 0
}

// Diff returns a − b as a signed value. It requires a, b < 2^127.
func (a U128) Diff(b U128) I128 {
	lo, c := bits.Sub64(a.Lo, b.Lo, 0)
	hi, _ := bits.Sub64(a.Hi, b.Hi, c)
	return I128{int64(hi), lo}
}

// ISqrt returns floor(sqrt(n)), by the digit-by-digit method: 64 steps of
// shifts, sums, and comparisons.
func ISqrt(n U128) uint64 {
	var res U128
	bit := U128{1 << 62, 0} // the highest power of four in 128 bits
	for bit.Cmp(n) > 0 {
		bit = shr2(bit)
	}
	for bit.Hi != 0 || bit.Lo != 0 {
		t := add(res, bit)
		if n.Cmp(t) >= 0 {
			n = sub(n, t)
			res = add(shr1(res), bit)
		} else {
			res = shr1(res)
		}
		bit = shr2(bit)
	}
	return res.Lo
}

func add(a, b U128) U128 {
	lo, c := bits.Add64(a.Lo, b.Lo, 0)
	hi, _ := bits.Add64(a.Hi, b.Hi, c)
	return U128{hi, lo}
}

func sub(a, b U128) U128 {
	lo, c := bits.Sub64(a.Lo, b.Lo, 0)
	hi, _ := bits.Sub64(a.Hi, b.Hi, c)
	return U128{hi, lo}
}

func shr1(a U128) U128 { return U128{a.Hi >> 1, a.Lo>>1 | a.Hi<<63} }
func shr2(a U128) U128 { return U128{a.Hi >> 2, a.Lo>>2 | a.Hi<<62} }

// ISqrt64 returns floor(sqrt(n)).
func ISqrt64(n uint64) uint64 { return ISqrt(U128{0, n}) }

// Speed returns the length of a velocity, rounded down, and SpeedUp returns
// it rounded up. Both require |v.X|, |v.Y| <= MaxSpeed.
func Speed(v Vec) int64 { return int64(ISqrt64(uint64(v.X*v.X + v.Y*v.Y))) }

func SpeedUp(v Vec) int64 {
	n := uint64(v.X*v.X + v.Y*v.Y)
	r := ISqrt64(n)
	if r*r < n {
		r++
	}
	return int64(r)
}

// Pos returns the position, in picometres, of a point that is at p0 (µm) at
// time t0 and moves at v (µm/s), at time t: M·p0 + v·(t − t0). It requires a
// result inside ±2^62 on each axis; a valid trajectory guarantees ±2^61.
func Pos(p0, v Vec, t0, t int64) Vec {
	dt := t - t0
	return Vec{
		Mul(p0.X, M).Add(Mul(v.X, dt)).Int64(),
		Mul(p0.Y, M).Add(Mul(v.Y, dt)).Int64(),
	}
}
