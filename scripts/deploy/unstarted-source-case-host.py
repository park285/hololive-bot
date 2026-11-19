#!/usr/bin/python3 -ISB
from decimal import Decimal
import hashlib
import os
import json
import re
import select
import signal
import sqlite3
from pathlib import Path
import time
import sys


def read(name):
    return (Path('/tmp') / name).read_text().strip()


def respond(value):
    print(value)
    raise SystemExit(0)


def report(kind, error, trace):
    import traceback
    with Path('/tmp/fixture-errors').open('a') as sink:
        traceback.print_exception(kind, error, trace, file=sink)


sys.excepthook = report
cmd = Path(sys.argv[0]).name
args = sys.argv[1:]
with Path('/events').open('a') as log:
    print(cmd, *args, file=log)
if cmd == 'sync':
    import subprocess
    subprocess.run(['/case/kernel-sync', *args], check=True)
    fault = Path('/tmp/fault').read_text().strip()
    if fault == 'publisher-sync-failure' and any(Path('/opt/hololive-bot/release-sources').glob('source-*.requested')):
        sys.exit(1)
    if fault == 'missing-optional' and args[0] == '-f' and args[-1].endswith('/writer-job'):
        dropin = Path('/etc/containers/systemd/hololive-bot/hololive-alarm-worker.container.d/40-x-spaces.conf')
        if dropin.exists():
            dropin.unlink()
            Path('/tmp/removed-optional').touch()
    if fault in ('late-writer', 'late-pending', 'late-restore'):
        store = Path('/opt/hololive-bot/compose/app-sources')
        receipt = store / ('transition-' + read('source-revision') + '-' + read('candidate-revision'))
        if args == ['-f', str(store.parent)] and (receipt/'pairs.after').is_file() and (receipt/'pairs.after').stat().st_size:
            if fault == 'late-restore':
                restored = store / 'restored-active'
                restored.write_text(read('source-revision')+'\n')
                restored.replace(store/'active')
                Path('/tmp/writer-restarted-at-sync').touch()
                sys.exit(0)
            Path('/tmp/fault').write_text('writer-changed' if fault == 'late-writer' else 'pending-at-sync')
            Path('/tmp/writer-restarted-at-sync').touch()
    sys.exit(0)
if cmd == 'systemctl':
    if args == ['show-environment']:
        sys.exit(0)
    if args == ['show', '--property=UnitPath', '--value']:
        respond('/etc/systemd/system /run/systemd/generator /usr/lib/systemd/system')
    if len(args) == 4 and args[0] == 'show' and args[1].endswith('.service') and args[2:] == ['--property=NeedDaemonReload', '--value']:
        respond('no')
if cmd == 'tailscale' and args == ['ip', '--assert', '100.100.1.8']:
    sys.exit(0)
if cmd == 'ip' and args == ['-4', '-o', 'addr', 'show', 'dev', 'tailscale0']:
    respond('5: tailscale0 inet 100.100.1.8/32 scope global tailscale0')
if cmd == 'nft' and args == ['-j', 'list', 'table', 'inet', 'admin_dashboard_ingress']:
    respond(Path('/case/ingress-firewall.json').read_text())
state = json.loads(read('observations'))
peers = state['peers']
fault = read('fault')
api, worker, db = 'hololive-api', 'hololive-alarm-worker', 'hololive-db-migrate'
fragment = Path('/run/systemd/generator/hololive-db-migrate.service')
if fault == 'writer-changed':
    peers[worker][0] = 'b' * 32
active = Path('/opt/hololive-bot/compose/current/artifacts/worker-profile/active')

def committing_confirmation():
    stage = Path('/opt/hololive-bot/release-sources') / ('dv-holo-worker-' + read('candidate-revision'))
    return (stage/'activation.confirmation.requested').exists() and any(stage.glob('.receipt-*'))


if fault == 'late-receipt-writer' and committing_confirmation():
    peers[api][0] = 'c'*32
if fault == 'late-receipt-lock' and committing_confirmation():
    lock = Path('/run/lock/hololive-quadlet-redeploy.lock')
    replacement = lock.with_name('.hololive-quadlet-redeploy.lock.next')
    replacement.write_bytes(lock.read_bytes())
    replacement.chmod(lock.stat().st_mode & 0o7777)
    replacement.replace(lock)
