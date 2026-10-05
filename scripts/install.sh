#!/bin/sh
# SMB OS Desktop installer for macOS and Linux.
#
#   curl -fsSL https://github.com/wayneColt/smb-os-desktop/releases/latest/download/install.sh | sh
#
# Downloads the build for this computer, checks its SHA-256 against the
# release's SHA256SUMS.txt, installs it, and starts it. Nothing is installed
# if the checksum does not match. Arguments are passed on to aios, e.g.
#   curl -fsSL .../install.sh | sh -s -- --no-open
#
# Environment (all optional):
#   AIOS_BASE_URL     download from here instead of the latest GitHub release
#   AIOS_VERSION      install this version (e.g. 0.1.0) instead of the latest
#   AIOS_INSTALL_DIR  install into this folder
#   AIOS_NO_START=1   install only; don't start it
set -eu

REPO="wayneColt/smb-os-desktop"

say() { printf '%s\n' "$*"; }
die() {
  printf 'install: %s\n' "$*" >&2
  exit 1
}

if [ -n "${AIOS_BASE_URL:-}" ]; then
  BASE="${AIOS_BASE_URL%/}"
elif [ -n "${AIOS_VERSION:-}" ]; then
  BASE="https://github.com/$REPO/releases/download/v${AIOS_VERSION#v}"
else
  BASE="https://github.com/$REPO/releases/latest/download"
fi

os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) die "unsupported system '$os' (on Windows, use install.ps1)" ;;
esac
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) die "unsupported processor '$arch'" ;;
esac
# An Apple silicon Mac running this script under Rosetta reports x86_64.
if [ "$os" = darwin ] && [ "$arch" = amd64 ] &&
  [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = 1 ]; then
  arch=arm64
fi

fetch() { # fetch URL FILE
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 --proto '=https,http' -o "$2" "$1"
  elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$2" "$1"
  else
    die "curl or wget is needed to download"
  fi
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d ' ' -f 1
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | cut -d ' ' -f 1
  else
    die "sha256sum or shasum is needed to verify the download"
  fi
}

unpack() { # unpack ZIP DIR
  if command -v unzip >/dev/null 2>&1; then
    unzip -q -o "$1" -d "$2"
  elif command -v bsdtar >/dev/null 2>&1; then
    bsdtar -xf "$1" -C "$2"
  elif command -v python3 >/dev/null 2>&1; then
    python3 -m zipfile -e "$1" "$2"
  else
    die "unzip (or bsdtar or python3) is needed to unpack the download"
  fi
}

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t smb-os-desktop)"
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT TERM

say "Downloading the release checksums from $BASE ..."
fetch "$BASE/SHA256SUMS.txt" "$tmp/SHA256SUMS.txt" || die "could not download $BASE/SHA256SUMS.txt"
lines="$(grep -E "^[0-9a-f]{64}  smb-os-desktop_[0-9A-Za-z.+-]+_${os}_${arch}\.zip\$" "$tmp/SHA256SUMS.txt" || true)"
[ -n "$lines" ] || die "this release has no build for $os/$arch"
[ "$(printf '%s\n' "$lines" | wc -l | tr -d ' ')" = 1 ] || die "SHA256SUMS.txt lists more than one build for $os/$arch"
want="${lines%%  *}"
zip="${lines#*  }"

say "Downloading $zip ..."
fetch "$BASE/$zip" "$tmp/$zip" || die "could not download $BASE/$zip"
got="$(sha256_of "$tmp/$zip")"
if [ "$got" != "$want" ]; then
  die "checksum mismatch for $zip: expected $want, got $got. Nothing was installed."
fi
say "Checksum verified: $got"

if [ -n "${AIOS_INSTALL_DIR:-}" ]; then
  dest="$AIOS_INSTALL_DIR"
elif [ "$os" = darwin ]; then
  dest="$HOME/Applications/SMB OS Desktop"
else
  dest="${XDG_DATA_HOME:-$HOME/.local/share}/smb-os-desktop"
fi

mkdir -p "$tmp/unpacked" "$dest"
unpack "$tmp/$zip" "$tmp/unpacked"
[ -f "$tmp/unpacked/aios" ] || die "the download does not contain aios"
chmod 0755 "$tmp/unpacked/aios"
# Copy beside the old binary, then rename over it: a copy that is running
# keeps its open file and the new one takes effect on the next start.
cp "$tmp/unpacked/aios" "$dest/.aios.new"
mv -f "$dest/.aios.new" "$dest/aios"
if [ -f "$tmp/unpacked/README.txt" ]; then cp "$tmp/unpacked/README.txt" "$dest/README.txt"; fi
say "Installed $("$dest/aios" --version) to $dest"

rm -rf "$tmp"
trap - EXIT
if [ "${AIOS_NO_START:-}" = 1 ]; then
  say "Start it any time with: \"$dest/aios\""
  exit 0
fi
say "Starting SMB OS Desktop. Press Ctrl+C to stop it."
exec "$dest/aios" "$@"
