#!/usr/bin/env python3
"""Fresh driver-bound safe-error prerequisite, independent of product cleanup."""
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
import run as f02

REPO=Path(__file__).resolve().parents[2]
MYSQL='mysql:8.4@sha256:24989179f58856db763f418010c1169fecc944e3ec1ea7e921572d83eb070f70'

def selected_driver(args):
 if args==['--exclusive-validation-lane']:return 'postgres'
 if len(args)==3 and args[:2]==['--exclusive-validation-lane','--driver'] and args[2] in ['postgres','mysql']:return args[2]
 raise RuntimeError('controlled acceptance refused')

def mysql_service(root,run):
 return f'''  mysql:
    image: {MYSQL}
    platform: linux/arm64
    restart: 'no'
    labels: {{routex.acceptance.run: {run}}}
    logging: {{driver: json-file, options: {{max-size: '1m', max-file: '1'}}}}
    environment:
      MYSQL_USER: routex_f28_dummy
      MYSQL_DATABASE: routex_f28_dummy_{run}
      MYSQL_PASSWORD_FILE: /input/password
      MYSQL_ROOT_PASSWORD_FILE: /input/root-password
    volumes: ['{root}/input/password:/input/password:ro', '{root}/input/root-password:/input/root-password:ro', '{root}/input/client.cnf:/input/client.cnf:ro']
    tmpfs: ['/var/lib/mysql:uid=999,gid=999,mode=0700,size=1g', '/var/run/mysqld:uid=999,gid=999,mode=0755,size=8m', '/tmp:mode=1777,size=32m']
    command: [mysqld, '--innodb-buffer-pool-size=128M', '--skip-log-bin', '--default-time-zone=+00:00', '--general-log=OFF', '--slow-query-log=OFF', '--log-output=NONE', '--log-error-verbosity=2', '--log-timestamps=UTC', '--skip-name-resolve', '--local-infile=OFF']
    healthcheck:
      test: [CMD, mysql, '--defaults-extra-file=/input/client.cnf', '--execute', 'SELECT 1']
      interval: 1s
      timeout: 2s
      retries: 90
'''

