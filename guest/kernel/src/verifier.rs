//! The tube, the separation standard, and the two Verifier implementations.
//! Mirrors `tube.go`, `analytic.go`, `sampled.go`, and `dual.go` of the Go
//! kernel. The answers must be the same, bit for bit.

use crate::fx::{self, Vec2};
use crate::traj::{self, Leg, Trajectory, VSeg};
use alloc::vec::Vec;

/// The protected envelope of one trajectory.
#[derive(Clone, PartialEq, Eq, Debug)]
pub struct Tube {
    pub id: u64,
    pub flight: i64,
    pub traj: Trajectory,
    pub anchor: i64,
    pub r0: i64,
    pub k: i64,
    pub vu0: i64,
    pub vu_rate: i64,
    pub vmin: i64,
    pub intruder: bool,
}

impl Tube {
    pub fn start(&self) -> i64 {
        self.traj.start()
    }
    pub fn end(&self) -> i64 {
        self.traj.end()
    }

    /// The protected horizontal radius at t, in picometres.
    pub fn radius_pm(&self, t: i64) -> i128 {
        fx::mul(self.r0, fx::M) + fx::mul(self.k, (t - self.anchor).max(0))
    }

    /// The vertical uncertainty at t, in picometres.
    pub fn vu_pm(&self, t: i64) -> i128 {
        fx::mul(self.vu0, fx::M) + fx::mul(self.vu_rate, (t - self.anchor).max(0))
    }

    /// Whether the vertical segment s counts at time t, with its start moved
    /// earlier by `early`.
    fn in_effect(&self, s: &VSeg, t: i64, early: i64) -> bool {
        if self.vmin == 0 {
            return s.t0 - early <= t && t <= s.t1;
        }
        let r = self.radius_pm(t);
        fx::mul(self.vmin, s.t0 - early - t) <= r && fx::mul(self.vmin, t - s.t1) <= r
    }

    /// The vertical extent of the tube at t, with the start of each segment
    /// moved earlier by `early`.
    pub fn band(&self, t: i64, early: i64) -> (i64, i64) {
        let mut found: Option<(i64, i64)> = None;
        for s in &self.traj.vsegs {
            if !self.in_effect(s, t, early) {
                continue;
            }
            found = Some(match found {
                None => (s.lo(), s.hi()),
                Some((lo, hi)) => (lo.min(s.lo()), hi.max(s.hi())),
            });
        }
        match found {
            Some(b) => b,
            None => {
                let segs = &self.traj.vsegs;
                let s = if t > self.end() { &segs[segs.len() - 1] } else { &segs[0] };
                (s.lo(), s.hi())
            }
        }
    }
}

/// The separation standard that a checker applies.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct SepParams {
    pub sh: i64,
    pub sv_true: i64,
    pub vert_slack: i64,
}

/// The outcome of one pair check.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct CheckResult {
    pub conflict: bool,
    pub t: i64,
}

fn span(a: &Tube, b: &Tube, from: i64, to: i64) -> (i64, i64) {
    let lo = from.max(a.start()).max(b.start()).max(a.anchor).max(b.anchor);
    let hi = to.min(a.end()).min(b.end());
    (lo, hi)
}

/// The required distance at t in picometres, or None if it is above MAX_REQ.
fn req_pm(a: &Tube, b: &Tube, sh: i64, t: i64) -> Option<i64> {
    let r = fx::mul(sh, fx::M) + a.radius_pm(t) + b.radius_pm(t);
    if r > fx::MAX_REQ as i128 {
        None
    } else {
        Some(r as i64)
    }
}

fn vert_separated(a: &Tube, b: &Tube, a_band: (i64, i64), b_band: (i64, i64), sv_true: i64, t: i64) -> bool {
    let gap = (a_band.0 - b_band.1).max(b_band.0 - a_band.1) - sv_true;
    gap >= 0 && fx::mul(gap, fx::M) >= a.vu_pm(t) + b.vu_pm(t)
}

// ---------------------------------------------------------------- analytic

const NEVER: i64 = -fx::NO_LIMIT;
const ALWAYS: i64 = fx::NO_LIMIT;

struct VertEvent {
    open: i64,
    close: i64,
    lo: i64,
    hi: i64,
}

