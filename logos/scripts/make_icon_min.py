#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Rainer Sestig und Mitwirkende.
"""Vereinfachtes Emblem fuer kleine Groessen (Favicon/Avatar <= 48 px).

Weniger Rauschen: keine Sehnen, keine Datenpakete, kein beitretender Peer -
nur Hub-Stern + Peer-Ring. Erzeugt ai-swarms-net-light-icon-min.svg (+ PNGs).
"""
import math
import os

import cairosvg

from make_logo import (NAVY, defs_markup, doc, dot, hexagon, hexpoints, line,
                       NODE_FILLS, save)

OUT = os.path.dirname(os.path.abspath(__file__))


def minimal(cx, cy, R, hub_r, node_r, halo="#FFFFFF", core="#FFFFFF"):
    peers = [(cx + R * math.cos(math.radians(90 + 60 * i)),
              cy - R * math.sin(math.radians(90 + 60 * i))) for i in range(6)]
    hub = (cx, cy)
    g = []
    for p in peers:
        g.append(line(hub, p, stroke="url(#link)",
                      **{"stroke-width": 9, "stroke-opacity": 0.95,
                         "stroke-linecap": "round"}))
    for i in range(6):
        g.append(line(peers[i], peers[(i + 1) % 6], stroke="url(#link)",
                      **{"stroke-width": 7, "stroke-opacity": 0.55,
                         "stroke-linecap": "round"}))
    for i, p in enumerate(peers):
        g.append(hexagon(p, node_r, NODE_FILLS[i], stroke=halo, sw=6))
        g.append(dot(p, node_r * 0.30, core, opacity=0.92))
    g.append(hexagon(hub, hub_r, "url(#hub)", stroke=halo, sw=7))
    g.append(dot(hub, hub_r * 0.34, core))
    return "".join(g)


def main():
    S = 440
    body = minimal(S / 2, S / 2 + 4, R=138, hub_r=62, node_r=42)
    save("ai-swarms-net-light-icon-min.svg", doc(S, S, body, bg="#FFFFFF",
                                                 title="AI-Swarms.net emblem"))
    body_d = minimal(S / 2, S / 2 + 4, R=138, hub_r=62, node_r=42, halo=NAVY)
    save("ai-swarms-net-dark-icon-min.svg", doc(S, S, body_d, bg=NAVY,
                                                title="AI-Swarms.net emblem"))
    os.makedirs(os.path.join(OUT, "favicon"), exist_ok=True)
    for s in (16, 32, 48, 64, 128, 180, 320, 512):
        name = (f"favicon/apple-touch-icon-{s}.png" if s == 180
                else f"favicon/favicon-{s}.png")
        cairosvg.svg2png(url=os.path.join(OUT, "ai-swarms-net-light-icon-min.svg"),
                         write_to=os.path.join(OUT, name),
                         output_width=s, output_height=s)
    print("favicon-Set aktualisiert (minimal)")


if __name__ == "__main__":
    main()
