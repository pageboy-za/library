#!/bin/sh
# Run as the logged-in desktop user; the server uses a separate service account.
set -eu
if [ -z "${DISPLAY:-}" ] && [ -z "${WAYLAND_DISPLAY:-}" ]; then
  echo 'Run this launcher inside the Raspberry Pi desktop session.' >&2
  exit 1
fi
library_browser=''
for library_candidate in chromium chromium-browser; do
  if command -v "$library_candidate" >/dev/null 2>&1; then
    library_browser=$(command -v "$library_candidate")
    break
  fi
done
if [ -z "$library_browser" ]; then
  echo 'Install Chromium before starting the kiosk.' >&2
  exit 1
fi
if ! command -v curl >/dev/null 2>&1; then
  echo 'Install curl so the kiosk can wait for library readiness.' >&2
  exit 1
fi
library_attempt=0
until curl --fail --silent --max-time 2 http://127.0.0.1:8080/healthz >/dev/null; do
  library_attempt=$((library_attempt + 1))
  if [ "$library_attempt" -ge 60 ]; then
    echo 'Library is not ready. Check: systemctl status home-library' >&2
    exit 1
  fi
  sleep 2
done
# Use a separate persistent browser profile; do not modify the user's normal browser.
library_profile=${XDG_STATE_HOME:-"$HOME/.local/state"}/home-library/chromium
mkdir -p "$library_profile"
exec "$library_browser" --user-data-dir="$library_profile" --kiosk --no-first-run --no-default-browser-check http://127.0.0.1:8080/