fn clamp_div(q: i128, n: i128) -> i64 {
    if q > NEVER as i128 && q < ALWAYS as i128 {
        q as i64
    } else if n < 0 {
        NEVER
    } else {
        ALWAYS
    }
}

fn floor_div(n: i128, d: i64) -> i64 {
    clamp_div(fx::div_floor(n, d), n)
}

fn ceil_div(n: i128, d: i64) -> i64 {
    clamp_div(fx::div_ceil(n, d), n)
}

/// Moves the end of a span by d. An end that is NEVER or ALWAYS stays, and an
/// end that would pass one of them becomes it.
fn widen(t: i64, d: i64) -> i64 {
    if t <= NEVER || t >= ALWAYS {
        t
    } else if d < 0 && t < NEVER - d {
        NEVER
    } else if d > 0 && t > ALWAYS - d {
        ALWAYS
    } else {
        t + d
    }
}

/// The spans over which the vertical segments are in effect under the slack
/// w: each segment is in effect at t if it is in effect, with no slack, at
/// some whole microsecond from t − w to t + w. The radius is M·r0 up to the
/// anchor and grows from it, so each inequality of the rule is solved once for
/// each side of the anchor. One segment has at most two spans.
fn events(tb: &Tube, w: i64) -> Vec<VertEvent> {
    let mut ev = Vec::with_capacity(tb.traj.vsegs.len());
    let base = fx::mul(tb.r0, fx::M) - fx::mul(tb.k, tb.anchor); // from the anchor, R(t) = base + k·t
    // Up to the anchor, the allowance: floor(M·r0 / vmin). M·r0 is below 2^60.
    let early = if tb.vmin > 0 { tb.r0 * fx::M / tb.vmin } else { 0 };
    for s in &tb.traj.vsegs {
        let mut add = |open: i64, close: i64| {
            if open <= close {
                ev.push(VertEvent { open: widen(open, -w), close: widen(close, w), lo: s.lo(), hi: s.hi() });
            }
        };
        if tb.vmin == 0 {
            add(s.t0, s.t1);
            continue;
        }
        // Up to the anchor.
        let open1 = s.t0 - early;
        let close1 = (s.t1 + early).min(tb.anchor);
        // From the anchor.
        let mut open2 = ceil_div(fx::mul(tb.vmin, s.t0) - base, tb.vmin + tb.k).max(tb.anchor);
        let mut close2 = ALWAYS;
        let n = fx::mul(tb.vmin, s.t1) + base;
        if tb.vmin > tb.k {
            close2 = floor_div(n, tb.vmin - tb.k);
        } else if tb.vmin == tb.k {
            if n < 0 {
                close2 = NEVER;
            }
        } else {
            open2 = open2.max(ceil_div(-n, tb.k - tb.vmin));
        }
        if open1 <= close1 && open2 <= close2 && close1 == tb.anchor && open2 == tb.anchor {
            add(open1, close2); // the two spans meet at the anchor
            continue;
        }
        add(open1, close1);
        add(open2, close2);
    }
    ev
}

fn band_over(ev: &[VertEvent], t0: i64, t1: i64) -> Option<(i64, i64)> {
    let mut found: Option<(i64, i64)> = None;
    for e in ev {
        if e.open > t1 || e.close < t0 {
            continue;
        }
        found = Some(match found {
            None => (e.lo, e.hi),
            Some((lo, hi)) => (lo.min(e.lo), hi.max(e.hi)),
        });
    }
    found
}

/// The least t from lo to hi for which pred holds. pred must hold at hi, and
/// at every time after one at which it holds.
fn first_true(mut lo: i64, mut hi: i64, pred: &dyn Fn(i64) -> bool) -> i64 {
    while lo < hi {
        let mid = lo + (hi - lo) / 2;
        if pred(mid) {
            hi = mid;
        } else {
            lo = mid + 1;
        }
    }
    lo
}

