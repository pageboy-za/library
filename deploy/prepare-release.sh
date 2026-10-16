#!/bin/sh
set -eu
library_version=$1
library_output=$2
library_commit=$(git rev-parse HEAD)
case "$library_output" in /*) ;; *) echo 'Use an absolute output path' >&2;exit 1;;esac
if [ -e "$library_output/runtime-root" ]; then
 echo 'Release runtime directory already exists; use a fresh output directory' >&2; exit 1
fi
mkdir -p "$library_output/runtime-root/bin" "$library_output/runtime-root/web"
CGO_ENABLED=0 go build -trimpath -ldflags "-X main.version=$library_version -X main.commit=$library_commit" -o "$library_output/runtime-root/bin/library" ./cmd/library
CGO_ENABLED=0 go build -trimpath -o "$library_output/runtime-root/bin/library-backup" ./cmd/library-backup
# Build once on the release machine. The installed application needs no Node runtime.
(cd web && LIBRARY_APP_VERSION="$library_version" npm run build)
cp -R web/dist/. "$library_output/runtime-root/web/"
cp LICENSE web/*_LICENSE "$library_output/runtime-root/web/"
python3 deploy/build-release.py --root "$library_output/runtime-root" --out "$library_output/assets" --version "$library_version" --commit "$library_commit"
