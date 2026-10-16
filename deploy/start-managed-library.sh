#!/bin/sh
set -eu
library_release=$(readlink -f /opt/home-library/current)
# Legacy signed releases still need their bundled Node executable for rollback.
export PATH="/usr/local/bin:/usr/bin:/bin"
if [ -x "$library_release/runtime/bin/node" ]; then
 export PATH="$library_release/runtime/bin:$PATH"
 export LIBRARY_CACHE_DIR="/var/cache/home-library/$(basename "$library_release")"
 export NODE_OPTIONS=--max-old-space-size=384
fi
export LIBRARY_APP_VERSION="$(basename "$library_release")"
export GOMEMLIMIT=128MiB GOMAXPROCS=2
exec "$library_release/bin/library" -listen 127.0.0.1:8080 -web "$library_release/web" -data /var/lib/home-library/data -maintenance-file /var/lib/library-updater/state/maintenance.json
