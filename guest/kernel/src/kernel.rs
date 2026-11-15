//! The Contract Store, the Clearance Verifier around it, and the Conformance
//! Monitor. Mirrors `kernel.go` and `digest.go` of the Go kernel: the same
//! inputs must give the same verdicts and the same state digests.

use crate::fx::{self, Vec2};
use crate::sha256::Sha256;
use crate::traj::{self, Leg, Trajectory, VSeg};
use crate::verifier::{self, Arrangement, SepParams, Tube};
use alloc::collections::BTreeMap;
use alloc::vec::Vec;

/// The design values of one cell, in micrometres, microseconds, and
/// micrometres per second.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct Params {
    pub sh: i64,
    pub sv: i64,
    pub vtol: i64,
    pub horizon: i64,
    pub commit_horizon: i64,
    pub intruder_horizon: i64,
    pub extend_slack: i64,
    pub w: i64,
    pub e0: i64,
    pub bh: i64,
    pub k: i64,
    pub min_lead: i64,
    pub cell_radius: i64,
    pub intruder_r0: i64,
    pub intruder_k: i64,
    pub intruder_vu: i64,
    pub recontract_tol: i64,
    pub intent_check: bool,
    pub intent_lookahead: i64,
}

impl Params {
    pub fn sv_true(&self) -> i64 {
        self.sv - 2 * self.vtol
    }
    pub fn base_r0(&self) -> i64 {
        self.w + self.e0 + self.bh
    }
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Authority {
    Automation = 0,
    Human = 1,
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum PendingState {
    None = 0,
    Awaiting = 1,
    Unknown = 2,
}

/// One surveillance report.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub struct Track {
    pub t: i64,
    pub pos: Vec2,
    pub vel: Vec2,
    pub alt: i64,
    pub vrate: i64,
    pub has_sel: bool,
    pub sel_alt: i64,
}

/// The latest time of the kernel's clock and of a track.
const MAX_CLOCK: i64 = fx::MAX_TIME / 2;

impl Track {
    pub fn valid(&self) -> bool {
        let within = |limit: i64, vals: &[i64]| vals.iter().all(|&v| v >= -limit && v <= limit);
        within(MAX_CLOCK, &[self.t])
            && within(fx::MAX_TRACK, &[self.pos.x, self.pos.y, self.alt, self.sel_alt])
            && within(fx::MAX_SPEED, &[self.vel.x, self.vel.y, self.vrate])
            && (self.has_sel || self.sel_alt == 0)
    }
}

/// A cleared trajectory with its tube.
#[derive(Clone, PartialEq, Eq, Debug)]
pub struct Contract {
    pub tube: Tube,
    pub diverge_at: i64,
    pub recontract: bool,
}

pub struct Flight {
    pub id: i64,
    pub authority: Authority,
    pub active: Option<Contract>,
    pub pending: Option<Contract>,
    pub pstate: PendingState,
    pub intruder: bool,
    pub itube: Option<Tube>,
    pub track: Track,
    pub has_track: bool,
}

/// Why the Verifier did not commit a proposal. The numbers are those of the
/// Go kernel and of the wire format.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub enum Reason {
    None = 0,
    UnknownFlight = 1,
    Duplicate = 2,
    Authority = 3,
    Intruder = 4,
    Busy = 5,
    Malformed = 6,
    Lead = 7,
    Vertical = 8,
    Open = 9,
    Conflict = 10,
    Disagree = 11,
}

/// The answer to one proposal.
#[derive(Clone, Copy, PartialEq, Eq, Debug)]
pub struct Verdict {
    pub ok: bool,
    pub reason: Reason,
    pub with: i64,
    pub at: i64,
    pub contract: u64,
}

fn reject(reason: Reason, with: i64, at: i64) -> Verdict {
    Verdict { ok: false, reason, with, at, contract: 0 }
}

fn plain(reason: Reason) -> Verdict {
    reject(reason, 0, 0)
}

