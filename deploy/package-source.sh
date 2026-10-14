#!/bin/sh
# Build a source-only archive. No dependency installation, upload, or data access.
set -eu
if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  printf '%s\n' 'Usage: deploy/package-source.sh OUTPUT.tar.gz [SOURCE_DIRECTORY]' >&2
  exit 2
fi
library_package_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
python3 - "$1" "${2:-$library_package_root}" <<'PY'
import gzip, hashlib, io, json, os, pathlib, sys, tarfile, tempfile

output = pathlib.Path(sys.argv[1]).absolute()
source = pathlib.Path(sys.argv[2]).resolve(strict=True)
if not source.is_dir():
    sys.exit("Source must be a directory")
if not output.name.endswith(".tar.gz"):
    sys.exit("Output must end with .tar.gz")
parent = output.parent.resolve(strict=True)
output = parent / output.name
if output.exists() or output.is_symlink():
    sys.exit("Refusing to replace an existing output")
if parent == source or source in parent.parents:
    sys.exit("Choose an output outside the source directory")

# Only known source types and selected documents are eligible. Never recurse
# through the project root, data, dependencies, build output, or dot directories.
selected = set()
required = ["go.mod", "go.sum", "internal/store/schema.sql", "cmd/library/main.go",
            "cmd/library-backup/main.go", "cmd/library-mcp/main.go",
            "web/package.json", "web/package-lock.json", "web/astro.config.mjs",
            "web/tsconfig.json", "web/src/pages/books/[id].astro",
            "deploy/build-pi.sh", "deploy/home-library.service", "PI_README.md"]
roots = {"cmd": {".go"}, "internal": {".go", ".sql"},
         "web/src": {".astro", ".svelte", ".ts", ".js", ".css", ".svg", ".woff2"}}
explicit = required + ["README.md", "LIBRARY_PLAN.md", "MULTIFORMAT_PLAN.md",
    "BACKUP_README.md", "MCP_README.md", "RELEASE_README.md", "ER_DIAGRAM.md",
    "ER_DIAGRAM.mmd", "ER_DIAGRAM.png", "web/LUMOS.md", "web/LUMOS_LICENSE",
    "deploy/start-kiosk.sh", "deploy/home-library-kiosk.desktop",
    "deploy/backup-run.sh", "deploy/backup-test.sh",
    "deploy/backup-home-library.service.example", "deploy/backup-home-library.timer.example",
    "deploy/backfill-subtitles.py", "deploy/backfill-subtitles-test.py", "SUBTITLES_README.md", "deploy/package-source.sh", "deploy/package-test.sh", "deploy/offline-acceptance.sh",
    "OFFLINE_ACCEPTANCE.md", "RELEASE_ACCEPTANCE.md", "DESIGN.md", "DESIGN_REVIEW.md", "DESIGN_DIRECTIONS.md",
    "web/NEWSREADER_LICENSE", "web/INTER_LICENSE", "SECURITY.md", "LAN_README.md",
    "deploy/Caddyfile.lan.example", "deploy/home-library-gateway.service",
    "deploy/install-lan.sh", "deploy/gateway-test.py", "REMOTE_UPDATES_PLAN.md", "UPDATES_README.md",
    "deploy/build-release.py", "deploy/prepare-release.sh", "deploy/check-page-version.py", "deploy/updates_common.py",
    "deploy/update-api.py", "deploy/update-activate.py", "deploy/update-test.py",
    "deploy/bootstrap-updater.py", "deploy/start-managed-library.sh",
    "deploy/home-library-update-api.service", "deploy/home-library-update.service",
    "deploy/home-library-update.path"]

def add(relative):
    path = source / relative
    for component in [path, *path.parents]:
        if component == source:
            break
        if component.is_symlink():
            sys.exit(f"Refusing a symbolic link in selected source: {relative}")
    if not path.is_file():
        sys.exit(f"Selected source is not a regular file: {relative}")
    selected.add(path.relative_to(source).as_posix())

for relative in required:
    add(relative)
for relative in explicit:
    if (source / relative).exists() or (source / relative).is_symlink():
        add(relative)
for relative, extensions in roots.items():
    base = source / relative
    if base.is_symlink():
        sys.exit(f"Refusing a source root symbolic link: {relative}")
    for directory, directories, files in os.walk(base, followlinks=False):
        directories[:] = sorted(d for d in directories if not d.startswith(".") and
                                d not in {"node_modules", "data", "bin", "dist", "cache", "vendor"})
        for child in directories:
            if (pathlib.Path(directory) / child).is_symlink():
                sys.exit(f"Refusing a source directory symbolic link: {pathlib.Path(directory) / child}")
        for filename in sorted(files):
            if filename.startswith(".") or pathlib.Path(filename).suffix not in extensions:
                continue
            add((pathlib.Path(directory) / filename).relative_to(source))

prefix = "home-library-source/"
checksums = []
entries = []
# Read each allowed file once: checksums describe exactly the archived bytes.
for relative in sorted(selected):
    data = (source / relative).read_bytes()
    mode = 0o755 if relative.startswith("deploy/") and relative.endswith(".sh") else 0o644
    checksums.append(hashlib.sha256(data).hexdigest() + "  " + relative + "\n")
    entries.append((relative, data, mode))
entries.append(("SHA256SUMS", "".join(checksums).encode(), 0o644))
entries.append(("release-manifest.json", (json.dumps({"format": 1, "kind": "source-only",
    "target": "linux-arm64", "files": sorted(selected),
    "dependencies_included": False, "household_data_included": False}, indent=2) + "\n").encode(), 0o644))

temporary = None
try:
    with tempfile.NamedTemporaryFile(prefix=".library-source-", suffix=".tmp", dir=parent, delete=False) as raw:
        temporary = pathlib.Path(raw.name)
        with gzip.GzipFile(fileobj=raw, mode="wb", filename="", mtime=0) as compressed:
            with tarfile.open(fileobj=compressed, mode="w", format=tarfile.PAX_FORMAT) as archive:
                for relative, data, mode in entries:
                    info = tarfile.TarInfo(prefix + relative)
                    info.size, info.mode, info.mtime = len(data), mode, 0
                    info.uid, info.gid, info.uname, info.gname = 0, 0, "", ""
                    archive.addfile(info, io.BytesIO(data))
        raw.flush()
        os.fsync(raw.fileno())
    # Same-directory hard link publishes atomically and never overwrites output,
    # including a file created by another process after the initial check.
    os.link(temporary, output)
    temporary.unlink()
    temporary = None
    print(f"Created source archive: {output}")
    print(f"SHA-256: {hashlib.sha256(output.read_bytes()).hexdigest()}")
    print(f"Source files: {len(selected)}; household data and installed dependencies excluded")
finally:
    if temporary is not None:
        temporary.unlink(missing_ok=True)
PY
