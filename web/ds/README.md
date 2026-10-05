# Apex design system 2.0 (vendored)

The one style source for the SMB OS Desktop page: paper ground, ink type, one signal colour. Dark slabs mark
what sits under the boundary (the dock, code, raw instructions). Instruments are flat, bordered and typed;
nothing glows. Forked with the desktop it styles; token values are unchanged from the source, only comments
were neutralised.

- **Calibrated** 2026-10-04: light is the source theme; a dark "night desk" is derived from the slab.
  `python3 contrast_check.py tokens.css` measures every text pair (>= 4.5:1) and every edge (>= 3:1) in both
  themes, with a negative control that must fail. It passes 46/46.
- **No third-party host is ever contacted.** The fonts ship here; the page's CSP is `default-src 'self'`.

## Files

| File | What it is |
|---|---|
| `tokens.css` | Every token, light + dark (`prefers-color-scheme`, overridable with `data-theme="light"`/`"dark"` on `<html>`) |
| `fonts.css` + `fonts/` | Geist (variable) and IBM Plex Mono 400/500/600, self-hosted, SIL OFL 1.1 (licences beside the files) |
| `components.css` | Every component, all `wx-*` classes |
| `tokens.json` | The same tokens as data |
| `contrast_check.py` | The ruler. A colour change ships only when it passes |

## Rules the desktop follows

- **Four result words.** HIT, MISS, REFUSED, NO_SIGNAL. A probe that could not see the thing reports NO_SIGNAL,
  never absence, and never an invented number.
- **One signal.** `--wx-signal` is reserved for tier 2 (money, outward, irreversible), the stop control and the one
  recommended action. Text on it is `--wx-on-signal`.
- **Evidence colours** always sit beside their word: blue = reported / tier 1 / pass, lime = verified / on,
  dashed quiet edge = unreported / NO_SIGNAL / unknown. A number is ink; its tag carries the evidence.
- **Edges.** Rest border 1px `--wx-hair`. Active state is a heavier ink border (1.5px), never a glow or a fill.
- **Type.** Geist for statements and labels; IBM Plex Mono for evidence: hashes, tiers, timestamps, paths.
  Sentence case; upper case only for result words, in the mono face.
- **Reach.** Touch targets at least `--wx-tap` (44px); focus is a 2px ink outline; motion is off when the
  operator asks for reduced motion.

Change a colour here, run the ruler, and only then use it.