#[derive(Clone, Copy, PartialEq, Eq, Debug)]
struct Conflict {
    a: i64,
    b: i64,
    t: i64,
    intruder: bool,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Exit {
    Horizontal,
    Vertical,
    Intent,
}

struct VerifyOut {
    ok: bool,
    reason: Reason,
    with: i64,
    at: i64,
    wins: Vec<(u64, i64)>,
}

fn key(x: u64, y: u64) -> (u64, u64) {
    if x > y {
        (y, x)
    } else {
        (x, y)
    }
}

const RECONTRACT_GRACE: i64 = 30 * fx::SEC;
const INTENT_SLACK: i64 = 50 * fx::FT;
const LEVEL_RATE: i64 = 5 * fx::FT;
const LEVEL_SNAP: i64 = 150 * fx::FT;
const LEVEL_GRID: i64 = 1000 * fx::FT;
const SEL_ALT_GATE: i64 = 150 * fx::FT;

/// The Store of one cell.
pub struct Kernel {
    p: Params,
    arr: Arrangement,
    now: i64,
    flights: BTreeMap<i64, Flight>,
    next_tube: u64,
    windows: BTreeMap<(u64, u64), i64>,
    conflicts: Vec<Conflict>,
    coast: i64,
}

fn inside(p: &Params, tb: &Tube, tr: &Track) -> (bool, Exit) {
    if tr.t > tb.end() {
        return (true, Exit::Horizontal);
    }
    let at = tb.traj.pos(tr.t);
    let (dx, dy) = (tr.pos.x * fx::M - at.x, tr.pos.y * fx::M - at.y);
    let lim = tb.radius_pm(tr.t) - fx::mul(p.bh, fx::M);
    if lim < 0 {
        return (false, Exit::Horizontal);
    }
    if lim <= fx::MAX_REQ as i128 && fx::sq_sum(dx, dy) > fx::sq(lim as i64) {
        return (false, Exit::Horizontal);
    }
    let (lo, hi) = tb.band(tr.t, 0);
    if tr.alt < lo - p.vtol || tr.alt > hi + p.vtol {
        return (false, Exit::Vertical);
    }
    (true, Exit::Horizontal)
}

fn intent_ok(p: &Params, tb: &Tube, tr: &Track) -> bool {
    if !p.intent_check || !tr.has_sel || tr.t > tb.end() {
        return true;
    }
    let (lo, hi) = tb.band(tr.t, p.intent_lookahead);
    tr.sel_alt >= lo - INTENT_SLACK && tr.sel_alt <= hi + INTENT_SLACK
}

impl Kernel {
    pub fn new(p: Params, arr: Arrangement) -> Kernel {
        Kernel {
            p,
            arr,
            now: 0,
            flights: BTreeMap::new(),
            next_tube: 0,
            windows: BTreeMap::new(),
            conflicts: Vec::new(),
            coast: fx::NO_LIMIT,
        }
    }

    fn sep(&self) -> SepParams {
        SepParams { sh: self.p.sh, sv_true: self.p.sv_true(), vert_slack: 0 }
    }

    fn next_id(&mut self) -> u64 {
        self.next_tube += 1;
        self.next_tube
    }

    fn closed(&self, tr: &Trajectory) -> bool {
        let end = tr.pos(tr.end());
        fx::sq_sum(end.x, end.y) >= fx::sq((self.p.cell_radius - traj::JOIN) * fx::M)
    }

    fn contract_tube(&self, id: i64, tr: Trajectory, anchor: i64, r0: i64) -> Tube {
        let vmin = tr.min_speed();
        Tube { id: 0, flight: id, traj: tr, anchor, r0, k: self.p.k, vu0: self.p.vtol, vu_rate: 0, vmin, intruder: false }
    }

    /// Forgets every window of the tube.
    fn remove_tube(&mut self, id: u64) {
        self.windows.retain(|k, _| k.0 != id && k.1 != id);
    }

    fn install(&mut self, id: u64, wins: &[(u64, i64)]) {
        for &(other, until) in wins {
            self.windows.insert(key(id, other), until);
        }
    }

