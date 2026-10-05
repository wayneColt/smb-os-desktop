#!/usr/bin/env python3
"""xlen.py: weighted length of an X post, counted the way X counts it.

    python3 marketing/tools/xlen.py "post text"      # one post
    python3 marketing/tools/xlen.py --file POST.txt   # the whole file is one post

Rules (twitter-text v3): text is NFC-normalised; every http(s) URL counts 23; code points in
U+0000-U+10FF, U+2000-U+200D, U+2010-U+201F and U+2032-U+2037 count 1; everything else
(emoji, CJK) counts 2. The limit is 280.

It also prints the raw character count, because the posting path checks that too: its client refuses
any text longer than 280 characters before X's weighting is applied. So a post must fit both ways.
Exit 0 if the post fits both counts, 1 if it does not.
"""
import re
import sys
import unicodedata

URL = re.compile(r"https?://[^\s]+?(?=[.,;:!?)\]]*(?:\s|$))")
LIGHT = ((0x0000, 0x10FF), (0x2000, 0x200D), (0x2010, 0x201F), (0x2032, 0x2037))
LIMIT = 280


def weighted_length(text):
    text = unicodedata.normalize("NFC", text)
    urls = URL.findall(text)
    rest = URL.sub("", text)
    n = 23 * len(urls)
    for ch in rest:
        cp = ord(ch)
        n += 1 if any(lo <= cp <= hi for lo, hi in LIGHT) else 2
    return n, urls


def main(argv):
    if len(argv) == 3 and argv[1] == "--file":
        with open(argv[2], encoding="utf-8") as f:
            text = f.read().rstrip("\n")
    elif len(argv) == 2:
        text = argv[1]
    else:
        sys.stderr.write(__doc__)
        return 2
    n, urls = weighted_length(text)
    raw = len(unicodedata.normalize("NFC", text))
    ok = n <= LIMIT and raw <= LIMIT
    print("%d/%d weighted (%d link(s) at 23 each) · %d/%d raw characters%s"
          % (n, LIMIT, len(urls), raw, LIMIT, "" if ok else "  TOO LONG"))
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main(sys.argv))
