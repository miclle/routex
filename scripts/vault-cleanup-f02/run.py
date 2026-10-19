#!/usr/bin/env python3
"""One controlled expected post-effect refusal/restart; never normal acknowledgement."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import re
import signal
import stat
import subprocess
import sys
import tempfile
import time

KIT=Path(__file__).resolve().parent
REPO=KIT.parent.parent
VAULT='hashicorp/vault:2.2.0@sha256:e0acd3c681bdf57f002d7214e5d050b876e1704678005c6b0406dd96a65be919'
POSTGRES='postgres:18-bookworm@sha256:67ba68a22dc7d5af3627b7e0f33d10bbefad07562ff9703b3a6c2de247649129'
BASE='debian:bookworm-slim@sha256:a1b86db52ce3daef089e45aabe36dfec4091f82464c25c1fdcf03de197cbe82a'

def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def require(value):
 if not value:raise RuntimeError('controlled acceptance refused')

def normal_prerequisite(value,uid):
 require(isinstance(value,dict) and value.get('accepted') is True and value.get('driver')=='postgres')
 require(value.get('source_unchanged') is True and value.get('original_containers_network_image_binaries_absent') is True)
 require(value.get('native_files')==29 and value.get('preflight_files')==18 and value.get('paired_audit_calls')==5)
 require(value.get('observer_Cookie_headers')==[False,False,False] and value.get('F02') is False and value.get('global_completion') is False and value.get('host_uid')==uid)
 for key,size in [('run',32),('source',40),('source_manifest',64),('raw_audit_sha256',64)]:require(isinstance(value.get(key),str) and re.fullmatch('[a-f0-9]{'+str(size)+'}',value[key]))
 return value

def private_output():
 parent=Path.home()/'.local/share/routex/acceptance/f02-vault'
 parent.mkdir(mode=0o700,parents=True,exist_ok=True);parent=parent.resolve(strict=True)
 require(parent.is_dir() and parent.stat().st_uid==os.getuid() and stat.S_IMODE(parent.stat().st_mode)==0o700)
 return Path(tempfile.mkdtemp(prefix='routex-vault-f02-',dir=parent)).resolve()

def retire_artifacts(root,expected):
 artifacts=root/'artifacts';require(artifacts.is_dir() and not artifacts.is_symlink())
 require({p.name for p in artifacts.iterdir()}==set(expected))
 for name,digest in expected.items():
  p=artifacts/name;require(p.is_file() and not p.is_symlink() and sha(p)==digest)
 shutil.rmtree(artifacts);require(not artifacts.exists())
 return dict(after_verified_closure=True,binaries=expected,removed=True)

def main():
 require(len(sys.argv)==10 and sys.argv[1]=='--exclusive-validation-lane' and sys.argv[2]=='--normal-receipt' and sys.argv[4]=='--normal-receipt-sha256' and sys.argv[6]=='--logging-receipt' and sys.argv[8]=='--logging-receipt-sha256')
 os.umask(0o077)
 uid=os.getuid();require(uid>0)
 root=private_output();(root/'commands').mkdir(mode=0o700);(root/'input').mkdir(mode=0o700);(root/'artifacts').mkdir(mode=0o700);(root/'journal').mkdir(mode=0o700)
 run=secrets.token_hex(16);project='routex-f28-f02-'+run
 started=datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='microseconds').replace('+00:00','Z')
 end=time.monotonic()+1200;rows=[];owned={};network=None;stage='source';failed=None;native_started=False;preflight_started=False;native_terminal=None;closure=False;replay_started=False;replay_terminal=None
 def save(name,value):
  raw=value if isinstance(value,bytes) else json.dumps(value,indent=2).encode()
  with (root/name).open('xb') as f:os.chmod(f.name,0o600);f.write(raw)
 def command(name,args,timeout=20,env=None,allow=False):
  nonlocal stage
  stage=name;index=len(rows)+1;prefix=f'commands/{index:03d}-{name}';t=time.monotonic();require(t<end)
  entry=dict(name=name,argv=args,started_at=datetime.datetime.now(datetime.timezone.utc).isoformat());rows.append(entry)
  with (root/(prefix+'.stdout')).open('xb') as out,(root/(prefix+'.stderr')).open('xb') as err:
   p=subprocess.Popen(args,cwd=REPO,stdin=subprocess.DEVNULL,stdout=out,stderr=err,env=env,start_new_session=True);entry.update(pid=p.pid,pgid=p.pid);save(prefix+'.start.json',entry)
   timed=False;overflow=False
   try:
    while p.poll() is None:
     if time.monotonic()-t>timeout or time.monotonic()>end or out.tell()>(16<<20) or err.tell()>(4<<20):
      timed=time.monotonic()-t>timeout or time.monotonic()>end;overflow=not timed;os.killpg(p.pid,signal.SIGTERM);break
     time.sleep(.05)
    try:code=p.wait(timeout=10)
    except subprocess.TimeoutExpired:os.killpg(p.pid,signal.SIGKILL);code=p.wait(timeout=5)
   finally:
    if p.poll() is None:os.killpg(p.pid,signal.SIGTERM);p.wait(timeout=10)
  try:os.killpg(p.pid,0);gone=False
  except ProcessLookupError:gone=True
  entry.update(exit_code=code,joined=True,group_absent=gone,timed_out=timed,overflow=overflow,duration=time.monotonic()-t,stdout_sha256=sha(root/(prefix+'.stdout')),stderr_sha256=sha(root/(prefix+'.stderr')));save(prefix+'.terminal.json',entry)
  require(gone and not timed and not overflow and (code==0 or allow))
  return (root/(prefix+'.stdout')).read_bytes()
 def docker(name,args,timeout=20,allow=False):return command(name,['docker']+args,timeout,allow=allow)
 def source():
  names=command('source-list',['git','ls-files','-z','--cached','--others','--exclude-standard']).decode().split('\0');items=[]
  for n in sorted(set(names)-{''}):
   p=REPO/n;require(p.is_file() and not p.is_symlink());items.append(dict(path=n,mode=stat.S_IMODE(p.stat().st_mode),sha256=sha(p)))
  return dict(files=items,sha256=hashlib.sha256(json.dumps(items,sort_keys=True,separators=(',',':')).encode()).hexdigest())
 def receipt(path_text,digest):
  require(len(digest)==64 and all(c in '0123456789abcdef' for c in digest))
  p=Path(path_text);require(p.is_absolute() and p.resolve()==p and not p.is_symlink())
  parent=p.parent.stat();info=p.stat();require(stat.S_IMODE(parent.st_mode)==0o700 and parent.st_uid==uid and stat.S_IMODE(info.st_mode)==0o600 and info.st_uid==uid and info.st_nlink==1 and stat.S_ISREG(info.st_mode) and 0<info.st_size<=65536)
  raw=p.read_bytes();require(len(raw)==info.st_size and hashlib.sha256(raw).hexdigest()==digest);return json.loads(raw)
 normal_sha=sys.argv[5];normal=receipt(sys.argv[3],normal_sha)
 normal_prerequisite(normal,uid)
 safe=receipt(sys.argv[7],sys.argv[9]);require(safe['status']=='PASS_ACTUAL_PRIVATE_PG18_SAFE_ERROR_APPEND_AND_TYPED_ROLLBACK' and safe['whole_original_append_count']==1 and safe['private_marker_and_password_absent'] and safe['product_cleanup'] is False and safe['F02'] is False)
 save('normal-dependency.json',normal);save('logging-dependency.json',safe);save('runner-source.py',Path(__file__).read_bytes())
 before=source();save('source-before.json',before);head=command('source-head',['git','rev-parse','HEAD']).decode().strip();require(len(head)==40)
 compose=['compose','-p',project,'-f',str(root/'compose.yaml')]
 images={};capsule=project+':fresh';capsule_id=None;saved_configs={};binaries={}
 def inspect(role,stopped=False):
  require(role in owned);v=json.loads(docker('inspect-'+role,['inspect',owned[role]]))[0]
  require(v['Id']==owned[role] and v['Image']==images[role] and v['Name']=='/'+project+'-'+role+'-1')
  lab=v['Config']['Labels'];require(lab['com.docker.compose.project']==project and lab['com.docker.compose.service']==role and lab['routex.acceptance.run']==run)
  require(not v['HostConfig']['Privileged'] and not v['State']['OOMKilled'] and v['HostConfig']['RestartPolicy']['Name']=='no')
  require(not v['HostConfig'].get('PortBindings') and all(not x for x in (v['NetworkSettings'].get('Ports') or {}).values()))
  if role!='postgres':require(v['Config']['User']==f'{uid}:{uid}' and v['HostConfig']['ReadonlyRootfs'] and v['HostConfig']['CapDrop']==['ALL'] and not v['HostConfig'].get('CapAdd'))
  require(not any(m['Type']=='volume' for m in v.get('Mounts',[])))
  if stopped:require(not v['State']['Running'] and v['State']['Pid']==0 and v['State']['FinishedAt']!='0001-01-01T00:00:00Z')
  else:
   expected_finish=saved_configs.get(role,{}).get('State',{}).get('FinishedAt','0001-01-01T00:00:00Z')
   require(v['State']['Running'] and v['State']['Pid']>0 and v['State']['FinishedAt']==expected_finish)
   if expected_finish!='0001-01-01T00:00:00Z':require(role=='gateway' and v['State']['StartedAt']>expected_finish and (root/'original-gateway-stopped.json').is_file())
  if role in saved_configs:require(v['Config']==saved_configs[role]['Config'] and v['Mounts']==saved_configs[role]['Mounts'] and v['HostConfig']==saved_configs[role]['HostConfig'] and v['State']['StartedAt']==saved_configs[role]['State']['StartedAt'])
  return v
 def discover():
  nonlocal network
  ids=docker('namespace-census',['ps','-aq','--filter','label=com.docker.compose.project='+project]).decode().split();require(0<len(ids)<=4)
  for cid in ids:
   v=json.loads(docker('capture-container',['inspect',cid]))[0];role=v['Config']['Labels']['com.docker.compose.service'];require(role in images and role not in owned and len(v['Id'])==64);owned[role]=v['Id']
  for role in owned:
   saved_configs[role]=inspect(role);save('owned-'+role+'.json',saved_configs[role])
  nets=docker('network-census',['network','ls','-q','--no-trunc','--filter','label=com.docker.compose.project='+project]).decode().split();require(len(nets)==1);network=nets[0];check_network()
 def check_network(stopped=False):
  v=json.loads(docker('inspect-network',['network','inspect',network]))[0];require(v['Id']==network and v['Name']==project+'_default' and v['Internal'] and v['Labels']['com.docker.compose.project']==project)
  expected=set() if stopped else {owned[r] for r in owned if r in ('postgres','anchor')};require(set(v['Containers'])==expected);save('network-'+str(len(rows))+'.json',v)
 try:
  env=dict(os.environ,GOOS='linux',GOARCH='arm64',CGO_ENABLED='0')
  command('install-fresh-frontend',['npm','--prefix','website','ci','--offline','--no-audit','--no-fund'],180)
  command('fresh-assets',['npm','--prefix','website','run','build'],120)
  for name,pkg,flags in [
   ('routex','./cmd/routex','-X main.CommitID='+head),
   ('wire-client','./scripts/vault-cleanup-wire-client',f'-X main.BuildRunID={run} -X main.BuildSourceManifest={before["sha256"]} -X main.BuildHostUID={uid}'),
   ('f02-client','./scripts/vault-cleanup-f02/client',f'-X main.BuildRunID={run} -X main.BuildSource={head} -X main.BuildManifest={before["sha256"]} -X main.BuildHostUID={uid} -X main.BuildStarted={started} -X main.BuildNormalReceipt={normal_sha}'),
   ('cleanup-evidence','./scripts/provider-credential-cleanup-evidence/cmd/cleanup-evidence',f'-X main.BuildRunID={run} -X main.BuildSource={head} -X main.BuildPhase=f02 -X main.BuildTrustedHostUID={uid}')]:
   command('build-'+name,['go','build','-trimpath','-ldflags',flags,'-o',str(root/'artifacts'/name),pkg],180,env)
   binaries[name]=sha(root/'artifacts'/name)
   save('build-'+name+'.json',dict(sha256=binaries[name],source_manifest=before['sha256'],head=head))
  for name,pkg,flags in [
   ('fault-helper','./scripts/vault-cleanup-f02/installer',f'-X main.BuildRunID={run} -X main.BuildSource={head} -X main.BuildNormalSchemaReceiptSHA256={normal_sha} -X main.BuildTrustedHostUID={uid}'),
   ('pg-qualifier','./scripts/vault-cleanup-f02/postgres',f'-X main.BuildRunID={run} -X main.BuildSource={head} -X main.BuildTrustedHostUID={uid}')]:
   command('build-'+name,['go','build','-trimpath','-ldflags',flags,'-o',str(root/'artifacts'/name),pkg],180,env)
   binaries[name]=sha(root/'artifacts'/name)
   save('build-'+name+'.json',dict(sha256=binaries[name],source_manifest=before['sha256'],head=head))
  for name,pkg in [('fault-controls','./scripts/vault-cleanup-f02/installer'),('f02-controls','./scripts/vault-cleanup-f02/client'),('pg-controls','./scripts/vault-cleanup-f02/postgres')]:
   command('build-'+name,['go','test','-c','-trimpath','-o',str(root/'artifacts'/name),pkg],180,env)
   binaries[name]=sha(root/'artifacts'/name)
   save('build-'+name+'.json',dict(sha256=binaries[name],source_manifest=before['sha256'],head=head))
  for name,ref in [('vault',VAULT),('postgres',POSTGRES),('base',BASE)]:
   docker('pull-'+name,['pull','--platform','linux/arm64',ref],180);images[name]=docker('image-'+name,['image','inspect','--format','{{.Id}}',ref]).decode().strip()
  (root/'.dockerignore').write_text('*\n!artifacts/\n!artifacts/**\n!Dockerfile\n')
  (root/'Dockerfile').write_text('FROM '+BASE+'\nCOPY --chmod=0555 artifacts/ /opt/routex/\nUSER '+str(uid)+':'+str(uid)+'\nCMD ["sleep","900"]\n');os.chmod(root/'Dockerfile',0o644)
  docker('build-capsule',['build','--network=none','--no-cache','--platform','linux/arm64','-t',capsule,'-f',str(root/'Dockerfile'),str(root)],120)
  capsule_id=docker('capsule-id',['image','inspect','--format','{{.Id}}',capsule]).decode().strip();images['anchor']=capsule_id;images['gateway']=capsule_id
  password=secrets.token_urlsafe(36);save('input/database-password',password.encode());save('input/config.yaml',('addr: "0.0.0.0:19001"\ndriver: postgres\ndsn: "host=postgres port=5432 user=routex_f28_f02 password='+password+' dbname=routex_f28_f02_'+run+' sslmode=disable connect_timeout=2 application_name=routex-f28-runtime-'+run[:16]+'"\nencryption_key: "'+secrets.token_urlsafe(32)+'"\nevent_queue_path: "/journal/calls.db"\nallow_private_upstreams: true\n').encode())
  save('input/database-container-id',b'not-captured')
  save('input/restart-started-at',b'not-restarted')
  # Standard base64 root key is required, not URL-safe base64.
  import base64
  cfg=root/'input/config.yaml';cfg.write_text(cfg.read_text().replace(cfg.read_text().split('encryption_key: "')[1].split('"')[0],base64.b64encode(secrets.token_bytes(32)).decode()))
  (root/'vault.hcl').write_text('disable_mlock=true\nui=false\napi_addr="http://127.0.0.1:8200"\nstorage "file" { path="/vault/file" }\nlistener "tcp" {\n address="127.0.0.1:8200"\n tls_disable=true\n}\ntelemetry {\n prometheus_retention_time="120s"\n disable_hostname=true\n}\n');os.chmod(root/'vault.hcl',0o644)
  common=f'''    platform: linux/arm64
    restart: 'no'
    logging: {{driver: none}}
    labels: {{routex.acceptance.run: {run}}}
    security_opt: ['no-new-privileges:true']
    healthcheck: {{disable: true}}
'''
  pgcommon=common.replace('healthcheck: {disable: true}', 'healthcheck: {test: [CMD-SHELL, \"pg_isready -h 127.0.0.1 -U routex_f28_f02 -d routex_f28_f02_'+run+' >/dev/null 2>&1\"], interval: 1s, timeout: 2s, retries: 45}')
  pgcommon=pgcommon.replace('logging: {driver: none}', 'logging: {driver: json-file, options: {max-size: \"1m\", max-file: \"1\"}}')
  gatewaycommon=common.replace('logging: {driver: none}', 'logging: {driver: json-file, options: {max-size: \"1m\", max-file: \"1\"}}')
  text=f'''services:
  postgres:
    image: {POSTGRES}
{pgcommon}    environment:
      POSTGRES_USER: routex_f28_f02
      POSTGRES_DB: routex_f28_f02_{run}
      POSTGRES_PASSWORD_FILE: /input/database-password
    volumes: ['{root}/input/database-password:/input/database-password:ro']
    tmpfs: ['/var/lib/postgresql:size=512m', '/var/run/postgresql:size=8m']
    command: [postgres, '-c', log_error_verbosity=terse, '-c', log_min_error_statement=panic, '-c', log_parameter_max_length_on_error=0, '-c', log_statement=none, '-c', log_duration=off, '-c', 'log_connections=', '-c', log_disconnections=off, '-c', lc_messages=C, '-c', log_timezone=UTC, '-c', 'log_line_prefix=%m|%p|%a|%d|%e|']
  anchor:
    image: {capsule}
{common}    init: true
    command: [sleep, '900']
    read_only: true
    cap_drop: [ALL]
  vault:
    image: {VAULT}
{common}    user: '{uid}:{uid}'
    network_mode: service:anchor
    entrypoint: [vault]
    command: [server, '-config=/config/vault.hcl']
    environment: {{HOME: /tmp}}
    volumes:
      - '{root}/vault.hcl:/config/vault.hcl:ro'
      - '{root}/artifacts:/opt/routex:ro'
      - '{root}/input/database-password:/input/database-password:ro'
      - '{root}/input/database-container-id:/input/database-container-id:ro'
      - '{root}/input/restart-started-at:/input/restart-started-at:ro'
    tmpfs: ['/vault/file:uid={uid},gid={uid},mode=0700,size=128m', '/vault/logs:uid={uid},gid={uid},mode=0700,size=16m', '/tmp:mode=1777,size=16m', '/fixture-f28-input:uid={uid},gid={uid},mode=0700,size=1m']
    read_only: true
    cap_drop: [ALL]
  gateway:
    image: {capsule}
{gatewaycommon}    user: '{uid}:{uid}'
    network_mode: service:anchor
    depends_on:
      postgres: {{condition: service_healthy}}
      anchor: {{condition: service_started}}
    command: [/opt/routex/routex, '-c', /input/config.yaml]
    volumes: ['{root}/input/config.yaml:/input/config.yaml:ro', '{root}/journal:/journal']
    tmpfs: ['/tmp:uid={uid},gid={uid},mode=0700,size=256m']
    read_only: true
    cap_drop: [ALL]
networks:
  default:
    internal: true
'''
  save('compose.yaml',text.encode());save('build-input.json',dict(run=run,project=project,host_uid=uid,source=head,source_manifest=before['sha256'],started_at=started,images=images))
  docker('compose-up',compose+['up','-d','--no-build'],90);discover();require(set(owned)=={'postgres','anchor','vault','gateway'})
  preflight_started=True
  docker('native-preflight',['exec','--user',f'{uid}:{uid}',owned['vault'],'/opt/routex/wire-client','--app-role-reader'],75)
  # Complete current topology identity precedes every protected native input.
  (root/'input/database-container-id').write_text(owned['postgres'])
  for name in ['fault-controls','f02-controls','pg-controls']:
   controls=docker('native-'+name,['exec','--user',f'{uid}:{uid}',owned['vault'],'/opt/routex/'+name,'-test.v','-test.count=1'],45).decode()
   require(controls.endswith('PASS\n') and '--- SKIP:' not in controls and '--- FAIL:' not in controls)
   import re
   require(len(re.findall(r'^--- PASS:',controls,re.M))=={'fault-controls':12,'f02-controls':6,'pg-controls':2}[name])
  def capture_logs(label):
   data=docker(label,['logs',owned['postgres']]);entry=rows[-1];prefix=f"commands/{len(rows):03d}-{label}";err=(root/(prefix+'.stderr')).read_bytes();save(label+'.stdout',data);save(label+'.stderr',err);return data,err
  pg_before=capture_logs('postgres-original-before')
  native_started=True
  docker('native-original',['exec','--user',f'{uid}:{uid}',owned['vault'],'/opt/routex/f02-client','original'],110)
  pg_after=capture_logs('postgres-original-after')
  additions=[]
  for before_log,after_log in zip(pg_before,pg_after):
   require(after_log.startswith(before_log))
   if after_log[len(before_log):]:additions.append(after_log[len(before_log):])
  require(len(additions)==1);raw_error=additions[0];save('original-complete-error-append.log',raw_error)
  import re
  grammar=rb'^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3} UTC\|[1-9][0-9]*\|routex-f28-runtime-'+run[:16].encode()+rb'\|routex_f28_f02_'+run.encode()+rb'\|23514\|ERROR:  new row for relation "credential_published_cleanups" violates check constraint "routex_fixture_f28_postack_refusal"\n$'
  require(re.fullmatch(grammar,raw_error) is not None and password.encode() not in raw_error)
  save('before-restart-gateway.json',inspect('gateway'));docker('join-original-gateway',['stop','--time','10',owned['gateway']],20);stopped=inspect('gateway',True);require(stopped['State']['ExitCode']==0);save('original-gateway-stopped.json',stopped)
  docker('same-binary-restart',['start',owned['gateway']]);new=json.loads(docker('new-gateway-capture',['inspect',owned['gateway']]))[0];docker('restarted-gateway-original-logs',['logs',owned['gateway']]);old=saved_configs['gateway']
  for field in ['Id','Image','Name','Config','HostConfig','Mounts']:require(new[field]==old[field])
  require(new['State']['Running'] and new['State']['Pid']>0 and new['State']['StartedAt']>stopped['State']['FinishedAt']);saved_configs['gateway']=new;save('restarted-gateway.json',new);(root/'input/restart-started-at').write_text(new['State']['StartedAt'])
  replay_started=True
  docker('native-replay',['exec','--user',f'{uid}:{uid}',owned['vault'],'/opt/routex/f02-client','replay'],110)

 except BaseException:
  failed=stage
 finally:
  # Closure has a separate finite obligation; it never renews business success.
  end=time.monotonic()+70
  try:
   if not owned and (root/'compose.yaml').exists():discover()
   if owned:
    require(network is not None);check_network()
    docker('original-gateway-final-logs',['logs',owned['gateway']],allow=True)
    # Preserve the completed original preflight before deleting its tmpfs.
    pre='/vault/logs/discovery-'+run
    pre_raw=docker('preflight-terminal',['exec','--user',f'{uid}:{uid}',owned['vault'],'cat',pre+'/native-terminal.json'],allow=True)
    if rows[-1]['exit_code']==0:
     pre_terminal=json.loads(pre_raw);require(pre_terminal['run']==run and pre_terminal['source_manifest']==before['sha256']);save('preflight-terminal.json',pre_raw)
     expected={'private-initialization.json','private-token-writer.json','private-token-cleanup.json','private-material.json','metrics-before.prom','metrics-after.prom','observed-wire.json','http-census.json','private-response-dummy-create.json','private-response-dummy-read.json','private-response-dummy-metadata.json','private-response-audit-version-hash.json','audit-before.jsonl','audit-after.jsonl','private-response-reader-role-id.json','private-response-reader-secret-id.json','private-response-reader-login.json','private-reader-material.json'}
     names=[f['name'] for f in pre_terminal['files']];require(len(names)==len(set(names)) and set(names)<=expected)
     if pre_terminal['passed']:require(set(names)==expected)
     for f in pre_terminal['files']:
      raw=docker('capture-preflight-file',['exec','--user',f'{uid}:{uid}',owned['vault'],'cat',pre+'/'+f['name']]);require(len(raw)==f['bytes'] and hashlib.sha256(raw).hexdigest()==f['sha256']);save('preflight-'+f['name'],raw)
    else:
     require(not preflight_started and not native_started)
    final_audit=docker('final-vault-audit',['exec','--user',f'{uid}:{uid}',owned['vault'],'cat','/vault/logs/audit.jsonl'],allow=True)
    if rows[-1]['exit_code']==0:save('vault-final-audit.jsonl',final_audit)
    # Independently fixed artifact names, original and replay scopes kept apart.
    phase_responses={
     'original':{'setup','integration-catalog','integration-create','policy-read','policy-write','storage-context','provider-create','retained-preview','credential-metadata','credential-delete','cleanup-preview','version-hash','metrics-before','original-cleanup','metrics-after','pending-command-read'},
     'replay':{'same-session-refresh','expired-token-free-replay','expired-command-read'}}
    phase_files={
     'original':{'http-census.json','f02-observations.json','private-original-session.json','postgres-settings.json','fault-helper-result.json','fault-helper-terminal.json','private-original-expectation.json','private-helper-input.json','helper-result.json','helper-terminal.json','audit-before.jsonl','audit-after.jsonl'},
     'replay':{'http-census.json','f02-observations.json','private-helper-input.json','helper-result.json','helper-terminal.json','replay-audit-before.jsonl','replay-audit-after.jsonl','private-restart-lifecycle.json'}}
    for phase,dispatched in [('original',native_started),('replay',replay_started)]:
     if not dispatched:continue
     prefix='/vault/logs/f02-'+phase+'-'+run;raw=docker('f02-'+phase+'-terminal',['exec','--user',f'{uid}:{uid}',owned['vault'],'cat',prefix+'/f02-terminal.json']);value=json.loads(raw)
     require(value['phase']==phase and value['normal_acknowledgement'] is False and value['run']==run and value['source']==head and value['source_manifest']==before['sha256']);save('captured-'+phase+'-terminal.json',raw)
     if phase=='original':native_terminal=value
     else:replay_terminal=value
     fixed=phase_files[phase]|{'private-response-'+n+'.json' for n in phase_responses[phase]};names=[f['name'] for f in value['files']];require(len(names)==len(set(names)) and set(names)<=fixed)
     if value['passed']:require(set(names)==fixed)
     for f in value['files']:
      raw=docker('capture-'+phase+'-file',['exec','--user',f'{uid}:{uid}',owned['vault'],'cat',prefix+'/'+f['name']]);require(len(raw)==f['bytes'] and hashlib.sha256(raw).hexdigest()==f['sha256']);save('captured-'+phase+'-'+f['name'],raw)
    require(set(owned)=={'postgres','anchor','vault','gateway'})
    for role in ['gateway','vault','postgres','anchor']:
     save('before-stop-'+role+'.json',inspect(role));docker('stop-'+role,['stop','--time','10',owned[role]],20);v=inspect(role,True);save('stopped-'+role+'.json',v);require(v['State']['ExitCode'] in ([0,143] if role=='anchor' else [0]))
    check_network(stopped=True)
    for role in ['gateway','vault','postgres','anchor']:inspect(role,True);docker('remove-'+role,['rm',owned[role]])
    v=json.loads(docker('network-before-remove',['network','inspect',network]))[0];require(not v['Containers'] and v['Id']==network and v['Internal']);save('network-before-remove.json',v);docker('remove-network',['network','rm',network])
    require(not docker('final-containers',['ps','-aq','--filter','label=com.docker.compose.project='+project]).strip());require(not docker('final-network',['network','ls','-q','--filter','label=com.docker.compose.project='+project]).strip());require(not docker('final-volumes',['volume','ls','-q','--filter','label=com.docker.compose.project='+project]).strip())
    for cid in owned.values():
     raw=docker('exact-container-absence',['inspect',cid],allow=True);require(rows[-1]['exit_code']!=0 and raw.strip()==b'[]')
    after=source();save('source-after.json',after);require(before==after and Path(__file__).read_bytes()==(root/'runner-source.py').read_bytes());closure=True
    require(capsule_id is not None and docker('cleanup-image-identity',['image','inspect','--format','{{.Id}}',capsule]).decode().strip()==capsule_id)
    docker('remove-capsule',['image','rm',capsule])
    gone=docker('exact-image-absence',['image','inspect',capsule],allow=True);require(rows[-1]['exit_code']!=0 and gone.strip()==b'[]')
    retired=retire_artifacts(root,binaries);retired.update(image_removed=True,image_id=capsule_id);save('owned-artifact-cleanup.json',retired)
  except BaseException:
   failed=failed or stage;closure=False
  save('processes.json',rows);passed=failed is None and closure and native_terminal is not None and native_terminal['passed'] is True and replay_terminal is not None and replay_terminal['passed'] is True
  result=dict(passed=passed,failed_stage=failed,recorded_closure=closure,native_terminal=native_terminal,replay_terminal=replay_terminal,normal_acknowledgement=False,output=str(root),global_completion=False);save('terminal.json',result);print(json.dumps(result))
 return 0 if passed else 1

if __name__=='__main__':sys.exit(main())
