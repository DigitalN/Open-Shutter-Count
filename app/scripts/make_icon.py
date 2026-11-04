#!/usr/bin/env python3
"""Draws Open Shutter Count's icon: a simple aperture on a graphite tile.

    python3 app/scripts/make_icon.py <folder>.iconset       the PNGs iconutil turns into an .icns
    python3 app/scripts/make_icon.py --png <file.png> [size]  one PNG, to look at

Pure Python, so it runs anywhere build.sh does and the icon can always be rebuilt. Each
shape is a signed distance field, which gives smooth edges without supersampling. The
1024 pixel image is drawn, and the smaller ones are scaled down from it.

The geometry matches the aperture in index.html's title bar: six blades, each edge of a
hexagon carried on to the rim of the lens.
"""

import math
import struct
import sys
import zlib
from pathlib import Path

SIZE = 1024
CENTER = SIZE / 2

# Apple's macOS icon grid: an 824 pt tile with 185 pt corners, centred in 1024.
TILE_HALF, TILE_RADIUS = 412, 185
RING_OUTER, RING_INNER = 300, 262
HEX_RADIUS = 112  # centre to each corner of the opening
BLADE_WIDTH = 12

GRAPHITE_TOP, GRAPHITE_BOTTOM = (0x3a, 0x3f, 0x4b), (0x16, 0x18, 0x1d)
RING_TOP, RING_BOTTOM = (0xec, 0xef, 0xf4), (0x98, 0xa1, 0xb3)
GLASS_CENTER, GLASS_EDGE = (0x1f, 0x2f, 0x55), (0x0b, 0x10, 0x1e)
OPENING_TOP, OPENING_BOTTOM = (0x5c, 0xb8, 0xff), (0x00, 0x6a, 0xff)
BLADE = (0x8f, 0xa4, 0xca)

# iconutil's names for each size it wants.
ICONSET = [(16, 1), (16, 2), (32, 1), (32, 2), (128, 1), (128, 2), (256, 1), (256, 2), (512, 1), (512, 2)]


def _mix(a, b, t):
    t = min(max(t, 0.0), 1.0)
    return tuple(a[i] + (b[i] - a[i]) * t for i in range(3))


def _coverage(distance, pixel):
    """How much of a pixel a shape covers, from the distance to its edge (negative inside)."""
    return min(max(0.5 - distance / pixel, 0.0), 1.0)


def _corners():
    return [(CENTER + HEX_RADIUS * math.cos(math.radians(-90 + 60 * k)),
             CENTER + HEX_RADIUS * math.sin(math.radians(-90 + 60 * k))) for k in range(6)]


def _blades():
    """Each hexagon edge, from its first corner, carried on to the inside of the ring."""
    corners = _corners()
    segments = []
    for k in range(6):
        (ax, ay), (bx, by) = corners[k], corners[(k + 1) % 6]
        dx, dy = bx - ax, by - ay
        length = math.hypot(dx, dy)
        dx, dy = dx / length, dy / length
        ox, oy = ax - CENTER, ay - CENTER
        b = ox * dx + oy * dy
        t = -b + math.sqrt(b * b - (ox * ox + oy * oy - RING_INNER ** 2))
        segments.append((ax, ay, ax + t * dx, ay + t * dy))
    return segments


def _edges():
    """The hexagon as half-planes: an outward normal and an offset for each edge."""
    corners = _corners()
    edges = []
    for k in range(6):
        (ax, ay), (bx, by) = corners[k], corners[(k + 1) % 6]
        nx, ny = by - ay, -(bx - ax)
        length = math.hypot(nx, ny)
        nx, ny = nx / length, ny / length
        if nx * (CENTER - ax) + ny * (CENTER - ay) > 0:  # point it outwards
            nx, ny = -nx, -ny
        edges.append((nx, ny, nx * ax + ny * ay))
    return edges


