"""Builds every FlexTerm icon variant from flexterm.svg (option 6, "Flex": navy
tile #0F2351, orange #F46414 '>' and wave cursor). Writes render jobs for
render.mjs (Chrome headless) and assembles build/windows/icon.ico."""
import json, os, sys

NAVY, ORANGE = '#0F2351', '#F46414'
CHEVRON = 'M26 36 L42 50 L26 64'
WAVE = 'M48 62 C54 54 60 54 66 62 S78 70 82 62'

def glyph(color, w1=8, w2=7):
    return (f'<path d="{CHEVRON}" fill="none" stroke="{color}" stroke-width="{w1}" stroke-linecap="round" stroke-linejoin="round"/>'
            f'<path d="{WAVE}" fill="none" stroke="{color}" stroke-width="{w2}" stroke-linecap="round" stroke-linejoin="round"/>')

def svg(body, view='0 0 100 100', defs=''):
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{view}" width="1024" height="1024"><defs>{defs}</defs>{body}</svg>'

def tile(margin, shadow=False):
    """The design tile inset by `margin` (design units of 100), optional soft shadow."""
    s = (100 - 2 * margin) / 100
    defs = ('<filter id="sh" x="-20%" y="-20%" width="140%" height="140%">'
            '<feDropShadow dx="0" dy="0.9" stdDeviation="1.3" flood-color="#000" flood-opacity="0.30"/></filter>') if shadow else ''
    rect = f'<rect width="100" height="100" rx="23" fill="{NAVY}"{" filter=\"url(#sh)\"" if shadow else ""}/>'
    return svg(f'<g transform="translate({margin} {margin}) scale({s})">{rect}{glyph(ORANGE)}</g>', defs=defs)

# macOS grid: 824 px of art in the 1024 canvas (margin 100/1024 = 9.77%).
APPICON = tile(100 / 1024 * 100, shadow=True)
# Small/flat contexts (Linux theme sizes, Android legacy, favicon): near
# full-bleed so the glyph keeps every pixel it can.
FLAT = tile(4)

def round_launcher():
    body = (f'<circle cx="50" cy="50" r="46" fill="{NAVY}"/>'
            f'<g transform="translate(50 50) scale(0.82) translate(-53.75 -50.75)">{glyph(ORANGE)}</g>')
    return svg(body)

# Tray glyph (Windows/Linux): orange glyph alone, filling the canvas, strokes a
# touch heavier for 16-24 px (optical sizing). Bounds ~x 21.5..86, y 31.5..70.
TRAY = svg(glyph(ORANGE, 9, 8), view='20.25 18.25 67 67')

# macOS menu bar template: black tile, glyph knocked out to transparency.
TEMPLATE = svg(
    '<rect x="4" y="4" width="92" height="92" rx="21" fill="#000" mask="url(#k)"/>',
    defs=f'<mask id="k"><rect width="100" height="100" fill="#fff"/>{glyph("#000", 9, 8)}</mask>')

def flat_small(w1=11, w2=10, zoom=1.12):
    """Optical-size variant for 16-24 px: heavier strokes and a slightly
    larger glyph, so the wave survives on the navy tile."""
    g = glyph(ORANGE, w1, w2)
    body = (f'<rect x="4" y="4" width="92" height="92" rx="21" fill="{NAVY}"/>'
            f'<g transform="translate(50 50) scale({zoom * 0.92}) translate(-53.75 -50.75) translate(3.75 0.75)">{g}</g>')
    return svg(body)


def jobs(repo, out_preview):
    J = []
    add = lambda svg_, size, path: J.append({'svg': svg_, 'size': size, 'out': path})
    add(APPICON, 1024, f'{repo}/build/appicon.png')
    add(TRAY, 64, f'{repo}/build/appicon_tray.png')
    add(TEMPLATE, 64, f'{repo}/build/appicon_tray_template.png')
    for s in (16, 32, 48, 64, 128, 256):
        add(flat_small() if s == 16 else FLAT, s, f'{repo}/build/linux/icons/hicolor/{s}x{s}/apps/flexterm.png')
    for s in ICO_SIZES:
        add(flat_small() if s <= 24 else FLAT, s, f'{out_preview}/ico-{s}.png')
    for d, s in (('mdpi', 48), ('hdpi', 72), ('xhdpi', 96), ('xxhdpi', 144), ('xxxhdpi', 192)):
        add(FLAT, s, f'{repo}/build/android/app/src/main/res/mipmap-{d}/ic_launcher.png')
        add(round_launcher(), s, f'{repo}/build/android/app/src/main/res/mipmap-{d}/ic_launcher_round.png')
    add(FLAT, 512, f'{repo}/frontend/public/favicon.png')
    return J

ICO_SIZES = (16, 24, 32, 48, 64, 128, 256)


def write_ico(pngs_dir, out):
    """Windows .ico with PNG-compressed entries (Vista+)."""
    import struct
    blobs = [open(f'{pngs_dir}/ico-{s}.png', 'rb').read() for s in ICO_SIZES]
    head = struct.pack('<HHH', 0, 1, len(ICO_SIZES))
    off, dirs = 6 + 16 * len(ICO_SIZES), b''
    for s, b in zip(ICO_SIZES, blobs):
        dirs += struct.pack('<BBBBHHII', s % 256, s % 256, 0, 0, 1, 32, len(b), off)
        off += len(b)
    open(out, 'wb').write(head + dirs + b''.join(blobs))


# Usage (from the repo root, macOS with Google Chrome):
#   python3 build/icon-src/build_icons.py jobs /tmp/flexicons /tmp/flexicons/jobs.json
#   node build/icon-src/render.mjs /tmp/flexicons/jobs.json
#   python3 build/icon-src/build_icons.py ico /tmp/flexicons
#   (cd build && wails3 generate icons -input appicon.png \
#      -macfilename darwin/icons.icns -windowsfilename darwin/.icon-unused.ico)
if __name__ == '__main__':
    cmd = sys.argv[1]
    repo = os.path.abspath(os.path.join(os.path.dirname(__file__), '..', '..'))
    if cmd == 'jobs':
        preview, out = sys.argv[2:4]
        os.makedirs(preview, exist_ok=True)
        json.dump(jobs(repo, preview), open(out, 'w'))
    elif cmd == 'ico':
        write_ico(sys.argv[2], f'{repo}/build/windows/icon.ico')
