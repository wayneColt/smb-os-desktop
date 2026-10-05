#!/usr/bin/env bash
# firewall_scan.sh: scan a tree for words and identifiers that must never ship in public bytes.
#
#   scripts/firewall_scan.sh [DIR]          DIR defaults to the repository this script lives in
#
# What it checks
#   1. A private term list, read from files that live OUTSIDE this repository and are never
#      copied into it. FIREWALL_TERMS_FILE holds one or more paths separated by ':'. Default:
#        ${XDG_CONFIG_HOME:-$HOME/.config}/waynecolt/firewall/SANITIZATION_RULES.md   (required)
#        ${XDG_CONFIG_HOME:-$HOME/.config}/waynecolt/firewall/waynecolt_publish.py    (if present)
#        ${XDG_CONFIG_HOME:-$HOME/.config}/waynecolt/firewall/extra_terms.txt         (if present)
#      Each file may be a markdown rules file (numbered table of `backticked` terms and/or a
#      `grep -E "..."` line), a Python file with a `..._TERMS = [...]` list, or a plain list
#      (one term per line, # comments). A source that yields no terms stops the scan (exit 2):
#      a format change must never turn into a silent pass.
#   2. Built-in generic patterns: private and link-local IPv4 addresses, email addresses other
#      than wayne@catalytic-computing.com (reserved example domains are allowed), phone-number
#      shapes (the fictional 555-01xx range is allowed), localhost/127.0.0.1 ports other than
#      7701 (and 0, "any free port"), internal hostnames (.lan .local .internal .home.arpa .ts.net), MAC addresses and
#      secret-shaped tokens.
#
# What it scans
#   Inside a git work tree: tracked + untracked files (ignored files are not pushed, so not
#   scanned). Elsewhere: every regular file under DIR except .git/. Binary files are skipped
#   and counted; images are read with tesseract when it is installed (FIREWALL_OCR=auto|1|0).
#
# Inline allowance (generic patterns only)
#   A line that carries `firewall:allow=<category>[,<category>]` (in a comment) does not report those
#   generic categories, e.g. a test that proves a wrong port is refused:
#       host: "127.0.0.1:9999"}, 403  // firewall:allow=localhost-port
#   Private-term hits can never be allowed inline. Every allowance is counted in the verdict line.
#   One built-in reading: a port-shaped number written as money ($8512, 8512.40) is not a port; each
#   such skip is counted in the verdict too. FIREWALL_SHOW_SKIPS=1 lists where every skip is (stderr).
#
# Output
#   One line per hit on stdout: `path:line:category` (`path:ocr:category` for images). The
#   matched text is never printed, so the output is safe to paste into a public log. A term
#   hit reads `firewall:<source>#<n>`: the n-th term of that private source.
#   A one-line verdict goes to stderr.
#
# Exit: 0 = GREEN (no hits) · 1 = RED (hits) · 2 = cannot scan (missing term list, bad args)
#
# FIREWALL_GENERIC_ONLY=1 runs the generic patterns alone (for CI, where the private list is
# not available). That run says so in its verdict; it is not a full pass.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
target="${1:-$(cd "$here/.." && pwd)}"
[ -d "$target" ] || { echo "firewall_scan: not a directory: $target" >&2; exit 2; }
command -v python3 >/dev/null 2>&1 || { echo "firewall_scan: needs python3 (3.8+)" >&2; exit 2; }

cfg="${XDG_CONFIG_HOME:-$HOME/.config}/waynecolt/firewall"
export FW_DEFAULT_REQUIRED="$cfg/SANITIZATION_RULES.md"
export FW_DEFAULT_OPTIONAL="$cfg/waynecolt_publish.py:$cfg/extra_terms.txt"

exec python3 - "$target" <<'PY'
import os, re, shutil, subprocess, sys


def _crash(kind, value, tb):
    # A crash must never read as RED (exit 1) or GREEN (exit 0).
    sys.stdout.flush()
    sys.stderr.write("firewall_scan: internal error (%s); no verdict\n" % kind.__name__)
    sys.stderr.flush()
    os._exit(2)


sys.excepthook = _crash

target = os.path.realpath(sys.argv[1])
ALNUM = re.compile(r"[A-Za-z0-9]")


def die(msg):
    sys.stderr.write("firewall_scan: %s\n" % msg)
    sys.exit(2)


def inside(path, root):
    path, root = os.path.realpath(path), os.path.realpath(root)
    return os.path.commonpath([path, root]) == root


