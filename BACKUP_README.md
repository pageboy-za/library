# Backup, export and restore

Build the standalone backup command:

```sh
go build -o bin/library-backup ./cmd/library-backup
```

From the project directory, create a backup in a **new** directory:

```sh
./bin/library-backup create -data data -out /path/to/backups/library-2026-10-10
./bin/library-backup verify -backup /path/to/backups/library-2026-10-10
```

The parent destination folder must already exist. The final backup folder must
not exist, even if empty. Store a copy on another device for protection against
failure of the development machine or Raspberry Pi.

## What is saved

- `library.sqlite`: consistent database snapshot, including committed changes
  still in the live WAL file.
- `covers/`: every cover image referenced by that snapshot, including uploads.
- `catalogue.json`: portable JSON export of catalogue, copies, bookcases,
  shelves, borrowers, loans and other user-data tables. IDs and relationships
  are preserved; author/tag/subject JSON columns become JSON values. Internal
  mutation request keys and the derived FTS search index/shadow tables are excluded from this export.
- `manifest.json`: schema version and SHA-256 checksums/lengths of every file.

The app may continue running during backup. Image files are immutable and saved
before their database associations, so cover files can be copied after the
database snapshot. A missing or damaged referenced image fails the backup.
The manifest is written last; interrupted or incomplete backups fail verification.

Location revision tombstones are included in the database snapshot and export, so stale location screens remain rejected after restoration. The SQL snapshot retains the derived FTS search index; its triggers maintain it, and startup backfills a missing index or repairs outdated trigger definitions. JSON exports omit it. Cover thumbnails and provider metadata lookup caches are derived data and can be recreated.

Static pages, frontend dependencies and application binaries are generated or
installed separately and are not user data. They are not included.

## Restore safely

```sh
./bin/library-backup restore -backup /path/to/backups/library-2026-10-10 -out data-restored
./bin/library -data data-restored -listen 127.0.0.1:8082
```

Restore verifies all files and SQLite integrity and foreign-key relationships before creating the destination.
It refuses to overwrite an existing directory. Check the restored catalogue and
covers on port 8082 before choosing it as your main data directory. Static pages
regenerate through the normal application publishing process.

Checksums detect accidental corruption, not deliberate modification by someone
who can rewrite both files and the manifest. Backups contain private borrower
details; keep their storage private. The Settings screen shows read-only backup health; JSON import and browser restore are not included. Optional scheduling templates below
remain disabled until you choose a durable destination and install them.

## Daily runner and optional Pi scheduling

Choose an existing private folder on another disk, then run:

```sh
./deploy/backup-run.sh --binary /absolute/project/bin/library-backup --data /absolute/project/data --destination /absolute/durable/backups
```

The portable runner creates a unique UTC timestamp/PID directory, runs `create`
and `verify`, and uses an atomic directory lock to prevent concurrent runs into
the same destination. It never deletes older backup directories. Missing paths,
permission errors, full disks, failed creation and failed verification produce
a nonzero exit. The caller sees the backup utility's error and a short runner
message. Inspect the command output or system journal after failures.

An interrupted machine can leave `.library-backup.lock`. Check its `pid` file
and confirm no backup is running before removing the stale `pid` file and empty
lock directory manually. A backup without a valid manifest is incomplete; do
not restore it. Verify important backups again after copying to another disk.

`deploy/backup-home-library.service.example` and
`deploy/backup-home-library.timer.example` are **templates only**. They are not
installed or enabled. See [Pi setup](PI_README.md#optional-daily-backup-timer)
for deliberate installation after choosing a mounted, durable backup disk.
The sample timer runs daily at 03:00 in the Pi's local timezone, with up to ten
minutes of delay. A missed run is caught up after the timer becomes active.
The sample service skips when its configured backup disk is not mounted; check
the journal when reconnecting it. No automatic retention deletion or failure notification is supplied. Settings checks saved backup folders rather than operating-system scheduler outcomes.

Scheduling follows the official [systemd timer documentation](https://github.com/systemd/systemd/blob/main/man/systemd.timer.xml)
and [oneshot service documentation](https://github.com/systemd/systemd/blob/main/man/systemd.service.xml).

Run disposable runner checks with `./deploy/backup-test.sh`. They cover quoted
paths, verified success, simulated disk-full/verification failures, lock cleanup,
concurrent runs and preservation of older backups. A separate disposable SQLite
fixture was backed up and verified with the real CLI on the development Mac.
The systemd examples still require validation on the chosen Pi; no service or
timer was installed here.

## Implementation

The backup command opens a dedicated read-only SQLite connection and uses
[`VACUUM INTO`](https://sqlite.org/lang_vacuum.html#vacuum_with_an_into_clause)
for a consistent live snapshot. It never copies only the active main database
file, which could omit committed WAL changes. Verification checks the manifest,
SQLite `quick_check`, foreign-key integrity, schema version and database-to-cover checksums.

## Read-only backup status

After choosing a durable backup parent folder, start the app with `-backup-dir /absolute/durable/backups`. Open Settings to check the newest saved attempt and latest verified backup. Leaving the flag unset reports that monitoring is not configured; it does not discover or treat temporary development snapshots as durable protection. No folder picker, backup writes, restore, scheduling or deletion is exposed by this screen.

Status checks checksums, SQLite integrity/schema, foreign-key relationships and referenced covers. A damaged newest backup remains a warning even if an older backup verifies. A verified backup over48hours old is marked stale. Missing disks, unsafe paths and incomplete folders fail conservatively. The runner internal lock is excluded from folder counts. Creation dates describe saved snapshots, not when every scheduler run last attempted a backup; check the system journal for failed/skipped scheduled runs.

The dashboard caches results for30seconds and verifies at most the newest plus two older candidates. It scans at most1000directory entries and limits manifests to64KiB,2000files,32MiBperfile,128MiBtotal and8seconds. Larger valid backups require the full CLI verification workflow; a dashboard limit is not proof of corruption. Canonical real paths are required: symlink roots/ancestors, candidate entries and file paths are rejected. Status responses contain no filesystem paths or raw logs.
