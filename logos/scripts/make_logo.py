#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later
# Copyright (C) 2026 Rainer Sestig und Mitwirkende.
"""AI-Swarms.net Logo-Generator (echtes SVG, Wortmarke als Pfad-Outlines).

Erzeugt in /home/user/logos:
  ai-swarms-net-light.svg        helles Logo fuer README/Docs (Weiss)
  ai-swarms-net-light-icon.svg   nur Schwarm-Emblem, quadratisch (Favicon/Avatar)
  ai-swarms-net-dark.svg         Variante auf Navy
  *.png                          Preview (cairosvg)

Motiv "vernetzter Schwarm": koordinierender Hub-Knoten in der Mitte, sechs
Peer-Knoten als Sechseck mit Vollvermaschung (Stern + Ring + Sehnen) und ein
beitretender Peer, der nur als Kontur mit gestrichelten Links gezeigt wird.
"""
import math
import os

from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.ttLib import TTFont

OUT = os.path.dirname(os.path.abspath(__file__))
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"

NAVY = "#0B1220"
TEAL = "#0E7490"
SUBTLE = "#5B6B85"
DASH = "#334E75"


# ------------------------------------------------------------------ Font -> Pfad
class Face:
    def __init__(self, path):
        self.font = TTFont(path)
        self.upem = self.font["head"].unitsPerEm
        self.cmap = self.font.getBestCmap()
        self.glyphs = self.font.getGlyphSet()
        self.hmtx = self.font["hmtx"]

    def advance(self, ch, size):
        g = self.cmap.get(ord(ch))
        return self.hmtx[g][0] * size / self.upem if g else 0.0

    def width(self, text, size):
        return sum(self.advance(c, size) for c in text)

    def run(self, text, size, x, baseline, fill):
        """<g> mit Pfad-Outlines fuer `text`; Rueckgabe (markup, x nach Text)."""
        s = size / self.upem
        parts, px = [], x
        for ch in text:
            g = self.cmap.get(ord(ch))
            if not g:
                continue
            pen = SVGPathPen(self.glyphs)
            self.glyphs[g].draw(pen)
            d = pen.getCommands()
            if d:
                parts.append(
                    f'<path transform="translate({px:.2f},{baseline:.2f}) '
                    f'scale({s:.7f},{-s:.7f})" d="{d}" fill="{fill}"/>')
            px += self.advance(ch, size)
        return "".join(parts), px


# --------------------------------------------------------------------- Geometrie
def hexpoints(cx, cy, r, rot=90.0):
    out = []
    for i in range(6):
        a = math.radians(rot + 60 * i)
        out.append(f"{cx + r * math.cos(a):.2f},{cy - r * math.sin(a):.2f}")
    return " ".join(out)


def line(p, q, **kw):
    attrs = {
        "x1": f"{p[0]:.2f}", "y1": f"{p[1]:.2f}",
        "x2": f"{q[0]:.2f}", "y2": f"{q[1]:.2f}",
    }
    attrs.update(kw)
    return "<line " + " ".join(f'{k}="{v}"' for k, v in attrs.items()) + "/>"


def dot(p, r, fill, opacity=None):
    o = f' opacity="{opacity}"' if opacity is not None else ""
    return f'<circle cx="{p[0]:.2f}" cy="{p[1]:.2f}" r="{r:.2f}" fill="{fill}"{o}/>'


def hexagon(p, r, fill, stroke="none", sw=0, dash=None, opacity=None):
    d = f' stroke-dasharray="{dash}"' if dash else ""
    o = f' opacity="{opacity}"' if opacity is not None else ""
    return (f'<polygon points="{hexpoints(p[0], p[1], r)}" fill="{fill}" '
            f'stroke="{stroke}" stroke-width="{sw}" stroke-linejoin="round"{d}{o}/>')


# --------------------------------------------------------------------- Farbverlauf
GRADS = {
    "link": [("#22D3EE", 0), ("#4F46E5", 1)],
    "hub": [("#22D3EE", 0), ("#2563EB", 0.55), ("#4F46E5", 1)],
    "n0": [("#2DD4BF", 0), ("#0E7490", 1)],
    "n1": [("#38BDF8", 0), ("#4338CA", 1)],
    "n2": [("#14B8A6", 0), ("#1D4ED8", 1)],
    "n3": [("#6366F1", 0), ("#7C3AED", 1)],
    "n4": [("#22D3EE", 0), ("#1E40AF", 1)],
    "n5": [("#06B6D4", 0), ("#6D28D9", 1)],
}
NODE_FILLS = ["url(#n0)", "url(#n1)", "url(#n2)", "url(#n3)", "url(#n4)", "url(#n5)"]


def defs_markup(stroke_on_light=True):
    parts = []
    for gid, stops in GRADS.items():
        parts.append(f'<linearGradient id="{gid}" x1="0" y1="0" x2="1" y2="1">')
        for col, off in stops:
            parts.append(f'<stop offset="{off}" stop-color="{col}"/>')
        parts.append("</linearGradient>")
    return "".join(parts)