def guarded(term):
    """Literal term, with word-edge guards where the term starts or ends alphanumeric."""
    body = re.escape(term).replace(r"\*", r"\S*")
    pre = r"(?<![A-Za-z0-9])" if ALNUM.match(term[0]) else ""
    post = r"(?![A-Za-z0-9])" if ALNUM.match(term[-1]) else ""
    return pre + body + post


def bash_dq_unescape(s):
    return re.sub(r'\\([$`"\\\n])', r"\1", s)


# ── term sources ──────────────────────────────────────────────────────────────────────────────
def load_source(path):
    """Return [(category, compiled)] for one private source. Never returns the terms themselves."""
    with open(path, encoding="utf-8", errors="replace") as f:
        text = f.read()
    tag = re.sub(r"[^a-z0-9]+", "-", os.path.splitext(os.path.basename(path))[0].lower()).strip("-")
    out = []

    # (a) grep -E "..." lines: the source's own single-pass pattern, kept as written
    greps = []
    for n, m in enumerate(re.finditer(r'grep\s+(-[A-Za-z]+)\s+"((?:[^"\\]|\\.)*)"', text), 1):
        flags, pat = m.group(1), bash_dq_unescape(m.group(2))
        if "E" not in flags:
            continue
        try:
            rx = re.compile(pat, re.I if "i" in flags else 0)
        except re.error:
            die("term source %s: grep pattern %d does not compile" % (tag, n))
        greps.append(rx)
        out.append(("firewall:%s#grep%d" % (tag, n), rx))

    # (b) numbered markdown table rows: every `backticked` term in the second column
    rows = re.findall(r"^\s*\|\s*(\d+)\s*\|([^|\n]*)\|", text, re.M)
    for num, cell in rows:
        for term in re.findall(r"`([^`]+)`", cell):
            term = term.strip()
            if not term:
                continue
            if any(g.search(term) for g in greps):
                continue  # the source's own grep already covers it, with its own word edges
            out.append(("firewall:%s#%s" % (tag, num), re.compile(guarded(term.rstrip("*")))))

    # (c) Python lists named *_TERMS = [...]  (case-insensitive, like the publisher that owns them)
    for m in re.finditer(r"\b[A-Z_]*TERMS\s*=\s*\[(.*?)\]", text, re.S):
        lits = re.findall(r'"((?:[^"\\]|\\.)*)"|\'((?:[^\'\\]|\\.)*)\'', m.group(1))
        for i, (a, b) in enumerate(lits, 1):
            term = (a or b).strip()
            if term:
                out.append(("firewall:%s#%d" % (tag, i), re.compile(guarded(term), re.I)))

    # (d) no structure found: a plain list, one term per line (case-insensitive)
    if not greps and not rows and not out:
        for i, line in enumerate(text.splitlines(), 1):
            term = line.strip()
            if term and not term.startswith("#"):
                out.append(("firewall:%s#%d" % (tag, i), re.compile(guarded(term), re.I)))

    if not out:
        die("term source %s yielded no terms (format changed?); refusing to call this a pass" % tag)
    return out


generic_only = os.environ.get("FIREWALL_GENERIC_ONLY") == "1"
term_matchers, sources = [], []
if not generic_only:
    explicit = os.environ.get("FIREWALL_TERMS_FILE")
    if explicit:
        paths = [p for p in explicit.split(":") if p]
        missing = [p for p in paths if not os.path.isfile(p)]
        if missing:
            die("FIREWALL_TERMS_FILE names %d file(s) that do not exist" % len(missing))
    else:
        req = os.environ["FW_DEFAULT_REQUIRED"]
        if not os.path.isfile(req):
            die("no term list at the default path (set FIREWALL_TERMS_FILE, or "
                "FIREWALL_GENERIC_ONLY=1 for a generic-only run)")
        paths = [req] + [p for p in os.environ["FW_DEFAULT_OPTIONAL"].split(":") if os.path.isfile(p)]
    for p in paths:
        if inside(p, target):
            die("a term source lives inside the scanned tree; term lists must stay outside the repo")
        term_matchers += load_source(p)
        sources.append(p)

# ── generic patterns ──────────────────────────────────────────────────────────────────────────
ALLOWED_EMAILS = {"wayne@catalytic-computing.com"}
RESERVED_DOMAIN = re.compile(r"(^|\.)(example\.(com|net|org)|[a-z0-9-]+\.(example|test|invalid|localhost))$", re.I)


def email_ok(s):
    s = s.lower()
    return s in ALLOWED_EMAILS or bool(RESERVED_DOMAIN.search(s.split("@", 1)[1]))


