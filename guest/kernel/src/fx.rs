//! The arithmetic of the kernel: signed 64-bit integers in fixed units, with
//! exact 128-bit intermediate values. No floating point.
//!
//! Units (specification, section 1): length in micrometres, time in
//! microseconds, speed in micrometres per second. One micrometre per second
//! for one microsecond is one picometre, so a position at a whole microsecond
//! is an exact number of picometres: `M·p0 + v·(t − t0)`.
//!
//! This module mirrors `internal/fx` of the Go kernel. The two must give the
//! same bits.

/// Picometres per micrometre, and microseconds per second.
pub const M: i64 = 1_000_000;
pub const SEC: i64 = 1_000_000;
pub const NM: i64 = 1_852_000_000;
pub const FT: i64 = 304_800;

/// Limits of valid values.
pub const MAX_TIME: i64 = 1 << 50;
pub const MAX_COORD: i64 = 1 << 41;
pub const MAX_TRACK: i64 = 1 << 40;
pub const MAX_SPEED: i64 = 1 << 30;
pub const MAX_LEN: i64 = 1 << 40;

/// The largest required distance that is compared exactly (pm).
pub const MAX_REQ: i64 = 1 << 62;

/// The time that stands for "no limit".
pub const NO_LIMIT: i64 = i64::MAX;

/// A point (µm) or a velocity (µm/s) in the horizontal plane.
#[derive(Clone, Copy, PartialEq, Eq, Default, Debug)]
pub struct Vec2 {
    pub x: i64,
    pub y: i64,
}

impl Vec2 {
    pub fn add(self, b: Vec2) -> Vec2 {
        Vec2 { x: self.x + b.x, y: self.y + b.y }
    }
}

/// The exact product a·b.
#[inline]
pub fn mul(a: i64, b: i64) -> i128 {
    a as i128 * b as i128
}

/// floor(n / d) for d > 0.
#[inline]
pub fn div_floor(n: i128, d: i64) -> i128 {
    n.div_euclid(d as i128)
}

/// ceil(n / d) for d > 0.
#[inline]
pub fn div_ceil(n: i128, d: i64) -> i128 {
    -((-n).div_euclid(d as i128))
}

/// Whether q is an i64.
#[inline]
pub fn fits(q: i128) -> bool {
    q >= i64::MIN as i128 && q <= i64::MAX as i128
}

/// a².
#[inline]
pub fn sq(a: i64) -> u128 {
    let m = a.unsigned_abs() as u128;
    m * m
}

/// x² + y². Requires |x|, |y| <= 2^62.
#[inline]
pub fn sq_sum(x: i64, y: i64) -> u128 {
    sq(x) + sq(y)
}

/// floor(sqrt(n)), by the digit-by-digit method.
pub fn isqrt(mut n: u128) -> u64 {
    let mut res: u128 = 0;
    let mut bit: u128 = 1 << 126;
    while bit > n {
        bit >>= 2;
    }
    while bit != 0 {
        let t = res + bit;
        if n >= t {
            n -= t;
            res = (res >> 1) + bit;
        } else {
            res >>= 1;
        }
        bit >>= 2;
    }
    res as u64
}

/// The length of a velocity, rounded down. Requires |v| <= MAX_SPEED on each
/// axis.
pub fn speed(v: Vec2) -> i64 {
    isqrt((v.x * v.x + v.y * v.y) as u128) as i64
}

/// The length of a velocity, rounded up.
pub fn speed_up(v: Vec2) -> i64 {
    let n = (v.x * v.x + v.y * v.y) as u64;
    let mut r = isqrt(n as u128);
    if r * r < n {
        r += 1;
    }
    r as i64
}

/// The position, in picometres, of a point that is at p0 (µm) at t0 and moves
/// at v (µm/s), at time t. Requires a result inside ±2^62 on each axis.
#[inline]
pub fn pos(p0: Vec2, v: Vec2, t0: i64, t: i64) -> Vec2 {
    let dt = t - t0;
    Vec2 { x: (mul(p0.x, M) + mul(v.x, dt)) as i64, y: (mul(p0.y, M) + mul(v.y, dt)) as i64 }
}
