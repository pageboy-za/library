# Local release acceptance — 10 October 2026

The development preview is usable. **The Raspberry Pi release is not yet accepted:** no Pi installation, physical scanner test or durable scheduled-backup activation has been verified. This matrix separates local evidence from work requiring the chosen device. Linear remains the live ticket tracker; this file does not close tickets.

## Acceptance matrix

| Ticket | Implemented and verified locally | Still needed before acceptance |
| --- | --- | --- |
| NEA-24 — Lumos styling | Astro shells and Svelte controls use Lumos tokens/layers; browser focus, responsive book editor and Settings layout checked at 390px; frontend checks pass. | Activate the mobile Menu with **Enter on a physical keyboard**, confirm Escape returns focus, then check visible focus and navigation on the actual display. |
| NEA-30 — Barcode scanning | Initial ISBN focus; canonical ISBN validation; existing ISBN Enter opens its stable book route without adding a copy; manual drafts and failed-publication receipts retained. | Configure physical scanner for ISBN/EAN keyboard output plus Enter, excluding price supplements; verify rapid/repeated scans and focus after navigation on the kiosk. |
| NEA-39 — Pi packaging | Reproducible source-only archive, exclusions/checksums, overwrite refusal; extracted source builds Go commands for ARM64 and Astro with installed Mac dependencies. Shutdown stops unfinished Astro builds. | Install ARM64 dependencies and run the complete build on the chosen Pi; measure full-catalogue startup/publication time, memory and storage. Keep Node, Astro source and installed frontend dependencies available at runtime. |
| NEA-40 — Backup and recovery | Consistent SQLite snapshot, export/covers, checksums and foreign-key verification; clean-directory restore; runner failure/concurrency fixtures; read-only Settings verified for unconfigured/empty/failed/stale/retry states. | Choose and mount a durable separate backup disk; configure monitoring; verify a backup and independent restore on the Pi; deliberately install/enable the timer only after a successful manual run. Check missing-disk/full-disk outcomes and available space. |
| NEA-41 — Service and kiosk | Service, launcher and disabled timer templates plus installation instructions prepared; syntax/structure and ARM64 build checks pass. | Verify service account permissions, Node path, desktop startup, cold boot, kiosk health/focus, display/touch behavior and recovery on actual Pi OS. No service or timer has been installed by these checks. |
| NEA-42 — Kiosk/keyboard/small screens | Disposable browser checks cover navigation, scanner/manual-entry focus, receipt retry focus, book editor, lending, cover upload and phone Settings layout. | Run the keyboard/display checklist below, including tab order, Menu Enter, touchscreen controls and scanner interaction. Desktop browser emulation is not physical-device acceptance. |
| NEA-43 — Release/offline recovery | New records automatically regenerate static book pages; inventory, shelf and lending changes do not rebuild metadata pages. Local fixture tests cover persisted data, retries/stale edits, loan safety, broken publication recovery, backup corruption/orphans and shutdown cleanup. [Offline fixture acceptance](OFFLINE_ACCEPTANCE.md) records its own runner/evidence. | Exercise a real disconnected Pi, cold boot, on-device page regeneration, interrupted publication recovery and restored-data browsing. Record measured results and remaining failures before accepting release. |

Public verification guidance: [Pi installation](PI_README.md), [source packaging](RELEASE_README.md), [offline acceptance](OFFLINE_ACCEPTANCE.md), and [backup workflow](BACKUP_README.md). Local development checkpoints and screenshots are not published.

## Morning device checklist

Start with `./deploy/offline-acceptance.sh` for the automated disposable API/recovery checks, then use a disposable catalogue for browser, scanner, lending and recovery checks. From the project root, with current binaries and installed frontend dependencies:

```sh
acceptance_dir=$(mktemp -d /private/tmp/library-acceptance.XXXXXX)
mkdir "$acceptance_dir/backups"
printf 'Disposable acceptance folder: %s\n' "$acceptance_dir"
./bin/library -listen 127.0.0.1:8082 -data "$acceptance_dir/data" -web web -backup-dir "$acceptance_dir/backups"
```

The temporary folder above suits the development Mac. On the Pi, use `mktemp -d /tmp/library-acceptance.XXXXXX` instead. Keep that terminal open; use another terminal for the backup commands below. This temporary backup parent is a test fixture, not protection for household data.

1. **Keyboard and screen:** Open `http://127.0.0.1:8082/`. Check ISBN focus immediately. Tab through visible controls. At a narrow window, focus Menu and press Enter; Escape should close it and return focus. Repeat on the intended screen/touch input.
2. **Scanner:** Configure keyboard output with Enter and no price supplement. Add a test shelf, scan a book, review details and save. Scan that ISBN twice more: both scans should open the same `/books/{id}/` page and retain one copy. Add another copy explicitly and confirm the count becomes two. Return to Library and scan again without clicking the ISBN field.
3. **Inventory and lending:** Move one copy to another test shelf. Create a test borrower, check out that copy, inspect Loans, then return it. Confirm shelf/availability update immediately and history remains. These actions should not need a static-page build.
4. **Covers and static pages:** Choose or upload a cover on a test book and save. Reload its page: current metadata, the selected local cover and shelf/lending controls load from the local API without waiting for static generation. A failed provider lookup should still permit manual details.
5. **Offline:** Follow [the isolated offline checks](OFFLINE_ACCEPTANCE.md). Then disconnect the actual device from the internet and repeat existing scans, title/author search, manual entry, shelf moves and lending. Restart while offline and check local pages/covers. External metadata/artwork requests may fail; record whether their fallback is usable. Reconnect afterward.
6. **Timing and storage:** On the Pi, record cold-start readiness time and new-book/edited-cover publication time with the expected catalogue size. Record memory/disk usage and whether the screen remains responsive. Agree usable limits from these measurements; no Pi performance target has been verified yet.

To test a snapshot and restore, copy the printed fixture folder into the second terminal's `acceptance_dir` variable, then run:

```sh
./bin/library-backup create -data "$acceptance_dir/data" -out "$acceptance_dir/backups/first"
./bin/library-backup verify -backup "$acceptance_dir/backups/first"
./bin/library-backup restore -backup "$acceptance_dir/backups/first" -out "$acceptance_dir/restored"
./bin/library -listen 127.0.0.1:8083 -data "$acceptance_dir/restored" -web web
```

Open `http://127.0.0.1:8083/` and compare book IDs, copy counts, shelves, covers and lending history. The restore destination must not already exist. Settings on port8082 should report the saved verified fixture; Refresh may use its 30-second cache. Stop both fixture servers with Ctrl+C when finished. Use the [Pi setup instructions](PI_README.md) separately for service installation and a durable mounted backup disk; do not enable scheduling just to complete this test.

## Acceptance record and boundaries

Record date, device/OS, scanner model/settings, tested catalogue size, pass/fail per step, observed timings and unresolved failures. Hardware rows stay pending until that evidence exists. Interrupting builds or simulating storage failures belongs in disposable data, never the household catalogue.

Local cover uploads do not publish to Open Library. The contribution panel prepares an explicitly reviewed sanitized download for manual contribution; no automatic or authenticated upstream upload is implemented. Explained theme search uses local keywords, not semantic similarity.

The previously authorized temporary LAN preview has expired. Its separately tracked firewall review remains a follow-up ticket; this checklist neither renews network exposure nor changes firewall settings. Keep acceptance servers on loopback.
