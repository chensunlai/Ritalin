#!/bin/sh
set -eu

repo="chensunlai/Ritalin"
install_dir="${RITALIN_INSTALL_DIR:-$HOME/.local/bin}"
version="${RITALIN_VERSION:-latest}"
download() {
    if command -v curl >/dev/null 2>&1; then
        curl --fail --location --silent --show-error --connect-timeout 15 --max-time 300 "$1" -o "$2"
    elif command -v wget >/dev/null 2>&1; then
        wget -q --timeout=60 -O "$2" "$1"
    else
        echo 'Please install curl or wget first.' >&2; exit 1
    fi
}
case "$(uname -s)" in
    Linux) platform=linux ;;
    Darwin) platform=darwin ;;
    *) echo 'Use install.ps1 on Windows.' >&2; exit 1 ;;
esac
case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    i386|i486|i586|i686) arch=386 ;;
    armv7*|armv8l) arch=armv7 ;;
    *) echo 'Unsupported CPU architecture.' >&2; exit 1 ;;
esac
if [ "$platform" = darwin ] && [ "$arch" != amd64 ] && [ "$arch" != arm64 ]; then
    echo 'macOS supports Intel x86_64 and Apple Silicon arm64.' >&2; exit 1
fi
ritalin_tmp=$(mktemp -d)
trap 'rm -rf -- "$ritalin_tmp"' EXIT HUP INT TERM
if [ "$version" = latest ]; then
    download "https://api.github.com/repos/$repo/releases/latest" "$ritalin_tmp/release.json"
    version=$(sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$ritalin_tmp/release.json" | head -n 1)
fi
case "$version" in
    ''|*[!a-zA-Z0-9._-]*) echo 'Invalid release version.' >&2; exit 1 ;;
esac
asset="codex-ritalin_${platform}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$version"
echo "Downloading Ritalin $version ($platform/$arch)..."
download "$base/$asset" "$ritalin_tmp/$asset"
tar -xzf "$ritalin_tmp/$asset" -C "$ritalin_tmp" codex-ritalin
[ -f "$ritalin_tmp/codex-ritalin" ] && [ ! -L "$ritalin_tmp/codex-ritalin" ] || exit 1
mkdir -p "$install_dir"
install -m 755 "$ritalin_tmp/codex-ritalin" "$install_dir/.codex-ritalin.new.$$"
mv -f "$install_dir/.codex-ritalin.new.$$" "$install_dir/codex-ritalin"
echo "Installed: $install_dir/codex-ritalin"
case ":$PATH:" in
    *":$install_dir:"*) echo 'Start: codex-ritalin dosing' ;;
    *) printf 'Add to your shell profile: export PATH="%s:$PATH"\n' "$install_dir"
       printf 'Start now: "%s/codex-ritalin" dosing\n' "$install_dir" ;;
esac
