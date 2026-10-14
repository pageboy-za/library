# Raspberry Pi installation draft

This package prepares NEA-39 and NEA-41. It has **not** been installed or tested on a Raspberry Pi. The supported target for this draft is Raspberry Pi OS **64-bit**, with a desktop and Chromium. Select the hardware only after measuring page-generation speed and memory use; no claim is made that the smallest Pi can run these builds comfortably.

## What runs on the device

- Go serves the local browser and owns SQLite operations.
- Svelte islands fetch current copies, shelves and loans.
- Node runs the installed Astro CLI when book metadata, descriptions, subjects or covers change. Each publication builds a complete static site from a database snapshot; it is not currently a single-page incremental compiler.
- Static HTML and assets are generated under `data/pages/`; SQLite and cached covers remain under `data/`.

This architecture requires the Astro **source and installed dependencies** on the Pi. Copying only `bin/library` and `web/dist` is insufficient. Go is needed to build/update the binaries, but not for normal runtime. Node is required at runtime for page regeneration. A future multi-format rendering proposal is design work only and does not replace this requirement.

## 1. Build on the Pi

Install Go **1.27 or newer**, Node **22.12 or newer**, npm **9.6.5 or newer**, and Chromium. Use the correct ARM64 packages and check versions before building. Follow the [official Go installation instructions](https://go.dev/doc/install) and your chosen Node distribution's installation instructions. Do not assume the OS package versions meet the requirements.

Copy this project's source and lock files to the Pi, excluding the Mac's `bin/`, `web/node_modules/`, `web/dist/`, `.astro/` and development `data/`. Install dependencies on the Pi itself so their platform packages match ARM64. Transfer existing household data separately through the verified backup/restore workflow.

From the project root on the Pi:

```sh
./deploy/build-pi.sh
./bin/library -listen 127.0.0.1:8080 -data data -web web
```

The script checks the platform/Node/npm versions, installs locked frontend dependencies with `npm ci`, checks/builds Astro, runs Go tests, and builds the server, backup utility and read-only MCP utility without CGO. It writes `bin/build-info.txt` with versions and checksums. Module requirements enforce the Go version. The standalone frontend build has no book snapshot; the server generates the real pages at startup.

Open `http://127.0.0.1:8080/` on the Pi and add one test book. Inspect its HTML source, then move its copy to a shelf. Confirm that the move updates immediately without a build. Try scans while disconnected from the internet: existing records/manual entry should still work; external lookups cannot.

## 2. Install the service

The following commands are **instructions to run on the Pi**, not actions performed here. First stop the test instance. This draft uses a dedicated account and keeps application sources writable within its own private state directory because Astro generates build caches.

```sh
sudo useradd --system --user-group --home-dir /var/lib/home-library --shell /usr/sbin/nologin home-library
sudo install -d -m 700 -o home-library -g home-library /var/lib/home-library
sudo install -d -m 700 -o home-library -g home-library /var/lib/home-library/app /var/lib/home-library/data
```

If the account already exists, skip its creation. Copy the built project to `/var/lib/home-library/app/`, including `web/node_modules/` installed on the Pi. Keep the application's household data outside `app/`. Then, from the project root:

```sh
sudo chown -R home-library:home-library /var/lib/home-library/app
sudo install -m 644 deploy/home-library.service /etc/systemd/system/home-library.service
sudo systemd-analyze verify /etc/systemd/system/home-library.service
sudo systemctl daemon-reload
sudo systemctl enable --now home-library.service
systemctl status home-library.service
journalctl -u home-library.service -n 50 --no-pager
curl --fail http://127.0.0.1:8080/healthz
```

The service binds only to loopback. It restarts after failure and writes only under `/var/lib/home-library/` (plus private temporary storage). If Node is installed somewhere else, change the unit's `PATH` to its actual location; a shell version manager under your normal user's home is inaccessible to this account. Its filesystem settings follow the [systemd execution documentation](https://github.com/systemd/systemd/blob/main/man/systemd.exec.xml).

Do not run two server processes against the same data/page-generation directories. Stop the service before replacing code/dependencies, and restart it after a successful build. Check the journal and one new-book save after every update. Rollback the application files separately from household data; do not overwrite SQLite with an old release copy.

## 3. Optional browser kiosk

The web app already focuses its ISBN search box. Kiosk mode launches Chromium full screen; it does not by itself lock down the whole operating system. Keep a keyboard available for setup and recovery.

Install the launcher and run it from the logged-in desktop session:

```sh
sudo install -m 755 deploy/start-kiosk.sh /usr/local/bin/home-library-kiosk
/usr/local/bin/home-library-kiosk
```

It waits for the loopback health endpoint and uses a separate browser profile. It keeps Chromium's normal sandbox/security settings. Exit with Alt+F4, or close Chromium from the desktop.

For desktops supporting XDG autostart, copy the provided template for the **desktop user**, not the service account:

```sh
mkdir -p "$HOME/.config/autostart"
cp deploy/home-library-kiosk.desktop "$HOME/.config/autostart/"
```

Desktop autologin, Wayland startup configuration, display rotation, blanking and on-screen keyboard settings depend on the selected Raspberry Pi OS desktop. Follow the [official Pi kiosk tutorial](https://www.raspberrypi.com/tutorials/how-to-use-a-raspberry-pi-in-kiosk-mode/) when configuring those. The supplied `.desktop` template has not been verified on the selected Pi session; use its supported session startup method if XDG autostart is unavailable. Browser startup failures need checking locally; the desktop launcher is not supervised by systemd.

Set the USB scanner to keyboard output, ISBN/EAN only, with an Enter suffix. Scan an existing book twice and confirm its copy count stays unchanged. The physical scanner, boot behavior and touchscreen remain hardware acceptance checks.

## 4. Backup and recovery

Use the consistent snapshot command rather than copying a live `library.sqlite` file:

```sh
sudo install -d -m 700 -o home-library -g home-library /var/lib/home-library/backups
sudo -u home-library /var/lib/home-library/app/bin/library-backup create -data /var/lib/home-library/data -out /var/lib/home-library/backups/library-2026-10-10
sudo -u home-library /var/lib/home-library/app/bin/library-backup verify -backup /var/lib/home-library/backups/library-2026-10-10
```

Choose a fresh output directory for every run. Copy verified backups to another device. See `BACKUP_README.md` for restore to a new directory and testing on a separate port. Static pages, thumbnails and dependencies are derived data and can be regenerated. Settings now displays read-only backup verification/freshness; browser restore remains future work.

### Optional daily backup timer

The backup service/timer examples are disabled templates. Nothing has been
installed or scheduled on this development machine or a Pi. First mount a
durable backup disk and choose its destination folder; the examples use
`/mnt/library-backups/home-library`. Edit both the service's mount check and
paths to match the actual disk. Do not simply choose another folder on the
same SD card as the only backup.

The following commands are instructions for the Pi after the disk is mounted:

```sh
sudo install -d -m 700 -o home-library -g home-library /mnt/library-backups/home-library
sudo chmod 755 /var/lib/home-library/app/deploy/backup-run.sh
sudo install -m 644 deploy/backup-home-library.service.example /etc/systemd/system/backup-home-library.service
sudo install -m 644 deploy/backup-home-library.timer.example /etc/systemd/system/backup-home-library.timer
sudo systemd-analyze verify /etc/systemd/system/backup-home-library.service /etc/systemd/system/backup-home-library.timer
sudo systemctl daemon-reload
sudo systemctl start backup-home-library.service
journalctl -u backup-home-library.service -n 50 --no-pager
```

Confirm the manual run produced a verified backup on the chosen disk before
enabling daily runs:

```sh
sudo systemctl enable --now backup-home-library.timer
systemctl list-timers backup-home-library.timer
```

The example runs daily at 03:00 local time, with up to ten minutes of random
delay, and catches up a missed run after activation. A mount check prevents
writing to the Pi's internal filesystem if the disk is absent; an absent mount
skips the run and should be investigated in the journal. Disk-full, permission
and verification failures exit nonzero. Concurrent runs share an atomic lock.
Old backups are never removed automatically, so monitor disk space and choose
your own retention policy. The Settings screen can show saved-backup verification and freshness when the app is started with `-backup-dir` pointing to the chosen backup parent folder. It does not monitor systemd run outcomes or send failure notifications.

Stop scheduled runs with `sudo systemctl disable --now backup-home-library.timer`.
For stale-lock recovery and restore verification, follow `BACKUP_README.md`.

## Validation and remaining checks

Performed on the development Mac: POSIX shell syntax checks, template structure/path checks, a non-mutating platform rejection check, and successful cross-compilation of the server for Linux ARM64 with CGO disabled. Cross-compilation confirms it builds; it does not verify behavior on the Pi. No service was installed, no desktop startup changed and no household data was copied.

Before marking the Pi tickets complete, verify on actual hardware: ARM64 builds, runtime dependency paths, service permissions under systemd, cold boot/offline startup, interrupted publication recovery, disk usage from retained generations, usable build time with the full catalogue, USB scanner focus/Enter behavior, display/touch navigation, backups and restore.

Source-only transfer packaging is documented in [RELEASE_README.md](RELEASE_README.md); household data remains a separate verified backup transfer. No package or service was installed on a Pi during these checks.
