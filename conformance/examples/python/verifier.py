#!/usr/bin/env python3
"""An example Verifier for the Clearway conformance suite, in Python.

It shows the line protocol and nothing more. The method is a port of this
project's "sampled" implementation, so it is not an independent one. A new
Verifier should be written from docs/spec.md, not from this file.

Protocol: one case per line of JSON on standard input, one answer per line of
JSON on standard output, flushed after each line.

    python3 verifier.py < cases.jsonl

Units: nautical miles, feet, seconds.
"""
import json
import math
import sys

STEP = 1.0  # seconds: the longest interval that is reported without proof


def radius(tube, t):
    return tube["r0"] + tube["k"] * max(0.0, t - tube["anchor"])


def vertical_uncertainty(tube, t):
    return tube["vu0"] + tube["vuRate"] * max(0.0, t - tube["anchor"])


def sigma(tube, t):
    """The time by which the aircraft may be early or late at t."""
    return radius(tube, t) / tube["vmin"] if tube["vmin"] > 0 else 0.0


def start(tube):
    return tube["legs"][0]["t0"]


def end(tube):
    return tube["legs"][-1]["t1"]


def position(tube, t):
    t = min(max(t, start(tube)), end(tube))
    leg = tube["legs"][-1]
    for candidate in tube["legs"]:
        if t < candidate["t1"]:
            leg = candidate
            break
    dt = t - leg["t0"]
    return leg["p0"][0] + leg["v"][0] * dt, leg["p0"][1] + leg["v"][1] * dt


def band(tube, t0, t1):
    """The union of the bands of the vertical segments that overlap [t0, t1]."""
    lo, hi = math.inf, -math.inf
    for seg in tube["vsegs"]:
        if seg["t1"] < t0 or seg["t0"] > t1:
            continue
        lo = min(lo, seg["a0"], seg["a1"])
        hi = max(hi, seg["a0"], seg["a1"])
    if lo > hi:  # the interval lies outside the trajectory
        seg = tube["vsegs"][-1] if t0 > end(tube) else tube["vsegs"][0]
        return min(seg["a0"], seg["a1"]), max(seg["a0"], seg["a1"])
    return lo, hi


def max_speed(tube, t0, t1):
    best = 0.0
    for leg in tube["legs"]:
        if leg["t1"] < t0 or leg["t0"] > t1:
            continue
        best = max(best, math.hypot(*leg["v"]))
    if best == 0.0:
        best = math.hypot(*tube["legs"][0]["v"])
    return best


def first_unproved(a, b, lo, hi, sep):
    """The start of the first interval that cannot be shown separated, or None.

    The pair is separated on [lo, hi] if its vertical extents are far enough
    apart for the whole interval, or if the distance at the midpoint, less the
    most that the pair can close in half the interval, exceeds the required
    distance at the end of the interval. Otherwise halve the interval. An
    interval of at most STEP seconds that cannot be shown separated is reported.
    """
    slack = sep.get("vertSlack", 0.0)
    a_lo, a_hi = band(a, lo - sigma(a, lo) - slack, hi + sigma(a, hi) + slack)
    b_lo, b_hi = band(b, lo - sigma(b, lo) - slack, hi + sigma(b, hi) + slack)
    u = vertical_uncertainty(a, hi) + vertical_uncertainty(b, hi)
    if a_lo - b_hi - u >= sep["svTrue"] or b_lo - a_hi - u >= sep["svTrue"]:
        return None
    mid = 0.5 * (lo + hi)
    ax, ay = position(a, mid)
    bx, by = position(b, mid)
    closure = max_speed(a, lo, hi) + max_speed(b, lo, hi)
    required = sep["sh"] + radius(a, hi) + radius(b, hi)
    if math.hypot(ax - bx, ay - by) - closure * 0.5 * (hi - lo) - required >= 0:
        return None
    if hi - lo <= STEP:
        return lo
    left = first_unproved(a, b, lo, mid, sep)
    return left if left is not None else first_unproved(a, b, mid, hi, sep)


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
