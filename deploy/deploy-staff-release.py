#!/usr/bin/env python3
"""One-time, interactive Raspberry Pi staff/member deployment.

Uses only a locally staged release signed by the installation's existing trusted
release key. No publication, network download or real Wallet credentials. Run on
the Pi with sudo from a reviewed staging directory; first-admin passwords are
read from the controlling terminal and passed to the account tool on stdin.
"""
import argparse, fcntl, getpass, hashlib, importlib.machinery, json, os, pathlib, pwd, secrets, shutil, socket, sqlite3, subprocess, sys, tarfile, tempfile, time, urllib.error, urllib.request, uuid
from updates_common import MAX_BUNDLE, atomic_json, check_bundle, check_existing, extract, read_json, require_staff_gateway, safe_copy, verify

ROOT=pathlib.Path('/var/lib/library-updater');STATE=ROOT/'state'
RELEASES=pathlib.Path('/opt/home-library/releases');CURRENT=pathlib.Path('/opt/home-library/current')
DATA=pathlib.Path('/var/lib/home-library/data');BACKUPS=pathlib.Path('/var/backups/home-library-updates')
INSTALLED=pathlib.Path('/usr/local/libexec/home-library-updater')
GATEWAY=pathlib.Path('/etc/home-library-gateway/Caddyfile')
CLIENT=urllib.request.build_opener(urllib.request.ProxyHandler({}))

def run(args,**kwargs):return subprocess.run(list(map(str,args)),check=True,timeout=300,**kwargs)
def system(*args):return run(['/usr/bin/systemctl',*args])
def as_user(user,*args,**kwargs):return run(['/usr/sbin/runuser','-u',user,'--',*args],**kwargs)
def protected(path,uid=0):
 info=path.lstat()
 if path.is_symlink() or info.st_uid!=uid or info.st_mode&0o022:raise ValueError('Expected root-owned protected installation path')
def quote_identifier(value):return '"'+value.replace('"','""')+'"'
def fingerprint(data,columns=None):
 """Compare every original field; additive migration columns are permitted."""
 database=data/'library.sqlite'
 with sqlite3.connect(database.as_uri()+'?mode=ro',uri=True) as db:
  if columns is None:
   names=[r[0] for r in db.execute("SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")]
   columns={n:[r[1] for r in db.execute('PRAGMA table_info('+quote_identifier(n)+')')] for n in names if n not in ('staff_users','staff_sessions','book_search') and not n.startswith('book_search_')}
  tables={}
  for name,fields in columns.items():
   values=[]
   for row in db.execute('SELECT '+','.join(map(quote_identifier,fields))+' FROM '+quote_identifier(name)):
    values.append(json.dumps([{'blob':v.hex()} if isinstance(v,bytes) else v for v in row],separators=(',',':'),ensure_ascii=True))
   tables[name]={'rows':len(values),'sha256':hashlib.sha256('\n'.join(sorted(values)).encode()).hexdigest()}
 covers={}
 cover_root=data/'covers'
 if cover_root.exists():
  for file in sorted(cover_root.rglob('*')):
   if file.is_symlink():raise ValueError('Unexpected cover symlink')
   if file.is_file():covers[file.relative_to(cover_root).as_posix()]=hashlib.sha256(file.read_bytes()).hexdigest()
 return {'columns':columns,'tables':tables,'covers':covers}

def compare(data,baseline):
 actual=fingerprint(data,baseline['columns'])
 if actual!=baseline:raise RuntimeError('Catalogue fields or cover files changed unexpectedly')

def gateway_with_staff(raw):
 text=raw.decode('utf-8')
 required=['https://library.local {','tls internal','basic_auth {','{$LIBRARY_LOGIN_USER} {$LIBRARY_LOGIN_HASH}','respond @foreignOrigin','reverse_proxy 127.0.0.1:8080','handle /api/updates/* {']
 if any(part not in text for part in required):raise ValueError('Gateway differs from supported private library.local installation; manual review required')
 if text.count('handle /api/updates/* {')!=1:raise ValueError('Ambiguous updater gateway route')
 if '/api/auth/authorize-update' in text:raise ValueError('Staff gateway already exists; use normal reviewed update procedure')
 insert='''
			# Require an individual administrator before the updater gateway.
			forward_auth 127.0.0.1:8080 {
				uri /api/auth/authorize-update
				header_up Host 127.0.0.1:8080
				header_up Origin http://127.0.0.1:8080
				header_up X-Forwarded-Method {http.request.method}
				header_up -Authorization
			}'''
 return text.replace('handle /api/updates/* {','handle /api/updates/* {'+insert,1).encode()