# ------------------------------------------------------------------------ Emblem
def emblem(cx, cy, R, hub_r, node_r, join_r, join_angle=18.0, join_dist=1.62,
           halo="#FFFFFF", core="#FFFFFF", join_fill="#FFFFFF", join_ink=DASH):
    """Schwarm-Zentrum bei (cx, cy). R = Radius des Peer-Sechsecks."""
    peers = [(cx + R * math.cos(math.radians(90 + 60 * i)),
              cy - R * math.sin(math.radians(90 + 60 * i))) for i in range(6)]
    jr = R * join_dist
    joiner = (cx + jr * math.cos(math.radians(join_angle)),
              cy - jr * math.sin(math.radians(join_angle)))
    hub = (cx, cy)

    g = []

    # 1) Sehnen: duedunn, zeigen den dichten Maschendraht des Schwarms
    for a, b in [(0, 2), (2, 4), (4, 0), (1, 3), (3, 5), (5, 1), (0, 3), (1, 4), (2, 5)]:
        g.append(line(peers[a], peers[b], stroke="url(#link)",
                      **{"stroke-width": 3.2, "stroke-opacity": 0.35,
                         "stroke-linecap": "round"}))
    # 2) Hub-Stern: jeder Peer haengt am koordinierenden Modell
    for p in peers:
        g.append(line(hub, p, stroke="url(#link)",
                      **{"stroke-width": 6.5, "stroke-opacity": 0.95,
                         "stroke-linecap": "round"}))
    # 3) Peer-Ring
    for i in range(6):
        g.append(line(peers[i], peers[(i + 1) % 6], stroke="url(#link)",
                      **{"stroke-width": 6.5, "stroke-opacity": 0.95,
                         "stroke-linecap": "round"}))
    # 4)beitretender Peer: gestrichelte Links zu den zwei naechsten Nachbarn
    for p in (peers[0], peers[1]):
        g.append(line(joiner, p, stroke=join_ink,
                      **{"stroke-width": 4.2, "stroke-opacity": 0.8,
                         "stroke-linecap": "round", "stroke-dasharray": "1 12"}))

    # 5) Datenpakete auf den aktiven Links
    for p, q in [(hub, peers[0]), (hub, peers[3]), (peers[1], peers[2]),
                 (peers[4], peers[5]), (hub, peers[5])]:
        for t in (0.42, 0.70):
            dp = (p[0] + (q[0] - p[0]) * t, p[1] + (q[1] - p[1]) * t)
            g.append(dot(dp, 4.2, "url(#link)"))

    # 6) Peer-Knoten
    for i, p in enumerate(peers):
        g.append(hexagon(p, node_r, NODE_FILLS[i], stroke=halo, sw=5.5))
        g.append(dot(p, node_r * 0.28, core, opacity=0.92))

    # 7) Hub = koordinierendes Modell: groesser, Doppelring
    g.append(f'<circle cx="{cx}" cy="{cy}" r="{hub_r + 13}" fill="none" '
             f'stroke="url(#link)" stroke-width="3" stroke-opacity="0.45"/>')
    g.append(hexagon(hub, hub_r, "url(#hub)", stroke=halo, sw=6.5))
    g.append(dot(hub, hub_r * 0.33, core))

    # 8)beitretender Peer: nur Kontur
    g.append(hexagon(joiner, join_r, join_fill, stroke=join_ink, sw=4.2,
                     dash="8 7", opacity=0.95))
    g.append(dot(joiner, join_r * 0.26, join_ink, opacity=0.6))

    return "".join(g), (peers, joiner)


def doc(w, h, body, bg=None, title="AI-Swarms.net"):
    bg_el = f'<rect width="{w}" height="{h}" fill="{bg}"/>' if bg else ""
    return (f'<?xml version="1.0" encoding="UTF-8"?>\n'
            f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" '
            f'width="{w}" height="{h}" role="img" aria-label="{title} logo">\n'
            f'<title>{title}</title>\n<defs>{defs_markup()}</defs>\n{bg_el}{body}\n</svg>\n')


def save(name, content):
    with open(os.path.join(OUT, name), "w", encoding="utf-8") as fh:
        fh.write(content)
    print("geschrieben:", name)


# -------------------------------------------------------------------------- Lockup
def lockup(name, bg, word_fill, tail_fill, sub_fill, *, halo="#FFFFFF",
           join_fill="#FFFFFF", join_ink=DASH):
    face = Face(FONT)
    head, tail_txt, sub = "AI-Swarms", ".net", "LOCAL AI MODELS · ONE SWARM · ONE ENDPOINT"
    size, sub_size = 112, 26

    pad, box, gap = 60, 420, 54
    hw = face.width(head, size)
    tw = face.width(tail_txt, size)
    W = int(pad + box + gap + hw + tw + pad)
    H = 420
    cx, cy = pad + box / 2, H / 2 - 8

    body, _ = emblem(cx, cy, R=132, hub_r=52, node_r=35, join_r=29, halo=halo,
                     join_fill=join_fill, join_ink=join_ink)

    tx = pad + box + gap
    base = H / 2 + 6
    m1, x2 = face.run(head, size, tx, base, word_fill)
    m2, _ = face.run(tail_txt, size, x2, base, tail_fill)
    m3, _ = face.run(sub, sub_size, tx, base + 66, sub_fill)
    body += m1 + m2 + m3
    save(name, doc(W, H, body, bg=bg))


def icon(name, bg, *, halo="#FFFFFF", join_fill="#FFFFFF", join_ink=DASH):
    S = 440
    body, _ = emblem(S / 2 - 34, S / 2 + 16, R=132, hub_r=52, node_r=35, join_r=29,
                     halo=halo, join_fill=join_fill, join_ink=join_ink)
    save(name, doc(S, S, body, bg=bg, title="AI-Swarms.net emblem"))


def main():
    lockup("ai-swarms-net-light.svg", "#FFFFFF", NAVY, TEAL, SUBTLE)
    lockup("ai-swarms-net-dark.svg", NAVY, "#FFFFFF", "#22D3EE", "#93A4C3",
           halo=NAVY, join_fill=NAVY, join_ink="#7DA2D8")
    icon("ai-swarms-net-light-icon.svg", "#FFFFFF")
    icon("ai-swarms-net-dark-icon.svg", NAVY, halo=NAVY, join_fill=NAVY,
         join_ink="#7DA2D8")


if __name__ == "__main__":
    main()
