#!/usr/bin/env python3
"""Generates outline-vpn.svg: the Outline circle split vertically, with half
of a KDE-style gear (white) on the left and a half circle in Plasma blue on
the right.

    python3 data/icons/make_icon.py > data/icons/outline-vpn.svg
    python3 data/icons/make_icon.py --rotation 18 --background '#31363b' --background-radius 12

`make icons` regenerates the installed icon and the README's light/dark
theme variants.

The constants below are the defaults; the options override them.
"""
import argparse
import math
import sys

CX, CY = 32.0, 32.0
R_TIP, R_BODY, R_HUB = 28.0, 20.0, 15.0  # body ring is R_BODY - R_HUB thick
TEETH = 8                # around a full gear
TIP_HALF, BASE_HALF = 7.0, 15.0  # half-widths in degrees (tapered teeth)
CUT_WIDTH = 5.0              # vertical strip cut out of the center; the halves stay in place
ROTATION = 22.5               # gear rotation in degrees, clockwise; 0 = teeth centered on the cut
BACKGROUND = None           # background color, None = transparent
BACKGROUND_RADIUS = 12.0      # corner radius of the background, 0..32
BLUE = "#1d99f3"
WHITE = "#ffffff"
GEAR_COLOR = WHITE           # gear color; use a dark color for light backgrounds

EPS = 1e-9


def polar(r, deg):
    a = math.radians(deg)
    return CX + r * math.cos(a), CY + r * math.sin(a)


def fmt(p):
    return f"{p[0]:.3f},{p[1]:.3f}"


# The gear outline is a closed loop of segments with increasing angle
# (clockwise on screen, since y points down):
#   ("arc", r, a0, a1)           circular arc at radius r from angle a0 to a1
#   ("line", r0, a0, r1, a1)     straight tooth flank between two polar points
def gear_segments(rotation):
    period = 360 / TEETH
    segs = []
    for k in range(TEETH):
        c = 90 + rotation + k * period
        segs += [
            ("line", R_BODY, c - BASE_HALF, R_TIP, c - TIP_HALF),   # rising flank
            ("arc", R_TIP, c - TIP_HALF, c + TIP_HALF),             # tooth top
            ("line", R_TIP, c + TIP_HALF, R_BODY, c + BASE_HALF),   # falling flank
            ("arc", R_BODY, c + BASE_HALF, c + period - BASE_HALF),  # gap
        ]
    return segs


def seg_ends(s):
    if s[0] == "arc":
        _, r, a0, a1 = s
        return polar(r, a0), polar(r, a1)
    _, r0, a0, r1, a1 = s
    return polar(r0, a0), polar(r1, a1)


def crossing(s, xc):
    """Point where segment s crosses the vertical line x = xc (it is known to)."""
    if s[0] == "line":
        p0, p1 = seg_ends(s)
        t = (xc - p0[0]) / (p1[0] - p0[0])
        return (xc, p0[1] + t * (p1[1] - p0[1])), None
    _, r, a0, a1 = s
    base = math.degrees(math.acos((xc - CX) / r))
    for a in (base + 360 * n for n in range(-3, 4)):
        for cand in (a, -a):
            if a0 - EPS <= cand <= a1 + EPS:
                return polar(r, cand), cand
    raise ValueError("arc does not cross the cut")


def seg_to(s, start_angle=None, end_angle=None):
    """SVG command drawing segment s (optionally only part of an arc)."""
    if s[0] == "line":
        return f"L{fmt(seg_ends(s)[1])}"
    _, r, a0, a1 = s
    a1 = a1 if end_angle is None else end_angle
    return f"A{r},{r} 0 0 1 {fmt(polar(r, a1))}"


def left_gear(rotation, h):
    """Gear outline trimmed to x <= CX - h, with the hub hole, as an SVG path."""
    xc = CX - h
    segs = gear_segments(rotation)
    n = len(segs)
    inside = [seg_ends(s)[1][0] < xc for s in segs]  # is each segment's end on the left?

    # Entry: a segment that starts on the right and ends on the left (near the
    # bottom, since angles increase clockwise from the bottom to the left).
    def starts_inside(i):
        return seg_ends(segs[i])[0][0] < xc

    entry = next(i for i in range(n) if not starts_inside(i) and inside[i])
    d = []
    point, angle = crossing(segs[entry], xc)
    d.append(f"M{fmt(point)}")
    d.append(seg_to(segs[entry]))
    i = (entry + 1) % n
    while True:
        s = segs[i]
        if not inside[i]:  # exit: ends on the right, draw only up to the cut
            point, angle = crossing(s, xc)
            d.append(seg_to(s, end_angle=angle) if s[0] == "arc" else f"L{fmt(point)}")
            break
        d.append(seg_to(s))
        i = (i + 1) % n

    # Down the cut edge to the hub, around the hub hole, and back up the edge.
    hub = math.degrees(math.acos(-h / R_HUB)) - 90
    d.append(f"L{fmt(polar(R_HUB, 270 - hub))}")
    d.append(f"A{R_HUB},{R_HUB} 0 0 0 {fmt(polar(R_HUB, 90 + hub))}")
    d.append("Z")
    return " ".join(d)


def right_half(h):
    dy = math.sqrt(R_TIP**2 - h**2)
    return f"M{CX + h:.3f},{CY - dy:.3f} A{R_TIP},{R_TIP} 0 0 1 {CX + h:.3f},{CY + dy:.3f} Z"


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    ap.add_argument("--rotation", type=float, default=ROTATION, help="gear rotation in degrees, clockwise (default %(default)s)")
    ap.add_argument("--gear-color", default=GEAR_COLOR, help="gear color (default %(default)s)")
    ap.add_argument("--background", default=BACKGROUND, help="background color; transparent if omitted")
    ap.add_argument("--background-radius", type=float, default=BACKGROUND_RADIUS,
                    help="corner radius of the background, 0..32 (default %(default)s)")
    args = ap.parse_args()

    h = CUT_WIDTH / 2
    if not h < R_HUB < R_BODY < R_TIP:
        sys.exit("need CUT_WIDTH/2 < R_HUB < R_BODY < R_TIP")
    if not 0 <= args.background_radius <= 32:
        sys.exit("--background-radius must be between 0 and 32")

    background = ""
    if args.background:
        background = (f'  <rect width="64" height="64" rx="{args.background_radius:g}" '
                      f'fill="{args.background}"/>\n')

    sys.stdout.write(f'''<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">
  <title>Outline VPN</title>
  <!-- One circle with a vertical strip cut out of the center.
       Left: half of a KDE-style gear. Right: half circle in Plasma blue. -->
{background}  <path fill="{args.gear_color}"
        d="{left_gear(args.rotation, h)}"/>
  <path fill="{BLUE}"
        d="{right_half(h)}"/>
</svg>
''')


if __name__ == "__main__":
    main()
