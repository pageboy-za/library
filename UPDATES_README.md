# Application updates

Once the one-time administrator setup is complete, open the library over HTTPS,
then **Settings → Application updates → Check for updates → Install**. Pause book
entry while installing and follow the progress shown on that page. Normal app
updates do not require SSH, sudo, or a GitHub credential when releases are public.

The update service checks a publisher signature and signed file hashes, tests the
candidate against disposable data, pauses writes, creates and verifies a catalogue
backup, and switches application versions. It checks that the prebuilt interface identifies the
expected version, so old fallback pages cannot count as a successful update. Books, copies, shelves, borrowers,
loans, covers, web-login configuration and TLS keys remain on the Pi.

Only the previous successfully installed version is offered for rollback. Older
release install buttons are hidden, and a rollback does not offer another step
backward. Failed
activation restores the previous application code without replacing the catalogue
with an older snapshot. The fixed activation journal supports interrupted-job
recovery. Incompatible database migrations, OS updates, gateway changes and
signing-key rotation require separate administrator work.

## First prebuilt runtime update

New releases declare `runtime: static-v1` and contain prebuilt SvelteKit frontend files. The frontend migration preserves the existing release layout and updater contract. The Pi runs Go directly and no longer needs Node to start or save books. Existing managed installations must refresh their protected updater helpers once before accepting this format; follow [the helper upgrade and verification instructions](RELEASE_README.md#existing-managed-pi-refresh-updater-helpers-once). Legacy releases remain supported for recovery. This helper upgrade is separate from ordinary application updates.

## Publishing a release

Run the **Approved Pi release** workflow on the reviewed commit with a fresh
version such as `v0.2.0`. It tests and scans source, builds the ARM64 runtime, signs
the manifest and publishes three assets. Keep `LIBRARY_RELEASE_SIGNING_KEY` in
GitHub Actions secrets; never add a private signing key to Git. The Pi stores only
the public verification key. Publishing source does not expose household data.

The bootstrap is an administrator operation performed once, with a verified
initial bundle and public key. Application releases cannot replace the protected
updater or run installation commands as root. Do not grant the application
passwordless sudo. See [the design](REMOTE_UPDATES_PLAN.md) for the trust boundary.

## Scanning preferences and measurements

Choose a default shelf in Settings. New-book review forms preselect it; choose
Unassigned or another shelf to override it. This preference is shared across
household devices. Deleting its shelf clears the default safely.

Return saves reviewed book details from a single-line text field. Barcode Return
still looks up the ISBN. A repeated ISBN opens the existing book; adding another
physical copy stays an explicit action. After a successful shelf change, focus
returns to Scan next ISBN unless you have moved to another field or started a scan.

Save measurements include database work, commit, cover retrieval, publication
and total response time. Legacy releases also included snapshot, Astro build and atomic publication stages. Some
stages are absent when no work is needed. Server entries use `save_timing`; browser
console entries use `Library save timing`. Match them using the request ID and
`Server-Timing` response header. The measurements contain no titles, ISBNs,
borrower names, notes or passwords. They measure the existing save path; they do
not make saves faster by themselves.

An on-card update snapshot protects against update mistakes. Keep regular backups
on another device too, as described in [the backup guide](BACKUP_README.md).
