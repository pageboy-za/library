#!/bin/sh
# Exercise built application with disposable data only; never accepts a data path.
set -eu
if [ "$#" -ne 0 ]; then
  printf '%s\n' 'Usage: ./deploy/offline-acceptance.sh (no arguments; disposable data only)' >&2
  exit 2
fi
library_acceptance_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
python3 - "$library_acceptance_root" <<'PY'
import contextlib, hashlib, http.server, json, os, pathlib, shutil, socket, struct, subprocess, sys, tempfile, threading, time, urllib.error, urllib.request, uuid, zlib
root = pathlib.Path(sys.argv[1]).resolve()
server_binary, backup_binary = root / "bin/library", root / "bin/library-backup"
for binary in (server_binary, backup_binary):
    if not binary.is_file() or not os.access(binary, os.X_OK):
        sys.exit(f"Missing built executable: {binary}; build locally first")
if not (root / "web/node_modules/astro/bin/astro.mjs").is_file():
    sys.exit("Installed Astro dependencies required; no dependencies are installed by this check")

# Production clients use Go DefaultTransport.ProxyFromEnvironment. This owned
# loopback proxy denies every request and never connects to an upstream server.
provider_attempts = []
class DenyProxy(http.server.BaseHTTPRequestHandler):
    def deny(self):
        provider_attempts.append((self.command, self.path))
        self.send_error(503, "Offline acceptance: external providers blocked")
    do_CONNECT = deny
    do_GET = deny
    do_POST = deny
    def log_message(self, *args): pass
proxy = http.server.ThreadingHTTPServer(("127.0.0.1", 0), DenyProxy)
proxy.daemon_threads = True
proxy_thread = threading.Thread(target=proxy.serve_forever, daemon=True)
proxy_thread.start()
proxy_url = f"http://127.0.0.1:{proxy.server_address[1]}"
environment = os.environ.copy()
for name in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"):
    environment[name] = proxy_url
for name in ("NO_PROXY", "no_proxy"):
    environment[name] = "127.0.0.1,localhost"
environment["ASTRO_TELEMETRY_DISABLED"] = "1"
environment.pop("LIBRARY_BOOK_SNAPSHOT", None)
client = urllib.request.build_opener(urllib.request.ProxyHandler({}))
owned = None
log = None
base = ""

def stop():
    global owned, log
    if owned is not None:
        if owned.poll() is None:
            owned.terminate()
            try: owned.wait(timeout=8)
            except subprocess.TimeoutExpired:
                owned.kill(); owned.wait(timeout=5)
        owned = None
    if log is not None:
        log.close(); log = None

def request(path, payload=None, method="GET", raw=None, content_type=None):
    headers = {"Origin": base}
    if payload is not None:
        raw = json.dumps(payload).encode(); content_type = "application/json"
    if content_type: headers["Content-Type"] = content_type
    req = urllib.request.Request(base + path, data=raw, method=method, headers=headers)
    try:
        with client.open(req, timeout=45) as response:
            data = response.read(2 << 20)
            if response.status not in (200, 201):
                raise AssertionError(f"{path}: unexpected status {response.status}: {data[:500]!r}")
            return json.loads(data) if "application/json" in response.headers.get("Content-Type", "") else data
    except urllib.error.HTTPError as error:
        raise AssertionError(f"{path}: HTTP {error.code}: {error.read(500)!r}") from error

def start(data, web, stage, label):
    global owned, log, base
    reservation = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    reservation.bind(("127.0.0.1", 0)); port = reservation.getsockname()[1]; reservation.close()
    base = f"http://127.0.0.1:{port}"
    log_path = stage / (label + ".log"); log = log_path.open("wb")
    owned = subprocess.Popen([str(server_binary), "-listen", f"127.0.0.1:{port}", "-data", str(data), "-web", str(web)], cwd=stage, env=environment, stdout=log, stderr=subprocess.STDOUT)
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        if owned.poll() is not None:
            raise AssertionError("Acceptance server exited: " + log_path.read_text()[-3000:])
        try:
            request("/healthz"); return
        except (urllib.error.URLError, TimeoutError): time.sleep(0.2)
    raise AssertionError("Acceptance server readiness timeout: " + log_path.read_text()[-3000:])

def key(): return str(uuid.uuid4())
def chunk(kind, data): return struct.pack(">I", len(data)) + kind + data + struct.pack(">I", zlib.crc32(kind + data) & 0xffffffff)
def fixture_png():
    pixels = b"".join(b"\x00" + b"\x40\x80\xc0" * 2 for _ in range(3))
    return b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 2, 3, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(pixels)) + chunk(b"IEND", b"")
def backup(command, *args):
    subprocess.run([str(backup_binary), command, *map(str, args)], cwd=root, env=environment, check=True, timeout=45)

