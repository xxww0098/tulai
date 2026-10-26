#!/usr/bin/env bash
# 生成 internal/browser/cft_manifest.json（版本、sha256、压缩包内可执行文件路径）。
# Chrome for Testing 不打进安装包；用户在 tulai 里点下载后才落到
# <UserConfigDir>/xbs/runtime/cft/<版本>/。
# 用法: scripts/cft-manifest.sh [版本]
# 默认版本是 Chrome for Testing 的 Stable。需要能访问 npmmirror 或 storage.googleapis.com。
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
out="$root/internal/browser/cft_manifest.json"
version=${1:-}
if [[ -z "$version" ]]; then
  version=$(curl -fsSL --max-time 60 "https://googlechromelabs.github.io/chrome-for-testing/last-known-good-versions-with-downloads.json" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["channels"]["Stable"]["version"])')
fi

python3 - "$version" "$out" <<'PY'
import hashlib, json, sys, urllib.request

version, out = sys.argv[1], sys.argv[2]
mirrors = [
    "https://cdn.npmmirror.com/binaries/chrome-for-testing",
    "https://storage.googleapis.com/chrome-for-testing-public",
]
platforms = {
    "linux64": ("chrome-linux64.zip", "chrome-linux64/chrome"),
    "linux-arm64": ("chrome-linux-arm64.zip", "chrome-linux-arm64/chrome"),
    "mac-arm64": (
        "chrome-mac-arm64.zip",
        "chrome-mac-arm64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
    ),
    "mac-x64": (
        "chrome-mac-x64.zip",
        "chrome-mac-x64/Google Chrome for Testing.app/Contents/MacOS/Google Chrome for Testing",
    ),
    "win64": ("chrome-win64.zip", "chrome-win64/chrome.exe"),
}

def sha256(url):
    req = urllib.request.Request(url, headers={"User-Agent": "xbs"})
    with urllib.request.urlopen(req, timeout=120) as resp:
        digest = hashlib.sha256()
        while True:
            chunk = resp.read(1024 * 1024)
            if not chunk:
                break
            digest.update(chunk)
        return digest.hexdigest()

manifest = {"version": version, "platforms": {}}
for platform, (filename, exe) in platforms.items():
    rel = f"{version}/{platform}/{filename}"
    last = None
    digest = None
    for base in mirrors:
        url = f"{base}/{rel}"
        try:
            digest = sha256(url)
            break
        except Exception as err:  # noqa: BLE001 — 换下一个镜像
            last = err
    if digest is None:
        raise SystemExit(f"{platform}: {last}")
    manifest["platforms"][platform] = {"file": filename, "sha256": digest, "exe": exe}
    print(platform, digest)

with open(out, "w", encoding="utf-8") as fh:
    json.dump(manifest, fh, indent=2, ensure_ascii=False)
    fh.write("\n")
print("wrote", out)
PY
