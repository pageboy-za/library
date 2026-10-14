#!/bin/sh
# Portable manual/daily runner. No retention deletion or scheduling is installed.
set -eu
umask 077

usage() {
  echo "Usage: backup-run.sh --binary /path/library-backup --data /path/data --destination /durable/backups" >&2
  exit 64
}
backup_binary=''
source_data=''
backup_destination=''
while [ "$#" -gt 0 ]; do
  [ "$#" -ge 2 ] || usage
  case "$1" in
    --binary) backup_binary=$2 ;;
    --data) source_data=$2 ;;
    --destination) backup_destination=$2 ;;
    *) usage ;;
  esac
  shift 2
done
[ -n "$backup_binary" ] && [ -n "$source_data" ] && [ -n "$backup_destination" ] || usage
case "$backup_binary" in /*) ;; *) echo "Choose an absolute backup binary path." >&2; exit 64 ;; esac
case "$source_data" in /*) ;; *) echo "Choose an absolute data path." >&2; exit 64 ;; esac
case "$backup_destination" in /*) ;; *) echo "Choose an absolute backup destination." >&2; exit 64 ;; esac
[ -x "$backup_binary" ] || { echo "Backup binary is missing or not executable." >&2; exit 69; }
[ -f "$source_data/library.sqlite" ] || { echo "Source library database was not found." >&2; exit 66; }
[ -d "$backup_destination" ] && [ -w "$backup_destination" ] || { echo "Choose an existing writable durable backup folder." >&2; exit 73; }

lock_directory=$backup_destination/.library-backup.lock
if ! mkdir "$lock_directory" 2>/dev/null; then
  echo "Backup is already running, or a stale backup lock needs checking: $lock_directory" >&2
  exit 75
fi
cleanup() {
  rm -f "$lock_directory/pid"
  rmdir "$lock_directory" 2>/dev/null || true
}
trap cleanup 0
trap 'exit 130' INT
trap 'exit 143' TERM HUP
printf '%s\n' "$$" > "$lock_directory/pid"
backup_stamp=$(date -u +%Y%m%dT%H%M%SZ)
backup_output=$backup_destination/library-$backup_stamp-$$
if ! "$backup_binary" create -data "$source_data" -out "$backup_output"; then
  echo "Backup failed. Check available disk space and permissions; no older backups were removed." >&2
  exit 74
fi
if ! "$backup_binary" verify -backup "$backup_output"; then
  echo "Backup verification failed: $backup_output. Do not use this copy to restore." >&2
  exit 74
fi
echo "Verified backup: $backup_output"
