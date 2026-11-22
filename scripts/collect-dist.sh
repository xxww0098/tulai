#!/usr/bin/env bash
# Copy one OS family of `go tool nsgo build ./cmd/tulai` outputs into dist/.
# Usage: scripts/collect-dist.sh linux|windows|macos
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"

kind=${1:?usage: scripts/collect-dist.sh linux|windows|macos}
version=$(awk '/^version:[[:space:]]*/ {sub(/^version:[[:space:]]*/, ""); gsub(/["\047[:space:]]/, ""); print; exit}' cmd/tulai/nsgo.yaml)
if [[ -z "$version" ]]; then
  echo "cmd/tulai/nsgo.yaml has no version" >&2
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
  for arch_pair in "amd64:x64" "arm64:arm64"; do
    arch=${arch_pair%%:*}
    nsgo_arch=${arch_pair##*:}
    dir="build/tulai-${version}-linux_${nsgo_arch}"
    require "$dir/tulai"
    tar -C "$dir" -czf "dist/tulai-${version}-linux-${arch}.tar.gz" .
    if [[ -f "build/releases/tulai-${version}-linux-${nsgo_arch}.deb" ]]; then
      cp "build/releases/tulai-${version}-linux-${nsgo_arch}.deb" "dist/tulai_${version}_${arch}.deb"
    fi
  done
  if [[ -f build/releases/install.sh ]]; then
    cp build/releases/install.sh dist/install.sh
  fi
  ;;
windows)
  for arch_pair in "amd64:x64" "arm64:arm64"; do
    arch=${arch_pair%%:*}
    nsgo_arch=${arch_pair##*:}
    dir="build/tulai-${version}-windows_${nsgo_arch}"
    setup="$dir/tulai-${version}-windows-${nsgo_arch}-setup.exe"
    exe="$dir/tulai.exe"
    require "$setup"
    require "$exe"
    cp "$setup" "dist/tulai Setup ${version} ${arch}.exe"
    cp "$exe" "dist/tulai-${version}-windows-${arch}.exe"
  done
  ;;
macos)
  dmg="build/releases/tulai-${version}-macos-arm64.dmg"
  if [[ ! -f "$dmg" ]]; then
    dmg=$(find build/releases -maxdepth 1 -name "tulai-${version}-*.dmg" | head -n1 || true)
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

if find dist \( -iname '*chrome-for-testing*' -o -name 'chrome-linux64.zip' -o -name 'chrome-linux-arm64.zip' -o -name 'chrome-mac-arm64.zip' -o -name 'chrome-mac-x64.zip' -o -name 'chrome-win64.zip' \) | grep -q .; then
  echo "Chrome for Testing must not be bundled in the installer" >&2
  exit 1
fi

echo "collected $kind ($version):"
ls -lh dist
