#!/bin/sh
#
# Install the staqly binary (CLI + `staqly serve`) from the GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/staqly/staqly/main/scripts/install.sh | sh
#
# Environment:
#   STAQLY_VERSION       release tag, e.g. v0.2.0 (default: the latest release)
#   STAQLY_INSTALL_DIR   where to put the binary (default: ~/.local/bin)
#   STAQLY_DOWNLOAD_URL  base URL of the release assets (testing, mirrors)
#
# macOS and Linux (amd64, arm64). On Windows download the .zip from
# https://github.com/staqly/staqly/releases.

set -eu

repo=staqly/staqly
dir=${STAQLY_INSTALL_DIR:-$HOME/.local/bin}

fail() {
  echo "install.sh: $*" >&2
  exit 1
}

case $(uname -s) in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) fail "unsupported OS $(uname -s); download a release from https://github.com/$repo/releases" ;;
esac

case $(uname -m) in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) fail "unsupported CPU $(uname -m)" ;;
esac

if [ -n "${STAQLY_DOWNLOAD_URL:-}" ]; then
  base=$STAQLY_DOWNLOAD_URL
elif [ -n "${STAQLY_VERSION:-}" ]; then
  base=https://github.com/$repo/releases/download/$STAQLY_VERSION
else
  base=https://github.com/$repo/releases/latest/download
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  fail "need sha256sum or shasum to verify the download"
fi

archive=staqly_${os}_${arch}.tar.gz
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $base/$archive"
curl -fsSL -o "$tmp/$archive" "$base/$archive" || fail "download failed: $base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || fail "download failed: $base/checksums.txt"

want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || fail "$archive is not listed in checksums.txt"
[ "$(sha256 "$tmp/$archive")" = "$want" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" staqly
mkdir -p "$dir"
install -m 0755 "$tmp/staqly" "$dir/staqly"

echo "Installed $("$dir/staqly" version 2>/dev/null | head -n 1) to $dir/staqly"
case :$PATH: in
  *:"$dir":*) ;;
  *)
    # The startup file of the user's shell (zsh is the macOS default and ignores ~/.profile).
    case ${SHELL:-} in
      */zsh) rc=.zshrc ;;
      */bash) rc=.bashrc ;;
      *) rc=.profile ;;
    esac
    echo "Add $dir to your PATH, e.g.: echo 'export PATH=\"$dir:\$PATH\"' >> ~/$rc"
    ;;
esac
echo "Start staqly in the background (starts with your computer): staqly service install"
echo "Or just once in this terminal:                            staqly serve"
