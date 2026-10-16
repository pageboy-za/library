"""Shared release verification. This installed module is root-owned."""
import ctypes, hashlib, json, os, pathlib, posixpath, re, shutil, stat, subprocess, sys, tarfile, tempfile

VERSION=re.compile(r'v\d+\.\d+\.\d+\Z')
MAX_BUNDLE=512<<20

def atomic_json(path,value):
 path=pathlib.Path(path);path.parent.mkdir(parents=True,exist_ok=True)
 fd,tmp=tempfile.mkstemp(dir=path.parent,prefix='.write-')
 try:
  with os.fdopen(fd,'w') as f:json.dump(value,f);f.flush();os.fsync(f.fileno())
  os.chmod(tmp,0o644);os.replace(tmp,path)
  d=os.open(path.parent,os.O_DIRECTORY);os.fsync(d);os.close(d)
 finally:
  if os.path.exists(tmp):os.unlink(tmp)

def read_json(path):
 try:return json.loads(pathlib.Path(path).read_text())
 except FileNotFoundError:return {}

def verify(manifest,signature,key,verifier):
 subprocess.run([str(verifier),'-action','verify','-key',str(key),'-manifest',str(manifest),'-signature',str(signature)],check=True,timeout=30,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 m=read_json(manifest)
 if m.get('format')!=1 or not VERSION.fullmatch(m.get('version','')) or m.get('arch')!='linux-arm64' or m.get('schema')!=1 or m.get('rollback_compatible') is not True:raise ValueError('Incompatible release')
 if not re.fullmatch('[0-9a-f]{40}',m.get('commit','')) or not re.fullmatch('[0-9a-f]{64}',m.get('bundle_sha256','')):raise ValueError('Invalid release identity')
 if not isinstance(m.get('files'),list) or len(m['files'])>50000:raise ValueError('Invalid file list')
 if 'runtime' in m and m['runtime']!='static-v1':raise ValueError('Unsupported release runtime')
 names=set();links=set();size=0
 for f in m['files']:
  n=f['path']
  if not isinstance(n,str) or '\\' in n or posixpath.normpath(n)!=n or n.startswith('/') or n.split('/')[0] not in ['bin','web','runtime'] or '..' in n.split('/') or n in names:raise ValueError('Unsafe release path')
  if n in ['web/.astro','web/node_modules/.vite'] or n.startswith(('web/.astro/','web/node_modules/.vite/')):raise ValueError('Cache paths must not be published')
  names.add(n)
  if f['type']=='file':
   if not re.fullmatch('[0-9a-f]{64}',f.get('sha256','')) or type(f.get('size')) is not int or not 0<=f['size']<=MAX_BUNDLE or f.get('mode') not in [0o644,0o755]:raise ValueError('Invalid file declaration')
   size+=f['size']
  elif f['type']=='symlink':
   target=f['target'];dest=posixpath.normpath(posixpath.join(posixpath.dirname(n),target))
   if target.startswith('/') or '\\' in target or dest.startswith('../') or dest not in [x['path'] for x in m['files']]:raise ValueError('Unsafe symbolic link')
   links.add(n)
  else:raise ValueError('Unsupported file type')
 if any(str(parent) in links for n in names for parent in pathlib.PurePosixPath(n).parents):raise ValueError('Path traverses symbolic link')
 if size!=m.get('unpacked_size') or size>2<<30:raise ValueError('Invalid expanded size')
 required_files=['bin/library','bin/library-backup']
 if m.get('runtime')=='static-v1':
  required_files+=['web/index.html','web/book-view/index.html','web/settings/index.html','web/scan/index.html']
  if any(n.startswith(('runtime/','web/node_modules/','web/src/','web/.svelte-kit/')) for n in names):raise ValueError('Static runtime contains development dependencies')
 else:required_files+=['web/package.json','web/node_modules/astro/bin/astro.mjs','runtime/bin/node']
 for required in required_files:
  if required not in names:raise ValueError('Incomplete runtime')
 return m

def check_bundle(bundle,manifest):
 digest=hashlib.sha256()
 with open(bundle,'rb') as stream:
  while block:=stream.read(1<<20):digest.update(block)
 if digest.hexdigest()!=manifest['bundle_sha256']:raise ValueError('Bundle checksum rejected')

def check_existing(destination,manifest):
 destination=pathlib.Path(destination)
 for f in manifest['files']:
  p=destination/f['path']
  if f['type']=='symlink':
   if not p.is_symlink() or os.readlink(p)!=f['target']:raise ValueError('Existing release link mismatch')
  else:
   if p.is_symlink() or not p.is_file():raise ValueError('Existing release file mismatch')
   digest=hashlib.sha256()
   with p.open('rb') as source:
    while chunk:=source.read(1<<20):digest.update(chunk)
   if digest.hexdigest()!=f['sha256']:raise ValueError('Existing release checksum mismatch')

def sync_release(destination):
 # On SD cards, one filesystem durability barrier avoids thousands of costly
 # per-file journal commits. It completes before the release can be activated.
 fd=os.open(destination,os.O_RDONLY|os.O_DIRECTORY)
 try:
  if sys.platform.startswith('linux'):
   libc=ctypes.CDLL(None,use_errno=True)
   if libc.syncfs(fd)!=0:
    error=ctypes.get_errno();raise OSError(error,os.strerror(error))
  else:os.fsync(fd)
 finally:os.close(fd)

def extract(bundle,manifest,destination):
 """Write only signed regular bytes, then contained leaf links. No tar.extract."""
 destination=pathlib.Path(destination)
 check_bundle(bundle,manifest)
 if shutil.disk_usage(destination.parent).free<manifest['unpacked_size']*2+(256<<20):raise ValueError('Insufficient staging space')
 expected={f['path']:f for f in manifest['files']};seen=set();symlinks=[]
 destination.mkdir(mode=0o755)
 try:
  with tarfile.open(bundle,'r:gz') as archive:
   for member in archive:
    n=member.name
    if n not in expected or n in seen:raise ValueError('Unexpected archive member')
    f=expected[n];seen.add(n);out=destination/n;out.parent.mkdir(parents=True,exist_ok=True)
    if f['type']=='symlink':
     if not member.issym() or member.linkname!=f['target']:raise ValueError('Link declaration mismatch')
     symlinks.append((out,f['target']))
    else:
     if not member.isfile() or member.size!=f['size']:raise ValueError('File declaration mismatch')
     digest=hashlib.sha256()
     with archive.extractfile(member) as src,out.open('xb') as dst:
      while b:=src.read(1<<20):digest.update(b);dst.write(b)
      dst.flush()
      if not sys.platform.startswith('linux'):os.fsync(dst.fileno())
     if digest.hexdigest()!=f['sha256']:raise ValueError('File checksum rejected')
     out.chmod(f['mode'])
  if seen!=set(expected):raise ValueError('Missing runtime files')
  for out,target in symlinks:out.symlink_to(target)
  sync_release(destination)
 except Exception:
  shutil.rmtree(destination);raise

def safe_copy(source,destination,limit):
 fd=os.open(source,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
 try:
  info=os.fstat(fd)
  if not stat.S_ISREG(info.st_mode) or info.st_size>limit:raise ValueError('Invalid staged input')
  with os.fdopen(fd,'rb',closefd=False) as src,open(destination,'xb') as dst:
   total=0
   while chunk:=src.read(1<<20):
    total+=len(chunk)
    if total>limit:raise ValueError('Staged input grew beyond its limit')
    dst.write(chunk)
   dst.flush();os.fsync(dst.fileno())
 finally:os.close(fd)
