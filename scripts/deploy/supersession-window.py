import ctypes
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import re
import select
import signal
import runpy
import socket
import stat
import subprocess
import sys
import time
from types import SimpleNamespace
import uuid


ROOT = Path('/opt/hololive-bot')
STAGES = ROOT / 'release-sources'
STORE = ROOT / 'compose/app-sources'
DEPLOY = Path('scripts/deploy')
SUPERVISOR = DEPLOY / 'supersession-window.py'
PUBLISHER = DEPLOY / 'sync-opt-current.sh'
COORDINATOR = DEPLOY / 'quadlet-redeploy-service.sh'
VERIFIER = DEPLOY / 'lib/unstarted-source.py'
SERVICES = ('hololive-api', 'hololive-alarm-worker')
ENV = {'HOME': '/root', 'PATH': '/usr/sbin:/usr/bin', 'LC_ALL': 'C'}
GIT_ENV = dict(ENV, GIT_NO_REPLACE_OBJECTS='1', GIT_CONFIG_NOSYSTEM='1', GIT_CONFIG_GLOBAL='/dev/null',
               GIT_ATTR_NOSYSTEM='1', GIT_OPTIONAL_LOCKS='0', GIT_TERMINAL_PROMPT='0')
SOURCE_RECORDS = ('source.requested', 'source.peers.before', 'source.before.json', 'source.coordinator.exit',
                  'source.after.json', 'source.peers.after', 'source.accepted', 'source.not-accepted', 'source.stdout.sha256')
SLOT_RECORDS = ('activation.config-before', 'activation.requested', 'coordinator.diagnostics', 'coordinator.exit',
                'activation.accepted', 'activation.not-accepted', 'activation.stdout.sha256')
FENCE = 19
PHASES = ('source', 'api', 'worker')
LIBC = ctypes.CDLL(None, use_errno=True)


def need(ok):
    if not ok:
        raise ValueError('복구 증거 불일치')


def oid(value, size):
    return isinstance(value, str) and re.fullmatch(r'[0-9a-f]{%d}' % size, value) and int(value, 16) != 0


def checked(p, directory):
    st = p.lstat()
    need((stat.S_ISDIR(st.st_mode) if directory else stat.S_ISREG(st.st_mode) and st.st_nlink == 1)
         and st.st_uid == st.st_gid == 0 and not st.st_mode & 0o7022
         and not any(k.startswith('system.posix_acl_') or k == 'security.capability'
                     for k in os.listxattr(p, follow_symlinks=False)))
    return st


def custody(p, directory=False):
    need(p.is_absolute() and '..' not in p.parts)
    for q in reversed(p.parents):
        checked(q, True)
    return checked(p, directory)


def clone_metadata(gd, tag):
    def entries(directory, files, directories):
        custody(directory, True)
        for p in directory.iterdir():
            if re.fullmatch(files, p.name):
                checked(p, False)
            else:
                need(re.fullmatch(directories, p.name))
                checked(p, True)
    entries(gd, r'HEAD|config|index|FETCH_HEAD|ORIG_HEAD|packed-refs|description', r'objects|refs|logs|hooks|info|branches')
    for name in ('info', 'hooks', 'branches', 'refs/heads'):
        if (gd / name).exists():
            entries(gd / name, r'(?!)', r'(?!)')
    entries(gd / 'refs', r'(?!)', r'heads|tags')
    entries(gd / 'refs/tags', re.escape(tag), r'(?!)')
    if (gd / 'logs').exists():
        entries(gd / 'logs', r'HEAD', r'(?!)')
    objects = gd / 'objects'
    entries(objects, r'(?!)', r'pack|info|[0-9a-f]{2}')
    for directory in objects.iterdir():
        pattern = {'pack': r'pack-[0-9a-f]{40}\.(pack|idx|rev)', 'info': r'(?!)'}.get(directory.name, r'[0-9a-f]{38}')
        entries(directory, pattern, r'(?!)')


def file_hash(path, kind='sha256'):
    initial = custody(path)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), 'rb') as stream:
        result = hashlib.file_digest(stream, kind).hexdigest()
        end = os.fstat(stream.fileno())
        need(os.path.samestat(end, initial) and os.path.samestat(end, path.lstat()) and end.st_ctime_ns == initial.st_ctime_ns)
    return result


def read(path):
    st = custody(path)
    need(0 < st.st_size <= 16 * 1024 * 1024)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), 'rb') as f:
        value = f.read(st.st_size + 1)
        actual = os.fstat(f.fileno())
        need(len(value) == st.st_size and os.path.samestat(st, actual) and os.path.samestat(actual, path.lstat())
             and actual.st_ctime_ns == st.st_ctime_ns)
        return value


def unique(items):
    data = dict(items)
    need(len(items) == len(data))
    return data


def decode(data):
    return json.loads(data, object_pairs_hook=unique)


def encoded(data):
    return (json.dumps(data, sort_keys=True, separators=(',', ':')) + '\n').encode()


def store(path, data, before_commit=None):
    custody(path.parent, True)
    atomic = path.suffix in ('.accepted', '.confirmed')
    target = path.with_name('.receipt-' + uuid.uuid4().hex) if atomic else path
    fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o400)
    with os.fdopen(fd, 'wb') as f:
        f.write(data if isinstance(data, bytes) else encoded(data))
        f.flush()
        stamp = time.time_ns()
        os.utime(f.fileno(), ns=(stamp, stamp))
        os.fsync(f.fileno())
    parent = os.open(path.parent, os.O_DIRECTORY | os.O_RDONLY | os.O_NOFOLLOW)
    try:
        if before_commit is not None:
            need(atomic)
            before_commit()
        if atomic and LIBC.renameat2(parent, os.fsencode(target.name), parent, os.fsencode(path.name), 1):
            raise OSError(ctypes.get_errno(), '수락 파일 공개 실패')
        os.fsync(parent)
    finally:
        os.close(parent)


def empty(directory, names):
    custody(directory, True)
    for name in names:
        try:
            (directory / name).lstat()
        except FileNotFoundError:
            continue
        raise ValueError('이미 사용된 복구 기록')