    /// Checks a new tube against every protected tube of every other flight.
    fn verify_amend(&self, n: &Tube, own: i64) -> VerifyOut {
        let sp = self.sep();
        let to = self.now + self.p.horizon;
        let limit = self.now + self.p.commit_horizon;
        let mut out = VerifyOut { ok: true, reason: Reason::None, with: 0, at: 0, wins: Vec::new() };
        let fail = |reason, with, at| VerifyOut { ok: false, reason, with, at, wins: Vec::new() };
        for (&id, g) in &self.flights {
            if id == own {
                continue;
            }
            let tubes = [g.active.as_ref().map(|c| &c.tube), g.pending.as_ref().map(|c| &c.tube), g.itube.as_ref()];
            for x in tubes.into_iter().flatten() {
                let (r, hard) = verifier::check(self.arr, n, x, self.now, to, sp);
                if hard {
                    return fail(Reason::Disagree, id, r.t);
                }
                if x.intruder {
                    if r.conflict {
                        return fail(Reason::Conflict, id, r.t);
                    }
                    continue;
                }
                let mut until = to.min(n.end()).min(x.end());
                if r.conflict {
                    if r.t < limit {
                        return fail(Reason::Conflict, id, r.t);
                    }
                    until = r.t;
                }
                out.wins.push((x.id, until));
            }
        }
        out
    }

    /// Checks a re-anchored tube: no verified window may become shorter.
    fn verify_reanchor(&self, n: &Tube, old: u64, own: i64) -> VerifyOut {
        let sp = self.sep();
        let to = self.now + self.p.horizon;
        let mut out = VerifyOut { ok: true, reason: Reason::None, with: 0, at: 0, wins: Vec::new() };
        let fail = |reason, with, at| VerifyOut { ok: false, reason, with, at, wins: Vec::new() };
        for (&id, g) in &self.flights {
            if id == own {
                continue;
            }
            let tubes = [g.active.as_ref().map(|c| &c.tube), g.pending.as_ref().map(|c| &c.tube)];
            for x in tubes.into_iter().flatten() {
                let (r, hard) = verifier::check(self.arr, n, x, self.now, to, sp);
                if hard {
                    return fail(Reason::Disagree, id, r.t);
                }
                let mut until = to.min(n.end()).min(x.end());
                if r.conflict {
                    match self.windows.get(&key(old, x.id)) {
                        Some(&prev) if r.t >= prev => until = r.t,
                        _ => return fail(Reason::Conflict, id, r.t),
                    }
                }
                out.wins.push((x.id, until));
            }
        }
        out
    }

    pub fn advance(&mut self, t: i64) {
        if t > self.now && t <= MAX_CLOCK {
            self.now = t;
        }
    }

    pub fn admit(&mut self, id: i64, tr: Trajectory) -> Verdict {
        if self.flights.contains_key(&id) {
            return plain(Reason::Duplicate);
        }
        if !tr.valid() || tr.end() <= self.now {
            return plain(Reason::Malformed);
        }
        if !self.closed(&tr) {
            return plain(Reason::Open);
        }
        let mut tube = self.contract_tube(id, tr, self.now, self.p.base_r0());
        let out = self.verify_amend(&tube, id);
        if !out.ok {
            return reject(out.reason, out.with, out.at);
        }
        tube.id = self.next_id();
        self.install(tube.id, &out.wins);
        let contract = tube.id;
        self.flights.insert(
            id,
            Flight {
                id,
                authority: Authority::Automation,
                active: Some(Contract { tube, diverge_at: 0, recontract: false }),
                pending: None,
                pstate: PendingState::None,
                intruder: false,
                itube: None,
                track: Track::default(),
                has_track: false,
            },
        );
        Verdict { ok: true, reason: Reason::None, with: 0, at: 0, contract }
    }

    pub fn set_authority(&mut self, id: i64, a: Authority) {
        if let Some(f) = self.flights.get_mut(&id) {
            f.authority = a;
        }
    }

