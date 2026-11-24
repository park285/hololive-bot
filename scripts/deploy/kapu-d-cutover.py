#!/usr/bin/env python3
import argparse
import contextlib
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import socket
import stat
import subprocess
import sys
import tempfile
import time


RELEASE_ROOT = Path(__file__).resolve().parents[2]
SLOTS_FILE = RELEASE_ROOT / 'scripts/deploy/lib/collector-slots.tsv'
SLOT_HEADER, *SLOT_ROWS = (line.split('\t') for line in SLOTS_FILE.read_text().splitlines())
SLOT = next(dict(zip(SLOT_HEADER, row)) for row in SLOT_ROWS if row[0] == 'd' and len(row) == len(SLOT_HEADER))
SERVICE = SLOT['service']
C = SLOT['collector_unit'].removesuffix('.service')
I = SLOT['issuer_unit'].removesuffix('.service')
NAMES = (C, I)
TITLE = {C: SLOT['collector_image'].rsplit('/', 1)[1], I: SLOT['issuer_image'].rsplit('/', 1)[1]}
NETWORK_UNIT, NETWORK = SLOT['network_unit'], SLOT['network']
PORT = SLOT['port']
UNIT = Path('/etc/containers/systemd')
SYSTEM = Path('/etc/systemd/system')
GEN = Path('/run/systemd/generator')
QUADLET = Path('/usr/lib/systemd/system-generators/podman-system-generator')
INPUT = RELEASE_ROOT / SLOT['unit_dir']
GUARD = Path(__file__).resolve().with_name('lib') / 'systemd-stop-guard.py'
CANDIDATE_PORT = '5433'
QUIESCED = b'kapu-d-guarded-stop-v5 already-direct\n'
STORE = Path('/var/lib') / (C + '-cutover')
D = Path('/etc/hololive-bot') / SERVICE / 'worker-profile-release'
SECRETS = Path('/etc/stack-secrets/hololive-bot') / SERVICE
SRC = SECRETS / 'worker-profiles'
CA = SECRETS / 'certs/postgres-ca.pem'
ENVFILE = SECRETS / 'collector.env'
DEST = '/run/hololive-bot/worker-profile-release'
SOURCE = '/run/hololive-bot/worker-profiles'
CERT = '/run/hololive-bot/certs/postgres-ca.pem'
SOCK = '/run/hololive-youtube-po/worker.sock'
DB = {'POSTGRES_HOST': '100.100.1.8', 'POSTGRES_DB': 'hololive', 'POSTGRES_USER': 'hololive_collector_d',
      'POSTGRES_SSLMODE': 'verify-full', 'POSTGRES_SSLROOTCERT': CERT, 'POSTGRES_SOCKET_PATH': '',
      'POSTGRES_QUERY_EXEC_MODE': 'cache_statement', 'PGAPPNAME': SERVICE,
      'COLLECTOR_INSTANCE_ID': SERVICE}
PROFILE_ENV = {'STACK_WORKER_PROFILE_FILE': DEST + '/profile.json',
               'STACK_WORKER_PROFILE_SOURCE_FILE': SOURCE + '/' + SERVICE + '.json'}
HEX = r'[0-9a-f]{64}'
LEVEL = r's0:c[0-9]+(?:,c[0-9]+)*'
COMMAND_ENV = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'LC_ALL': 'C', 'HOME': '/root'}


class Refused(ValueError):
    pass


def need(ok, reason):
    if not ok:
        raise Refused(reason)


def run(*args, timeout=35, status=False, env=None, clean=False):
    proc = subprocess.run(args, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE if clean else subprocess.DEVNULL,
                          timeout=timeout, env={**COMMAND_ENV, **(env or {})})
    if status:
        return proc.returncode
    need(not proc.returncode and not (clean and proc.stderr.strip()), f'{args[0]} {args[1]} 실패 ({proc.returncode})')
    return proc.stdout.decode().strip()


def inspect(kind, name, fmt):
    return run('podman', kind, 'inspect', '--format', fmt, name)


def prop(name, key):
    return run('systemctl', 'show', '--property=' + key, '--value', name + '.service')


def unit(name):
    return name + '.container'


def mask_anchor(h, name):
    return SYSTEM / ('.' + name + '.' + h.name + '.mask')


def mask(name):
    return SYSTEM / (name + '.service')


def prod(name):
    return 'docker.io/library/' + TITLE[name] + ':prod'


def oldtag(name, hold):
    return 'localhost/' + name + ':runtime-before-' + hold.name


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def number(path):
    st = path.lstat()
    return [st.st_dev, st.st_ino]


def trusted(path, directory=False):
    st = path.lstat()
    need((stat.S_ISDIR if directory else stat.S_ISREG)(st.st_mode) and st.st_uid == 0 and not st.st_mode & 0o022,
         'root 소유 비쓰기 파일/디렉터리 필요: ' + str(path))
    for parent in path.parents:
        st = parent.lstat()
        need(stat.S_ISDIR(st.st_mode) and st.st_uid == 0 and not st.st_mode & 0o022, '불명 경로: ' + str(parent))


def raw(path):
    trusted(path)
    need(0 < path.stat().st_size <= 262144, '지원하지 않는 파일 크기')
    return path.read_bytes()


