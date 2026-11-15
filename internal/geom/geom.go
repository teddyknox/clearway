// Package geom provides plane vectors in floating point, for the simulator
// and the planners. The kernel does not use it: the kernel computes with the
// integers of package fx.
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
