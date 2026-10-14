#!/usr/bin/env python3
"""Adversarial release tests with disposable keys and files, no real catalogue."""
import copy, hashlib, http.client, io, json, os, pathlib, socket, subprocess, sys, tarfile, tempfile, threading, types, uuid
from importlib.machinery import SourceFileLoader
from updates_common import verify, extract

verifier=os.environ.get('RELEASE_VERIFY_BIN','./bin/library-release')
with tempfile.TemporaryDirectory(prefix='library-release-security-') as directory:
 root=pathlib.Path(directory);private=root/'private.pem';public=root/'public.pem'
 subprocess.run([verifier,'-action','generate','-key',str(private),'-public',str(public)],check=True)
 names=['bin/library','bin/library-backup','web/package.json','web/node_modules/astro/bin/astro.mjs','runtime/bin/node']
 bodies={n:b'fixture' for n in names}
 bundle=root/'bundle.tar.gz';manifest=root/'manifest.json';signature=root/'manifest.sig'
 def write_bundle(extra=None):
  with tarfile.open(bundle,'w:gz') as t:
   for n,b in bodies.items():
    m=tarfile.TarInfo(n);m.size=len(b);t.addfile(m,io.BytesIO(b))
   if extra:t.addfile(extra,io.BytesIO(b'x') if extra.isfile() else None)
 def sign(value):
  value['bundle_sha256']=hashlib.sha256(bundle.read_bytes()).hexdigest();manifest.write_text(json.dumps(value))
  subprocess.run([verifier,'-action','sign','-key',str(private),'-manifest',str(manifest),'-signature',str(signature)],check=True)
 def rejected(fn):
  try:fn()
  except (ValueError,subprocess.CalledProcessError):return
  raise AssertionError('Unsafe release was accepted')
 base=dict(format=1,version='v9.9.9',commit='a'*40,arch='linux-arm64',schema=1,rollback_compatible=True,unpacked_size=sum(map(len,bodies.values())),files=[dict(path=n,type='file',sha256=hashlib.sha256(b).hexdigest(),size=len(b),mode=0o755) for n,b in bodies.items()])
 write_bundle();sign(copy.deepcopy(base));valid=verify(manifest,signature,public,verifier);extract(bundle,valid,root/'valid')
 assert (root/'valid/bin/library').read_bytes()==b'fixture'
 # Altered publisher bytes must fail even when the tar is otherwise valid.
 manifest.write_bytes(manifest.read_bytes()+b' ');rejected(lambda:verify(manifest,signature,public,verifier))
 for change in [lambda m:m.update(arch='linux-amd64'),lambda m:m.update(schema=2),lambda m:m.update(rollback_compatible=False),lambda m:m['files'].append(dict(path='../../escape',type='file',size=0,sha256='0'*64,mode=0o644)),lambda m:m['files'].append(dict(path='web/escape',type='symlink',target='../../etc/passwd'))]:
  m=copy.deepcopy(base);change(m);sign(m);rejected(lambda:verify(manifest,signature,public,verifier))
 # A signed bundle hash cannot excuse extra archive entries or hard links.
 for name,kind in [('unexpected',tarfile.REGTYPE),('bin/library',tarfile.REGTYPE),('bin/library',tarfile.LNKTYPE)]:
  extra=tarfile.TarInfo(name);extra.type=kind;extra.size=1 if kind==tarfile.REGTYPE else 0;extra.linkname='../../escape' if kind==tarfile.LNKTYPE else ''
  write_bundle(extra);sign(copy.deepcopy(base));m=verify(manifest,signature,public,verifier);rejected(lambda:extract(bundle,m,root/'bad'))
  assert not (root/'bad').exists()
 write_bundle();m=copy.deepcopy(base);m['files'][0]['sha256']='0'*64;sign(m);m=verify(manifest,signature,public,verifier);rejected(lambda:extract(bundle,m,root/'bad'))
 assert not (root/'escape').exists()
 # Recovery operates on protected version paths and never restores catalogue bytes.
 activation=SourceFileLoader('activation_test',str(pathlib.Path(__file__).with_name('update-activate.py'))).load_module()
 activation.ROOT=root/'updater';activation.STATE=activation.ROOT/'state';activation.STATE.mkdir(parents=True)
 activation.RELEASES=root/'releases';previous=activation.RELEASES/'v1.0.0';previous.mkdir(parents=True)
 activation.CURRENT=root/'current';activation.CURRENT.symlink_to(previous)
 catalogue=root/'catalogue';catalogue.write_bytes(b'disposable persistent record')
 calls=[];activation.system=lambda *args:calls.append(args);activation.ready=lambda *args,**kwargs:True
 journal=dict(job=str(uuid.uuid4()),version='v2.0.0',previous=str(previous),phase='restarting')
 activation.atomic_json(activation.STATE/'status.json',journal);activation.activate()
 assert activation.read_json(activation.STATE/'status.json')['phase']=='recovered'
 assert catalogue.read_bytes()==b'disposable persistent record'
 assert calls==[('stop','home-library.service'),('start','home-library.service')]
 calls.clear();bad={**journal,'previous':str(root)};rejected(lambda:activation.recover(bad));assert calls==[]
 # Exercise the real HTTP request handler on Linux, including owner/origin gates.
 if sys.platform.startswith('linux'):
  api=SourceFileLoader('api_test',str(pathlib.Path(__file__).with_name('update-api.py'))).load_module()
  api.ROOT=root/'api';(api.ROOT/'requests').mkdir(parents=True);(api.ROOT/'state').mkdir()
  api.pwd=types.SimpleNamespace(getpwnam=lambda name:types.SimpleNamespace(pw_uid=os.getuid()))
  prepared=[];api.prepare=lambda version,job:prepared.append((version,job))
  path=str(root/'test.sock');server=api.Server(path,api.Handler);threading.Thread(target=server.serve_forever,daemon=True).start()
  class Connection(http.client.HTTPConnection):
   def connect(self):self.sock=socket.socket(socket.AF_UNIX);self.sock.connect(path)
  def request(body,headers=None):
   c=Connection('localhost');h={'Host':'127.0.0.1:8090','Origin':'http://127.0.0.1:8090','X-Library-Owner':'nealle','Content-Type':'application/json'}
   if headers:h.update(headers)
   c.request('POST','/api/updates/install',json.dumps(body),h);r=c.getresponse();code=r.status;r.read();c.close();return code
  body={'version':'v9.9.9','request_key':str(uuid.uuid4())}
  try:
   assert request(body,{'Origin':'https://attacker.invalid'})==403
   assert request(body,{'X-Library-Owner':'guest'})==403
   assert request({**body,'url':'https://attacker.invalid'})==400
   assert request(body)==202
   assert request(body)==200
   assert request({**body,'version':'v8.8.8'})==409
   assert request({**body,'request_key':str(uuid.uuid4())})==409
  finally:server.shutdown();server.server_close()
 print('PASS: signed extraction; tampered signatures; incompatible schema/architecture; unsafe paths/links; extra/duplicate/hard-link members; file hash mismatch; cleanup without catalogue access.')
 print('PASS: interrupted activation recovery preserves data; fixed-service recovery scope; owner/origin validation and duplicate/busy request gates (Linux).')
