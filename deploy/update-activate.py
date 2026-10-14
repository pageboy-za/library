#!/usr/bin/env python3
"""Fixed root activation job. It never runs release content as root."""
import fcntl, json, os, pathlib, pwd, shutil, subprocess, tempfile, time, urllib.request, uuid
from html.parser import HTMLParser
from updates_common import VERSION, MAX_BUNDLE, atomic_json, read_json, verify, extract, safe_copy, check_bundle, check_existing

ROOT=pathlib.Path('/var/lib/library-updater');STATE=ROOT/'state'
RELEASES=pathlib.Path('/opt/home-library/releases');CURRENT=pathlib.Path('/opt/home-library/current')
DATA=pathlib.Path('/var/lib/home-library/data');BACKUPS=pathlib.Path('/var/backups/home-library-updates')
VERIFIER='/usr/local/libexec/home-library-updater/release-verify';KEY='/etc/home-library-updater/public.pem'
CLIENT=urllib.request.build_opener(urllib.request.ProxyHandler({}))

def system(*args):subprocess.run(['/usr/bin/systemctl',*args],check=True,timeout=200)
def as_app(*args):subprocess.run(['/usr/sbin/runuser','-u','home-library','--',*map(str,args)],check=True,timeout=300)
def switch(path):
 tmp=CURRENT.with_name('next');tmp.unlink(missing_ok=True);tmp.symlink_to(path);os.replace(tmp,CURRENT)
 fd=os.open(CURRENT.parent,os.O_DIRECTORY);os.fsync(fd);os.close(fd)
class PageVersion(HTMLParser):
 def __init__(self):super().__init__();self.version=None
 def handle_starttag(self,tag,attrs):
  attributes=dict(attrs)
  if tag=='meta' and attributes.get('name')=='library-app-version':self.version=attributes.get('content')

def ready(version,timeout=180,port=8080):
 deadline=time.monotonic()+timeout
 while time.monotonic()<deadline:
  try:
   with CLIENT.open(f'http://127.0.0.1:{port}/api/version',timeout=4) as r:v=json.load(r)
   with CLIENT.open(f'http://127.0.0.1:{port}/healthz',timeout=4) as r:h=json.load(r)
   with CLIENT.open(f'http://127.0.0.1:{port}/',timeout=4) as r:page=r.read(1<<20)
   with CLIENT.open(f'http://127.0.0.1:{port}/api/books',timeout=4) as r:json.load(r)
   with CLIENT.open(f'http://127.0.0.1:{port}/settings/',timeout=4) as r:settings=r.read(1<<20)
   parser=PageVersion();parser.feed(page.decode('utf-8'))
   current_pages=(version=='v0.1.0' or parser.version==version)
   if v.get('version')==version and h.get('status')=='ready' and b'kiosk-search' in page and b'Application updates' in settings and current_pages:return True
  except Exception:pass
  time.sleep(2)
 return False

def recover(journal):
 previous=pathlib.Path(journal['previous'])
 if previous.parent!=RELEASES or not VERSION.fullmatch(previous.name) or not previous.is_dir():raise ValueError('Invalid recovery release')
 atomic_json(STATE/'maintenance.json',{'job':journal['job']})
 atomic_json(STATE/'status.json',{**journal,'phase':'recovering','message':'Restoring the previous application release.'})
 system('stop','home-library.service');switch(previous);system('start','home-library.service')
 if not ready(previous.name):raise RuntimeError('Previous release is not ready')
 (STATE/'maintenance.json').unlink(missing_ok=True)
 atomic_json(STATE/'status.json',{**journal,'phase':'recovered','message':'Update failed; the previous application is running. Your catalogue was not replaced.'})

