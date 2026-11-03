#!/usr/bin/env bash
# Copy one OS family of `go tool mygo build` outputs into dist/.
# Usage: scripts/collect-dist.sh linux|windows|macos
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

kind=${1:?usage: scripts/collect-dist.sh linux|windows|macos}
require_signed=${REQUIRE_SIGNED_UPDATES:-0}
version=$(jq -r .version mygo.json)
if [[ -z "$version" || "$version" == "null" ]]; then
  echo "mygo.json has no version" >&2
  exit 1
fi

rm -rf dist
mkdir -p dist

require() {
  if [[ ! -f "$1" ]]; then
    echo "missing $1" >&2
    exit 1
  fi
}

collect_update() {
  local target=$1 dir="build/$1" manifest="build/$1/update-$1.json"
  if [[ ! -f "$manifest" ]]; then
    if [[ "$require_signed" == 1 ]]; then
      echo "missing signed update manifest $manifest" >&2
      exit 1
    fi
    return
  fi
  require "$dir/tulai-$version-$target.tar.gz"
  cp "$manifest" dist/
  if [[ "$target" != linux-* ]]; then
    cp "$dir/tulai-$version-$target.tar.gz" dist/
  fi
}

case "$kind" in
linux)
  for arch in amd64 arm64; do
    dir="build/linux-$arch"
    require "$dir/tulai-$version-linux-$arch.tar.gz"
    require "$dir/tulai_${version}_${arch}.deb"
    cp "$dir/tulai-$version-linux-$arch.tar.gz" "$dir/tulai_${version}_${arch}.deb" dist/
    collect_update "linux-$arch"
  done
  require build/linux-amd64/install.sh
  require build/linux-arm64/install.sh
  if ! cmp -s build/linux-amd64/install.sh build/linux-arm64/install.sh; then
    echo "install.sh differs between linux architectures" >&2
    exit 1
  fi
  cp build/linux-amd64/install.sh dist/install.sh
  ;;
windows)
  for arch in amd64 arm64; do
    dir="build/windows-$arch"
    require "$dir/tulai-setup-$version-$arch.exe"
    require "$dir/tulai.exe"
    cp "$dir/tulai-setup-$version-$arch.exe" "dist/tulai Setup $version $arch.exe"
    cp "$dir/tulai.exe" "dist/tulai-$version-windows-$arch.exe"
    collect_update "windows-$arch"
  done
  ;;
macos)
  shopt -s nullglob
  require "build/darwin-universal/tulai-$version.dmg"
  cp "build/darwin-universal/tulai-$version.dmg" "dist/tulai $version.dmg"
  collect_update "darwin-universal"
  ;;
*)
  echo "unknown target $kind (want linux, windows, or macos)" >&2
  exit 1
  ;;
esac

(
  cd dist
  files=()
  while IFS= read -r line; do
    files+=("$line")
  done < <(find . -maxdepth 1 -type f -print | sed 's|^\./||' | LC_ALL=C sort)
  if [[ ${#files[@]} -eq 0 ]]; then
    echo "dist is empty" >&2
    exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${files[@]}" > SHA256SUMS.txt
  else
    shasum -a 256 "${files[@]}" > SHA256SUMS.txt
  fi
)

if find dist \( -iname '*chrome-for-testing*' -o -name 'chrome-linux64.zip' -o -name 'chrome-linux-arm64.zip' -o -name 'chrome-mac-arm64.zip' -o -name 'chrome-mac-x64.zip' -o -name 'chrome-win64.zip' \) | grep -q .; then
  echo "Chrome for Testing must not be bundled in the installer" >&2
  exit 1
fi

echo "collected $kind ($version):"
ls -lh dist