    pub fn propose(&mut self, id: i64, tail: Trajectory, by: Authority) -> Verdict {
        let (tube, td) = {
            let f = match self.flights.get(&id) {
                Some(f) => f,
                None => return plain(Reason::UnknownFlight),
            };
            if f.authority != by {
                return plain(Reason::Authority);
            }
            let act = match (&f.active, f.intruder) {
                (Some(act), false) => act,
                _ => return plain(Reason::Intruder),
            };
            if f.pstate != PendingState::None {
                return plain(Reason::Busy);
            }
            if !tail.valid() {
                return plain(Reason::Malformed);
            }
            let td = tail.start();
            if td < self.now + self.p.min_lead {
                return plain(Reason::Lead);
            }
            if td <= act.tube.start() || td >= act.tube.end() || !verifier::joins(tail.legs[0].p0, act.tube.traj.pos(td), traj::JOIN) {
                return plain(Reason::Malformed);
            }
            let (lo, hi) = act.tube.band(td, 0);
            if lo != hi {
                return plain(Reason::Vertical);
            }
            if tail.vsegs[0].a0 != lo {
                return plain(Reason::Malformed);
            }
            if !self.closed(&tail) {
                return plain(Reason::Open);
            }
            let tr = match traj::splice(&act.tube.traj, td, &tail) {
                Some(tr) => tr,
                None => return plain(Reason::Malformed),
            };
            // The aircraft may be anywhere inside the old envelope now, so
            // the new contract inherits the old anchor and radius.
            (self.contract_tube(id, tr, act.tube.anchor, act.tube.r0), td)
        };
        self.commit_pending(id, tube, td, false)
    }

    pub fn propose_recontract(&mut self, id: i64, tr: Trajectory, by: Authority) -> Verdict {
        let (tube, ts) = {
            let f = match self.flights.get(&id) {
                Some(f) => f,
                None => return plain(Reason::UnknownFlight),
            };
            if f.authority != by {
                return plain(Reason::Authority);
            }
            if !f.intruder || !f.has_track {
                return plain(Reason::Intruder);
            }
            if f.pstate != PendingState::None || f.active.is_some() {
                return plain(Reason::Busy);
            }
            if !tr.valid() {
                return plain(Reason::Malformed);
            }
            let ts = tr.start();
            if ts < self.now + self.p.min_lead {
                return plain(Reason::Lead);
            }
            if !self.starts_at_track(tr.legs[0].p0, &f.track, ts) {
                return plain(Reason::Malformed);
            }
            if !self.closed(&tr) {
                return plain(Reason::Open);
            }
            (self.contract_tube(id, tr, self.now, self.p.base_r0()), ts)
        };
        self.commit_pending(id, tube, ts, true)
    }

    fn commit_pending(&mut self, id: i64, mut tube: Tube, diverge_at: i64, recontract: bool) -> Verdict {
        let out = self.verify_amend(&tube, id);
        if !out.ok {
            return reject(out.reason, out.with, out.at);
        }
        tube.id = self.next_id();
        self.install(tube.id, &out.wins);
        let contract = tube.id;
        let f = self.flights.get_mut(&id).unwrap();
        f.pending = Some(Contract { tube, diverge_at, recontract });
        f.pstate = PendingState::Awaiting;
        Verdict { ok: true, reason: Reason::None, with: 0, at: 0, contract }
    }

    fn starts_at_track(&self, p: Vec2, tk: &Track, ts: i64) -> bool {
        let dt = ts - tk.t;
        let dx = fx::mul(p.x - tk.pos.x, fx::M) - fx::mul(tk.vel.x, dt);
        let dy = fx::mul(p.y - tk.pos.y, fx::M) - fx::mul(tk.vel.y, dt);
        let lim = (self.p.recontract_tol * fx::M) as i128;
        if dx > lim || dx < -lim || dy > lim || dy < -lim {
            return false;
        }
        fx::sq_sum(dx as i64, dy as i64) <= fx::sq(self.p.recontract_tol * fx::M)
    }

    /// The pending contract becomes active, and the old tube is released.
    fn promote(&mut self, f: &mut Flight) {
        let old = f.active.take();
        f.active = f.pending.take();
        f.pstate = PendingState::None;
        if let Some(old) = old {
            self.remove_tube(old.tube.id);
        }
    }

    fn drop_pending(&mut self, f: &mut Flight) {
        if let Some(p) = f.pending.take() {
            self.remove_tube(p.tube.id);
        }
        f.pstate = PendingState::None;
    }

    pub fn ack(&mut self, id: i64) {
        if let Some(mut f) = self.take_with_pending(id) {
            self.promote(&mut f);
            self.flights.insert(id, f);
        }
    }

    pub fn refuse(&mut self, id: i64) {
        if let Some(mut f) = self.take_with_pending(id) {
            self.drop_pending(&mut f);
            self.flights.insert(id, f);
        }
    }

    pub fn delivery_failed(&mut self, id: i64) {
        if let Some(mut f) = self.take_with_pending(id) {
            if f.intruder {
                self.drop_pending(&mut f);
            } else {
                f.pstate = PendingState::Unknown;
            }
            self.flights.insert(id, f);
        }
    }

