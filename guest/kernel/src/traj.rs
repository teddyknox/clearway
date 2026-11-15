//! Trajectories of the kernel. Mirrors `internal/traj` of the Go kernel.

use crate::fx::{self, Vec2};
use alloc::vec::Vec;

/// The largest distance, on each axis, between the end of a leg and the start
/// of the next (µm).
pub const JOIN: i64 = 18_520;

/// The largest number of legs, and of vertical segments, in one trajectory.
pub const MAX_PARTS: usize = 256;

/// A straight horizontal segment flown at constant ground velocity.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct Leg {
    pub t0: i64,
    pub t1: i64,
    pub p0: Vec2,
    pub v: Vec2,
}

impl Leg {
    /// The position on the leg at t, in picometres.
    #[inline]
    pub fn pos(&self, t: i64) -> Vec2 {
        fx::pos(self.p0, self.v, self.t0, t)
    }
}

/// A vertical segment: level if a0 == a1, otherwise a transition that holds
/// its whole band for its whole duration.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct VSeg {
    pub t0: i64,
    pub t1: i64,
    pub a0: i64,
    pub a1: i64,
}

impl VSeg {
    pub fn lo(&self) -> i64 {
        self.a0.min(self.a1)
    }
    pub fn hi(&self) -> i64 {
        self.a0.max(self.a1)
    }
}

#[derive(Clone, PartialEq, Eq, Debug, Default)]
pub struct Trajectory {
    pub legs: Vec<Leg>,
    pub vsegs: Vec<VSeg>,
}

fn inside(limit: i64, vals: &[i64]) -> bool {
    vals.iter().all(|&v| v >= -limit && v <= limit)
}

fn alt_at(s: &VSeg, t: i64) -> i64 {
    s.a0 + fx::div_floor(fx::mul(s.a1 - s.a0, t - s.t0), s.t1 - s.t0) as i64
}

impl Trajectory {
    /// The start of the first leg. The trajectory must have a leg.
    pub fn start(&self) -> i64 {
        self.legs[0].t0
    }

    pub fn end(&self) -> i64 {
        self.legs[self.legs.len() - 1].t1
    }

    /// Whether the trajectory is valid. See `Trajectory.Validate` in
    /// `internal/traj`: the two accept the same trajectories.
    pub fn valid(&self) -> bool {
        if self.legs.is_empty() || self.vsegs.is_empty() {
            return false;
        }
        if self.legs.len() > MAX_PARTS || self.vsegs.len() > MAX_PARTS {
            return false;
        }
        let limit = fx::mul(fx::MAX_COORD, fx::M);
        let join = (JOIN * fx::M) as i128;
        let mut prev_end = [0i128; 2];
        for (i, l) in self.legs.iter().enumerate() {
            if l.t1 <= l.t0
                || !inside(fx::MAX_TIME, &[l.t0, l.t1])
                || !inside(fx::MAX_COORD, &[l.p0.x, l.p0.y])
                || !inside(fx::MAX_SPEED, &[l.v.x, l.v.y])
            {
                return false;
            }
            let start = [fx::mul(l.p0.x, fx::M), fx::mul(l.p0.y, fx::M)];
            let end = [start[0] + fx::mul(l.v.x, l.t1 - l.t0), start[1] + fx::mul(l.v.y, l.t1 - l.t0)];
            for e in end {
                if e > limit || e < -limit {
                    return false;
                }
            }
            if i > 0 {
                if l.t0 != self.legs[i - 1].t1 {
                    return false;
                }
                for axis in 0..2 {
                    let d = start[axis] - prev_end[axis];
                    if d > join || d < -join {
                        return false;
                    }
                }
            }
            prev_end = end;
        }
        for (i, s) in self.vsegs.iter().enumerate() {
            if s.t1 <= s.t0 || !inside(fx::MAX_TIME, &[s.t0, s.t1]) || !inside(fx::MAX_COORD, &[s.a0, s.a1]) {
                return false;
            }
            if i > 0 && (s.t0 != self.vsegs[i - 1].t1 || s.a0 != self.vsegs[i - 1].a1) {
                return false;
            }
        }
        self.vsegs[0].t0 == self.start() && self.vsegs[self.vsegs.len() - 1].t1 == self.end()
    }

    /// The leg in effect at t: the first leg that ends after t, or the last.
    pub fn leg_at(&self, t: i64) -> &Leg {
        for l in &self.legs {
            if l.t1 > t {
                return l;
            }
        }
        &self.legs[self.legs.len() - 1]
    }

    fn vseg_at(&self, t: i64) -> &VSeg {
        for s in &self.vsegs {
            if s.t1 > t {
                return s;
            }
        }
        &self.vsegs[self.vsegs.len() - 1]
    }

    fn clamp(&self, t: i64) -> i64 {
        self.start().max(self.end().min(t))
    }

    /// The nominal position at t, in picometres.
    pub fn pos(&self, t: i64) -> Vec2 {
        let t = self.clamp(t);
        self.leg_at(t).pos(t)
    }

    /// The nominal altitude at t, rounded down.
    pub fn alt_at(&self, t: i64) -> i64 {
        let t = self.clamp(t);
        alt_at(self.vseg_at(t), t)
    }

    /// The lowest ground speed of any leg, rounded down.
    pub fn min_speed(&self) -> i64 {
        self.legs.iter().map(|l| fx::speed(l.v)).min().unwrap()
    }

    /// The highest ground speed of any leg that overlaps t0 to t1, rounded
    /// up, and the number of those legs.
    pub fn speed_over(&self, t0: i64, t1: i64) -> (i64, i64) {
        let mut speed = 0;
        let mut legs = 0;
        for l in &self.legs {
            if l.t1 < t0 || l.t0 > t1 {
                continue;
            }
            speed = speed.max(fx::speed_up(l.v));
            legs += 1;
        }
        if legs == 0 {
            return (fx::speed_up(self.leg_at(t0).v), 1);
        }
        (speed, legs)
    }

    /// A copy with dt added to every time. The caller must check the result.
    /// The caller must keep |dt| <= 2·MAX_TIME, so that no time overflows.
    pub fn shift(&self, dt: i64) -> Trajectory {
        Trajectory {
            legs: self.legs.iter().map(|l| Leg { t0: l.t0 + dt, t1: l.t1 + dt, ..*l }).collect(),
            vsegs: self.vsegs.iter().map(|s| VSeg { t0: s.t0 + dt, t1: s.t1 + dt, ..*s }).collect(),
        }
    }
}

/// base up to time t followed by tail. None if t is not strictly inside base,
/// if tail does not start at t, or if the result is not valid.
pub fn splice(base: &Trajectory, t: i64, tail: &Trajectory) -> Option<Trajectory> {
    if t <= base.start() || t >= base.end() || tail.start() != t {
        return None;
    }
    let mut out = Trajectory::default();
    for l in &base.legs {
        if l.t0 >= t {
            break;
        }
        let mut l = *l;
        if l.t1 > t {
            l.t1 = t;
        }
        out.legs.push(l);
    }
    out.legs.extend_from_slice(&tail.legs);
    for s in &base.vsegs {
        if s.t0 >= t {
            break;
        }
        let mut s = *s;
        if s.t1 > t {
            s.a1 = alt_at(&s, t);
            s.t1 = t;
        }
        out.vsegs.push(s);
    }
    out.vsegs.extend_from_slice(&tail.vsegs);
    if out.valid() {
        Some(out)
    } else {
        None
    }
}
