#!/usr/bin/env python3
"""Unprivileged owner-only update controller, reached through Caddy's Unix socket."""
import fcntl, http.server, json, os, pathlib, pwd, re, socket, socketserver, struct, subprocess, tempfile, threading, time, urllib.parse, urllib.request, uuid
from updates_common import VERSION, MAX_BUNDLE, atomic_json, read_json, verify

ROOT=pathlib.Path('/var/lib/library-updater')
CONFIG=read_json('/etc/home-library-updater/config.json')
REPO=CONFIG.get('repository','pageboy-za/library')
VERIFIER='/usr/local/libexec/home-library-updater/release-verify'
KEY='/etc/home-library-updater/public.pem'
LOCK=threading.Lock()

class Redirect(urllib.request.HTTPRedirectHandler):
 def redirect_request(self,req,fp,code,msg,headers,newurl):
  host=urllib.parse.urlparse(newurl).hostname or ''
  if urllib.parse.urlparse(newurl).scheme!='https' or not (host=='api.github.com' or host.endswith('.githubusercontent.com') or host=='github.com'):raise ValueError('Untrusted download redirect')
  out=super().redirect_request(req,fp,code,msg,headers,newurl)
  if host!=urllib.parse.urlparse(req.full_url).hostname:out.remove_header('Authorization')
  return out

def download(url,path,limit,accept='application/vnd.github+json'):
 if not url.startswith('https://api.github.com/repos/'+REPO+'/'):raise ValueError('Untrusted download location')
 headers={'Accept':accept,'User-Agent':'Home-Library-Updater'}
 if token:=os.environ.get('LIBRARY_GITHUB_READ_TOKEN'):headers['Authorization']='Bearer '+token
 request=urllib.request.Request(url,headers=headers)
 total=0
 with urllib.request.build_opener(Redirect()).open(request,timeout=45) as response,open(path,'wb') as output:
  while chunk:=response.read(1<<20):
   total+=len(chunk)
   if total>limit:raise ValueError('Download exceeds limit')
   output.write(chunk)
  output.flush();os.fsync(output.fileno())

def releases():
 with tempfile.TemporaryDirectory(dir=ROOT/'requests',prefix='.github-') as temp:
  path=pathlib.Path(temp)/'releases.json'
  download('https://api.github.com/repos/'+REPO+'/releases?per_page=10',path,1<<20)
  return [r for r in read_json(path) if not r['draft'] and not r['prerelease'] and VERSION.fullmatch(r['tag_name'])]

def asset(release,name):
 item=next(a for a in release['assets'] if a['name']==name)
 return 'https://api.github.com/repos/'+REPO+'/releases/assets/'+str(int(item['id']))

def status():
 pending=read_json(ROOT/'requests/progress.json');activation=read_json(ROOT/'state/status.json')
 result=activation if activation and (not pending or activation.get('job')==pending.get('job')) else pending
 return {'installed':read_json('/opt/home-library/current/RELEASE.json').get('version','legacy'),**result}

def prepare(version,job):
 stage=ROOT/'downloads'/job
 try:
  stage.mkdir(mode=0o700)
  atomic_json(ROOT/'requests/progress.json',dict(job=job,version=version,phase='downloading'))
  release=next(r for r in releases() if r['tag_name']==version)
  for name,limit in [('manifest.json',16<<20),('manifest.sig',1024),('bundle.tar.gz',MAX_BUNDLE)]:
   download(asset(release,name),stage/name,limit,'application/octet-stream')
  atomic_json(ROOT/'requests/progress.json',dict(job=job,version=version,phase='verifying'))
  manifest=verify(stage/'manifest.json',stage/'manifest.sig',KEY,VERIFIER)
  if manifest['version']!=version:raise ValueError('Release version mismatch')
  atomic_json(ROOT/'requests/ready.json',dict(job=job,version=version))
  atomic_json(ROOT/'requests/progress.json',dict(job=job,version=version,phase='preparing'))
 except Exception as error:
  atomic_json(ROOT/'requests/progress.json',dict(job=job,version=version,phase='failed',message='Could not download or verify this release. The current application is unchanged.'))
  print('Release preparation failed:',type(error).__name__,flush=True)

class Handler(http.server.BaseHTTPRequestHandler):
 def setup(self):
  super().setup();self.connection.settimeout(15)
 def reply(self,code,value):
  data=json.dumps(value).encode();self.send_response(code);self.send_header('Content-Type','application/json');self.send_header('Cache-Control','no-store');self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
 def allowed(self):
  peer=struct.unpack('3i',self.connection.getsockopt(socket.SOL_SOCKET,socket.SO_PEERCRED,12))[1]
  if peer not in [0,pwd.getpwnam('library-gateway').pw_uid]:return False
  return self.headers.get('X-Library-Owner')=='nealle' and self.headers.get('Host')=='127.0.0.1:8090' and self.headers.get('Origin')=='http://127.0.0.1:8090'
 def do_GET(self):
  if not self.allowed():return self.reply(403,{'error':'Owner access required'})
  if self.path=='/api/updates/status':return self.reply(200,status())
  if self.path=='/api/updates/releases':
   try:return self.reply(200,{'releases':[{'version':r['tag_name'],'notes':r.get('body') or ''} for r in releases() if {'manifest.json','manifest.sig','bundle.tar.gz'}.issubset({a['name'] for a in r['assets']})]})
   except Exception:return self.reply(503,{'error':'Could not check GitHub. Try again later.'})
  return self.reply(404,{'error':'Not found'})
 def do_POST(self):
  if not self.allowed():return self.reply(403,{'error':'Owner access required'})
  if self.path!='/api/updates/install' or self.headers.get('Content-Type')!='application/json':return self.reply(400,{'error':'Invalid update request'})
  try:
   length=int(self.headers.get('Content-Length','0'));assert 0<length<=1024
   body=json.loads(self.rfile.read(length));version=body['version'];job=str(uuid.UUID(body['request_key']))
   assert VERSION.fullmatch(version) and set(body)=={'version','request_key'}
  except Exception:return self.reply(400,{'error':'Choose an approved release'})
  with LOCK:
   current=status()
   if current.get('job')==job:
    return self.reply(200,current) if current.get('version')==version else self.reply(409,{'error':'Request already used'})
   if current.get('phase') in ['downloading','verifying','preparing','backing_up','restarting','recovering','needs_attention']:return self.reply(409,{'error':'An update is already running or needs attention'})
   if current.get('installed')==version:return self.reply(200,{'installed':version,'phase':'ready'})
   atomic_json(ROOT/'requests/progress.json',dict(job=job,version=version,phase='downloading'))
   threading.Thread(target=prepare,args=(version,job),daemon=True).start()
  return self.reply(202,dict(job=job,version=version,phase='downloading'))
 def log_message(self,*args):pass

class Server(http.server.ThreadingHTTPServer):
 address_family=socket.AF_UNIX
 def server_bind(self):
  socketserver.TCPServer.server_bind(self);self.server_name='localhost';self.server_port=0
if __name__=='__main__':
 progress=read_json(ROOT/'requests/progress.json')
 if progress.get('phase') in ['downloading','verifying']:
  atomic_json(ROOT/'requests/progress.json',{**progress,'phase':'failed','message':'Download was interrupted. Please retry.'})
 path='/run/home-library-updater/http.sock'
 if os.path.exists(path):os.unlink(path)
 server=Server(path,Handler);os.chmod(path,0o660);server.serve_forever()
