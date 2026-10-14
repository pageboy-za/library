#!/bin/sh
# Fresh gateway setup. Invoke interactively with sudo; the user creates the login.
set -eu
[ "$(id -u)" = 0 ] || { echo 'Run with sudo.' >&2; exit 1; }
library_bundle=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$library_bundle"
sha256sum -c gateway-SHA256SUMS
[ ! -e /etc/home-library-gateway ] || { echo 'Existing gateway configuration found; refusing to replace it.' >&2; exit 1; }
[ ! -e /var/lib/library-gateway ] || { echo 'Existing gateway data found; refusing to replace it.' >&2; exit 1; }
systemctl is-active --quiet home-library.service
install -m 755 library-caddy /usr/local/bin/library-caddy
printf 'Choose a new library web login password. The username will be nealle.\n'
library_hash=$(/usr/local/bin/library-caddy hash-password)
[ -n "$library_hash" ] || { echo 'No password hash was created.' >&2; exit 1; }
if ! id library-gateway >/dev/null 2>&1; then
  /usr/sbin/useradd --system --user-group --home-dir /var/lib/library-gateway --shell /usr/sbin/nologin library-gateway
fi
install -d -m 700 -o library-gateway -g library-gateway /var/lib/library-gateway
install -d -m 755 -o library-gateway -g library-gateway /var/lib/library-gateway/public
install -d -m 755 /etc/home-library-gateway
umask 077
printf 'LIBRARY_LOGIN_USER=nealle\nLIBRARY_LOGIN_HASH=%s\n' "$library_hash" > /etc/home-library-gateway/login.env
install -m 644 Caddyfile.lan.example /etc/home-library-gateway/Caddyfile
LIBRARY_LOGIN_USER=nealle LIBRARY_LOGIN_HASH="$library_hash" /usr/local/bin/library-caddy adapt --config /etc/home-library-gateway/Caddyfile --adapter caddyfile >/dev/null
unset library_hash
install -m 644 home-library-gateway.service /etc/systemd/system/home-library-gateway.service
systemd-analyze verify /etc/systemd/system/home-library-gateway.service
systemctl daemon-reload
systemctl enable --now home-library-gateway.service
library_ca=/var/lib/library-gateway/.local/share/caddy/pki/authorities/local/root.crt
library_attempt=0
until [ -s "$library_ca" ]; do
  library_attempt=$((library_attempt + 1))
  [ "$library_attempt" -lt 30 ] || { journalctl -u home-library-gateway.service -n 40 --no-pager; exit 1; }
  sleep 2
done
openssl x509 -in "$library_ca" -outform DER -out /var/lib/library-gateway/public/library-root.cer
chmod 644 /var/lib/library-gateway/public/library-root.cer
install -m 644 "$library_ca" "$library_bundle/library-root.crt"
echo 'Local certificate fingerprint (compare before trusting it on a device):'
openssl x509 -in "$library_ca" -noout -fingerprint -sha256
echo 'Ready: https://library.local (username: nealle).'
echo 'Device trust certificate: http://library.local:8081/library-root.cer'
