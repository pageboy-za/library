# Staff access and permissions migration

This release separates public catalogue browsing from staff operations. Librarians
manage catalogue and lending work. Administrators additionally manage staff,
settings and updates. Borrowers are not staff accounts.

## Before installing on an existing Raspberry Pi

This is a database schema 2 release. It is intentionally rejected by the older
updater. Do not bypass that rejection or install an old application against a
migrated database. No live installation is performed by the development scripts.

1. Schedule a maintenance window and pause changes. Create and verify a fresh
   private backup with the currently installed `library-backup` executable. Keep
   an off-device copy and record the current catalogue baseline.
2. Review and install the updated fixed helpers using
   `sudo python3 deploy/upgrade-runtime-updater.py` from the reviewed source.
   This refreshes helpers; it does not upgrade the gateway or application.
3. An administrator must update the installed gateway configuration using the
   reviewed `deploy/Caddyfile.lan.example`. Preserve the installation's hostname,
   TLS, outer basic login and foreign-Origin checks. The `/api/updates/*` handler
   must run the Go `/api/auth/authorize-update` check before reaching the updater
   socket. After reviewing the ordering and security boundary, run
   `sudo python3 deploy/attest-staff-gateway.py --acknowledge-reviewed-gateway`.
   This validates syntax, restarts the gateway and records a root-owned marker
   bound to the exact config checksum. Schema 2 activation refuses missing or
   stale markers, so later gateway edits require review and attestation again. Do not expose the application or updater directly on LAN.
   Until the new app is installed, updater access through this new check fails
   closed. Use a reviewed maintenance deployment procedure.
4. Install the signed schema 2 release. Create the first administrator locally
   using its `bin/library-account` command as the application OS user. Supply the
   password on standard input using a hidden terminal prompt, never an argument,
   shell history, source file or chat message. Example Bash session:

   ```bash
   read -r -s -p 'New administrator password: ' library_staff_password; printf '\n'
   printf '%s\n' "$library_staff_password" | sudo -u home-library /opt/home-library/current/bin/library-account -data /var/lib/home-library/data -username YOUR_USERNAME -display-name 'Your name' bootstrap
   unset library_staff_password
   ```

   Bootstrap refuses to replace an existing administrator. `reset-password`
   provides local recovery for an existing staff username; `list` shows accounts.
5. Sign in through the gateway. Verify anonymous users cannot read borrowers,
   loans, private notes, staff records, backup status or updater status, and
   cannot mutate data. Check librarian and administrator access separately,
   including that a librarian cannot manage accounts, settings or updates.
6. Recheck books, copies, loans, settings, covers and service health against the
   baseline. Exercise saves and lending only with independent disposable data.

## Backups and recovery

Private SQLite snapshots retain staff accounts and password hashes. Protect them
like the library database. The readable catalogue JSON excludes staff accounts
and sessions; it is not an account migration format. Restoring a snapshot clears
all staff browser sessions, requiring fresh sign-in, while retaining accounts.

After schema 2 migration the updater refuses automatic rollback to a schema 1
release and leaves writes paused with `needs_attention`. An administrator must
repair the current release or explicitly restore the verified pre-migration
backup while the service is stopped, accepting loss of post-backup changes.
Automatic rollback to a known schema 2 release remains possible. Recovery never
silently replaces household catalogue bytes.

## Build and verification

Runtime remains Go + SQLite with prebuilt SvelteKit files, including on ARM64
Raspberry Pi. Node is needed only while building. Release bundles now include
`bin/library-account`; schema 2 manifests declare `rollback_compatible: false`.

Run Go tests, frontend checks/build, updater rejection/recovery tests and the
fully disposable `deploy/offline-acceptance.sh` before a release. The existing
outer gateway login is deliberately retained as an additional private-LAN
boundary; it does not substitute for individual staff permissions.

## Permissions

| Experience | Access |
| --- | --- |
| Member, no sign-in | Catalogue, public book descriptions, selected covers and availability |
| Librarian | Member access plus books, copies, shelves, borrowers and lending |
| Administrator | Librarian access plus staff accounts, settings, backup status and updates |

Staff API access is checked by Go for every request. Mutations require a
session-specific CSRF token. Changing a password, role or enabled status revokes
that person's sessions; the last enabled administrator cannot be removed.
Cookies require HTTPS by default. `-secure-cookies=false` is for a loopback HTTP
preview with disposable data only.

## Member registration and cards

Member registration is available at `/join/`, with the browser-held approval
receipt and card at `/membership/`. Staff review registrations at `/staff/members/`.
Name and email are required; telephone is optional. These contact details are
self-reported, not independently verified. Staff should confirm the person before
approval. Approval creates a new borrower record; existing borrowers are not
silently matched or merged by name or email.

A printed registration QR code opens the configured private HTTPS origin's join
page for phones on the library Wi-Fi. `-public-url` (or `LIBRARY_PUBLIC_URL`)
selects that origin; the default is `https://library.local`. It changes the QR
code's destination, not network exposure. The server still binds to loopback and
the existing TLS gateway remains the LAN entry point.

Approved cards show a name and random card number. Staff scan or type the number
to find the borrower. A card number never grants browser access, private contact
details or staff permissions. The browser's receipt is a different random secret,
kept in an HttpOnly cookie; only its hash is stored in SQLite. It expires after
30 days. Clearing cookies loses browser access; a downloaded card can still be
used while the membership remains active. Staff approval does not send email.

Suspension blocks new loans, card downloads and card lookup, while existing
loans can still be returned. Replacing a card invalidates its previous number;
the member must download the replacement. Archiving the borrower also blocks
new card issuance and lookup. Saved Wallet passes do not update or disappear
remotely: staff lookup always checks the current local membership status.

Private SQLite backups preserve memberships, contacts and browser receipts.
Readable catalogue exports exclude the member registration table, alongside
staff credentials. Wallet issuer credentials live separately, outside catalogue
backups, and require a separate secure operator backup.

Apple and Google Wallet issuance is implemented as optional, locally signed
providers. Neither requires incoming internet access to the Pi. Real issuance
requires operator credentials and device acceptance testing. Until configured,
the app offers its ordinary downloadable QR card. See [Wallet setup](WALLET_SETUP.md).
