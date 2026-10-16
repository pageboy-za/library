#!/usr/bin/env python3
"""Remove only the shared gateway login; retain TLS, origins and staff access.

An administrator runs this once on the existing Pi. Application releases never
execute this helper or change the gateway themselves.
"""
import hashlib, json, os, pathlib, re, ssl, stat, subprocess, tempfile, time, urllib.error, urllib.request

def network_gateway(raw):
 text=raw.decode()
 required=['https://library.local {','tls internal','respond @foreignOrigin','handle /api/updates/* {','forward_auth 127.0.0.1:8080 {','uri /api/auth/authorize-update','header_up X-Forwarded-Method {http.request.method}','reverse_proxy unix//run/home-library-updater/http.sock','reverse_proxy 127.0.0.1:8080 {']
 if any(text.count(part)!=1 for part in required):raise ValueError('Unexpected gateway; manual review required')
 gate=text.index('uri /api/auth/authorize-update')
 socket=text.index('reverse_proxy unix//run/home-library-updater/http.sock')
 if gate>socket:raise ValueError('Staff authorization must precede the updater')
 login=re.compile(r'(?m)^\s*basic_auth \{\s*\{\$LIBRARY_LOGIN_USER\} \{\$LIBRARY_LOGIN_HASH\}\s*\}\n')
 matches=list(login.finditer(text))
 if len(matches)!=1 or text.count('basic_auth')!=1 or text.count('header_up X-Library-Owner {http.auth.user.id}')!=1:raise ValueError('Expected the existing shared-login gateway')
 text=login.sub('',text,count=1)
 # The legacy socket accepts the installation owner only. Set its routing
 # identity AFTER individual staff administrator/CSRF authorization above.
 text=text.replace('header_up X-Library-Owner {http.auth.user.id}','header_up X-Library-Owner nealle')
 return text.encode()

def write(path,raw,mode=0o644):
 fd,tmp=tempfile.mkstemp(prefix='.network-access-',dir=path.parent)
 try:
  with os.fdopen(fd,'wb') as f:f.write(raw);f.flush();os.fsync(f.fileno())
  os.chmod(tmp,mode);os.replace(tmp,path)
 finally:
  if os.path.exists(tmp):os.unlink(tmp)

def main():
 if os.geteuid()!=0:raise SystemExit('Run with sudo on the Raspberry Pi')
 config=pathlib.Path('/etc/home-library-gateway/Caddyfile');marker=config.with_name('staff-permissions.json')
 for p in [config,marker]:
  info=p.lstat()
  if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_mode&0o022:raise ValueError('Expected protected gateway files')
 original=config.read_bytes();old_marker=marker.read_bytes();proposed=network_gateway(original)
 backup=config.with_name('Caddyfile.before-network-access-'+str(time.time_ns()))
 write(backup,original,0o600)
 validation=config.with_name('Caddyfile.network-candidate')
 write(validation,proposed)
 try:subprocess.run(['/usr/local/bin/library-caddy','adapt','--config',str(validation),'--adapter','caddyfile'],check=True,stdout=subprocess.DEVNULL)
 finally:validation.unlink(missing_ok=True)
 try:
  write(config,proposed)
  subprocess.run(['systemctl','restart','home-library-gateway.service'],check=True)
  subprocess.run(['systemctl','is-active','--quiet','home-library-gateway.service'],check=True)
  context=ssl.create_default_context(cafile='/var/lib/library-gateway/.local/share/caddy/pki/authorities/local/root.crt')
  client=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPSHandler(context=context))
  def status(path,origin=None):
   request=urllib.request.Request('https://library.local'+path,headers={'Origin':origin} if origin else {})
   try:
    with client.open(request,timeout=15) as response:return response.status
   except urllib.error.HTTPError as e:return e.code
  for attempt in range(30):
   try:
    if status('/healthz')==200:break
   except (urllib.error.URLError,TimeoutError):pass
   time.sleep(.5)
  checks={'browse':status('/'),'health':status('/healthz'),'borrowers':status('/api/borrowers'),'updater':status('/api/updates/status'),'foreign_origin':status('/healthz','https://elsewhere.invalid')}
  if checks!={'browse':200,'health':200,'borrowers':401,'updater':401,'foreign_origin':403}:raise RuntimeError('Gateway access verification failed: '+json.dumps(checks))
  if config.read_bytes()!=proposed:raise RuntimeError('Gateway changed during verification')
  write(marker,(json.dumps(dict(format=1,capability='staff-update-authorization-v1',config_sha256=hashlib.sha256(proposed).hexdigest()))+'\n').encode())
 except BaseException:
  write(config,original);write(marker,old_marker)
  subprocess.run(['systemctl','restart','home-library-gateway.service'],check=True)
  raise
 print('Shared browser login removed. HTTPS, origin checks and individual staff administrator access verified.')

if __name__=='__main__':main()
