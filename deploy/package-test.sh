#!/bin/sh
# Disposable packaging acceptance checks; never opens the household database.
set -eu
library_package_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
python3 - "$library_package_root" <<'PY'
import hashlib, json, os, pathlib, subprocess, sys, tarfile, tempfile
root = pathlib.Path(sys.argv[1])
builder = root / "deploy/package-source.sh"

def run(output, source, success=True):
    result = subprocess.run([str(builder), str(output), str(source)], capture_output=True, text=True)
    if (result.returncode == 0) != success:
        raise AssertionError(result.stdout + result.stderr)
    return result

def inspect(path):
    with tarfile.open(path, "r:gz") as archive:
        members = archive.getmembers()
        names = [member.name for member in members]
        assert len(names) == len(set(names)), "Duplicate archive names"
        payload = {}
        for member in members:
            name = pathlib.PurePosixPath(member.name)
            assert not name.is_absolute() and ".." not in name.parts
            assert name.parts[0] == "home-library-source" and member.isfile()
            assert member.uid == 0 and member.gid == 0
            payload[name.relative_to("home-library-source").as_posix()] = archive.extractfile(member).read()
        manifest = json.loads(payload["release-manifest.json"])
        assert manifest["kind"] == "source-only" and not manifest["household_data_included"]
        assert not manifest["dependencies_included"]
        assert set(manifest["files"]) == set(payload) - {"SHA256SUMS", "release-manifest.json"}
        hashes = {}
        for line in payload["SHA256SUMS"].decode().splitlines():
            digest, relative = line.split("  ", 1)
            assert hashlib.sha256(payload[relative]).hexdigest() == digest, relative
            hashes[relative] = digest
        assert set(hashes) == set(manifest["files"])
        for required in ["go.mod", "go.sum", "cmd/library/main.go", "internal/store/schema.sql",
                         "web/package-lock.json", "web/src/pages/books/[id].astro",
                         "web/LUMOS_LICENSE", "deploy/build-pi.sh", "PI_README.md",
                         "deploy/offline-acceptance.sh", "OFFLINE_ACCEPTANCE.md", "RELEASE_ACCEPTANCE.md"]:
            assert required in payload, required
        assert all(member.mode == (0o755 if member.name.startswith("home-library-source/deploy/") and member.name.endswith(".sh") else 0o644) for member in members)
        return payload

with tempfile.TemporaryDirectory(prefix="library package test ") as directory:
    temporary = pathlib.Path(directory)
    initial = temporary / "source sample.tar.gz"
    run(initial, root)
    payload = inspect(initial)
    fixture = temporary / "source fixture with spaces"
    fixture.mkdir()
    for relative, contents in payload.items():
        if relative in {"SHA256SUMS", "release-manifest.json"}:
            continue
        destination = fixture / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_bytes(contents)
    excluded = ["data/library.sqlite", "data/covers/private.jpg", "data/pages/household.html",
        "bin/library", "web/node_modules/secret.js", "web/dist/index.html", "web/.astro/cache.json",
        "web/.book-builds/private.html", ".env", ".npmrc", ".git/config", "web/.env.local",
        "internal/credentials.json", "internal/private.key", "internal/.private/secret.go",
        "internal/cache/generated.go", "deploy/unknown-secret.sh", "private-household-notes.md"]
    private_fixture_marker = "EXCLUDED_" + "PRIVATE_FIXTURE_4159"
    for relative in excluded:
        path = fixture / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(private_fixture_marker + "\n")
    spaced = "internal/test fixtures/space name.go"
    (fixture / spaced).parent.mkdir(parents=True)
    (fixture / spaced).write_text("package fixture\n")
    archive = temporary / "verified source with spaces.tar.gz"
    run(archive, fixture)
    packaged = inspect(archive)
    assert spaced in packaged
    assert not set(excluded).intersection(packaged)
    assert not any(private_fixture_marker.encode() in contents for contents in packaged.values())
    original = archive.read_bytes()
    run(archive, fixture, success=False)
    assert archive.read_bytes() == original, "Existing archive changed"
    run(fixture / "inside.tar.gz", fixture, success=False)
    assert not (fixture / "inside.tar.gz").exists()
    duplicate = temporary / "reproducible.tar.gz"
    run(duplicate, fixture)
    assert duplicate.read_bytes() == original, "Archive is not reproducible"
    link = fixture / "internal/symlink.go"
    link.symlink_to(fixture / ".env")
    refused = temporary / "symlink.tar.gz"
    run(refused, fixture, success=False)
    assert not refused.exists(), "Symbolic-link archive was published"
    link.unlink()
    (fixture / "go.sum").unlink()
    run(temporary / "missing-required.tar.gz", fixture, success=False)
print("PASS: required source, checksums, safe paths, exclusions, spaces, reproducibility, no overwrite, outside-source output, symbolic links, missing required input")
PY