def main():
 driver=selected_driver(sys.argv[1:]);qualifier='pg' if driver=='postgres' else 'mysql';os.umask(0o077)
 uid=os.getuid();f02.require(uid>0)
 parent=Path.home()/'.local/share/routex/acceptance/f02-logging';parent.mkdir(mode=0o700,parents=True,exist_ok=True)
 f02.require(parent.stat().st_uid==uid and stat.S_IMODE(parent.stat().st_mode)==0o700)
 root=Path(tempfile.mkdtemp(prefix='routex-f02-logging-',dir=parent)).resolve()
 for name in ['commands','input','artifacts']: (root/name).mkdir(mode=0o700)
 run=secrets.token_hex(16);project='routex-f28-logging-'+run;end=time.monotonic()+600
 rows=[];owned={};network=None;capsule=project+':fresh';capsule_id=None;binaries={};failed=None;closure=False;stage='source';images={}
 def save(name,value):
  raw=value if isinstance(value,bytes) else json.dumps(value,indent=2).encode()
  with (root/name).open('xb') as file:os.chmod(file.name,0o600);file.write(raw)
 def command(name,args,timeout=30,data=None,allow=False):
  nonlocal stage
  stage=name;prefix=f'commands/{len(rows)+1:03d}-{name}';started=time.monotonic();f02.require(started<end)
  with (root/(prefix+'.stdout')).open('xb') as out,(root/(prefix+'.stderr')).open('xb') as err:
   p=subprocess.Popen(args,cwd=REPO,stdin=subprocess.PIPE if data is not None else subprocess.DEVNULL,stdout=out,stderr=err,start_new_session=True)
   row=dict(name=name,argv=args,pid=p.pid,pgid=p.pid);rows.append(row);save(prefix+'.start.json',row)
   timed=False;overflow=False
   try:
    if data is not None:f02.require(len(data)<16384);p.stdin.write(data);p.stdin.close()
    while p.poll() is None:
     if time.monotonic()-started>timeout or time.monotonic()>end or out.tell()>4<<20 or err.tell()>4<<20:
      timed=time.monotonic()-started>timeout or time.monotonic()>end;overflow=not timed;os.killpg(p.pid,signal.SIGTERM);break
     time.sleep(.05)
    try:code=p.wait(timeout=10)
    except subprocess.TimeoutExpired:os.killpg(p.pid,signal.SIGKILL);code=p.wait(timeout=5)
   finally:
    if p.poll() is None:os.killpg(p.pid,signal.SIGTERM);p.wait(timeout=10)
  try:os.killpg(p.pid,0);gone=False
  except ProcessLookupError:gone=True
  row.update(joined=True,exit_code=code,group_absent=gone,timed_out=timed,overflow=overflow,stdout_sha256=f02.sha(root/(prefix+'.stdout')),stderr_sha256=f02.sha(root/(prefix+'.stderr')));save(prefix+'.terminal.json',row)
  f02.require(gone and not timed and not overflow and (code==0 or allow));return (root/(prefix+'.stdout')).read_bytes(),(root/(prefix+'.stderr')).read_bytes()
 def docker(name,args,timeout=30,data=None,allow=False):return command(name,['docker']+args,timeout,data,allow)
 def source():
  names=subprocess.check_output(['git','ls-files','-z','--cached','--others','--exclude-standard'],cwd=REPO).decode().split('\0');files=[]
  for name in sorted(set(names)-{''}):
   p=REPO/name;f02.require(p.is_file() and not p.is_symlink());files.append(dict(path=name,mode=stat.S_IMODE(p.stat().st_mode),sha256=f02.sha(p)))
  return dict(files=files,sha256=hashlib.sha256(json.dumps(files,sort_keys=True,separators=(',',':')).encode()).hexdigest())
 before=source();save('source-before.json',before);head=subprocess.check_output(['git','rev-parse','HEAD'],cwd=REPO).decode().strip()
 save('run-input.json',dict(run=run,project=project,source=head,source_manifest=before['sha256'],host_uid=uid))
 compose=['compose','-p',project,'-f',str(root/'compose.yaml')]
 def inspect(role,stopped=False):
  value=json.loads(docker('inspect-'+role,['inspect',owned[role]])[0])[0]
  f02.require(value['Id']==owned[role] and value['Image']==images[role] and value['Name']=='/'+project+'-'+role+'-1' and not value['HostConfig'].get('PortBindings') and not value['HostConfig']['Privileged'] and not any(m['Type']=='volume' for m in value['Mounts']))
  f02.require(value['Config']['Labels']['routex.acceptance.run']==run and value['Config']['Labels']['com.docker.compose.project']==project and value['HostConfig']['RestartPolicy']['Name']=='no')
  f02.require(not value['State']['OOMKilled'])
  if role=='client':f02.require(value['Config']['User']==f'{uid}:{uid}' and value['HostConfig']['ReadonlyRootfs'] and value['HostConfig']['CapDrop']==['ALL'])
  f02.require((not value['State']['Running'] and value['State']['Pid']==0) if stopped else (value['State']['Running'] and value['State']['Pid']>0))
  return value
 try:
  env=dict(os.environ,GOOS='linux',GOARCH='arm64',CGO_ENABLED='0')
  for name,args in [(qualifier+'-qualifier',['build','-ldflags',f'-X main.BuildSource={head} -X main.BuildRunID={run} -X main.BuildTrustedHostUID={uid}']),(qualifier+'-controls',['test','-c'])]:
   previous={k:os.environ.get(k) for k in ['GOOS','GOARCH','CGO_ENABLED']};os.environ.update({k:env[k] for k in previous})
   try:command('build-'+name,['go']+args+['-trimpath','-o',str(root/'artifacts'/name),'./scripts/vault-cleanup-f02/'+driver],180)
   finally:
    for key,value in previous.items():
     if value is None:os.environ.pop(key,None)
     else:os.environ[key]=value
   binaries[name]=f02.sha(root/'artifacts'/name);save('build-'+name+'.json',dict(sha256=binaries[name],source=head,source_manifest=before['sha256']))
  for name,ref in [(driver,f02.POSTGRES if driver=='postgres' else MYSQL),('base',f02.BASE)]:
   docker('pull-'+name,['pull','--platform','linux/arm64',ref],180);images[name]=docker('image-'+name,['image','inspect','--format','{{.Id}}',ref])[0].decode().strip()
  (root/'.dockerignore').write_text('*\n!artifacts/\n!artifacts/**\n!Dockerfile\n');(root/'Dockerfile').write_text('FROM '+f02.BASE+'\nCOPY --chmod=0555 artifacts/ /opt/routex/\nUSER '+str(uid)+':'+str(uid)+'\nCMD ["sleep","900"]\n')
  docker('build-capsule',['build','--no-cache','--network=none','--platform','linux/arm64','-t',capsule,str(root)],120);capsule_id=docker('capsule-id',['image','inspect','--format','{{.Id}}',capsule])[0].decode().strip();images['client']=capsule_id
  password=secrets.token_urlsafe(36);save('input/password',password.encode())
  settings={'log_error_verbosity':'terse','log_min_error_statement':'panic','log_parameter_max_length_on_error':'0','log_statement':'none','log_duration':'off','log_connections':'','log_disconnections':'off','lc_messages':'C','log_timezone':'UTC','log_line_prefix':'%m|%p|%a|%d|%e|'}
  args=['postgres']
  for key,value in settings.items():args+=['-c',key+'='+value]
  save('compose.yaml',f'''services:
  postgres:
    image: {f02.POSTGRES}
    platform: linux/arm64
    restart: 'no'
    labels: {{routex.acceptance.run: {run}}}
    logging: {{driver: json-file, options: {{max-size: '1m', max-file: '1'}}}}
    environment:
      POSTGRES_USER: routex_f28_dummy
      POSTGRES_DB: routex_f28_dummy_{run}
      POSTGRES_PASSWORD_FILE: /input/password
    volumes: ['{root}/input/password:/input/password:ro']
    tmpfs: ['/var/lib/postgresql:size=256m', '/var/run/postgresql:size=8m']
    command: {json.dumps(args)}
    healthcheck:
      test: [CMD-SHELL, 'pg_isready -h 127.0.0.1 -U routex_f28_dummy -d routex_f28_dummy_{run} >/dev/null 2>&1']
      interval: 1s
      timeout: 2s
      retries: 45
  client:
    image: {capsule}
    init: true
    platform: linux/arm64
    user: '{uid}:{uid}'
    restart: 'no'
    labels: {{routex.acceptance.run: {run}}}
    logging: {{driver: none}}
    command: [sleep, '900']
    read_only: true
    cap_drop: [ALL]
    security_opt: ['no-new-privileges:true']
    depends_on:
      postgres: {{condition: service_healthy}}
networks:
  default:
    internal: true
'''.encode())
  if driver=='mysql':
   save('input/root-password',secrets.token_urlsafe(36).encode())
   save('input/client.cnf',('[client]\nhost=127.0.0.1\nuser=routex_f28_dummy\npassword='+password+'\ndatabase=routex_f28_dummy_'+run+'\n').encode())
   text=(root/'compose.yaml').read_text();start=text.index('  postgres:');stop=text.index('  client:',start)
   text=text[:start]+mysql_service(root,run)+text[stop:];text=text.replace('postgres: {condition: service_healthy}','mysql: {condition: service_healthy}');(root/'compose.yaml').write_text(text)
  docker('compose-up',compose+['up','-d','--no-build'],90)
  ids=docker('namespace-census',['ps','-aq','--filter','label=com.docker.compose.project='+project])[0].decode().split();f02.require(len(ids)==2)
  for cid in ids:
   value=json.loads(docker('capture-container',['inspect',cid])[0])[0];role=value['Config']['Labels']['com.docker.compose.service'];f02.require(role in [driver,'client'] and role not in owned);owned[role]=value['Id']
  for role in owned:save('owned-'+role+'.json',inspect(role))
  networks=docker('networks',['network','ls','--no-trunc','-q','--filter','label=com.docker.compose.project='+project])[0].decode().split();f02.require(len(networks)==1);network=networks[0]
  n=json.loads(docker('network-proof',['network','inspect',network])[0])[0];f02.require(n['Internal'] and set(n['Containers'])==set(owned.values()));save('network.json',n)
  controls=docker('native-controls',['exec','--user',f'{uid}:{uid}',owned['client'],'/opt/routex/'+qualifier+'-controls','-test.v','-test.count=1'])[0];f02.require(controls.endswith(b'PASS\n') and controls.count(b'--- PASS:')==2 and b'--- SKIP:' not in controls)
  input=json.dumps(dict(run_id=run,source_commit=head,database_container_id=owned[driver],database_password=password)).encode()
  settings_out=docker('native-settings',['exec','-i','--user',f'{uid}:{uid}',owned['client'],'/opt/routex/'+qualifier+'-qualifier','settings-dummy'],data=input)[0];save('settings.json',settings_out)
  old=docker('logs-before',['logs',owned[driver]]);save('logs-before.stdout',old[0]);save('logs-before.stderr',old[1])
  result=docker('native-dummy',['exec','-i','--user',f'{uid}:{uid}',owned['client'],'/opt/routex/'+qualifier+'-qualifier','dummy'],data=input)[0];save('dummy-result.json',result);native=json.loads(result);f02.require(native['result']=='dummy_typed_refusal_and_checked_rollback' and native['sqlstate']==('23514' if driver=='postgres' else 'HY000') and (driver=='postgres' or native['server_error_number']==3819))
  after=docker('logs-after',['logs',owned[driver]]);save('logs-after.stdout',after[0]);save('logs-after.stderr',after[1]);appends=[]
  for left,right in zip(old,after):f02.require(right.startswith(left));appends.append(right[len(left):])
  whole=b''.join(appends);save('complete-original-append.log',whole)
  import re
  grammar=rb'^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3} UTC\|[1-9][0-9]*\|routex-f28-dummy-'+run[:16].encode()+rb'\|routex_f28_dummy_'+run.encode()+rb'\|23514\|ERROR:  new row for relation "routex_f28_dummy_postack" violates check constraint "routex_fixture_f28_dummy_refusal"\n$'
  f02.require(re.fullmatch(grammar,whole) is not None if driver=='postgres' else whole==b'')
  for raw in old+after:f02.require(password.encode() not in raw and ('routex-f28-private-'+run).encode() not in raw)
  state_args=['exec',owned[driver]]+(['psql','-U','routex_f28_dummy','-d','routex_f28_dummy_'+run,'-Atc','SELECT state FROM routex_f28_dummy_postack WHERE id=1'] if driver=='postgres' else ['mysql','--defaults-extra-file=/input/client.cnf','--batch','--skip-column-names','--execute','SELECT state FROM routex_f28_dummy_postack WHERE id=1']);state=docker('independent-row-state',state_args)[0];save('rollback-state.txt',state);f02.require(state==b'pending\n')
 except BaseException:failed=stage
 finally:
  end=time.monotonic()+70
  try:
   if owned:
    f02.require(set(owned)=={'client',driver} and network)
    for role in ['client',driver]:
     inspect(role);docker('stop-'+role,['stop','--time','5',owned[role]]);v=inspect(role,True);f02.require(v['State']['ExitCode'] in ([0,143] if role=='client' else [0]));save('stopped-'+role+'.json',v);docker('remove-'+role,['rm',owned[role]])
    n=json.loads(docker('network-before-remove',['network','inspect',network])[0])[0];f02.require(n['Internal'] and not n['Containers']);save('network-before-remove.json',n);docker('remove-network',['network','rm',network])
   for kind in ['container','network','volume']:
    args=[kind,'ls','-q']+(['-a'] if kind=='container' else [])+['--filter','label=com.docker.compose.project='+project];f02.require(not docker('absence-'+kind,args)[0].strip())
   for cid in owned.values():
    raw=docker('exact-container-absence',['inspect',cid],allow=True)[0];f02.require(rows[-1]['exit_code']!=0 and raw.strip()==b'[]')
   after_source=source();save('source-after.json',after_source);f02.require(before==after_source);closure=True
   if capsule_id:
    f02.require(docker('image-before-remove',['image','inspect','--format','{{.Id}}',capsule])[0].decode().strip()==capsule_id);docker('remove-image',['image','rm',capsule]);raw=docker('exact-image-absence',['image','inspect',capsule],allow=True)[0];f02.require(rows[-1]['exit_code']!=0 and raw.strip()==b'[]')
   retired=f02.retire_artifacts(root,binaries);retired['image_removed']=bool(capsule_id);save('artifact-cleanup.json',retired)
  except BaseException:failed=failed or stage;closure=False
  save('processes.json',rows);passed=failed is None and closure
  if passed:save('logging-receipt.json',dict(status='PASS_ACTUAL_PRIVATE_PG18_SAFE_ERROR_APPEND_AND_TYPED_ROLLBACK' if driver=='postgres' else 'PASS_ACTUAL_PRIVATE_MYSQL84_TYPED_REFUSAL_AND_CHECKED_ROLLBACK',driver=driver,whole_original_append_count=1 if driver=='postgres' else 0,original_append_bytes=len(whole),private_marker_and_password_absent=True,product_cleanup=False,F02=False,global_completion=False,run=run,source=head,source_manifest=before['sha256'],raw_append_sha256=hashlib.sha256(whole).hexdigest()))
  result=dict(driver=driver,passed=passed,failed_stage=failed,closure=closure,output=str(root),product_cleanup=False,F02=False);save('terminal.json',result);print(json.dumps(result))
 return 0 if passed else 1

if __name__=='__main__':sys.exit(main())