def command(*args, env=ENV):
    return subprocess.check_output(args, cwd='/', env=env, stdin=subprocess.DEVNULL,
                                   stderr=subprocess.DEVNULL, timeout=120).decode().rstrip('\n')


def git(src, *args):
    return command('/usr/bin/git', '-c', 'core.hooksPath=/dev/null', '-c', 'core.fsmonitor=false',
                   '-C', src, *args, env=GIT_ENV)


def clone(stage, spec, bundle_sum):
    src = stage / 'source'
    tag, rev, obj = 'refs/tags/' + spec['tag'], spec['revision'], spec['tag_object']
    need(file_hash(stage / 'source.bundle') == bundle_sum
         and git('/', 'bundle', 'list-heads', stage / 'source.bundle') == obj + ' ' + tag)
    gd = src / '.git'
    clone_metadata(gd, spec['tag'])
    config = git('/', 'config', '--file', gd / 'config', '--no-includes', '--list').splitlines()
    wanted = {'core.repositoryformatversion=0', 'core.filemode=true', 'core.bare=false', 'core.logallrefupdates=true'}
    need(len(set(config)) == len(config) and wanted <= set(config) <= wanted | {'tar.umask=0022'})
    need(git(src, 'rev-parse', '--show-toplevel') == str(src) and git(src, 'rev-parse', 'HEAD') == rev
         and git(src, 'rev-parse', tag) == obj and git(src, 'cat-file', '-t', obj) == 'tag'
         and git(src, 'rev-parse', tag + '^{commit}') == rev and git(src, 'show', '-s', '--format=%P', rev) == spec['parent']
         and git(src, 'rev-parse', rev + '^{tree}') == spec['reviewed_tree']
         and read(src / 'VERSION') == (spec['version'] + '\n').encode()
         and git(src, 'rev-parse', f'{rev}:{COORDINATOR}') == spec['coordinator_blob']
         and file_hash(src / PUBLISHER) == spec['publisher_sha256'])
    git(src, 'merge-base', '--is-ancestor', spec['source_base'], rev)
    sealed_tree(src, src, rev, clone=True)
    need(not git(src, 'status', '--porcelain', '--untracked-files=all', '--ignored'))
    return src


def sealed_tree(src, root, revision, clone=False):
    rows = git(src, 'ls-tree', '-rz', revision).split('\0')
    need(rows.pop() == '')
    expected = {}
    for row in rows:
        header, name = row.split('\t', 1)
        mode, kind, oid_ = header.split()
        need(kind == 'blob' and mode in ('100644', '100755') and oid(oid_, 40))
        expected[root / name] = (0o555 if mode == '100755' else 0o444, oid_)
    stamp = root / '.source-revision'
    if not clone:
        need(read(stamp) == (revision + '\n').encode() and stat.S_IMODE(custody(stamp).st_mode) == 0o400
             and stat.S_IMODE(custody(root, True).st_mode) == 0o500)
    directories = {parent for p in expected for parent in p.parents if parent.is_relative_to(root)}
    custody(root, True)
    def fail(error):
        raise error
    for base, dirs, files in os.walk(root, onerror=fail):
        if clone and Path(base) == root:
            dirs.remove('.git')
        for name in dirs:
            d = Path(base, name)
            need(d in directories)
            mode = stat.S_IMODE(checked(d, True).st_mode)
            need(clone or mode == 0o555)
        for name in files:
            p = Path(base, name)
            if not clone and p == stamp:
                continue
            mode, blob = expected.pop(p)
            st = checked(p, False)
            need(bool(st.st_mode & 0o111) == bool(mode & 0o111) if clone else stat.S_IMODE(st.st_mode) == mode)
            with os.fdopen(os.open(p, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), 'rb') as f:
                h = hashlib.sha1(b'blob %d\0' % st.st_size)
                while chunk := f.read(1024 * 1024):
                    h.update(chunk)
                actual = os.fstat(f.fileno())
                need(h.hexdigest() == blob and os.path.samestat(actual, st) and os.path.samestat(actual, p.lstat())
                     and actual.st_ctime_ns == st.st_ctime_ns)
    need(not expected)


def pin(src, path, revision, relative):
    custody(path)
    need(git(src, 'hash-object', '--no-filters', path) == git(src, 'rev-parse', f'{revision}:{relative}'))


def inspect_image(key, service, m):
    fields = '{{.Id}} {{.Os}}/{{.Architecture}} ' + ' '.join('{{index .Config.Labels "org.opencontainers.image.' + k + '"}}'
                                                            for k in ('title', 'version', 'revision', 'created'))
    expected = ' '.join([m['image'][7:], 'linux/arm64', service, m['version'], m['revision'], m['created']])
    need(m['ref'] == 'localhost/dv-' + key + ':' + m['revision'])
    for image in (m['image'], m['ref']):
        need(command('podman', 'image', 'inspect', '--format', fields, image) == expected)
    detail = command('podman', 'image', 'inspect', '--format', '{{.ManifestType}}|{{json .RootFS.Layers}}|{{json .Config.Healthcheck}}', m['image']).split('|')
    need(len(detail) == 3 and detail[0] == 'application/vnd.docker.distribution.manifest.v2+json'
         and decode(detail[1]) == m['docker_archive']['rootfs_diff_ids'] and decode(detail[2]) == m['docker_archive']['health'])


