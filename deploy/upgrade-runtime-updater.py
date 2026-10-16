#!/usr/bin/env python3
"""Refresh fixed updater helpers before installing static-v1 releases.

Run from a reviewed checkout: sudo python3 deploy/upgrade-runtime-updater.py
This does not activate an application release or change catalogue files.
"""
import fcntl, os, pathlib, subprocess, tempfile

if os.geteuid() != 0: raise SystemExit('Run with sudo')
stage = pathlib.Path(__file__).resolve().parent
installed = pathlib.Path('/usr/local/libexec/home-library-updater')
state = pathlib.Path('/var/lib/library-updater/state')
if not (installed.is_dir() and not installed.is_symlink() and installed.stat().st_uid == 0 and (state/'activation.lock').is_file()):
    raise SystemExit('Managed updater must already be installed')
helpers = ['updates_common.py', 'update-activate.py', 'update-api.py', 'start-managed-library.sh']
for name in helpers:
    source = stage/name
    if not source.is_file() or source.is_symlink(): raise SystemExit('Missing regular helper: '+name)
    compile(source.read_text(), str(source), 'exec') if name.endswith('.py') else subprocess.run(['/bin/sh', '-n', str(source)], check=True)
services = ['home-library-update.path', 'home-library-update-api.service']
active = [name for name in services if subprocess.run(['systemctl', 'is-active', '--quiet', name]).returncode == 0]
# Prevent new requests and activation while replacing the fixed helper set.
subprocess.run(['systemctl', 'stop', *services], check=True)
try:
    with (state/'activation.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if pathlib.Path('/var/lib/library-updater/requests/ready.json').exists():
            raise SystemExit('Finish the queued update before refreshing helpers')
        originals = {name: ((installed/name).read_bytes(), (installed/name).stat().st_mode & 0o777) for name in helpers}
        staged = {}
        try:
            for name in helpers:
                fd, temporary = tempfile.mkstemp(prefix='.upgrade-', dir=installed)
                try:
                    with os.fdopen(fd, 'wb') as output:
                        output.write((stage/name).read_bytes()); output.flush(); os.fsync(output.fileno())
                    os.chmod(temporary, 0o755 if name.endswith('.sh') else 0o644)
                    os.chown(temporary, 0, 0)
                    staged[name] = temporary
                except BaseException:
                    os.unlink(temporary); raise
            for name, temporary in staged.items(): os.replace(temporary, installed/name)
        except BaseException:
            # Do not leave a mixed helper set if a normal filesystem operation fails.
            for name, (content, mode) in originals.items():
                fd, temporary = tempfile.mkstemp(prefix='.restore-', dir=installed)
                try:
                    with os.fdopen(fd, 'wb') as output:
                        output.write(content); output.flush(); os.fsync(output.fileno())
                    os.chmod(temporary, mode); os.chown(temporary, 0, 0)
                    os.replace(temporary, installed/name)
                finally:
                    if os.path.exists(temporary): os.unlink(temporary)
            raise
        finally:
            for temporary in staged.values():
                if os.path.exists(temporary): os.unlink(temporary)
        directory = os.open(installed, os.O_DIRECTORY)
        try: os.fsync(directory)
        finally: os.close(directory)
finally:
    if active: subprocess.run(['systemctl', 'start', *active], check=True)
print('Updater helpers refreshed. The running library and catalogue are unchanged; static releases can now be installed.')
