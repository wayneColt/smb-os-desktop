#!/usr/bin/env bash
# Build the SMB OS Desktop release into dist/:
#   smb-os-desktop_<version>_<os>_<arch>.zip  (aios binary + README.txt) for
#   windows/amd64, darwin/arm64, darwin/amd64, linux/amd64, linux/arm64,
#   a version-less copy of each (smb-os-desktop_<os>_<arch>.zip) for stable
#   "latest" download links, plus install.sh, install.ps1 and SHA256SUMS.txt
#   covering all of them.
#
# Usage: scripts/build_release.sh VERSION      (e.g. 0.1.0 or v0.1.0)
#
# Builds are static (CGO_ENABLED=0) and reproducible: -trimpath, no VCS stamp,
# and zip entries carry a fixed timestamp (SOURCE_DATE_EPOCH, default: the
# last commit's time).
set -euo pipefail

VERSION="${1:-}"
VERSION="${VERSION#v}"
if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$ ]]; then
  echo "usage: $0 VERSION   (e.g. 0.1.0)" >&2
  exit 2
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FINAL="$ROOT/dist"
# Everything is built in a private directory and swapped into dist/ only when
# complete, so two builds running at once never mix their files and dist/ is
# never seen half-written.
WORK="$(mktemp -d "$ROOT/.dist-build.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT # after the swap below WORK no longer exists
DIST="$WORK"
STAGE="$DIST/.stage"
TARGETS=(windows/amd64 darwin/arm64 darwin/amd64 linux/amd64 linux/arm64)

for tool in go zip; do
  command -v "$tool" >/dev/null || { echo "build_release: $tool is required" >&2; exit 1; }
done
if command -v sha256sum >/dev/null; then SHA=(sha256sum); else SHA=(shasum -a 256); fi

export TZ=UTC
EPOCH="${SOURCE_DATE_EPOCH:-$(git -C "$ROOT" log -1 --format=%ct 2>/dev/null || true)}"
EPOCH="${EPOCH:-1767225600}" # 2026-01-01T00:00:00Z when there is no commit yet

stamp() { # give files a fixed modification time so zips are reproducible
  touch -d "@$EPOCH" "$@" 2>/dev/null || touch -t "$(date -u -r "$EPOCH" +%Y%m%d%H%M.%S)" "$@"
}

readme() { # readme OS BINARY
  local os="$1" bin="$2" start firstrun datadir
  case "$os" in
    windows)
      start="Double-click $bin."
      datadir='%AppData%\smb-os-desktop'
      firstrun='Windows may show "Windows protected your PC". This build is not code-signed
yet, so Windows cannot confirm who published it. Click "More info", then
"Run anyway". To check that the file is the one that was published, compare
its SHA-256 (PowerShell: Get-FileHash <zip file>) with SHA256SUMS.txt on the
release page.

The one-line PowerShell installer (install.ps1) does not trigger this prompt:
files downloaded from the command line are not marked as coming from the
internet. The installer checks the SHA-256 itself before installing.'
      ;;
    darwin)
      start="Double-click $bin. It opens in Terminal."
      datadir='~/Library/Application Support/smb-os-desktop'
      firstrun='macOS may say that "aios" cannot be opened because the developer cannot be
verified. This build is not yet signed or notarized by Apple, so macOS cannot
confirm who published it. Control-click (or right-click) aios, choose Open,
then choose Open again. On recent macOS versions: open System Settings >
Privacy & Security, scroll down and click "Open Anyway". To check that the
file is the one that was published, compare the output of
  shasum -a 256 <zip file>
with SHA256SUMS.txt on the release page.

The one-line installer (install.sh) does not trigger this prompt: files
downloaded from the command line are not marked as coming from the internet.
The installer checks the SHA-256 itself before installing.'
      ;;
    *)
      start="Open a terminal in this folder and run ./$bin"
      datadir='~/.config/smb-os-desktop'
      firstrun='If you see "permission denied", make the file executable first:
  chmod +x aios
To check that the file is the one that was published, compare the output of
  sha256sum <zip file>
with SHA256SUMS.txt on the release page.'
      ;;
  esac
  cat <<EOF
