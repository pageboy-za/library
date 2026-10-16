# Optional Apple Wallet and Google Wallet member cards

Wallet providers start disabled. Ordinary printable member cards and QR codes do
not require an Apple or Google account. Enabling a provider requires the library
operator's own issuer credentials; installing this app does not create them.

Each wallet card contains the member's display name and opaque library card code.
The QR code contains only that card code, not an email address, telephone number,
borrowing history, registration receipt or sign-in credential. The card identifies
a member to staff; it does not authenticate a member or authorize borrowing.

## Operator configuration

Pass `-wallet-config /etc/home-library-wallet/config.json` to the Go application.
Use absolute paths. The config and private credential files must be readable only
by the application OS user (mode `0600`, normally owner `home-library`). Put them
in a private directory such as `/etc/home-library-wallet`, outside the repository,
web assets and library data/backup directory. Certificates may be publicly readable;
private keys and the Google service-account JSON must not be. Symlink credential
files are rejected. Keep private issuer credentials in a separate secure backup.

The supplied service and newly generated updater drop-in load the optional file
`/etc/home-library-wallet/runtime.env`. Put only paths/public settings in it:

```text
LIBRARY_WALLET_CONFIG=/etc/home-library-wallet/config.json
LIBRARY_PUBLIC_URL=https://library.local
```

For an existing installation whose systemd unit predates this support, add a
reviewed service drop-in containing `[Service]` and
`EnvironmentFile=-/etc/home-library-wallet/runtime.env`. Then run a systemd daemon
reload and restart the library during an agreed maintenance window. This document
does not modify the Pi. The public URL is the trusted member-facing HTTPS origin;
it does not publish the private Pi on the internet.

A provider block may be omitted. An included but incomplete, expired or invalid
provider block makes startup fail, rather than advertise a working wallet button.
Changing credentials requires restarting the application. Status endpoints expose
only two configured booleans, never credential material or filesystem paths.

Example structure (paths and identifiers are placeholders, not working issuers):

```json
{
  "apple": {
    "pass_type_id": "pass.example.library",
    "team_id": "YOUR_APPLE_TEAM_ID",
    "organization": "Your library",
    "certificate": "/etc/home-library-wallet/pass-certificate.pem",
    "private_key": "/etc/home-library-wallet/pass-private-key.pem",
    "wwdr_certificates": "/etc/home-library-wallet/wwdr.pem"
  },
  "google": {
    "issuer_id": "YOUR_NUMERIC_ISSUER_ID",
    "class_id": "YOUR_NUMERIC_ISSUER_ID.library_members",
    "organization": "Your library",
    "service_account": "/etc/home-library-wallet/google-service-account.json",
    "origins": ["https://library.local"]
  }
}
```

Do not paste real keys into an issue, chat, environment variable or source file.
The application reads unencrypted RSA PEM keys (PKCS#1 or PKCS#8), minimum 2048
bits; operating-system permissions protect the stored files. It neither invokes
OpenSSL nor runs Node at runtime, including on the ARM64 Raspberry Pi.

## Apple prerequisites and delivery

1. Enrol in the Apple Developer Program and register a Pass Type Identifier.
2. Obtain an Apple-issued Pass Type ID signing certificate for that identifier
   and the corresponding private key. Export those into separate PEM files on a
   trusted administrator machine; transfer privately to the Pi.
3. Download the correct Apple Worldwide Developer Relations intermediate
   certificate for that signing certificate. Supply its PEM certificate, followed
   by any additional parent certificates in issuer order. The service validates
   the supplied chain's signatures, CA usage and validity; operators must obtain
   the authentic issuer certificates from Apple. It does not download trust data.
4. Set `pass_type_id` to the certificate subject UID and `team_id` to its subject
   organisational unit. The service checks those values, key matching, signing
   usage and expiry. Renew certificates before expiry.
5. Download a real test member's `.pkpass` through the app on an Apple device,
   and verify that Wallet accepts it before offering it to members.

The app creates `pass.json`, PNG icons, the required SHA-1 file manifest and a
SHA-256 detached PKCS#7 signature with the intermediate certificates and signing
attributes. The result is a `.pkpass` ZIP, served as
`application/vnd.apple.pkpass` by the application.

No Apple web-service update endpoint, authentication token, push notification or
automatic card update is configured. A card remains static after saving. Revoking
or rotating a member card must be enforced by staff lookup; it does not remotely
delete an already saved pass. A replacement pass must be downloaded explicitly.
A private LAN Pi needs no incoming public internet for this download, but a
member's device must reach it and trust its HTTPS certificate.

See Apple's [pass creation guide](https://developer.apple.com/library/archive/documentation/UserExperience/Conceptual/PassKit_PG/Creating.html),
[building a pass](https://developer.apple.com/documentation/walletpasses/building-a-pass)
and [Wallet prerequisites](https://developer.apple.com/wallet/get-started/).

## Google prerequisites and delivery

1. Create a Google Wallet issuer account and follow Google's publishing process.
   Demo/test restrictions apply until Google grants publishing access.
2. Enable the Google Wallet API in a Google Cloud project. Create a service
   account, grant it access to the Wallet issuer account, and obtain its private
   JSON key through an approved administrator process.
3. Create the generic pass class in that issuer account **before** using this app.
   Set `class_id` to its complete `issuerID.identifier`; this app does not create
   or edit classes, request publishing approval or call Google's REST API.
4. Configure the issuer ID, service-account JSON and HTTPS origin(s) used by your
   member pages. Origins must have no path or trailing slash. Account for browser
   trust of the Pi's private certificate.
5. Test saving a member card with an authorised Google test account, then with
   the intended audience after publishing access is granted.

The Pi signs a local RS256 `savetowallet` JWT containing the existing class ID and
a generic object with the member name and QR card code. It returns Google's
`https://pay.google.com/gp/v/save/…` link. The browser contacts Google when the
member chooses to save; the Pi makes no issuer API call and needs no public
incoming endpoint. Google receives the member name and opaque card code in the
save payload. Treat the signed save URL as private member data; do not log it.

The Google object ID is the issuer ID plus the opaque card code. Creating a new
code creates a different object. The application does not update/revoke saved
Google objects or synchronize loans; staff must check card validity locally.

See Google's [web save flow](https://codelabs.developers.google.com/add-to-wallet-web),
[JWT format](https://developers.google.com/wallet/reference/rest/v1/Jwt),
[generic object fields](https://developers.google.com/wallet/reference/rest/v1/genericobject)
and [working with JWTs](https://developers.google.com/wallet/generic/use-cases/jwt).

## Verification limits

Automated tests generate disposable issuer certificates and keys, verify the
actual detached CMS signature and certificate chain, validate pass file hashes,
verify the RSA JWT signature and inspect its payload. They also reject mismatched
identity, unsafe file permissions and invalid origins. Those tests demonstrate
correct packaging and cryptography; they are not Apple/Google issuer approval or
proof of acceptance by a real device. Final device acceptance requires your real
issuer accounts and an explicit operator test. Never distribute synthetic test
passes as working cards.

The native CMS dependency is [smallstep/pkcs7](https://github.com/smallstep/pkcs7),
pinned at v0.2.3. Printable QR encoding uses
[skip2/go-qrcode](https://github.com/skip2/go-qrcode), pinned to
v0.0.0-20200617195104-da1b6568686e. Dependency licensing belongs in the release's
third-party notices.