    /// Takes the flight out of the Store if it has a pending contract. The
    /// caller puts it back.
    fn take_with_pending(&mut self, id: i64) -> Option<Flight> {
        if self.flights.get(&id)?.pending.is_none() {
            return None;
        }
        self.flights.remove(&id)
    }

    /// The Conformance Monitor.
    pub fn update_track(&mut self, id: i64, tr: Track) {
        if !self.flights.contains_key(&id) || !tr.valid() {
            return;
        }
        let mut f = self.flights.remove(&id).unwrap();
        self.monitor(&mut f, tr);
        self.flights.insert(id, f);
    }

    fn monitor(&mut self, f: &mut Flight, tr: Track) {
        let p = self.p;
        f.track = tr;
        f.has_track = true;
        if f.intruder {
            let mut expired: Option<u64> = None;
            if let Some(act) = &f.active {
                if tr.t >= act.tube.start() {
                    if inside(&p, &act.tube, &tr).0 && intent_ok(&p, &act.tube, &tr) {
                        f.intruder = false;
                        f.itube = None;
                        return;
                    }
                    if tr.t > act.tube.start() + RECONTRACT_GRACE {
                        expired = Some(act.tube.id);
                    }
                }
            }
            if let Some(tube) = expired {
                self.remove_tube(tube);
                f.active = None;
            }
            f.itube = Some(self.intruder_tube(f));
            return;
        }
        let (in_a, cause, intent_a) = match &f.active {
            Some(act) if tr.t >= act.tube.start() => {
                let (ok, cause) = inside(&p, &act.tube, &tr);
                (ok, cause, intent_ok(&p, &act.tube, &tr))
            }
            _ => return,
        };
        let before_divergence = match &f.pending {
            None => true,
            Some(pend) => tr.t < pend.diverge_at,
        };
        if before_divergence {
            // Before the divergence point the two tubes are the same.
            if !in_a {
                self.mark_intruder(f);
                let _ = cause;
            } else if !intent_a && f.pending.as_ref().map_or(true, |pend| !intent_ok(&p, &pend.tube, &tr)) {
                let _ = Exit::Intent;
                self.mark_intruder(f);
            }
            return;
        }
        let in_p = inside(&p, &f.pending.as_ref().unwrap().tube, &tr).0;
        match (in_a, in_p) {
            (true, true) => {}
            (false, true) => {
                if f.pstate == PendingState::Unknown {
                    self.promote(f);
                }
            }
            (true, false) => {
                if f.pstate == PendingState::Unknown {
                    self.drop_pending(f);
                }
            }
            (false, false) => self.mark_intruder(f),
        }
    }

    pub fn mark_intruder_input(&mut self, id: i64) {
        match self.flights.get(&id) {
            Some(f) if !f.intruder => {}
            _ => return,
        }
        let mut f = self.flights.remove(&id).unwrap();
        self.mark_intruder(&mut f);
        self.flights.insert(id, f);
    }

    fn mark_intruder(&mut self, f: &mut Flight) {
        if let Some(c) = f.pending.take() {
            self.remove_tube(c.tube.id);
        }
        if let Some(c) = f.active.take() {
            self.remove_tube(c.tube.id);
        }
        f.pstate = PendingState::None;
        f.intruder = true;
        if f.has_track {
            f.itube = Some(self.intruder_tube(f));
        }
    }

