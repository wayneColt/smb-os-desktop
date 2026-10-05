# The marketing package

How a WayneColt product launch is announced, every time. It starts from a release that already shipped
with receipts and ends with one post a person approved, plus the receipt that the post went out. Nothing
in it posts itself.

This process adds to the site's existing conventions; it doesn't replace them:

- **waynecolt.com** is a static site. Its Solutions directory is generated from `solutions/manifest.json`;
  each entry is `{slug, title, description, date, type, path}`, with `type` one of `essay`, `article`,
  `paper`, `report`, `note`. The site's publisher regenerates the directory page and `sitemap.xml` from
  the manifest and runs its own term check before it writes anything. The publisher and its term list
  live outside every public repository.
- **Post receipt pages** live at `/posts/<YYYY-MM-DD>-<slug>/`, are listed newest first on `/posts/`, and
  end with a box of links where a reader can check each claim.
- **Posts to X** go out through one posting service that holds the account's credential. It takes text
  only (no media upload) and refuses any post that doesn't cite a live receipt page on waynecolt.com, so
  the receipt page has to be live before the post. Its client also refuses text over 280 raw characters,
  link spelled out in full, on top of X's own weighted limit (a link counts 23). An image reaches X only
  as the link card, from the cited page's `og:image` and `twitter:image`.

## Input

A shipped release and its receipts (`docs/RELEASE_PROCESS.md`, steps 0 to 7): the release URL and assets,
`SHA256SUMS.txt`, the clean-machine check, and the test names that back the product's claims. No release
receipts, no package.

## The package

| Part | File in this repo | Becomes |
|---|---|---|
| Release notes | `marketing/RELEASE_NOTES_vX.Y.Z.md` | the GitHub release text |
| Claims ledger | `marketing/CLAIMS.md` | the proof behind every sentence below |
| README hero | top of `README.md`: one-breath description, screenshot, download | the repository's front page |
| Screenshots | `marketing/screenshots/*.png`, alt text in `marketing/POST_X.md` | README, solutions page, X card |
| Solutions page | `site/solutions/<slug>/index.html` | `waynecolt.com/solutions/<slug>/` |
| Manifest entry | `site/solutions/manifest.entry.json` | one entry in the site's `solutions/manifest.json` |
| Post receipt page | `site/posts/<date>-<slug>/index.html` | `waynecolt.com/posts/<date>-<slug>/` |
| X post | `marketing/POST_X.md`: candidates, a recommendation, the alt text; the recommended text alone in `marketing/post.txt` (no trailing newline) | one post, approved word for word |
| Link card | `site/solutions/<slug>/og.png`, 1200×630, cropped from a screenshot | the picture X shows with the link |

Pages under `site/` are written in the site's own look: the Solutions page uses the solutions template
(stock background, Space Grotesk headings, IBM Plex Mono text, the six-colour bars) and the receipt page
uses the posts template (serif text, one receipt box). They must read well at phone width: most people
who tap a link on X are on a phone, so a page for a desktop download says "download on your computer"
there instead of offering a file the phone can't run.

## The gates, in order

Each gate passes before the next one starts. A gate that fails sends the package back; it isn't waived.

**1. Claims have receipts.** Every sentence that says something about the product is in
`marketing/CLAIMS.md` with its proof: a test that passes on the tagged commit, a file in the tagged tree,
or a URL that answers. A claim without proof is cut, or rewritten as something the release does not do
yet. Numbers carry their unit and the conditions they were measured under.
*Check:* no row in the ledger is unchecked, and every claim in the package text can be found in it.

**2. Voice.** Plain, specific, first person singular when a person is speaking. Say what the product
does, then what that means for the reader, then the rule that follows. State what is not done yet in the
same place as what is. No hype words: `revolutionary`, `seamless`, `effortless`, `powerful`,
`game-changing`, `cutting-edge`, `best-in-class`, `unlock`, `supercharge`, `leverage`, `AI-powered`,
`simply`, `just`, `obviously`, `clearly`, `world-class`, `next-generation`.
*Check:*
`grep -niwE 'revolutionary|seamless(ly)?|effortless(ly)?|powerful|game-changing|cutting-edge|best-in-class|unlock|supercharge|leverage|ai-powered|simply|just|obviously|clearly|world-class|next-generation' README.md marketing/*.md site -r`
prints nothing (or only lines a person has read and kept on purpose).

