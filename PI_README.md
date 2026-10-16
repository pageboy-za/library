# Raspberry Pi installation

The target remains Raspberry Pi OS **64-bit (ARM64)**. The application runs as a local Go service with SQLite and a prebuilt browser interface. Chromium is needed only for an optional attached kiosk display. This runtime change still needs actual-device acceptance.

## What runs on the device

- `bin/library` serves compiled HTML/assets and the local API.
- Svelte loads current catalogue, copy, shelf and loan data from that API.
- SQLite, covers and caches live outside the application directory.
- No Node, npm, Go compiler or frontend source is needed for normal operation.

Book changes no longer rebuild the site. `/books/ID/` opens a shared shell that fetches the current record; initial page source no longer includes individual book metadata.

## Existing managed Pi: upgrade prerequisite

**Do not install a new static runtime release through an old updater.** The installed updater helper must first be upgraded to understand and validate `runtime: static-v1` bundles without requiring Node dependencies. Follow [RELEASE_README.md](RELEASE_README.md) for the coordinated helper upgrade and signed release process. Preserve its existing paths, gateway configuration, backup checks and rollback flow. The fresh-install instructions below are not an upgrade script for the managed Pi.

## 1. Build or obtain the runtime

Prefer a verified ARM64 runtime bundle from a trusted release. Follow [RELEASE_README.md](RELEASE_README.md) to verify the release signature and bundle manifest before installing; a checksum by itself does not establish the publisher's identity. Extract only into a fresh staging directory. A runtime bundle has `bin/library`, `bin/library-backup` and compiled files directly inside `web/`.

For a source build on the Pi, install Go **1.27+**, Node **22.12+** and npm **9.6.5+**, then run from the checkout root:

```sh
./deploy/build-pi.sh
./bin/library -listen 127.0.0.1:8080 -data /absolute/path/to/test-data -web web/dist
```

These tools are build dependencies only. `build-pi.sh` checks the platform and versions, installs locked frontend dependencies, checks/builds the frontend, runs Go tests and builds the Go utilities. Use disposable test data; transfer household data only through verified backup/restore.

Open `http://127.0.0.1:8080/`, add a test book, edit it and open its direct URL. Confirm each change appears immediately. Repeat local browsing and manual entry offline; external metadata lookup needs internet. Stop this test process before installing a service.

## 2. Fresh manual service installation

These commands are **instructions to run on the Pi**, not actions performed here. They install a fresh instance only. Stop if any installation, data, account or service already exists; use its upgrade procedure instead. Obtain the service template from the same trusted source revision as the runtime.

Run the following from a verified, extracted runtime bundle, with `library_service_template` pointing to that revision's `deploy/home-library.service`. For a source build, first assemble a fresh runtime staging directory containing `bin/library`, `bin/library-backup`, and the contents of `web/dist/` under `web/`.

```sh
set -eu
library_service_template=/absolute/path/to/source/deploy/home-library.service
# Refuse an existing installation, including dangling symbolic links.
for library_existing in /var/lib/home-library /etc/systemd/system/home-library.service; do
  if [ -e "$library_existing" ] || [ -L "$library_existing" ]; then
    echo "Existing installation: $library_existing" >&2
    exit 1
  fi
done
if getent passwd home-library >/dev/null || getent group home-library >/dev/null; then
  echo 'Existing service account or group; inspect the existing installation first.' >&2
  exit 1
fi
test -x bin/library && test -x bin/library-backup && test -f web/index.html
sudo useradd --system --user-group --home-dir /var/lib/home-library --shell /usr/sbin/nologin home-library
sudo install -d -m 755 -o root -g root /var/lib/home-library /var/lib/home-library/app
sudo install -d -m 700 -o home-library -g home-library /var/lib/home-library/data
sudo cp -R bin web /var/lib/home-library/app/
sudo chown -R root:root /var/lib/home-library/app
sudo chmod -R go-w /var/lib/home-library/app
sudo install -m 644 "$library_service_template" /etc/systemd/system/home-library.service
sudo systemd-analyze verify /etc/systemd/system/home-library.service
sudo systemctl daemon-reload
sudo systemctl enable --now home-library.service
systemctl status home-library.service
journalctl -u home-library.service -n 50 --no-pager
curl --fail http://127.0.0.1:8080/healthz
```

The service binds to loopback, runs as an unprivileged account and can write only to its data directory and private temporary storage. Application files belong to root. Do not run two server processes against the same data directory. Check service logs and a new-book save after each upgrade; roll back application files separately from household data.

The default service grants no write access to a backup folder. The separate backup command/timer owns backup writes; `-backup-dir` adds read-only backup health to the app. Keep backup folders readable by `home-library`. See [LAN_README.md](LAN_README.md) for authenticated network access; do not expose the loopback service directly.

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

Choose a fresh output directory for every run. Copy verified backups to another device. See `BACKUP_README.md` for restore to a new directory and testing on a separate port. The frontend ships with the application; thumbnails are derived data that can be regenerated. Settings now displays read-only backup verification/freshness; browser restore remains future work.

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
sudo install -d -m 755 -o root -g root /var/lib/home-library/app/deploy
sudo install -m 755 deploy/backup-run.sh /var/lib/home-library/app/deploy/backup-run.sh
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

The static runtime and packaging checks run on development equipment. This change has not been deployed to the existing Pi by these instructions. Verify on the actual device: service permissions, cold boot and offline startup, new-book/edit/cover workflows without Node, direct book URLs, scanner focus/Enter behavior, backup and restore, and the existing gateway/update flow.

An ARM64 cross-build proves compilation, not hardware behavior. Keep household data separate from application files and preserve a verified backup before changing an existing installation.