    /// A state-based tube from the last track.
    fn intruder_tube(&mut self, f: &Flight) -> Tube {
        let tr = f.track;
        let end = tr.t + self.p.intruder_horizon;
        let (mut lo, mut hi) = (tr.alt, tr.alt);
        let mut vu = self.p.vtol;
        if tr.has_sel && (tr.sel_alt - tr.alt).abs() > SEL_ALT_GATE {
            lo = tr.alt.min(tr.sel_alt);
            hi = tr.alt.max(tr.sel_alt);
            vu = self.p.intruder_vu;
        } else if tr.vrate.abs() < LEVEL_RATE {
            // The nearest cardinal level; half-way rounds up.
            let n = fx::div_floor((tr.alt + LEVEL_GRID / 2) as i128, LEVEL_GRID) as i64 * LEVEL_GRID;
            if (n - tr.alt).abs() <= LEVEL_SNAP {
                lo = n;
                hi = n;
            }
        } else {
            let tgt = tr.alt + fx::div_floor(fx::mul(tr.vrate, self.p.intruder_horizon), fx::M) as i64;
            lo = tr.alt.min(tgt);
            hi = tr.alt.max(tgt);
            vu = self.p.intruder_vu;
        }
        let traj = Trajectory {
            legs: alloc::vec![Leg { t0: tr.t, t1: end, p0: tr.pos, v: tr.vel }],
            vsegs: alloc::vec![VSeg { t0: tr.t, t1: end, a0: lo, a1: hi }],
        };
        Tube {
            id: self.next_id(),
            flight: f.id,
            traj,
            anchor: tr.t,
            r0: self.p.intruder_r0,
            k: self.p.intruder_k,
            vu0: vu,
            vu_rate: 0,
            vmin: fx::speed(tr.vel),
            intruder: true,
        }
    }

    /// Re-anchors every eligible contract on the latest track.
    pub fn reanchor_all(&mut self) {
        let ids: Vec<i64> = self.flights.keys().copied().collect();
        for id in ids {
            let (tube, old, diverge_at) = {
                let f = &self.flights[&id];
                let act = match &f.active {
                    Some(act) => act,
                    None => continue,
                };
                if f.intruder
                    || f.pstate != PendingState::None
                    || !f.has_track
                    || f.track.t != self.now
                    || self.now < act.tube.start()
                    || self.now >= act.tube.end()
                {
                    continue;
                }
                let leg = act.tube.traj.leg_at(self.now);
                let vsq = leg.v.x * leg.v.x + leg.v.y * leg.v.y;
                if vsq == 0 {
                    continue;
                }
                let at = act.tube.traj.pos(self.now);
                let (dx, dy) = (f.track.pos.x * fx::M - at.x, f.track.pos.y * fx::M - at.y);
                let dt = fx::div_floor(fx::mul(dx, leg.v.x) + fx::mul(dy, leg.v.y), vsq);
                if dt < -(fx::MAX_TIME as i128) || dt > fx::MAX_TIME as i128 {
                    continue;
                }
                let dt = dt as i64;
                let tr = act.tube.traj.shift(-dt);
                if !tr.valid() || self.now < tr.start() || self.now >= tr.end() {
                    continue;
                }
                (self.contract_tube(id, tr, self.now, self.p.base_r0()), act.tube.id, act.diverge_at - dt)
            };
            let out = self.verify_reanchor(&tube, old, id);
            if !out.ok {
                continue;
            }
            let mut tube = tube;
            tube.id = self.next_id();
            self.remove_tube(old);
            self.install(tube.id, &out.wins);
            self.flights.get_mut(&id).unwrap().active = Some(Contract { tube, diverge_at, recontract: false });
        }
    }

    pub fn remove(&mut self, id: i64) {
        if let Some(f) = self.flights.remove(&id) {
            if let Some(c) = f.pending {
                self.remove_tube(c.tube.id);
            }
            if let Some(c) = f.active {
                self.remove_tube(c.tube.id);
            }
        }
    }

