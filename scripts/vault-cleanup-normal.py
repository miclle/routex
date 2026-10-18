#!/usr/bin/env python3
"""Opt-in isolated normal API/SQL/Vault acceptance; not fleet acceptance."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import secrets
import signal
import stat
import subprocess
import sys
import tempfile
import time

REPO=Path(__file__).resolve().parent.parent
VAULT='hashicorp/vault:2.2.0@sha256:e0acd3c681bdf57f002d7214e5d050b876e1704678005c6b0406dd96a65be919'
POSTGRES='postgres:18-bookworm@sha256:67ba68a22dc7d5af3627b7e0f33d10bbefad07562ff9703b3a6c2de247649129'
BASE='debian:bookworm-slim@sha256:a1b86db52ce3daef089e45aabe36dfec4091f82464c25c1fdcf03de197cbe82a'

def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def require(value):
 if not value:raise RuntimeError('controlled acceptance refused')

def main():
 require(sys.argv[1:]==['--exclusive-validation-lane'])
 os.umask(0o077)
 uid=os.getuid();require(uid>0)
 root=Path(tempfile.mkdtemp(prefix='routex-vault-normal-')).resolve();(root/'commands').mkdir(mode=0o700);(root/'input').mkdir(mode=0o700);(root/'artifacts').mkdir(mode=0o700)
 run=secrets.token_hex(16);project='routex-f28-normal-'+run
 started=datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='microseconds').replace('+00:00','Z')
 end=time.monotonic()+1200;rows=[];owned={};network=None;stage='source';failed=None;native_started=False;preflight_started=False;native_terminal=None;closure=False
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
 before=source();save('source-before.json',before);head=command('source-head',['git','rev-parse','HEAD']).decode().strip();require(len(head)==40)
 compose=['compose','-p',project,'-f',str(root/'compose.yaml')]
 images={};capsule=project+':fresh';saved_configs={}
 def inspect(role,stopped=False):
  require(role in owned);v=json.loads(docker('inspect-'+role,['inspect',owned[role]]))[0]
  require(v['Id']==owned[role] and v['Image']==images[role] and v['Name']=='/'+project+'-'+role+'-1')
  lab=v['Config']['Labels'];require(lab['com.docker.compose.project']==project and lab['com.docker.compose.service']==role and lab['routex.acceptance.run']==run)
  require(not v['HostConfig']['Privileged'] and not v['State']['OOMKilled'] and v['HostConfig']['RestartPolicy']['Name']=='no')
  require(not v['HostConfig'].get('PortBindings') and all(not x for x in (v['NetworkSettings'].get('Ports') or {}).values()))
  if role!='postgres':require(v['Config']['User']==f'{uid}:{uid}' and v['HostConfig']['ReadonlyRootfs'] and v['HostConfig']['CapDrop']==['ALL'] and not v['HostConfig'].get('CapAdd'))
  require(not any(m['Type']=='volume' for m in v.get('Mounts',[])))
  if stopped:require(not v['State']['Running'] and v['State']['Pid']==0 and v['State']['FinishedAt']!='0001-01-01T00:00:00Z')
  else:require(v['State']['Running'] and v['State']['Pid']>0 and v['State']['FinishedAt']=='0001-01-01T00:00:00Z')
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
  command('fresh-assets',['npm','--prefix','website','run','build'],120)
  for name,pkg,flags in [
   ('routex','./cmd/routex','-X main.CommitID='+head),
   ('wire-client','./scripts/vault-cleanup-wire-client',f'-X main.BuildRunID={run} -X main.BuildSourceManifest={before["sha256"]} -X main.BuildHostUID={uid}'),
   ('normal-client','./scripts/vault-cleanup-normal-client',f'-X main.BuildRunID={run} -X main.BuildSource={head} -X main.BuildManifest={before["sha256"]} -X main.BuildHostUID={uid} -X main.BuildStarted={started}'),
   ('cleanup-evidence','./scripts/provider-credential-cleanup-evidence/cmd/cleanup-evidence',f'-X main.BuildRunID={run} -X main.BuildSource={head} -X main.BuildPhase=normal -X main.BuildTrustedHostUID={uid}')]:
   command('build-'+name,['go','build','-trimpath','-ldflags',flags,'-o',str(root/'artifacts'/name),pkg],180,env)
   save('build-'+name+'.json',dict(sha256=sha(root/'artifacts'/name),source_manifest=before['sha256'],head=head))
  for name,ref in [('vault',VAULT),('postgres',POSTGRES),('base',BASE)]:
   docker('pull-'+name,['pull','--platform','linux/arm64',ref],180);images[name]=docker('image-'+name,['image','inspect','--format','{{.Id}}',ref]).decode().strip()
  (root/'.dockerignore').write_text('*\n!artifacts/\n!artifacts/**\n!Dockerfile\n')
  (root/'Dockerfile').write_text('FROM '+BASE+'\nCOPY --chmod=0555 artifacts/ /opt/routex/\nUSER '+str(uid)+':'+str(uid)+'\nCMD ["sleep","900"]\n');os.chmod(root/'Dockerfile',0o644)
  docker('build-capsule',['build','--network=none','--no-cache','--platform','linux/arm64','-t',capsule,'-f',str(root/'Dockerfile'),str(root)],120)
  capsule_id=docker('capsule-id',['image','inspect','--format','{{.Id}}',capsule]).decode().strip();images['anchor']=capsule_id;images['gateway']=capsule_id
  password=secrets.token_urlsafe(36);save('input/database-password',password.encode());save('input/config.yaml',('addr: "0.0.0.0:19001"\ndriver: postgres\ndsn: "host=postgres port=5432 user=routex_f28_normal password='+password+' dbname=routex_f28_normal_'+run+' sslmode=disable connect_timeout=2"\nencryption_key: "'+secrets.token_urlsafe(32)+'"\nevent_queue_path: "/tmp/calls.db"\nallow_private_upstreams: true\n').encode())
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
  pgcommon=common.replace('healthcheck: {disable: true}', 'healthcheck: {test: [CMD-SHELL, \"pg_isready -U routex_f28_normal -d routex_f28_normal_'+run+' >/dev/null 2>&1\"], interval: 1s, timeout: 2s, retries: 45}')
  text=f'''services:
  postgres:
    image: {POSTGRES}
{pgcommon}    environment:
      POSTGRES_USER: routex_f28_normal
      POSTGRES_DB: routex_f28_normal_{run}
      POSTGRES_PASSWORD_FILE: /input/database-password
    volumes: ['{root}/input/database-password:/input/database-password:ro']
    tmpfs: ['/var/lib/postgresql:size=512m', '/var/run/postgresql:size=8m']
    command: [postgres, '-c', log_error_verbosity=terse, '-c', log_min_error_statement=panic, '-c', log_parameter_max_length_on_error=0]
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
    tmpfs: ['/vault/file:uid={uid},gid={uid},mode=0700,size=128m', '/vault/logs:uid={uid},gid={uid},mode=0700,size=16m', '/tmp:mode=1777,size=16m']
    read_only: true
    cap_drop: [ALL]
  gateway:
    image: {capsule}
{common}    user: '{uid}:{uid}'
    network_mode: service:anchor
    depends_on:
      postgres: {{condition: service_healthy}}
      anchor: {{condition: service_started}}
    command: [/opt/routex/routex, '-c', /input/config.yaml]
    volumes: ['{root}/input/config.yaml:/input/config.yaml:ro']
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
  native_started=True
  docker('native-normal',['exec','--user',f'{uid}:{uid}',owned['vault'],'/opt/routex/normal-client'],110)
 except BaseException:
  failed=stage
 finally:
  # Closure has a separate finite obligation; it never renews business success.
  end=time.monotonic()+70
  try:
   if not owned and (root/'compose.yaml').exists():discover()
   if owned:
    require(network is not None);check_network()
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
    # Capture fixed allowlisted files while the original tmpfs is still alive.
    if native_started:
     prefix='/vault/logs/normal-'+run
     raw=docker('normal-terminal',['exec','--user',f'{uid}:{uid}',owned['vault'],'cat',prefix+'/normal-terminal.json'])
     native_terminal=json.loads(raw);require(native_terminal['run']==run and native_terminal['source']==head and native_terminal['source_manifest']==before['sha256']);save('captured-normal-terminal.json',raw)
     fixed={'http-census.json','normal-observations.json','private-original-expectation.json','private-helper-input.json','helper-result.json','helper-terminal.json','helper-replay-result.json','helper-replay-terminal.json','audit-before.jsonl','audit-after.jsonl','replay-audit-before.jsonl','replay-audit-after.jsonl'}
     responses={'setup','integration-catalog','integration-create','policy-read','policy-write','storage-context','provider-create','retained-preview','credential-metadata','credential-delete','cleanup-preview','version-hash','metrics-before','original-cleanup','metrics-after','token-free-replay','original-command-read'}
     fixed|={'private-response-'+n+'.json' for n in responses}
     names=[f['name'] for f in native_terminal['files']];require(len(names)==len(set(names)) and set(names)<=fixed)
     if native_terminal['passed']:require(set(names)==fixed)
     for f in native_terminal['files']:
      raw=docker('capture-normal-file',['exec','--user',f'{uid}:{uid}',owned['vault'],'cat',prefix+'/'+f['name']]);require(len(raw)==f['bytes'] and hashlib.sha256(raw).hexdigest()==f['sha256']);save('captured-'+f['name'],raw)
    require(set(owned)=={'postgres','anchor','vault','gateway'})
    for role in ['gateway','vault','postgres','anchor']:
     save('before-stop-'+role+'.json',inspect(role));docker('stop-'+role,['stop','--time','10',owned[role]],20);v=inspect(role,True);save('stopped-'+role+'.json',v);require(v['State']['ExitCode'] in ([0,143] if role=='anchor' else [0]))
    check_network(stopped=True)
    for role in ['gateway','vault','postgres','anchor']:inspect(role,True);docker('remove-'+role,['rm',owned[role]])
    v=json.loads(docker('network-before-remove',['network','inspect',network]))[0];require(not v['Containers'] and v['Id']==network and v['Internal']);save('network-before-remove.json',v);docker('remove-network',['network','rm',network])
    require(not docker('final-containers',['ps','-aq','--filter','label=com.docker.compose.project='+project]).strip());require(not docker('final-network',['network','ls','-q','--filter','label=com.docker.compose.project='+project]).strip());require(not docker('final-volumes',['volume','ls','-q','--filter','label=com.docker.compose.project='+project]).strip())
    for cid in owned.values():
     raw=docker('exact-container-absence',['inspect',cid],allow=True);require(rows[-1]['exit_code']!=0 and raw.strip()==b'[]')
    after=source();save('source-after.json',after);require(before==after);closure=True
  except BaseException:
   failed=failed or stage;closure=False
  save('processes.json',rows);passed=failed is None and closure and native_terminal is not None and native_terminal['passed'] is True
  result=dict(passed=passed,failed_stage=failed,recorded_closure=closure,native_terminal=native_terminal,output=str(root),global_completion=False);save('terminal.json',result);print(json.dumps(result))
 return 0 if passed else 1

if __name__=='__main__':sys.exit(main())
