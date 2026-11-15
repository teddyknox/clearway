package fx

import (
	"math/big"
	"math/rand/v2"
	"testing"
)

func bigI(a I128) *big.Int {
	v := new(big.Int).Lsh(big.NewInt(a.Hi), 64)
	return v.Add(v, new(big.Int).SetUint64(a.Lo))
}

func bigU(a U128) *big.Int {
	v := new(big.Int).Lsh(new(big.Int).SetUint64(a.Hi), 64)
	return v.Add(v, new(big.Int).SetUint64(a.Lo))
}

// num draws an int64 whose size varies from a few bits to all 63, with the
// limits themselves among the values.
func num(r *rand.Rand, maxBits int) int64 {
	switch r.IntN(12) {
	case 0:
		return 0
	case 1:
		return 1
	case 2:
		return -1
	case 3:
		return 1<<maxBits - 1
	case 4:
		return -(1<<maxBits - 1)
	}
	v := int64(r.Uint64() >> (64 - 1 - r.IntN(maxBits)))
	if r.IntN(2) == 0 {
		return -v
	}
	return v
}

// Every operation agrees with arbitrary-precision arithmetic.
func TestAgainstBig(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for n := 0; n < 200000; n++ {
		a, b, c, d := num(r, 63), num(r, 63), num(r, 63), num(r, 63)
		ab, cd := Mul(a, b), Mul(c, d)
		wantAB := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
		wantCD := new(big.Int).Mul(big.NewInt(c), big.NewInt(d))
		if bigI(ab).Cmp(wantAB) != 0 {
			t.Fatalf("Mul(%d, %d) = %v, want %v", a, b, bigI(ab), wantAB)
		}
		// Halve the operands of a sum, so that it stays inside 128 bits.
		h1, h2 := Mul(a>>1, b), Mul(c>>1, d)
		w1 := new(big.Int).Mul(big.NewInt(a>>1), big.NewInt(b))
		w2 := new(big.Int).Mul(big.NewInt(c>>1), big.NewInt(d))
		if got, want := bigI(h1.Add(h2)), new(big.Int).Add(w1, w2); got.Cmp(want) != 0 {
			t.Fatalf("Add: %v, want %v", got, want)
		}
		if got, want := bigI(h1.Sub(h2)), new(big.Int).Sub(w1, w2); got.Cmp(want) != 0 {
			t.Fatalf("Sub: %v, want %v", got, want)
		}
		if got, want := ab.Cmp(cd), wantAB.Cmp(wantCD); got != want {
			t.Fatalf("Cmp(%v, %v) = %d, want %d", wantAB, wantCD, got, want)
		}
		if got, want := ab.Sign(), wantAB.Sign(); got != want {
			t.Fatalf("Sign(%v) = %d", wantAB, got)
		}
		if got, want := ab.Fits(), wantAB.IsInt64(); got != want {
			t.Fatalf("Fits(%v) = %v", wantAB, got)
		}
		if bigI(Int(a)).Cmp(big.NewInt(a)) != 0 {
			t.Fatalf("Int(%d)", a)
		}

		// Division by a positive number, rounded down and rounded up.
		if den := Abs(num(r, 62)) + 1; true {
			for _, x := range []I128{ab, Mul(a, 1), Mul(a>>20, b>>20)} {
				bx := bigI(x)
				floor := new(big.Int).Div(bx, big.NewInt(den)) // Euclidean; equals floor for a positive divisor
				ceil := new(big.Int).Neg(new(big.Int).Div(new(big.Int).Neg(bx), big.NewInt(den)))
				q, ok := x.DivFloor(den)
				if ok != floor.IsInt64() || (ok && q != floor.Int64()) {
					t.Fatalf("DivFloor(%v, %d) = %d, %v; want %v", bx, den, q, ok, floor)
				}
				q, ok = x.DivCeil(den)
				if ok != ceil.IsInt64() || (ok && q != ceil.Int64()) {
					t.Fatalf("DivCeil(%v, %d) = %d, %v; want %v", bx, den, q, ok, ceil)
				}
			}
		}

		// Squares, as the horizontal check uses them.
		x, y := num(r, 62), num(r, 62)
		sum := SqSum(x, y)
		want := new(big.Int).Add(new(big.Int).Mul(big.NewInt(x), big.NewInt(x)), new(big.Int).Mul(big.NewInt(y), big.NewInt(y)))
		if bigU(sum).Cmp(want) != 0 {
			t.Fatalf("SqSum(%d, %d) = %v, want %v", x, y, bigU(sum), want)
		}
		other := Sq(num(r, 62))
		if got, want := bigI(sum.Diff(other)), new(big.Int).Sub(bigU(sum), bigU(other)); got.Cmp(want) != 0 {
			t.Fatalf("Diff: %v, want %v", got, want)
		}
		if got, want := sum.Cmp(other), bigU(sum).Cmp(bigU(other)); got != want {
			t.Fatalf("U128.Cmp = %d, want %d", got, want)
		}
		root := ISqrt(sum)
		if want := new(big.Int).Sqrt(bigU(sum)); !want.IsUint64() || want.Uint64() != root {
			t.Fatalf("ISqrt(%v) = %d, want %v", bigU(sum), root, want)
		}
	}
}

func TestISqrtAtTheLimits(t *testing.T) {
	for _, n := range []U128{{0, 0}, {0, 1}, {0, 3}, {0, 4}, {0, 1<<64 - 1}, {1, 0}, {1<<64 - 1, 1<<64 - 1}, {1 << 62, 0}} {
		if got, want := ISqrt(n), new(big.Int).Sqrt(bigU(n)); want.Uint64() != got {
			t.Errorf("ISqrt(%v) = %d, want %v", bigU(n), got, want)
		}
	}
	for _, v := range []Vec{{0, 0}, {3, 4}, {1, 1}, {MaxSpeed, MaxSpeed}, {-MaxSpeed, 7}} {
		lo, hi := Speed(v), SpeedUp(v)
		sq := v.X*v.X + v.Y*v.Y
		if lo*lo > sq || (lo+1)*(lo+1) <= sq || hi*hi < sq || (hi > 0 && (hi-1)*(hi-1) >= sq) {
			t.Errorf("Speed(%v) = %d, SpeedUp = %d", v, lo, hi)
		}
	}
}

// A position is exact at the limits of a valid trajectory.
func TestPos(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for n := 0; n < 100000; n++ {
		p0 := Vec{num(r, 40), num(r, 40)}
		v := Vec{num(r, 30), num(r, 30)}
		t0 := num(r, 50)
		dt := Abs(num(r, 30))
		got := Pos(p0, v, t0, t0+dt)
		for i, c := range [][3]int64{{p0.X, v.X, got.X}, {p0.Y, v.Y, got.Y}} {
			want := new(big.Int).Add(new(big.Int).Mul(big.NewInt(c[0]), big.NewInt(M)), new(big.Int).Mul(big.NewInt(c[1]), big.NewInt(dt)))
			if !want.IsInt64() || want.Int64() != c[2] {
				t.Fatalf("axis %d: Pos = %d, want %v", i, c[2], want)
			}
		}
	}
}
