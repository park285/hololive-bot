import ast
import datetime
import hashlib
import json
import os
from pathlib import Path
import subprocess
import re
import runpy
import sqlite3
import shlex
import shutil
import sys
import time


if sys.argv[1] == 'detached-publisher':
    sys.argv = sys.argv[2:]
    main = runpy.run_path(sys.argv[0])['main']
    invoke = main.__globals__['invoke']
    def observe(args, callback):
        def record_line(line):
            if line.startswith('[sync-opt-current] ERROR:'):
                print(line, file=sys.stderr)
            callback(line)
        return invoke(args, record_line)
    main.__globals__['invoke'] = observe
    try:
        main()
    except ValueError:
        sys.exit(75)
    sys.exit('detached publisher admitted')

phase, *selectors = sys.argv[1:]
root = Path('/opt/hololive-bot')
legacy = root / 'compose/current'
store = root / 'compose/app-sources'
stages = root / 'release-sources'
source = Path('/tmp/source-revision').read_text().strip()
writer = Path('/tmp/accepted-revision').read_text().strip()
next_revision = Path('/tmp/candidate-revision').read_text().strip()
api, worker = 'hololive-api', 'hololive-alarm-worker'
runtime = writer
successor = phase == 'stage-successor'
if successor:
    source, writer = next_revision, source
tool = stages / ('dv-tool-' + source)
slots = [stages / ('dv-' + key + '-' + source) for key in ('holo-api', 'holo-worker')]
receipt = store / ('transition-' + writer + '-' + source)
active = legacy / 'artifacts/worker-profile/active'
releases = active.with_name('releases')
quad = Path('/etc/containers/systemd/hololive-bot')
migrator = quad / 'hololive-db-migrate.container.d/50-migrator-image.conf'
observations = Path('/tmp/observations')


def save(path, value):
    path.write_text(json.dumps(value, sort_keys=True) + '\n')


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def snapshot():
    def info(path, pin=False):
        if not path.exists():
            return {'state': 'absent'}
        st = path.stat()
        out = {'inode': [st.st_dev, st.st_ino]}
        if path.is_dir():
            return dict(out, state='directory')
        if pin:
            return dict(out, state={runtime: 'running', source: 'new', writer: 'old'}[path.read_text().strip()])
        return dict(out, state='file', bytes=st.st_size, sha256=digest(path))

    names = ('revisions', 'changes.raw', 'previous.actual', 'previous.metadata', 'previous.after',
             'protected.before', 'protected.after', 'images', 'images.after')
    return dict(active=info(store / 'active', True), old=info(store / writer / '.source-revision', True),
                new=info(store / source / '.source-revision', True), legacy=info(legacy / '.source-revision', True),
                store=info(store), transition=info(receipt), revisions_match=receipt.exists(),
                receipts={k: info(receipt / k) for k in names})


def generation(peers, name):
    return ' '.join([name, *peers[name][:3], 'holo-pgbouncer|6432']) + '\n'


def observed_binds(peers):
    binds = {}
    for name, src, dest in [('holo-postgres', 'postgres/pg_hba.conf', '/run/hololive-bot/postgres/pg_hba.conf'),
                            ('holo-pgbouncer', 'pgbouncer/pg_hba.conf', '/etc/pgbouncer/pg_hba.conf'),
                            ('holo-pgbouncer', 'pgbouncer/stats-users.txt', '/etc/pgbouncer/stats-users.txt')]:
        proc = Path('/proc') / Path('/tmp/infra-pid').read_text().strip()
        host, live = legacy / 'deploy' / src, proc / 'root' / dest.lstrip('/')
        rows = [x.split() for x in (proc / 'mountinfo').read_text().splitlines() if x.split()[4] == dest]
        row, = rows
        assert 'ro' in row[5].split(',') and digest(host) == digest(live)
        assert {k: os.getxattr(host, k) for k in os.listxattr(host)} == {k: os.getxattr(live, k) for k in os.listxattr(live)}
        def metadata(p):
            st = p.stat()
            return [st.st_dev, st.st_ino, st.st_mode, st.st_uid, st.st_gid, st.st_size, st.st_nlink, st.st_mtime_ns, st.st_ctime_ns]
        binds[dest] = dict(host=metadata(host), mounted=metadata(live), mount_id=int(row[0]), cid=peers[name][1], equal_bytes=True, equal_xattrs=True)
    return binds