**3. Firewall scan.** `scripts/firewall_scan.sh` over the repository, then again over the copied pages
in the site repository, as a full pass (the private term list present). Images are read by OCR when
tesseract is installed; otherwise a person checks each one by eye, including the browser chrome around
the screenshot.
A generic-pattern hit that is correct on purpose (a test proving a wrong port is refused) may carry an
inline `firewall:allow=<category>` comment on that line; the verdict counts every such allowance, and a
private-term hit can never be allowed inline.
*Check:* the verdict line says `GREEN` and `private term sources` (not `term list NOT applied`), and any
inline allowances it counts have been read by a person.

**4. Links tested from outside.** Every URL in the package answers from a vantage that is not the build
machine's network (a phone on cellular data, a hosted CI runner, any outside network): pages 200, GitHub
downloads 302 then 200. The solutions page's download button names the right system on Windows, a Mac
and Linux, and the page works at phone width.
*Check:* `marketing/tools/check_links.sh README.md marketing/RELEASE_NOTES_vX.Y.Z.md site/` from that
vantage, output saved with the date and where it ran.

**5. Staged.** Everything is in its final place and the only public parts are the ones the post needs:
the release, the solutions page and the receipt page. The post text sits in `marketing/post.txt`, exact
characters, no trailing newline. `python3 marketing/tools/xlen.py --file marketing/post.txt` must show
both counts within 280: X's weighted count (a link counts 23) and the raw count the posting client checks.
The page's `og:image` answers 200 and is 1200×630.
*Check:* the receipt page is live, its post link still reads `__TWEET_URL__`, both lengths fit, and
`sha256sum marketing/post.txt` is recorded.

**6. A person approves the exact post.** The owner of the account reads the exact text and says yes
to those characters: the approval names the SHA-256 of `marketing/post.txt`. Any edit changes the hash
and needs a new approval. Nothing posts on a timer or because an agent decided to.

## Who fires

The person who owns the account. Agents draft, check and stage; they don't post. After approval the
text goes out once through the posting service, which checks the cited receipt page again before it
posts.

## The receipt that comes back

- The post's URL (`https://x.com/<handle>/status/<id>`) and the time it was posted.
- The posted text, compared with `marketing/post.txt`: byte for byte identical (same SHA-256).
- The receipt page updated with the post's URL (replace every `__TWEET_URL__` with it; the link stays
  hidden until it is an `https://x.com/` address), committed and live.
- One line in `marketing/receipts/vX.Y.Z.md`: post URL, approval time, posted time.

Engagement is read later, from the post itself, and is not part of the launch receipt.

## Checklist

Copy this into `marketing/receipts/vX.Y.Z.md` and tick it as you go.

```text
INPUT
[ ] release vX.Y.Z live; RELEASE_PROCESS steps 0-7 each have a receipt

PACKAGE
[ ] RELEASE_NOTES_vX.Y.Z.md written; used as the release text
[ ] README hero: one-breath line, screenshot, download links for X.Y.Z
[ ] screenshots final; alt text written for each one used
[ ] site/solutions/<slug>/index.html: copy current; download links are latest/ (no per-release edit)
[ ] site/solutions/manifest.entry.json matches the page title and description
[ ] site/posts/<date>-<slug>/index.html written; its post link still reads __TWEET_URL__ (hidden)
[ ] POST_X.md: three candidates, both lengths, one recommended, alt text
[ ] post.txt = the recommended text, no trailing newline; sha256 recorded
[ ] og.png 1200x630 beside the solutions page; og:image + twitter:image absolute URLs

GATES
[ ] 1 claims: every claim in CLAIMS.md, every row ticked with its proof
[ ] 2 voice: hype-word grep empty (or each hit read and kept on purpose)
[ ] 3 firewall: GREEN, full pass, repo and copied site pages; images OCR'd or eyed
[ ] 4 links: check_links.sh GREEN from an outside vantage; button checked on 3 systems + a phone
[ ] 5 staged: release + solutions page + receipt page live; post.txt fits both counts; og.png answers 200
[ ] 6 approval: the account owner approved post.txt by its sha256 (time recorded)

FIRE
[ ] posted once through the posting service
[ ] posted text sha256 = approved sha256

RECEIPT
[ ] post URL added to the receipt page; page live
[ ] post URL, approval time and posted time recorded in marketing/receipts/vX.Y.Z.md
```
