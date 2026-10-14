#!/usr/bin/env python3
"""One-time administrator setup. Preserve the live catalogue and TLS configuration."""
import json, os, pathlib, shutil, subprocess, sys
from updates_common import atomic_json, verify, extract

assert os.geteuid()==0,'Run with sudo'
stage=pathlib.Path(__file__).resolve().parent
root=pathlib.Path('/var/lib/library-updater');state=root/'state';releases=pathlib.Path('/opt/home-library/releases')
if root.exists():
 # Resume only an interrupted unpack before any live configuration changed.
 assert not pathlib.Path('/opt/home-library/current').exists(), 'Updater already installed'
 assert not pathlib.Path('/etc/systemd/system/home-library.service.d/updates.conf').exists(), 'Updater configuration already installed'
 assert not (state/'original-dropins').exists() and not (state/'maintenance.json').exists() and not (state/'status.json').exists(), 'Bootstrap already changed live state'
 assert not root.is_symlink() and root.stat().st_uid==0, 'Invalid bootstrap directory'
 for name in ['requests','downloads']:
  assert not (root/name).is_symlink() and not any((root/name).iterdir()), 'Bootstrap requests already exist'
 partial=releases/'v0.1.0'
 if partial.exists():
  assert not partial.is_symlink() and partial.stat().st_uid==0, 'Invalid partial release'
  shutil.rmtree(partial)
 print('Resuming interrupted unpack; the original application is unchanged.',flush=True)
subprocess.run(['sha256sum','-c','bootstrap-SHA256SUMS'],cwd=stage,check=True)
manifest=verify(stage/'manifest.json',stage/'manifest.sig',stage/'public.pem',stage/'release-verify')
assert manifest['version']=='v0.1.0'
for name in ['library-updater','library-smoke']:
 if subprocess.run(['id',name],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL).returncode:
  subprocess.run(['/usr/sbin/useradd','--system','--user-group','--home-dir','/nonexistent','--shell','/usr/sbin/nologin',name],check=True)
for p in [root,state,root/'requests',root/'downloads',releases,pathlib.Path('/var/cache/home-library'),pathlib.Path('/var/backups/home-library-updates'),pathlib.Path('/etc/home-library-updater'),pathlib.Path('/usr/local/libexec/home-library-updater')]:p.mkdir(parents=True,mode=0o755,exist_ok=True)
for n in ['requests','downloads']:
 shutil.chown(root/n,user='library-updater',group='library-gateway');(root/n).chmod(0o700)
for name in ['updates_common.py','update-api.py','update-activate.py','release-verify','start-managed-library.sh']:
 shutil.copyfile(stage/name,pathlib.Path('/usr/local/libexec/home-library-updater')/name)
 os.chmod(pathlib.Path('/usr/local/libexec/home-library-updater')/name,0o755 if name in ['release-verify','start-managed-library.sh'] else 0o644)
shutil.copyfile(stage/'public.pem','/etc/home-library-updater/public.pem')
atomic_json('/etc/home-library-updater/config.json',{'repository':'pageboy-za/library'})
candidate=releases/manifest['version'];extract(stage/'bundle.tar.gz',manifest,candidate)
atomic_json(candidate/'RELEASE.json',{'version':manifest['version'],'commit':manifest['commit']})
cache=pathlib.Path('/var/cache/home-library')/manifest['version'];cache.mkdir(mode=0o755)
for n in ['types','vite','astro-cache']:
 (cache/n).mkdir(mode=0o700);shutil.chown(cache/n,user='home-library',group='home-library')
(candidate/'web/.astro').symlink_to(cache/'types');(candidate/'web/node_modules/.vite').symlink_to(cache/'vite')
# Keep all original service files for manual recovery during this one-time change.
shutil.copytree('/etc/systemd/system/home-library.service.d',state/'original-dropins')
shutil.copyfile('/etc/home-library-gateway/Caddyfile',state/'original-Caddyfile')
atomic_json(state/'maintenance.json',{'bootstrap':True})
try:
 subprocess.run(['systemctl','stop','home-library.service'],check=True)
 backup=pathlib.Path('/var/backups/home-library-updates/bootstrap');backup.mkdir(mode=0o700);shutil.chown(backup,user='home-library',group='home-library')
 for action in ['create','verify']:
  args=['/usr/sbin/runuser','-u','home-library','--','/var/lib/home-library/app/bin/library-backup',action]
  args+=['-data','/var/lib/home-library/data','-out',str(backup/'snapshot')] if action=='create' else ['-backup',str(backup/'snapshot')]
  subprocess.run(args,check=True)
 pathlib.Path('/opt/home-library/current').symlink_to(candidate)
 pathlib.Path('/etc/systemd/system/home-library.service.d/updates.conf').write_text('[Service]\nExecStart=\nExecStart=/usr/local/libexec/home-library-updater/start-managed-library.sh\nReadWritePaths=/var/cache/home-library\nTimeoutStopSec=200\n')
 for name in ['home-library-update-api.service','home-library-update.service','home-library-update.path']:
  shutil.copyfile(stage/name,pathlib.Path('/etc/systemd/system')/name)
 shutil.copyfile(stage/'Caddyfile.lan.example','/etc/home-library-gateway/Caddyfile')
 subprocess.run(['systemctl','daemon-reload'],check=True)
 subprocess.run(['systemctl','enable','--now','home-library-update-api.service','home-library-update.path'],check=True)
 subprocess.run(['systemctl','enable','home-library-update.service'],check=True)
 subprocess.run(['systemctl','restart','home-library-gateway.service','home-library.service'],check=True)
 sys.path.insert(0,'/usr/local/libexec/home-library-updater')
 from importlib.machinery import SourceFileLoader
 activation=SourceFileLoader('activation','/usr/local/libexec/home-library-updater/update-activate.py').load_module()
 if not activation.ready('v0.1.0'):
  print('Bootstrap did not become ready; restoring the original application configuration.',file=sys.stderr)
  pathlib.Path('/etc/systemd/system/home-library.service.d/updates.conf').unlink()
  shutil.copyfile(state/'original-Caddyfile','/etc/home-library-gateway/Caddyfile')
  subprocess.run(['systemctl','daemon-reload'],check=True)
  subprocess.run(['systemctl','restart','home-library.service','home-library-gateway.service'],check=True)
  raise SystemExit(1)
 (state/'maintenance.json').unlink()
 atomic_json(state/'status.json',{'phase':'ready','version':'v0.1.0','message':'Remote updates are ready.'})
 print('Remote updates installed. Open Settings > Application updates.')
except BaseException:
 print('Restoring the original services after a bootstrap failure.',file=sys.stderr)
 override=pathlib.Path('/etc/systemd/system/home-library.service.d/updates.conf')
 override.unlink(missing_ok=True)
 if (state/'original-Caddyfile').exists():shutil.copyfile(state/'original-Caddyfile','/etc/home-library-gateway/Caddyfile')
 subprocess.run(['systemctl','disable','--now','home-library-update-api.service','home-library-update.path','home-library-update.service'],check=False)
 subprocess.run(['systemctl','daemon-reload'],check=False)
 subprocess.run(['systemctl','restart','home-library.service','home-library-gateway.service'],check=False)
 raise