    /// Extends every verified window toward the horizon and rebuilds the list
    /// of predicted conflicts.
    pub fn tick(&mut self) {
        let sp = self.sep();
        let to = self.now + self.p.horizon;
        let mut conflicts: Vec<Conflict> = Vec::new();
        let mut coast = fx::NO_LIMIT;
        {
            let mut tubes: Vec<&Tube> = Vec::new();
            for f in self.flights.values() {
                if let Some(c) = &f.active {
                    tubes.push(&c.tube);
                }
                if let Some(c) = &f.pending {
                    tubes.push(&c.tube);
                }
            }
            for (i, a) in tubes.iter().enumerate() {
                for b in &tubes[i + 1..] {
                    if a.flight == b.flight {
                        continue;
                    }
                    let target = to.min(a.end()).min(b.end());
                    let k = key(a.id, b.id);
                    let u = self.windows.get(&k).copied().unwrap_or(0);
                    if u >= target - self.p.extend_slack && u >= self.now {
                        continue;
                    }
                    let from = u.max(self.now);
                    if from >= target {
                        continue;
                    }
                    let (r, _) = verifier::check(self.arr, a, b, from, target, sp);
                    if !r.conflict {
                        self.windows.insert(k, target);
                        continue;
                    }
                    self.windows.insert(k, r.t);
                    conflicts.push(Conflict { a: a.flight, b: b.flight, t: r.t, intruder: false });
                    coast = coast.min(r.t - self.now);
                }
            }
            for (&gid, g) in &self.flights {
                let it = match &g.itube {
                    Some(it) => it,
                    None => continue,
                };
                for a in &tubes {
                    if a.flight == gid {
                        continue;
                    }
                    let (r, _) = verifier::check(self.arr, a, it, self.now, to, sp);
                    if r.conflict {
                        conflicts.push(Conflict { a: a.flight, b: gid, t: r.t, intruder: true });
                    }
                }
            }
        }
        // One entry for each ordered pair: the earliest, and of two at the
        // same time the one between two contracts. Then by time, stable.
        conflicts.sort_by(|x, y| (x.a, x.b, x.t, x.intruder).cmp(&(y.a, y.b, y.t, y.intruder)));
        conflicts.dedup_by(|later, first| later.a == first.a && later.b == first.b);
        conflicts.sort_by(|x, y| x.t.cmp(&y.t));
        self.conflicts = conflicts;
        self.coast = coast;
    }

    /// The SHA-256 digest of the Store (docs/record.md, section 5).
    pub fn state_digest(&self) -> [u8; 32] {
        let mut h = Sha256::new();
        h.update(b"clearway state 0.2\n");
        let mut e = Enc { h: &mut h };
        e.i64(self.now);
        e.u64(self.next_tube);
        e.u64(self.flights.len() as u64);
        for (&id, f) in &self.flights {
            e.i64(id);
            e.u64(f.authority as u64);
            e.u64(f.pstate as u64);
            e.flag(f.intruder);
            e.flag(f.has_track);
            if f.has_track {
                let t = &f.track;
                for v in [t.t, t.pos.x, t.pos.y, t.vel.x, t.vel.y, t.alt, t.vrate] {
                    e.i64(v);
                }
                e.flag(t.has_sel);
                e.i64(t.sel_alt);
            }
            e.contract(f.active.as_ref());
            e.contract(f.pending.as_ref());
            e.tube(f.itube.as_ref());
        }
        e.u64(self.windows.len() as u64);
        for (&(a, b), &until) in &self.windows {
            e.u64(a);
            e.u64(b);
            e.i64(until);
        }
        e.u64(self.conflicts.len() as u64);
        for c in &self.conflicts {
            e.i64(c.a);
            e.i64(c.b);
            e.i64(c.t);
            e.flag(c.intruder);
        }
        e.i64(self.coast);
        h.finish()
    }
}

/// Writes 64-bit little-endian words to a hash.
struct Enc<'a> {
    h: &'a mut Sha256,
}

impl Enc<'_> {
    fn u64(&mut self, v: u64) {
        self.h.update(&v.to_le_bytes());
    }
    fn i64(&mut self, v: i64) {
        self.u64(v as u64);
    }
    fn flag(&mut self, b: bool) {
        self.u64(b as u64);
    }
    fn tube(&mut self, t: Option<&Tube>) {
        let t = match t {
            Some(t) => t,
            None => return self.u64(0),
        };
        self.u64(1);
        self.u64(t.id);
        for v in [t.anchor, t.r0, t.k, t.vu0, t.vu_rate, t.vmin] {
            self.i64(v);
        }
        self.u64(t.traj.legs.len() as u64);
        for l in &t.traj.legs {
            for v in [l.t0, l.t1, l.p0.x, l.p0.y, l.v.x, l.v.y] {
                self.i64(v);
            }
        }
        self.u64(t.traj.vsegs.len() as u64);
        for s in &t.traj.vsegs {
            for v in [s.t0, s.t1, s.a0, s.a1] {
                self.i64(v);
            }
        }
    }
    fn contract(&mut self, c: Option<&Contract>) {
        let c = match c {
            Some(c) => c,
            None => return self.u64(0),
        };
        self.tube(Some(&c.tube));
        self.i64(c.diverge_at);
        self.flag(c.recontract);
    }
}