def configuration():
    paths = [active / (n + '.pair') for n in (api, worker)]
    paths += [quad / (n + '.container.d/50-worker-profile.conf') for n in (api, worker)] + [migrator]
    return {str(p): [digest(p), p.stat().st_ino, p.stat().st_ctime_ns] for p in paths}


def original_request(start, manifest, image, old, protected, source, prior):
    return dict(utc_ns=start, manifest=manifest, image=image, old=old, protected=protected, source=source,
                tool=source, prior=sorted(prior))


def original_exit(returncode, markers):
    return dict(rc=returncode, markers=markers)


def original_refusal(error, jobs, service, unit):
    return dict(outcome='outcome_unknown', error=type(error).__name__,
                reason=str(error) if type(error) is RuntimeError else 'original evidence incomplete',
                original_jobs=jobs, active=unit(service, 'ActiveState'), load=unit(service, 'LoadState'),
                invocation=unit(service, 'InvocationID'))


if phase == 'unreadable-refusal':
    from unittest.mock import patch
    supervisor = runpy.run_path(str(stages / ('dv-tool-' + next_revision) / 'source/scripts/deploy/supersession-window.py'))
    directory = Path('/case/unreadable')
    directory.mkdir()
    marker = directory / 'activation.confirmation-not-accepted'
    marker.touch()
    def unreadable(probe):
        def lookup(path, *args, **kwargs):
            if path == marker or path == str(marker):
                raise OSError(5, 'injected marker read failure')
            return probe(path, *args, **kwargs)
        return lookup
    with patch.object(os, 'stat', unreadable(os.stat)), patch.object(os, 'lstat', unreadable(os.lstat)):
        try:
            supervisor['empty'](directory, (marker.name,))
        except OSError as error:
            if error.errno == 5:
                sys.exit(75)
            raise
    sys.exit(0)

if phase == 'migrator-entry':
    service, = selectors
    verifier = runpy.run_path(str(store / next_revision / 'scripts/deploy/lib/unstarted-source.py'))
    pair = json.loads((stages / ('dv-holo-api-' + next_revision) / 'candidate-manifests.json').read_bytes())
    try:
        verifier['slot_migrator'](source, next_revision, service, None, pair, {}, 'entry')
    except verifier['Refusal'] as error:
        sys.exit(str(error))
    sys.exit(0)

