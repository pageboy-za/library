#!/usr/bin/env python3
"""Exercise the actual Caddy LAN template against a disposable loopback backend."""
import base64
import http.server
import json
import os
import pathlib
import secrets
import socket
import ssl
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

root = pathlib.Path(__file__).resolve().parent
caddy = os.environ.get("CADDY_BIN", "caddy")

def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]

class Backend(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(dict(self.headers)).encode())
    do_POST = do_GET
    def log_message(self, *args):
        pass

backend = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Backend)
threading.Thread(target=backend.serve_forever, daemon=True).start()
try:
    with tempfile.TemporaryDirectory(prefix="library-gateway-test-") as directory:
        stage = pathlib.Path(directory)
        port, cert_port = free_port(), free_port()
        password = secrets.token_urlsafe(24)  # Disposable fixture; never a real login.
        # Low cost keeps disposable fixture checks fast on a Pi; the installer
        # retains Caddy's default cost for the real user-created login.
        password_hash = subprocess.check_output([caddy, "hash-password", "--bcrypt-cost", "4", "--plaintext", password], text=True).strip()
        env = dict(os.environ, HOME=str(stage), XDG_DATA_HOME=str(stage / "data"), XDG_CONFIG_HOME=str(stage / "config"),
                   LIBRARY_LOGIN_USER="fixture", LIBRARY_LOGIN_HASH=password_hash)
        origin = f"https://localhost:{port}"
        upstream = f"127.0.0.1:{backend.server_port}"
        config = (root / "Caddyfile.lan.example").read_text().replace("https://library.local", origin)
        # Unprivileged tests must not request Caddy's automatic port-80 redirect.
        config = config.replace("{\n", "{\n\tauto_https disable_redirects\n", 1)
        config = config.replace("http://library.local:8081", f"http://localhost:{cert_port}")
        config = config.replace("127.0.0.1:8080", upstream).replace("/var/lib/library-gateway/public", str(stage / "public"))
        path = stage / "Caddyfile"
        path.write_text(config)
        subprocess.run([caddy, "adapt", "--config", str(path), "--adapter", "caddyfile"], env=env, stdout=subprocess.DEVNULL, check=True)
        with (stage / "gateway.log").open("wb") as log:
            process = subprocess.Popen([caddy, "run", "--config", str(path), "--adapter", "caddyfile"], env=env, stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 20
                while True:
                    certificates = list(stage.rglob("root.crt"))
                    if certificates:
                        break
                    if process.poll() is not None or time.monotonic() > deadline:
                        raise RuntimeError("Gateway failed to start: " + (stage / "gateway.log").read_text())
                    time.sleep(0.1)
                context = ssl.create_default_context(cafile=str(certificates[0]))
                client = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=context))
                authorization = "Basic " + base64.b64encode(f"fixture:{password}".encode()).decode()
                def request(headers=None, method="GET", suffix="/healthz", base=origin):
                    req = urllib.request.Request(base + suffix, headers=headers or {}, method=method)
                    try:
                        # Caddy deliberately spends time hashing unknown users;
                        # allow that anti-enumeration check on a slow Pi 3.
                        with client.open(req, timeout=45) as response:
                            return response.status, response.read()
                    except urllib.error.HTTPError as error:
                        return error.code, error.read()
                while True:
                    try:
                        assert request()[0] == 401
                        break
                    except (urllib.error.URLError, TimeoutError):
                        if time.monotonic() > deadline:
                            raise
                        time.sleep(0.1)
                assert request({"Authorization": "Basic Zm9vOmJhcg=="})[0] == 401
                for bad in ["https://elsewhere.invalid", "null", "http://localhost", origin + ".invalid"]:
                    assert request({"Authorization": authorization, "Origin": bad}, "POST")[0] == 403
                for headers in [{"Authorization": authorization}, {"Authorization": authorization, "Origin": origin}]:
                    status, body = request(headers, "POST")
                    assert status == 200
                    received = {k.lower(): v for k, v in json.loads(body).items()}
                    assert received["host"] == upstream
                    assert received["origin"] == "http://" + upstream
                    assert "authorization" not in received
                assert request(suffix="/", base=f"http://localhost:{cert_port}")[0] == 404
                print("PASS: TLS trust, login enforcement, foreign-Origin rejection, authenticated proxying, credential removal and certificate-only HTTP endpoint.")
            finally:
                if process.poll() is not None:
                    print((stage / "gateway.log").read_text())
                process.terminate()
                process.wait(timeout=10)
finally:
    backend.shutdown()
    backend.server_close()