SMB OS Desktop $VERSION
$(printf '%*s' "$((15 + ${#VERSION}))" '' | tr ' ' '=')

An operator's desktop for a small multi-location franchise business. It runs
entirely on this computer: your data stays here, and nothing is sent anywhere
unless you turn on the optional AI model yourself (see the end of this file).

This copy comes with fictional demo data: two made-up franchise businesses.

START IT
  $start

WHAT YOU'LL SEE
  A terminal window that says
    SMB OS Desktop running at http://127.0.0.1:7701/ (Ctrl+C to stop)
  and your web browser opening the desktop at that address. If the browser
  does not open by itself, copy the address into it. The address only works
  on this computer; nobody else on your network can reach it. (If port 7701
  is taken, the next free port is used and shown instead.)

  Starting it a second time just opens the browser to the copy that is
  already running.

STOP IT
  Click the terminal window and press Ctrl+C, or close that window.

WHERE YOUR DATA LIVES
  $datadir
  receipts.jsonl  a tamper-evident record of everything the desktop did
  state.json      your saved drafts, matches and approvals
  outbox/         where approved actions are written; in this demo nothing
                  is ever sent anywhere
  Delete the folder to start over. To keep the data somewhere else, start it
  with: $bin --data <folder>

FIRST RUN
$(printf '%s\n' "$firstrun" | sed 's/^/  /')

OPTIONS
  --port N      serve on port N (default 7701; if busy, the next free one)
  --data DIR    keep your data in DIR
  --pack NAME   which demo business to show: auto or homecare
  --no-open     don't open the browser
  --version     print the version and exit

OPTIONAL AI MODEL
  Off by default. If the environment variable ANTHROPIC_API_KEY is set when
  the desktop starts, the Ask window sends your question and a summary of the
  business data to the model provider and shows its answer; each such
  question is recorded in receipts.jsonl. Without the key, Ask answers
  straight from your data and nothing leaves the computer.
EOF
}

mkdir -p "$STAGE"
cd "$ROOT"

for target in "${TARGETS[@]}"; do
  os="${target%/*}" arch="${target#*/}"
  bin="aios"
  [[ "$os" == windows ]] && bin="aios.exe"
  name="smb-os-desktop_${VERSION}_${os}_${arch}"
  dir="$STAGE/$name"
  mkdir -p "$dir"
  echo "building $os/$arch"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false \
    -ldflags "-s -w -X main.version=$VERSION" -o "$dir/$bin" ./cmd/aios
  readme "$os" "$bin" > "$dir/README.txt"
  chmod 0755 "$dir/$bin"
  chmod 0644 "$dir/README.txt"
  stamp "$dir/$bin" "$dir/README.txt"
  (cd "$dir" && zip -X -q -9 "$DIST/$name.zip" "$bin" README.txt)
  # A version-less copy, so a link to releases/latest/download/<name> keeps
  # pointing at the newest build. Same bytes, listed in SHA256SUMS.txt too.
  cp "$DIST/$name.zip" "$DIST/smb-os-desktop_${os}_${arch}.zip"
done

cp scripts/install.sh scripts/install.ps1 "$DIST/"
chmod 0755 "$DIST/install.sh"
rm -rf "$STAGE"

(cd "$DIST" && "${SHA[@]}" ./*.zip install.sh install.ps1 | sed 's#  \./#  #' > SHA256SUMS.txt)
(cd "$DIST" && "${SHA[@]}" -c --quiet SHA256SUMS.txt 2>/dev/null || "${SHA[@]}" -c SHA256SUMS.txt >/dev/null)

chmod 0755 "$DIST"
OLD="$(mktemp -d "$ROOT/.dist-old.XXXXXX")"
if [[ -e "$FINAL" ]]; then mv "$FINAL" "$OLD/dist"; fi
mv "$DIST" "$FINAL"
rm -rf "$OLD"
DIST="$FINAL"

echo
echo "dist/ ($VERSION):"
(cd "$DIST" && for f in *; do printf '  %10s  %s\n' "$(wc -c < "$f" | tr -d ' ')" "$f"; done)
echo
cat "$DIST/SHA256SUMS.txt"