fn first_horizontal(a: &Tube, b: &Tube, la: &Leg, lb: &Leg, sh: i64, s: i64, e: i64) -> Option<i64> {
    let mut e = e;
    let mut capped: Option<i64> = None;
    if req_pm(a, b, sh, e).is_none() {
        if req_pm(a, b, sh, s).is_none() {
            return Some(s);
        }
        let first = first_true(s, e, &|t| req_pm(a, b, sh, t).is_none());
        capped = Some(first);
        e = first - 1;
    }
    let q = |t: i64| -> i128 {
        let (pa, pb) = (la.pos(t), lb.pos(t));
        let req = req_pm(a, b, sh, t).unwrap_or(0);
        // Both terms are below 2^126: the difference fits.
        fx::sq_sum(pa.x - pb.x, pa.y - pb.y) as i128 - fx::sq(req) as i128
    };
    let neg = |t: i64| q(t) < 0;
    let found = || -> Option<i64> {
        if neg(s) {
            return Some(s);
        }
        if e == s {
            return None;
        }
        let (dvx, dvy, m) = (la.v.x - lb.v.x, la.v.y - lb.v.y, a.k + b.k);
        let lead = fx::mul(dvx, dvx) + fx::mul(dvy, dvy) - fx::mul(m, m);
        if lead <= 0 {
            if !neg(e) {
                return None;
            }
            return Some(first_true(s, e, &neg));
        }
        let rising = |t: i64| q(t + 1) >= q(t);
        let mut least = e;
        if rising(e - 1) {
            least = first_true(s, e - 1, &rising);
        }
        if !neg(least) {
            return None;
        }
        Some(first_true(s, least, &neg))
    };
    found().or(capped)
}

/// Verifier implementation A: exact.
pub fn analytic(a: &Tube, b: &Tube, from: i64, to: i64, sp: SepParams) -> CheckResult {
    let (lo, hi) = span(a, b, from, to);
    if hi <= lo {
        return CheckResult::default();
    }
    let eva = events(a, sp.vert_slack);
    let evb = events(b, sp.vert_slack);

    if let (Some(ab), Some(bb)) = (band_over(&eva, lo, hi), band_over(&evb, lo, hi)) {
        if vert_separated(a, b, ab, bb, sp.sv_true, hi) {
            return CheckResult::default();
        }
    }

    let mut bps: Vec<i64> = Vec::with_capacity(16);
    bps.push(lo);
    bps.push(hi + 1);
    let mut add = |t: i64| {
        if t > lo && t <= hi {
            bps.push(t);
        }
    };
    for tr in [&a.traj, &b.traj] {
        for i in 0..tr.legs.len() - 1 {
            add(tr.legs[i].t1);
        }
    }
    for ev in [&eva, &evb] {
        for e in ev.iter() {
            if e.open > NEVER {
                add(e.open);
            }
            if e.close < ALWAYS {
                add(e.close + 1);
            }
        }
    }
    bps.sort_unstable();

    for i in 0..bps.len() - 1 {
        let (s, e) = (bps[i], bps[i + 1] - 1);
        if e < s {
            continue;
        }
        let bands = (band_over(&eva, s, s), band_over(&evb, s, s));
        let mut hs = s;
        if let (Some(ab), Some(bb)) = bands {
            if vert_separated(a, b, ab, bb, sp.sv_true, s) {
                if vert_separated(a, b, ab, bb, sp.sv_true, e) {
                    continue;
                }
                hs = first_true(s, e, &|t| !vert_separated(a, b, ab, bb, sp.sv_true, t));
            }
        }
        if let Some(t) = first_horizontal(a, b, a.traj.leg_at(hs), b.traj.leg_at(hs), sp.sh, hs, e) {
            return CheckResult { conflict: true, t };
        }
    }
    CheckResult::default()
}

// ----------------------------------------------------------------- sampled

/// The union of the bands of the vertical segments that the aircraft may
/// occupy at some whole microsecond from t0 to t1. The test of the two ends is
/// exact: `occupied` of sampled.go gives the reasons.
fn occupied(tb: &Tube, t0: i64, t1: i64) -> Option<(i64, i64)> {
    let (r0, r1) = (tb.radius_pm(t0), tb.radius_pm(t1));
    let mut found: Option<(i64, i64)> = None;
    for s in &tb.traj.vsegs {
        let inside = if s.t0 <= t1 && t0 <= s.t1 {
            true // nominal at a time of the interval
        } else if tb.vmin == 0 {
            false // no allowance
        } else if s.t0 > t1 {
            fx::mul(tb.vmin, s.t0 - t1) <= r1 // ahead: entered by t1
        } else {
            // Behind: not left at t0, or not left at t1.
            fx::mul(tb.vmin, t0 - s.t1) <= r0 || fx::mul(tb.vmin, t1 - s.t1) <= r1
        };
        if !inside {
            continue;
        }
        found = Some(match found {
            None => (s.lo(), s.hi()),
            Some((lo, hi)) => (lo.min(s.lo()), hi.max(s.hi())),
        });
    }
    found
}