def inputs(old, new, running, seals, tool, stages):
    manifests = []
    entries = []
    for stage, slot, service, seal in zip(stages, ('api', 'worker'), SERVICES, seals, strict=True):
        key = 'holo-' + slot
        raw = read(stage / 'manifest.json')
        need(hashlib.sha256(raw).hexdigest() == seal and read(stage / 'stage.accepted') == (seal + '\n').encode())
        m = decode(raw)
        s = m['release_spec']
        need((m['service'], m['repo'], m['revision'], m['base']) == (key, 'hololive-bot', new, old)
             and (s['source_base'], s['running_base'], s['revision'], s['origin']) == (old, running, new, 'https://github.com/park285/hololive-bot.git')
             and (not manifests or s == manifests[0]['release_spec']) and oid(s['parent'], 40)
             and oid(s['tag_object'], 40) and s['tag_object'] == m['tag_object'] and oid(s['coordinator_blob'], 40)
             and oid(s['publisher_sha256'], 64) and s['tag'] in ('r' + s['version'], 'v' + s['version']) and s['version'] == m['version']
             and re.fullmatch(r'(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)', m['version'])
             and m['roundtrip_verified'] is True and re.fullmatch('sha256:[0-9a-f]{64}', m['image'])
             and oid(m['bundle'], 64) and oid(m['archive'], 64) and set(s['images']) == {'holo-api', 'holo-worker'}
             and m['docker_archive']['config'] == m['image'] and s['images'][key] == dict(
                 image=m['image'], archive=m['archive'], ref=m['ref'], health=m['docker_archive']['health'])
             and oid(m['binaries']['hololive-api' if slot == 'api' else 'alarm-worker']['sha256'], 64))
        inspect_image(key, service, m)
        clone(stage, s, m['bundle'])
        entries.append(dict(service=service, manifest=seal, image=m['image'], tag_object=m['tag_object']))
        manifests.append(m)
    need(entries[0]['image'] != entries[1]['image'])
    spec = manifests[0]['release_spec']
    binding = dict(revision=new, tag=spec['tag'], entries=entries)
    bindings = [(service + '.candidate-image', (e['image'] + '\n').encode()) for service, e in zip(SERVICES, entries, strict=True)]
    bindings += [(name, encoded(binding)) for name in ('candidate-manifests.json', 'pair-binding.requested')]
    for name, value in bindings:
        path = stages[0] / name
        need(stat.S_IMODE(custody(path).st_mode) == 0o400 and read(path) == value)
    bundle = read(tool / 'stage.accepted').decode().split()
    need(len(bundle) == 3 and bundle[:2] == [new, spec['tag_object']] and oid(bundle[2], 64)
         and read(tool / 'stage.accepted') == (' '.join(bundle) + '\n').encode())
    src = clone(tool, spec, bundle[2])
    for path in (SUPERVISOR, PUBLISHER, VERIFIER, DEPLOY / 'lib/sealed-app.sh', DEPLOY / 'lib/postgres-endpoint.sh',
                 DEPLOY / 'lib/worker-profile-pair.py'):
        pin(src, src / path, new, path)
    return manifests, binding, src


def observe(p, *, directory=False, versions=None):
    try:
        st = custody(p, directory)
        identity = [st.st_dev, st.st_ino]
        if directory:
            return dict(state='directory', inode=identity)
        if versions:
            return dict(state=versions.get(read(p), 'unexpected'), inode=identity)
        return dict(state='file', bytes=st.st_size, sha256=file_hash(p), inode=identity)
    except FileNotFoundError:
        return dict(state='absent')
    except (OSError, ValueError):
        return dict(state='unknown')


def snapshot(h, old, new, running, receipt):
    versions = {(v + '\n').encode(): k for k, v in [('old', old), ('new', new), ('running', running)]}
    versions[f'incomplete {old} -> {new}\n'.encode()] = 'incomplete'
    rows = {k: observe(receipt / k, directory=k == 'previous.actual') for k in h.RECOVERY_RECEIPTS}
    revisions = f'installed={old}\ncandidate={new}\ntool={new}\nrunning={running}\n'.encode()
    return dict(legacy=observe(h.LEGACY / '.source-revision', versions=versions), active=observe(STORE / 'active', versions=versions),
                old=observe(STORE / old / '.source-revision', versions=versions), new=observe(STORE / new / '.source-revision', versions=versions),
                store=observe(STORE, directory=True), transition=observe(receipt, directory=True), receipts=rows,
                revisions_match=rows['revisions'].get('sha256') == hashlib.sha256(revisions).hexdigest())


def death_guard(parent):
    def bind_parent():
        if LIBC.prctl(1, signal.SIGKILL, 0, 0, 0) != 0 or os.getppid() != parent:
            os._exit(75)
    return bind_parent


def manager_send(*args, coordinator_fd=None):
    parent = os.getpid()
    def guard():
        death_guard(parent)()
        if coordinator_fd is not None and select.select([coordinator_fd], [], [], 0)[0]:
            os._exit(75)
    inherited = () if coordinator_fd is None else (coordinator_fd,)
    with subprocess.Popen(args, cwd='/', stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                          env=ENV, preexec_fn=guard, pass_fds=inherited) as proc:
        try:
            client = os.pidfd_open(proc.pid)
            try:
                ready = select.select([client, *inherited], [], [], 120)[0]
                need(client in ready and coordinator_fd not in ready)
                output, _ = proc.communicate(timeout=2)
                need(proc.returncode == 0 and len(output) <= 65536)
                return output
            finally:
                os.close(client)
        finally:
            proc.kill()


def invoke(args, line_observer, timeout=1800, dispatch=None, deploy_fd=None):
    channel, peer = socket.socketpair(socket.AF_UNIX, socket.SOCK_SEQPACKET)
    try:
        return supervise(args, line_observer, timeout, dispatch, channel, peer, deploy_fd)
    finally:
        peer.close()
        channel.close()


