#!/bin/sh
# Disposable runner fixtures; never touches household data or installs services.
set -eu
script_directory=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
fixture_directory=$(mktemp -d "${TMPDIR:-/tmp}/library-backup-runner.XXXXXX")
trap 'rm -rf "$fixture_directory"' 0
trap 'exit 130' INT
trap 'exit 143' TERM HUP
mkdir "$fixture_directory/data with spaces" "$fixture_directory/backups with spaces"
: > "$fixture_directory/data with spaces/library.sqlite"
mkdir "$fixture_directory/backups with spaces/older-backup"
: > "$fixture_directory/backups with spaces/older-backup/keep"
export FAKE_BACKUP_LOG=$fixture_directory/calls
cat > "$fixture_directory/fake-backup" <<'FAKE'
#!/bin/sh
set -eu
printf '%s\n' "$1" >> "$FAKE_BACKUP_LOG"
if [ "${FAKE_BACKUP_FAIL:-}" = "$1" ]; then echo 'Simulated disk full or verification failure' >&2; exit 1; fi
case "$1" in
  create)
    shift
    output=''
    while [ "$#" -gt 0 ]; do
      case "$1" in -out) output=$2 ;; -data) ;; *) exit 64 ;; esac
      shift 2
    done
    mkdir "$output"
    if [ -n "${FAKE_BACKUP_WAIT:-}" ]; then
      : > "$FAKE_BACKUP_WAIT.ready"
      count=0
      while [ ! -f "$FAKE_BACKUP_WAIT.release" ]; do
        count=$((count + 1))
        [ "$count" -le 5 ] || exit 1
        sleep 1
      done
    fi
    ;;
  verify) [ "$2" = '-backup' ] && [ -d "$3" ] ;;
  *) exit 64 ;;
esac
FAKE
chmod +x "$fixture_directory/fake-backup"
run_backup() {
  "$script_directory/backup-run.sh" --binary "$fixture_directory/fake-backup" --data "$fixture_directory/data with spaces" --destination "$fixture_directory/backups with spaces"
}
sh -n "$script_directory/backup-run.sh"
run_backup
[ "$(cat "$FAKE_BACKUP_LOG")" = "$(printf 'create\nverify')" ]
[ ! -e "$fixture_directory/backups with spaces/.library-backup.lock" ]
for failure in create verify; do
  export FAKE_BACKUP_FAIL=$failure
  set +e
  run_backup > "$fixture_directory/failure-log" 2>&1
  failure_status=$?
  set -e
  [ "$failure_status" -eq 74 ]
  [ ! -e "$fixture_directory/backups with spaces/.library-backup.lock" ]
  [ -f "$fixture_directory/backups with spaces/older-backup/keep" ]
done
unset FAKE_BACKUP_FAIL
export FAKE_BACKUP_WAIT=$fixture_directory/wait
run_backup > "$fixture_directory/first-run-log" 2>&1 &
first_pid=$!
for attempt in 1 2 3 4 5; do
  [ ! -f "$FAKE_BACKUP_WAIT.ready" ] || break
  sleep 1
done
[ -f "$FAKE_BACKUP_WAIT.ready" ]
set +e
run_backup > "$fixture_directory/concurrent-log" 2>&1
concurrent_status=$?
set -e
[ "$concurrent_status" -eq 75 ]
: > "$FAKE_BACKUP_WAIT.release"
wait "$first_pid"
[ ! -e "$fixture_directory/backups with spaces/.library-backup.lock" ]
[ -f "$fixture_directory/backups with spaces/older-backup/keep" ]
echo 'Backup runner syntax, quoted paths, verified success, failure cleanup, no retention deletion and concurrent locking passed.'