try:
    with contextlib.ExitStack() as cleanup:
        directory = cleanup.enter_context(tempfile.TemporaryDirectory(prefix="library-offline-acceptance-"))
        cleanup.callback(stop)
        stage = pathlib.Path(directory); data = stage / "fresh-data"; web = stage / "web"
        web.mkdir()
        for name in ("package.json", "package-lock.json", "astro.config.mjs", "tsconfig.json"):
            shutil.copyfile(root / "web" / name, web / name)
        shutil.copytree(root / "web/src", web / "src")
        (web / "node_modules").symlink_to(root / "web/node_modules", target_is_directory=True)
        start(data, web, stage, "initial-server")
        began = time.monotonic(); unknown = request("/api/isbn/9780140328721")
        assert unknown.get("warning") and not unknown["draft"]["title"] and not unknown.get("existing"), "Offline lookup did not offer manual fallback"
        assert time.monotonic() - began < 10, "Offline provider failure was unexpectedly slow"
        assert any(method == "CONNECT" and host == "openlibrary.org:443" for method, host in provider_attempts), "Provider did not use the deny proxy"
        shelves = request("/api/shelves", {"bookcase":"Acceptance case", "name":"Acceptance shelf"}, "POST")
        shelf = next(item for item in shelves if item["name"] == "Acceptance shelf")
        book = request("/api/books", {"title":"Offline acceptance fixture", "authors":["Fixture Author"], "publisher":"Fixture Press", "publication_date":"2026", "shelf_id":shelf["id"], "request_key":key()}, "POST")
        assert b"kiosk-search" in request("/"), "Live shell is not compatible with updater readiness"
        book_id = book["id"]; assert book["count"] == 1 and book["isbn"] == "" and book["publication_date"] == "2026"
        boundary = "offline-fixture-" + uuid.uuid4().hex
        multipart = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"image\"; filename=\"fixture.png\"\r\nContent-Type: image/png\r\n\r\n".encode() + fixture_png() + f"\r\n--{boundary}--\r\n".encode())
        uploaded = request(f"/api/books/{book_id}/covers/upload", method="POST", raw=multipart, content_type="multipart/form-data; boundary=" + boundary)
        assert not uploaded.get("page_warning")
        book = request(f"/api/books/{book_id}"); cover_url = book["cover_url"]; assert cover_url.startswith("/covers/")
        cover_bytes = request(cover_url); cover_hash = hashlib.sha256(cover_bytes).hexdigest(); assert cover_bytes.startswith(b"\xff\xd8")
        book = request(f"/api/books/{book_id}", {"title":"Offline revised fixture", "authors":["Fixture Author"], "isbn":"9781857028898", "publisher":"Fixture Press", "publication_date":"2026", "revision":book["revision"]}, "PATCH")
        assert book["cover_url"] == cover_url
        for _ in range(2):
            scan = request("/api/isbn/9781857028898"); assert scan["existing"]["id"] == book_id and scan["existing"]["count"] == 1
        book = request(f"/api/books/{book_id}/copies", {"shelf_id":shelf["id"], "request_key":key()}, "POST"); assert book["count"] == 2
        borrower = request("/api/borrowers", {"name":"Fixture borrower", "notes":"Synthetic borrower", "request_key":key()}, "POST")
        html_before = request(f"/books/{book_id}/"); assert b"astro-island" in html_before
        current = request(f"/api/books/{book_id}"); assert current["title"] == "Offline revised fixture" and current["publication_date"] == "2026"
        loan = request("/api/loans", {"copy_id":book["copies"][0]["id"], "borrower_id":borrower["id"], "due_date":"", "request_key":key()}, "POST")
        checked = request(f"/api/books/{book_id}"); assert checked["available"] == 1 and checked["copies"][0]["out"] and checked["copies"][0]["shelf_id"] == shelf["id"]
        assert request(f"/books/{book_id}/") == html_before, "Lending changed the reusable book shell"
        discovery = request(f"/api/books/{book_id}/discovery")
        discovery.update(description="Synthetic public description", subjects=["Offline acceptance"], notes="PRIVATE_FIXTURE_NOTE_43", tags=["private fixture"])
        saved = request(f"/api/books/{book_id}/discovery", discovery, "PATCH"); assert not saved.get("page_warning")
        html = request(f"/books/{book_id}/"); assert b"PRIVATE_FIXTURE_NOTE_43" not in html
        assert request(f"/api/books/{book_id}/discovery")["description"] == "Synthetic public description"
        assert request("/api/books?q=Fixture%20Author")["total"] == 1
        archive = stage / "verified-backup"; restored = stage / "restored-data"
        backup("create", "-data", data, "-out", archive); backup("verify", "-backup", archive)
        stop(); backup("restore", "-backup", archive, "-out", restored)
        start(restored, web, stage, "restored-server")
        recovered = request(f"/api/books/{book_id}")
        assert recovered["count"] == 2 and recovered["available"] == 1 and recovered["cover_url"] == cover_url
        assert hashlib.sha256(request(cover_url)).hexdigest() == cover_hash
        assert request(f"/api/books/{book_id}/discovery")["notes"] == "PRIVATE_FIXTURE_NOTE_43"
        assert b"astro-island" in request(f"/books/{book_id}/")
        assert request(f"/api/books/{book_id}/discovery")["description"] == "Synthetic public description"
        assert request("/api/isbn/9781857028898")["existing"]["count"] == 2
        request(f"/api/loans/{loan['id']}/return", {"request_key":key()}, "POST")
        assert request(f"/api/books/{book_id}")["available"] == 2
        stop()
        print("PASS: real app offline lookup fallback, manual/year-only entry, local cover, ISBN rescans, explicit copies, shelves, checkout/return, live metadata/reusable islands, private-note exclusion, consistent backup/verify/restore and offline restart")
        print("Disposable data removed. No household paths, downloads, service installation or external provider requests.")
finally:
    stop(); proxy.shutdown(); proxy.server_close(); proxy_thread.join(timeout=3)
PY
