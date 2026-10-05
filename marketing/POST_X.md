# X post: SMB OS Desktop 0.1.0

Staged text for gate 6 of [docs/MARKETING_PACKAGE.md](../docs/MARKETING_PACKAGE.md). Each candidate is
the exact characters to post; nothing here posts itself. The recommended text is also in
[`post.txt`](post.txt), with no trailing newline: the approval binds to that file's SHA-256, so any
edit to it needs a new approval.

**Two length rules, both must hold.** X counts a link as 23 characters (weighted limit 280). The posting
path's client also refuses any text over 280 raw characters, link spelled out in full. The link here is
47 characters, so the text around it has 233 at most. `python3 marketing/tools/xlen.py --file
marketing/post.txt` prints both counts.

The post carries no uploaded image; the posting path is text and link only. The picture reaches X as the
link card from the solutions page (`og:image` → `https://waynecolt.com/solutions/smb-os-desktop/og.png`,
1200×630), with the alt text below in `twitter:image:alt`.

## Candidate A (recommended): 254 weighted · 278 raw

```text
Business intelligence that runs on your computer, and answers to you. SMB OS Desktop 0.1 preps a franchise's brief, payroll and reconciliation; anything that moves money waits for your typed APPROVE. Try it on fictional demo data: https://waynecolt.com/solutions/smb-os-desktop/
```

Claims: P1, P2, A2, A3, A4, G3, G5, A12.

## Candidate B: 256 weighted · 280 raw

```text
Two planes, one authority: SMB OS Desktop's agents read, reconcile and draft with no authority; paying or sending waits for your typed APPROVE, bound to one exact action, used once. 0.1 runs on your computer, on fictional demo data: https://waynecolt.com/solutions/smb-os-desktop/
```

Claims: P3, P4, G3, G4, G5, G6, P1, A12.

## Candidate C: 237 weighted · 261 raw

```text
A franchise owner's morning on one screen: every location against target, payroll checked, bank reconciled. Business intelligence that runs on your computer, and answers to you. 0.1 ships with fictional demo data: https://waynecolt.com/solutions/smb-os-desktop/
```

Claims: A2, A3, A4, P1, P2, A12.

## Why A

A opens with the line the whole release is built on, then makes it concrete for the person it is for:
what the agents prepare (the brief, payroll, reconciliation), what they cannot do on their own (move
money without a typed APPROVE), and that the download runs on fictional demo data, so nobody expects it
to open their own books. Every phrase maps to a claim in `CLAIMS.md`.

B says the architecture most exactly ("two planes, one authority", one exact action, used once), but
it reads as engineering before the reader knows what the product is for. Keep it for the reply or a
follow-up post. C is the most readable, but it leaves out the gate, the one thing comparable tools
don't do.

## Alt text for the card image

For `site/solutions/smb-os-desktop/og.png`. It describes the UI's `og-card.png`, which
`marketing/tools/site_images.py` copies to `og.png`; re-read it against the final 0.1.0 image before
posting. It is also in the page's `twitter:image:alt`
and `og:image:alt`.

```text
The Today window of SMB OS Desktop for Kestrel Auto Care, a fictional auto service franchise: the morning brief headline, a line for each location, the measures off target worst first and KPI tiles against target, with desktop icons on the left and the dock along the bottom reading No approvals waiting and AI model off.
```

## Approval record

Fill this in when the account owner approves. The approval covers these exact characters only.

```text
approved file: marketing/post.txt
sha256:
approved at (UTC):
posted at (UTC):
post URL:
posted text identical to post.txt (yes/no):
```

## What went out (2026-10-05)
The posting path only accepts a link to a dated receipt page (`/posts/<date>-<slug>/`), so the first text, which
linked the solutions page, was refused and nothing was posted. `post.txt` now holds the text that went out:
the same message with the receipt-page link, "0.1" and one comma trimmed to stay within 280 raw characters
(249 weighted). Posted: https://x.com/wayne__colt/status/2107155554179088487
