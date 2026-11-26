#!/usr/bin/env bash
# Copy one OS family of `go tool mygo build` outputs into dist/.
# Usage: scripts/collect-dist.sh linux|windows|macos
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

kind=${1:?usage: scripts/collect-dist.sh linux|windows|macos}
if command -v jq >/dev/null 2>&1; then
  version=$(jq -r '.version // empty' mygo.json)
else
  version=$(awk -F'"' '/"version"[[:space:]]*:/ {print $4; exit}' mygo.json)
fi
if [[ -z "$version" ]]; then
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

case "$kind" in
linux)
  for arch in amd64 arm64; do
    dir="build/linux-${arch}"
    if [[ -f "$dir/tulai-${version}-linux-${arch}.tar.gz" ]]; then
      cp "$dir/tulai-${version}-linux-${arch}.tar.gz" "dist/tulai-${version}-linux-${arch}.tar.gz"
    else
      require "$dir/tulai"
      tar -C "$dir" -czf "dist/tulai-${version}-linux-${arch}.tar.gz" tulai
    fi
    if [[ -f "$dir/tulai_${version}_${arch}.deb" ]]; then
      cp "$dir/tulai_${version}_${arch}.deb" "dist/tulai_${version}_${arch}.deb"
    fi
  done
  ;;
windows)
  for arch in amd64 arm64; do
    dir="build/windows-${arch}"
    setup="$dir/tulai-setup-${version}-${arch}.exe"
    exe="$dir/tulai.exe"
    require "$setup"
    require "$exe"
    cp "$setup" "dist/tulai Setup ${version} ${arch}.exe"
    cp "$exe" "dist/tulai-${version}-windows-${arch}.exe"
  done
  ;;
macos)
  dmg="build/darwin-arm64/tulai-${version}-arm64.dmg"
  if [[ ! -f "$dmg" ]]; then
    dmg=$(find build -maxdepth 2 -name "tulai-${version}*.dmg" | head -n1 || true)
  fi
  require "$dmg"
  cp "$dmg" "dist/tulai ${version}.dmg"
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

if find dist \( -iname '*chrome-for-testing*' -o -iname '*ungoogled-chromium*' -o -iname '*fingerprint-chromium*' -o -name 'chrome-linux64.zip' -o -name 'chrome-linux-arm64.zip' -o -name 'chrome-mac-arm64.zip' -o -name 'chrome-mac-x64.zip' -o -name 'chrome-win64.zip' \) | grep -q .; then
  echo "Managed browser binaries must not be bundled in the installer" >&2
  exit 1
fi

echo "collected $kind ($version):"
ls -lh dist