def supervise(args, line_observer, timeout, dispatch, channel, peer, deploy_fd):
    deadline = time.monotonic() + timeout
    env = dict(ENV, HOLOLIVE_MANAGER_FD=str(peer.fileno())) if dispatch else ENV
    fds = (FENCE, peer.fileno()) if dispatch else (FENCE,)
    if deploy_fd is not None:
        env = dict(env, HOLOLIVE_DEPLOY_FD=str(deploy_fd))
        fds += (deploy_fd,)
    proc = subprocess.Popen(args, cwd='/', stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                            env=env, pass_fds=fds, start_new_session=True, preexec_fn=death_guard(os.getpid()))
    peer.close()
    pidfd = os.pidfd_open(proc.pid)
    def alive():
        need(not select.select([pidfd], [], [], 0)[0] and proc.poll() is None)
        if deploy_fd is not None:
            need(os.path.samestat(os.fstat(deploy_fd), custody(Path('/run/lock/hololive-quadlet-redeploy.lock'))))
    def send(*args):
        alive()
        return manager_send(*args, coordinator_fd=pidfd)
    watched = [proc.stdout, channel, pidfd] if dispatch else [proc.stdout]
    pending = b''
    try:
        while True:
            remaining = deadline - time.monotonic()
            ready = select.select(watched, [], [], max(0, remaining))[0]
            if remaining <= 0 or not ready:
                raise TimeoutError('coordinator output deadline')
            if pidfd in ready:
                if channel in watched:
                    need(not select.select([channel], [], [], 0)[0] or not os.read(channel.fileno(), 4097))
                    watched.remove(channel)
                watched.remove(pidfd)
                if proc.stdout not in ready:
                    continue
            if channel in ready and channel in watched:
                packet = os.read(channel.fileno(), 4097)
                if packet:
                    need(len(packet) <= 4096)
                    action, witness = decode(packet)
                    alive()
                    reply = encoded(dispatch(action, witness, deadline - time.monotonic(), alive, send))
                    need(os.write(channel.fileno(), reply) == len(reply))
                else:
                    watched.remove(channel)
                if proc.stdout not in ready:
                    continue
            block = os.read(proc.stdout.fileno(), 65536)
            if not block:
                if pending:
                    line_observer(pending.decode(errors='replace'))
                result = proc.wait(timeout=max(0, deadline - time.monotonic()))
                if time.monotonic() > deadline:
                    raise TimeoutError('coordinator exit deadline')
                return result
            *lines, pending = (pending + block).split(b'\n')
            need(len(pending) <= 1048576)
            for line in lines:
                line_observer(line.decode(errors='replace'))
    except BaseException:
        try:
            os.killpg(proc.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        try:
            proc.wait(timeout=2)
        except subprocess.TimeoutExpired:
            pass
        raise
    finally:
        os.close(pidfd)
        proc.stdout.close()


def source_precheck(h, old, new, running, failed):
    receipt = STORE / f'transition-{old}-{new}'
    empty(STORE, (new,))
    options = command('mv', '--help')
    need('--exchange' in options and '--no-copy' in options)
    before = snapshot(h, old, new, running, receipt)
    need([before[k]['state'] for k in ('active', 'old', 'new', 'legacy', 'store', 'transition')] ==
         ['old', 'old', 'absent', 'running', 'directory', 'absent'] and all(r == {'state': 'absent'} for r in before['receipts'].values()))
    before['manager_jobs'] = h.job_boundary()
    before['journal_cursor'] = before['manager_jobs']['cursor']
    writers = h.prove(old, running, failed)
    h.verify_backup(running, old)
    old_image = h.manifest_pair(old, STORE / f'transition-{running}-{old}', running)[1][h.API]
    before['migrator_commands'] = h.migrator_witness(STORE / old, old_image)
    peers = {service: h.running_generation(service) for service in sorted(h.PEERS)}
    need(peers == decode(read(STAGES / ('dv-tool-' + old) / 'source.peers.after')))
    return receipt, before, writers, peers, old_image


def publish(h, old, new, running, tool, stages, pair, src, checked_source, failed):
    receipt, before, writers, peers, old_image = checked_source
    store(tool / 'source.peers.before', peers)
    store(tool / 'source.before.json', before)
    markers = []
    def outcome(line):
        if line.startswith('[OUTCOME]'):
            markers.append({'result': 'outcome_unknown' if line.startswith('[OUTCOME] outcome_unknown ') else 'unexpected-outcome'})
        elif line.startswith('[sync-opt-current] ERROR:'):
            markers.append({'result': 'publisher-refused'})
    rc = invoke(['/usr/bin/bash', str(src / PUBLISHER), '--supersede-unstarted', old, new, new,
                 str(stages[0] / 'source'), running, failed], outcome)
    store(tool / 'source.coordinator.exit', dict(returncode=rc, markers=markers, ended_utc_ns=time.time_ns()))
    after = snapshot(h, old, new, running, receipt)
    try:
        after['migrator_commands'] = h.migrator_witness(STORE / old, old_image)
        after['manager_jobs'] = h.job_boundary()
        after['journal_cursor'] = after['manager_jobs']['cursor']
        h.journal_segment(before['journal_cursor'], after['journal_cursor'], {}, population=(before['manager_jobs'], after['manager_jobs']))
    finally:
        store(tool / 'source.after.json', after)
    observed = {service: h.running_generation(service) for service in sorted(h.PEERS)}
    store(tool / 'source.peers.after', observed)
    need(rc == 0 and not markers and observed == peers and after['revisions_match']
         and all(after[k] == before[k] for k in ('store', 'legacy', 'old', 'migrator_commands'))
         and [after[k]['state'] for k in ('new', 'active', 'transition')] == ['new', 'new', 'directory']
         and all(r['state'] == ('directory' if k == 'previous.actual' else 'file') and (k == 'previous.actual' or r['bytes'] > 0)
                 for k, r in after['receipts'].items()))
    for first, last in [('previous.metadata', 'previous.after'), ('protected.before', 'protected.after'), ('images', 'images.after'), ('pairs.before', 'pairs.after')]:
        need(read(receipt / first) == read(receipt / last))
    h.exact(receipt / 'predecessor.writers', '\n'.join(writers))
    h.exact(receipt / 'previous.actual/.source-revision', old)
    need(h.manifest_pair(new, receipt, old)[0] == pair)
    for e in pair['entries']:
        stage = stages[SERVICES.index(e['service'])]
        raw = read(stage / 'manifest.json')
        need(hashlib.sha256(raw).hexdigest() == e['manifest'])
        manifest = decode(raw)
        inspect_image(manifest['service'], e['service'], manifest)
    store(tool / 'source.accepted', (str(receipt) + '\n').encode())


def configuration(h):
    paths = [h.PROFILES / 'active' / (n + '.pair') for n in SERVICES]
    paths += [h.QUADLET / (n + '.container.d/50-worker-profile.conf') for n in SERVICES]
    paths.append(h.QUADLET / (h.MIGRATOR + '.container.d/50-migrator-image.conf'))
    items = {}
    for p in paths:
        data, st = h.record(p)
        items[str(p)] = [h.digest(data), st.st_ino, st.st_ctime_ns]
    return items


def release_names(h, service):
    custody(h.RELEASES, True)
    result = sorted(n for n in os.listdir(h.RELEASES) if n.startswith(service + '-v2-'))
    for name in result:
        need(h.release_name(service, name))
        custody(h.RELEASES / name, True)
    return result


def activate(h, context, index, old, new, stage, manifests, coord, deploy_fd):
    service, peer = SERVICES[index], SERVICES[1 - index]
    _, peers, protected, pair, _ = context
    original = h.running_generation(service)
    need(original == peers[service])
    others = {n: h.running_generation(n) for n in sorted(h.PEERS - {service})}
    need(all(others[n] == peers[n] for n in others if n != peer or not index))
    others.update(ledger=protected['ledger'], protected_file_pairs=protected['protected_file_pairs'])
    before = configuration(h)
    store(stage / 'activation.config-before', before)
    prior = release_names(h, service)
    entry = pair['entries'][index]
    request = dict(utc_ns=time.time_ns(), manifest=entry['manifest'], image=entry['image'], old=original,
                   protected=others, source=new, tool=new, prior=prior)
    store(stage / 'activation.requested', request)
    h.slot(old, new, service)
    markers, diagnostics, success = [], [], []
    def outcome(line):
        if not line.startswith('[OUTCOME]'):
            return
        match = re.match(r'^\[OUTCOME\] (outcome_unknown|not_activated|failed_after_activation|deployed) service=' + service + r'(?: |$)', line)
        markers.append(match[1] if match else 'unexpected')
        phase = re.search(r' stage=([a-z-]+) ', line)
        if phase:
            diagnostics.append(dict(phase=phase[1]))
        if match and match[1] == 'deployed':
            prefix = f"[OUTCOME] deployed service={service} revision={new} image={entry['image']} release="
            success.append(line[len(prefix):] if line.startswith(prefix) else '')
    actions = iter(('reload-schema', 'schema', 'reload-writer', 'writer'))
    transition = STORE / f'transition-{old}-{new}'
    inventory = h.raw(transition / 'protected.after')
    source_after = decode(read(location('source', new)[0] / 'source.after.json'))
    epoch = (decode(read(location('api',new)[0] / 'activation.accepted')) if index else source_after)['manager_jobs']['reload']
    snapshot = h.raw(transition / 'pairs.after').decode().splitlines()
    previous_pair = Path(snapshot[index*3].split()[1])
    peer_pair = Path(h.raw(h.PROFILES / 'active' / (peer + '.pair')).decode().strip())
    migrator_image = pair['entries'][0]['image']
    old_migrator = (h.STORE / new, migrator_image) if index else (h.STORE / old, decode(h.raw(h.stage_for(h.API, old) / 'activation.requested'))['image'])
    def dispatch(action, witness, remaining, alive, send):
        with h.bounded(remaining,'manager dispatch deadline exceeded'):
            return perform(action, witness, alive, send)
    def perform(action, witness, alive, send):
        nonlocal epoch
        need(next(actions) == action)
        release = Path(h.raw(transition / ('attempt-' + service) / 'release').decode().strip())
        need(release.parent == h.RELEASES and h.release_name(service, release.name))
        writers = {service: original, peer: others[peer]}
        def definitions(after=False):
            switching = action == 'reload-writer'
            selected = action == 'writer' or switching and after
            candidate_pair = release / 'candidate'
            h.writer_definition(service, new, entry['image'] if selected else 'sha256:'+original[2],
                                candidate_pair if selected else previous_pair, inventory,
                                candidate_pair if selected or switching else previous_pair)
            h.writer_definition(peer, new, 'sha256:'+others[peer][2], peer_pair, inventory)
            loaded = old_migrator if action == 'reload-schema' and not after else (h.STORE/new, migrator_image)
            need(h.migrator_witness(*loaded, installation=(h.STORE/new, migrator_image) if action == 'reload-schema' and not after else None)
                 == source_after['migrator_commands'])
        def prepare():
            h.pre_request(witness, writers, definitions, epoch, alive, changed_overlay=action == 'reload-schema')
        if action.startswith('reload-'):
            prepare()
            send('systemctl', 'daemon-reload')
            changed = h.reload_epoch()
            need(changed > epoch)
            definitions(after=True)
            need(h.reload_epoch() == changed)
            store(release / action, dict(before=epoch, after=changed))
            epoch = changed
            return 'reloaded'
        schema = action == 'schema'
        name = h.MIGRATOR if schema else service
        directory = release / (action + '-job')
        h.raw(directory / 'request')
        return h.recovery_job('start' if schema else 'try-restart', name + '.service', directory, prepare, send)
    rc = invoke([str(coord), service, entry['image'], *([pair['entries'][0]['image']] if index else [])], outcome, dispatch=dispatch, deploy_fd=deploy_fd)
    need(rc != 0 or next(actions, None) is None)
    store(stage / 'coordinator.diagnostics', diagnostics)
    store(stage / 'coordinator.exit', dict(rc=rc, markers=markers))
    need(rc == 0 and markers == ['deployed'] and not diagnostics)
    present = set(release_names(h, service))
    created = present - set(prior)
    need(len(created) == 1 and set(prior) <= present)
    release = h.RELEASES / created.pop()
    need(success == [str(release)])
    need(h.slot_context(old, new) == context and h.running_generation(peer) == others[peer])
    need(decode(read(stage / 'activation.config-before')) == before and decode(read(stage / 'activation.requested')) == request)
    accepted = h.accepted_slot(new, service, request, pair, release, h.job_boundary())
    check = subprocess.check_output([str(coord), '--rollback', str(release), '--check'], cwd='/', stdin=subprocess.DEVNULL,
                                    env=dict(ENV, HOLOLIVE_DEPLOY_FD=str(deploy_fd)), pass_fds=(deploy_fd,), timeout=180).decode().rstrip('\n')
    need(check.splitlines()[-1] == f'[CHECK] {service} previous pair verified: {release}/previous')
    need(context == h.slot_context(old, new) and h.running_generation(peer) == others[peer]
         and accepted['coordinator'] == manifests[index]['release_spec']['coordinator_blob'])
    for e, m in zip(pair['entries'], manifests, strict=True):
        inspect_image(m['service'], e['service'], m)
    need(accepted == h.accepted_slot(new, service, request, pair, release, accepted['manager_jobs'])
         and accepted['proof']['migrator_overlay'] == h.overlay_identity())
    latest = h.job_boundary()
    h.journal_segment(accepted['journal_cursor'], latest['cursor'], {}, population=(accepted['manager_jobs'], latest))
    accepted.update(journal_cursor=latest['cursor'], manager_jobs=latest)
    store(stage / 'activation.accepted', accepted)


def location(phase, new):
    need(phase in PHASES and oid(new, 40))
    if phase == 'source':
        return STAGES / ('dv-tool-' + new), 'source'
    return STAGES / ('dv-holo-' + phase + '-' + new), 'activation'


def original_identity(old, new, running, failed=None):
    data = decode(read(location('source', new)[0] / 'source.window'))
    nonce = data.pop('nonce')
    need(oid(nonce, 32) and set(data) == {'old', 'new', 'running', 'failed', 'boot', 'manifests'}
         and (data['old'], data['new'], data['running']) == (old, new, running) and (failed is None or failed == data['failed'])
         and re.fullmatch(r'hololive-api-v2-\d{8}T\d{6}Z-[A-Za-z0-9]+', data['failed'])
         and isinstance(data['manifests'], list) and len(set(data['manifests'])) == len(data['manifests']) == 2
         and all(oid(v, 64) for v in data['manifests']) and str(uuid.UUID(data['boot'])) == data['boot'])
    return data, nonce


def outcome_receipt(phase, identity, nonce):
    directory, prefix = location(phase, identity['new'])
    empty(directory, (prefix + '.not-accepted',))
    window = directory / (prefix + '.window')
    need(decode(read(window)) == dict(identity, nonce=nonce, **({} if phase == 'source' else {'phase': phase})))
    response = dict(identity=identity, phase=phase, window_nonce=nonce, window_sha256=file_hash(window),
                    accepted_sha256=file_hash(directory / (prefix + '.accepted')))
    if phase == 'source':
        response['snapshot_sha256'] = file_hash(directory / 'source.after.json')
    return response


def reserve_transition(transition, phase, incoming):
    name = phase + '.requested'
    empty(transition, (name,))
    store(transition / name, incoming)


def confirmation_response(transport, nonce):
    need(oid(nonce, 32))
    return dict(confirmed='worker', worker_transport_sha256=hashlib.sha256(transport).hexdigest(), output_nonce=nonce)


def receipt_originals(transition, stage, incoming):
    need(read(transition / 'receipt.requested') == incoming and read(stage / 'activation.confirmation.requested') == incoming)


def ack_path(phase, new):
    if phase == 'worker':
        return location('worker', new)[0] / 'activation.confirmed'
    following = PHASES[PHASES.index(phase) + 1]
    return location(following, new)[0] / 'activation.transport'


def transport_input(ack, phase, approved):
    data = decode(ack)
    need(encoded(data) == ack and type(data['process_exit']) is int and data.pop('process_exit') == 0)
    identity = data['identity']
    boot = identity['boot']
    need(str(uuid.UUID(boot)) == boot and identity == dict(approved, boot=boot) and data['phase'] == phase)
    hashes = {'window_sha256', 'accepted_sha256'} | ({'snapshot_sha256'} if phase == 'source' else set())
    need(set(data) == hashes | {'identity','phase','window_nonce','output_nonce'} and
         all(oid(data[k], 64) for k in hashes) and all(oid(data[k], 32) for k in ('window_nonce','output_nonce')))
    directory, prefix = location(phase, identity['new'])
    checksum = hashlib.sha256(encoded(data)).hexdigest()
    need(read(directory / (prefix + '.stdout.sha256')) == (checksum + '\n').encode())
    data.pop('output_nonce')
    return data


def check_ack(ack, phase, identity, nonce):
    need(transport_input(ack, phase, identity) == outcome_receipt(phase, identity, nonce))


def verify_transport(phase, old, new, running, failed=None):
    identity, nonce = original_identity(old, new, running, failed)
    ack = read(ack_path(phase, new))
    if phase == 'worker':
        confirmed = decode(ack)
        need(set(confirmed) == {'transport','manager_jobs','response'})
        boundary = confirmed['manager_jobs']
        directory, _ = location(phase, new)
        worker = decode(read(directory / 'activation.accepted'))['manager_jobs']
        need(set(boundary) == set(worker) and boundary['version'] == worker['version'] and boundary['reload'] == worker['reload'] and
             type(boundary['installed']) is int and boundary['installed'] >= worker['installed'] and
             isinstance(boundary['pending'], list) and all(type(n) is int for n in boundary['pending']) and
             isinstance(boundary['cursor'], str) and boundary['cursor'])
        ack = encoded(confirmed['transport'])
        receipt_originals(STORE / f'transition-{old}-{new}', directory, ack)
        need(confirmed['response'] == confirmation_response(ack, confirmed['response']['output_nonce']))
    else:
        next_phase = PHASES[PHASES.index(phase) + 1]
        need(read(STORE / f'transition-{old}-{new}' / (next_phase + '.requested')) == ack)
    check_ack(ack, phase, identity, nonce)
    if phase == 'source':
        directory, _ = location(phase, new)
        snapshot_rows = decode(read(directory / 'source.after.json'))['receipts']
        transition = STORE / f'transition-{old}-{new}'
        for name in ('pairs.before', 'pairs.after'):
            current = observe(transition / name)
            need(current['state'] == 'file' and current == snapshot_rows[name])
    if phase == 'worker':
        empty(location('worker', new)[0], ('activation.confirmation-not-accepted',))


def verify_transport_chain(old, new, running):
    for phase in PHASES:
        verify_transport(phase, old, new, running)


def boot_id():
    p = Path('/proc/sys/kernel/random/boot_id')
    mountpoints = [Path(row.split()[4]) for row in Path('/proc/self/mountinfo').read_text().splitlines()]
    need(p.resolve(strict=True) == p and not any(x != Path('/proc') and x.is_relative_to('/proc') and
                                                 p.is_relative_to(x) for x in mountpoints))
    with os.fdopen(os.open(p, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as f:
        buf = ctypes.create_string_buffer(256)
        need(LIBC.fstatfs(f.fileno(), buf) == 0 and ctypes.c_long.from_buffer(buf).value == 0x9fa0)
        return str(uuid.UUID(f.read(64).decode().strip()))


def verify_dispatch(phase, old, new, running, failed=None):
    identity, nonce = original_identity(old, new, running, failed)
    need(os.geteuid() == os.getegid() == 0 and identity['boot'] == boot_id())
    directory, prefix = location(phase, new)
    need(decode(read(directory / (prefix + '.window'))) ==
         dict(identity, nonce=nonce, **({} if phase == 'source' else {'phase': phase})))
    empty(directory, (prefix + '.accepted', prefix + '.not-accepted', 'source.coordinator.exit' if phase == 'source' else 'coordinator.exit'))
    for previous in PHASES[:PHASES.index(phase)]:
        verify_transport(previous, old, new, running, failed)
    tool = location('source', new)[0] / 'source'
    if phase == 'source':
        child_args = [str(tool / PUBLISHER), '--supersede-unstarted', old, new, new,
                      str(location('api', new)[0] / 'source'), running, identity['failed']]
    else:
        images = []
        for name, digest in zip(('api', 'worker'), identity['manifests'], strict=True):
            m = read(location(name, new)[0] / 'manifest.json')
            need(hashlib.sha256(m).hexdigest() == digest)
            images.append(decode(m)['image'])
        index = int(phase == 'worker')
        child_args = [str(STORE / new / COORDINATOR), SERVICES[index], images[index], *([images[0]] if index else [])]
    parent_args = ['-ISB', str(tool / SUPERVISOR), phase, old, new, running, identity['failed'], *identity['manifests']]
    opt = custody(Path('/opt'), True)
    need(os.path.samestat(opt, os.fstat(FENCE)))
    def process(pid):
        proc = Path('/proc', str(pid))
        fields = dict(line.split(':', 1) for line in (proc / 'status').read_text().splitlines())
        need(fields['Uid'].split() == fields['Gid'].split() == ['0'] * 4)
        argv = (proc / 'cmdline').read_bytes().split(b'\0')
        need(argv.pop() == b'')
        return proc, int(fields['PPid']), [os.fsdecode(v) for v in argv]
    child = os.getppid()
    proc, parent, args = process(child)
    need((proc / 'exe').samefile('/usr/bin/bash') and args[1:] == child_args)
    handle = os.pidfd_open(parent)
    try:
        supervisor, _, args = process(parent)
        need((supervisor / 'exe').samefile('/usr/bin/python3') and args[1:] == parent_args
             and os.path.samestat(opt, (supervisor / 'fd' / str(FENCE)).stat()))
        pattern = rf'lock:\s+\d+: FLOCK\s+ADVISORY\s+WRITE\s+{parent}\s+\S+:{opt.st_ino} 0 EOF'
        for fdinfo in (Path('/proc/self/fdinfo'), proc / 'fdinfo', supervisor / 'fdinfo'):
            rows = (fdinfo / str(FENCE)).read_text().splitlines()
            need(len([r for r in rows if re.fullmatch(pattern, r)]) == 1)
        need(not select.select([handle], [], [], 0)[0] and os.getppid() == child and process(child)[1] == parent)
    finally:
        os.close(handle)


def main():
    phase, old, new, running, failed, *seals = sys.argv[1:]
    need(phase in (*PHASES, 'receipt', 'receipt-check', 'precheck') and len(seals) == len(set(seals)) == 2 and all(oid(v, 64) for v in seals)
         and len({old, new, running}) == 3 and all(oid(v, 40) for v in (old, new, running))
         and re.fullmatch(r'hololive-api-v2-\d{8}T\d{6}Z-[a-zA-Z0-9]+', failed)
         and os.getuid() == os.geteuid() == os.getgid() == os.getegid() == 0 and socket.gethostname().split('.')[0] == 'hololive-seoul'
         and sys.flags.isolated and sys.flags.no_site and sys.flags.dont_write_bytecode)
    os.environ.clear()
    os.environ.update(ENV)
    os.chdir('/')
    os.umask(0o077)
    tool = STAGES / ('dv-tool-' + new)
    stages = [STAGES / ('dv-holo-' + slot + '-' + new) for slot in ('api', 'worker')]
    need(Path(__file__) == tool / 'source' / SUPERVISOR)
    custody(Path(__file__))
    incoming = sys.stdin.buffer.read(4097)
    need(len(incoming) <= 4096)
    outer = Path('/opt')
    custody(outer, True)
    lock = os.open(outer, os.O_DIRECTORY | os.O_RDONLY | os.O_NOFOLLOW)
    if lock != FENCE:
        need(fcntl.fcntl(lock, fcntl.F_DUPFD_CLOEXEC, FENCE) == FENCE)
        os.close(lock)
    failure = None
    deploy_fd = None
    try:
        fcntl.flock(FENCE, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if phase == 'receipt-check':
            identity, _ = original_identity(old, new, running, failed)
            need(identity['manifests'] == seals)
            verify_transport_chain(old, new, running)
            need(encoded(decode(read(ack_path('worker', new)))['response']) == incoming)
            return
        directory = location({'receipt':'worker','precheck':'source'}.get(phase, phase), new)[0]
        need(stat.S_IMODE(custody(directory, True).st_mode) == 0o700)
        identity = dict(old=old, new=new, running=running, failed=failed, manifests=seals)
        pending = (*SLOT_RECORDS, 'activation.window', 'activation.transport', 'activation.confirmed',
                   'activation.confirmation.requested', 'activation.confirmation-not-accepted')
        window = tool / 'source.window'
        if phase in ('source','precheck'):
            need(incoming == b'')
            reservation = STAGES / f'source-{old}-{new}.requested'
            empty(STAGES, (reservation.name,))
            empty(tool, (*SOURCE_RECORDS, 'source.window'))
        else:
            previous = {'api': 'source', 'worker': 'api', 'receipt': 'worker'}[phase]
            response = transport_input(incoming, previous, identity)
            identity, nonce = response['identity'], response['window_nonce']
            index = int(phase != 'api')
            stage = stages[index]
            transition = STORE / f'transition-{old}-{new}'
            lock_path = Path('/run/lock/hololive-quadlet-redeploy.lock')
            lock_info = custody(lock_path)
            deploy_fd = os.open(lock_path, os.O_RDONLY | os.O_NOFOLLOW)
            fcntl.flock(deploy_fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            need(os.path.samestat(lock_info, os.fstat(deploy_fd)) and os.path.samestat(lock_info, custody(lock_path)))
            reserve_transition(transition, phase, incoming)
            if phase == 'receipt':
                empty(stage, ('activation.confirmation.requested', 'activation.confirmed', 'activation.confirmation-not-accepted'))
                failure = stage / 'activation.confirmation-not-accepted'
                store(stage / 'activation.confirmation.requested', incoming)
            else:
                empty(stage, pending)
                failure = stage / 'activation.not-accepted'
                store(stage / 'activation.window', dict(identity, nonce=nonce, phase=phase))
                store(ack_path(previous, new), incoming)
            need(original_identity(old, new, running, failed) == (identity, nonce) and identity['boot'] == boot_id())
            need(response == outcome_receipt(previous, identity, nonce))
            for earlier in PHASES[:PHASES.index(previous)]:
                verify_transport(earlier, old, new, running, failed)
        for d in [STAGES, tool, *stages]:
            need(stat.S_IMODE(custody(d, True).st_mode) == 0o700)
            empty(d, ('stage.incomplete',))
        for later in (stages if phase in ('source','precheck') else stages[1:] if phase == 'api' else ()):
            empty(later, pending)
        manifests, pair, src = inputs(old, new, running, seals, tool, stages)
        sealed_tree(src, STORE / old, old)
        if phase in ('source','precheck'):
            h = SimpleNamespace(**runpy.run_path(str(src / VERIFIER)))
            checked_source = source_precheck(h, old, new, running, failed)
            if phase == 'precheck':
                print('source precheck passed')
                return
            empty(STAGES, (reservation.name,))
            empty(tool, (*SOURCE_RECORDS, 'source.window'))
            store(reservation, dict(identity, boot=boot_id()))
            failure = tool / 'source.not-accepted'
            store(tool / 'source.requested', (datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='seconds') + '\n').encode())
            identity['boot'] = boot_id()
            nonce = uuid.uuid4().hex
            store(window, dict(identity, nonce=nonce))
            publish(h, old, new, running, tool, stages, pair, src, checked_source, failed)
        else:
            hroot = STORE / new
            need(stat.S_IMODE(custody(hroot, True).st_mode) == 0o500 and read(STORE / 'active') == (new + '\n').encode())
            sealed_tree(src, hroot, new)
            h = SimpleNamespace(**runpy.run_path(str(hroot / VERIFIER)))
            context = h.slot_context(old, new)
            writer, peers, protected, confirmed, earliest = context
            need(writer == running and confirmed == pair)
            incident = decode(read(STAGES / ('dv-holo-api-' + old) / 'activation.not-accepted'))
            need(set(incident['original_jobs']) == {failed})
            if index:
                api_generation, earliest = h.completed_api(old, new, peers, protected, confirmed, earliest)
            if phase == 'receipt':
                for earlier in PHASES[:-1]:
                    verify_transport(earlier, old, new, running, failed)
                empty(stage, ('activation.confirmed', 'activation.confirmation-not-accepted'))
                need(read(stage / 'activation.confirmation.requested') == incoming)
                check_ack(incoming, previous, identity, nonce)
                current = h.worker_protection(peers, protected, api_generation)
                accepted, _ = h.completed_slot(old, new, h.WORKER, peers, current, pair, earliest)
                prior = h.job_boundary()
                worker_boundary = accepted['manager_jobs']
                h.journal_segment(worker_boundary['cursor'], prior['cursor'], {}, population=(worker_boundary, prior))
            else:
                try:
                    activate(h, context, index, old, new, stage, manifests, hroot / COORDINATOR, deploy_fd)
                finally:
                    os.close(deploy_fd)
                    deploy_fd = None
        if phase == 'receipt':
            response = confirmation_response(incoming, uuid.uuid4().hex)
            output = encoded(response)
        else:
            output = encoded(dict(outcome_receipt(phase, identity, nonce), output_nonce=uuid.uuid4().hex))
            directory, prefix = location(phase, new)
            store(directory / (prefix + '.stdout.sha256'), (hashlib.sha256(output).hexdigest() + '\n').encode())
        need(os.write(sys.stdout.fileno(), output) == len(output))
        if phase == 'receipt':
            writers = {h.API: api_generation, h.WORKER: accepted['generation']}
            h.ready_writers(new, writers)
            deadline = time.monotonic() + 30
            latest = h.job_boundary()
            h.journal_segment(prior['cursor'], latest['cursor'], {}, population=(prior, latest))
            h.ready_writers(new, writers)
            final = h.job_boundary()
            h.journal_segment(latest['cursor'], final['cursor'], {}, population=(latest, final))
            def confirm():
                h.ready_writers(new, writers)
                tail = h.job_boundary()
                h.journal_segment(final['cursor'], tail['cursor'], {}, population=(final, tail))
                need(all(tail[key] == final[key] for key in ('version', 'reload')))
                receipt_originals(transition, stage, incoming)
                need(os.path.samestat(os.fstat(deploy_fd), custody(lock_path)) and os.path.samestat(os.fstat(FENCE), custody(outer, True)))
                h.fresh_confirmation(tail)
                need(time.monotonic() <= deadline)
            store(ack_path(previous, new), dict(transport=decode(incoming), response=response, manager_jobs=final), before_commit=confirm)
    except BaseException:
        if failure is not None:
            store(failure, dict(outcome='outcome_unknown'))
        raise
    finally:
        if deploy_fd is not None:
            os.close(deploy_fd)
        os.close(FENCE)


if __name__ == '__main__':
    def interrupted(*_):
        raise InterruptedError('supervisor interrupted')
    for sig in (signal.SIGHUP, signal.SIGTERM):
        signal.signal(sig, interrupted)
    try:
        main()
    except (Exception, KeyboardInterrupt) as error:
        try:
            reason = f' 사유: {error}' if type(error).__name__ == 'Refusal' else ''
            os.write(2, f'복구 단계 미수락: 결과가 확인되지 않았습니다. 원본 증거를 보존하고 재시도하지 마십시오.{reason}\n'.encode())
        finally:
            os._exit(75)