def activate():
 pending=ROOT/'requests/ready.json';journal=read_json(STATE/'status.json')
 if journal.get('phase') in ['preparing','backing_up','restarting','recovering']:
  recover(journal)
  journal=read_json(STATE/'status.json')
 if not pending.exists():
  if journal.get('phase') in ['preparing','backing_up','restarting','recovering']:recover(journal)
  return
 request=read_json(pending)
 job=str(uuid.UUID(request['job']));version=request['version']
 if not VERSION.fullmatch(version):raise ValueError('Invalid version')
 if journal.get('job')==job and journal.get('phase') in ['ready','recovered','failed']:
  pending.unlink();return
 previous=CURRENT.resolve();assert previous.parent==RELEASES and VERSION.fullmatch(previous.name)
 journal=dict(job=job,version=version,previous=str(previous),phase='preparing')
 atomic_json(STATE/'status.json',journal)
 # Consume only after the recovery journal is durable.
 pending.unlink()
 candidate=RELEASES/version
 created=False
 try:
  with tempfile.TemporaryDirectory(dir=STATE,prefix='verify-') as temp:
   secure=pathlib.Path(temp);source=ROOT/'downloads'/job
   for name,limit in [('manifest.json',16<<20),('manifest.sig',1024),('bundle.tar.gz',MAX_BUNDLE)]:safe_copy(source/name,secure/name,limit)
   manifest=verify(secure/'manifest.json',secure/'manifest.sig',KEY,VERIFIER)
   if manifest['version']!=version:raise ValueError('Version mismatch')
   check_bundle(secure/'bundle.tar.gz',manifest)
   if candidate.exists():check_existing(candidate,manifest)
   else:
    extract(secure/'bundle.tar.gz',manifest,candidate);created=True
    atomic_json(candidate/'RELEASE.json',{'version':version,'commit':manifest['commit']})
  # Cache links are created by this fixed helper, never accepted from the archive.
  cache=pathlib.Path('/var/cache/home-library')/version
  cache.mkdir(mode=0o755,exist_ok=True)
  for name in ['types','vite','astro-cache']:
   target=cache/name
   if target.is_symlink():raise ValueError('Invalid cache directory')
   target.mkdir(mode=0o700,exist_ok=True);target.chmod(0o700);shutil.chown(target,user='home-library',group='home-library')
  types=DATA/'build-cache'/version/'types'
  as_app('/usr/bin/mkdir','-p','-m','700',types)
  for target,link in [(types,candidate/'web/.astro'),(cache/'vite',candidate/'web/node_modules/.vite')]:
   if link.is_symlink():
    if os.readlink(link)!=str(target):raise ValueError('Invalid cache link')
   elif link.exists():raise ValueError('Cache path is not a managed link')
   else:link.symlink_to(target)
  smoke=cache/'smoke';smoke.mkdir(mode=0o700);shutil.chown(smoke,user='library-smoke',group='home-library')
  # Use protected hard-linked source with independent, private test caches.
  # The smoke account cannot traverse the live catalogue or its cache contents.
  shutil.copytree(candidate/'web',smoke/'web',symlinks=True,copy_function=os.link)
  test_cache=smoke/'cache';test_cache.mkdir(mode=0o700);shutil.chown(test_cache,user='library-smoke',group='library-smoke')
  for name,link in [('types',smoke/'web/.astro'),('vite',smoke/'web/node_modules/.vite')]:
   link.unlink();target=test_cache/name;target.mkdir(mode=0o700);shutil.chown(target,user='library-smoke',group='library-smoke');link.symlink_to(target)
  env={**os.environ,'PATH':str(candidate/'runtime/bin')+':/usr/bin:/bin','HOME':str(smoke),'LIBRARY_CACHE_DIR':str(test_cache),'NODE_OPTIONS':'--max-old-space-size=384'}
  process=subprocess.Popen(['/usr/sbin/runuser','-u','library-smoke','--',str(candidate/'bin/library'),'-listen','127.0.0.1:18081','-data',str(smoke/'data'),'-web',str(smoke/'web')],env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
  try:
   if not ready(version,port=18081):raise RuntimeError('Candidate smoke test failed')
  finally:
   process.terminate()
   try:process.wait(timeout=20)
   except subprocess.TimeoutExpired:process.kill();process.wait(timeout=10)
   shutil.rmtree(smoke)
  # Stop new mutations before draining existing HTTP writes.
  atomic_json(STATE/'maintenance.json',{'job':job})
  deadline=time.monotonic()+190
  while time.monotonic()<deadline:
   try:
    with CLIENT.open('http://127.0.0.1:8080/api/version',timeout=5) as r:info=json.load(r)
    if info.get('active_writes',0)==0:break
   except Exception:break
   time.sleep(1)
  else:raise RuntimeError('Writes did not drain')
  system('stop','home-library.service')
  journal['phase']='backing_up';atomic_json(STATE/'status.json',journal)
  parent=BACKUPS/job;parent.mkdir(mode=0o700);shutil.chown(parent,user='home-library',group='home-library')
  backup=parent/'snapshot'
  as_app(previous/'bin/library-backup','create','-data',DATA,'-out',backup)
  as_app(previous/'bin/library-backup','verify','-backup',backup)
  journal['phase']='restarting';atomic_json(STATE/'status.json',journal)
  switch(candidate);system('start','home-library.service')
  if not ready(version):raise RuntimeError('Candidate did not become ready')
  (STATE/'maintenance.json').unlink(missing_ok=True)
  atomic_json(STATE/'status.json',{**journal,'phase':'ready','message':'Update installed. Your library is ready.'})
 except Exception as error:
  print('Activation failed:',type(error).__name__,flush=True)
  if (STATE/'maintenance.json').exists():recover(journal)
  else:atomic_json(STATE/'status.json',{**journal,'phase':'failed','message':'Release rejected before activation. The current application is unchanged.'})
  if created and candidate.exists() and candidate.resolve()!=CURRENT.resolve() and candidate!=previous:
   shutil.rmtree(candidate)
   unused=pathlib.Path('/var/cache/home-library')/version
   if unused.exists():shutil.rmtree(unused)

if __name__=='__main__':
 assert os.geteuid()==0
 with open(STATE/'activation.lock','w') as lock:
  fcntl.flock(lock,fcntl.LOCK_EX)
  try:activate()
  except Exception as error:
   print('Recovery needs attention:',type(error).__name__,flush=True)
   old=read_json(STATE/'status.json');atomic_json(STATE/'status.json',{**old,'phase':'needs_attention','message':'Update recovery needs attention. Writes remain paused.'})
   raise
