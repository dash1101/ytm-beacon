#!/usr/bin/env python3
"""Regenerates the extension icons and the Discord play/pause badges.

Needs Pillow (pip install pillow). Only run this if you want to change the artwork;
the generated PNGs are committed.
"""
import os
from PIL import Image, ImageDraw

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SS = 4  # supersampling factor for smooth edges
BLURPLE = (88, 101, 242, 255)
WHITE = (255, 255, 255, 255)
DARK = (32, 34, 37, 255)


def note(d, s):
    """Draw a beamed double eighth note, coordinates are fractions of s."""
    def pt(x, y):
        return (x * s, y * s)

    d.polygon([pt(0.435, 0.30), pt(0.805, 0.22), pt(0.805, 0.34), pt(0.435, 0.42)], fill=WHITE)
    d.rectangle([pt(0.435, 0.30), pt(0.485, 0.70)], fill=WHITE)
    d.rectangle([pt(0.755, 0.27), pt(0.805, 0.62)], fill=WHITE)
    for cx, cy in ((0.375, 0.70), (0.695, 0.62)):
        head = Image.new("L", (int(s), int(s)), 0)
        hd = ImageDraw.Draw(head)
        hd.ellipse([(cx - 0.11) * s, (cy - 0.08) * s, (cx + 0.11) * s, (cy + 0.08) * s], fill=255)
        head = head.rotate(20, center=(cx * s, cy * s), resample=Image.BICUBIC)
        d._image.paste(WHITE, (0, 0), head)


def app_icon(size):
    s = size * SS
    img = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    d.rounded_rectangle([0, 0, s - 1, s - 1], radius=int(s * 0.22), fill=BLURPLE)
    note(d, s)
    return img.resize((size, size), Image.LANCZOS)


def badge(size, kind):
    s = size * SS
    img = Image.new("RGBA", (s, s), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    d.ellipse([0, 0, s - 1, s - 1], fill=DARK)
    if kind == "play":
        d.polygon([(0.38 * s, 0.28 * s), (0.38 * s, 0.72 * s), (0.74 * s, 0.50 * s)], fill=WHITE)
    else:
        d.rounded_rectangle([0.32 * s, 0.28 * s, 0.44 * s, 0.72 * s], radius=int(s * 0.03), fill=WHITE)
        d.rounded_rectangle([0.56 * s, 0.28 * s, 0.68 * s, 0.72 * s], radius=int(s * 0.03), fill=WHITE)
    return img.resize((size, size), Image.LANCZOS)


def main():
    icons = os.path.join(ROOT, "extension", "icons")
    assets = os.path.join(ROOT, "assets")
    os.makedirs(icons, exist_ok=True)
    os.makedirs(assets, exist_ok=True)
    for size in (16, 32, 48, 128):
        app_icon(size).save(os.path.join(icons, f"icon{size}.png"))
    app_icon(512).save(os.path.join(assets, "logo.png"))
    badge(256, "play").save(os.path.join(assets, "play.png"))
    badge(256, "pause").save(os.path.join(assets, "pause.png"))
    print("wrote icons")


if __name__ == "__main__":
    main()