def write_private(path,raw,mode=0o600):
 fd,temp=tempfile.mkstemp(prefix='.staff-deploy-',dir=path.parent)
 try:
  with os.fdopen(fd,'wb') as f:f.write(raw);f.flush();os.fsync(f.fileno())
  os.chmod(temp,mode);os.chown(temp,0,0);os.replace(temp,path)
  directory=os.open(path.parent,os.O_DIRECTORY)
  try:os.fsync(directory)
  finally:os.close(directory)
 finally:
  if os.path.exists(temp):os.unlink(temp)

def request(base,path,method='GET',payload=None,headers=None):
 body=json.dumps(payload).encode() if payload is not None else None
 h={'Origin':base,**(headers or {})}
 if body is not None:h['Content-Type']='application/json'
 try:
  with CLIENT.open(urllib.request.Request(base+path,data=body,method=method,headers=h),timeout=10) as response:
   raw=response.read(2<<20);return response.status,json.loads(raw) if 'application/json' in response.headers.get('Content-Type','') else raw,response.headers
 except urllib.error.HTTPError as error:return error.code,None,error.headers

def member_checks(base):
 for path in ['/api/borrowers','/api/loans','/api/members','/api/staff/accounts','/api/auth/authorize-update']:
  if request(base,path)[0]!=401:raise RuntimeError('Anonymous staff permission check failed')
 status,payload,_=request(base,'/api/member/registration')
 if status!=200 or payload.get('wallet')!={'apple':False,'google':False}:raise RuntimeError('Member registration or disabled Wallet verification failed')
 if request(base,'/api/member/books')[0]!=200:raise RuntimeError('Anonymous member catalogue failed')
 for path in ['/join/','/membership/','/staff/login/']:
  if request(base,path)[0]!=200:raise RuntimeError('Membership page unavailable')

def candidate_check(candidate,snapshot,stage,activation):
 smoke=stage/'candidate-check';smoke.mkdir(mode=0o700);shutil.chown(smoke,user='library-smoke',group='library-smoke')
 # Never expose live household paths to the smoke OS account.
 shutil.copytree(snapshot,smoke/'snapshot');shutil.copytree(candidate/'web',smoke/'web')
 for directory,dirs,files in os.walk(smoke):
  shutil.chown(directory,user='library-smoke',group='library-smoke');os.chmod(directory,0o700)
  for name in files:shutil.chown(pathlib.Path(directory)/name,user='library-smoke',group='library-smoke')
 as_user('library-smoke',candidate/'bin/library-backup','restore','-backup',smoke/'snapshot','-out',smoke/'data')
 before=fingerprint(smoke/'data')
 with socket.socket() as reservation:reservation.bind(('127.0.0.1',0));port=reservation.getsockname()[1]
 env={**os.environ,'LIBRARY_WALLET_CONFIG':'','PATH':'/usr/bin:/bin','HOME':str(smoke),'GOMEMLIMIT':'128MiB'}
 with (stage/'candidate.log').open('wb') as log:
  os.chmod(stage/'candidate.log',0o600)
  process=subprocess.Popen(['/usr/sbin/runuser','-u','library-smoke','--',str(candidate/'bin/library'),'-listen',f'127.0.0.1:{port}','-data',str(smoke/'data'),'-web',str(smoke/'web'),'-wallet-config','','-public-url','https://library.local','-maintenance-file',str(STATE/'maintenance.json')],env=env,stdout=log,stderr=log)
  try:
   if not activation.ready(candidate.name,port=port):raise RuntimeError('Isolated candidate readiness failed')
   member_checks(f'http://127.0.0.1:{port}')
  finally:
   process.terminate()
   try:process.wait(timeout=15)
   except subprocess.TimeoutExpired:process.kill();process.wait(timeout=10)
 compare(smoke/'data',before)
 shutil.rmtree(smoke)