if phase == 'before-source':
    slots[1].mkdir(mode=0o700)
    peers = {name: ['%032x' % i, '%064d' % (20 + i), '%064d' % (40 + i), 'healthy']
             for i, name in enumerate(('admin-dashboard-ingress-firewall', 'chatbotgo-bot', 'twentyq-bot',
                                       'chatbotgo-postgres', 'chatbotgo-pgbouncer', 'twentyq-postgres',
                                       'holo-postgres', 'holo-pgbouncer', api, worker), 1)}
    del peers['admin-dashboard-ingress-firewall'][1:]
    peers[api][1:3] = ['%064d' % 8, '%064d' % 2]
    peers[worker][1:3] = ['%064d' % 9, '%064d' % 5]
    images = {'%064d' % i: [n, rev] for i, n, rev in [(2, api, writer), (5, worker, writer),
              (3, api, source), (4, worker, source), (6, api, next_revision), (7, worker, next_revision)]}
    lineage = (tool / 'source/hololive/hololive-api/internal/migrationrunner/queries/lineage_head.sql').read_text()
    rows = sorted(re.findall(r"\('([^']+\.sql)','([a-f0-9]{64})'\)", lineage))
    db = sqlite3.connect('/tmp/ledger.db')
    db.executescript('''
        CREATE TABLE pg_class(oid TEXT PRIMARY KEY, relkind TEXT);
        CREATE TABLE pg_roles(oid TEXT PRIMARY KEY, rolname TEXT);
        CREATE TABLE pg_index(indisvalid INTEGER, indisready INTEGER, indislive INTEGER);
        CREATE TABLE pg_trigger(tgisinternal INTEGER, tgenabled TEXT, tgrelid TEXT, tgname TEXT, tgfoid TEXT);
        CREATE TABLE privilege(role TEXT, object TEXT, action TEXT);
    ''')
    for name in ('schema_migrations','schema_migration_checksums','member_schedule_card_templates','alarms',
                 'source_observations','source_observation_queue'):
        db.execute('INSERT INTO pg_class VALUES(?,?)', ('public.' + name, 'r'))
    for table,trigger in [('source_observations','reject_retired_holodex_channel_profile'),
                          ('source_observation_queue','terminalize_retired_holodex_channel_profile')]:
        proc = 'public.' + trigger + '()'
        db.execute('INSERT INTO pg_class VALUES(?,?)', (proc, 'p'))
        db.execute('INSERT INTO pg_trigger VALUES(0,"O",?,?,?)', ('public.' + table, trigger, proc))
    for role, grants in {'hololive_alarm_worker': [('alarms','SELECT'),('alarm_dispatch_events','INSERT'),('alarm_dispatch_events_id_seq','USAGE')],
                         'hololive_scraper': [('source_observations','INSERT'),('youtube_collection_job_leases','UPDATE')],
                         'hololive_runtime': [('source_observations','SELECT')]}.items():
        db.execute('INSERT INTO pg_roles VALUES(?,?)', (role, role))
        db.execute('INSERT INTO privilege VALUES(?,?,?)', (role, 'public', 'USAGE'))
        db.executemany('INSERT INTO privilege VALUES(?,?,?)', ((role,'public.' + obj,action) for obj,action in grants))
    logical, physical = [], []
    db.execute('create table schema_migrations(filename text primary key, tableoid int, ctid text, xmin int, applied_at text)')
    db.execute('create table schema_migration_checksums(filename text primary key, tableoid int, ctid text, xmin int, recorded_at text, checksum_sha256 text)')
    for i, (filename, checksum) in enumerate(rows, 1):
        ts = f'2026-01-01T00:00:{i:02}'
        ctid = f'(1,{i})'
        db.execute('insert into schema_migrations values(?,?,?,?,?)', (filename, 41, ctid, i, ts))
        db.execute('insert into schema_migration_checksums values(?,?,?,?,?,?)', (filename, 42, ctid, i, ts, checksum))
        logical.append(json.dumps(dict(filename=filename, applied_at=ts, recorded_at=ts, checksum_sha256=checksum), separators=(',', ':')))
        physical.append(json.dumps([41, 141, ctid, i, ts, 42, 142, ctid, i, ts]))
    db.commit()
    db.close()
    prefix = '|'.join([str(len(rows))]*3 + [hashlib.md5('\n'.join(n + ':' + c for n,c in rows).encode()).hexdigest(), ''])
    state = dict(peers=peers, images=images, started=time.time_ns() - 10_000_000_000,
                 ledger=prefix + hashlib.sha256('\n'.join(logical).encode()).hexdigest(),
                 versions={writer: (legacy/'hololive/hololive-api/VERSION').read_text().strip(), source: (tool/'source/VERSION').read_text().strip(),
                           next_revision: (stages/('dv-tool-' + next_revision)/'source/VERSION').read_text().strip()})
    save(observations, state)
    protected = dict(legacy=runtime, generations=peers, ledger=prefix + hashlib.sha256('\n'.join(physical).encode()).hexdigest(),
                     split_binds=observed_binds(peers), aliases={})
    for slot in slots:
        for name in ('protected.before', 'protected.after'):
            save(slot / name, protected)

