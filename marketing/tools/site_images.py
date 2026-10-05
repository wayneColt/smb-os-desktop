#!/usr/bin/env python3
"""site_images.py: copy the release screenshots the solutions page uses, and make its link-card image.

    python3 marketing/tools/site_images.py [--card desktop-today-light.png]

Copies each screenshot the page references (src="images/<name>") from marketing/screenshots/ to
site/solutions/smb-os-desktop/images/, and writes site/solutions/smb-os-desktop/og.png, the page's
og:image and twitter:image (1200x630, the size X shows as a large link card). If the UI delivered
marketing/screenshots/og-card.png at exactly 1200x630, that file is used as is; otherwise the --card
screenshot is cropped to 1.91:1 (a tall shot keeps its top, where the window titles are) and resized.
Prints every image's size so the width/height attributes in the page can be checked against them.
Needs Pillow. Run scripts/firewall_scan.sh afterwards: the copies are read by OCR like the originals.
"""
import argparse
import os
import re
import shutil
import sys

ROOT = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
SRC = os.path.join(ROOT, "marketing", "screenshots")
DST = os.path.join(ROOT, "site", "solutions", "smb-os-desktop", "images")
CARD = os.path.join(ROOT, "site", "solutions", "smb-os-desktop", "og.png")
CARD_W, CARD_H = 1200, 630


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--card", default="desktop-today-light.png", help="screenshot to crop if og-card.png is absent")
    args = ap.parse_args()
    try:
        from PIL import Image
    except ImportError:
        sys.exit("site_images: needs Pillow (pip install pillow)")
    page = open(os.path.join(os.path.dirname(DST), "index.html"), encoding="utf-8").read()
    wanted = sorted(set(re.findall(r'src="images/([^"]+)"', page)))
    missing = [f for f in wanted if not os.path.isfile(os.path.join(SRC, f))]
    if missing:
        sys.exit("site_images: the page uses screenshots that do not exist yet: " + ", ".join(missing))
    os.makedirs(DST, exist_ok=True)
    for f in wanted:
        shutil.copy2(os.path.join(SRC, f), os.path.join(DST, f))
        with Image.open(os.path.join(DST, f)) as im:
            print("%-40s %dx%d" % ("images/" + f, im.width, im.height))
    ready = os.path.join(SRC, "og-card.png")
    if os.path.isfile(ready):
        with Image.open(ready) as im:
            if im.size != (CARD_W, CARD_H):
                sys.exit("site_images: og-card.png is %dx%d, not %dx%d" % (im.width, im.height, CARD_W, CARD_H))
        shutil.copy2(ready, CARD)
        print("%-40s %dx%d (og-card.png as delivered)" % ("og.png", CARD_W, CARD_H))
        return
    with Image.open(os.path.join(SRC, args.card)) as im:
        im = im.convert("RGB")
        want = CARD_W / CARD_H
        if im.width / im.height > want:  # too wide: trim both sides
            w = round(im.height * want)
            box = ((im.width - w) // 2, 0, (im.width - w) // 2 + w, im.height)
        else:  # too tall: keep the top
            box = (0, 0, im.width, round(im.width / want))
        im.crop(box).resize((CARD_W, CARD_H), Image.LANCZOS).save(CARD, optimize=True)
    print("%-40s %dx%d (cropped from %s; provisional until og-card.png arrives)" % ("og.png", CARD_W, CARD_H, args.card))


if __name__ == "__main__":
    main()
