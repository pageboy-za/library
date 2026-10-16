# Offline application acceptance

`deploy/offline-acceptance.sh` exercises the built application with temporary data. It accepts **no arguments**, so a household data path cannot be supplied. It does not install dependencies, upload files, change a service, or connect to an existing library server.

## Run

Build the current Go binaries and frontend beforehand. The runner requires Python 3, `bin/library`, `bin/library-backup`, and prebuilt `web/dist/`. Node and installed frontend dependencies are not needed by the runner:

```sh
./deploy/offline-acceptance.sh
```

It starts its own server on a temporary `127.0.0.1` port. Another owned loopback listener acts as a deny proxy: every CONNECT/GET/POST receives HTTP 503 and is never forwarded. Only child-process proxy settings change. The API client bypasses proxies for local requests. The check requires evidence that an Open Library request reached the deny proxy, preventing a successful online response from being mistaken for offline fallback.

Current metadata, enrichment and cover clients use Go's default HTTP transport, whose `ProxyFromEnvironment` honors these settings. Frontend assets/fonts are local and the application executable search path is empty. This simulates provider failure; it is not operating-system network isolation or a physical Wi-Fi-disconnection test.

## What it checks

- An uncached ISBN fails quickly and offers manual entry.
- A manual book without an ISBN accepts a year-only publication date and a shelf.
- A generated synthetic PNG uploads locally, becomes the selected cover and is served as a normalized JPEG.
- A metadata edit is immediately available from the API; its reusable SvelteKit book shell loads current data without waiting for static generation.
- Assigning an ISBN preserves the chosen cover; repeated ISBN scans open the same record without adding copies. Another physical copy is added explicitly.
- Checkout changes live inventory availability/shelf status while the reusable book shell remains unchanged.
- The saved description is available through the local API; private notes never appear in the reusable HTML shell.
- Author search finds the saved book.
- The backup utility creates and verifies a consistent backup while the fixture server runs, restores to a separate clean folder, and starts a new offline instance.
- Restored copies, an outstanding loan, private notes, prebuilt interface and cover bytes remain intact. Returning the loan restores availability.

The prebuilt web/dist interface is copied into the disposable workspace. The child application runs with an empty executable search path, without Node or build tools. The script generates no book pages. Request, startup and backup commands have time limits; shutdown targets only its own subprocess. Cleanup stops that process before removing temporary files. A listener-permission failure is a failed check and should be reported, not worked around by using the household server.

## Limits

This validates API behavior, prebuilt HTML and recovery using the actual built executables. It does not drive a browser, prove Svelte hydration/click behavior, test scanner focus or timing, or establish Pi performance, systemd startup, touchscreen operation or durable backup-disk behavior. Browser and physical Raspberry Pi acceptance remain separate. It tests the existing binaries; rebuild after source changes before relying on its result.

Historical development result: **PASS on 10 October 2026**, before the SvelteKit migration. The deny-proxy observation, all fixture assertions, backup verification, restored offline startup and local cover checksum passed. Temporary processes and folders were cleaned up; no household database was opened. This is development acceptance, not a Pi hardware sign-off.

## Verified checkpoint

21:48 Europe/London, 10 October 2026: the runner passed once against the current built executables. Its temporary server, proxy and data were removed. Shell syntax and refusal of supplied data arguments also passed. The source-package check confirmed this runner and both acceptance documents are included.