if phase in ('before-source', 'stage-successor'):
    entries = []
    version = (tool / 'source/VERSION').read_text().strip()
    tag = 'v' + version
    tag_object = subprocess.check_output(['git', '-C', str(tool / 'source'), 'rev-parse', tag], text=True).strip() if successor else 'f' * 40
    release_spec = {}
    if successor:
        repo = tool / 'source'
        health = {}
        items = {}
        for label, title, number in [('holo-api', api, 6), ('holo-worker', worker, 7)]:
            urls = (['--api-key-env', 'API_SECRET_KEY', 'https://127.0.0.1:30001/internal/ready', 'https://127.0.0.1:30006/health']
                    if title == api else ['https://127.0.0.1:30007/health'])
            health[label] = dict(Test=['CMD', './bin/healthcheck', *urls], Interval=30000000000, Timeout=5000000000,
                                 StartPeriod=45000000000 if title == api else 5000000000, Retries=3)
            items[label] = dict(image='sha256:%064d' % number, ref='localhost/dv-' + label + ':' + source,
                                archive=hashlib.sha256((label + source).encode()).hexdigest(), health=health[label])
        coordinator = 'scripts/deploy/quadlet-redeploy-service.sh'
        blob = subprocess.check_output(['git', '-C', str(repo), 'rev-parse', source + ':' + coordinator], text=True).strip()
        parent = subprocess.check_output(['git', '-C', repo, 'show', '-s', '--format=%P', source], text=True).strip()
        release_spec = dict(version=version, tag=tag, tag_object=tag_object, parent=parent, revision=source, root=str(repo),
                            origin='https://github.com/park285/hololive-bot.git', source_base=writer, running_base=runtime,
                            publisher_sha256=digest(repo / 'scripts/deploy/sync-opt-current.sh'), coordinator_blob=blob, images=items)
        release_spec['reviewed_tree'] = subprocess.check_output(['git', '-C', str(repo), 'rev-parse', source + '^{tree}'], text=True).strip()
        (tool / 'stage.accepted').write_text(source + ' ' + tag_object + ' ' + digest(tool / 'source.bundle') + '\n')
        state = json.loads(observations.read_bytes())
        state['health'] = health
        save(observations, state)
    for slot, label, service, number in zip(slots, ('holo-api', 'holo-worker'), (api, worker), (6, 7) if successor else (3, 4), strict=True):
        image = 'sha256:%064d' % number
        manifest = slot / 'manifest.json'
        binaries = {('hololive-api' if service == api else 'alarm-worker'): {'sha256': digest(Path('/usr/bin/python3').resolve())}}
        body = dict(service=label, revision=source, base=writer, version=version, tag_object=tag_object, image=image, binaries=binaries)
        if successor:
            item = items[label]
            body.update(release_spec=release_spec, repo='hololive-bot', ref=item['ref'], archive=item['archive'],
                        bundle=digest(slot / 'source.bundle'), created='2026-11-19T00:00:00Z', roundtrip_verified=True,
                        docker_archive=dict(config=image, format='docker-archive', health=item['health'], rootfs_diff_ids=[image]))
        save(manifest, body)
        seal = digest(manifest)
        (slot / 'stage.accepted').write_text(seal + '\n')
        image_record = slots[0] / (service + '.candidate-image')
        image_record.write_text(image + '\n')
        image_record.chmod(0o400)
        entries.append(dict(service=service, image=image, manifest=seal, tag_object=tag_object))
    for name in ('pair-binding.requested', 'candidate-manifests.json'):
        p = slots[0] / name
        p.write_text(json.dumps(dict(revision=source, tag='v' + version, entries=entries), sort_keys=True, separators=(',', ':')) + '\n')
        p.chmod(0o400)
    if successor:
        sys.exit(0)
    save(tool / 'source.peers.before', peers)
    (tool / 'source.requested').write_text(datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='seconds') + '\n')
    save(tool / 'source.before.json', snapshot())
    sys.exit(0)

