// Package geom provides plane vectors and small numeric helpers.
//
// Units: nautical miles and seconds.
package geom

import "math"

// Kt is one knot in nautical miles per second.
const Kt = 1.0 / 3600.0

// Vec2 is a point or a displacement in the horizontal plane.
type Vec2 struct{ X, Y float64 }

func (a Vec2) Add(b Vec2) Vec2      { return Vec2{a.X + b.X, a.Y + b.Y} }
func (a Vec2) Sub(b Vec2) Vec2      { return Vec2{a.X - b.X, a.Y - b.Y} }
func (a Vec2) Scale(s float64) Vec2 { return Vec2{a.X * s, a.Y * s} }
func (a Vec2) Dot(b Vec2) float64   { return a.X*b.X + a.Y*b.Y }
func (a Vec2) Norm() float64        { return math.Hypot(a.X, a.Y) }
func (a Vec2) Dist(b Vec2) float64  { return a.Sub(b).Norm() }

// Perp returns a rotated a quarter turn counter-clockwise.
func (a Vec2) Perp() Vec2 { return Vec2{-a.Y, a.X} }

// Unit returns the unit vector along a, or the zero vector if a is zero.
func (a Vec2) Unit() Vec2 {
	n := a.Norm()
	if n == 0 {
		return Vec2{}
	}
	return a.Scale(1 / n)
}

// Rotate returns a rotated counter-clockwise by rad radians.
func (a Vec2) Rotate(rad float64) Vec2 {
	s, c := math.Sincos(rad)
	return Vec2{a.X*c - a.Y*s, a.X*s + a.Y*c}
}

// Polar returns the point at distance r and angle theta from the origin.
func Polar(r, theta float64) Vec2 {
	s, c := math.Sincos(theta)
	return Vec2{r * c, r * s}
}

// SolveQuadratic returns the real roots of a·x² + b·x + c = 0 in ascending
// order. n is the number of roots returned: 0, 1 (linear case), or 2.
func SolveQuadratic(a, b, c float64) (r1, r2 float64, n int) {
	if math.Abs(a) < 1e-14 {
		if math.Abs(b) < 1e-14 {
			return 0, 0, 0
		}
		return -c / b, 0, 1
	}
	disc := b*b - 4*a*c
	if disc < 0 {
		return 0, 0, 0
	}
	s := math.Sqrt(disc)
	var q float64
	if b >= 0 {
		q = -0.5 * (b + s)
	} else {
		q = -0.5 * (b - s)
	}
	r1 = q / a
	if q != 0 {
		r2 = c / q
	} else {
		r2 = r1
	}
	if r1 > r2 {
		r1, r2 = r2, r1
	}
	return r1, r2, 2
}