def _segment_distance(px, py, segment):
    ax, ay, bx, by = segment
    dx, dy = bx - ax, by - ay
    t = min(max(((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy), 0.0), 1.0)
    return math.hypot(px - ax - t * dx, py - ay - t * dy)


def _tile_distance(px, py, offset=0.0):
    qx = abs(px - CENTER) - (TILE_HALF - TILE_RADIUS)
    qy = abs(py - CENTER - offset) - (TILE_HALF - TILE_RADIUS)
    outside = math.hypot(max(qx, 0.0), max(qy, 0.0))
    return outside + min(max(qx, qy), 0.0) - TILE_RADIUS


def _over(dst, color, alpha):
    """Paints color at alpha over dst, an (r, g, b, a) in 0..255 and 0..1."""
    if alpha <= 0:
        return dst
    r, g, b, a = dst
    out = alpha + a * (1 - alpha)
    if out <= 0:
        return dst
    keep = a * (1 - alpha)
    return ((color[0] * alpha + r * keep) / out, (color[1] * alpha + g * keep) / out,
            (color[2] * alpha + b * keep) / out, out)


def render(size=SIZE):
    """The icon at `size` pixels square, as rows of (r, g, b, a)."""
    pixel = SIZE / size
    blades, edges = _blades(), _edges()
    rows = []
    for j in range(size):
        y = (j + 0.5) * pixel
        row = []
        for i in range(size):
            x = (i + 0.5) * pixel
            out = (0.0, 0.0, 0.0, 0.0)

            # A soft shadow under the tile, then the tile.
            shadow = _tile_distance(x, y, offset=10)
            if shadow < 30:
                out = _over(out, (0, 0, 0), 0.28 * min(max(1 - shadow / 30, 0.0), 1.0) ** 2)
            tile = _coverage(_tile_distance(x, y), pixel)
            if tile:
                out = _over(out, _mix(GRAPHITE_TOP, GRAPHITE_BOTTOM, (y - 100) / 824), tile)

            r = math.hypot(x - CENTER, y - CENTER)
            if r < RING_OUTER + pixel:
                ring = _coverage(r - RING_OUTER, pixel)
                out = _over(out, _mix(RING_TOP, RING_BOTTOM, (y - CENTER + RING_OUTER) / (2 * RING_OUTER)), ring)
                glass = _coverage(r - RING_INNER, pixel)
                if glass:
                    out = _over(out, _mix(GLASS_CENTER, GLASS_EDGE, r / RING_INNER), glass)

                    hexagon = max(nx * x + ny * y - c for nx, ny, c in edges)
                    opening = _coverage(hexagon, pixel)
                    if opening:
                        out = _over(out, _mix(OPENING_TOP, OPENING_BOTTOM, (y - CENTER + HEX_RADIUS) / (2 * HEX_RADIUS)),
                                    opening * glass)

                    blade = min(_segment_distance(x, y, s) for s in blades) - BLADE_WIDTH / 2
                    out = _over(out, BLADE, 0.9 * _coverage(blade, pixel) * glass)

                    # A soft reflection on the glass, top left.
                    shine = 1 - math.hypot(x - CENTER + 95, y - CENTER + 110) / 150
                    if shine > 0:
                        out = _over(out, (255, 255, 255), 0.10 * shine * glass)
            row.append(out)
        rows.append(row)
    return rows


def scale_down(rows, size):
    """Box-filters a square image down to `size`, which must divide it."""
    step = len(rows) // size
    out = []
    for j in range(size):
        row = []
        for i in range(size):
            r = g = b = a = 0.0
            for y in range(j * step, (j + 1) * step):
                for x in range(i * step, (i + 1) * step):
                    pr, pg, pb, pa = rows[y][x]
                    r, g, b, a = r + pr * pa, g + pg * pa, b + pb * pa, a + pa
            n = step * step
            row.append((r / a, g / a, b / a, a / n) if a else (0.0, 0.0, 0.0, 0.0))
        out.append(row)
    return out


def png(rows):
    """An RGBA PNG of rows of (r, g, b, a)."""
    height, width = len(rows), len(rows[0])
    raw = bytearray()
    for row in rows:
        raw.append(0)
        for r, g, b, a in row:
            raw += bytes((round(r), round(g), round(b), round(a * 255)))

    def chunk(kind, data):
        return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xffffffff)

    header = struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) + chunk(b"IDAT", zlib.compress(bytes(raw), 9))
            + chunk(b"IEND", b""))


def iconset(folder):
    folder = Path(folder)
    folder.mkdir(parents=True, exist_ok=True)
    full = render(SIZE)
    for points, scale in ICONSET:
        pixels = points * scale
        image = full if pixels == SIZE else scale_down(full, pixels)
        name = f"icon_{points}x{points}" + ("@2x" if scale == 2 else "") + ".png"
        (folder / name).write_bytes(png(image))
    return folder


def main(arguments):
    if len(arguments) >= 2 and arguments[0] == "--png":
        size = int(arguments[2]) if len(arguments) > 2 else 256
        full = render(SIZE)
        Path(arguments[1]).write_bytes(png(full if size == SIZE else scale_down(full, size)))
        return 0
    if len(arguments) == 1 and arguments[0].endswith(".iconset"):
        iconset(arguments[0])
        return 0
    print(__doc__.split("\n\n")[1], file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