if cmd == 'systemctl' and args[:3] == ['show','--all','systemd-journald.service']:
    journald_start = Path('/tmp/journald-start')
    if not journald_start.exists():
        journald_start.write_text(str((time.monotonic_ns() - time.time_ns() + os.stat('/etc/systemd').st_ctime_ns) // 1000 + 1000))
    started = int(state['journal'][1]['__MONOTONIC_TIMESTAMP']) if fault == 'journald-restart' else int(journald_start.read_text())
    respond('ActiveState=active\nInvocationID='+'a'*32+'\nExecMainStartTimestampMonotonic='+str(started))
if cmd == 'journalctl' and args == ['--sync']:
    overlay_dir = Path('/etc/containers/systemd/hololive-bot/hololive-db-migrate.container.d')
    race = Path('/tmp/dispatch-race')
    if fault == 'health-at-sync' and read('candidate-revision') in (overlay_dir/'50-migrator-image.conf').read_text():
        race.touch()
    if fault == 'external-reload' and not race.exists() and any((active.parent/'releases').glob(api+'-v2-*/schema-job/request')):
        import subprocess
        race.touch()
        dropin = overlay_dir/'60-external.conf'
        dropin.write_text('[Service]\nExecStopPost=/usr/bin/false\n')
        subprocess.run(['systemctl','daemon-reload'],check=True)
        dropin.unlink()
        subprocess.run(['systemctl','daemon-reload'],check=True)
    sys.exit(0)
manager_request = cmd == 'busctl' and args[:2] == ['--system','--json=short']


def allocate_job():
    import fcntl
    with Path('/tmp/manager-allocator').open('a+') as stream:
        fcntl.flock(stream, fcntl.LOCK_EX)
        stream.seek(0)
        number = int(stream.read() or 34) + 1
        stream.seek(0)
        stream.truncate()
        stream.write(str(number))
    return number


def enqueue_target(unit, operation):
    requested = allocate_job()
    existing = state.get('pending_job')
    if existing:
        assert (existing['unit'], existing['operation']) == (unit, operation)
        return existing['id'], requested
    kind = 'restart' if operation == 'try-restart' else operation
    state['pending_job'] = dict(id=requested, operation=operation, unit=unit, kind=kind, polls=0)
    return requested, requested


def migrator_events(job):
    yield 'start', job['container_started_ns']
    if 'extra_start_ns' in job:
        yield 'died', job['first_exit_ns']
        yield 'start', job['extra_start_ns']
    yield 'died', job['started_ns']


def transient_events():
    file = Path('/tmp/transient-jobs')
    return [line.split() for line in file.read_text().splitlines()] if file.exists() else []


def persist():
    path = Path('/tmp/observations')
    temporary = path.with_suffix('.next')
    temporary.write_text(json.dumps(state))
    temporary.replace(path)


def background_job():
    number = allocate_job()
    with Path('/tmp/background-installs').open('a') as log:
        print(number, file=log)
    return [number, f'healthcheck-{number}.service', 'start', 'running', '', '']


if manager_request:
    background = background_job()
    pending_job = state.get('pending_job')
    if args[-1] == 'UnitsLoadTimestampMonotonic':
        respond(json.dumps(dict(type='t',data=state.get('reload_epoch',1))))
    if args[-1] == 'Version':
        respond(json.dumps(dict(type='s', data='258' if fault == 'unknown-allocator' else '257-33.el10')))
    if args[-1] in ('LogLevel', 'LogTarget'):
        value = dict(LogLevel='notice' if fault == 'quiet-manager' else 'info', LogTarget='journal-or-kmsg')[args[-1]]
        respond(json.dumps(dict(type='s', data=value)))
    if args[-1] == 'NInstalledJobs':
        count = 34 + len(state.get('jobs', {})) + len(state.get('allocations', [])) + int(bool(pending_job))
        attempt = active.parent / 'releases'
        count += sum(result == 'starting' for _, _, result in transient_events())
        count += sum(Path('/tmp', name).exists() for name in ('silent-cancel', 'unrelated-install'))
        count += len(Path('/tmp/background-installs').read_text().split())
        respond(json.dumps(dict(type='u', data=34 if fault == 'unaccounted-install' else count)))
    if args[-1] == 'ListJobs':
        transient = {number:result for _,number,result in transient_events()}
        running = [[int(number),'run-r'+number+'.service','start','running','',''] for number,result in transient.items() if result=='starting']
        if not pending_job:
            jobs = [[999,api+'.service','restart','waiting','','']] if Path('/tmp/racing-pending').exists() else []
            if fault == 'pending-nop':
                jobs.append([77,'init.scope','nop','waiting','',''])
            respond(json.dumps(dict(type='a(usssoo)', data=[jobs+running+[background]])))
        pending_job['polls'] += 1
        if pending_job['polls'] <= 2:
            persist()
            respond(json.dumps(dict(type='a(usssoo)', data=[running+[background]+[[pending_job['id'],pending_job['unit'],pending_job['kind'],'running','','']]])))
        cmd = 'systemctl'
        args = ['--show-transaction', '--job-mode=ignore-dependencies', pending_job['operation'], pending_job['unit']]
    else:
        assert args[6:8] == ['EnqueueUnitJob','sss'] and args[-1] == 'ignore-dependencies'
        bus_unit, operation = args[8:10]
        kind = 'restart' if operation == 'try-restart' else operation
        if operation == 'nop':
            assert bus_unit == 'init.scope' and not pending_job
            if fault in ('merged-schema', 'merged-writer'):
                state['reserved_job'] = allocate_job()
            number = allocate_job()
            state.setdefault('allocations', []).append(number)
        else:
            earlier = (fault,operation) in (('merged-schema','start'),('merged-writer','try-restart'))
            if earlier:
                state['pending_job'] = dict(id=state.pop('reserved_job'), operation=operation, unit=bus_unit, kind=kind, polls=0)
            number, requested = enqueue_target(bus_unit,operation)
            if earlier:
                Path('/tmp/merged-job').write_text(json.dumps(dict(anchor=number,requested=requested,unit=bus_unit)))
        persist()
        if fault == 'lost-response' and operation == 'start':
            sys.exit(1)
        respond(json.dumps(dict(type='uososa(uosos)', data=[number,f'/org/freedesktop/systemd1/job/{number}',bus_unit,
                           '/org/freedesktop/systemd1/unit/fixture',kind,[]])))


def writer_job(name):
    if fault == 'late-receipt-manager' and committing_confirmation():
        return '999'
    if name != worker or fault not in ('pending-worker', 'pre-reload-job','pre-activate-job','pre-overlay-job','pending-at-sync'):
        return ''
    candidate = read('candidate-revision')
    if fault in ('pending-worker','pre-activate-job','pre-overlay-job'):
        store = Path('/opt/hololive-bot/compose/app-sources')
        attempt = store / ('transition-' + read('source-revision') + '-' + candidate) / ('attempt-' + api) / 'release'
        if not attempt.exists():
            return ''
        release = Path(attempt.read_text().strip())
        if fault == 'pending-worker' and not (release / 'schema-job/request').exists():
            return ''
        if fault == 'pre-activate-job' and not (release / 'activation.intent').exists():
            return ''
        if fault == 'pre-overlay-job' and not list(Path('/etc/containers/systemd/hololive-bot/hololive-db-migrate.container.d').glob('.50-migrator-image.conf.*')):
            return ''
    elif fault == 'pre-reload-job':
        pair = Path((active / (api + '.pair')).read_text().strip())
        if (pair.parent / 'candidate.revision').read_text().strip() != candidate:
            return ''
    Path('/tmp/observed-pending-job').touch()
    return '999'


def api_binary():
    pid = int(read(api + '.pid'))
    return Path(f'/proc/{pid}/root/app/bin/{api}')


def launch(slot, pair, binary):
    import signal
    import subprocess
    import tempfile
    target = Path(tempfile.mkdtemp(dir='/tmp', prefix='writer-'))
    for src, dst in [('/etc/stack-secrets/hololive-bot/worker-profiles', '/run/hololive-bot/worker-profiles'),
                     (str(pair), '/run/hololive-bot/worker-profile-release'), (str(Path(sys.executable).resolve()), '/app/bin/' + binary)]:
        mount = target / dst[1:]
        if Path(src).is_dir():
            mount.mkdir(parents=True)
        else:
            mount.parent.mkdir(parents=True)
            mount.touch()
        subprocess.run(['mount', '--bind', src, str(mount)], check=True)
        subprocess.run(['mount', '-o', 'remount,bind,ro', str(mount)], check=True)
    pid = os.fork()
    if not pid:
        for fd in (0, 1, 2):
            os.dup2(os.open('/dev/null', os.O_RDWR), fd)
        os.closerange(3, os.sysconf('SC_OPEN_MAX'))
        os.chroot(target)
        os.chdir('/')
        signal.pause()
        os._exit(0)
    for _ in range(100):
        if (Path('/proc') / str(pid) / 'root').samefile(target):
            break
        time.sleep(.01)
    with Path('/tmp/extra-pids').open('a') as sink:
        print(pid, target, file=sink)
    return str(pid)


if cmd == 'systemctl':
    if args[:2] == ['--show-transaction', '--job-mode=ignore-dependencies'] and Path('/tmp/slot-jobs-enabled').exists():
        operation, unit = args[2:]
        name = unit.removesuffix('.service')
        jobs = state.setdefault('jobs', {})
        slot = 'hololive-alarm-worker' if api in jobs else api
        key = db + '-' + slot if name == db else name
        if (operation, name) not in [('start', db), ('try-restart', slot)] or key in jobs:
            sys.exit('refusing unknown or duplicate fixture job')
        pending_job = state['pending_job']
        assert pending_job['polls'] > 2
        seq = pending_job['id']
        now = time.time_ns()
        job = dict(job=str(seq), kind='start' if name == db else 'restart', invocation=f'{seq:032x}', cid=f'a{seq:063x}',
                   image='sha256:%064d' % (7 if name == worker else 6), unit=unit, at_ns=now)
        import shlex
        import subprocess
        env = dict(os.environ, PATH='/case/bin:/usr/bin:/usr/sbin')
        section = ''
        pre = []
        for line in fragment.with_name(unit).read_text().splitlines():
            if line.startswith('['):
                section = line
            key_, _, value = line.partition('=')
            if section == '[Service]' and key_ == 'ExecStartPre':
                if value:
                    pre.append(shlex.split(value))
                else:
                    pre.clear()
            if section == '[Service]' and key_ == 'Environment':
                for assignment in shlex.split(value):
                    k, v = assignment.split('=', 1)
                    env[k] = v
        for line in Path('/etc/stack-secrets/hololive-bot/compose.env').read_text().splitlines():
            k, v = line.split('=', 1)
            env[k] = v
        hook, = pre
        if name in (api, worker):
            job['old_invocation'], job['old_cid'] = peers[name][:2]
            job['stopping_ns'] = time.time_ns()
            fd = os.pidfd_open(int(read(name + '.pid')))
            try:
                signal.pidfd_send_signal(fd, signal.SIGTERM)
                if not select.select([fd], [], [], 5)[0]:
                    sys.exit('fixture writer failed to stop')
            finally:
                os.close(fd)
            job['stopped_ns'] = time.time_ns()
        job['starting_ns'] = time.time_ns()
        subprocess.run(hook, env=env, stdout=sys.stderr, check=True)
        job['container_started_ns'] = time.time_ns()
        if name in (api, worker):
            if db + '-' + slot not in jobs:
                sys.exit('no prior schema check')
            pair = Path((active / (name + '.pair')).read_text().strip())
            if (pair.parent / 'candidate.image').read_text().strip() != job['image']:
                sys.exit('writer transaction did not select its pinned image')
            pid = launch(name, pair, api if name == api else 'alarm-worker')
            Path('/tmp/' + name + '.pid').write_text(pid)
            state[name + '_started'] = time.time_ns()
            mount = json.loads(Path('/tmp/' + name + '.oci').read_text())
            mount['mounts'][-1]['source'] = str(pair)
            Path('/tmp/' + name + '.oci').write_text(json.dumps(mount))
            Path('/tmp/' + job['cid'] + '.loaded').write_text(json.loads((pair / 'pair.json').read_text())['profile_hash'])
            peers[name] = [job['invocation'], job['cid'], job['image'][7:], 'healthy']
        if fault == 'run-before-config' and name == db:
            Path('/tmp/direct-container').write_text(str(time.time_ns()//1000))
        if name == db and fault in ('merge-during-run','merge-extra-run'):
            number, _ = enqueue_target(unit,operation)
            merged = time.time_ns()
            assert number == seq and merged > job['container_started_ns']
            Path('/tmp/merged-running').write_text(str(seq))
            if fault == 'merge-extra-run':
                job['first_exit_ns'] = time.time_ns()
                job['extra_start_ns'] = time.time_ns()
        job['started_ns'] = time.time_ns()
        jobs[key] = job
        del state['pending_job']
        persist()
        if manager_request:
            respond(json.dumps(dict(type='a(usssoo)',data=[[]])))
        respond('Enqueued anchor job %s %s/%s.' % (job['job'], unit, job['kind']))
    if len(args) == 11 and args[0] == 'show' and args[1] in (api + '.service', worker + '.service', db + '.service') and args[2:] == [
            '-p', 'Requires', '-p', 'Wants', '-p', 'Requisite', '-p', 'BindsTo', '--value']:
        respond('-.mount holo-pgbouncer.service')
    if args == ['is-active', '--quiet', '-.mount']:
        sys.exit(1)
    if args == ['is-active', '--quiet', '--', '-.mount']:
        sys.exit(0)
    if args == ['daemon-reload']:
        state['reload_epoch'] = time.monotonic_ns()//1000
        selected = Path((active / (api + '.pair')).read_text().strip()).parent
        if fault == 'pre-reload-job' and (selected/'candidate.revision').read_text().strip() == read('candidate-revision'):
            Path('/tmp/reloaded-with-pending-job').touch()
        overlay = Path('/etc/containers/systemd/hololive-bot/hololive-db-migrate.container.d/50-migrator-image.conf')
        if Path('/tmp/slot-jobs-enabled').exists() and not Path('/tmp/replaced-migrator').exists():
            Path('/tmp/replaced-migrator').write_bytes(overlay.read_bytes())
            Path('/tmp/replaced-migrator').chmod(0o644)
        import os
        import subprocess
        import tempfile
        import shlex
        with tempfile.TemporaryDirectory() as out:
            subprocess.run(['/usr/libexec/podman/quadlet', out], env=dict(os.environ, QUADLET_UNIT_DIRS='/etc/containers/systemd/hololive-bot'), check=True)
            subprocess.run(['/usr/bin/cp', '-aT', out, fragment.parent], check=True)
        for name in (db, api, worker):
            loaded = {k: [] for k in ('ExecCondition','ExecStart','ExecStartPre','ExecStartPost','ExecReload','ExecStop','ExecStopPost')}
            section = ''
            for line in fragment.with_name(name+'.service').read_text().splitlines():
                if line.startswith('['):
                    section = line
                key, _, value = line.partition('=')
                if section == '[Service]' and key in loaded:
                    if value:
                        loaded[key].append(shlex.split(value))
                    else:
                        loaded[key].clear()
            state['bus' if name == db else name+'_bus'] = loaded
        if Path('/tmp/journal-ticks').exists():
            state.setdefault('reload_events',[]).append(time.time_ns()//1000)
        persist()
        sys.exit(0)
    if args == ['show', '--all', db + '.service', '-p', 'LoadState', '-p', 'ActiveState', '-p', 'SubState', '-p', 'Job']:
        respond('LoadState=loaded\nActiveState=inactive\nSubState=dead\nJob=')
    if len(args) == 4 and args[0] == 'show' and args[1] in [n + '.service' for n in (api, worker, db)] and args[2:] == ['--property=FragmentPath', '--value']:
        respond(fragment.with_name(args[1]))
    if args[:2] == ['is-active', '--quiet'] and args[-1].removesuffix('.service') in peers:
        sys.exit(0)
    if args[0] == 'show' and args[1].startswith('-.'):
        sys.exit(1)
    if args[:5] == ['show', '-p', 'Job', '--value', '--'] and len(args) == 6:
        respond(writer_job(args[5].removesuffix('.service')))
    if len(args) == 5 and args[0] == 'show' and args[2:] == ['-p', 'Job', '--value']:
        if fault == 'dependency-job' and args[1] == 'holo-pgbouncer.service':
            respond('998')
        respond(writer_job(args[1].removesuffix('.service')))
    if args == ['show', '--all', db + '.service', '-p', 'LoadState', '-p', 'ActiveState', '-p', 'SubState', '-p', 'Job', '-p', 'NeedDaemonReload',
                '-p', 'Type', '-p', 'Restart', '-p', 'RestartForceExitStatus']:
        restart = 'on-failure' if fault == 'restarting-migrator' else 'no'
        respond('LoadState=loaded\nActiveState=inactive\nSubState=dead\nJob=\nNeedDaemonReload=no\nType=oneshot\nRestart=' + restart + '\nRestartForceExitStatus=')
    if args[:2] == ['show','--all'] and args[3:] == ['-p','LoadState','-p','ActiveState','-p','NRestarts','-p','Job','-p','InvocationID']:
        name = args[2].removesuffix('.service')
        if name == worker and fault == 'pending-during-peer' and any((active.parent/'releases').glob(api+'-v2-*/schema-job/request')):
            Path('/tmp/racing-pending').touch()
        respond('LoadState=loaded\nActiveState=active\nNRestarts=0\nJob='+writer_job(name)+'\nInvocationID='+peers[name][0])
    if args[:2] == ['show', '--all'] and args[2] in (api + '.service', worker + '.service') and args[3:] == [
            '-p', 'ActiveState', '-p', 'InvocationID', '-p', 'Job', '-p', 'NRestarts']:
        name = args[2].removesuffix('.service')
        respond('ActiveState=active\nInvocationID=' + peers[name][0] + '\nJob=' + writer_job(name) + '\nNRestarts=0')
    if len(args) == 5 and args[0] == 'show' and args[2] == '-p' and args[4] == '--value':
        name = args[1].removesuffix('.service')
        if args[3] == 'LogLevelMax' and name in (api, worker, db):
            respond('5' if fault == 'quiet-unit' and name == db else '-1')
        if name in peers:
            if name == worker and args[3] == 'InvocationID' and fault == 'pending-during-peer' and any(
                    (active.parent/'releases').glob(api+'-v2-*/schema-job/request')):
                Path('/tmp/racing-pending').touch()
            values = {'LoadState': 'loaded', 'ActiveState': 'active', 'SubState': 'running', 'Job': writer_job(name),
                      'NRestarts': '0', 'InvocationID': peers[name][0]}
            if name in (api, worker):
                values['MainPID'] = read(name + '.pid')
            if args[3] in values:
                respond(values[args[3]])
if cmd == 'busctl':
    pathname = args[3]
    unit_name = pathname.rsplit('/',1)[-1].replace('_2d','-').removesuffix('_2eservice')
    assert unit_name in (db,api,worker)
    if fault.startswith('late-receipt-') and unit_name == db and args[-1] == 'ExecStopPost':
        Path('/tmp/receipt-checked').touch()
    loaded = state['bus' if unit_name == db else unit_name+'_bus']
    prefix = ['--json=short', 'get-property', 'org.freedesktop.systemd1', pathname]
    if args[:-2] == prefix and args[-2] == 'org.freedesktop.systemd1.Service' and args[-1] in loaded:
        commands = loaded[args[-1]]
        if unit_name == db and fault == 'stale-manager' and args[-1] == 'ExecStart':
            commands[0][-1] = '--schema-only'
        if fault == 'writer-effective-pre' and unit_name == worker and args[-1] == 'ExecStartPre':
            commands = []
        if fault == 'extra-condition' and args[-1] == 'ExecCondition':
            commands = [['/usr/bin/true']]
        data = [[c[0].removeprefix('-'), [c[0].removeprefix('-'), *c[1:]], c[0].startswith('-'), 0, 0, 0, 0, 0, 0, 0] for c in commands]
        respond(json.dumps({'type': 'a(sasbttttuii)', 'data': data}))
    if args[:-2] == prefix and args[-2] == 'org.freedesktop.systemd1.Unit':
        if args[-1] == 'DropInPaths':
            respond(json.dumps({'type': 'as', 'data': ['/etc/systemd/system/extra.conf'] if fault == 'native-overlay' or (fault == 'writer-native-dropin' and unit_name == worker) else []}))
        if args[-1] in ('Conditions', 'Asserts'):
            conditions = [['ConditionPathExists', False, True, '/var/lib/hololive-postgres-fence/' + n, 0] for n in ('fenced', 'fence.intent')]
            respond(json.dumps({'type': 'a(sbbsi)', 'data': conditions if args[-1] == 'Conditions' else []}))
if cmd == 'systemd-run':
    import os
    offset = args.index('/usr/bin/python3')
    assert args[offset + 2] == 'check-resolved' and args[offset + 3] in (api, worker)
    env = dict(os.environ)
    for opt in args[:offset]:
        if opt.startswith('--setenv='):
            k, v = opt[9:].split('=', 1)
            env[k] = v
        elif opt.startswith('--property=EnvironmentFile='):
            for line in Path(opt.split('=', 2)[2]).read_text().splitlines():
                if line and not line.startswith('#'):
                    k, v = line.split('=', 1)
                    env[k] = v
    import subprocess
    number = allocate_job()
    with Path('/tmp/transient-jobs').open('a') as stream:
        print(time.time_ns()//1000, number, 'starting', file=stream)
    with Path('/tmp/transient-profiles').open('a') as stream:
        print(number, args[offset+3], args[offset+4], file=stream)
    result = subprocess.run(args[offset:],env=env)
    with Path('/tmp/transient-jobs').open('a') as stream:
        print(time.time_ns()//1000, number, 'done' if not result.returncode else 'failed', file=stream)
    sys.exit(result.returncode)
def profile_probe(pair, image):
    profile = json.loads((pair / 'profile.json').read_bytes())
    name = state['images'][image.removeprefix('sha256:')][0]
    assert profile == {'fixture': name}


if cmd == 'podman':
    if args[:3] == ['--runtime', 'crun', 'create']:
        assert args[-1] == '--check-worker-profile' and '--network=none' in args and '--read-only' in args
        image = args[-2]
        name = state['images'][image][0]
        pair, = [Path(a.split(':')[0]) for a in args if ':/run/hololive-bot/worker-profile-release:' in a]
        profile_probe(pair, image)
        probe = hashlib.sha256(str(pair).encode()).hexdigest()
        binary = api if name == api else 'alarm-worker'
        pid = launch(name, pair, binary)
        spec = json.loads(Path('/tmp/' + name + '.oci').read_text())
        spec['process'].update(args=[args[-3], args[-1]], env=sys.stdin.read().splitlines())
        spec['mounts'][-1]['source'] = str(pair)
        path = Path('/tmp/probe-' + probe)
        path.write_text(json.dumps(dict(image=image, pid=pid, pair=str(pair))))
        path.with_suffix('.oci').write_text(json.dumps(spec))
        Path(args[args.index('--cidfile') + 1]).write_text(probe)
        sys.exit(0)
    probe = Path('/tmp/probe-' + args[-1]) if args and re.fullmatch('[0-9a-f]{64}', args[-1]) else None
    if probe and probe.is_file():
        data = json.loads(probe.read_bytes())
        if args[:-1] == ['container', 'inspect']:
            respond(json.dumps([dict(Image=data['image'], State=dict(Pid=int(data['pid']), Status='initialized'), OCIConfigPath=str(probe.with_suffix('.oci')))]))
        if args[:-1] == ['start', '--attach']:
            profile_probe(Path(data['pair']), data['image'])
            sys.exit(0)
        if args[:-1] == ['init']:
            sys.exit(0)
        if args[:-1] == ['rm', '--force', '--ignore', '--time=0']:
            os.kill(int(data['pid']), 15)
            probe.unlink()
            sys.exit(0)
    if args[0] == 'run' and args[-1] == '--check-worker-profile':
        assert '--read-only' in args and args[args.index('--network') + 1] == 'none'
        mount, = [v for v in args if 'dst=/run/hololive-bot/worker-profile-release,readonly' in v]
        profile_probe(Path(mount.split('src=')[1].split(',')[0]), args[-2])
        sys.exit(0)
    if args[0] == 'tag' and args[-1].startswith('hololive-worker-profile:pair-'):
        assert args[1].removeprefix('sha256:') in state['images']
        sys.exit(0)
    if args == ['exec', 'holo-pgbouncer', 'sha256sum', '/etc/pgbouncer/pgbouncer.ini']:
        respond(hashlib.sha256(Path(args[-1]).read_bytes()).hexdigest() + '  ' + args[-1])
    if args[:4] == ['exec', '-i', 'holo-postgres', 'env'] and args[-1] == 'hololive':
        sql = sys.stdin.read()
        assert 'READ ONLY' in sql and 'pending = 0' in sql and sql.strip().endswith('ROLLBACK;')
        respond('t')
    if args[:4] == ['exec', '-i', 'holo-postgres', 'timeout'] and args[-1] in ('hololive_runtime', 'hololive_alarm_worker'):
        assert 'BEGIN READ ONLY;' in sys.stdin.read() and 'PGSSLMODE=verify-full' in args[-3]
        respond('t')
    if args == ['info', '--format', '{{.Host.EventLogger}}']:
        respond('journald')
    if args == ['info', '--format', '{{.Host.Arch}}']:
        respond('arm64')
    if args[:2] == ['image', 'exists']:
        sys.exit(0 if args[2].removeprefix('sha256:') in state['images'] else 1)
    if args[:2] == ['image', 'inspect']:
        image = args[-1].removeprefix('sha256:')
        if image.startswith('localhost/dv-'):
            image = {'localhost/dv-holo-api:' + read('candidate-revision'): '%064d' % 6,
                     'localhost/dv-holo-worker:' + read('candidate-revision'): '%064d' % 7}[image]
        name, revision = state['images'][image]
        label = 'holo-api' if name == api else 'holo-worker'
        if args[2:4] == ['--format', '{{.ManifestType}}|{{json .RootFS.Layers}}|{{json .Config.Healthcheck}}']:
            health = state['health'][label]
            respond('application/vnd.docker.distribution.manifest.v2+json|' + json.dumps(['sha256:' + image]) + '|' + json.dumps(health))
        if len(args) == 5 and args[2] == '--format' and 'org.opencontainers.image.created' in args[3]:
            respond(f'{image} linux/arm64 {name} {state["versions"][revision]} {revision} 2026-11-19T00:00:00Z')
        if len(args) == 3:
            respond(json.dumps([{'Id': image, 'Os': 'linux', 'Architecture': 'arm64', 'Config': {'Labels': {
                'org.opencontainers.image.revision': revision, 'org.opencontainers.image.title': name}}}]))
        formats = {'{{.Id}} {{.Os}}/{{.Architecture}} {{index .Config.Labels "org.opencontainers.image.title"}} {{index .Config.Labels "org.opencontainers.image.revision"}}': f'{image} linux/arm64 {name} {revision}',
                   '{{.Id}}': image, '{{.Os}}/{{.Architecture}}': 'linux/arm64',
                   '{{eq (index .Config.Labels "io.hololive-bot.outbox-schema") "text-file-v1"}}': 'true',
                   '{{index .Config.Labels "org.opencontainers.image.version"}}': state['versions'].get(revision),
                   '{{.Id}} {{index .Config.Labels "org.opencontainers.image.revision"}} {{index .Config.Labels "org.opencontainers.image.title"}}': f'{image} {revision} {name}',
                   '{{index .Config.Labels "org.opencontainers.image.revision"}}': revision,
                   '{{index .Config.Labels "org.opencontainers.image.title"}}': name}
        if args[2] == '--format' and args[3] in formats:
            respond(formats[args[3]])
    by_cid = {g[1]: n for n, g in peers.items() if len(g) == 4}
    if args[:2] == ['container', 'inspect'] and (args[-1] in peers or args[-1] in by_cid):
        name = by_cid.get(args[-1], args[-1])
        _, cid, image, health = peers[name]
        if fault == 'unhealthy-' + name or fault == 'health-at-sync' and name == worker and Path('/tmp/dispatch-race').exists():
            health = 'unhealthy'
        pid = read(name + '.pid') if name in (api, worker) else read('infra-pid')
        started = str(state.get(name + '_started', state['started']))
        revision = state['images'].get(image, [None, None])[1]
        version = state['versions'].get(revision)
        if len(args) == 3 and name in (api, worker):
            live_spec = json.loads(Path('/tmp/' + name + '.oci').read_bytes())
            respond(json.dumps([{'Id': cid, 'Image': image, 'Name': name,
                'State': {'Running': True, 'Pid': int(pid), 'StartedAt': started},
                'OCIRuntime': 'crun', 'HostConfig': {'Tmpfs': {}}, 'Mounts': [{'Type': 'bind', 'Destination': m['destination']} for m in live_spec['mounts']],
                'Config': {'Entrypoint': [live_spec['process']['args'][0]], 'Env': ['HOLOLIVE_METRICS_ADDR=:30003', 'APP_VERSION=' + version], 'Labels': {
                    'io.hololive.worker-profile-pair': live_spec['mounts'][-1]['source']}},
                'OCIConfigPath': '/tmp/' + name + '.oci'}]))
        formats = {'{{.Id}} {{.State.Pid}} {{.State.Running}} {{.State.Health.Status}}': f'{cid} {pid} true {health}',
                   '{{.State.Running}} {{.Id}} {{.Image}} {{.State.StartedAt.UnixNano}}': f'true {cid} {image} {started}',
                   '{{.Id}} {{.State.Pid}}': f'{cid} {pid}', '{{.State.Pid}} {{.State.Health.Status}}': f'{pid} {health}',
                   '{{.Id}} {{.Image}} {{.State.Health.Status}}': f'{cid} {image} {health}',
                   '{{.Id}} {{.Image}} {{.State.Health.Status}} {{.State.StartedAt.UnixNano}}': f'{cid} {image} {health} {started}',
                   '{{.State.Pid}} {{.State.ConmonPid}} {{.State.StartedAt.UnixNano}}': f'{pid} {pid} {started}',
                   '{{index .Config.Labels "org.opencontainers.image.revision"}}': revision,
                   '{{.Image}} {{index .Config.Labels "org.opencontainers.image.revision"}} {{.State.Health.Status}}': f'{image} {revision} {health}'}
        if len(args) == 5 and args[2] == '--format' and args[3] in formats:
            respond(formats[args[3]])
    if args[:2] == ['inspect', '-f']:
        if args[2] == '{{.State.Running}} {{.Id}} {{.Image}} {{.State.ConmonPid}}' and args[3] in (api, worker):
            _, cid, image, _ = peers[args[3]]
            respond(f'true {cid} {image} ' + read(args[3] + '.pid'))
        if args[2] == '{{range .Config.Env}}{{println .}}{{end}}' and args[3] in (api, worker, peers[api][1], peers[worker][1]):
            respond('POSTGRES_HOST=holo-pgbouncer\nPOSTGRES_PORT=6432')
    if len(args) == 6 and args[0] == 'exec' and args[1] in (peers[api][1], peers[worker][1]) and args[2:] == [
            '/app/bin/healthcheck', '--body-api-key-env', 'API_SECRET_KEY', 'http://127.0.0.1:30003/diagnostics/workers']:
        respond(json.dumps({'contractVersion': 1, 'profile': {'hash': read(args[1] + '.loaded')}}))
    if args[:2] == ['events', '--stream=false']:
        for job in state.get('jobs', {}).values():
            if job['unit'] == db + '.service' and 'container=' + job['cid'] in args:
                if 'event=start' in args:
                    image = job['image'] if fault != 'wrong-run-image' else 'sha256:'+'d'*64
                    assert '.Image' in args[-1] and ('"'+job['image']+'"') in args[-1]
                    reference = 'localhost/schema-check:fixture' if fault != 'wrong-run-image' else image
                    respond('\n'.join(f'{job["cid"]}|{db}|{stamp}|image={reference}'
                                      for event,stamp in migrator_events(job) if event=='start'))
                if 'event=died' in args:
                    respond('\n'.join(f'{job["cid"]}|{db}|{stamp}|0' for event,stamp in migrator_events(job) if event=='died'))
    if api in state.get('jobs', {}) and args == ['exec', peers[api][1], './bin/healthcheck', '--api-key-env', 'API_SECRET_KEY',
                                                  'https://127.0.0.1:30001/internal/ready', 'https://127.0.0.1:30006/health']:
        if fault == 'late-receipt-run' and Path('/tmp/receipt-checked').exists():
            Path('/tmp/direct-container').write_text(str(time.time_ns()//1000))
        if fault in ('late-receipt-stage','late-receipt-transition') and committing_confirmation():
            candidate = read('candidate-revision')
            root = Path('/opt/hololive-bot')
            original = root/'release-sources'/('dv-holo-worker-'+candidate)/'activation.confirmation.requested'
            if fault == 'late-receipt-transition':
                original = root/'compose/app-sources'/('transition-'+read('source-revision')+'-'+candidate)/'receipt.requested'
            original.write_text('{}\n')
        sys.exit(0 if api_binary().samefile('/proc/' + read(api + '.pid') + '/exe') else 1)
    if worker in state.get('jobs', {}) and args == ['exec', peers[worker][1], './bin/healthcheck', '--api-key-env', 'API_SECRET_KEY', 'https://127.0.0.1:30007/internal/ready']:
        if fault == 'worker-not-ready':
            Path('/tmp/readiness-refused').touch()
            sys.exit(1)
        sys.exit(0 if Path('/proc', read(worker + '.pid'), 'exe').samefile(Path('/proc', read(worker + '.pid'), 'root/app/bin/alarm-worker')) else 1)
    if args == ['exec', '-i', '-e', 'PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=30s -c lock_timeout=3s',
                'holo-postgres', 'psql', '-XqAtw', '-v', 'ON_ERROR_STOP=1', '-h', '/var/run/postgresql', '-U', 'postgres_admin', '-d', 'hololive']:
        sql = sys.stdin.read().strip()
        source = Path('/opt/hololive-bot/compose/app-sources') / read('source-revision') / 'hololive/hololive-api'
        catalog = (source/'scripts/migrations/contracts/001_schema_epoch4_baseline.sql').read_text().strip()
        setup, separator, queries = sql.partition(catalog)
        assert separator and 'BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;' in setup
        statements = queries.strip().split(';')
        assert statements[-2:] == ['\nROLLBACK', '']
        db_conn = sqlite3.connect(':memory:', uri=True)
        db_conn.execute("attach database 'file:/tmp/ledger.db?mode=ro' as public")
        db_conn.execute('pragma query_only=on')
        db_conn.create_function('md5', 1, lambda v: hashlib.md5(v.encode()).hexdigest())
        db_conn.create_function('convert_to', 2, lambda v, enc: v.encode(enc))
        db_conn.create_function('sha256', 1, lambda v: hashlib.sha256(v).digest())
        db_conn.create_function('encode', 2, lambda v, enc: v.hex() if enc == 'hex' else None)
        db_conn.create_function('json_build_array', -1, lambda *v: json.dumps(v))
        db_conn.create_function('pg_relation_filenode', 1, lambda oid: oid + 100)
        db_conn.execute("attach database 'file:/tmp/ledger.db?mode=ro' as pg_catalog")
        lineage = setup[setup.index('WITH epoch3('):].strip()
        active = re.search(r"FROM unnest\(string_to_array\('([^']+)'::text, E'\\n'\)\) WITH ORDINALITY AS f\(line,ordinal\)", lineage)
        assert active
        checksums = [line.split('  ') for line in active[1].splitlines()]
        start = lineage.index('SELECT split_part(')
        lineage = lineage[:start] + ' VALUES ' + ','.join('(?,?,?)' for _ in checksums) + lineage[active.end():]
        lineage = lineage.replace('active AS (', 'active(filename,checksum,ordinal) AS (').replace('::text', '').replace('::integer', '')
        lineage = lineage.replace('bool_and(', 'min(').replace('::regclass', '').replace('SELECT FROM', 'SELECT 1 FROM')
        args_ = [v for i,(checksum,filename) in enumerate(checksums, 1) for v in (filename,checksum,i)]
        results = ['|'.join(map(str,row)) for row in db_conn.execute(lineage, args_)]
        def present(name):
            row = db_conn.execute('SELECT oid FROM pg_class WHERE oid=?', (name,)).fetchone()
            return row[0] if row else None
        def privilege(role, obj, action):
            return bool(db_conn.execute('SELECT 1 FROM privilege WHERE role=? AND object=? AND action=?', (role,obj,action)).fetchone())
        db_conn.create_function('to_regclass', 1, present)
        db_conn.create_function('to_regprocedure', 1, present)
        for kind in ('schema', 'table', 'sequence'):
            db_conn.create_function('has_' + kind + '_privilege', 3, privilege)
        query = catalog.replace('pg_catalog.', '').replace('SELECT FROM', 'SELECT 1 FROM')
        results.extend('t' if value else 'f' for value, in db_conn.execute(query))
        for statement in statements[:-2]:
            logical = ("json_patch(json_object('filename',m.filename,'applied_at',m.applied_at),"
                       "json_object('filename',c.filename,'recorded_at',c.recorded_at,'checksum_sha256',c.checksum_sha256))")
            sqlite = statement.replace('to_jsonb(m)||to_jsonb(c)', logical).replace('::text', '')
            sqlite = sqlite.replace(r"E'\n'", "char(10)").replace('COLLATE "C"', 'COLLATE binary')
            row, = db_conn.execute(sqlite)
            results.append('|'.join(map(str, row)))
        respond('\n'.join(results))
if cmd == 'journalctl':
    if fault == 'corrupt-journal-file':
        os.write(2, b'Journal file /var/log/journal/0/system@0-1.journal~ corrupted, ignoring file.\n')
    if any(a.startswith('--since=@') for a in args):
        since_ns = int(Decimal(next(a.split('@')[1] for a in args if a.startswith('--since=@'))) * 1_000_000_000)
        for name, job in state.get('jobs', {}).items():
            if job['at_ns'] < since_ns:
                continue
            if all(x in args for x in ('_PID=1', 'UNIT=' + job['unit'], 'JOB_ID=' + job['job'], 'JOB_RESULT=done',
                                       'MESSAGE_ID=39f53479d3a045ac8e11786248231fbf')):
                respond(json.dumps(dict(INVOCATION_ID=job['invocation'], JOB_ID=job['job'], UNIT=job['unit'], JOB_RESULT='done')))
            if job['unit'] == db + '.service' and '_SYSTEMD_INVOCATION_ID=' + job['invocation'] in args and 'CONTAINER_NAME=' + db in args:
                respond(json.dumps(dict(CONTAINER_ID_FULL=job['cid'], MESSAGE='==> hololive migrations applied (applied=0 skipped=32 total=32)')))
            if job['unit'] == db+'.service' and all(k in args for k in ('_UID=0','_COMM=podman','_SYSTEMD_UNIT='+job['unit'],
                    '_SYSTEMD_INVOCATION_ID='+job['invocation'],'PODMAN_NAME='+db,'PODMAN_EVENT=start','PODMAN_ID='+job['cid'])):
                respond(json.dumps(dict(_CMDLINE='/usr/bin/podman run --name '+db+' '+job['image']+' --check')))
            if name in (api, worker) and all(k in args for k in ('PODMAN_EVENT=start', 'PODMAN_NAME=' + name)):
                start = dict(PODMAN_ID=job['cid'], _SYSTEMD_INVOCATION_ID=job['invocation'], _SYSTEMD_UNIT=job['unit'], _COMM='podman')
                if fault == 'foreign-container-start' and not any(k.startswith('_SYSTEMD_UNIT=') for k in args):
                    print(json.dumps(dict(start, _SYSTEMD_UNIT='unrelated.scope')))
                respond(json.dumps(start))
    prefix = ['--boot', '--quiet', '--no-pager', '-o', 'json',
              '--output-fields=_BOOT_ID,_PID,UNIT,MESSAGE_ID,JOB_ID,JOB_TYPE,JOB_RESULT,INVOCATION_ID,MESSAGE,_TRANSPORT,SYSLOG_IDENTIFIER,N_DROPPED,_UID,_SYSTEMD_UNIT,_COMM,SYSLOG_PID,PRIORITY,PODMAN_NAME,PODMAN_EVENT,PODMAN_ID,_SYSTEMD_INVOCATION_ID,_AUDIT_TYPE_NAME,AUDIT_FIELD_UNIT,CONTAINER_NAME,CONTAINER_ID_FULL,CODE_FUNC']
    if args[:len(prefix)] == prefix:
        options = args[len(prefix):]
        rows = list(state['journal'])
        last = rows[-1]
        events = [(int(t), dict(UNIT=None, JOB_ID=None, JOB_TYPE=None, JOB_RESULT=None, MESSAGE_ID=None,
                                MESSAGE='Reloading...')) for t in Path('/tmp/journal-ticks').read_text().splitlines()]
        for stamp, number, result in transient_events():
            if int(stamp) >= int(rows[0]['__REALTIME_TIMESTAMP']):
                mid = '7d4958e842da4a758f6c1cdc7b36dcc5' if result == 'starting' else '39f53479d3a045ac8e11786248231fbf'
                events.append((int(stamp), dict(UNIT='run-r'+number+'.service',JOB_ID=number,JOB_TYPE='start',
                                               JOB_RESULT=None if result=='starting' else result, MESSAGE_ID=mid)))
        events.extend((us, dict(UNIT=None,JOB_ID=None,JOB_TYPE=None,JOB_RESULT=None,MESSAGE_ID=None, MESSAGE='Reloading finished in 2 ms.')) for us in state.get('reload_events', []))
        for j in state.get('jobs', {}).values():
            base = dict(UNIT=j['unit'], JOB_ID=j['job'])
            name = j['unit'].removesuffix('.service')
            audit = dict(_TRANSPORT='audit',_PID='1',_UID='0',UNIT=None,JOB_ID=None,JOB_TYPE=None,JOB_RESULT=None,
                         MESSAGE_ID=None,AUDIT_FIELD_UNIT=name)
            events.append((j['started_ns']//1000, dict(audit,_AUDIT_TYPE_NAME='SERVICE_START')))
            if 'old_invocation' in j:
                events.append((j['stopped_ns']//1000, dict(audit,_AUDIT_TYPE_NAME='SERVICE_STOP')))
            def container(action, cid, stamp):
                return (stamp//1000, dict(_PID='444', _UID='0', _COMM='podman', UNIT=None, JOB_ID=None, JOB_TYPE=None,
                        JOB_RESULT=None, MESSAGE_ID=None, MESSAGE='', _SYSTEMD_UNIT=j['unit'], _SYSTEMD_INVOCATION_ID=j['invocation'],
                        PODMAN_NAME=name, PODMAN_EVENT=action, PODMAN_ID=cid))
            if name == db:
                events.extend(container(action,j['cid'],stamp) for action,stamp in migrator_events(j))
            else:
                us, event = container('start',j['cid'],j['container_started_ns'])
                events.append((us,event))
                if fault == 'foreign-container-start':
                    events.append((us,dict(event,_SYSTEMD_UNIT='unrelated.scope')))
            if 'old_invocation' in j:
                events.append(container('died',j['old_cid'],j['stopped_ns']))
                old_inv = 'e'*32 if fault == 'wrong-stop-invocation' else j['old_invocation']
                for stamp, mid, result in [('stopping_ns','de5b426a63be47a7b6ac3eaac82e2f6f',None),
                                           ('stopped_ns','9d1aaa27d60140bd96365438aad20286','done')]:
                    if fault == 'missing-stop' and stamp == 'stopped_ns':
                        continue
                    us = j['started_ns']//1000 + 1 if fault == 'reordered-stop' else j[stamp]//1000
                    events.append((us, dict(base, INVOCATION_ID=old_inv, MESSAGE_ID=mid, JOB_TYPE='restart', JOB_RESULT=result)))
            for stamp, mid, result in [('starting_ns','7d4958e842da4a758f6c1cdc7b36dcc5',None),
                                       ('started_ns','39f53479d3a045ac8e11786248231fbf','done')]:
                events.append((j[stamp]//1000, dict(base, INVOCATION_ID=j['invocation'], MESSAGE_ID=mid, JOB_TYPE='start', JOB_RESULT=result)))
        extra_run = Path('/tmp/direct-container')
        if extra_run.exists():
            events.append((int(extra_run.read_text()), dict(_PID='333',_UID='0',_COMM='podman',_SYSTEMD_UNIT='session.scope',
                          PODMAN_EVENT='start',PODMAN_NAME=db,PODMAN_ID='a'*64)))
        other = Path('/tmp/unrelated-install')
        if other.exists():
            events.append((int(other.read_text()), dict(UNIT='backup.service', JOB_ID='9000', JOB_TYPE='start', JOB_RESULT='done',
                                                        MESSAGE_ID='39f53479d3a045ac8e11786248231fbf')))
        extra = Path('/tmp/extra-schema-event')
        if extra.exists():
            events.append((int(extra.read_text()), dict(UNIT=db+'.service', JOB_ID='777', JOB_TYPE='start', JOB_RESULT='canceled',
                                                        MESSAGE_ID='be02cf6855d2428ba40df7e9d022f03d')))
        for us, event in events:
            mono = int(last['__MONOTONIC_TIMESTAMP']) + us - int(last['__REALTIME_TIMESTAMP'])
            record = dict(last, __REALTIME_TIMESTAMP=str(us), __MONOTONIC_TIMESTAMP=str(mono), **event)
            rows.append({key:value for key,value in record.items() if value is not None})
        rows.sort(key=lambda row:int(row['__REALTIME_TIMESTAMP']))
        for seq,row in enumerate(rows,1):
            boot = row['_BOOT_ID']
            mono,us = (int(row[key]) for key in ('__MONOTONIC_TIMESTAMP','__REALTIME_TIMESTAMP'))
            row['__CURSOR'] = f's={"a"*32};i={seq:x};b={boot};m={mono:x};t={us:x};x={seq:x}'
        if options[-1] == '_PID=1':
            rows = [r for r in rows if r['_PID'] == '1']
            options = options[:-1]
        if fault == 'manager-debug':
            rows[1] = {k:v for k,v in rows[1].items() if not k.startswith('JOB_') and k != 'MESSAGE_ID'}
            rows[1].update(UNIT=db+'.service', MESSAGE=f'Installed new job {db}.service/start as 42')
        if fault == 'suppressed-manager':
            rows[2] = dict(rows[2], _TRANSPORT='driver', MESSAGE_ID='a596d6fe7bfa4994828e72309e95d61e', N_DROPPED='2',
                           MESSAGE='Suppressed 2 messages from init.scope')
        if fault in ('anonymous-manager-start', 'kmsg-manager-start', 'unattributed-kmsg'):
            rows[2] = {k:v for k,v in rows[2].items() if k not in ('UNIT','MESSAGE_ID') and not k.startswith('JOB_')}
            rows[2].update(_PID='1', _SYSTEMD_UNIT='init.scope', MESSAGE='Starting Hololive schema migration (one-shot)...')
            if fault != 'anonymous-manager-start':
                rows[2].pop('_PID')
                rows[2].update(_TRANSPORT='kernel', SYSLOG_IDENTIFIER='systemd')
                if fault == 'kmsg-manager-start':
                    rows[2]['SYSLOG_PID'] = '1'
        if fault in ('unrelated-audit','protected-audit'):
            rows[2].update(_TRANSPORT='audit', _PID='1', _AUDIT_TYPE_NAME='SERVICE_START',
                           AUDIT_FIELD_UNIT='backup' if fault == 'unrelated-audit' else db)
            rows[2].pop('UNIT',None)
        if fault == 'pid1-avc':
            rows[2].update(_TRANSPORT='audit', _PID='1', _AUDIT_TYPE_NAME='USER_AVC',
                           MESSAGE='avc:  denied  { status } for auid=n/a uid=0 gid=0 tclass=service permissive=0')
            rows[2].pop('UNIT',None)
        if fault in ('firewall-drop','ring-loss','credential-loss'):
            rows[2].update(_TRANSPORT='driver' if fault=='credential-loss' else 'kernel', _PID='5',
                           MESSAGE={'firewall-drop':'firewall: packet dropped','ring-loss':'Missed 17 kernel messages',
                                    'credential-loss':'Failed to get sender credentials'}[fault])
        if fault in ('no-manager-pid','no-manager-uid'):
            rows[2].update(_PID='1', _UID='0', UNIT=db+'.service', JOB_TYPE='start')
            rows[2].pop('_PID' if fault=='no-manager-pid' else '_UID')
        if fault == 'no-podman-uid':
            rows[2].update(PODMAN_EVENT='start',PODMAN_NAME=db)
            rows[2].pop('_UID',None)
        if fault == 'notice-only':
            rows[1].update(PRIORITY='5',MESSAGE="Reload requested from client PID 45 ('systemctl')...")
        if fault == 'skipped-via-kmsg':
            rows[2].update(_PID='0',_TRANSPORT='kernel',SYSLOG_PID='1',MESSAGE='Hololive schema migration (one-shot) was skipped ...')
            for key in ('UNIT','MESSAGE_ID','JOB_ID','JOB_TYPE','JOB_RESULT'):
                rows[2].pop(key,None)
        if fault == 'unexpected-write-error':
            rows[2].update(_TRANSPORT='driver',MESSAGE='Unexpected error while writing to journal file')
        if fault == 'original-cancel':
            rows[2].update(_PID='1', _UID='0', _SYSTEMD_UNIT='init.scope', UNIT=db+'.service', JOB_ID='42', JOB_TYPE='start',
                           JOB_RESULT='canceled', MESSAGE_ID='be02cf6855d2428ba40df7e9d022f03d')
        if fault == 'journal-store-failure':
            rows[2].update(_TRANSPORT='driver', MESSAGE='Failed to write entry despite vacuuming, ignoring')
        if fault in ('entry-printing-failed', 'kmsg-read-failed', 'kernel-audit-start', 'migrator-completion-row', 'permission-warning'):
            rows[2] = {k: v for k, v in rows[2].items() if k not in ('UNIT', 'MESSAGE_ID', 'N_DROPPED', '_PID') and not k.startswith('JOB_')}
            rows[2].update({
                'entry-printing-failed': dict(_TRANSPORT='driver', MESSAGE='Entry printing failed: Cannot allocate memory'),
                'kmsg-read-failed': dict(_TRANSPORT='driver', MESSAGE='Failed to read from /dev/kmsg: Broken pipe'),
                'kernel-audit-start': dict(_TRANSPORT='kernel', MESSAGE="audit: type=1130 audit(1795140000.123:42): pid=1 uid=0 "
                                           "auid=4294967295 ses=4294967295 msg='unit=" + db + " comm=\"systemd\" res=success'"),
                'migrator-completion-row': dict(_TRANSPORT='journal', _UID='0', _PID='4242', _COMM='conmon', SYSLOG_IDENTIFIER='conmon',
                                                CONTAINER_NAME=db, CONTAINER_ID_FULL='0' * 64, MESSAGE='applied=0 skipped=32 total=32'),
                'permission-warning': dict(_TRANSPORT='journal', _PID='1', _UID='0', _SYSTEMD_UNIT='init.scope', CODE_FUNC='stat_warn_permissions',
                                           MESSAGE='Configuration file /etc/systemd/system/backup.timer is marked executable. '
                                                   'Please remove executable permission bits. Proceeding anyway.'),
            }[fault])
        if fault in ('daemon-reload', 'unknown-manager-observation', 'unattributed-reload'):
            rows[1] = {k:v for k,v in rows[1].items() if k not in ('UNIT','MESSAGE_ID') and not k.startswith('JOB_')}
            rows[1]['MESSAGE'] = {'daemon-reload':'Reloading finished in 48 ms.', 'unknown-manager-observation':'Manager trace',
                                  'unattributed-reload':'Reloading Hololive worker...'}[fault]
        if fault == 'application-queue':
            rows[2].update(_TRANSPORT='stdout', MESSAGE='Dropped 2 messages from application queue')
        if fault == 'audit-only-close':
            for row in rows[3:]:
                row.update(_TRANSPORT='audit',_PID='1',_SYSTEMD_UNIT='init.scope',_AUDIT_TYPE_NAME='SERVICE_START',AUDIT_FIELD_UNIT='backup')
        if fault == 'unresolved-manager-pool':
            for row in rows[3:]:
                row.update(_PID='44', _SYSTEMD_UNIT='unrelated.service')
        if fault == 'missing-coverage':
            rows = rows[:2]
        if fault == 'missing-journal-segment':
            rows = [r for r in rows if r['_PID'] == '1']
        if fault == 'switched-sequence':
            rows[2] = dict(rows[2], __CURSOR=rows[2]['__CURSOR'].replace('a' * 32, 'c' * 32))
        if fault in (api, worker, db):
            rows[1] = dict(rows[1], UNIT=fault + '.service', JOB_ID='42')
        if fault == 'unknown-job-unit':
            rows[1] = dict(rows[1], JOB_ID='42')
            rows[1].pop('UNIT',None)
        if len(options) == 3 and options[0].startswith('--until=@') and options[1:] == ['-n', '1']:
            end = int(Decimal(options[0].split('@')[1]) * 1000000)
            rows = [r for r in rows if int(r['__REALTIME_TIMESTAMP']) <= end][-1:]
        elif len(options) == 2 and options[0].startswith('--since=@') and options[1].startswith('--until=@'):
            if fault == 'unknown-journal':
                sys.exit(1)
            start, end = [int(Decimal(opt.split('@')[1]) * 1000000) for opt in options]
            rows = [r for r in rows if start <= int(r['__REALTIME_TIMESTAMP']) <= end]
        else:
            sys.exit('unrecognized journal window')
        for row in rows:
            print(json.dumps(row))
        sys.exit(0)
with Path('/tmp/unexpected-commands').open('a') as log:
    print(cmd, *args, file=log)
sys.exit(93)