def sync_dir(path):
    fd = os.open(path, os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def record(path, value):
    with path.open('xb') as out:
        out.write(value)
        out.flush()
        os.fsync(out.fileno())
    sync_dir(path.parent)


def metadata(path, value):
    record(path, json.dumps(value, sort_keys=True).encode() + b'\n')


@contextlib.contextmanager
def defer_interrupts():
    previous = signal.pthread_sigmask(signal.SIG_BLOCK, (signal.SIGINT,signal.SIGTERM,signal.SIGHUP))
    try:
        yield
    finally:
        signal.pthread_sigmask(signal.SIG_SETMASK, previous)


def owned_anchor(h, name):
    anchor = mask_anchor(h,name)
    st = anchor.lstat()
    identity = [st.st_dev, st.st_ino]
    need(stat.S_ISLNK(st.st_mode) and st.st_uid == os.geteuid() and os.readlink(anchor) == '/dev/null' and
         identity == json.loads(raw(h / (name + '.mask'))), '소유 mask anchor 변경: ' + name)
    return anchor, identity


def guard(action, h, name):
    anchor, (dev, ino) = owned_anchor(h,name)
    return run('python3','-I','-B',str(GUARD),action,name + '.service',str(anchor),f'{dev}:{ino}')


def absent(kind, name):
    need(run('podman', kind, 'exists', name, status=True) == 1, kind + ' 부재 미확인: ' + name)


def image(name, ref):
    result = inspect('image', ref, '{{.Id}} {{.Os}}/{{.Architecture}} '
                     '{{index .Config.Labels "org.opencontainers.image.revision"}} '
                     '{{index .Config.Labels "org.opencontainers.image.title"}}').split()
    need(len(result) == 4 and re.fullmatch(HEX, result[0]) and result[1] == 'linux/amd64' and
         re.fullmatch(r'[0-9a-f]{40}', result[2]) and result[3] == TITLE[name], '이미지 identity 불일치: ' + name)
    return {'id': 'sha256:' + result[0], 'rev': result[2]}


def generation(name):
    inv = prop(name, 'InvocationID')
    need(prop(name, 'ActiveState') == 'active' and re.fullmatch(r'[0-9a-f]{32}', inv), 'active InvocationID 없음')
    parts = inspect('container', name, '{{.Id}} {{.Image}} {{.State.StartedAt.UnixNano}} {{.State.Pid}} '
                    '{{.State.Status}} {{.ConmonPidFile}}').split()
    main = prop(name,'MainPID')
    need(len(parts) == 6 and all(re.fullmatch(HEX, s) for s in parts[:2]) and
         all(re.fullmatch('[1-9][0-9]*', s) for s in parts[2:4]) and parts[4] == 'running' and
         re.fullmatch('[1-9][0-9]*', main) and Path(parts[5]).read_text().strip() == main, 'systemd/Podman 실행 세대 불일치')
    group = prop(name,'ControlGroup')
    need(group.startswith('/') and group.endswith('/' + name + '.service'), '원래 unit cgroup 미확인')
    for pid in (main, parts[3]):
        groups = [line[3:] for line in (Path('/proc') / pid / 'cgroup').read_text().splitlines() if line.startswith('0::')]
        need(len(groups) == 1 and (groups[0] == group or groups[0].startswith(group + '/')), 'unit 외부 PID')
    g = dict(zip(('cid', 'image', 'started', 'pid'), parts[:4]))
    g.update(main=main, cgroup=group)
    g['inv'] = inv
    need(inspect('container', g['cid'], '{{.HostConfig.RestartPolicy.Name}}') in ('', 'no'), '독립 Podman restart 정책')
    need(prop(name,'InvocationID') == inv, '조회 중 실행 세대 교체: ' + name)
    return g


def effective_ids(pid, uid, gid, extra):
    proc = Path('/proc') / pid
    def mapped(kind, value):
        for line in (proc / (kind + '_map')).read_text().splitlines():
            a, b, n = map(int, line.split())
            if a <= value < a + n:
                return b + value - a
        raise ValueError('user namespace 매핑 불일치')
    fields = dict(line.split(':', 1) for line in (proc / 'status').read_text().splitlines() if ':' in line)
    need(set(map(int, fields['Uid'].split())) == {mapped('uid', uid)} and
         set(map(int, fields['Gid'].split())) == {mapped('gid', gid)}, '실행 UID/GID 불일치')
    if extra:
        need(mapped('gid', extra) == extra and extra in map(int, fields['Groups'].split()), 'app 보조 GID 불일치')


def profile():
    trusted(D, True)
    need({p.name for p in D.iterdir()} <= {'profile.json','SHA256SUMS','pair.json','compose.yml'}, '불명 d release 파일')
    source = raw(SRC / (SERVICE + '.json'))
    released = raw(D / 'profile.json')
    need(released == source and isinstance(json.loads(released), dict), 'release/source profile drift')
    sums = {}
    for line in raw(D / 'SHA256SUMS').decode().splitlines():
        match = re.fullmatch('(' + HEX + r') [ *](profile\.json|pair\.json)', line)
        need(match and match[2] not in sums, '불명 release checksum')
        sums[match[2]] = match[1]
    expected = {'profile.json', 'pair.json'} if os.path.lexists(D/'pair.json') else {'profile.json'}
    need(set(sums) == expected and all(sha(raw(D / key)) == digest for key, digest in sums.items()), 'release checksum 불일치')
    return released


def policy(name):
    base = UNIT / unit(name)
    trusted(base)
    dropdir = UNIT / (unit(name) + '.d')
    drops = {}
    names = {'container.d', unit(name) + '.d', *(name[:i+1] + '.container.d' for i,c in enumerate(name) if c == '-')}
    def error(exc):
        raise exc
    for root in (UNIT, Path('/run/containers/systemd'), Path('/usr/share/containers/systemd')):
        if not root.exists():
            continue
        trusted(root, True)
        for folder, subs, files in os.walk(root, onerror=error):
            p = Path(folder)
            if p == root and 'users' in subs:
                subs.remove('users')
            need(not any((p / s).is_symlink() for s in subs), '불명 Quadlet 경로')
            if p.name.endswith('.d'):
                subs.clear()
                if p.name not in names:
                    continue
                for f in sorted(k for k in files if k.endswith('.conf')):
                    need(p == dropdir and name == C and f in ('50-mcs-level.conf', '90-resolved-dns.conf'), '불명 Quadlet drop-in')
                    value = raw(p / f)
                    lines = [s.strip() for s in value.decode().splitlines() if s.strip() and not s.lstrip().startswith(('#',';'))]
                    if f == '50-mcs-level.conf':
                        need(len(lines) == 2 and lines[0] == '[Container]' and re.fullmatch('SecurityLabelLevel=' + LEVEL, lines[1]), 'MCS override 불일치')
                    else:
                        need(lines == ['[Container]', 'DNS=', 'DNS=127.0.0.53'], 'DNS override 불일치')
                    drops[f] = value
            else:
                need(unit(name) not in files or p == UNIT, '중복 Quadlet source')
    return drops


def config(name):
    candidate = raw(INPUT / unit(name))
    if name == C:
        need(candidate.count(f'Environment=POSTGRES_PORT={CANDIDATE_PORT}\n'.encode()) == 1, '후보 port 선언 불일치')
    need(raw(UNIT / unit(name)) == candidate, '검토한 기본 unit과 설치본이 다름: ' + name)
    return candidate, policy(name)


def container_settings(text, section):
    settings = {}
    group = ''
    for line in text.splitlines():
        line = line.strip()
        if line.startswith('['):
            group = line
        elif group == section and line and not line.startswith(('#',';')):
            key, sep, value = line.partition('=')
            need(sep and not line.endswith('\\'), '지원하지 않는 Quadlet 문법')
            if not value:
                settings[key] = []
            else:
                settings.setdefault(key, []).append(value)
    return settings


def origin(name, masked, guarded=False, allow_job=False):
    generated(name)
    need(prop(name, 'FragmentPath') == str(mask(name) if masked else GEN / (name + '.service')) and
         prop(name, 'NeedDaemonReload') == 'no', '생성된 unit 원천 불일치')
    drops = str(SYSTEM / (name + '.service.d/90-hololive-stop-guard.conf')) if guarded else ''
    need(prop(name, 'DropInPaths') == drops and (allow_job or not prop(name, 'Job')) and
         (masked or prop(name, 'Type') == 'notify'), '불명 systemd drop-in/job/type')


def quadlet_lines(text, image):
    lines = text.splitlines()
    for i, line in enumerate(lines):
        if line.startswith('ExecStart='):
            words = line.split(' ')
            need(words.count(image) == 1, '생성된 Quadlet과 설치본 불일치')
            cut = words.index(image)
            lines[i] = (sorted(words[:cut]), words[cut:])
    return lines


def generated(name):
    source = raw(UNIT / unit(name))
    drops = policy(name)
    expected = container_settings(source.decode() + ''.join('\n' + v.decode() for _, v in sorted(drops.items())), '[Container]')
    need(expected.get('Image') == [prod(name)] and expected.get('Pull') == ['never'] and
         expected.get('ContainerName') == [name], '생성된 Quadlet과 설치본 불일치')
    with tempfile.TemporaryDirectory() as tmp:
        src, out = Path(tmp) / 'src', Path(tmp) / 'out'
        (src / (unit(name) + '.d')).mkdir(parents=True)
        out.mkdir()
        (src / unit(name)).write_bytes(source)
        for f, value in drops.items():
            (src / (unit(name) + '.d') / f).write_bytes(value)
        for ref in expected.get('Network', []) + [v.partition(':')[0] for v in expected.get('Volume', [])]:
            if ref.endswith(('.network', '.volume')) and Path(ref).name == ref:
                (src / ref).write_bytes(raw(INPUT / ref))
        run(str(QUADLET), '-no-kmsg-log', str(out), env={'QUADLET_UNIT_DIRS': str(src)})
        made = (out / (name + '.service')).read_text()
    made = made.replace('\nSourcePath=' + str(src / unit(name)) + '\n', '\nSourcePath=' + str(UNIT / unit(name)) + '\n')
    need(quadlet_lines(raw(GEN / (name + '.service')).decode(), prod(name)) == quadlet_lines(made, prod(name)),
         '생성된 Quadlet과 설치본 불일치')
    if name == C:
        settings = expected['Environment']
        port_setting = [v for v in settings if v.startswith('POSTGRES_PORT=')]
        need(port_setting == ['POSTGRES_PORT=' + CANDIDATE_PORT] and
             all([v for v in settings if v.startswith(k + '=')] == [k + '=' + value]
                 for k,value in {**DB, **PROFILE_ENV}.items()) and
             expected['EnvironmentFile'] == [str(ENVFILE)] and expected['User'] == ['1000:1000'] and
             expected['GroupAdd'] == ['1002'] and expected['UserNS'] == ['auto:size=65536,gidmapping=1002:1002:1'] and
             expected['Network'] == [NETWORK_UNIT] and expected['IP'] == ['10.89.15.14'] and
             str(CA) + ':' + CERT + ':ro' in expected['Volume'], '후보 probe/서비스 계약 불일치')


def prereqs():
    for obj, kind, service in [(NETWORK, 'network', NETWORK + '-network'),
                              (I + '-socket', 'volume', I + '-volume')]:
        need(prop(service, 'ActiveState') == 'active' and run('podman', kind, 'exists', obj, status=True) == 0,
             '기존 인프라가 준비되지 않음: ' + obj)
    for key in ('QUADLET_UNIT_DIRS', 'SYSTEMD_UNIT_PATH'):
        need(not os.environ.get(key), '비표준 unit 검색 경로')
    need(run('systemctl','is-active','network-online.target') == 'active', '기존 network-online.target 비활성')
    trusted(ENVFILE)
    trusted(CA)


def collector(g, level):
    cid, pid = g['cid'], g['pid']
    effective_ids(pid, 1000, 1000, 1002)
    need(inspect('container', cid, '{{.Config.User}} {{.ProcessLabel}}') == '1000:1000 system_u:system_r:container_t:' + level,
         'collector user/MCS 불일치')
    net = json.loads(inspect('container', cid, '{{json .NetworkSettings.Networks}}'))
    need(set(net) == {NETWORK} and net[NETWORK]['IPAddress'] == '10.89.15.14', 'collector network 불일치')
    expected = {**DB, **PROFILE_ENV, 'POSTGRES_PORT': CANDIDATE_PORT}
    checks = []
    for key, value in expected.items():
        prefix = key + '='
        checks.append('{{if eq (printf "%.' + str(len(prefix)) + 's" .) ' + json.dumps(prefix) +
                      '}}{{if eq . ' + json.dumps(prefix + value) + '}}' + key + ' {{else}}mismatch {{end}}{{end}}')
    seen = inspect('container', cid, '{{range .Config.Env}}' + ''.join(checks) + '{{end}}').split()
    need(sorted(seen) == sorted(expected), 'collector 실행 환경 불일치/중복')
    mounts = json.loads(inspect('container', cid, '{{json .Mounts}}'))
    root = Path('/proc') / pid / 'root'
    mountinfo = [line.split() for line in (root.parent / 'mountinfo').read_text().splitlines()]
    for dest, host in ((DEST,D), (SOURCE,SRC), (CERT,CA)):
        selected = [m for m in mounts if m['Destination'] == dest]
        need(len(selected) == 1 and selected[0]['Type'] == 'bind' and not selected[0]['RW'] and
             selected[0]['Source'] == str(host) and (root / dest[1:]).samefile(host), '실행 bind 불일치: ' + dest)
        flags = [parts[5].split(',') for parts in mountinfo if parts[4] == dest]
        need(len(flags) == 1 and 'ro' in flags[0] and not any(parts[4].startswith(dest + '/') for parts in mountinfo),
             '중첩/쓰기 가능 mount')
    payload = run('podman', 'exec', cid, '/app/bin/healthcheck', '--body-api-key-env', 'METRICS_API_KEY',
                  'http://127.0.0.1:30096/diagnostics/workers')
    diagnostics = json.loads(payload)
    data = profile()
    need(diagnostics['contractVersion'] == 1 and diagnostics['profile']['hash'] == sha(data), 'loaded profile hash 불일치')
    return data


def issuer(g):
    effective_ids(g['pid'], 65532, 1000, None)
    mounts = json.loads(inspect('container', g['cid'], '{{json .Mounts}}'))
    selected = [m for m in mounts if m['Destination'] == str(Path(SOCK).parent)]
    need(len(selected) == 1 and selected[0]['Type'] == 'volume' and selected[0]['Name'] == I + '-socket',
         'issuer socket volume 불일치')
    run('podman', 'exec', g['cid'], '/app/bin/po-broker', '--healthcheck', '--socket', SOCK)


def healthy(name, g, wait=False):
    cid = g['cid']
    if wait:
        run('podman', 'wait', '--condition=healthy', '--condition=unhealthy', '--condition=exited', cid,
            timeout=93 if name == I else 243)
    need(inspect('container', cid, '{{.State.Health.Status}}') == 'healthy' and generation(name) == g,
         '비정상/교체된 컨테이너: ' + name)


def journal_since():
    seconds, micros = divmod(time.time_ns() // 1000, 1_000_000)
    return f'@{seconds}.{micros:06}'


def stop_invocation(name, invocation, since):
    options = ('--job-mode=ignore-dependencies',) if name == I else ()
    run('systemctl', 'stop', *options, name + '.service', timeout=125)
    evidence = run('journalctl', '--boot', '--quiet', '--no-pager', '-n', '1', '-o', 'json',
                   '--output-fields=INVOCATION_ID', '--since=' + since,
                   '_PID=1', 'UNIT=' + name + '.service', 'INVOCATION_ID=' + invocation,
                   'JOB_TYPE=stop', 'MESSAGE_ID=de5b426a63be47a7b6ac3eaac82e2f6f')
    need(evidence and json.loads(evidence).get('INVOCATION_ID') == invocation, '이번 manager stop 미확인: ' + name)


def terminal(name):
    keys = ('ActiveState','InvocationID','Result','ExecMainCode','ExecMainStatus','MainPID','ControlPID','Job')
    text = run('systemctl','show','--all','--property=' + ','.join(keys),name + '.service')
    result = dict(line.split('=',1) for line in text.splitlines())
    need(set(result) == set(keys), 'manager 종료 상태 필드 미확인: ' + name)
    return result


def issuer_exit(name, result, inv):
    return (name == I and re.fullmatch('[0-9a-f]{32}', inv) and result['InvocationID'] == inv and
            result['ActiveState'] == 'failed' and result['Result'] == 'exit-code' and
            result['ExecMainCode'] == '1' and result['ExecMainStatus'] == '143')


def successful_exit(name, inv, result, code):
    return ((code == '0' and result['ActiveState'] == 'inactive' and result['Result'] == 'success' and
             result['ExecMainCode'] in ('0','1') and result['ExecMainStatus'] == '0' and result['InvocationID'] in ('',inv)) or
            (code == '143' and issuer_exit(name,result,inv)))


def no_process(name, result, cid):
    need(result['ActiveState'] in ('inactive','failed') and result['MainPID'] == result['ControlPID'] == '0' and not result['Job'],
         '종료/PID 부재 미확인: ' + name)
    absent('container', name)
    absent('container', cid)


def guarded_stop(h, name, g, candidate=False):
    guard('check',h,name)
    origin(name,False,True)
    need(generation(name) == g, '정지 직전 세대 변경')
    event = False
    code = None
    since = journal_since()
    try:
        stop_invocation(name, g['inv'], since)
        event = True
        code = run('python3','-I','-B',str(GUARD),'exit-status',name + '.service',g['cid'],since)
    finally:
        result = terminal(name)
        marker = name + ('.candidate-stop' if candidate else '.stopped')
        metadata(h / (marker + '.json'), {'invocation':g['inv'], 'cid':g['cid'], 'since':since,
                                          'manager_stop':event, 'container_exit':code, **result})
    no_process(name,result,g['cid'])
    need(successful_exit(name,g['inv'],result,code), '원래 invocation의 정상 종료 미확인: ' + name)
    record(h / marker, g['inv'].encode())


def require_previous_stop(h, name, g):
    marker = h / (name + '.stopped')
    result = json.loads(raw(h / (name + '.stopped.json')))
    need(result['manager_stop'] is True and re.fullmatch(r'@[0-9]+\.[0-9]{6}',result['since']) and
         raw(marker).decode() == result['invocation'] == g['inv'] and result['cid'] == g['cid'] and
         result['MainPID'] == result['ControlPID'] == '0' and not result['Job'] and
         successful_exit(name,g['inv'],result,result['container_exit']), '원래 실행 세대의 정지 영수증 불일치: ' + name)


def held_one(h, state, name, stopped=None):
    p = mask(name)
    _, identity = owned_anchor(h,name)
    need(p.is_symlink() and os.readlink(p) == '/dev/null' and number(p) == identity and
         prop(name, 'LoadState') == 'masked', '소유 mask 불일치: ' + name)
    result = terminal(name)
    no_process(name,result,state['old'][name]['cid'])
    if result['ActiveState'] != 'inactive':
        inv = raw(h / (name + '.stopped')).decode() if stopped is None else stopped
        need(issuer_exit(name,result,inv), '동일 issuer stop/143 증거 없음')


def bindings():
    result = {}
    for path in (ENVFILE, CA, SRC / (SERVICE + '.json'), D/'profile.json'):
        st = path.lstat()
        result[str(path)] = [st.st_dev,st.st_ino,st.st_uid,st.st_gid,st.st_mode,st.st_size,st.st_mtime_ns]
    return result


def previous_endpoint():
    return {'host':DB['POSTGRES_HOST'], 'port':int(CANDIDATE_PORT), 'role':DB['POSTGRES_USER'],
            'collector_user':'1000:1000', 'collector_group':1002, 'issuer_user':'65532:1000'}


def retained(h, state):
    need(state['previous_endpoint'] == previous_endpoint(), '원래 endpoint 영수증 불일치')
    prereqs()
    need(state['hold'] == number(h) and state['boot'] == Path('/proc/sys/kernel/random/boot_id').read_text().strip(), '다른 hold/boot')
    need(state['bindings'] == bindings(), '보존 bind/envfile 파일 교체')
    data = profile()
    need(raw(h / 'profile.json') == data and sha(data) == state['profile'], '보존 profile drift')
    for n in NAMES:
        base, drops = config(n)
        old = raw(h / unit(n))
        need(old == base and state['base'][n] == sha(old) and state['drops'][n] == {k:sha(v) for k,v in drops.items()} and
             all(raw(h / (n + '.' + k)) == v for k,v in drops.items()), '보존 unit/drop-in drift')
        need(image(n, oldtag(n,h)) == state['images'][n], '보존 이미지 tag drift')


def release_candidate(revision, ids, digests):
    need(re.fullmatch('[0-9a-f]{40}', revision) and len(set(ids.values())) == 2 and
         all(re.fullmatch('sha256:' + HEX, ids[n]) and re.fullmatch(HEX, digests[n]) for n in NAMES), '후보 인수 불일치')
    return {'revision':revision, 'images':ids, 'units':digests}


def verify_candidate_input(n, data, target):
    ref = target['images'][n]
    need(sha(data) == target['units'][n] and image(n,ref) == {'id':ref,'rev':target['revision']},
         '후보 unit digest 또는 이미지 revision 불일치: ' + n)


def snapshot(h, target):
    prereqs()
    state = {'boot': Path('/proc/sys/kernel/random/boot_id').read_text().strip(), 'old':{}, 'images':{}, 'drops':{}, 'base':{}}
    files = {}
    for n in NAMES:
        need(not os.path.lexists(mask(n)) and prop(n,'LoadState') == 'loaded', '기존 mask 또는 service override')
        origin(n, False)
        need(not service_dropins(n), '정지 전 systemd drop-in 또는 의존성 링크: ' + n)
        base, drops = config(n)
        files[unit(n)] = base
        files.update({n + '.' + k:v for k,v in drops.items()})
        state['base'][n] = sha(base)
        state['drops'][n] = {k:sha(v) for k,v in drops.items()}
        g = state['old'][n] = generation(n)
        ident = state['images'][n] = image(n, prod(n))
        need('sha256:' + g['image'] == ident['id'], '실행 이미지와 실제 :prod tag 불일치')
        healthy(n, g)
        if n == C:
            mcs = drops.get('50-mcs-level.conf', b'').decode()
            match = re.search('^SecurityLabelLevel=(' + LEVEL + ')$', mcs, re.M)
            need(match, '고정된 collector MCS 없음')
            state['level'] = match[1]
            files['profile.json'] = collector(g, match[1])
            state['profile'] = sha(files['profile.json'])
        else:
            issuer(g)
        absent('image', oldtag(n,h))
    for n in NAMES:
        old = state['images'][n]
        need(target['revision'] != old['rev'] and target['images'][n] != old['id'], '이전 실행 이미지 또는 revision 재사용: ' + n)
        verify_candidate_input(n,raw(INPUT / unit(n)),target)
    state['candidate'] = target
    state['bindings'] = bindings()
    state['previous_endpoint'] = previous_endpoint()
    h.mkdir(mode=0o700)
    sync_dir(h.parent)
    state['hold'] = number(h)
    for name,data in files.items():
        record(h / name, data)
    metadata(h / 'previous.json', state)
    for n in NAMES:
        absent('image', oldtag(n,h))
        run('podman','tag',state['images'][n]['id'],oldtag(n,h))
    retained(h,state)
    return state


def stop_receipt(state):
    return QUIESCED + sha(json.dumps(state,sort_keys=True).encode()).encode() + b'\n'


def quiesce(h, target):
    need(target is not None, '같은 endpoint 교체에는 별도 후보 identity 필요')
    need(not os.path.lexists(h), '새 hold 필요')
    trusted(GUARD)
    trusted(SLOTS_FILE)
    s = snapshot(h,target)
    for n in NAMES:
        need(generation(n) == s['old'][n], '정지 전 세대 교체')
    for n in NAMES:
        anchor = mask_anchor(h,n)
        os.symlink('/dev/null', anchor)
        sync_dir(SYSTEM)
        metadata(h / (n + '.mask'), number(anchor))
        guard('arm',h,n)
    for n in NAMES:
        guard('check',h,n)
        origin(n,False,True)
        need(set(service_dropins(n)) <= {str(SYSTEM / (n + '.service.d/90-hololive-stop-guard.conf'))},
             '정지 전 systemd drop-in 또는 의존성 링크: ' + n)
    for n in (I,C):
        g = s['old'][n]
        retained(h,s)
        need(image(n,prod(n)) == s['images'][n] and generation(n) == g, '정지 전 실행 세대 변경')
        if n == C:
            need(collector(g,s['level']) == raw(h / 'profile.json'), '정지 전 profile 불일치')
        else:
            issuer(g)
        guarded_stop(h,n,g)
    for n in (I,C):
        guard('seal',h,n)
    for n in NAMES:
        held_one(h,s,n)
    record(h / 'quiesced', stop_receipt(s))
    print('quiesced:', h)


def install_unit(name, data):
    with tempfile.NamedTemporaryFile(dir=UNIT, prefix='.' + name + '-', delete=False) as f:
        tmp = Path(f.name)
        try:
            f.write(data)
            f.flush()
            os.fchmod(f.fileno(), 0o644)
            os.fsync(f.fileno())
            os.replace(tmp, UNIT / unit(name))
        finally:
            tmp.unlink(missing_ok=True)


def gate(image_id, level):
    common = ['podman','run','--rm','--pull=never','--log-driver=none','--read-only','--read-only-tmpfs=false',
              '--no-healthcheck','--image-volume=ignore','--cap-drop=ALL','--security-opt=no-new-privileges',
              '--security-opt=label=level:' + level,'--user=1000:1000','--group-add=1002',
              '--userns=auto:size=65536,gidmapping=1002:1002:1','--pids-limit=32']
    runtime = container_settings(raw(INPUT / unit(C)).decode(), '[Container]')
    envfile = '--env-file=' + str(ENVFILE)
    ca = '--volume=' + str(CA) + ':' + CERT + ':ro'
    run(*common, '--name=' + C + '-profile-check', '--network=none', envfile, ca,
        '--volume=' + str(D/'profile.json') + ':' + PROFILE_ENV['STACK_WORKER_PROFILE_FILE'] + ':ro',
        *('--env=' + item for item in runtime['Environment']), image_id, '--check-worker-profile', timeout=65)
    args = [*common,'--name=' + C + '-database-check','--network=' + NETWORK,'--ip=10.89.15.14',envfile,ca]
    args.extend('--env=' + k + '=' + v for k,v in {**DB,'POSTGRES_PORT':CANDIDATE_PORT}.items())
    run(*args, image_id, '--check-database-endpoint', timeout=65)


def start_once(n, requested):
    unit_name = n + '.service'
    since = journal_since()
    with os.fdopen(os.memfd_create('cutover-start'), 'w+b') as log:
        requested[n] = (since, None)
        try:
            p = subprocess.run(['systemctl','start','--show-transaction',unit_name], stdin=subprocess.DEVNULL,
                               stdout=log, stderr=subprocess.STDOUT, timeout=245,
                               env={**COMMAND_ENV,'SYSTEMD_LOG_TARGET':'console','SYSTEMD_LOG_LEVEL':'info'})
        finally:
            log.seek(0)
            pattern = rb'Enqueued anchor job ([1-9][0-9]*) ' + re.escape(unit_name.encode()) + rb'/start\.\n?'
            jobs = [m[1].decode() for line in log if (m := re.fullmatch(pattern,line))]
            if len(jobs) == 1:
                requested[n] = (since, jobs[0])
        need(p.returncode == 0 and requested[n][1], '원래 start job 미확인: ' + n)


def candidate(n, request, old, image_id, *, guarded_hold=None):
    since, job = request
    need(job, '요청한 start job 번호 미확인: ' + n)
    result = terminal(n)
    inv = None
    if guarded_hold is not None and result['Job'] == job:
        budget = guard('start-budget',guarded_hold,n)
        need(re.fullmatch('[1-9][0-9]*',budget), 'manager 기동 제한 미확인: ' + n)
        limit = time.monotonic() + int(budget)
        while result['Job'] == job:
            current = result['InvocationID']
            if inv is not None or (result['ActiveState'] == 'activating' and current):
                need(re.fullmatch('[0-9a-f]{32}',current) and current != old['inv'] and inv in (None,current),
                     '진행 중인 start invocation 변경: ' + n)
                inv = current
            need(time.monotonic() < limit, '원래 start job 완료 미확인: ' + n)
            time.sleep(0.25)
            result = terminal(n)
    need(result['ActiveState'] == 'active' and not result['Job'], '원래 후보 기동 완료 미확인: ' + n)
    if inv is None:
        evidence = run('journalctl','--boot','--quiet','--no-pager','-o','json','--output-fields=INVOCATION_ID',
                       '--since=' + since,'_PID=1','UNIT=' + n + '.service','JOB_ID=' + job,'JOB_TYPE=start',
                       'MESSAGE_ID=39f53479d3a045ac8e11786248231fbf')
        rows = evidence.splitlines()
        need(len(rows) == 1, '원래 start job 완료 기록 미확인: ' + n)
        inv = json.loads(rows[0]).get('INVOCATION_ID')
    need(inv and inv == result['InvocationID'], '원래 start job의 실행 세대 미확인: ' + n)
    g = generation(n)
    need(g['inv'] == result['InvocationID'] and g['inv'] != old['inv'] and g['cid'] != old['cid'] and
         'sha256:' + g['image'] == image_id, '새 실행 세대 불일치: ' + n)
    return g


def acceptance(h, s, n, g):
    healthy(n,g,wait=True)
    if n == I:
        issuer(g)
    else:
        need(collector(g,s['level']) == raw(h / 'profile.json'), '후보 loaded profile drift')
        result = json.loads(run('podman','exec',g['cid'],'/app/bin/healthcheck','--body','https://127.0.0.1:' + PORT + '/ready'))
        need(result['status'] == 'ready' and result['instance_id'] == SERVICE and result['helper'] == 'ok' and
             result['first_success'] is True and result['handoff_status'] == 'PROCESSED' and result['handoff_processed'] is True and
             prop(C,'NRestarts') == '0', 'collector handoff/NRestarts 불일치')
    healthy(n,g)


def rehold(h, s, ids, opened, requested, started, receipt_failed):
    errors = (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError)
    damaged = False
    before, armed, stops = {}, set(), {}
    for n in opened:
        try:
            before[n] = terminal(n)
        except errors:
            damaged = True
            continue
        try:
            metadata(h/(n+'.before-rehold.json'), before[n])
        except errors:
            receipt_failed = True
    for n in (I,C):
        if n not in opened:
            try:
                held_one(h,s,n)
            except errors:
                damaged = True
            continue
        p = mask(n)
        if os.path.lexists(p):
            damaged = True
            continue
        uncertain = False
        if n not in requested:
            try:
                if prop(n,'LoadState') == 'masked':
                    result = terminal(n)
                    need(n in before and before[n] == result and
                         (result['ActiveState'] == 'inactive' or issuer_exit(n,result,s['old'][n]['inv'])) and
                         prop(n,'FragmentPath') == str(p), '원 mask 유지 미확인')
                    require_previous_stop(h,n,s['old'][n])
                    no_process(n,result,s['old'][n]['cid'])
                    guard('undo-unmask',h,n)
                    held_one(h,s,n)
                    origin(n,True)
                    continue
            except errors:
                damaged = uncertain = True
        if os.path.lexists(p):
            continue
        try:
            guard('arm',h,n)
            origin(n,False,True,allow_job=True)
            if not uncertain:
                armed.add(n)
        except errors:
            damaged = True
    for n in (I,C):
        if n not in armed:
            continue
        try:
            if n in requested:
                if n not in started:
                    started[n] = candidate(n,requested[n],s['old'][n],ids[n],guarded_hold=h)
                guarded_stop(h,n,started[n],candidate=True)
                stops[n] = started[n]['inv']
            else:
                guard('check',h,n)
                origin(n,False,True)
                result = terminal(n)
                need(n in before and before[n]['ActiveState'] == result['ActiveState'] == 'inactive', '후보 종료 미확인')
                no_process(n,result,s['old'][n]['cid'])
                stops[n] = ''
        except OSError:
            receipt_failed = True
        except errors:
            damaged = True
    for n in (I,C):
        try:
            if n in armed:
                need(not receipt_failed and n in stops, '미확인 종료/영수증: 보호 가드 유지')
                guard('seal',h,n)
            held_one(h,s,n,stops.get(n))
            if n in started:
                absent('container',started[n]['cid'])
        except errors:
            damaged = True
    need(not damaged and not receipt_failed, '양쪽 소유 mask 복원 미확인: 증거와 잔여 가드 유지')


def unit_paths():
    paths = [Path(p) for p in run('systemctl','show','--property=UnitPath','--value').split()]
    need(paths and all(p.is_absolute() for p in paths), '불명 systemd unit 경로')
    return paths


def foreign_units(name):
    paths = unit_paths()
    need(GEN in paths and SYSTEM in paths, '불명 systemd unit 경로')
    proc = subprocess.run(['systemd-analyze', 'unit-files'], stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                          stderr=subprocess.DEVNULL, timeout=60, cwd='/',
                          env={**COMMAND_ENV, 'LC_ALL': 'C',
                               'SYSTEMD_UNIT_PATH': ':'.join(map(str, [p for p in paths if p != SYSTEM] + [SYSTEM]))})
    need(proc.returncode == 0, 'systemd unit 이름 판정 불가: ' + name)
    picked = re.findall('^ids: ' + re.escape(name + '.service') + ' (?:→|->) (.*)$',
                        proc.stdout.decode(errors='replace'), re.M)
    return [] if picked == [str(GEN / (name + '.service'))] else picked or [name + '.service']


PULLS = ('Wants', 'Requires', 'Requisite', 'BindsTo', 'Upholds')


def service_dropins(name):
    top = SYSTEM.stat()
    owned = {(top.st_dev, top.st_ino, path.name) for n in NAMES for path in [mask(n), *SYSTEM.glob('.' + n + '.*.mask')]}
    with tempfile.TemporaryDirectory() as tmp:
        fragment = Path(tmp) / 'unit' / (name + '.service')
        fragment.parent.mkdir()
        fragment.write_bytes(raw(GEN / (name + '.service')))
        aliases, drops, masked, pulls, mounts = systemd_view(fragment, unit_paths())
        need(not aliases, 'systemd unit 별칭 있음: ' + name + ' ← ' + ', '.join(aliases))
        _, _, _, own, _ = systemd_view(fragment, [Path(tmp) / 'none'])
    return (drops + sorted(dep + ' ← ' + path for dep, path in masked if owned_link(path, owned)) + sorted(pulls - own) +
            sorted(dep for dep in mounts if not mounted(dep.partition(': ')[2])))


def owned_link(path, owned):
    for _ in range(64):
        parent = os.stat(os.path.dirname(path))
        if (parent.st_dev, parent.st_ino, os.path.basename(path)) in owned:
            return True
        if not os.path.islink(path):
            return False
        path = os.path.join(os.path.dirname(path), os.readlink(path))
    return True


def mounted(name):
    return name.endswith('.mount') and run('systemctl', 'show', '--property=ActiveState', '--value', '--', name) == 'active'


def systemd_view(fragment, paths):
    proc = subprocess.run(['systemd-analyze', 'verify', '--man=no', '--generators=no', str(fragment)],
                          stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60, cwd='/',
                          env={**COMMAND_ENV, 'SYSTEMD_UNIT_PATH': ':'.join(map(str, paths)),
                               'SYSTEMD_LOG_LEVEL': 'debug', 'SYSTEMD_LOG_TARGET': 'console', 'LC_ALL': 'C'})
    head = '-> Unit ' + fragment.name + ':\n'
    text = proc.stdout.decode(errors='replace')
    need(text.count(head) == 1, 'systemd unit 판정 없음: ' + fragment.name)
    lines = [line.strip() for line in text.split(head)[1].split('\n-> ')[0].splitlines()]
    need('Unit Load State: loaded' in lines, 'systemd unit 판정 불가: ' + fragment.name)
    drops = sorted(line.removeprefix('DropIn Path: ') for line in lines if line.startswith('DropIn Path: '))
    pulls = [(dep, origin.rstrip(')').split()) for dep, _, origin in (line.partition(' (') for line in lines)
             if dep.split(':')[0] in PULLS]
    aliases = sorted(line.removeprefix('Alias: ') for line in lines if line.startswith('Alias: '))
    masked = re.findall('^' + re.escape(fragment.name) + ': (\\w+ dependency on \\S+) is masked by (.*), ignoring\\.$',
                        proc.stderr.decode(errors='replace'), re.M)
    return aliases, drops, masked, {dep for dep, origins in pulls if 'origin-file' in origins}, {dep for dep, origins in pulls if 'origin-path' in origins}


def resumable(h, target):
    trusted(h,True)
    trusted(GUARD)
    trusted(SLOTS_FILE)
    need(not os.path.lexists(h/'resume-attempt.json'), '이 hold의 이전 재개 시도 기록 있음')
    s = json.loads(raw(h/'previous.json'))
    need(stat.S_IMODE(h.stat().st_mode) == 0o700 and raw(h/'quiesced') == stop_receipt(s),
         '선택한 모드의 새 quiesce 영수증 필요')
    need(s.get('candidate') == target, '이 hold에서 정지 전에 승인한 후보와 불일치')
    retained(h,s)
    candidates = {}
    for n in NAMES:
        require_previous_stop(h,n,s['old'][n])
        held_one(h,s,n)
        generated(n)
        need(not foreign_units(n), '정지 뒤 추가된 systemd unit 파일: ' + n)
        need(not service_dropins(n), '정지 뒤 추가된 systemd drop-in: ' + n)
        candidates[n] = raw(INPUT / unit(n))
        verify_candidate_input(n,candidates[n],target)
        need(image(n,prod(n)) == s['images'][n], '이전 :prod 불일치')
    return s, candidates


def check(h, target):
    resumable(h, target)
    print('d check: ' + h.name + ' 재개 가능')


def resume(h, target):
    s, candidates = resumable(h, target)
    revision, ids, digests = target['revision'], target['images'], target['units']
    metadata(h/'resume-attempt.json', {**target,'profile':s['profile']})
    for n in NAMES:
        if candidates[n] != raw(UNIT/unit(n)):
            install_unit(n, candidates[n])
        run('podman','tag', ids[n], prod(n))
    run('systemctl','daemon-reload')

    def inputs():
        retained(h,s)
        for name in NAMES:
            need(sha(raw(UNIT/unit(name))) == digests[name] and image(name,prod(name)) == {'id':ids[name],'rev':revision},
                 '실제 :prod 또는 설치 unit drift')
    inputs()
    for n in NAMES:
        held_one(h,s,n)
        origin(n,True)
    gate(ids[C],s['level'])
    record(h/'sql-passed',b'kapu-d-v1\n')
    opened = []
    requested = {}
    started = {}
    published = False
    receipt_failed = False
    try:
        for n in (I,C):
            inputs()
            held_one(h,s,n)
            if n == I:
                held_one(h,s,C)
            else:
                healthy(I,started[I]); issuer(started[I])
            p = mask(n)
            _, ident = owned_anchor(h,n)
            need(number(p) == ident, '해제 직전 mask 교체')
            with defer_interrupts():
                p.unlink()
                opened.append(n)
                sync_dir(SYSTEM)
            run('systemctl','daemon-reload')
            for m in opened:
                origin(m,False)
                need(not service_dropins(m), '재개 중 추가된 systemd drop-in 또는 의존성 링크: ' + m)
            need(prop(n,'LoadState') == 'loaded' and prop(n,'ActiveState') in ('inactive','failed'), '단독 기동 전 상태 불일치')
            if n == I:
                held_one(h,s,C)
            else:
                healthy(I,started[I])
            inputs()
            with defer_interrupts():
                start_once(n,requested)
                started[n] = candidate(n,requested[n],s['old'][n],ids[n])
            acceptance(h,s,n,started[n])
        inputs()
        healthy(I,started[I]); issuer(started[I]); healthy(C,started[C])
        need(prop(C,'NRestarts') == '0' and generation(I) == started[I], '최종 실행 세대 변경')
        pending = h/'.accepted-pending.json'
        accepted = h/'accepted.json'
        try:
            metadata(pending, {'generations':started, 'revision':revision, 'profile':s['profile'],
                               'host':DB['POSTGRES_HOST'], 'port':int(CANDIDATE_PORT), 'role':DB['POSTGRES_USER'],
                               'first_success':True, 'handoff':'PROCESSED', 'collector_restarts':0})
            with defer_interrupts():
                os.link(pending,accepted,follow_symlinks=False)
                try:
                    sync_dir(h)
                    pending.unlink()
                except OSError:
                    if os.path.samestat(pending.lstat(),accepted.lstat()):
                        accepted.unlink()
                        sync_dir(h)
                    raise
                published = True
        except OSError:
            receipt_failed = True
            raise
    except BaseException:
        if not published:
            with defer_interrupts():
                rehold(h,s,ids,opened,requested,started,receipt_failed)
            print('후보 거부: 재기동 없이 소유 mask 유지', file=sys.stderr)
        raise
    for n in opened:
        a = mask_anchor(h,n)
        try:
            need(number(a) == json.loads(raw(h/(n+'.mask'))), '외부 mask anchor')
            a.unlink()
        except (OSError, ValueError):
            print('수용 증거 유지: 잔여 anchor는 삭제하지 않음:', a, file=sys.stderr)
    print('accepted:', h)


def prepare_store(path, *, create):
    if create:
        trusted(path.parent, True)
        path.mkdir(mode=0o700, exist_ok=True)
    trusted(path, True)
    need(stat.S_IMODE(path.stat().st_mode) == 0o700, '전환 영수증 디렉터리는 root 전용 0700이어야 함')


def main():
    parser = argparse.ArgumentParser(description='kapu d 전용. 기존 설치·실행 증거가 없는 첫 설치와 자동 복구는 지원하지 않는다.')
    sub = parser.add_subparsers(dest='action', required=True)
    for cmd in ('quiesce','resume','check'):
        p = sub.add_parser(cmd, allow_abbrev=False)
        p.add_argument('hold', type=Path)
        for arg in ('revision','collector_id','issuer_id','collector_unit_sha256','issuer_unit_sha256'):
            p.add_argument(arg)
    a = parser.parse_args()
    target = release_candidate(a.revision,{C:a.collector_id,I:a.issuer_id},
                               {C:a.collector_unit_sha256,I:a.issuer_unit_sha256})
    need(os.geteuid() == 0 and socket.gethostname().split('.')[0] == 'kapu' and os.uname().machine == 'x86_64', 'kapu rootful amd64 전용')
    need(a.hold.parent == STORE and re.fullmatch('d-cutover-[a-zA-Z0-9][a-zA-Z0-9-]{0,63}',a.hold.name), 'hold 경로 불일치')
    trusted(Path(__file__).resolve())
    need(raw(RELEASE_ROOT / '.source-revision').removesuffix(b'\n') == a.revision.encode(),
         '배포 트리의 source revision과 불일치')
    need(json.loads(raw(RELEASE_ROOT / 'd-card-candidate.json')) == target, '배포 트리의 승인 후보와 불일치')
    os.umask(0o077)
    prepare_store(STORE, create=a.action == 'quiesce')
    cancelled = False
    def interrupted(signum, frame):
        nonlocal cancelled
        if not cancelled:
            cancelled = True
            raise InterruptedError('실행 중단')
    for sig in (signal.SIGINT,signal.SIGTERM,signal.SIGHUP):
        signal.signal(sig,interrupted)
    with contextlib.ExitStack() as stack:
        fd = os.open(STORE, os.O_DIRECTORY | os.O_NOFOLLOW)
        stack.callback(os.close,fd)
        fcntl.flock(fd,fcntl.LOCK_EX | fcntl.LOCK_NB)
        if a.action == 'quiesce':
            quiesce(a.hold, target)
        elif a.action == 'resume':
            resume(a.hold, target)
        else:
            check(a.hold, target)


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError, KeyboardInterrupt) as exc:
        print('전환 중단: ' + (str(exc) if isinstance(exc, Refused) else type(exc).__name__) + '; 증거 보존, 재시도/자동 복구 금지', file=sys.stderr)
        sys.exit(1)