def phone_ok(s):
    d = re.sub(r"\D", "", s)
    return len(d) >= 7 and d[-7:-2] == "55501"  # 555-0100..0199: reserved for fiction


def port_ok(m):
    return m.group(1) in ("7701", "0")  # the product's port, and "pick any free port"


KEY = "-----BEGIN"  # split so this file does not match its own pattern
GENERIC = [
    ("private-ip", re.compile(
        r"(?<![0-9.])(?:10(?:\.\d{1,3}){3}|172\.(?:1[6-9]|2\d|3[01])(?:\.\d{1,3}){2}"
        r"|192\.168(?:\.\d{1,3}){2}|100\.(?:6[4-9]|[7-9]\d|1[01]\d|12[0-7])(?:\.\d{1,3}){2}"
        r"|169\.254(?:\.\d{1,3}){2})(?![0-9]|\.[0-9])"), None),
    ("email", re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z][A-Za-z0-9-]*(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}"),
     lambda m: email_ok(m.group(0))),
    ("phone", re.compile(
        r"(?<![\w.])(?:\+?1[\s.-]?)?(?:\(\d{3}\)\s?|\d{3}[\s.-])\d{3}[\s.-]\d{4}(?!\w)"
        r"|(?<![\w+])\+[2-9]\d{0,2}[\s.-]\d{1,4}(?:[\s.-]\d{2,4}){2,3}(?!\w)"),
     lambda m: phone_ok(m.group(0))),
    ("localhost-port", re.compile(
        r"(?i)(?<![\w.-])(?:localhost|127\.0\.0\.1|0\.0\.0\.0|\[::1\]):(\d{1,5})(?!\d)"), port_ok),
    ("hostname", re.compile(
        r"(?i)(?<![\w.-])[a-z0-9][a-z0-9-]*(?:\.[a-z0-9-]+)*\.(?:lan|local|internal|localdomain|home\.arpa|ts\.net)(?![\w-])"),
     None),
    ("device-id", re.compile(r"(?<![0-9A-Fa-f:])(?:[0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}(?![0-9A-Fa-f:])"), None),
    ("secret", re.compile("|".join([
        r"sk-ant-[A-Za-z0-9_-]{16,}", r"\bsk-[A-Za-z0-9]{32,}", r"\bgh[pousr]_[A-Za-z0-9]{30,}",
        r"\bgithub_pat_[A-Za-z0-9_]{30,}", r"\b(?:AKIA|ASIA)[0-9A-Z]{16}\b", r"\bAIza[0-9A-Za-z_-]{35}",
        r"\bxox[abposr]-[A-Za-z0-9-]{10,}", re.escape(KEY) + r"[A-Z ]*PRIVATE KEY",
        r"\bcfat_[A-Za-z0-9_-]{20,}", r"\bcfast_[A-Za-z0-9]{20,}", r"\bglpat-[A-Za-z0-9_-]{20,}",
        r"\bnpm_[A-Za-z0-9]{30,}", r"\bhf_[A-Za-z0-9]{30,}", r"\bgsk_[A-Za-z0-9]{40,}",
        r"\bxai-[A-Za-z0-9]{40,}", r"\b[rs]k_(?:live|test)_[A-Za-z0-9]{16,}", r"\bwhsec_[A-Za-z0-9]{20,}",
        r"\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}",
        r"(?i:\bbearer\s+[A-Za-z0-9._~+/=-]{24,})",
        r"(?i:(?:api[_-]?key|secret|token|passw(?:or)?d)[\"']?\s*[:=]\s*[\"'][^\"'\s]{16,}[\"'])",
    ])), None),
]
MATCHERS = term_matchers + [(c, rx) for c, rx, _ in GENERIC]
ALLOW = {c: fn for c, _, fn in GENERIC if fn}


GENERIC_CATS = {c for c, _, _ in GENERIC}
INLINE = re.compile(r"firewall:allow=([a-z,-]+)")
allowed_inline = [0]
money_skips = [0]


def is_money(line, m):
    """A purely numeric term hit that is written as an amount ($8512 or 8512.40) is money, not a port."""
    if not m.group(0).isdigit():
        return False
    before, after = line[:m.start()], line[m.end():]
    return before.rstrip().endswith("$") or re.match(r"\.\d{2}(?!\d)", after) is not None


