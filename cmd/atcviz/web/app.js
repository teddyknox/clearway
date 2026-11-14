// Clearway cell viewer. Plain JavaScript, no dependencies.
//
// The server runs one scenario and returns a recording: a frame every five
// seconds, the paths of all contracts, and a list of events. This file replays
// it. Nothing here decides anything about the traffic.
(() => {
'use strict';

const $ = (id) => document.getElementById(id);

const STATE_NAME = [
  'Under contract, conforming',
  'Amendment sent, waiting for the crew',
  'No answer: either route may be flown',
  'Intruder: outside the kernel\'s guarantee',
];
const REASON = {
  conflict: 'Would conflict with another flight',
  open: 'Route ends inside the cell',
  malformed: 'Does not connect to the current route',
  lead: 'Too late to deliver',
  vertical: 'Starts inside a level change',
  busy: 'An amendment is already pending',
  intruder: 'Flight has no contract to amend',
  disagree: 'The two Verifiers disagree',
  authority: 'Proposer does not hold authority',
  'unknown-flight': 'Unknown flight',
  duplicate: 'Flight already admitted',
};
const REASON_SHORT = {
  conflict: 'conflict', open: 'ends inside the cell', malformed: 'does not connect', lead: 'too late',
  vertical: 'inside a level change', busy: 'already pending', intruder: 'no contract', disagree: 'Verifiers disagree',
};
const SOURCE = { planner: 'Planner', chaos: 'Hostile planner', fallback: 'Fallback Resolver', recontract: 'Re-contract' };
const FL_OFF_ROUTE = 1, FL_OFF_LEVEL = 2, FL_NO_RADIO = 4, FL_INSTRUCTED = 8, FL_INSIDE = 16;
const ALT_LO = 29000, ALT_HI = 41000;

const S = {
  scenarios: [], scenario: null, rec: null,
  t: 0, playing: false, speed: 30,
  sel: null, hover: null,
  view: { x: 0, y: 0, k: 1 },
  layers: { labels: true, routes: false, envelopes: false, conflicts: true, rejected: true, truth: true },
  col: {}, font: 'sans-serif', dirty: true, screen: [], lastPanel: 0, logKey: '',
};
let C = {}, N = {}, F = [], DT = 5, DUR = 0, R = 150, META = null;
let PROPS = [], NOTABLE = [], MARKS = [], LOSSES = [], INSTR = [], ACKS = [];
let buckets = null;

// ---------- small helpers ----------

const pad2 = (n) => String(n).padStart(2, '0');
function clock(s) {
  s = Math.max(0, Math.floor(s));
  return `${pad2(Math.floor(s / 3600))}:${pad2(Math.floor(s / 60) % 60)}:${pad2(s % 60)}`;
}
const fl = (alt) => 'FL' + String(Math.round(alt / 100)).padStart(3, '0');
const num = (n) => n.toLocaleString('en-US');

function alpha(hex, a) {
  const v = parseInt(hex.slice(1), 16);
  return `rgba(${v >> 16}, ${(v >> 8) & 255}, ${v & 255}, ${a})`;
}

function lowerBound(arr, t) {
  let lo = 0, hi = arr.length;
  while (lo < hi) {
    const m = (lo + hi) >> 1;
    if (arr[m].t < t) lo = m + 1; else hi = m;
  }
  return lo;
}

function readColors() {
  const cs = getComputedStyle(document.documentElement);
  for (const k of ['map', 'chrome', 'panel', 'line', 'ink', 'ink-2', 'ink-3', 'blue', 'orange', 'aqua']) {
    S.col[k] = cs.getPropertyValue('--' + k).trim();
  }
  S.font = cs.getPropertyValue('--cond').trim() || 'sans-serif';
  S.dirty = true;
}

function fit(canvas) {
  const r = canvas.getBoundingClientRect();
  const dpr = window.devicePixelRatio || 1;
  const w = Math.max(1, Math.round(r.width)), h = Math.max(1, Math.round(r.height));
  if (canvas.width !== Math.round(w * dpr) || canvas.height !== Math.round(h * dpr)) {
    canvas.width = Math.round(w * dpr);
    canvas.height = Math.round(h * dpr);
  }
  const ctx = canvas.getContext('2d');
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  return { ctx, w, h };
}

// ---------- recording ----------

function index(rec) {
  S.rec = rec;
  META = rec.meta;
  C = {}; META.aircraft.forEach((name, i) => { C[name] = i; });
  N = {}; META.counters.forEach((name, i) => { N[name] = i; });
  F = rec.frames; DT = META.frameEvery; DUR = META.duration; R = META.radius;
  for (const f of F) f._m = null;
  PROPS = []; NOTABLE = []; MARKS = []; LOSSES = []; INSTR = []; ACKS = [];
  let alarms = 0;
  for (const e of rec.events) {
    switch (e.k) {
      case 'propose': PROPS.push(e); if (e.ok) NOTABLE.push(e); break;
      case 'ack': ACKS.push(e); break;
      case 'admit': break;
      case 'alarm': if (alarms++ < 1) NOTABLE.push(e); MARKS.push(e); break;
      case 'loss': LOSSES.push(e); NOTABLE.push(e); MARKS.push(e); break;
      case 'instruct': INSTR.push(e); NOTABLE.push(e); MARKS.push(e); break;
      case 'fault': case 'intruder': case 'alert': NOTABLE.push(e); MARKS.push(e); break;
      default: NOTABLE.push(e);
    }
  }
  buckets = null;
  buildReasons();
}

function fmap(f) {
  if (!f._m) f._m = new Map((f.a || []).map((r) => [r[C.id], r]));
  return f._m;
}

function frameIndex(t) {
  return Math.min(F.length - 1, Math.floor(t / DT + 1e-9) - 1);
}

// aircraftAt returns every aircraft at time t, moved smoothly between frames.
function aircraftAt(t) {
  const i = frameIndex(t);
  if (i < 0) return { list: [], f: null, i };
  const f0 = F[i], f1 = F[Math.min(i + 1, F.length - 1)];
  const u = f1 === f0 ? 0 : Math.min(1, (t - f0.t) / DT);
  const m1 = fmap(f1);
  const list = [];
  for (const r of f0.a || []) {
    const q = m1.get(r[C.id]);
    let x, y, alt;
    if (q && u > 0) {
      x = r[C.x] + (q[C.x] - r[C.x]) * u;
      y = r[C.y] + (q[C.y] - r[C.y]) * u;
      alt = r[C.alt] + (q[C.alt] - r[C.alt]) * u;
    } else {
      const h = (t - f0.t) / 3600;
      x = r[C.x] + r[C.vx] * h; y = r[C.y] + r[C.vy] * h; alt = r[C.alt];
    }
    list.push({ r, id: r[C.id], x, y, alt });
  }
  return { list, f: f0, i };
}

// ahead returns the part of a path from the point nearest (x, y) to its end.
function ahead(path, x, y) {
  const p = path.p;
  let best = Infinity, bi = 0, bx = x, by = y;
  for (let i = 0; i + 1 < p.length; i++) {
    const ax = p[i][0], ay = p[i][1], dx = p[i + 1][0] - ax, dy = p[i + 1][1] - ay;
    const l2 = dx * dx + dy * dy;
    let u = l2 ? ((x - ax) * dx + (y - ay) * dy) / l2 : 0;
    u = Math.max(0, Math.min(1, u));
    const qx = ax + u * dx, qy = ay + u * dy;
    const d = (x - qx) * (x - qx) + (y - qy) * (y - qy);
    if (d < best - 1e-9) { best = d; bi = i; bx = qx; by = qy; }
  }
  return [[bx, by], ...p.slice(bi + 1)];
}

function pointAlong(poly, dist) {
  for (let i = 0; i + 1 < poly.length; i++) {
    const dx = poly[i + 1][0] - poly[i][0], dy = poly[i + 1][1] - poly[i][1];
    const l = Math.hypot(dx, dy);
    if (dist <= l || i + 2 === poly.length) {
      const u = l ? Math.min(1, dist / l) : 0;
      return [poly[i][0] + dx * u, poly[i][1] + dy * u, dist > l];
    }
    dist -= l;
  }
  return [poly[0][0], poly[0][1], true];
}

// amendment says in plain words what a committed proposal changes.
function amendment(e) {
  if (e.src === 'recontract') return `a new contract at ${fl(e.alt)}`;
  const i = frameIndex(e.t);
  const r = i >= 0 ? fmap(F[i]).get(e.a) : null;
  if (r && Math.abs(e.alt - r[C.alt]) > 500) return `a level change to ${fl(e.alt)}`;
  return e.p && e.p.length > 2 ? 'a detour' : 'a speed change';
}

function describe(e) {
  const a = `Flight ${e.a}`;
  switch (e.k) {
    case 'propose': return `${SOURCE[e.src] || e.src}: kernel commits ${amendment(e)} for flight ${e.a}`;
    case 'fault': return `${a} ${e.txt}`;
    case 'intruder': return `Monitor: flight ${e.a} ${e.txt}. Now an intruder.`;
    case 'recontracted': return `${a} is back under contract`;
    case 'settled': return `${a} flies the ${e.txt} route. The track settled it.`;
    case 'fail': return `No answer from flight ${e.a}. Both routes stay protected.`;
    case 'refuse': return `${a} refuses the clearance`;
    case 'alert':
      return e.txt === 'verified'
        ? `Safety Net false alert: ${e.a} and ${e.b}. The kernel holds this pair as separated.`
        : `Safety Net alert: ${e.a} and ${e.b}, loss predicted in ${Math.round(e.num || 0)} s`;
    case 'instruct': return `Safety Net instructs flight ${e.a} to ${fl(e.alt)}${e.txt ? ' on a false alert' : ''}`;
    case 'loss': return `Loss of separation: ${e.a} and ${e.b}, ${(e.num || 0).toFixed(1)} nm and ${Math.round(e.alt || 0)} ft (${e.txt})`;
    case 'alarm': return `Alarm: the two Verifiers disagree on ${e.a} and ${e.b}. More follow.`;
    default: return `${e.k} ${e.a || ''}`;
  }
}

const MINOR = new Set(['propose', 'recontracted', 'settled', 'refuse']);

// ---------- map ----------

function resetView() {
  const r = $('map').getBoundingClientRect();
  S.view = { x: 0, y: 0, k: Math.min(r.width, r.height) / (2 * R * 1.14) };
  S.dirty = true;
}

function drawMap(now) {
  const { ctx, w, h } = fit($('map'));
  const col = S.col, v = S.view;
  const X = (x) => w / 2 + (x - v.x) * v.k;
  const Y = (y) => h / 2 - (y - v.y) * v.k;
  const font = (px, wt = 500) => { ctx.font = `${wt} ${px}px ${S.font}`; };
  // text draws a label with a thin halo in the map colour, so that it stays
  // readable on top of lines and other labels.
  const text = (str, x, y, fill) => {
    ctx.strokeStyle = alpha(col.map, 0.85); ctx.lineWidth = 3; ctx.setLineDash([]);
    ctx.strokeText(str, x, y);
    ctx.fillStyle = fill;
    ctx.fillText(str, x, y);
  };
  ctx.clearRect(0, 0, w, h);
  ctx.lineJoin = 'round'; ctx.lineCap = 'round';

  // Range rings and the cell boundary.
  ctx.setLineDash([]);
  for (const rr of [50, 100]) {
    ctx.strokeStyle = col.line; ctx.lineWidth = 1;
    ctx.beginPath(); ctx.arc(X(0), Y(0), rr * v.k, 0, 7); ctx.stroke();
  }
  ctx.strokeStyle = col['ink-3']; ctx.lineWidth = 1.5;
  ctx.beginPath(); ctx.arc(X(0), Y(0), R * v.k, 0, 7); ctx.stroke();
  font(11); ctx.fillStyle = col['ink-3']; ctx.textAlign = 'left'; ctx.textBaseline = 'middle';
  for (const rr of [50, 100, R]) ctx.fillText(`${rr} nm`, X(0) + 4, Y(-rr) - 8);
  ctx.textAlign = 'center';
  ctx.fillText('N', X(0), Y(R) - 10);

  S.screen = [];
  if (!S.rec) return;
  const t = S.t;
  const { list, f } = aircraftAt(t);
  const byId = new Map(list.map((a) => [a.id, a]));
  const paths = S.rec.paths;
  const fade = Math.max(15, S.speed);
  const stroke = (pts, close) => {
    ctx.beginPath();
    pts.forEach((p, i) => (i ? ctx.lineTo(X(p[0]), Y(p[1])) : ctx.moveTo(X(p[0]), Y(p[1]))));
    if (close) ctx.closePath();
    ctx.stroke();
  };

  // Every cleared route, if asked for.
  if (S.layers.routes) {
    ctx.strokeStyle = alpha(col['ink-3'], 0.55); ctx.lineWidth = 1;
    for (const a of list) {
      const pi = a.r[C.path];
      if (pi >= 0 && a.r[C.state] !== 3) stroke(ahead(paths[pi], a.x, a.y));
    }
  }

  // Intruder cones: a straight line at the present velocity, widening fast.
  for (const a of list) {
    if (a.r[C.state] !== 3) continue;
    const vx = a.r[C.vx] / 3600, vy = a.r[C.vy] / 3600, sp = Math.hypot(vx, vy);
    if (!sp) continue;
    const nx = -vy / sp, ny = vx / sp, left = [], right = [];
    const H = META.intruderHorizon;
    for (let tau = 0; tau <= H; tau += 30) {
      const rad = a.r[C.radius] + META.intruderK * tau;
      const cx = a.x + vx * tau, cy = a.y + vy * tau;
      left.push([cx + nx * rad, cy + ny * rad]);
      right.push([cx - nx * rad, cy - ny * rad]);
    }
    const end = [a.x + vx * H, a.y + vy * H], endR = a.r[C.radius] + META.intruderK * H;
    ctx.fillStyle = alpha(col.orange, 0.13);
    ctx.beginPath();
    left.concat(right.reverse()).forEach((p, i) => (i ? ctx.lineTo(X(p[0]), Y(p[1])) : ctx.moveTo(X(p[0]), Y(p[1]))));
    ctx.closePath();
    ctx.moveTo(X(end[0]) + endR * v.k, Y(end[1]));
    ctx.arc(X(end[0]), Y(end[1]), endR * v.k, 0, 7);
    ctx.fill();
    ctx.strokeStyle = alpha(col.orange, 0.7); ctx.lineWidth = 1; ctx.setLineDash([]);
    ctx.beginPath(); ctx.arc(X(end[0]), Y(end[1]), endR * v.k, 0, 7); ctx.stroke();
  }

  // The selected flight: its route ahead and the envelope the kernel protects.
  const sel = S.sel != null ? byId.get(S.sel) : null;
  if (sel && sel.r[C.path] >= 0 && sel.r[C.state] !== 3) {
    const route = ahead(paths[sel.r[C.path]], sel.x, sel.y);
    const sp = Math.hypot(sel.r[C.vx], sel.r[C.vy]) / 3600;
    ctx.fillStyle = alpha(col.ink, 0.07);
    ctx.beginPath();
    const marks = [];
    for (let tau = 0; tau <= META.horizon; tau += 30) {
      const [px, py, past] = pointAlong(route, sp * tau);
      if (past) break;
      const rad = (sel.r[C.radius] + META.k * tau) * v.k;
      ctx.moveTo(X(px) + rad, Y(py));
      ctx.arc(X(px), Y(py), rad, 0, 7);
      if (tau && tau % 300 === 0) marks.push([px, py, rad, tau]);
    }
    ctx.fill();
    ctx.strokeStyle = col.ink; ctx.lineWidth = 1.6; ctx.setLineDash([]);
    stroke(route);
    font(11); ctx.textAlign = 'center'; ctx.textBaseline = 'bottom';
    for (const [px, py, rad, tau] of marks) {
      ctx.strokeStyle = alpha(col['ink-2'], 0.8); ctx.lineWidth = 1; ctx.setLineDash([3, 3]);
      ctx.beginPath(); ctx.arc(X(px), Y(py), rad, 0, 7); ctx.stroke();
      ctx.fillStyle = col['ink-2'];
      ctx.fillText(`+${tau / 60} min`, X(px), Y(py) - rad - 2);
    }
    ctx.setLineDash([]);
    for (const s of paths[sel.r[C.path]].s || []) {
      ctx.fillStyle = col['ink-2']; ctx.textAlign = 'left'; ctx.textBaseline = 'middle';
      ctx.beginPath(); ctx.arc(X(s[0]), Y(s[1]), 2.5, 0, 7); ctx.fill();
      ctx.fillText(`${s[3] > s[2] ? '↑' : '↓'} ${fl(s[3])}`, X(s[0]) + 6, Y(s[1]) + 10);
    }
  }

  // Predicted conflicts: where the two flights would meet, and when.
  if (S.layers.conflicts && f && f.c) {
    font(11); ctx.textAlign = 'left'; ctx.textBaseline = 'middle';
    for (const c of f.c) {
      const a = byId.get(c[0]), b = byId.get(c[1]);
      if (!a || !b) continue;
      const ttc = c[2] - t, px = X(c[4]), py = Y(c[5]);
      const hue = c[3] ? col.orange : col.ink;
      if (ttc > 600) { ctx.setLineDash([2, 5]); ctx.lineWidth = 1; ctx.strokeStyle = alpha(hue, 0.5); }
      else if (ttc > 300) { ctx.setLineDash([6, 4]); ctx.lineWidth = 1.3; ctx.strokeStyle = alpha(hue, 0.75); }
      else { ctx.setLineDash([]); ctx.lineWidth = 2; ctx.strokeStyle = hue; }
      ctx.beginPath();
      ctx.moveTo(X(a.x), Y(a.y)); ctx.lineTo(px, py); ctx.lineTo(X(b.x), Y(b.y));
      ctx.stroke();
      ctx.setLineDash([]); ctx.lineWidth = 1.5;
      ctx.beginPath(); ctx.moveTo(px - 4, py - 4); ctx.lineTo(px + 4, py + 4); ctx.moveTo(px + 4, py - 4); ctx.lineTo(px - 4, py + 4); ctx.stroke();
      text(ttc >= 120 ? `in ${Math.round(ttc / 60)} min` : `in ${Math.max(0, Math.round(ttc))} s`, px + 8, py, col['ink-2']);
    }
    ctx.setLineDash([]);
  }

  // Protected envelopes.
  if (S.layers.envelopes) {
    ctx.strokeStyle = alpha(col['ink-3'], 0.8); ctx.lineWidth = 1;
    for (const a of list) {
      if (a.r[C.state] === 3 || !(a.r[C.flags] & FL_INSIDE)) continue;
      ctx.beginPath(); ctx.arc(X(a.x), Y(a.y), a.r[C.radius] * v.k, 0, 7); ctx.stroke();
    }
  }

  // Amendments in flight: the old route and, dashed, the new one.
  for (const a of list) {
    const pp = a.r[C.pending];
    if (pp < 0) continue;
    if (a.r[C.path] >= 0 && a.r[C.state] !== 3) {
      ctx.strokeStyle = alpha(col['ink-3'], 0.8); ctx.lineWidth = 1; ctx.setLineDash([]);
      stroke(ahead(paths[a.r[C.path]], a.x, a.y));
    }
    ctx.strokeStyle = col.blue; ctx.lineWidth = 1.8; ctx.setLineDash([7, 5]);
    stroke(a.r[C.state] === 3 ? paths[pp].p : ahead(paths[pp], a.x, a.y));
    ctx.setLineDash([]);
  }

  // Proposals, at the moment the kernel decides on them.
  const p0 = lowerBound(PROPS, t - fade);
  let shownRejected = 0;
  for (let i = p0; i < PROPS.length && PROPS[i].t <= t; i++) if (!PROPS[i].ok) shownRejected++;
  font(11); ctx.textBaseline = 'middle'; ctx.textAlign = 'left';
  for (let i = p0; i < PROPS.length && PROPS[i].t <= t; i++) {
    const e = PROPS[i], life = 1 - (t - e.t) / fade;
    if (e.ok) {
      ctx.strokeStyle = alpha(col.blue, 0.15 + 0.7 * life); ctx.lineWidth = 2.2; ctx.setLineDash([]);
      stroke(e.p);
    } else if (S.layers.rejected) {
      ctx.strokeStyle = alpha(col['ink-2'], 0.9 * life); ctx.lineWidth = 1.2; ctx.setLineDash([4, 4]);
      stroke(e.p);
      const end = e.p[e.p.length > 2 ? 1 : e.p.length - 1], ex = X(end[0]), ey = Y(end[1]);
      ctx.setLineDash([]); ctx.strokeStyle = alpha(col.ink, life); ctx.lineWidth = 1.6;
      ctx.beginPath(); ctx.moveTo(ex - 4, ey - 4); ctx.lineTo(ex + 4, ey + 4); ctx.moveTo(ex + 4, ey - 4); ctx.lineTo(ex - 4, ey + 4); ctx.stroke();
      if (shownRejected <= 8) text(REASON_SHORT[e.why] || e.why, ex + 8, ey, alpha(col.ink, life));
    }
  }
  ctx.setLineDash([]);

  // Safety Net alerts.
  if (f && f.l) {
    font(11, 600); ctx.textAlign = 'center'; ctx.textBaseline = 'bottom';
    for (const l of f.l) {
      const a = byId.get(l[0]), b = byId.get(l[1]);
      if (!a || !b) continue;
      ctx.strokeStyle = col.aqua; ctx.lineWidth = 2;
      ctx.beginPath(); ctx.moveTo(X(a.x), Y(a.y)); ctx.lineTo(X(b.x), Y(b.y)); ctx.stroke();
      for (const q of [a, b]) { ctx.beginPath(); ctx.arc(X(q.x), Y(q.y), 13, 0, 7); ctx.stroke(); }
      text(`Safety Net: loss in ${Math.round(l[2])} s`, (X(a.x) + X(b.x)) / 2, (Y(a.y) + Y(b.y)) / 2 - 16, col.ink);
    }
  }

  // Aircraft.
  const dense = list.length > 110;
  const i0 = frameIndex(t);
  for (const a of list) {
    const sx = X(a.x), sy = Y(a.y);
    if (sx < -40 || sy < -40 || sx > w + 40 || sy > h + 40) continue;
    const r = a.r, st = r[C.state], flags = r[C.flags], inside = flags & FL_INSIDE;
    const hue = st === 3 ? col.orange : st === 0 ? col.ink : col.blue;
    ctx.globalAlpha = inside ? 1 : 0.42;

    // A short trail: where the aircraft was 10, 20, ... 50 seconds ago.
    ctx.fillStyle = alpha(hue, 0.5);
    for (let j = 1; j <= 5; j++) {
      const fi = i0 - 2 * j;
      if (fi < 0) break;
      const q = fmap(F[fi]).get(a.id);
      if (!q) break;
      ctx.beginPath(); ctx.arc(X(q[C.x]), Y(q[C.y]), 1.2, 0, 7); ctx.fill();
    }

    const hdg = Math.atan2(r[C.vy], r[C.vx]);
    ctx.save();
    ctx.translate(sx, sy);
    if (st === 3) {
      ctx.fillStyle = hue;
      ctx.beginPath(); ctx.moveTo(0, -6.5); ctx.lineTo(6.5, 0); ctx.lineTo(0, 6.5); ctx.lineTo(-6.5, 0); ctx.closePath(); ctx.fill();
    } else {
      ctx.rotate(-hdg);
      ctx.beginPath(); ctx.moveTo(7, 0); ctx.lineTo(-5, 4.5); ctx.lineTo(-2, 0); ctx.lineTo(-5, -4.5); ctx.closePath();
      if (st === 2) { ctx.strokeStyle = hue; ctx.lineWidth = 1.6; ctx.stroke(); }
      else { ctx.fillStyle = hue; ctx.fill(); }
    }
    ctx.restore();

    if (flags & FL_INSTRUCTED) {
      ctx.strokeStyle = col.aqua; ctx.lineWidth = 2;
      ctx.beginPath(); ctx.arc(sx, sy, 10, 0, 7); ctx.stroke();
    }
    if (S.layers.truth && st !== 3 && (flags & (FL_OFF_ROUTE | FL_OFF_LEVEL))) {
      // The crew has left its clearance and the Monitor has not reported it yet.
      ctx.fillStyle = col.orange;
      ctx.beginPath(); ctx.arc(sx, sy - 11, 3, 0, 7); ctx.fill();
    }
    if (a.id === S.sel || a.id === S.hover) {
      ctx.strokeStyle = col.ink; ctx.lineWidth = 1.4;
      ctx.beginPath(); ctx.arc(sx, sy, 15, 0, 7); ctx.stroke();
    }

    const labelled = a.id === S.sel || a.id === S.hover || (S.layers.labels && (!dense || st !== 0));
    if (labelled) {
      font(11, 600); ctx.textAlign = 'left'; ctx.textBaseline = 'alphabetic';
      text(String(a.id) + (st === 2 ? ' ?' : ''), sx + 11, sy - 4, col.ink);
      font(11);
      const d = r[C.sel] - a.alt;
      const lvl = String(Math.round(a.alt / 100)).padStart(3, '0');
      text(Math.abs(d) > 150 ? `${lvl} ${d > 0 ? '↑' : '↓'} ${String(Math.round(r[C.sel] / 100)).padStart(3, '0')}` : lvl, sx + 11, sy + 8, col['ink-2']);
    }
    ctx.globalAlpha = 1;
    S.screen.push({ id: a.id, sx, sy, a });
  }

  // Safety Net instructions, for a short time after they are issued.
  font(11, 600); ctx.textAlign = 'right'; ctx.textBaseline = 'middle';
  for (let i = lowerBound(INSTR, t - 2 * fade); i < INSTR.length && INSTR[i].t <= t; i++) {
    const a = byId.get(INSTR[i].a);
    if (!a) continue;
    text(`Net: ${INSTR[i].alt > a.alt ? '↑' : '↓'} ${fl(INSTR[i].alt)}`, X(a.x) - 15, Y(a.y), col.ink);
  }

  // Losses of separation stay on the map for fifteen minutes.
  for (const e of LOSSES) {
    if (e.t > t || t - e.t > 900) continue;
    if (!e._at) {
      const m = fmap(F[Math.max(0, frameIndex(e.t))]);
      const a = m.get(e.a), b = m.get(e.b);
      e._at = a && b ? [(a[C.x] + b[C.x]) / 2, (a[C.y] + b[C.y]) / 2] : [0, 0];
    }
    const ex = X(e._at[0]), ey = Y(e._at[1]);
    ctx.fillStyle = col.orange;
    ctx.beginPath(); ctx.arc(ex, ey, 10, 0, 7); ctx.fill();
    ctx.strokeStyle = col.map; ctx.lineWidth = 2.2;
    ctx.beginPath(); ctx.moveTo(ex - 4, ey - 4); ctx.lineTo(ex + 4, ey + 4); ctx.moveTo(ex + 4, ey - 4); ctx.lineTo(ex - 4, ey + 4); ctx.stroke();
    font(12, 600); ctx.textAlign = 'left'; ctx.textBaseline = 'middle';
    text(`Loss of separation, ${e.txt.split(',')[0]} class`, ex + 15, ey, col.ink);
  }
  void now;
}

// ---------- side view ----------

function drawSide() {
  const { ctx, w, h } = fit($('side'));
  const col = S.col, v = S.view;
  const X = (x) => w / 2 + (x - v.x) * v.k;
  const top = 16, bottom = h - 8;
  const Y = (alt) => bottom - ((alt - ALT_LO) / (ALT_HI - ALT_LO)) * (bottom - top);
  ctx.clearRect(0, 0, w, h);
  ctx.font = `500 11px ${S.font}`; ctx.textBaseline = 'middle';
  for (let alt = 30000; alt <= 40000; alt += 1000) {
    ctx.strokeStyle = alt % 2000 ? alpha(col.line, 0.5) : col.line; ctx.lineWidth = 1;
    ctx.beginPath(); ctx.moveTo(0, Y(alt)); ctx.lineTo(w, Y(alt)); ctx.stroke();
    if (alt % 2000 === 0) { ctx.fillStyle = col['ink-3']; ctx.textAlign = 'left'; ctx.fillText(String(alt / 100), 6, Y(alt) - 6); }
  }
  ctx.fillStyle = col['ink-2']; ctx.textAlign = 'right'; ctx.textBaseline = 'top';
  ctx.fillText('Side view from the south: flight level against position', w - 8, 3);
  if (!S.rec) return;
  const { list, f } = aircraftAt(S.t);
  const alerting = new Set();
  if (f && f.l) for (const l of f.l) { alerting.add(l[0]); alerting.add(l[1]); }
  for (const a of list) {
    const r = a.r, st = r[C.state], sx = X(a.x), sy = Y(a.alt);
    if (sx < -10 || sx > w + 10) continue;
    const hue = st === 3 ? col.orange : st === 0 ? col.ink : col.blue;
    ctx.globalAlpha = r[C.flags] & FL_INSIDE ? 1 : 0.42;
    const d = r[C.sel] - a.alt;
    if (Math.abs(d) > 150) {
      ctx.strokeStyle = alpha(hue, 0.7); ctx.lineWidth = 1.2;
      ctx.beginPath(); ctx.moveTo(sx, sy); ctx.lineTo(sx, Y(r[C.sel])); ctx.stroke();
    }
    ctx.fillStyle = hue;
    ctx.beginPath();
    if (st === 3) { ctx.moveTo(sx, sy - 5); ctx.lineTo(sx + 5, sy); ctx.lineTo(sx, sy + 5); ctx.lineTo(sx - 5, sy); ctx.closePath(); }
    else ctx.arc(sx, sy, 3.2, 0, 7);
    if (st === 2) { ctx.strokeStyle = hue; ctx.lineWidth = 1.5; ctx.stroke(); } else ctx.fill();
    if (alerting.has(a.id) || (r[C.flags] & FL_INSTRUCTED)) {
      ctx.strokeStyle = col.aqua; ctx.lineWidth = 2; ctx.beginPath(); ctx.arc(sx, sy, 7, 0, 7); ctx.stroke();
    }
    if (a.id === S.sel || a.id === S.hover) {
      ctx.strokeStyle = col.ink; ctx.lineWidth = 1.4; ctx.beginPath(); ctx.arc(sx, sy, 10, 0, 7); ctx.stroke();
    }
    ctx.globalAlpha = 1;
  }
}

// ---------- timeline ----------

const TL = { pad: 16, top: 30, base: 50 };

function buildBuckets(w) {
  const n = Math.max(20, Math.floor((w - 2 * TL.pad) / 6));
  const ok = new Array(n).fill(0), no = new Array(n).fill(0);
  for (const e of PROPS) {
    const b = Math.min(n - 1, Math.floor((e.t / DUR) * n));
    (e.ok ? ok : no)[b]++;
  }
  let peak = 1;
  for (let i = 0; i < n; i++) peak = Math.max(peak, ok[i] + no[i]);
  buckets = { n, ok, no, peak, w };
}

function drawTimeline() {
  const { ctx, w, h } = fit($('timeline'));
  const col = S.col;
  ctx.clearRect(0, 0, w, h);
  if (!S.rec) return;
  if (!buckets || buckets.w !== w) buildBuckets(w);
  const span = w - 2 * TL.pad, X = (t) => TL.pad + (t / DUR) * span;
  const bw = span / buckets.n, room = TL.base - TL.top;

  // Proposals per interval: rejected from the baseline, committed above.
  for (let i = 0; i < buckets.n; i++) {
    const x = TL.pad + i * bw + 1, bar = Math.max(2, bw - 2);
    const hNo = (buckets.no[i] / buckets.peak) * room, hOk = (buckets.ok[i] / buckets.peak) * room;
    if (buckets.no[i]) { ctx.fillStyle = col['ink-3']; ctx.beginPath(); ctx.roundRect(x, TL.base - hNo, bar, hNo, [2, 2, 0, 0]); ctx.fill(); }
    if (buckets.ok[i]) {
      const y = TL.base - hNo - (buckets.no[i] ? 2 : 0) - Math.max(2, hOk);
      ctx.fillStyle = col.blue; ctx.beginPath(); ctx.roundRect(x, y, bar, Math.max(2, hOk), 2); ctx.fill();
    }
  }
  ctx.strokeStyle = col.line; ctx.lineWidth = 1;
  ctx.beginPath(); ctx.moveTo(TL.pad, TL.base + 0.5); ctx.lineTo(w - TL.pad, TL.base + 0.5); ctx.stroke();

  // Time axis.
  ctx.font = `500 11px ${S.font}`; ctx.fillStyle = col['ink-3']; ctx.textAlign = 'center'; ctx.textBaseline = 'top';
  const stepMin = DUR > 3 * 3600 ? 30 : DUR > 5400 ? 15 : 10;
  for (let m = 0; m * 60 <= DUR; m += stepMin) ctx.fillText(clock(m * 60).slice(0, 5), X(m * 60), TL.base + 3);

  // Events: Safety Net and losses on the first row, faults and intruders on the second.
  for (const e of MARKS) {
    const x = X(e.t);
    if (e.k === 'alarm') {
      ctx.strokeStyle = alpha(col.ink, 0.5); ctx.lineWidth = 1;
      ctx.beginPath(); ctx.moveTo(x, 3); ctx.lineTo(x, 8); ctx.stroke();
    } else if (e.k === 'alert') {
      ctx.strokeStyle = col.aqua; ctx.lineWidth = 2; ctx.beginPath(); ctx.arc(x, 11, 4, 0, 7); ctx.stroke();
    } else if (e.k === 'instruct') {
      ctx.fillStyle = col.aqua; ctx.beginPath(); ctx.arc(x, 11, 4.5, 0, 7); ctx.fill();
    } else if (e.k === 'fault') {
      ctx.strokeStyle = col.orange; ctx.lineWidth = 1.6;
      ctx.beginPath(); ctx.moveTo(x, 18); ctx.lineTo(x + 4.5, 26); ctx.lineTo(x - 4.5, 26); ctx.closePath(); ctx.stroke();
    } else if (e.k === 'intruder') {
      ctx.fillStyle = col.orange;
      ctx.beginPath(); ctx.moveTo(x, 17); ctx.lineTo(x + 5, 22); ctx.lineTo(x, 27); ctx.lineTo(x - 5, 22); ctx.closePath(); ctx.fill();
    }
  }
  for (const e of LOSSES) {
    const x = X(e.t);
    ctx.fillStyle = col.orange; ctx.beginPath(); ctx.arc(x, 11, 7, 0, 7); ctx.fill();
    ctx.strokeStyle = col.panel; ctx.lineWidth = 1.8;
    ctx.beginPath(); ctx.moveTo(x - 3, 8); ctx.lineTo(x + 3, 14); ctx.moveTo(x + 3, 8); ctx.lineTo(x - 3, 14); ctx.stroke();
  }

  // Playhead.
  const px = X(S.t);
  ctx.strokeStyle = col.ink; ctx.lineWidth = 1.5;
  ctx.beginPath(); ctx.moveTo(px, 0); ctx.lineTo(px, TL.base + 2); ctx.stroke();
  ctx.fillStyle = col.ink; ctx.beginPath(); ctx.arc(px, TL.base + 2, 3.5, 0, 7); ctx.fill();
}

// ---------- panels ----------

function buildReasons() {
  const host = $('reasons');
  host.textContent = '';
  for (const name of META.counters) {
    if (!name.startsWith('rej:')) continue;
    const key = name.slice(4);
    const row = document.createElement('div');
    row.hidden = true;
    row.dataset.counter = name;
    const dt = document.createElement('dt'); dt.textContent = REASON[key] || key;
    const dd = document.createElement('dd'); dd.textContent = '0';
    row.append(dt, dd);
    host.append(row);
  }
}

function set(id, text) {
  const el = $(id);
  if (el.textContent !== text) el.textContent = text;
}

function updatePanels() {
  set('clock', clock(S.t));
  if (!S.rec) return;
  const { list, f } = aircraftAt(S.t);
  let contract = 0, pending = 0, intruder = 0;
  for (const a of list) {
    if (!(a.r[C.flags] & FL_INSIDE)) continue;
    const st = a.r[C.state];
    if (st === 0) contract++; else if (st === 3) intruder++; else pending++;
  }
  set('clockSub', `${contract + pending + intruder} aircraft in the cell, ${list.length - contract - pending - intruder} on approach or leaving`);
  set('nContract', num(contract)); set('nPending', num(pending)); set('nIntruder', num(intruder));
  const n = f ? f.n : null, c = (name) => (n ? n[N[name]] : 0);
  const conflicts = f && f.c ? f.c.length : 0;
  let soonest = Infinity;
  if (f && f.c) for (const x of f.c) soonest = Math.min(soonest, x[2] - S.t);
  set('nConflicts', conflicts ? `${conflicts}, soonest in ${Math.max(0, Math.round(soonest / 60))} min` : '0');
  set('coast', f ? (f.k < 0 ? `more than ${Math.round(META.horizon / 60) - 1} min` : `${(f.k / 60).toFixed(1)} min`) : '–');

  const commits = c('commits'), proposals = c('proposals'), rejected = proposals - commits;
  set('nCommit', num(commits)); set('nReject', num(rejected));
  $('verA').style.flex = String(commits || 0.0001);
  $('verB').style.flex = String(rejected || 0.0001);
  $('verSplit').style.visibility = proposals ? 'visible' : 'hidden';
  for (const row of $('reasons').children) {
    const val = c(row.dataset.counter);
    row.hidden = !val;
    const dd = row.lastChild, text = num(val);
    if (dd.textContent !== text) dd.textContent = text;
  }
  const hard = c('hard');
  $('alarmRow').hidden = !hard;
  set('nHard', num(hard));

  set('nAlertI', num(c('alertsIntruder'))); set('nAlertU', num(c('alertsUnresolved')));
  set('nAlertV', num(c('alertsVerified'))); set('nInstr', num(c('instructions')));
  set('nLossK', num(c('lossKernel'))); set('nLossU', num(c('lossUnresolved')));
  set('nLossI', num(c('lossIntruder'))); set('nLossL', num(c('lossLatency')));
  $('lossKRow').classList.toggle('flag', c('lossKernel') > 0);

  updateStrip(list);
  updateLog();
}

function updateStrip(list) {
  const a = S.sel != null ? list.find((q) => q.id === S.sel) : null;
  $('stripSection').hidden = !a;
  if (!a) return;
  const r = a.r, flags = r[C.flags], d = r[C.sel] - a.alt;
  let acks = 0;
  for (const e of ACKS) { if (e.t > S.t) break; if (e.a === a.id) acks++; }
  const truth = flags & FL_OFF_ROUTE ? 'Flying straight, off its route'
    : flags & FL_OFF_LEVEL ? 'Leaving its cleared level'
    : flags & FL_NO_RADIO ? 'Conforming, with no radio' : 'Follows its clearance';
  const rows = [
    ['Flight', String(a.id), 'big'],
    ['Level', fl(a.alt) + (Math.abs(d) > 150 ? ` ${d > 0 ? '↑' : '↓'} ${fl(r[C.sel])}` : '')],
    ['Ground speed', `${Math.round(Math.hypot(r[C.vx], r[C.vy]))} kt`],
    ['Kernel state', STATE_NAME[r[C.state]]],
    ['Envelope radius', `${r[C.radius].toFixed(1)} nm`],
    ['Clearances accepted', String(acks)],
    ['In truth', truth],
  ];
  const key = JSON.stringify(rows);
  const host = $('strip');
  if (host.dataset.key === key) return;
  host.dataset.key = key;
  host.textContent = '';
  for (const [k, val, cls] of rows) {
    const kd = document.createElement('div'); kd.className = 'k'; kd.textContent = k;
    const vd = document.createElement('div'); vd.className = 'v' + (cls ? ' ' + cls : ''); vd.textContent = val;
    host.append(kd, vd);
  }
}

function updateLog() {
  const end = lowerBound(NOTABLE, S.t + 1e-6);
  const start = Math.max(0, end - 40);
  const key = `${start}:${end}`;
  if (key === S.logKey) return;
  S.logKey = key;
  const host = $('log');
  host.textContent = '';
  if (end === 0) {
    const li = document.createElement('li'); li.className = 'empty'; li.textContent = 'Nothing yet. Press Play.';
    host.append(li);
    return;
  }
  for (let i = end - 1; i >= start; i--) {
    const e = NOTABLE[i];
    const li = document.createElement('li'), b = document.createElement('button');
    const tm = document.createElement('time'); tm.textContent = clock(e.t).slice(0, 8);
    const sp = document.createElement('span'); sp.textContent = describe(e);
    if (MINOR.has(e.k)) sp.className = 'minor';
    b.type = 'button';
    b.append(tm, sp);
    b.addEventListener('click', () => { seek(e.t - 15); S.sel = e.a || null; });
    li.append(b);
    host.append(li);
  }
}

// ---------- transport ----------

function seek(t) {
  S.t = Math.max(0, Math.min(DUR, t));
  S.dirty = true;
}

function setPlaying(on) {
  if (on && S.t >= DUR) S.t = 0;
  S.playing = on && !!S.rec;
  $('play').textContent = S.playing ? 'Pause' : 'Play';
  S.dirty = true;
}

function jump(dir) {
  const list = MARKS.filter((e) => e.k !== 'alarm');
  if (!list.length) return;
  let e;
  if (dir > 0) e = list.find((x) => x.t > S.t + 16);
  else { for (const x of list) if (x.t < S.t + 14) e = x; else break; }
  if (!e) return;
  seek(e.t - 15);
  S.sel = e.a || S.sel;
}

let last = performance.now();
function tick(now) {
  // Advance by the time that really passed, so that playback keeps its speed
  // when the browser draws few frames. The cap covers a tab that was asleep.
  const dtw = Math.min(1, (now - last) / 1000);
  last = now;
  if (S.playing) {
    S.t += dtw * S.speed;
    if (S.t >= DUR) { S.t = DUR; setPlaying(false); }
  }
  if (S.playing || S.dirty) {
    S.dirty = false;
    drawMap(now); drawSide(); drawTimeline();
    if (!S.playing || now - S.lastPanel > 120) { S.lastPanel = now; updatePanels(); }
  }
  requestAnimationFrame(tick);
}

// ---------- scenarios ----------

function overlay(text) {
  const el = $('overlay');
  el.hidden = !text;
  el.textContent = text || '';
}

async function run(sc, seed, minutes) {
  setPlaying(false);
  S.scenario = sc;
  for (const b of document.querySelectorAll('.scenario')) b.setAttribute('aria-current', String(b.dataset.id === sc.id));
  $('seed').value = seed; $('minutes').value = minutes;
  const watch = $('watch');
  watch.textContent = '';
  const strong = document.createElement('strong'); strong.textContent = sc.name + '. ';
  watch.append(strong, sc.watch);
  overlay(`Running “${sc.name}” for ${minutes} simulated minutes…`);
  $('run').disabled = true;
  try {
    const res = await fetch(`/api/run?scenario=${encodeURIComponent(sc.id)}&seed=${seed}&minutes=${minutes}`);
    if (!res.ok) throw new Error(await res.text());
    index(await res.json());
    S.t = 0; S.sel = null; S.hover = null; S.logKey = '';
    $('speed').value = String(META.scripted ? 60 : 30);
    S.speed = Number($('speed').value);
    resetView();
    overlay('');
    // Start playing, unless the person has asked their system for less motion.
    setPlaying(!window.matchMedia('(prefers-reduced-motion: reduce)').matches);
  } catch (err) {
    overlay(`The viewer could not run this scenario (${err.message || err}). Check that the server is still running: go run ./cmd/atcviz`);
  } finally {
    $('run').disabled = false;
  }
}

function buildScenarioList() {
  const host = $('scenarios');
  host.textContent = '';
  let group = '';
  for (const sc of S.scenarios) {
    if (sc.group !== group) {
      group = sc.group;
      const h = document.createElement('h2'); h.textContent = group;
      host.append(h);
    }
    const b = document.createElement('button');
    b.type = 'button'; b.className = 'scenario'; b.dataset.id = sc.id;
    const name = document.createElement('strong'); name.textContent = sc.name;
    const blurb = document.createElement('span'); blurb.textContent = sc.blurb;
    b.append(name, blurb);
    b.addEventListener('click', () => run(sc, sc.seed, sc.minutes));
    host.append(b);
  }
}

// ---------- input ----------

function nearest(mx, my, canvasId) {
  let best = null, bd = 16 * 16;
  if (canvasId === 'map') {
    for (const p of S.screen) {
      const d = (p.sx - mx) * (p.sx - mx) + (p.sy - my) * (p.sy - my);
      if (d < bd) { bd = d; best = p; }
    }
  }
  return best;
}

function tip(html, x, y) {
  const el = $('tip');
  if (!html) { el.hidden = true; return; }
  el.innerHTML = html;
  el.hidden = false;
  const r = el.getBoundingClientRect();
  el.style.left = Math.min(window.innerWidth - r.width - 8, x + 14) + 'px';
  el.style.top = Math.max(8, y - r.height - 10) + 'px';
}

function bindMap() {
  const cv = $('map');
  let drag = null;
  cv.addEventListener('pointerdown', (e) => {
    drag = { x: e.clientX, y: e.clientY, vx: S.view.x, vy: S.view.y, moved: false };
    cv.setPointerCapture(e.pointerId);
  });
  cv.addEventListener('pointermove', (e) => {
    const r = cv.getBoundingClientRect(), mx = e.clientX - r.left, my = e.clientY - r.top;
    if (drag) {
      const dx = e.clientX - drag.x, dy = e.clientY - drag.y;
      if (Math.abs(dx) + Math.abs(dy) > 3) { drag.moved = true; cv.classList.add('dragging'); }
      if (drag.moved) { S.view.x = drag.vx - dx / S.view.k; S.view.y = drag.vy + dy / S.view.k; S.dirty = true; }
      tip(null);
      return;
    }
    const hit = nearest(mx, my, 'map');
    const id = hit ? hit.id : null;
    if (id !== S.hover) { S.hover = id; S.dirty = true; }
    if (hit) {
      const r0 = hit.a.r;
      tip(`<b>Flight ${hit.id}</b><br>${fl(hit.a.alt)}, ${Math.round(Math.hypot(r0[C.vx], r0[C.vy]))} kt<br>${STATE_NAME[r0[C.state]]}`, e.clientX, e.clientY);
    } else tip(null);
  });
  const up = (e) => {
    if (drag && !drag.moved) {
      const r = cv.getBoundingClientRect();
      const hit = nearest(e.clientX - r.left, e.clientY - r.top, 'map');
      S.sel = hit ? hit.id : null;
      S.dirty = true;
    }
    drag = null;
    cv.classList.remove('dragging');
  };
  cv.addEventListener('pointerup', up);
  cv.addEventListener('pointercancel', () => { drag = null; cv.classList.remove('dragging'); });
  cv.addEventListener('pointerleave', () => { if (S.hover != null) { S.hover = null; S.dirty = true; } tip(null); });
  cv.addEventListener('wheel', (e) => {
    e.preventDefault();
    const r = cv.getBoundingClientRect(), mx = e.clientX - r.left - r.width / 2, my = e.clientY - r.top - r.height / 2;
    const k0 = S.view.k, k1 = Math.max(0.6, Math.min(40, k0 * Math.exp(-e.deltaY * 0.0015)));
    // Keep the point under the cursor in place.
    S.view.x += mx / k0 - mx / k1;
    S.view.y -= my / k0 - my / k1;
    S.view.k = k1;
    S.dirty = true;
  }, { passive: false });
}

function bindTimeline() {
  const cv = $('timeline');
  let down = false;
  const at = (e) => {
    const r = cv.getBoundingClientRect();
    return Math.max(0, Math.min(1, (e.clientX - r.left - TL.pad) / (r.width - 2 * TL.pad))) * DUR;
  };
  cv.addEventListener('pointerdown', (e) => { if (!S.rec) return; down = true; cv.setPointerCapture(e.pointerId); seek(at(e)); });
  cv.addEventListener('pointermove', (e) => {
    if (!S.rec) return;
    const t = at(e);
    if (down) seek(t);
    let html = `<b>${clock(t)}</b>`;
    if (buckets) {
      const b = Math.min(buckets.n - 1, Math.floor((t / DUR) * buckets.n));
      html += `<br>${buckets.ok[b]} committed, ${buckets.no[b]} rejected in this interval`;
    }
    const r = cv.getBoundingClientRect(), tol = (7 / (r.width - 2 * TL.pad)) * DUR;
    const near = MARKS.find((m) => m.k !== 'alarm' && Math.abs(m.t - t) <= tol);
    if (near) html += `<br>${describe(near)}`;
    tip(html, e.clientX, e.clientY);
  });
  cv.addEventListener('pointerup', () => { down = false; });
  cv.addEventListener('pointerleave', () => { down = false; tip(null); });
}

function effectiveTheme() {
  const forced = document.documentElement.dataset.theme;
  if (forced) return forced;
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function bind() {
  bindMap();
  bindTimeline();
  $('play').addEventListener('click', () => setPlaying(!S.playing));
  $('speed').addEventListener('change', (e) => { S.speed = Number(e.target.value); S.dirty = true; });
  $('prevEvent').addEventListener('click', () => jump(-1));
  $('nextEvent').addEventListener('click', () => jump(1));
  $('resetView').addEventListener('click', resetView);
  $('runForm').addEventListener('submit', (e) => {
    e.preventDefault();
    if (!S.scenario) return;
    const seed = Math.max(1, Math.floor(Number($('seed').value) || S.scenario.seed));
    const minutes = Math.max(5, Math.min(240, Number($('minutes').value) || S.scenario.minutes));
    run(S.scenario, seed, minutes);
  });
  for (const box of document.querySelectorAll('[data-layer]')) {
    S.layers[box.dataset.layer] = box.checked;
    box.addEventListener('change', () => { S.layers[box.dataset.layer] = box.checked; S.dirty = true; });
  }
  const themeBtn = $('theme');
  const label = () => { themeBtn.textContent = effectiveTheme() === 'dark' ? 'Switch to light' : 'Switch to dark'; };
  themeBtn.addEventListener('click', () => {
    document.documentElement.dataset.theme = effectiveTheme() === 'dark' ? 'light' : 'dark';
    label(); readColors();
  });
  window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => { label(); readColors(); });
  label();
  new ResizeObserver(() => { S.dirty = true; }).observe(document.querySelector('.stage'));
  window.addEventListener('keydown', (e) => {
    if (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) return;
    if (e.key === ' ') { e.preventDefault(); setPlaying(!S.playing); }
    else if (e.key === 'ArrowRight') seek(S.t + (e.shiftKey ? 60 : 10));
    else if (e.key === 'ArrowLeft') seek(S.t - (e.shiftKey ? 60 : 10));
    else if (e.key === 'Escape') { S.sel = null; S.dirty = true; }
  });
}

async function start() {
  readColors();
  bind();
  resetView();
  requestAnimationFrame(tick);
  try {
    const res = await fetch('/api/scenarios');
    S.scenarios = await res.json();
    buildScenarioList();
    // A link can name a scenario and a moment in it:
    //   ?scenario=chaos&t=2040&sel=12&paused=1&theme=dark
    const q = new URLSearchParams(location.search);
    if (q.get('theme') === 'dark' || q.get('theme') === 'light') {
      document.documentElement.dataset.theme = q.get('theme');
      $('theme').textContent = q.get('theme') === 'dark' ? 'Switch to light' : 'Switch to dark';
      readColors();
    }
    const sc = S.scenarios.find((x) => x.id === q.get('scenario')) || S.scenarios[0];
    if (!sc) return;
    await run(sc, Number(q.get('seed')) || sc.seed, Number(q.get('minutes')) || sc.minutes);
    if (q.has('t')) seek(Number(q.get('t')));
    if (q.has('sel')) S.sel = Number(q.get('sel'));
    if (q.has('speed')) { $('speed').value = q.get('speed'); S.speed = Number($('speed').value) || S.speed; }
    if (q.get('paused') === '1') setPlaying(false);
  } catch (err) {
    overlay('The viewer could not reach its server. Start it with: go run ./cmd/atcviz');
  }
}

start();
})();
