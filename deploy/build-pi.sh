#!/bin/sh
# Build on the target 64-bit Pi. Does not install services or touch household data.
set -eu
library_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
if [ "$(uname -s)" != Linux ]; then
  echo 'Build this package on the target Linux Raspberry Pi.' >&2
  exit 1
fi
case "$(uname -m)" in
  aarch64|arm64) ;;
  *) echo 'This deployment package targets Raspberry Pi OS 64-bit (ARM64).' >&2; exit 1 ;;
esac
for library_tool in go node npm; do
  if ! command -v "$library_tool" >/dev/null 2>&1; then
    echo "Missing $library_tool. See PI_README.md." >&2
    exit 1
  fi
done
node -e 'const [major,minor]=process.versions.node.split(".").map(Number); if(major<22||(major===22&&minor<12)) { console.error("Node 22.12 or newer is required"); process.exit(1); }'
library_npm_version=$(npm --version)
node -e 'const [major,minor,patch]=process.argv[1].split(".").map(Number); if(major<9||(major===9&&(minor<6||(minor===6&&patch<5)))) { console.error("npm 9.6.5 or newer is required"); process.exit(1); }' "$library_npm_version"
cd "$library_root"
go version
node --version
npm --version
# Install exact frontend dependency versions from the checked-in lock file.
(cd web && npm ci && npm run check && npm run build)
go test ./...
mkdir -p bin
CGO_ENABLED=0 go build -trimpath -o bin/library ./cmd/library
CGO_ENABLED=0 go build -trimpath -o bin/library-backup ./cmd/library-backup
CGO_ENABLED=0 go build -trimpath -o bin/library-mcp ./cmd/library-mcp
{
  go version
  node --version
  npm --version
  sha256sum go.mod go.sum web/package-lock.json bin/library bin/library-backup bin/library-mcp
} > bin/build-info.txt
printf '%s\n' 'Built. Start ./bin/library -web web/dist from this directory. Node is not needed at runtime. See PI_README.md for installation.'
