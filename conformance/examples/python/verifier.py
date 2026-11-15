#!/usr/bin/env python3
"""An example Verifier for the Clearway conformance suite, in Python.

It shows the line protocol and nothing more. The method is a port of this
project's "sampled" implementation, so it is not an independent one. A new
Verifier should be written from docs/spec.md, not from this file.

Protocol: one case per line of JSON on standard input, one answer per line of
JSON on standard output, flushed after each line.

    python3 verifier.py < cases.jsonl

Units: micrometres, microseconds, micrometres per second. Every number in a
case is an integer, and so is every number that this program computes. A
Python integer has no size limit, so no step can overflow. One micrometre per
second for one microsecond is one picometre (pm): positions, radii, and
required distances are whole numbers of picometres.

The method is conservative. The vertical part is exact and has no division.
In the horizontal part every rounding goes to the safe side: a distance is
rounded down, and a speed is rounded up.
"""
import json
import math
import sys

M = 1_000_000  # picometres per micrometre, and microseconds per second
STEP = 1_000_000  # µs: the longest interval that is reported without proof
JOIN = 18_520  # µm: a leg may start this far from the end of the leg before it, on each axis
MAX_REQUIRED = 1 << 62  # pm: a pair that needs more distance than this is not horizontally separated


def radius(tube, t):
    """The protected radius at t (pm)."""
    return M * tube["r0"] + tube["k"] * max(0, t - tube["anchor"])


def vertical_uncertainty(tube, t):
    """The vertical uncertainty at t (pm)."""
    return M * tube["vu0"] + tube["vuRate"] * max(0, t - tube["anchor"])


def start(tube):
    return tube["legs"][0]["t0"]


def end(tube):
    return tube["legs"][-1]["t1"]


def leg_at(tube, t):
    """The leg in effect at t: the first leg that ends after t, or the last leg."""
    for leg in tube["legs"]:
        if leg["t1"] > t:
            return leg
    return tube["legs"][-1]


def position(tube, t):
    """The nominal position at t (pm). It is exact."""
    t = min(max(t, start(tube)), end(tube))
    leg = leg_at(tube, t)
    dt = t - leg["t0"]
    return M * leg["p0"][0] + leg["v"][0] * dt, M * leg["p0"][1] + leg["v"][1] * dt


def entered(tube, seg, t):
    """The aircraft, as early as it may be, has started the segment at t."""
    return tube["vmin"] * (seg["t0"] - t) <= radius(tube, t)


def not_left(tube, seg, t):
    """The aircraft, as late as it may be, has not ended the segment at t."""
    return tube["vmin"] * (t - seg["t1"]) <= radius(tube, t)


def occupied(tube, t0, t1):
    """The union of the bands of the vertical segments that are in effect at
    some whole microsecond from t0 to t1 (µm), or None if there is no such
    segment.

    A segment is in effect at t if entered and not_left both hold at t. This
    function tests the two at t0 and at t1 only, and that is exact:

    - entered holds at every time after one at which it holds. So it must
      hold at t1.
    - The radius does not grow more slowly as time passes. So the amount by
      which not_left fails is least at t0 or at t1, and not_left must hold at
      one of them.
    - If not_left holds at t0 only and entered at t1 only, both hold at the
      first time at which entered holds. One microsecond earlier entered
      fails, so that time is before the start of the segment. So the first
      time is not after the start, and not_left holds up to the end.

    With vmin = 0 there is no allowance: the segment is in effect from its
    start to its end.
    """
    lo = hi = None
    for seg in tube["vsegs"]:
        if tube["vmin"] == 0:
            inside = seg["t0"] <= t1 and t0 <= seg["t1"]
        else:
            inside = entered(tube, seg, t1) and (not_left(tube, seg, t0) or not_left(tube, seg, t1))
        if not inside:
            continue
        seg_lo, seg_hi = min(seg["a0"], seg["a1"]), max(seg["a0"], seg["a1"])
        lo = seg_lo if lo is None else min(lo, seg_lo)
        hi = seg_hi if hi is None else max(hi, seg_hi)
    return None if lo is None else (lo, hi)


def speed_up(v):
    """The length of a velocity, rounded up (µm/s)."""
    n = v[0] * v[0] + v[1] * v[1]
    r = math.isqrt(n)
    return r + 1 if r * r < n else r


def speed_over(tube, t0, t1):
    """The highest speed of the legs that overlap t0 to t1, rounded up (µm/s),
    and the number of those legs."""
    best, legs = 0, 0
    for leg in tube["legs"]:
        if leg["t1"] < t0 or leg["t0"] > t1:
            continue
        best = max(best, speed_up(leg["v"]))
        legs += 1
    if legs == 0:
        return speed_up(leg_at(tube, t0)["v"]), 1
    return best, legs


def first_unproved(a, b, lo, hi, sep):
    """The start of the first interval that cannot be shown separated, or None.

    The pair is separated at every time from lo to hi if its vertical extents
    are far enough apart for the whole interval, or if the distance at the
    middle, less the most that the pair can close on the way to either end,
    is at least the required distance at hi. Otherwise halve the interval. An
    interval of at most STEP that cannot be shown separated is reported.

    STEP must not be longer than the time tolerance of the specification.
    Section 5.2 of the specification says what the tolerance then covers.
    """
    # Vertical: every segment that is in effect at a time from lo to hi, or
    # within the slack of one. The uncertainty at hi is the largest of the
    # interval.
    slack = sep.get("vertSlack", 0)
    band_a = occupied(a, lo - slack, hi + slack)
    band_b = occupied(b, lo - slack, hi + slack)
    if band_a is not None and band_b is not None:
        u = vertical_uncertainty(a, hi) + vertical_uncertainty(b, hi)
        for gap in (band_a[0] - band_b[1] - sep["svTrue"], band_b[0] - band_a[1] - sep["svTrue"]):
            if gap >= 0 and M * gap >= u:
                return None
    # Horizontal. The distance is rounded down, and the speeds are rounded up.
    # A leg may start up to JOIN from the end of the leg before it on each
    # axis: at most 2 * JOIN in distance for each change of leg.
    mid = lo + (hi - lo) // 2
    ax, ay = position(a, mid)
    bx, by = position(b, mid)
    distance = math.isqrt((ax - bx) ** 2 + (ay - by) ** 2)
    speed_a, legs_a = speed_over(a, lo, hi)
    speed_b, legs_b = speed_over(b, lo, hi)
    closure = (speed_a + speed_b) * max(mid - lo, hi - mid) + (legs_a + legs_b - 2) * 2 * JOIN * M
    required = M * sep["sh"] + radius(a, hi) + radius(b, hi)
    if required <= MAX_REQUIRED and distance - closure >= required:
        return None
    if hi - lo <= STEP:
        return lo
    left = first_unproved(a, b, lo, mid, sep)
    return left if left is not None else first_unproved(a, b, mid + 1, hi, sep)


def check(case):
    a, b = case["a"], case["b"]
    lo = max(case["from"], start(a), start(b), a["anchor"], b["anchor"])
    hi = min(case["to"], end(a), end(b))
    t = first_unproved(a, b, lo, hi, case["sep"]) if hi > lo else None
    return {"id": case["id"], "conflict": t is not None, "t": t if t is not None else 0}


def main():
    for line in sys.stdin:
        if line.strip():
            sys.stdout.write(json.dumps(check(json.loads(line))) + "\n")
            sys.stdout.flush()


if __name__ == "__main__":
    main()
