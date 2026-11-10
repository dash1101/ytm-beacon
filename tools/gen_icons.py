#!/usr/bin/env python3
"""Renders the app's artwork from the SVG sources in assets/.

    python3 tools/gen_icons.py

Sources (edit these, then run this script):
  assets/icon.svg         the logo (48 px and up, the Discord fallback image, the dashboard)
  assets/icon-small.svg   the same logo simplified for 16 and 32 px (no badge, bigger shapes)
  assets/badge-play.svg   Discord's small round play / pause badge
  assets/badge-pause.svg

It writes the PNGs the extension and Discord use, the grey "switched off" toolbar icons,
and a copy of icon.svg for the dashboard. The results are committed, so only run this when
the artwork changes. Needs Pillow (pip install pillow) and one SVG renderer: rsvg-convert,
Inkscape, or the cairosvg Python package.
"""
import io
import os
import shutil
import subprocess
import sys
import tempfile

from PIL import Image, ImageEnhance

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ASSETS = os.path.join(ROOT, "assets")
ICONS = os.path.join(ROOT, "extension", "icons")


def render(svg, size):
    """Returns the SVG file rendered as a size x size RGBA image."""
    if shutil.which("rsvg-convert"):
        png = subprocess.run(["rsvg-convert", "-w", str(size), "-h", str(size), svg], check=True, capture_output=True).stdout
        return Image.open(io.BytesIO(png)).convert("RGBA")
    if shutil.which("inkscape"):
        with tempfile.TemporaryDirectory() as tmp:
            out = os.path.join(tmp, "out.png")
            subprocess.run(["inkscape", svg, "--export-type=png", f"--export-filename={out}", "-w", str(size), "-h", str(size)],
                           check=True, capture_output=True)
            return Image.open(out).convert("RGBA")
    try:
        import cairosvg
    except ImportError:
        sys.exit("No SVG renderer found: install rsvg-convert (librsvg), Inkscape, or `pip install cairosvg`.")
    return Image.open(io.BytesIO(cairosvg.svg2png(url=svg, output_width=size, output_height=size))).convert("RGBA")


def switched_off(img):
    """The toolbar icon while showing on Discord is switched off: grey and a little faded."""
    grey = img.convert("LA").convert("RGBA")
    grey = ImageEnhance.Contrast(grey).enhance(0.8)
    grey.putalpha(grey.getchannel("A").point(lambda a: int(a * 0.85)))
    return grey


def save(img, path):
    img.save(path, optimize=True)
    print("wrote", os.path.relpath(path, ROOT))


def main():
    big = os.path.join(ASSETS, "icon.svg")
    small = os.path.join(ASSETS, "icon-small.svg")
    os.makedirs(ICONS, exist_ok=True)

    for size in (16, 32):
        img = render(small, size)
        save(img, os.path.join(ICONS, f"icon{size}.png"))
        save(switched_off(img), os.path.join(ICONS, f"off{size}.png"))
    for size in (48, 128):
        save(render(big, size), os.path.join(ICONS, f"icon{size}.png"))

    # Discord fetches these from the repository (see internal/presence/build.go).
    save(render(big, 512), os.path.join(ASSETS, "logo.png"))
    save(render(os.path.join(ASSETS, "badge-play.svg"), 256), os.path.join(ASSETS, "play.png"))
    save(render(os.path.join(ASSETS, "badge-pause.svg"), 256), os.path.join(ASSETS, "pause.png"))

    # The dashboard's own copy (Go can only embed files from its own folder).
    shutil.copyfile(big, os.path.join(ROOT, "internal", "server", "icon.svg"))
    print("wrote internal/server/icon.svg")


if __name__ == "__main__":
    main()