fn node(a: &Tube, b: &Tube, lo: i64, hi: i64, sp: SepParams, step: i64, res: &mut CheckResult) -> bool {
    let bands = (
        occupied(a, lo - sp.vert_slack, hi + sp.vert_slack),
        occupied(b, lo - sp.vert_slack, hi + sp.vert_slack),
    );
    let u = a.vu_pm(hi) + b.vu_pm(hi);
    let clear = |gap: i64| gap >= 0 && fx::mul(gap, fx::M) >= u;
    if let (Some((a_lo, a_hi)), Some((b_lo, b_hi))) = bands {
        if clear(a_lo - b_hi - sp.sv_true) || clear(b_lo - a_hi - sp.sv_true) {
            return false;
        }
    }
    let mid = lo + (hi - lo) / 2;
    let (pa, pb) = (a.traj.pos(mid), b.traj.pos(mid));
    let d = fx::isqrt(fx::sq_sum(pa.x - pb.x, pa.y - pb.y));
    let (va, na) = a.traj.speed_over(lo, hi);
    let (vb, nb) = b.traj.speed_over(lo, hi);
    let closure = fx::mul(va + vb, (mid - lo).max(hi - mid)) + fx::mul(na + nb - 2, 2 * traj::JOIN * fx::M);
    let req = fx::mul(sp.sh, fx::M) + a.radius_pm(hi) + b.radius_pm(hi);
    if req <= fx::MAX_REQ as i128 && d as i128 - closure >= req {
        return false;
    }
    if hi - lo <= step {
        res.conflict = true;
        res.t = lo;
        return true;
    }
    if node(a, b, lo, mid, sp, step, res) {
        return true;
    }
    node(a, b, mid + 1, hi, sp, step, res)
}

/// Verifier implementation B: conservative, by sampling.
pub fn sampled(a: &Tube, b: &Tube, from: i64, to: i64, sp: SepParams, step: i64) -> CheckResult {
    let (lo, hi) = span(a, b, from, to);
    if hi <= lo {
        return CheckResult::default();
    }
    let mut res = CheckResult::default();
    node(a, b, lo, hi, sp, step, &mut res);
    res
}

// -------------------------------------------------------------------- dual

/// The tolerance of the specification, section 8.
pub const TOL: i64 = 3 * fx::NM / 4;
pub const TIME_TOL: i64 = fx::SEC;

/// The Verifier arrangement of a kernel.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Arrangement {
    /// The exact implementation alone.
    Single,
    /// The exact and the sampled implementation: both must approve.
    Dual,
}

/// Runs the arrangement. Returns the merged result, and whether the two
/// implementations disagree beyond the tolerance.
pub fn check(arr: Arrangement, a: &Tube, b: &Tube, from: i64, to: i64, sp: SepParams) -> (CheckResult, bool) {
    let ra = analytic(a, b, from, to, sp);
    if arr == Arrangement::Single {
        return (ra, false);
    }
    let rb = sampled(a, b, from, to, sp, fx::SEC);
    if ra.conflict == rb.conflict {
        let mut res = ra;
        if ra.conflict {
            res.t = ra.t.min(rb.t);
        }
        return (res, false);
    }
    let tight = SepParams {
        sh: sp.sh + TOL,
        sv_true: sp.sv_true + fx::div_ceil(fx::mul(a.vu_rate + b.vu_rate, TIME_TOL), fx::M) as i64,
        vert_slack: sp.vert_slack + TIME_TOL,
    };
    let (still_clear, rej) = if ra.conflict {
        (!sampled(a, b, from, to, tight, fx::SEC).conflict, ra)
    } else {
        (!analytic(a, b, from, to, tight).conflict, rb)
    };
    (rej, still_clear)
}

/// The position of a point p (µm) relative to a position at (pm), on each
/// axis, compared with a tolerance (µm).
pub fn joins(p: Vec2, at: Vec2, tol: i64) -> bool {
    let dx = fx::mul(p.x, fx::M) - at.x as i128;
    let dy = fx::mul(p.y, fx::M) - at.y as i128;
    let lim = (tol * fx::M) as i128;
    dx <= lim && dx >= -lim && dy <= lim && dy >= -lim
}
