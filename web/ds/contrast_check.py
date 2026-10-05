#!/usr/bin/env python3
"""Contrast ruler for the Apex design system tokens.css: reads both themes from the file itself."""
import re, sys
src = open(sys.argv[1] if len(sys.argv) > 1 else 'tokens.css').read()
def block(sel):
    m = re.search(re.escape(sel) + r'\s*\{(.*?)\n\}', src, re.S)
    return dict(re.findall(r'--(wx-[a-z0-9-]+):\s*(#[0-9A-Fa-f]{6})\b', m.group(1))) if m else {}
light = block(':root')
dark = {**light, **block(':root[data-theme="dark"]')}
def lum(h):
    c = [int(h[i:i+2], 16) / 255 for i in (1, 3, 5)]
    c = [x / 12.92 if x <= 0.04045 else ((x + 0.055) / 1.055) ** 2.4 for x in c]
    return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2]
def ratio(a, b):
    la, lb = sorted((lum(a), lum(b)), reverse=True)
    return (la + 0.05) / (lb + 0.05)
TEXT = [('ink','paper'),('ink','surface'),('ink','sheet'),('ink-2','paper'),('ink-2','surface'),('ink-2','sheet'),
        ('ink-3','paper'),('ink-3','surface'),('ink-3','sheet'),('slab-ink','slab'),('slab-ink-2','slab'),
        ('on-signal','signal'),('on-signal','signal-hover'),('blue-ink','blue'),('lime-ink','lime'),('ink','well'),('ink-2','well')]
EDGE = [('line-quiet','paper'),('line-quiet','surface'),('ink','paper'),('signal-deep','paper'),('blue-line','paper'),('lime-line','paper')]
fails = 0
for name, t in (('light', light), ('dark', dark)):
    for fg, bg, need in [(a, b, 4.5) for a, b in TEXT] + [(a, b, 3.0) for a, b in EDGE]:
        r = ratio(t['wx-' + fg], t['wx-' + bg]); ok = r >= need; fails += not ok
        print(f"{name:5} {fg:>12} on {bg:<12} {r:5.2f}:1  need {need}  {'PASS' if ok else 'FAIL'}")
# negative control: the source file's caption grey on paper must FAIL
ctrl = ratio('#7A7F85', light['wx-paper'])
print(f"control #7A7F85 on paper {ctrl:.2f}:1 -> {'FAIL (expected)' if ctrl < 4.5 else 'PASS (ruler broken)'}")
print("RESULT", "PASS" if fails == 0 and ctrl < 4.5 else f"FAIL {fails}")
sys.exit(0 if fails == 0 and ctrl < 4.5 else 1)
