#!/usr/bin/env bash
# check_links.sh: does every public URL in the given files answer?
#
#   marketing/tools/check_links.sh README.md marketing/RELEASE_NOTES_v0.1.0.md site/
#
# Collects every http(s) URL from the files (directories are searched), fetches each once with
# redirects followed (only the first byte of a download), and prints `status  url`. Run it from
# outside the network the release was built on; save the output with the date and where it ran.
# Links to x.com are listed as MANUAL: X answers scripts differently from browsers, so open them.
# Exit: 0 = every URL answered 200/206 · 1 = at least one did not · 2 = bad arguments.
set -uo pipefail
[ "$#" -ge 1 ] || { echo "usage: $0 <file-or-dir>..." >&2; exit 2; }

urls=$(grep -rhoE "https?://[^][[:space:]\"'<>()\`]+" "$@" 2>/dev/null \
  | sed -E 's/[.,;:!?]+$//' | grep -vE '\{\{|example\.(com|org|net)|127\.0\.0\.1|localhost' | sort -u)
[ -n "$urls" ] || { echo "no URLs found"; exit 0; }

bad=0; n=0
while IFS= read -r u; do
  n=$((n + 1))
  case "$u" in
    https://x.com/*|https://twitter.com/*) printf 'MANUAL  %s\n' "$u"; continue ;;
  esac
  code=$(curl -sS -L -r 0-0 -o /dev/null --max-time 30 -A "check_links/1 (+https://waynecolt.com/)" \
         -w '%{http_code}' "$u" 2>/dev/null || echo 000)
  printf '%s     %s\n' "$code" "$u"
  case "$code" in 200|206) ;; *) bad=$((bad + 1)) ;; esac
done <<< "$urls"

if [ "$bad" -eq 0 ]; then echo "check_links: GREEN · $n URLs" >&2; exit 0; fi
echo "check_links: RED · $bad of $n URLs did not answer 200" >&2; exit 1
