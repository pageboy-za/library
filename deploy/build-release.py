#!/usr/bin/env python3
"""Package a prepared ARM64 runtime; never traverse household data."""
import argparse, hashlib, json, os, pathlib, re, tarfile
p=argparse.ArgumentParser()
p.add_argument('--root',required=True);p.add_argument('--out',required=True)
p.add_argument('--version',required=True);p.add_argument('--commit',required=True)
a=p.parse_args()
assert re.fullmatch(r'v\d+\.\d+\.\d+',a.version)
root=pathlib.Path(a.root).resolve();out=pathlib.Path(a.out).resolve();out.mkdir(parents=True,exist_ok=True)
assert not out.is_relative_to(root)
required=['bin/library','bin/library-backup','bin/library-account','web/index.html','web/book-view/index.html','web/settings/index.html','web/scan/index.html','web/join/index.html','web/membership/index.html','web/staff/login/index.html','web/staff/members/index.html']
for n in required: assert (root/n).is_file(),n
files=[]
with tarfile.open(out/'bundle.tar.gz','w:gz',dereference=False) as t:
 for path in sorted(root.rglob('*')):
  relative=path.relative_to(root).as_posix()
  assert relative.split('/')[0] in ['bin','web'],relative
  if relative.startswith(('web/.astro','web/.svelte-kit','web/src/','web/node_modules/','web/dist')):raise ValueError('Development files must not be included in a prebuilt runtime: '+relative)
  if path.is_symlink():
   target=os.readlink(path);assert not os.path.isabs(target)
   assert path.resolve().is_relative_to(root),relative
   files.append(dict(path=relative,type='symlink',target=target))
  elif path.is_file():
   blob=path.read_bytes();files.append(dict(path=relative,type='file',sha256=hashlib.sha256(blob).hexdigest(),size=len(blob),mode=0o755 if path.stat().st_mode&0o111 else 0o644))
  elif path.is_dir():continue
  else:raise ValueError('Unsupported file type')
  t.add(path,arcname=relative,recursive=False)
manifest=dict(format=1,runtime='static-v1',version=a.version,commit=a.commit,arch='linux-arm64',schema=2,rollback_compatible=False,bundle_sha256=hashlib.sha256((out/'bundle.tar.gz').read_bytes()).hexdigest(),unpacked_size=sum(f.get('size',0) for f in files),files=files)
(out/'manifest.json').write_text(json.dumps(manifest,sort_keys=True,separators=(',',':'))+'\n')
print('Prepared',a.version,'with',len(files),'runtime files.')
