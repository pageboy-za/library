#!/bin/sh
set -eu
library_version=$1
library_output=$2
library_commit=$(git rev-parse HEAD)
library_root=$(pwd)
case "$library_output" in /*) ;; *) echo 'Use an absolute output path' >&2;exit 1;;esac
mkdir -p "$library_output/runtime-root/bin" "$library_output/runtime-root/web" "$library_output/runtime-root/runtime"
CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=$library_version -X main.commit=$library_commit" -o "$library_output/runtime-root/bin/library" ./cmd/library
CGO_ENABLED=0 go build -trimpath -o "$library_output/runtime-root/bin/library-backup" ./cmd/library-backup
cp -R web/src "$library_output/runtime-root/web/"
cp web/package.json web/package-lock.json web/astro.config.mjs web/tsconfig.json web/*_LICENSE "$library_output/runtime-root/web/"
cp -R web/node_modules "$library_output/runtime-root/web/"
rm -rf "$library_output/runtime-root/web/node_modules/.vite" "$library_output/runtime-root/web/node_modules/.astro"
curl --fail --location --silent --show-error https://nodejs.org/dist/v24.22.0/node-v24.22.0-linux-arm64.tar.xz -o "$library_output/node.tar.xz"
curl --fail --location --silent --show-error https://nodejs.org/dist/v24.22.0/SHASUMS256.txt -o "$library_output/node-checksums.txt"
python3 - "$library_output" <<'PY'
import pathlib,hashlib,sys
p=pathlib.Path(sys.argv[1]);name='node-v24.22.0-linux-arm64.tar.xz'
expected=next(x.split()[0] for x in (p/'node-checksums.txt').read_text().splitlines() if x.endswith(name))
assert hashlib.sha256((p/'node.tar.xz').read_bytes()).hexdigest()==expected
PY
# Astro's installed CLI needs Node itself, not npm, headers or the SDK.
# Keep the upstream licence beside the checksum-verified runtime executable.
tar -xJf "$library_output/node.tar.xz" --strip-components=1 -C "$library_output/runtime-root/runtime" node-v24.22.0-linux-arm64/bin/node node-v24.22.0-linux-arm64/LICENSE
python3 deploy/build-release.py --root "$library_output/runtime-root" --out "$library_output/assets" --version "$library_version" --commit "$library_commit"