state = json.loads(observations.read_bytes())
peers = state['peers']
if phase == 'after-source':
    save(tool / 'source.coordinator.exit', dict(returncode=0, markers=[], ended_utc_ns=time.time_ns()))
    save(tool / 'source.after.json', snapshot())
    save(tool / 'source.peers.after', peers)
    (tool / 'source.accepted').write_text(str(receipt) + '\n')
    stamp = time.time_ns()
    os.utime(tool / 'source.accepted', ns=(stamp, stamp))

    selected = slots[0]
    prior = {p.name for p in releases.glob(api + '-v2-*')}
    protected = {n: g for n, g in peers.items() if n != api}
    protected.update(ledger=state['ledger'], protected_file_pairs=observed_binds(peers))
    seal = digest(selected / 'manifest.json')
    image = json.loads((selected / 'manifest.json').read_bytes())['image']
    coord = store / source / 'scripts/deploy/quadlet-redeploy-service.sh'
    for name in (api, worker):
        Path('/tmp/' + name + '.generation').write_text(generation(peers, name))

    def created_release():
        created, = {p for p in releases.glob(api + '-v2-*') if p.name not in prior}
        return created

    def job_variants():
        for job in ('schema-job', 'writer-job'):
            variant = Path('/tmp') / (job + '-case')
            shutil.copytree(created_release(), variant)
            (variant / job).mkdir()

    observer = Path('/case/activate-holo.py')
    if observer.exists():
        program = ast.parse(observer.read_bytes())
        functions = {node.name: node for node in program.body if isinstance(node, ast.FunctionDef)}
        body = functions['main'].body
        begin, = [i for i, stmt in enumerate(body) if isinstance(stmt, ast.Expr)
                  and ast.unparse(stmt.value).startswith("write(d / 'activation.config-before',")]
        scope = dict(os=os, sys=sys, re=re, json=json, Path=Path, time=time, subprocess=subprocess, hashlib=hashlib,
                     coord=coord, sealed=store/source, n=api, s='holo-api', rev=source, tool=source, legacy=legacy,
                     im=image, apiid=image, mhash=seal, old=peers[api], d=selected, settings=configuration(),
                     evidence=lambda _: protected)
        def execute(nodes):
            exec(compile(ast.Module(body=nodes, type_ignores=[]), str(observer), 'exec'), scope)
        execute([functions[key] for key in ('need', 'sha', 'run', 'unit', 'write')])
        original_write = scope['write']
        def write(path, value):
            if path.name == 'activation.requested':
                assert value == original_request(scope['start_ns'], seal, image, peers[api], protected, source, prior)
            elif path.name == 'coordinator.exit':
                assert value == original_exit(scope['rc'], scope['markers'])
            elif path.name == 'activation.not-accepted':
                assert value == original_refusal(scope['e'], scope['proof'], api, scope['unit'])
            original_write(path, value)
        scope['write'] = write
        try:
            execute(body[begin:])
        except RuntimeError as error:
            assert str(error) == 'coordinator refused or outcome unknown'
        else:
            sys.exit('original observer accepted the failed coordinator')
    else:
        save(selected / 'activation.config-before', configuration())
        save(selected / 'activation.requested', original_request(time.time_ns(), seal, image, peers[api], protected, source, prior))
        proc = subprocess.run([coord, api, image], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        outcomes = re.findall(r'^\[OUTCOME\] (\w+) service=hololive-api stage=([a-z-]+) release=([^:\n]+):', proc.stdout, re.M)
        marker, phase_name, path = outcomes.pop()
        assert not outcomes and path == str(created_release()) and 'required existing dependency is inactive: -.mount' in proc.stdout
        save(selected / 'coordinator.diagnostics', [dict(phase=phase_name)])
        save(selected / 'coordinator.exit', original_exit(proc.returncode, [marker]))
        jobs = {}
        for release in releases.glob(api + '-v2-*'):
            if release.name in prior:
                continue
            paths = [release / job / key for job in ('writer-job', 'schema-job') for key in ('request', 'anchor', 'invocation')]
            paths += [release / 'schema-check', release / 'accepted-writer']
            jobs[release.name] = {str(p.relative_to(release)): digest(p) for p in paths if p.is_file() and not p.is_symlink()}
        def unit(service, key):
            return subprocess.check_output(['systemctl', 'show', service + '.service', '-p', key, '--value'], text=True).strip()
        try:
            if proc.returncode or marker != 'deployed':
                raise RuntimeError('coordinator refused or outcome unknown')
        except RuntimeError as error:
            save(selected / 'activation.not-accepted', original_refusal(error, jobs, api, unit))
    job_variants()
    identities = []
    for name in ('activation.config-before', 'activation.requested', 'coordinator.diagnostics', 'coordinator.exit', 'activation.not-accepted'):
        path = selected / name
        info = path.stat()
        identities.append([name, digest(path), info.st_ino, info.st_ctime_ns, info.st_mtime_ns])
    (selected / 'original-receipts.sha256').write_text(hashlib.sha256(json.dumps(identities, separators=(',', ':')).encode()).hexdigest() + '\n')
    assert json.loads((selected / 'coordinator.exit').read_bytes()) == dict(rc=1, markers=['not_activated'])
    assert json.loads((selected / 'coordinator.diagnostics').read_bytes()) == [dict(phase='migration')]
    assert 'systemctl is-active --quiet -.mount' in Path('/events').read_text().splitlines()
    Path('/tmp/failed-release').write_text(created_release().name + '\n')
    sys.exit(0)

if phase != 'after-attempt':
    sys.exit('unexpected fixture phase')
start = json.loads((slots[0] / 'activation.requested').read_bytes())['utc_ns'] // 1000
end = (slots[0] / 'activation.not-accepted').stat().st_mtime_ns // 1000
boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip().replace('-', '')
state['journal'] = []
for i, stamp in enumerate((start - 31_000_000, start + 1, start + 100, max(time.time_ns() // 1000, end + 1)), 1):
    mono = time.monotonic_ns() // 1000 + stamp - time.time_ns() // 1000
    state['journal'].append(dict(_UID='1000' if i == 3 else '0', _PID='2112' if i == 3 else '1', _TRANSPORT='stdout' if i == 3 else 'journal', _SYSTEMD_UNIT='app.service' if i == 3 else 'init.scope',
                                PRIORITY='6', _BOOT_ID=boot, __REALTIME_TIMESTAMP=str(stamp), __MONOTONIC_TIMESTAMP=str(mono),
                                __CURSOR=f's={"a" * 32};i={i:x};b={boot};m={mono:x};t={stamp:x};x={i:x}',
                                UNIT='unrelated.service', MESSAGE_ID='39f53479d3a045ac8e11786248231fbf', JOB_TYPE='start', JOB_RESULT='done', JOB_ID=str(i)))
state['journal'][1] = {k:v for k,v in state['journal'][1].items() if not k.startswith('JOB_') and k not in ('UNIT','MESSAGE_ID')}
state['journal'][1]['MESSAGE'] = 'Reloading finished in 48 ms.'
bus = {k: [] for k in ('ExecCondition', 'ExecStart', 'ExecStartPre', 'ExecStartPost', 'ExecReload', 'ExecStop', 'ExecStopPost')}
section = ''
for line in Path('/run/systemd/generator/hololive-db-migrate.service').read_text().splitlines():
    if line.startswith('['):
        section = line
    key, _, value = line.partition('=')
    if section == '[Service]' and key in bus:
        if value:
            bus[key].append(shlex.split(value))
        else:
            bus[key].clear()
state['bus'] = bus
save(observations, state)