def main():
 p=argparse.ArgumentParser(description=__doc__);p.add_argument('--assets',required=True,type=pathlib.Path);p.add_argument('--username',required=True);p.add_argument('--display-name',required=True);p.add_argument('--version',required=True);p.add_argument('--commit',required=True);args=p.parse_args()
 if os.geteuid()!=0:raise SystemExit('Run with sudo on the Raspberry Pi')
 stage_source=pathlib.Path(__file__).resolve().parent
 password=None
 for path in [ROOT,STATE,RELEASES,INSTALLED,GATEWAY,GATEWAY.parent]:protected(path)
 protected(DATA,pwd.getpwnam('home-library').pw_uid)
 previous=CURRENT.resolve();protected(previous)
 if previous.parent!=RELEASES:raise ValueError('Unexpected current release')
 with sqlite3.connect((DATA/'library.sqlite').as_uri()+'?mode=ro',uri=True) as db:
  if db.execute('PRAGMA user_version').fetchone()[0]!=1:raise ValueError('One-time migration requires an existing schema 1 installation')
 gateway=gateway_with_staff(GATEWAY.read_bytes())
 # Validate reviewed helper sources without running application code as root.
 for name in ['updates_common.py','update-api.py','update-activate.py']:
  compile((stage_source/name).read_text(),str(stage_source/name),'exec')
 run(['/bin/sh','-n',stage_source/'start-managed-library.sh'])
 job='staff-'+str(uuid.uuid4());job_dir=BACKUPS/job;job_dir.mkdir(mode=0o710);job_dir.chmod(0o710);shutil.chown(job_dir,user='root',group='home-library')
 private_backups=job_dir/'private-backups';private_backups.mkdir(mode=0o700);shutil.chown(private_backups,user='home-library',group='home-library')
 # Create the verified live backup before stopping or changing any service,
 # helper, gateway or installed release. The old snapshot tool handles live WAL.
 snapshot=private_backups/'snapshot'
 as_user('home-library',previous/'bin/library-backup','create','-data',DATA,'-out',snapshot)
 as_user('home-library',previous/'bin/library-backup','verify','-backup',snapshot)
 baseline=fingerprint(snapshot)
 write_private(job_dir/'baseline.json',json.dumps(baseline,sort_keys=True).encode())
 operator_name=os.environ.get('SUDO_USER','')
 if not operator_name or operator_name=='root':raise ValueError('Run using sudo from the installation administrator account')
 operator=pwd.getpwnam(operator_name);home=pathlib.Path(operator.pw_dir)
 control=home/'library-members-update'
 if control.exists():
  if control.is_symlink() or control.stat().st_uid!=operator.pw_uid:raise ValueError('Invalid deployment control directory')
 else:control.mkdir(mode=0o700);os.chown(control,operator.pw_uid,operator.pw_gid)
 archive=home/(job+'.tar.gz')
 with archive.open('xb') as stream:
  os.chmod(archive,0o600)
  with tarfile.open(fileobj=stream,mode='w:gz') as tar:tar.add(snapshot,arcname='snapshot',recursive=True)
  stream.flush();os.fsync(stream.fileno())
 os.chown(archive,operator.pw_uid,operator.pw_gid)
 digest=hashlib.sha256(archive.read_bytes()).hexdigest()
 state_path=control/'state.json';proof_path=control/'offdevice-proof.json'
 atomic_json(state_path,{'phase':'backup_ready','job':job,'backup':str(snapshot),'archive':str(archive),'sha256':digest,'expected_version':args.version,'expected_commit':args.commit})
 os.chown(state_path,operator.pw_uid,operator.pw_gid);state_path.chmod(0o600)
 print('Backup verified. Private export: '+str(archive)+'\nSHA256: '+digest,flush=True)
 input('Leave this terminal open. After Codex confirms the off-device backup verification, press Enter to continue: ')
 proof=read_json(proof_path)
 if proof.get('job')!=job or proof.get('sha256')!=digest or proof.get('verified') is not True:raise ValueError('Matching off-device backup verification proof is required')
 atomic_json(state_path,{'phase':'deploying','job':job,'backup':str(snapshot),'archive':str(archive),'sha256':digest});os.chown(state_path,operator.pw_uid,operator.pw_gid);state_path.chmod(0o600)
 # Passwords are read only from the controlling terminal; no CLI/env option.
 password=getpass.getpass('Choose your first staff administrator password (12–128 characters): ')
 if len(password)<12 or len(password)>128 or '\n' in password or '\r' in password:raise ValueError('Password must contain 12–128 characters on one line')
 if password!=getpass.getpass('Confirm staff administrator password: '):raise ValueError('Passwords differ')
 # Smoke account must traverse only its own isolated work, never private backup.
 work=pathlib.Path('/var/cache/home-library')/job;work.mkdir(mode=0o711);work.chmod(0o711)
 activation=importlib.machinery.SourceFileLoader('staff_activation',str(stage_source/'update-activate.py')).load_module()
 candidate=None;switched=False;stopped=False;success=False;config_changed=False;helpers_changed=False;dropin_created=False
 dropin=pathlib.Path('/etc/systemd/system/home-library.service.d/zz-staff-release.conf')
 if dropin.exists():raise ValueError('Staff migration override already exists')
 marker=GATEWAY.with_name('staff-permissions.json')
 marker_before=(marker.read_bytes(),marker.stat().st_mode&0o777) if marker.exists() else None
 services=['home-library-update.path','home-library-update-api.service']
 active=[name for name in services if subprocess.run(['/usr/bin/systemctl','is-active','--quiet',name]).returncode==0]
 system('stop',*services)
 try:
  with (STATE/'activation.lock').open('a') as lock:
   fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB)
   if (STATE/'maintenance.json').exists():raise RuntimeError('Existing maintenance must be resolved before migration')
   if (ROOT/'requests/ready.json').exists() or read_json(STATE/'status.json').get('phase') in ['preparing','backing_up','restarting','recovering','needs_attention']:raise RuntimeError('Finish the outstanding update or recovery first')
   secure=work/'verified';secure.mkdir(mode=0o700)
   for name,limit in [('manifest.json',16<<20),('manifest.sig',1024),('bundle.tar.gz',MAX_BUNDLE)]:safe_copy(args.assets/name,secure/name,limit)
   manifest=verify(secure/'manifest.json',secure/'manifest.sig','/etc/home-library-updater/public.pem',INSTALLED/'release-verify')
   if manifest['version']!=args.version or manifest['commit']!=args.commit:raise ValueError('Signed release does not match the approved version and commit')
   if manifest['schema']!=2 or manifest.get('runtime')!='static-v1':raise ValueError('Expected signed static schema 2 release')
   candidate=RELEASES/manifest['version']
   if candidate==previous:raise ValueError('Release must have a new version')
   check_bundle(secure/'bundle.tar.gz',manifest)
   if candidate.exists():
    protected(candidate);check_existing(candidate,manifest)
   else:extract(secure/'bundle.tar.gz',manifest,candidate)
   atomic_json(candidate/'RELEASE.json',{'version':manifest['version'],'commit':manifest['commit'],'runtime':'static-v1','schema':2})
   atomic_json(STATE/'maintenance.json',{'job':job})
   for _ in range(190):
    version_status,version_info,_=request('http://127.0.0.1:8080','/api/version')
    if version_status!=200 or not isinstance(version_info,dict):raise RuntimeError('Cannot confirm existing application readiness')
    if version_info.get('active_writes',0)==0:break
    time.sleep(1)
   else:raise RuntimeError('Writes did not drain')
   stopped=True;system('stop','home-library.service')
   compare(DATA,baseline);final_snapshot=private_backups/'final-snapshot'
   as_user('home-library',previous/'bin/library-backup','create','-data',DATA,'-out',final_snapshot)
   as_user('home-library',previous/'bin/library-backup','verify','-backup',final_snapshot)
   compare(final_snapshot,baseline)
   # Save root-only recovery copies before changing any gateway/service/helper.
   shutil.copy2(GATEWAY,job_dir/'Caddyfile.before')
   (job_dir/'helpers.before').mkdir(mode=0o700)
   for name in ['updates_common.py','update-api.py','update-activate.py','start-managed-library.sh']:shutil.copy2(INSTALLED/name,job_dir/'helpers.before'/name)
   print('Library backup verified. Testing candidate on an independent copy…',flush=True)
   candidate_check(candidate,snapshot,work,activation)
   compare(DATA,baseline)
   proposed=work/'Caddyfile';proposed.write_bytes(gateway)
   run(['/usr/local/bin/library-caddy','adapt','--config',proposed,'--adapter','caddyfile'],env={**os.environ,'LIBRARY_LOGIN_USER':'fixture','LIBRARY_LOGIN_HASH':'fixture'},stdout=subprocess.DEVNULL)
   config_changed=True
   write_private(GATEWAY,gateway,0o644)
   system('restart','home-library-gateway.service');system('is-active','--quiet','home-library-gateway.service')
   atomic_json(GATEWAY.with_name('staff-permissions.json'),{'format':1,'capability':'staff-update-authorization-v1','config_sha256':hashlib.sha256(gateway).hexdigest()});require_staff_gateway()
   helpers_changed=True
   for name in ['updates_common.py','update-api.py','update-activate.py','start-managed-library.sh']:write_private(INSTALLED/name,(stage_source/name).read_bytes(),0o755 if name.endswith('.sh') else 0o644)
   dropin_created=True
   write_private(dropin,b'[Service]\nUnsetEnvironment=LIBRARY_WALLET_CONFIG LIBRARY_PUBLIC_URL\nExecStart=\nExecStart=/usr/local/libexec/home-library-updater/start-managed-library.sh\n')
   system('daemon-reload')
   atomic_json(STATE/'status.json',{'job':job,'version':manifest['version'],'previous':str(previous),'phase':'restarting','message':'Migrating to individual staff access; writes remain paused.'})
   switched=True
   activation.switch(candidate)
   system('start','home-library.service')
   if not activation.ready(manifest['version']):raise RuntimeError('New library readiness failed')
   compare(DATA,baseline);member_checks('http://127.0.0.1:8080')
   # CLI is non-root and receives the interactive password only through stdin.
   as_user('home-library',candidate/'bin/library-account','-data',DATA,'-username',args.username,'-display-name',args.display_name,'bootstrap',input=(password+'\n').encode())
   # Authentication POSTs are also paused by maintenance; accounts now protect
   # writes, so resume only after the baseline and anonymous checks succeeded.
   (STATE/'maintenance.json').unlink()
   status,session,headers=request('http://127.0.0.1:8080','/api/auth/login','POST',{'username':args.username,'password':password})
   password=None
   if status!=200 or session.get('user',{}).get('role')!='administrator':raise RuntimeError('Administrator sign-in verification failed')
   cookie=headers.get('Set-Cookie','').split(';',1)[0]
   admin_headers={'Cookie':cookie,'X-CSRF-Token':session['csrf_token']}
   if request('http://127.0.0.1:8080','/api/members',headers=admin_headers)[0]!=200:raise RuntimeError('Staff registration queue permission failed')
   # End only the verification session; the administrator signs in afresh.
   request('http://127.0.0.1:8080','/api/auth/logout','POST',{},admin_headers)
   compare(DATA,baseline)
   write_private(job_dir/'after.json',json.dumps(fingerprint(DATA,baseline['columns']),sort_keys=True).encode())
   print('PASS: catalogue fields and covers unchanged; anonymous/member permissions and administrator queue verified; Wallet disabled.',flush=True)
   (STATE/'maintenance.json').unlink(missing_ok=True)
   atomic_json(STATE/'status.json',{'job':job,'version':manifest['version'],'previous':str(previous),'phase':'ready','message':'Staff/member release installed; library data verified; Wallet providers disabled.'})
   success=True
   atomic_json(state_path,{'phase':'ready','job':job,'backup':str(snapshot),'final_backup':str(final_snapshot),'archive':str(archive),'sha256':digest,'version':manifest['version'],'commit':manifest['commit'],'baseline':str(job_dir/'baseline.json')});os.chown(state_path,operator.pw_uid,operator.pw_gid);state_path.chmod(0o600)
   print('Installed and verified '+manifest['version']+'. Wallet providers are disabled. Private verified backup: '+str(snapshot),flush=True)
 except BaseException:
  success=False;password=None
  atomic_json(state_path,{'phase':'needs_attention' if switched else 'failed_before_migration','job':job,'backup':str(snapshot),'archive':str(archive),'sha256':digest});os.chown(state_path,operator.pw_uid,operator.pw_gid);state_path.chmod(0o600)
  if switched:
   atomic_json(STATE/'maintenance.json',{'job':job})
   atomic_json(STATE/'status.json',{'job':job,'phase':'needs_attention','message':'Staff migration needs administrator attention. Do not start a schema 1 release; writes remain paused.'})
   print('Migration needs attention. Verified backup retained at '+str(job_dir)+'. No automatic database downgrade attempted.',file=sys.stderr,flush=True)
  elif stopped:
   # Before activation no migrated process has opened household data. Restore
   # the entire trusted configuration set before restarting the old release.
   if config_changed:
    original=job_dir/'Caddyfile.before';write_private(GATEWAY,original.read_bytes(),original.stat().st_mode&0o777)
    if marker_before is None:marker.unlink(missing_ok=True)
    else:write_private(marker,marker_before[0],marker_before[1])
   if helpers_changed:
    for original in (job_dir/'helpers.before').iterdir():write_private(INSTALLED/original.name,original.read_bytes(),original.stat().st_mode&0o777)
   if dropin_created:dropin.unlink(missing_ok=True)
   if config_changed or helpers_changed or dropin_created:
    system('daemon-reload');system('restart','home-library-gateway.service')
   system('start','home-library.service');(STATE/'maintenance.json').unlink(missing_ok=True)
  else:(STATE/'maintenance.json').unlink(missing_ok=True)
  raise
 finally:
  # Never start the activation path while an incomplete schema migration needs
  # attention: an old recovery policy must not select the pre-permissions app.
  if success or not switched:
   if active:system('start',*active)
  if work.exists():shutil.rmtree(work)

if __name__=='__main__':main()
