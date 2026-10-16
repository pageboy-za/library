#!/usr/bin/env python3
"""Explicit administrator attestation; never installs or rewrites a gateway.

Review the installed gateway first, then run this helper with its acknowledgement
flag. Restart the gateway to ensure its running process uses the reviewed bytes.
"""
import argparse, hashlib, os, pathlib, stat, subprocess
from updates_common import atomic_json

p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--acknowledge-reviewed-gateway',action='store_true',required=True,help='Confirm the installed TLS/basic/origin boundary and staff forward_auth ordering have been reviewed')
p.parse_args()
if os.geteuid()!=0:raise SystemExit('Run as administrator with sudo')
config=pathlib.Path('/etc/home-library-gateway/Caddyfile')
info=config.lstat()
if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_mode&0o022:raise SystemExit('Gateway config must be a root-owned regular file without group/world write access')
raw=config.read_bytes()
if b'/api/auth/authorize-update' not in raw or b'forward_auth' not in raw:raise SystemExit('Missing staff updater authorization; review the template')
# The gateway service loads the private login environment itself. Adapt only
# verifies syntax here with disposable placeholders; no real credentials read.
subprocess.run(['/usr/local/bin/library-caddy','adapt','--config',str(config),'--adapter','caddyfile'],env={**os.environ,'LIBRARY_LOGIN_USER':'fixture','LIBRARY_LOGIN_HASH':'fixture'},check=True,stdout=subprocess.DEVNULL)
subprocess.run(['systemctl','restart','home-library-gateway.service'],check=True)
subprocess.run(['systemctl','is-active','--quiet','home-library-gateway.service'],check=True)
if config.read_bytes()!=raw:raise SystemExit('Gateway config changed during verification; repeat review')
atomic_json(config.with_name('staff-permissions.json'),dict(format=1,capability='staff-update-authorization-v1',config_sha256=hashlib.sha256(raw).hexdigest()))
print('Reviewed staff gateway restarted and recorded. Schema 2 updater prerequisite is ready.')
