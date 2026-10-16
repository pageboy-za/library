# Household network and iPad kiosk

The optional Caddy gateway makes the library available at
`https://library.local` on the household network. It uses HTTPS, with browsing available to everyone on the trusted network.
Staff tools require individual staff sign-in. The Go application stays on `127.0.0.1:8080`; neither its listener nor
its existing Host/Origin checks are weakened.

The gateway rejects foreign browser Origins before translating accepted
requests to loopback and removes the gateway Authorization header before
proxying. Port 8081 serves only the public device-trust certificate; it cannot
serve the library or catalogue. Do not forward these ports on the router.

## Install the gateway

First install and verify the library service. Prepare a directory on the Pi
containing these files:

- `library-caddy`: the official Caddy Linux ARM64 executable, checked against
  the official release checksums.
- `Caddyfile.lan.example`, `home-library-gateway.service`, and `install-lan.sh`
  from `deploy/`.
- `gateway-SHA256SUMS`: SHA-256 sums for the four files above.

From that directory, run `sudo sh install-lan.sh`. It verifies the bundle and
refuses to replace an existing gateway installation. No shared browser password
is required. On an existing staff-enabled installation, an administrator can run
`sudo python3 deploy/open-network-access.py` from reviewed source to remove the
shared login. This saves the previous gateway config, validates and restarts the
gateway, checks anonymous browsing and staff-only update access, and refreshes
the updater attestation. Failure restores the original gateway. The dedicated `library-gateway` account owns the
local certificate authority and cannot read the household application's
private state directory.

The service starts at boot. The installer prints the public root certificate's
SHA-256 fingerprint and exports `library-root.crt` beside the installer. Save
that fingerprint to compare when trusting devices. Private signing keys stay
under `/var/lib/library-gateway`; never copy them to Git or to client devices.

## Trust the certificate on each device

For an iPad on the same Wi-Fi:

1. Open `http://library.local:8081/library-root.cer` in Safari to download the
   certificate profile. This endpoint contains only the public certificate.
2. Inspect its certificate details and compare the SHA-256 fingerprint with
   the installer output before approving it.
3. Install the downloaded profile in Settings. Then go to **General > About >
   Certificate Trust Settings** and enable full trust for the local Caddy root.
4. Open `https://library.local`. Browsing needs no sign-in. Choose a book,
   tap Lend and scan an approved pass to borrow it. Staff sign in separately.

Apple's [certificate-trust instructions](https://support.apple.com/102390)
explain the separate installation and trust steps. Trusting this local root
allows certificates signed by the Pi to be accepted by the device, so protect
the Pi and its signing keys. Remove the profile when you retire this server.
Other devices need the same root certificate in their trusted certificate
store; downloading it alone does not establish trust.

`library.local` uses local name discovery. Guest Wi-Fi, client isolation and
some VPN configurations can prevent discovery or connections. Connect both
devices to the same household network. DHCP address changes do not affect the
hostname.

## Scan at the bookshelves

1. Pair the Bluetooth scanner with the iPad in **keyboard/HID mode** using
   the scanner manufacturer's instructions.
2. Configure ISBN/EAN-13 output and an **Enter/Return suffix**. Disable the
   five-digit price supplement if the scanner supports it.
3. Open the library and tap **Scan a book** once to focus the input. iPadOS
   may require a tap even when the page requests automatic focus.
4. Scan a known book twice: both scans should open the existing record without
   adding a copy. Use **Add another copy** explicitly when needed.
5. Scan a new book, review its details, choose a shelf and save. For rapid
   entry, the **Add a book** page is also available at `/scan/`.

Test a scan in Notes first if the library receives no text. If Notes gets the
digits but the library does not submit, check focus and the Enter suffix.
Physical Bluetooth scanner operation must be verified on your actual device.

For a kiosk session, use Safari's **Add to Home Screen** and optionally
[Guided Access](https://support.apple.com/111795) to keep the iPad in the app.
Allow keyboard input in Guided Access. This is a browser kiosk, not an offline
installed application: the iPad must remain connected to the Pi.

## Validation and maintenance

Run `CADDY_BIN=/path/to/caddy python3 deploy/gateway-test.py` on a development
machine. It uses disposable loopback services to check TLS trust, anonymous
browsing, foreign-Origin rejection, staff-only updater access, removal of
credentials before proxying, and the
certificate-only HTTP endpoint. It does not test an iPad or real scanner.

Inspect `systemctl status home-library-gateway` and
`journalctl -u home-library-gateway` on the Pi after configuration changes.
The local Caddy administration API is disabled; update configuration and
restart the service with administrator access. Back up the catalogue using
[BACKUP_README.md](BACKUP_README.md) to storage outside the SD card. Catalogue
backups do not include the gateway login or certificate authority.