def scan_lines(lines):
    """Yield (lineno, category) for every distinct hit; lineno is 1-based."""
    for n, line in enumerate(lines, 1):
        seen = set()
        m = INLINE.search(line)
        waived = {c for c in m.group(1).split(",") if c in GENERIC_CATS} if m else set()
        for cat, rx in MATCHERS:
            if cat in seen:
                continue
            ok = ALLOW.get(cat)
            for mt in rx.finditer(line):
                if cat.startswith("firewall:") and is_money(line, mt):
                    money_skips[0] += 1
                    yield n, "~money"
                    continue
                if ok is None or not ok(mt):
                    seen.add(cat)
                    if cat in waived:
                        allowed_inline[0] += 1
                        yield n, "~allowed:" + cat
                    else:
                        yield n, cat
                    break


# ── file list ─────────────────────────────────────────────────────────────────────────────────
def list_files(root):
    try:
        out = subprocess.run(["git", "ls-files", "-co", "--exclude-standard", "-z"], cwd=root,
                             capture_output=True, check=True).stdout
        if subprocess.run(["git", "rev-parse", "--is-inside-work-tree"], cwd=root,
                          capture_output=True, text=True).stdout.strip() == "true":
            return sorted({p for p in out.decode("utf-8", "replace").split("\0") if p})
    except (OSError, subprocess.CalledProcessError):
        pass
    found = []
    for dp, dns, fns in os.walk(root):
        dns[:] = [d for d in dns if d != ".git"]
        for fn in fns:
            found.append(os.path.relpath(os.path.join(dp, fn), root))
    return sorted(found)


IMAGE = re.compile(r"\.(png|jpe?g|gif|webp|bmp|tiff?)$", re.I)
ocr_mode = os.environ.get("FIREWALL_OCR", "auto")
tess = shutil.which("tesseract") if ocr_mode != "0" else None
if ocr_mode == "1" and not tess:
    die("FIREWALL_OCR=1 but tesseract is not installed")

hits, skips, scanned, binary, symlinks, ocrd, not_ocrd, too_big = [], [], 0, 0, 0, 0, 0, 0
for rel in list_files(target):
    path = os.path.join(target, rel)
    if os.path.islink(path):
        symlinks += 1
        continue
    if not os.path.isfile(path):
        continue  # listed by git but deleted in the work tree
    if os.path.getsize(path) > 20 * 1024 * 1024:
        too_big += 1
        continue
    with open(path, "rb") as f:
        data = f.read()
    if b"\0" in data[:4096]:
        binary += 1
        if IMAGE.search(rel):
            if tess:
                try:
                    txt = subprocess.run([tess, path, "-"], capture_output=True, text=True,
                                         timeout=120).stdout
                    ocrd += 1
                    for _, cat in scan_lines(txt.splitlines()):
                        (skips if cat[0] == "~" else hits).append((rel, "ocr", cat))
                except (OSError, subprocess.TimeoutExpired):
                    not_ocrd += 1
            else:
                not_ocrd += 1
        continue
    scanned += 1
    for n, cat in scan_lines(data.decode("utf-8", "replace").splitlines()):
        (skips if cat[0] == "~" else hits).append((rel, n, cat))

for rel, n, cat in sorted(set(hits), key=lambda h: (h[0], str(h[1]).zfill(8), h[2])):
    print("%s:%s:%s" % (rel, n, cat))
if os.environ.get("FIREWALL_SHOW_SKIPS") == "1":  # where the counted skips are, for the person reviewing them
    for rel, n, cat in sorted(set(skips), key=lambda h: (h[0], str(h[1]).zfill(8), h[2])):
        sys.stderr.write("skipped %s:%s:%s\n" % (rel, n, cat[1:]))

notes = ["%d text files scanned" % scanned, "%d binary skipped" % binary]
if ocrd or not_ocrd:
    notes.append("%d images OCR'd" % ocrd)
if not_ocrd:
    notes.append("%d images NOT read (no OCR): check them by eye" % not_ocrd)
if money_skips[0]:
    notes.append("%d number(s) read as money amounts, not ports" % money_skips[0])
if allowed_inline[0]:
    notes.append("%d generic hit(s) allowed inline (firewall:allow=)" % allowed_inline[0])
if symlinks:
    notes.append("%d symlinks skipped" % symlinks)
if too_big:
    notes.append("%d files over 20 MB skipped" % too_big)
if generic_only:
    notes.append("term list NOT applied (FIREWALL_GENERIC_ONLY=1): not a full pass")
else:
    notes.append("%d private term sources, %d matchers" % (len(sources), len(term_matchers)))
verdict = "RED %d hit(s)" % len(hits) if hits else "GREEN"
sys.stderr.write("firewall_scan: %s · %s\n" % (verdict, " · ".join(notes)))
sys.exit(1 if hits else 0)
PY
