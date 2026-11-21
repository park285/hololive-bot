#!/usr/bin/env python3
import hashlib
import json
import os
from pathlib import Path
from decimal import Decimal
from contextlib import contextmanager
import re
import runpy
import shlex
import signal
import stat
import subprocess
import sys
import time
import tempfile
import uuid


ROOT = Path('/opt/hololive-bot')
LEGACY = ROOT / 'compose/current'
STORE = LEGACY.parent / 'app-sources'
STAGES = ROOT / 'release-sources'
PROFILES = LEGACY / 'artifacts/worker-profile'
RELEASES = PROFILES / 'releases'
QUADLET = Path('/etc/containers/systemd/hololive-bot')
LIB = Path(__file__).absolute().parent
API, WORKER = WRITERS = ('hololive-api', 'hololive-alarm-worker')
MIGRATOR = 'hololive-db-migrate'
FIREWALL = 'admin-dashboard-ingress-firewall'
PEERS = {FIREWALL, *WRITERS, 'chatbotgo-bot', 'twentyq-bot', 'chatbotgo-postgres', 'chatbotgo-pgbouncer',
         'twentyq-postgres', 'holo-postgres', 'holo-pgbouncer'}
BINDS = (('holo-postgres', 'deploy/postgres/pg_hba.conf', '/run/hololive-bot/postgres/pg_hba.conf'),
         ('holo-pgbouncer', 'deploy/pgbouncer/pg_hba.conf', '/etc/pgbouncer/pg_hba.conf'),
         ('holo-pgbouncer', 'deploy/pgbouncer/stats-users.txt', '/etc/pgbouncer/stats-users.txt'))
RECEIPTS = ('revisions', 'changes.raw', 'previous.actual', 'previous.metadata', 'previous.after',
            'protected.before', 'protected.after', 'images', 'images.after')
RECOVERY_RECEIPTS = (*RECEIPTS, 'pairs.before', 'pairs.after')
ATTEMPT = ('activation.config-before', 'activation.requested', 'coordinator.diagnostics', 'coordinator.exit',
           'activation.not-accepted')
ENDPOINT = 'holo-pgbouncer|6432'
CLOCK_REALTIME_COARSE = 5


class Refusal(ValueError):
    pass


def require(condition, reason):
    if not condition:
        raise Refusal(reason)


def hexid(value, length):
    return isinstance(value, str) and re.fullmatch('[0-9a-f]{%d}' % length, value) and int(value, 16) != 0


def digest(data):
    return hashlib.sha256(data).hexdigest()


def checked(path, directory=False, protected=False):
    info = path.lstat()
    require((stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode) and info.st_nlink == 1)
            and info.st_uid == info.st_gid == 0 and not info.st_mode & (0o7002 if protected else 0o7022)
            and not any(k.startswith('system.posix_acl_') or k == 'security.capability'
                        for k in os.listxattr(path, follow_symlinks=False)), 'unsafe evidence custody')
    return info


def authority(path, directory=False, protected=False):
    require(path.is_absolute() and '..' not in path.parts, 'invalid absolute authority')
    for parent in reversed(path.parents):
        checked(parent, True, protected and LEGACY in parent.parents)
    return checked(path, directory, protected)


def stable(path, fd, initial):
    current = os.fstat(fd)
    require(os.path.samestat(current, initial) and os.path.samestat(current, path.lstat())
            and current.st_ctime_ns == initial.st_ctime_ns, 'evidence changed during read')


def record(path):
    info = authority(path)
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), 'rb') as stream:
        stable(path, stream.fileno(), info)
        require(0 < info.st_size <= 16 * 1024 * 1024, 'empty or oversized evidence')
        data = stream.read(info.st_size + 1)
        require(len(data) == info.st_size, 'incomplete evidence read')
        stable(path, stream.fileno(), info)
    return data, info


def raw(path):
    return record(path)[0]


def exact(path, value):
    require(raw(path) == (value + '\n').encode(), 'receipt bytes differ: ' + path.name)


def absent(path):
    authority(path.parent, True)
    try:
        path.lstat()
    except FileNotFoundError:
        return
    raise Refusal('unexpected activation evidence: ' + path.name)


def unique(pairs):
    result = dict(pairs)
    require(len(result) == len(pairs), 'ambiguous JSON evidence')
    return result


def decode(data):
    return json.loads(data, object_pairs_hook=unique)


def run(*args, input=None):
    process = subprocess.run(args, cwd='/', env={'PATH': '/usr/sbin:/usr/bin', 'HOME': '/root', 'LC_ALL': 'C'},
                             input=input, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, timeout=90, check=True)
    return process.stdout.decode().rstrip('\n')


def inspect(service, template):
    return run('podman', 'container', 'inspect', '--format', template, service)


def sealed(action, service, source, *args):
    for path in (LIB / 'sealed-app.sh', LIB / 'postgres-endpoint.sh'):
        require(LEGACY not in path.parents, 'mutable helper location')
        authority(path)
    binds = STORE / source / 'scripts/deploy/lib/protected-binds.sh'
    require(digest(raw(binds)) == '7618e0de3993e5d15f1db26849486f59a7723321b364d02acba9e9334212722d',
            'unreviewed original protected bind helper')
    return run('/usr/bin/bash', '--noprofile', '--norc', '-c',
               'set -euo pipefail; . "$1/sealed-app.sh"; . "$1/postgres-endpoint.sh"; '
               'deploy_root=$2; ROOT_DIR=$3; prefix=; shift 3; "$@"', 'evidence', LIB, LEGACY, STORE / source,
               action, service, *args)


def source_proof(source, writer):
    tool = STAGES / ('dv-tool-' + source)
    transition = STORE / ('transition-' + writer + '-' + source)
    absent(tool / 'source.not-accepted')
    exact(tool / 'source.accepted', str(transition))
    exact(transition / 'revisions', f'installed={writer}\ncandidate={source}\ntool={source}')
    old_peers = raw(tool / 'source.peers.before')
    require(raw(tool / 'source.peers.after') == old_peers, 'source peer records disagree')
    peers = decode(old_peers)
    require(isinstance(peers, dict) and peers.keys() == PEERS, 'incomplete original peer inventory')
    requested, request_stat = record(tool / 'source.requested')
    require(re.fullmatch(rb'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\+00:00\n', requested), 'invalid source request')
    before = decode(raw(tool / 'source.before.json'))
    after = decode(raw(tool / 'source.after.json'))
    legacy = authority(LEGACY / '.source-revision')
    old_pin = {'state': 'old', 'inode': [legacy.st_dev, legacy.st_ino]}
    missing = {'state': 'absent'}
    require(before == dict(legacy=old_pin, active=missing, old=missing, new=missing, store=missing,
                           transition=missing, revisions_match=False, receipts=dict.fromkeys(RECEIPTS, missing)),
            'not an original legacy-to-sealed source publication')
    require(after.keys() == before.keys() and after['receipts'].keys() == set(RECEIPTS)
            and after['revisions_match'] is True and after['active']['state'] == 'new'
            and len(after['active']['inode']) == 2, 'incomplete source acceptance')
    pin, info = record(STORE / 'active')
    if pin == (source + '\n').encode():
        require(after['active'] == {'state': 'new', 'inode': [info.st_dev, info.st_ino]}, 'original active source pointer changed')
    else:
        require(re.fullmatch(b'incomplete ' + source.encode() + b' -> [0-9a-f]{40}\n', pin), 'unexpected publication phase')
    for key, path, rev, state in [('legacy', LEGACY, writer, 'old'), ('old', STORE / writer, writer, 'old'),
                                  ('new', STORE / source, source, 'new')]:
        data, info = record(path / '.source-revision')
        require(data == (rev + '\n').encode() and after[key] == {'state': state, 'inode': [info.st_dev, info.st_ino]},
                'original source generation changed')
    for key, path in [('store', STORE), ('transition', transition), ('previous.actual', transition / 'previous.actual')]:
        info = authority(path, True)
        observation = (after if key != 'previous.actual' else after['receipts'])[key]
        require(stat.S_IMODE(info.st_mode) == 0o700 and observation == {'state': 'directory', 'inode': [info.st_dev, info.st_ino]},
                'original source directory changed')
    for name in RECEIPTS:
        if name == 'previous.actual':
            continue
        data, info = record(transition / name)
        require(after['receipts'][name] == {'state': 'file', 'bytes': len(data), 'sha256': digest(data),
                                            'inode': [info.st_dev, info.st_ino]}, 'original transition record changed')
    for a, b in [('images', 'images.after'), ('previous.metadata', 'previous.after'), ('protected.before', 'protected.after')]:
        require(after['receipts'][a]['sha256'] == after['receipts'][b]['sha256'], 'unconfirmed source acceptance')
    exit_record = decode(raw(tool / 'source.coordinator.exit'))
    ended = exit_record['ended_utc_ns']
    require(type(ended) is int and request_stat.st_mtime_ns <= ended <= authority(tool / 'source.accepted').st_mtime_ns
            and type(exit_record['returncode']) is int and exit_record == {'returncode': 0, 'markers': [], 'ended_utc_ns': ended},
            'original source publication did not succeed')
    _, images, manifests = manifest_pair(source, transition, writer)
    return peers, images[API], manifests[API], raw(transition / 'protected.after'), authority(tool / 'source.accepted').st_mtime_ns


def manifest_pair(source, transition, base=None):
    images = {}
    manifests = {}
    entries = []
    versions = set()
    api = STAGES / ('dv-holo-api-' + source)
    for service, slot in zip(WRITERS, ('api', 'worker'), strict=True):
        stage = STAGES / ('dv-holo-' + slot + '-' + source)
        data = raw(stage / 'manifest.json')
        manifests[service] = digest(data)
        exact(stage / 'stage.accepted', manifests[service])
        manifest = decode(data)
        image = manifest['image']
        require(isinstance(image, str) and re.fullmatch('sha256:[0-9a-f]{64}', image)
                and re.fullmatch(r'(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*)){2}', manifest['version'])
                and hexid(manifest['tag_object'], 40)
                and (manifest['service'], manifest['revision']) == ('holo-' + slot, source)
                and (base is None or manifest['base'] == base),
                'unbound original staged image')
        images[service] = image
        versions.add(manifest['version'])
        entries.append(dict(service=service, image=image, manifest=manifests[service], tag_object=manifest['tag_object']))
        path = api / (service + '.candidate-image')
        exact(path, image)
        require(stat.S_IMODE(authority(path).st_mode) == 0o400, 'mutable original image binding')
    require(len(versions) == 1 and entries[0]['tag_object'] == entries[1]['tag_object'], 'split original release identity')
    pair = dict(entries=entries, revision=source, tag='v' + versions.pop())
    for name in ('candidate-manifests.json', 'pair-binding.requested'):
        data, info = record(api / name)
        require(stat.S_IMODE(info.st_mode) == 0o400 and
                data == (json.dumps(pair, sort_keys=True, separators=(',', ':')) + '\n').encode(), 'original manifest binding changed')
    require(len(set(images.values())) == 2, 'duplicate source image')
    exact(transition / 'images', '\n'.join(name + ' ' + image for name, image in images.items()))
    return pair, images, manifests


def release_name(service, name):
    return isinstance(name, str) and re.fullmatch(re.escape(service) + r'-v2-[0-9]{8}T[0-9]{6}Z-[a-zA-Z0-9]+', name)


def generation_shape(service, original):
    require(isinstance(original, list) and len(original) == (1 if service == FIREWALL else 4)
            and hexid(original[0], 32) and (service == FIREWALL or
                (hexid(original[1], 64) and hexid(original[2], 64) and original[3] == 'healthy')), 'bad original generation')


def original_window(start, timestamps):
    tick_ns = round(time.clock_getres(CLOCK_REALTIME_COARSE) * 10**9)
    require(type(start) is int and timestamps == sorted(timestamps) and timestamps[-1] <= time.time_ns()
            and timestamps[1] <= start <= timestamps[2] + tick_ns, 'invalid original window')


def original_receipt_pin(source, failed, identities):
    if (source, failed) == ('6d7e8749983723867fa52d5a0e9cd648198d2e61', 'hololive-api-v2-20261119T121019Z-lVIJBA'):
        pin = 'c84484a54ef23edacdc4d5a707e423bf606854db59bd8543aa300aaea5cb22ab'
    else:
        pin = raw(stage_for(API, source) / 'original-receipts.sha256').decode().removesuffix('\n')
    require(digest(json.dumps(identities, separators=(',', ':')).encode()) == pin, 'original receipt identity changed')


def original_attempt(source, failed, peers, image, manifest, source_end):
    api = STAGES / ('dv-holo-api-' + source)
    worker = STAGES / ('dv-holo-worker-' + source)
    for name in (*ATTEMPT, 'activation.accepted'):
        absent(worker / name)
    absent(api / 'activation.accepted')
    rows = []
    for name in ATTEMPT:
        try:
            rows.append(record(api / name))
        except FileNotFoundError as error:
            raise Refusal('missing original receipt: ' + str(api / name)) from error
    config, request, diag, exit_record, refusal = (decode(data) for data, _ in rows)
    start = request['utc_ns']
    timestamps = [source_end, *(info.st_mtime_ns for _, info in rows)]
    original_window(start, timestamps)
    require(request.keys() == {'utc_ns', 'manifest', 'image', 'old', 'protected', 'source', 'tool', 'prior'} and
            (request['source'], request['tool'], request['image'], request['manifest']) == (source, source, image, manifest),
            'unexpected activation request')
    protected = request['protected']
    require(protected.keys() == PEERS - {API} | {'ledger', 'protected_file_pairs'} and
            all(peers[n] == (request['old'] if n == API else protected[n]) for n in PEERS),
            'activation request differs from the source publication')
    for name, gen in peers.items():
        generation_shape(name, gen)
    require(diag == [{'phase': 'migration'}] and type(exit_record['rc']) is int
            and exit_record == {'rc': 1, 'markers': ['not_activated']}, 'coordinator did not refuse before activation')
    require(refusal == {'outcome': 'outcome_unknown', 'error': 'RuntimeError', 'reason': 'coordinator refused or outcome unknown',
                        'original_jobs': {failed: {}}, 'active': 'active', 'load': 'loaded', 'invocation': peers[API][0]},
            'ambiguous original coordinator result')
    prior = request['prior']
    require(isinstance(prior, list) and prior and all(release_name(API, n) for n in prior) and prior == sorted(set(prior))
            and failed not in prior, 'ambiguous prior release set')
    identities = [[n, digest(data), info.st_ino, info.st_ctime_ns, info.st_mtime_ns] for n, (data, info) in zip(ATTEMPT, rows)]
    original_receipt_pin(source, failed, identities)
    return config, protected, start, rows[2][1].st_mtime_ns, timestamps[-1], prior


def request_proof(source, failed, peers, image, manifest, source_end):
    config, protected, start, diagnostic, end, prior = original_attempt(source, failed, peers, image, manifest, source_end)
    authority(RELEASES, True)
    names = os.listdir(RELEASES)
    require({name for name in names if name.startswith(API + '-v2-')} == set(prior) | {failed}, 'unexpected API release')
    attempt = RELEASES / failed
    require(set(os.listdir(attempt)) == {'service', 'candidate.revision', 'candidate.image', 'migrator.image', 'infrastructure',
                                        'unchanged-peer', 'previous', 'candidate', 'previous.endpoint', 'rollback.conf', 'rollback.sha256'},
            'missing or unexpected pre-job release evidence')
    require(all(start <= stamp <= diagnostic for info in (authority(attempt, True), authority(attempt / 'candidate.revision'))
                for stamp in (info.st_mtime_ns, info.st_ctime_ns)),
            'selected release outside coordinator window')
    for name in names:
        if name in prior or name.startswith(WORKER + '-v2-'):
            path = RELEASES / name
            directory = authority(path, True)
            require(max(directory.st_mtime_ns, directory.st_ctime_ns) <= start, 'another writer release changed')
            revision, info = record(path / 'candidate.revision')
            require((name in prior or release_name(WORKER, name)) and re.fullmatch(b'[0-9a-f]{40}\n', revision)
                    and revision != (source + '\n').encode() and max(info.st_mtime_ns, info.st_ctime_ns) <= start, 'another writer attempt')
    return config, protected, start, diagnostic, end


def configuration(config, inventory, peers, writer, start, diagnostic):
    paths = {}
    for service in WRITERS:
        paths[PROFILES / 'active' / (service + '.pair')] = False
        paths[QUADLET / (service + '.container.d/50-worker-profile.conf')] = True
    migrator = QUADLET / (MIGRATOR + '.container.d/50-migrator-image.conf')
    paths[migrator] = True
    require(config.keys() == {str(p) for p in paths}, 'incomplete original configuration inventory')
    for path, dropin in paths.items():
        old = config[str(path)]
        require(isinstance(old, list) and len(old) == 3 and hexid(old[0], 64) and all(type(i) is int and i > 0 for i in old[1:]),
                'invalid configuration witness')
        if dropin:
            rows = [line.split() for line in inventory.splitlines() if line.startswith(os.fsencode(path) + b' ')]
            require(len(rows) == 1 and len(rows[0]) == 7 and [rows[0][6].decode(), int(rows[0][2]), int(rows[0][5])] == old,
                    'original drop-in not bound to source publication')
        data, info = record(path)
        if path == migrator:
            require(digest(data) != old[0] and info.st_ino != old[1] and info.st_ctime_ns > old[2]
                    and start <= info.st_ctime_ns <= diagnostic, 'migrator was not replaced during original attempt')
        else:
            require([digest(data), info.st_ino, info.st_ctime_ns] == old, 'writer configuration changed')
    active = {}
    for service in WRITERS:
        pointer = PROFILES / 'active' / (service + '.pair')
        pair = Path(raw(pointer).decode().removesuffix('\n'))
        require(pair.name == 'candidate' and pair.parent.parent == RELEASES and release_name(service, pair.parent.name),
                'invalid legacy active pair')
        exact(pointer, str(pair))
        for name, value in [('service', service), ('candidate.revision', writer), ('candidate.image', 'sha256:' + peers[service][2]),
                            ('migrator.image', 'sha256:' + peers[API][2])]:
            exact(pair.parent / name, value)
        for name in ('accepted-writer', 'schema-check'):
            absent(pair.parent / name)
        data = pair_metadata(pair, service, writer, 'sha256:' + peers[service][2])
        require('source_generation' not in data, 'legacy pair unexpectedly sealed')
        active[service] = pair, data
    return active


def pair_metadata(pair, service, revision, image):
    data = decode(raw(pair / 'pair.json'))
    require((data['service'], data['revision'], data['image'], data['profile_hash']) ==
            (service, revision, image, digest(raw(pair / 'profile.json'))), 'profile pair mismatch')
    return data


def readable_pair(directory):
    require(authority(directory, True).st_mode & 0o001 and
            stat.S_IMODE(authority(directory / 'profile.json').st_mode) == 0o444, 'unreadable mounted profile pair')


def legacy_release(pair):
    require(pair.name == 'candidate' and pair.parent.parent == RELEASES, 'invalid legacy release')
    def check_tree(path):
        directory = stat.S_ISDIR(path.lstat().st_mode)
        authority(path, directory)
        if directory:
            for child in path.iterdir():
                check_tree(child)
    check_tree(pair.parent)
    readable_pair(pair)


def failed_release(failed, source, writer, peers, active, image):
    release = RELEASES / failed
    authority(release, True)
    for name, value in [('service', API), ('candidate.revision', source), ('candidate.image', image), ('migrator.image', image),
                        ('previous.endpoint', ENDPOINT)]:
        exact(release / name, value)
    previous = pair_metadata(release / 'previous', API, writer, 'sha256:' + peers[API][2])
    candidate = pair_metadata(release / 'candidate', API, source, image)
    require('source_generation' not in previous and previous['container_id'] == peers[API][1]
            and previous['captured_release'] == str(active[API][0]) and candidate['source_generation'] == source
            and active[API][1]['profile_hash'] == previous['profile_hash'] == candidate['profile_hash'], 'captured release drift')
    installed = raw(QUADLET / (API + '.container'))
    for slot in ('previous', 'candidate'):
        directory = release / slot
        require(raw(directory / 'unit.container') == installed, 'failed release unit changed')
        exact(directory / 'SHA256SUMS', '\n'.join(digest(raw(directory / n)) + '  ' + n
                                                  for n in ('profile.json', 'pair.json', 'unit.container')))
    require(LEGACY not in LIB.parents, 'mutable helper location')
    authority(LIB / 'worker-profile-pair.py')
    render = runpy.run_path(str(LIB / 'worker-profile-pair.py'))['dropin_contents']
    require(raw(release / 'candidate/50-worker-profile.conf') == render(release / 'candidate', API, candidate)
            and raw(release / 'rollback.conf') == render(release / 'previous', API, previous, previous['app_version']),
            'failed release drop-in changed')
    names = ('previous/pair.json', 'previous.endpoint', 'rollback.conf')
    exact(release / 'rollback.sha256', '\n'.join(digest(raw(release / n)) + '  ' + n for n in names))


def unit(name, key):
    return run('systemctl', 'show', name + '.service', '-p', key, '--value')


def running_generation(service):
    fields = run('systemctl', 'show', '--all', service + '.service', '-p', 'LoadState', '-p', 'ActiveState',
                 '-p', 'NRestarts', '-p', 'Job', '-p', 'InvocationID').splitlines()
    state = unique([line.split('=',1) for line in fields])
    invocation = state.pop('InvocationID')
    require(state.pop('Job') in ('','0') and state == dict(LoadState='loaded',ActiveState='active',NRestarts='0'),
            'unsettled protected service')
    generation = [invocation]
    if service != FIREWALL:
        generation.extend(inspect(service, '{{.Id}} {{.Image}} {{.State.Health.Status}}').split())
    generation_shape(service, generation)
    require(unit(service,'InvocationID') == invocation, 'protected generation changed: ' + service)
    return generation


def live_peers(peers):
    for service, original in peers.items():
        require(running_generation(service) == original, 'protected generation changed: ' + service)


def bind_stat(path, host):
    initial = authority(path, protected=True) if host else path.lstat()
    with os.fdopen(os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK), 'rb') as stream:
        fd = stream.fileno()
        stable(path, fd, initial)
        require(stat.S_ISREG(initial.st_mode) and initial.st_uid == initial.st_gid == 0
                and not initial.st_mode & 0o7002, 'unsafe protected bind')
        attrs = {k: os.getxattr(fd, k) for k in os.listxattr(fd)}
        require(not any(k.startswith('system.posix_acl_') or k == 'security.capability' for k in attrs), 'unsafe bind attributes')
        checksum = hashlib.file_digest(stream, 'sha256').digest()
        stable(path, fd, initial)
    s = initial
    return [s.st_dev, s.st_ino, s.st_mode, s.st_uid, s.st_gid, s.st_size, s.st_nlink, s.st_mtime_ns, s.st_ctime_ns], attrs, checksum


def infrastructure(source, failed, peers, original, start):
    require(original.keys() == {target for _, _, target in BINDS}, 'incomplete protected bind inventory')
    lines = []
    for name in ('holo-postgres', 'holo-pgbouncer'):
        state = inspect(name, '{{.State.Running}} {{.Id}} {{.Image}} {{.State.StartedAt.UnixNano}}').split()
        require(len(state) == 4 and state[:3] == ['true', *peers[name][1:3]] and 0 < int(state[3]) < start,
                'protected container start changed')
        ident = inspect(name, '{{.Id}} {{.State.Pid}}')
        cid, pid = ident.split()
        require(cid == state[1] and re.fullmatch('[1-9][0-9]*', pid), 'stale protected namespace')
        proc = Path('/proc', pid)
        mounts = [row.split() for row in (proc / 'mountinfo').read_text().splitlines()]
        for service, src, dst in BINDS:
            if service != name:
                continue
            rows = [row for row in mounts if row[4] == dst]
            require(len(rows) == 1 and 'ro' in rows[0][5].split(','), 'unrecognized protected mount')
            a, aa, ad = bind_stat(LEGACY / src, True)
            b, ba, bd = bind_stat(proc / 'root' / dst[1:], False)
            require(a[2:6] == b[2:6] and ad == bd and aa == ba and (a[:2] == b[:2] or aa.get('security.selinux')),
                    'host and mounted protected data disagree')
            require(original[dst]['equal_bytes'] is True and original[dst]['equal_xattrs'] is True and
                    original[dst] == dict(host=a, mounted=b, cid=cid, mount_id=int(rows[0][0]), equal_bytes=True, equal_xattrs=True),
                    'protected mount no longer matches original host and container identities')
        lines.extend([sealed('sealed_bind_generation', name, source), ' '.join([name, *state[1:]])])
        require(inspect(name, '{{.Id}} {{.State.Pid}}') == ident, 'protected namespace changed')
    exact(RELEASES / failed / 'infrastructure', '\n'.join(lines))


LEDGER = b"""BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
DO $$ BEGIN IF current_database()<>'hololive' OR current_user<>'postgres_admin' OR inet_server_addr() IS NOT NULL OR current_setting('transaction_read_only')<>'on' OR current_setting('statement_timeout')<>'30s' OR current_setting('lock_timeout')<>'3s' THEN RAISE EXCEPTION 'guard'; END IF; END $$;
%s
%s
SELECT count(*),count(m.filename),count(c.filename),md5(string_agg(c.filename||':'||c.checksum_sha256,E'\\n' ORDER BY c.filename)),encode(sha256(convert_to(string_agg((to_jsonb(m)||to_jsonb(c))::text,E'\\n' ORDER BY c.filename),'UTF8')),'hex') FROM public.schema_migrations m FULL JOIN public.schema_migration_checksums c USING(filename);
SELECT count(*),count(m.filename),count(c.filename),md5(string_agg(c.filename||':'||c.checksum_sha256,E'\\n' ORDER BY c.filename)),encode(sha256(convert_to(string_agg(json_build_array(m.tableoid,pg_relation_filenode(m.tableoid),m.ctid,m.xmin,m.applied_at,c.tableoid,pg_relation_filenode(c.tableoid),c.ctid,c.xmin,c.recorded_at)::text,E'\\n' ORDER BY c.filename COLLATE "C"),'UTF8')),'hex') FROM public.schema_migrations m FULL JOIN public.schema_migration_checksums c USING(filename);
ROLLBACK;
"""


def ledger(source, writer, peers, protected):
    stages = [STAGES / ('dv-holo-' + slot + '-' + source) for slot in ('api', 'worker')]
    original = raw(stages[0] / 'protected.before')
    for path in (stages[0] / 'protected.after', stages[1] / 'protected.before', stages[1] / 'protected.after'):
        require(raw(path) == original, 'original stage protection witnesses disagree')
    stage = decode(original)
    require(stage['legacy'] == writer and stage['generations'] == peers and stage['split_binds'] == protected['protected_file_pairs'],
            'original stage generation changed')
    logical, physical = protected['ledger'], stage['ledger']
    require(all(re.fullmatch(r'32\|32\|32\|c15cd19b3d188ac847f2e116a02d9a20\|[0-9a-f]{64}', s)
                for s in (logical, physical)), 'invalid original ledger')
    root = STORE / source / 'hololive/hololive-api'
    lineage = raw(root / 'internal/migrationrunner/queries/lineage_head.sql')
    catalog = raw(root / 'scripts/migrations/contracts/001_schema_epoch4_baseline.sql')
    require(digest(lineage) == '9d0861bfaf76f8911c5c314ea38994ef4ad59760f0ecfbceedd212e7edfbd106'
            and digest(catalog) == '2bb34b7ebd7a5b971c23b7102009661a64dc505755fed4f25bddf798978e986d',
            'unreviewed lineage or catalog query')
    manifest = b"'78a73440d7d8bfa24f663e64797f1198e4b44af0e9da336ea348e9b9aac14ed1  001_schema_epoch4_baseline.sql'"
    live = run('podman', 'exec', '-i', '-e', 'PGOPTIONS=-c default_transaction_read_only=on -c statement_timeout=30s -c lock_timeout=3s',
               'holo-postgres', 'psql', '-XqAtw', '-v', 'ON_ERROR_STOP=1', '-h', '/var/run/postgresql', '-U', 'postgres_admin',
               '-d', 'hololive', input=LEDGER % (lineage.replace(b'$1', manifest), catalog))
    require(live.splitlines() == ['3|0|0', 't', logical, physical], 'original migration ledger or catalog changed')


def cursor_fields(cursor):
    cursor = unique([part.split('=', 1) for part in cursor.split(';')])
    return cursor['b'], cursor['s'], int(cursor['i'], 16), int(cursor['t'], 16)


@contextmanager
def bounded(seconds, reason):
    def expired(*_):
        raise Refusal(reason)
    previous = signal.signal(signal.SIGALRM, expired)
    started = time.monotonic()
    remaining, period = signal.getitimer(signal.ITIMER_REAL)
    signal.setitimer(signal.ITIMER_REAL, max(0.000001,min(seconds,remaining) if remaining else seconds))
    try:
        yield
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous)
        if remaining:
            signal.setitimer(signal.ITIMER_REAL,max(.000001,remaining-(time.monotonic()-started)),period)


def journal_rows(args):
    env = {'PATH': '/usr/sbin:/usr/bin', 'HOME': '/root', 'LC_ALL': 'C'}
    with subprocess.Popen(args, cwd='/', env=env, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL) as proc:
        try:
            with bounded(90,'journal scan deadline exceeded'):
                for index, line in enumerate(iter(lambda: proc.stdout.readline(65537), b'')):
                    require(index < 4_000_000 and len(line) <= 65536 and line.endswith(b'\n'), 'journal scan limit exceeded')
                    yield decode(line)
                require(proc.wait() == 0, 'journal scan incomplete')
        finally:
            proc.kill()


JOURNAL = ('journalctl', '--boot', '--quiet', '--no-pager', '-o', 'json',
           '--output-fields=_BOOT_ID,_PID,UNIT,MESSAGE_ID,JOB_ID,JOB_TYPE,JOB_RESULT,INVOCATION_ID,MESSAGE,'
           '_TRANSPORT,SYSLOG_IDENTIFIER,N_DROPPED,_UID,_SYSTEMD_UNIT,_COMM,SYSLOG_PID,PRIORITY,'
           'PODMAN_NAME,PODMAN_EVENT,PODMAN_ID,_SYSTEMD_INVOCATION_ID,_AUDIT_TYPE_NAME,AUDIT_FIELD_UNIT,'
           'CONTAINER_NAME,CONTAINER_ID_FULL,CODE_FUNC')
STARTING = '7d4958e842da4a758f6c1cdc7b36dcc5'
STARTED = '39f53479d3a045ac8e11786248231fbf'
STOPPING = 'de5b426a63be47a7b6ac3eaac82e2f6f'
STOPPED = '9d1aaa27d60140bd96365438aad20286'
LIFECYCLE = {STARTING, STARTED, STOPPING, STOPPED, 'be02cf6855d2428ba40df7e9d022f03d', 'f5d1baa5b05d4015abd3c15d7dc833e9'}
UNITS = {n + '.service' for n in (*WRITERS, MIGRATOR)}


def instant(us):
    return '@%d.%06d' % divmod(us, 1000000)


def journal_tip():
    now = time.time_ns() // 1000
    closing, = journal_rows((*JOURNAL, '--until=' + instant(now), '-n', '1'))
    cursor = closing['__CURSOR']
    require(0 <= time.time_ns() // 1000 - cursor_fields(cursor)[3] <= 30_000_000, 'missing manager journal coverage')
    return cursor


def journal_loss(row):
    mid, msg = row.get('MESSAGE_ID'), row.get('MESSAGE', '')
    transport = row.get('_TRANSPORT')
    journal_driver = transport == 'driver' or row.get('_SYSTEMD_UNIT') == 'systemd-journald.service' or (transport == 'kernel' and isinstance(msg, str)
            and (row.get('SYSLOG_IDENTIFIER') == 'systemd-journald' or msg.startswith('systemd-journald[')))
    loss = isinstance(msg, str) and re.search(r'(?i)\b(suppressed|missed|lost|dropped|rate.limit\w*|'
            r'fail(?:ed|ure) to (?:write|store|append|rotate|open|sync|receive message)|'
            r'entry printing failed|failed to read from /dev/kmsg|(?:fail(?:ed|ure)|error|unable|cannot|could not)\b[^;\n]*\bcredentials?|credentials? (?:lookup |retrieval )?(?:failed|unavailable|missing)|truncated (?:payload|control) data|(?:unexpected )?error (?:while )?(?:writing|appending) to (?:the )?journal|despite vacuuming|giving up)\b', msg)
    ring_loss = transport == 'kernel' and isinstance(msg, str) and re.search(
        r'(?i)(?:printk:.*\b(?:dropped|lost)\b|\b(?:missed|lost|dropped) [0-9]+ kernel messages|/dev/kmsg.*overrun)', msg)
    require(not ring_loss and not (transport == 'kernel' and 'N_DROPPED' in row) and not (journal_driver and
            (mid in ('a596d6fe7bfa4994828e72309e95d61e', 'e9bf28e6e834481bb6f48f548ad13606')
             or 'N_DROPPED' in row or loss or not isinstance(msg, str))), 'possible manager journal loss')


def manager_event(row, approved, seen):
    mid, unit_name, msg = row.get('MESSAGE_ID'), row.get('UNIT'), row.get('MESSAGE', '')
    transport = row.get('_TRANSPORT')
    sender = row.get('SYSLOG_PID')
    forwarded = transport in ('kernel', 'syslog') and (sender == '1' or
                sender is None and row.get('SYSLOG_IDENTIFIER') == 'systemd' or
                isinstance(msg, str) and msg.startswith(('systemd[1]:', 'systemd:')))
    audit = row.get('_AUDIT_TYPE_NAME')
    audit_unit = row.get('AUDIT_FIELD_UNIT')
    audit_pid = row.get('_PID')
    if transport == 'kernel' and isinstance(msg, str) and re.search(r'\btype=113[01]\b', msg):
        printed = re.match(r"audit: type=(113[01]) audit\([0-9.:]+\): pid=([0-9]+) .*?\bmsg='unit=([^ ']+) ", msg)
        require(printed, 'unidentified manager audit record')
        audit, audit_pid, audit_unit = {'1130': 'SERVICE_START', '1131': 'SERVICE_STOP'}[printed[1]], printed[2], printed[3]
        transport = 'audit'
    if transport == 'audit' and (audit in ('SERVICE_START', 'SERVICE_STOP') or audit_unit in (*WRITERS, MIGRATOR, *UNITS)):
        require(audit in ('SERVICE_START', 'SERVICE_STOP') and audit_pid == '1' and isinstance(audit_unit, str)
                and audit_unit, 'unidentified manager audit record')
        name = audit_unit if audit_unit.endswith('.service') else audit_unit + '.service'
        if name in UNITS:
            key = name, audit
            require(key not in seen, 'duplicate protected audit action')
            seen.add(key)
            require(name in approved and int(row['__REALTIME_TIMESTAMP'])*1000 >= approved[name][0]['request_utc_ns']
                    and (audit == 'SERVICE_START' or approved[name][1] or name == MIGRATOR+'.service'),
                    'unexpected protected service audit record')
        return True
    if transport == 'audit' or row.get('_PID') != '1' and not forwarded:
        return False
    require(isinstance(msg, str), 'unclassified manager message')
    if forwarded:
        msg = msg.removeprefix('systemd[1]: ').removeprefix('systemd: ')
    fields = row.keys() & {'JOB_ID', 'JOB_TYPE', 'JOB_RESULT'}
    if not unit_name and not fields and mid not in LIFECYCLE and re.fullmatch(
            r"Reloading\.\.\.|Reloading finished in \d+ ms\.|Reload requested from client PID [1-9]\d*"
            r"(?: \('[^'\n]+'\))?(?: \(unit [^()\n]+\))?\.\.\.", msg):
        return True
    if (not unit_name and not fields and mid is None and row.get('_PID') == '1' and transport == 'journal'
            and row.get('CODE_FUNC') == 'stat_warn_permissions' and re.fullmatch(
                r'Configuration file /[^\n]+ is marked (?:executable|world-writable|world-inaccessible)\. [^\n]*Proceeding anyway\.', msg)):
        return False
    require(isinstance(unit_name, str) and unit_name, 'possible schema or writer job')
    named = {name for name in UNITS if name in msg}
    if unit_name not in UNITS and not named:
        return bool(unit_name and (fields or mid in LIFECYCLE))
    require(unit_name in approved and named <= {unit_name}, 'possible schema or writer job')
    job, prior = approved[unit_name]
    require(int(row['__REALTIME_TIMESTAMP']) * 1000 >= job['request_utc_ns'], 'unit activity before approved request')
    inv = row.get('INVOCATION_ID')
    if mid in ('7ad2d189f7e94e70a38c781354912448', 'ae8f7b866b0347b9af31fe1c80b127c0'):
        require(not fields and inv == (prior or job['invocation']), 'unbound unit deactivation')
        return
    stopping = mid in (STOPPING, STOPPED)
    kind = row.get('JOB_TYPE')
    require(mid in (STARTING, STARTED, STOPPING, STOPPED) and row.get('JOB_ID') == str(job['job'])
            and (not stopping or prior) and inv == (prior if stopping else job['invocation'])
            and (kind in ('stop', 'restart') if stopping else kind == 'start')
            and row.get('JOB_RESULT') == ('done' if mid in (STOPPED, STARTED) else None),
            'unapproved manager job or invocation')
    require((unit_name, mid) not in seen, 'duplicate manager job event')
    if prior:
        order = (STOPPING, STOPPED, STARTING, STARTED)
        position = order.index(mid)
        require(not position or (unit_name, order[position-1]) in seen, 'writer stop/start order is incomplete')
    seen.add((unit_name, mid))


def container_event(row, approved, observed):
    if row.get('CONTAINER_NAME') == MIGRATOR and not row.get('PODMAN_EVENT'):
        require(isinstance(row.get('_UID'), str) and row['_UID'].isdecimal(), 'unidentified protected container sender')
        if row['_UID'] == '0':
            unit_name = MIGRATOR + '.service'
            require(unit_name in approved and row.get('CONTAINER_ID_FULL') == approved[unit_name][0]['cid'] and
                    int(row['__REALTIME_TIMESTAMP'])*1000 >= approved[unit_name][0]['request_utc_ns'], 'unexpected rootful container execution')
        return
    name, action = row.get('PODMAN_NAME'), row.get('PODMAN_EVENT')
    if name not in (*WRITERS, MIGRATOR) or action not in ('start', 'died'):
        return
    require(isinstance(row.get('_UID'), str) and row['_UID'].isdecimal() and row.get('_PID'), 'unidentified protected container sender')
    if row['_UID'] != '0':
        return
    unit_name = name + '.service'
    require(unit_name in approved, 'unexpected rootful container execution')
    job, _ = approved[unit_name]
    key = (unit_name, action)
    cid = job.get('previous_cid') if name in WRITERS and action == 'died' else job['cid']
    require(row.get('PODMAN_ID') == cid and key not in observed and
            int(row['__REALTIME_TIMESTAMP'])*1000 >= job['request_utc_ns'], 'ambiguous rootful container execution')
    if action == 'start':
        require(row.get('_COMM') == 'podman' and row.get('_SYSTEMD_UNIT') == unit_name and
                row.get('_SYSTEMD_INVOCATION_ID') == job['invocation'], 'unbound rootful container start')
    elif name == MIGRATOR:
        require((unit_name, 'start') in observed, 'migrator exit precedes its start')
    observed.add(key)


def journald_generation():
    fields = run('systemctl', 'show', '--all', 'systemd-journald.service', '-p', 'ActiveState', '-p', 'InvocationID',
                 '-p', 'ExecMainStartTimestampMonotonic').splitlines()
    state = unique([field.split('=', 1) for field in fields])
    started = int(state['ExecMainStartTimestampMonotonic'])
    require(state['ActiveState'] == 'active' and hexid(state['InvocationID'], 32) and started > 0,
            'journal daemon generation unavailable')
    return state['InvocationID'], started


def journal_segment(anchor, closing, approved, original=None, population=None, original_tail=False, reloads=()):
    boot_path = Path('/proc/sys/kernel/random/boot_id')
    boot = uuid.UUID(boot_path.read_text().strip()).hex
    anchor_boot, seq, first, before = cursor_fields(anchor)
    close_boot, close_seq, last, latest = cursor_fields(closing)
    require(anchor_boot == close_boot == boot and seq == close_seq and hexid(seq, 32) and 0 < first <= last
            and before <= latest, 'missing manager journal coverage')
    journal_daemon = journald_generation()
    expected, last_mono, last_wall = first, -1, -1
    inside = resolved = False
    admitted = 0
    seen = set()
    installed = set()
    containers = set()
    reload_completions = []
    for row in journal_rows((*JOURNAL, '--since=' + instant(before), '--until=' + instant(latest))):
        cursor = row['__CURSOR']
        cursor_boot, cursor_seq, index, stamp = cursor_fields(cursor)
        wall, mono = int(row['__REALTIME_TIMESTAMP']), int(row['__MONOTONIC_TIMESTAMP'])
        require(row['_BOOT_ID'] == cursor_boot == boot and cursor_seq == seq and wall == stamp, 'journal sequence changed')
        if (expected == first and index < first and wall == before) or (expected == last + 1 and index > last and wall == latest):
            continue
        require(index == expected <= last and (index != first or cursor == anchor) and (index != last or cursor == closing)
                and mono >= last_mono and latest >= wall >= last_wall, 'missing original journal segment')
        expected += 1
        last_mono, last_wall = mono, wall
        require(journal_daemon[1] <= mono and (row.get('_SYSTEMD_UNIT') != 'systemd-journald.service' or
                row.get('_SYSTEMD_INVOCATION_ID', journal_daemon[0]) == journal_daemon[0]), 'journal daemon changed across evidence interval')
        journal_loss(row)
        if original:
            if original[0] - original[2] <= wall <= original[1] and (row.get('_PID') == '1' or row.get('_SYSTEMD_UNIT') == 'init.scope'):
                admitted += 1
            if wall < original[0]:
                continue
        elif index == first:
            continue
        if row.get('UNIT') in UNITS and row.keys() & {'JOB_ID','JOB_TYPE','JOB_RESULT'}:
            require(row.get('_PID') == '1' and row.get('_UID') == '0', 'unidentified protected manager record')
        container_event(row, approved, containers)
        if (original or original_tail) and row.get('_PID') == '1' and row.get('UNIT') in UNITS and row.get('JOB_RESULT') == 'canceled' and row.get('MESSAGE_ID') not in (STARTING, STARTED, STOPPING, STOPPED):
            continue
        identified = manager_event(row, approved, seen)
        if population and row.get('_PID') == '1' and row.get('_TRANSPORT') == 'journal' and re.fullmatch(
                r'Reloading finished in [0-9]+ ms\.', row.get('MESSAGE','')):
            reload_completions.append(mono)
        if (population and row.get('_PID') == '1' and row.get('UNIT') and row.get('MESSAGE_ID') in LIFECYCLE - {STARTING, STOPPING}
                and row.get('JOB_RESULT') and re.fullmatch('[1-9][0-9]*', str(row.get('JOB_ID', '')))):
            installed.add(int(row['JOB_ID']))
        if original and identified:
            native = row.get('_PID') == '1'
            inside |= (native and row.get('_TRANSPORT') == 'journal' and row.get('PRIORITY') == '6' and row.get('_SYSTEMD_UNIT') == 'init.scope'
                       and wall <= original[1] and re.fullmatch(r'Reloading finished in [0-9]+ ms\.', row.get('MESSAGE', '')) is not None)
            resolved |= (native and row.get('_TRANSPORT') == 'journal' and wall > original[1]
                         and row.get('_SYSTEMD_UNIT') == 'init.scope' and row.get('PRIORITY') in ('5', '6'))
    require(journald_generation() == journal_daemon, 'journal daemon changed across evidence interval')
    require(expected == last + 1 and (not original or inside and resolved) and uuid.UUID(boot_path.read_text().strip()).hex == boot,
            'missing manager journal coverage')
    if original:
        require(not original[2] or not original[3] or admitted < original[3], 'manager rate-limit pool may have saturated')
    require(all((name, STARTED) in seen for name in approved), 'approved completion missing from journal segment')
    for unit_name in approved:
        require((unit_name, 'start') in containers and (unit_name != MIGRATOR + '.service' or (unit_name, 'died') in containers),
                'missing rootful container execution')
    if population:
        start, end = population
        epoch = start['reload']
        require(len(reload_completions) == len(reloads), 'unapproved manager reload')
        for change, finished in zip(reloads, reload_completions, strict=True):
            require(change['before'] == epoch < change['after'] <= finished, 'unbound manager reload')
            epoch = change['after']
        require(epoch == end['reload'], 'unapproved manager reload')
        require(start['version'] == end['version'], 'manager implementation changed across job boundary')
        require(end['ceiling'] - start['installed'] >= len(installed.difference(start['pending'])),
                'unaccounted manager job installation')


BUS = ('busctl', '--system', '--json=short')
SYSTEMD = ('org.freedesktop.systemd1', '/org/freedesktop/systemd1', 'org.freedesktop.systemd1.Manager')


def manager_call(method, *args):
    return decode(run(*BUS, 'call', *SYSTEMD, method, *args))['data']


def manager_version():
    prop = decode(run(*BUS, 'get-property', *SYSTEMD, 'Version'))
    version = prop['data']
    require(prop['type'] == 's' and isinstance(version, str) and re.fullmatch(r'257(?:[^0-9].*)?', version),
            'unreviewed manager job allocator')
    return version


def installed_jobs():
    row = decode(run(*BUS, 'get-property', *SYSTEMD, 'NInstalledJobs'))
    require(row['type'] == 'u' and type(row['data']) is int, 'missing manager installation counter')
    return row['data']


def pending_jobs():
    jobs, = manager_call('ListJobs')
    require(not any(row[1] in UNITS for row in jobs), 'pending schema or writer job')
    return sorted(row[0] for row in jobs)


def reload_epoch():
    row = decode(run(*BUS, 'get-property', *SYSTEMD, 'UnitsLoadTimestampMonotonic'))
    require(row['type'] == 't' and type(row['data']) is int and row['data'] > 0, 'missing manager reload boundary')
    return row['data']


def manager_logging():
    level, target = (decode(run(*BUS, 'get-property', *SYSTEMD, key)) for key in ('LogLevel', 'LogTarget'))
    require(level['type'] == target['type'] == 's' and level['data'] in ('info', 'debug')
            and target['data'] in ('journal', 'journal-or-kmsg')
            and all(run('systemctl', 'show', name, '-p', 'LogLevelMax', '--value') in ('-1', '6', '7') for name in sorted(UNITS)),
            'protected manager records may be suppressed')


def job_boundary():
    epoch = reload_epoch()
    version = manager_version()
    manager_logging()
    installed = installed_jobs()
    pending = pending_jobs()
    run('journalctl', '--sync')
    cursor = journal_tip()
    ceiling = installed_jobs()
    require(reload_epoch() == epoch, 'manager changed during journal boundary')
    return dict(version=version, installed=installed, ceiling=ceiling, pending=pending, cursor=cursor, reload=epoch)


def fresh_confirmation(boundary):
    age = time.time_ns() // 1000 - cursor_fields(boundary['cursor'])[3]
    require(0 <= age <= 30_000_000, 'final confirmation cursor expired')


def ready_writers(candidate, writers):
    for name, generation in writers.items():
        sealed('sealed_ready', name, candidate, generation[1])
    live_peers(writers)


def recovery_job(operation, unit_name, directory, prepare, send):
    require((operation, unit_name) in {('start', MIGRATOR + '.service'), *(('try-restart', n + '.service') for n in WRITERS)},
            'unexpected recovery job')
    def enqueue(name, kind):
        job, path, unit, _, actual, affected = decode(send(*BUS, 'call', *SYSTEMD, 'EnqueueUnitJob', 'sss', name, kind, 'ignore-dependencies'))['data']
        require(type(job) is int and 0 < job < 2**32 - 1 and unit == name and path == f'/org/freedesktop/systemd1/job/{job}'
                and actual == ('restart' if kind == 'try-restart' else kind) and affected == [],
                'ambiguous manager transaction')
        return job, actual
    def persist(name, value):
        with (directory / name).open('x') as proof:
            print(value, file=proof)
            proof.flush()
            os.fsync(proof.fileno())
        fd = os.open(directory, os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    version = manager_version()
    prepare()
    jobs, = manager_call('ListJobs')
    require(not any(row[1:3] == ['init.scope', 'nop'] for row in jobs), 'pending manager allocation witness')
    marker, _ = enqueue('init.scope', 'nop')
    persist('allocation', marker)
    job, kind = enqueue(unit_name, operation)
    persist('manager-job', f'{job} {kind}')
    require(marker < job and manager_version() == version, 'merged or ambiguous recovery job allocation')
    deadline = time.monotonic() + 600
    while any(row[0] == job for row in manager_call('ListJobs')[0]):
        require(time.monotonic() < deadline, 'manager job completion deadline')
        time.sleep(.1)
    run('journalctl', '--sync')
    return f'Enqueued anchor job {job} {unit_name}/{kind}.'


def loaded_before(path, loaded_ns):
    try:
        infos = path.lstat(), path.stat()
    except FileNotFoundError:
        infos = next(parent.stat() for parent in path.parents if parent.exists()),
    return all(info.st_ctime_ns <= loaded_ns for info in infos)


def manager_rate_limit(start_ns, journal_daemon):
    interval, burst = 30_000_000, 10_000
    loaded_ns = time.time_ns() - time.clock_gettime_ns(time.CLOCK_MONOTONIC) + journal_daemon[1] * 1000
    for root in ('/etc', '/run', '/usr/local/lib', '/usr/lib'):
        for name in ('journald.conf', 'journald.conf.d'):
            path = Path(root, 'systemd', name)
            require((root == '/run' and not path.exists(follow_symlinks=False)) or loaded_before(path, loaded_ns),
                    'original journal policy unavailable')
    config = run('systemd-analyze', '--no-pager', 'cat-config', 'systemd/journald.conf')
    section = ''
    for line in config.splitlines():
        line = line.strip()
        if line.startswith('# /') and line.endswith('.conf'):
            section = ''
            require(loaded_before(Path(line[2:]), loaded_ns), 'original journal policy unavailable')
        if line.startswith('['):
            section = line
        key, _, value = line.partition('=')
        if section != '[Journal]':
            continue
        if key.strip() in ('RateLimitIntervalSec', 'RateLimitInterval'):
            match = re.fullmatch(r'(\d+(?:\.\d+)?)\s*(us|ms|s|sec|min|h)?',value.strip())
            require(match, 'unknown manager journal rate interval')
            interval = int(Decimal(match[1]) * {None:10**6,'s':10**6,'sec':10**6,'us':1,'ms':1000,'min':60*10**6,'h':3600*10**6}[match[2]])
        elif key.strip() == 'RateLimitBurst':
            burst = int(value.strip())
    for kind in ('interval', 'burst'):
        path = Path('/run/systemd/units/log-rate-limit-' + kind + ':init.scope')
        try:
            info = path.lstat()
        except FileNotFoundError:
            continue
        require(stat.S_ISLNK(info.st_mode) and info.st_ctime_ns <= start_ns, 'original init.scope rate limit unavailable')
        value = int(os.readlink(path))
        if kind == 'interval':
            interval = value
        else:
            burst = value
    require(0 <= interval <= 86400 * 10**6 and 0 <= burst <= 10**7, 'unknown manager journal rate limits')
    return interval, burst


def journal(start_ns, end_ns):
    start, end = start_ns // 1000, (end_ns + 999) // 1000
    require(run('podman','info','--format','{{.Host.EventLogger}}') == 'journald', 'rootful Podman journal unavailable')
    journal_daemon = journald_generation()
    interval, burst = manager_rate_limit(start_ns, journal_daemon)
    run('journalctl','--sync')
    anchor, = journal_rows((*JOURNAL, '--until=' + instant(start - interval - 1), '-n', '1', '_PID=1'))
    closing = journal_tip()
    require(0 < int(anchor['__REALTIME_TIMESTAMP']) < start <= end <= cursor_fields(closing)[3],
            'missing manager journal coverage')
    journal_segment(anchor['__CURSOR'], closing, {}, (start, end, interval, burst))
    journal_segment(closing, journal_tip(), {}, original_tail=True)
    require(journald_generation() == journal_daemon, 'journal daemon changed across evidence interval')


def prove(source, writer, failed):
    require(os.geteuid() == os.getegid() == 0 and hexid(source, 40) and hexid(writer, 40) and source != writer
            and release_name(API, failed), 'invalid supersession selectors')
    peers, image, manifest, inventory, source_end = source_proof(source, writer)
    config, protected, start, diagnostic, end = request_proof(source, failed, peers, image, manifest, source_end)
    active = configuration(config, inventory, peers, writer, start, diagnostic)
    for pair, _ in active.values():
        legacy_release(pair)
    failed_release(failed, source, writer, peers, active, image)
    live_peers(peers)
    infrastructure(source, failed, peers, protected['protected_file_pairs'], start)
    ledger(source, writer, peers, protected)
    journal(start, end)
    writers = [sealed('sealed_generation', name, source) for name in WRITERS]
    require(writers == [' '.join([name, *peers[name][:3], ENDPOINT]) for name in WRITERS], 'live pooled writer generation changed')
    exact(RELEASES / failed / 'unchanged-peer', writers[1])
    return writers


def stage_for(service, revision):
    slot = {API: 'api', WORKER: 'worker'}[service]
    return STAGES / ('dv-holo-' + slot + '-' + revision)


def generation_line(service, gen):
    generation_shape(service, gen)
    return ' '.join((service, *gen[:3], ENDPOINT))


MIGRATOR_OVERLAY = QUADLET / (MIGRATOR + '.container.d/50-migrator-image.conf')
FENCES = {'/var/lib/hololive-postgres-fence/fenced', '/var/lib/hololive-postgres-fence/fence.intent'}
HOOKS = ('ExecCondition', 'ExecStartPre', 'ExecStart', 'ExecStartPost', 'ExecReload', 'ExecStop', 'ExecStopPost')


def generated_commands(path):
    hooks = {k: [] for k in HOOKS}
    conditions = []
    section = ''
    for line in raw(path).decode().splitlines():
        if line.startswith('['):
            section = line
        key, _, value = line.partition('=')
        if section == '[Unit]' and key.startswith(('Condition', 'Assert')):
            require(key == 'ConditionPathExists' and value.startswith('!'), 'unreviewed migrator prerequisite')
            conditions.append(value[1:])
        if section == '[Service]' and key in hooks:
            if value:
                hooks[key].append(shlex.split(value))
            else:
                hooks[key].clear()
    require(len(conditions) == len(FENCES) and set(conditions) == FENCES, 'missing or duplicate unit fence')
    return hooks


def canonical_commands(hooks, root, image):
    require(all(not hooks[k] for k in ('ExecCondition', 'ExecStartPost', 'ExecReload'))
            and all(len(hooks[k]) == 1 for k in ('ExecStart', 'ExecStop', 'ExecStopPost'))
            and hooks['ExecStartPre'] == [['/usr/bin/bash', str(root / 'scripts/deploy/quadlet-prestart.sh'), MIGRATOR, 'schema']],
            'unreviewed migrator hooks')
    start, = hooks['ExecStart']
    require(start[:2] == ['/usr/bin/podman', 'run'] and start[-2:] == [image, '--check'] and start.count(image) == 1
            and any('hololive-bot_holo-pg-admin-socket:/var/run/postgresql:ro' in arg for arg in start)
            and not any('/migrations' in arg or 'MIGRATION_' in arg for arg in start), 'unreviewed migrator command')
    options = {'--pids-limit': '256', '--stop-timeout': '10', '--pull': 'never'}
    indices = [i for i, arg in enumerate(start) if arg.split('=', 1)[0] in options]
    require(len(indices) == 3, 'ambiguous generated migrator options')
    first = indices[0]
    require(indices == [first, first + 2, first + 4] and 2 <= first <= len(start) - 8 and '--' not in start[:first + 6]
            and {start[i] for i in indices} == options.keys() and all(start[i + 1] == options[start[i]] for i in indices),
            'unreviewed generated migrator option group')
    canonical = dict(hooks, ExecStart=[start[:first] + [a for item in options.items() for a in item] + start[first + 6:-2] + ['IMAGE', '--check']],
                     ExecStartPre=[['/usr/bin/bash', 'PINNED-PRESTART', MIGRATOR, 'schema']])
    return digest(json.dumps(canonical, sort_keys=True, separators=(',', ':')).encode())


def manager(interface, key, signature, name=MIGRATOR):
    prop = decode(run('busctl', '--json=short', 'get-property', 'org.freedesktop.systemd1',
                      '/org/freedesktop/systemd1/unit/' + name.replace('-', '_2d') + '_2eservice', 'org.freedesktop.systemd1.' + interface, key))
    require(prop['type'] == signature and isinstance(prop['data'], list), 'invalid manager property: ' + key)
    return prop['data']


def effective_commands(name, hooks):
    require(not manager('Unit', 'Asserts', 'a(sbbsi)', name), 'unexpected unit assertion: ' + name)
    conditions = manager('Unit', 'Conditions', 'a(sbbsi)', name)
    require(len(conditions) == len(FENCES) and all(len(row) == 5 and row[:3] == ['ConditionPathExists', False, True] for row in conditions)
            and {row[3] for row in conditions} == FENCES, 'loaded unit fence conditions differ: ' + name)
    require(not manager('Unit', 'DropInPaths', 'as', name), 'unexpected native unit override: ' + name)
    for key, entries in hooks.items():
        wanted = []
        for argv in entries:
            ignore = argv[0].startswith('-')
            exe = argv[0].removeprefix('-')
            require(exe.startswith('/') and (not ignore or key in ('ExecStop', 'ExecStopPost')), 'unexpected command prefix')
            wanted.append([exe, [exe, *argv[1:]], ignore])
        rows = manager('Service', key, 'a(sasbttttuii)', name)
        require([r[:3] for r in rows] == wanted, 'loaded unit command differs from reviewed generation: ' + name + '/' + key)


def rendered_unit(data, base):
    lines = []
    options = {'--pids-limit','--pull','--memory','--stop-timeout','--group-add','--dns','--tmpfs','--add-host','--env','--label','--annotation'}
    for line in data.decode().splitlines():
        if not line.strip() or line.lstrip().startswith(('#',';')):
            continue
        if line == 'SourcePath=' + str(base):
            line = ('SourcePath', base.name)
        elif line.startswith('ExecStart=/usr/bin/podman run '):
            argv = shlex.split(line)
            normalized = []
            index = 0
            while index < len(argv):
                if argv[index] not in options:
                    normalized.append(argv[index])
                    index += 1
                    continue
                groups = {}
                while index < len(argv) and argv[index] in options:
                    groups.setdefault(argv[index], []).append(argv[index+1])
                    index += 2
                normalized.append(sorted(groups.items()))
            line = normalized
        lines.append(line)
    return lines


def writer_definition(service, candidate, image, pair, inventory, installed_pair=None):
    source = STORE / candidate / 'deploy/quadlet/hololive-bot'
    base = source / (service + '.container')
    installed_pair = installed_pair or pair
    sealed('sealed_installed_pair', service, candidate, installed_pair, QUADLET)
    require(raw(base) == raw(pair / 'unit.container') == raw(installed_pair / 'unit.container'), 'writer base differs from sealed source')
    directory = QUADLET / (base.name + '.d')
    authority(directory, True)
    pinned = {}
    for line in inventory.splitlines():
        if line.startswith(os.fsencode(directory) + b'/'):
            path, _, _, _, _, _, checksum = line.decode().split()
            path = Path(path)
            require(path.parent == directory and path.name not in pinned and hexid(checksum,64), 'invalid protected writer drop-in')
            pinned[path.name] = checksum
    profile = '50-worker-profile.conf'
    require(profile in pinned and set(pinned) == {p.name for p in directory.iterdir()}, 'installed writer drop-in set changed')
    for retained in {pair, installed_pair}:
        require(retained.name == 'candidate' and retained.parent.parent == RELEASES and release_name(service, retained.parent.name),
                'unexpected retained writer reference')
        metadata = decode(raw(retained / 'pair.json'))
        if 'source_generation' in metadata:
            require(metadata['source_generation'] == candidate == metadata['revision'], 'unapproved writer profile generation')
            intent = raw(retained.parent / 'activation.intent').decode().splitlines()
            require(all(intent.count(digest(raw(retained / name)) + '  candidate/' + name) == 1
                        for name in ('pair.json', profile)), 'writer profile differs from activation intent')
        else:
            require(digest(raw(retained / profile)) == pinned[profile], 'original writer profile differs from protected inventory')
    with tempfile.TemporaryDirectory(dir='/run') as tmp:
        inputs, output = Path(tmp,'inputs'), Path(tmp,'generated')
        inputs.mkdir()
        output.mkdir()
        drops = inputs / directory.name
        drops.mkdir()
        for path in (base, source / 'hololive-net.network'):
            (inputs / path.name).write_bytes(raw(path))
        for name, checksum in pinned.items():
            data = raw(directory / name)
            if name == profile:
                require(data == raw(installed_pair / name), 'installed writer profile changed')
                data = raw(pair / name)
            else:
                require(digest(data) == checksum, 'protected writer drop-in changed: ' + name)
            (drops / name).write_bytes(data)
        run('/usr/bin/env', 'QUADLET_UNIT_DIRS=' + str(inputs), '/usr/libexec/podman/quadlet', str(output))
        generated = Path('/run/systemd/generator') / (service + '.service')
        require(run('systemctl','show',service+'.service','--property=FragmentPath','--value') == str(generated) and
                rendered_unit(raw(generated), QUADLET/base.name) == rendered_unit(raw(output/generated.name), inputs/base.name),
                'generated writer differs from approved rendering: ' + service)
        hooks = generated_commands(generated)
        require(len(hooks['ExecStart']) == 1 and hooks['ExecStart'][0][-1] == image, 'unapproved writer image')
        effective_commands(service, hooks)


def pre_request(witness, writers, definitions, epoch, alive, changed_overlay=False):
    baseline = witness['boundary']
    require(manager_version() == baseline['version'] and baseline['reload'] == epoch == reload_epoch(),
            'manager reload since last definition verification')
    closing = job_boundary()
    journal_segment(baseline['cursor'], closing['cursor'], {}, population=(baseline, closing))
    definitions()
    if not changed_overlay:
        require(overlay_identity() == witness['overlay'], 'overlay changed before manager request')
    live_peers(writers)
    latest = job_boundary()
    journal_segment(closing['cursor'], latest['cursor'], {}, population=(closing, latest))
    require(reload_epoch() == epoch, 'manager changed during pre-dispatch checks')
    alive()


def migrator_witness(root, image, rendered=None, installation=None):
    root = Path(root)
    require(root.parent == STORE and hexid(root.name, 40) and re.fullmatch('sha256:[0-9a-f]{64}', image) and LEGACY not in LIB.parents,
            'invalid migrator selectors')
    authority(LIB / 'sealed-app.sh')
    overlay = run('/usr/bin/bash', '--noprofile', '--norc', '-c', 'set -eu; . "$1"; sealed_migrator_dropin "$2" "$3"',
                  'evidence', LIB / 'sealed-app.sh', *(installation or (root, image)))
    exact(MIGRATOR_OVERLAY, overlay)
    require(stat.S_IMODE(authority(MIGRATOR_OVERLAY).st_mode) == 0o644, 'unsafe migrator definition')
    fragment = Path('/run/systemd/generator') / (MIGRATOR + '.service')
    require(run('systemctl', 'show', MIGRATOR + '.service', '--property=FragmentPath', '--value') == str(fragment),
            'unrecognized generated migrator')
    hooks = generated_commands(fragment)
    fingerprint = canonical_commands(hooks, root, image)
    if rendered is not None:
        require(canonical_commands(generated_commands(Path(rendered)), root, image) == fingerprint,
                'rendered and generated migrator commands differ')
    effective_commands(MIGRATOR, hooks)
    state = run('systemctl', 'show', '--all', MIGRATOR + '.service', '-p', 'LoadState', '-p', 'ActiveState', '-p', 'SubState',
                '-p', 'Job', '-p', 'NeedDaemonReload', '-p', 'Type', '-p', 'Restart', '-p', 'RestartForceExitStatus').splitlines()
    require(set(state) - {'NeedDaemonReload=yes' if installation else 'NeedDaemonReload=no', 'NeedDaemonReload=no'} ==
            {'ActiveState=inactive','Job=','LoadState=loaded','SubState=dead','Type=oneshot','Restart=no','RestartForceExitStatus='}
            and len(state) == 8, 'unstarted migrator is not idle and current or may restart itself')
    return fingerprint


def supervisor_functions(candidate):
    require(hexid(candidate, 40), 'invalid supervisor revision')
    tool = STAGES / ('dv-tool-' + candidate) / 'source'
    root = LIB.parents[2]
    require(root in (STORE / candidate, tool) and LIB == root / 'scripts/deploy/lib', 'unrecognized supervisor root')
    relative = 'scripts/deploy/supersession-window.py'
    path = root / relative
    data = raw(path)
    authority(tool / '.git', True)
    authority(tool / '.git/config')
    pinned = run('/usr/bin/env', 'GIT_CONFIG_NOSYSTEM=1', 'GIT_CONFIG_GLOBAL=/dev/null', 'GIT_NO_REPLACE_OBJECTS=1',
                 'GIT_NO_LAZY_FETCH=1', 'GIT_ALLOW_PROTOCOL=', '/usr/bin/git', '--no-pager', '--git-dir=' + str(tool / '.git'),
                 'rev-parse', '--verify', candidate + ':' + relative)
    require(hashlib.sha1(b'blob ' + str(len(data)).encode() + b'\0' + data).hexdigest() == pinned, 'unreviewed supervisor module')
    scope = {'__file__': str(path), '__name__': 'supersession_window'}
    exec(compile(data, str(path), 'exec'), scope)
    return scope


def window(phase, old, candidate, *selectors):
    require(os.geteuid() == os.getegid() == 0 and hexid(old, 40) and hexid(candidate, 40) and old != candidate,
            'invalid supervisor selectors')
    if phase == 'publish':
        running, failed = selectors
        expected = STAGES / ('dv-tool-' + candidate) / 'source/scripts/deploy/lib'
        require(release_name(API, failed), 'invalid original release')
        phase = 'source'
    else:
        require(not selectors and phase in WRITERS, 'invalid supervisor slot')
        running = raw(LEGACY / '.source-revision').decode().removesuffix('\n')
        failed = None
        expected = STORE / candidate / 'scripts/deploy/lib'
        phase = 'api' if phase == API else 'worker'
    require(LIB == expected and hexid(running, 40) and running not in (old, candidate), 'incorrect supervisor authority')
    supervisor_functions(candidate)['verify_dispatch'](phase, old, candidate, running, failed)


def source_window(old, candidate, writer, peers, old_end, successor=None):
    tool = STAGES / ('dv-tool-' + candidate)
    transition = STORE / ('transition-' + old + '-' + candidate)
    exact(transition / 'revisions', f'installed={old}\ncandidate={candidate}\ntool={candidate}\nrunning={writer}')
    exact(transition / 'predecessor.writers', '\n'.join(generation_line(n, peers[n]) for n in WRITERS))
    absent(tool / 'source.not-accepted')
    exact(tool / 'source.accepted', str(transition))
    before_peers = raw(tool / 'source.peers.before')
    require(decode(before_peers) == peers and raw(tool / 'source.peers.after') == before_peers, 'source window peers changed')
    requested, info = record(tool / 'source.requested')
    require(re.fullmatch(rb'\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\+00:00\n', requested), 'invalid source request')
    before = decode(raw(tool / 'source.before.json'))
    require(before['active']['state'] == 'old' and before['transition'] == before['new'] == {'state': 'absent'},
            'source transition was not new')
    after = decode(raw(tool / 'source.after.json'))
    require(hexid(after['migrator_commands'], 64) and after['migrator_commands'] == before['migrator_commands'],
            'migrator changed during publication')
    exit_record = decode(raw(tool / 'source.coordinator.exit'))
    ended = exit_record['ended_utc_ns']
    accepted = authority(tool / 'source.accepted').st_mtime_ns
    require(type(exit_record['returncode']) is int and type(ended) is int
            and exit_record == {'returncode': 0, 'markers': [], 'ended_utc_ns': ended}
            and old_end <= info.st_mtime_ns <= ended <= accepted <= time.time_ns(), 'source publisher did not succeed')
    require(after['revisions_match'] is True, 'publisher did not confirm recovered revisions')
    for key, path, value, state in [('active', STORE / 'active', candidate, 'new'),
                                   ('new', STORE / candidate / '.source-revision', candidate, 'new'),
                                   ('old', STORE / old / '.source-revision', old, 'old')]:
        data, info = record(path)
        if key == 'active' and successor is not None:
            require(hexid(successor, 40) and successor not in (candidate, old, writer)
                    and data == f'incomplete {candidate} -> {successor}\n'.encode() and after[key]['state'] == 'new',
                    'not committing the selected follow-on source')
            continue
        if key != 'active':
            require(stat.S_IMODE(authority(path.parent, True).st_mode) == 0o500 and stat.S_IMODE(info.st_mode) == 0o400,
                    'unsealed source generation')
        require(data == (value + '\n').encode() and after[key] == dict(state=state, inode=[info.st_dev, info.st_ino]),
                'source pointer differs from publication: ' + key)
    exact(LEGACY / '.source-revision', writer)
    for key, path, row in [('store', STORE, after), ('transition', transition, after),
                           ('previous.actual', transition / 'previous.actual', after['receipts'])]:
        info = authority(path, True)
        require(stat.S_IMODE(info.st_mode) == 0o700 and row[key] == dict(state='directory', inode=[info.st_dev, info.st_ino]),
                'source directory differs from publication')
    for name in RECOVERY_RECEIPTS:
        if name != 'previous.actual':
            data, info = record(transition / name)
            require(after['receipts'][name] == dict(state='file', bytes=len(data), sha256=digest(data), inode=[info.st_dev, info.st_ino]),
                    'source receipt differs from publication: ' + name)
    for left, right in [('images', 'images.after'), ('previous.metadata', 'previous.after'), ('protected.before', 'protected.after')]:
        require(after['receipts'][left]['sha256'] == after['receipts'][right]['sha256'], 'unconfirmed publisher comparison')
    require(raw(transition / 'pairs.before') == raw(transition / 'pairs.after'), 'source predecessor pair comparison failed')
    return manifest_pair(candidate, transition)[0], accepted


def recovery_context(old, candidate, *, terminal=False, successor=None):
    require(os.geteuid() == os.getegid() == 0 and hexid(old, 40) and hexid(candidate, 40) and candidate != old,
            'invalid recovery revisions')
    writer = raw(LEGACY / '.source-revision').decode().removesuffix('\n')
    require(hexid(writer, 40) and writer not in (old, candidate), 'invalid original running revision')
    old_tool = STAGES / ('dv-tool-' + old)
    absent(old_tool / 'source.not-accepted')
    original_peers = raw(old_tool / 'source.peers.before')
    peers = decode(original_peers)
    require(peers.keys() == PEERS and raw(old_tool / 'source.peers.after') == original_peers, 'incomplete first peer inventory')
    first = STORE / ('transition-' + writer + '-' + old)
    exact(old_tool / 'source.accepted', str(first))
    exact(first / 'revisions', f'installed={writer}\ncandidate={old}\ntool={old}')
    _, images, manifests = manifest_pair(old, first, writer)
    failed, = decode(raw(stage_for(API, old) / 'activation.not-accepted'))['original_jobs']
    require(release_name(API, failed), 'unbound original failed release')
    _, protected, start, _, end, _ = original_attempt(old, failed, peers, images[API], manifests[API],
                                                      authority(old_tool / 'source.accepted').st_mtime_ns)
    pair, accepted = source_window(old, candidate, writer, peers, end, successor)
    if not terminal:
        require(time.time_ns() - time.clock_gettime_ns(time.CLOCK_BOOTTIME) <= start, 'original attempt outside this boot')
        live_peers({n: peers[n] for n in PEERS - set(WRITERS)})
        infrastructure(old, failed, peers, protected['protected_file_pairs'], start)
        ledger(old, writer, peers, protected)
    return writer, peers, protected, pair, accepted


def slot_context(old, candidate):
    context = recovery_context(old, candidate)
    supervisor_functions(candidate)['verify_transport']('source', old, candidate, context[0])
    return context


def stage_request(service, candidate, entry, old, protected, earliest):
    stage = stage_for(service, candidate)
    config, info = record(stage / 'activation.config-before')
    paths = {str(QUADLET / (MIGRATOR + '.container.d/50-migrator-image.conf'))}
    for name in WRITERS:
        paths.update((str(PROFILES / 'active' / (name + '.pair')), str(QUADLET / (name + '.container.d/50-worker-profile.conf'))))
    config = decode(config)
    require(config.keys() == paths and all(isinstance(v, list) and len(v) == 3 and hexid(v[0], 64)
            and all(type(i) is int and i > 0 for i in v[1:]) for v in config.values()), 'missing external configuration witness')
    data, request_info = record(stage / 'activation.requested')
    request = decode(data)
    start = request['utc_ns']
    require(request.keys() == {'source', 'tool', 'utc_ns', 'prior', 'protected', 'old', 'image', 'manifest'}
            and type(start) is int and earliest <= info.st_mtime_ns <= start <= request_info.st_mtime_ns <= time.time_ns()
            and request['tool'] == request['source'] == candidate and request['old'] == old and request['protected'] == protected
            and request['image'] == entry['image'] and request['manifest'] == entry['manifest'], 'external slot request mismatch')
    prior = request['prior']
    require(isinstance(prior, list) and prior and all(release_name(service, n) for n in prior) and prior == sorted(set(prior)),
            'unrecognized external release inventory')
    authority(RELEASES, True)
    present = {p for p in os.listdir(RELEASES) if p.startswith(service + '-v2-')}
    fresh = present - set(prior)
    require(set(prior) <= present and len(fresh) <= 1, 'multiple or missing external releases')
    for name in prior:
        revision, stamp = record(RELEASES / name / 'candidate.revision')
        require(re.fullmatch(b'[0-9a-f]{40}\n', revision) and revision != (candidate + '\n').encode()
                and stamp.st_mtime_ns < start, 'prior slot already attempted')
    release = None
    if fresh:
        name, = fresh
        require(release_name(service, name), 'invalid new release')
        release = RELEASES / name
        exact(release / 'service', service)
        exact(release / 'candidate.revision', candidate)
        require(authority(release / 'candidate.revision').st_mtime_ns >= start, 'release predates slot request')
    return request, release


def job_receipt(directory, unit_name, operation, image, minimum):
    data = raw(directory / 'request').decode()
    match = re.fullmatch(re.escape(f'{operation} {unit_name} {image} ') + r'([1-9][0-9]*\.[0-9]{6})\n', data)
    require(match, 'unbound unit job request')
    since = match[1]
    ns = int(since.replace('.', '')) * 1000
    require(minimum <= ns <= time.time_ns(), 'job outside original slot window')
    anchor = re.fullmatch(r'([1-9][0-9]*) (start|restart|try-restart) 0\n', raw(directory / 'anchor').decode())
    require(anchor and (anchor[2] == operation or (operation, anchor[2]) == ('try-restart', 'restart')), 'unbound job anchor')
    invocation = raw(directory / 'invocation').decode().removesuffix('\n')
    require(hexid(invocation, 32), 'invalid recorded invocation')
    exact(directory / 'invocation', invocation)
    row = decode(run('journalctl', '--boot', '--quiet', '--no-pager', '-o', 'json', '--since=@' + since,
                     '--output-fields=INVOCATION_ID,JOB_ID,UNIT,JOB_RESULT', '_PID=1', 'UNIT=' + unit_name, 'JOB_ID=' + anchor[1],
                     'JOB_RESULT=done', 'MESSAGE_ID=39f53479d3a045ac8e11786248231fbf'))
    require((row['JOB_ID'], row['UNIT'], row['JOB_RESULT'], row['INVOCATION_ID']) == (anchor[1], unit_name, 'done', invocation),
            'job receipt differs from original manager completion')
    marker = re.fullmatch(r'([1-9][0-9]*)\n', raw(directory / 'allocation').decode())
    require(marker and int(marker[1]) < int(anchor[1]), 'unbound job allocation')
    allocation = int(marker[1])
    exact(directory / 'manager-job', f'{anchor[1]} {anchor[2]}')
    return dict(job=int(anchor[1]), kind=anchor[2], allocation=allocation, invocation=invocation, request_utc_ns=ns, since=since)


def slot_records(candidate, service, request, pair, release):
    require(service in WRITERS and hexid(candidate, 40) and release.parent == RELEASES and release_name(service, release.name),
            'invalid accepted release')
    for slot in ('previous', 'candidate'):
        readable_pair(release / slot)
    old, image = request['old'], request['image']
    for name in ('recovery-attempt', 'restore-original', 'rollback-job'):
        absent(release / name)
    api_image = pair['entries'][0]['image']
    stage = stage_for(service, candidate)
    data = raw(stage / 'manifest.json')
    manifest = decode(data)
    entry = pair['entries'][WRITERS.index(service)]
    require(digest(data) == entry['manifest'] == request['manifest'] and manifest['image'] == image == entry['image']
            and manifest['revision'] == candidate == pair['revision'], 'accepted manifest mismatch')
    exact(stage / 'stage.accepted', entry['manifest'])
    for key, value in [('service', service), ('candidate.revision', candidate), ('candidate.image', image), ('migrator.image', api_image)]:
        exact(release / key, value)
    exact(release / 'before.generation', generation_line(service, old))
    exact(PROFILES / 'active' / (service + '.pair'), str(release / 'candidate'))
    previous = decode(raw(release / 'previous/pair.json'))
    current = pair_metadata(release / 'candidate', service, candidate, image)
    require(previous['image'] == 'sha256:' + old[2] and previous['container_id'] == old[1]
            and previous['profile_hash'] == current['profile_hash'], 'wrong accepted profile pair')
    coord = raw(STORE / candidate / 'scripts/deploy/quadlet-redeploy-service.sh')
    blob = hashlib.sha1(b'blob ' + str(len(coord)).encode() + b'\0' + coord).hexdigest()
    files = ['rollback.conf', 'rollback.sha256', 'previous.endpoint', 'before.pair', 'before.conf', 'activation.intent',
             'schema-check', 'schema-job/migrator-overlay', 'candidate/50-worker-profile.conf']
    files.extend(('reload-schema','reload-writer'))
    files.extend(f'{job}/{key}' for job in ('schema-job', 'writer-job') for key in ('request', 'anchor', 'invocation', 'manager-job', 'allocation'))
    files.extend(f'{slot}/{key}' for slot in ('previous', 'candidate')
                 for key in ('profile.json', 'pair.json', 'SHA256SUMS', 'unit.container'))
    hashes = {name: digest(raw(release / name)) for name in files}
    return manifest, dict(service=service, revision=candidate, image=image, manifest=entry['manifest'], receipt=str(release),
                          profile=current['profile_hash'], previous=previous['image'], pair=pair,
                          ledger=request['protected']['ledger'], coordinator=blob, release_sha256=hashes)


def accepted_generation(service, request, release, gen):
    generation_shape(service, gen)
    old = request['old']
    require(gen[0] != old[0] and gen[1] != old[1] and gen[2] == request['image'][7:], 'writer not replaced')
    exact(release / 'accepted-writer', generation_line(service, gen))


def slot_journal(candidate, service, closing, approved, release):
    if service == API:
        start = STAGES / ('dv-tool-' + candidate) / 'source.after.json'
    else:
        start = stage_for(API, candidate) / 'activation.accepted'
    baseline = decode(raw(start))['manager_jobs']
    reloads = []
    if release:
        for step in ('reload-schema','reload-writer'):
            try:
                reloads.append(decode(raw(release / step)))
            except FileNotFoundError:
                break
    journal_segment(baseline['cursor'], closing['cursor'], approved, population=(baseline, closing), reloads=reloads)


def overlay_identity():
    data, info = record(MIGRATOR_OVERLAY)
    return dict(dev=info.st_dev, ino=info.st_ino, ctime_ns=info.st_ctime_ns, sha256=digest(data))


def accepted_slot(candidate, service, request, pair, release, closing):
    manifest, recorded = slot_records(candidate, service, request, pair, release)
    image = request['image']
    api_image = pair['entries'][0]['image']
    gen = [unit(service, 'InvocationID'), *inspect(service, '{{.Id}} {{.Image}} {{.State.Health.Status}}').split()]
    accepted_generation(service, request, release, gen)
    require(inspect(service, '{{index .Config.Labels "org.opencontainers.image.revision"}}') == candidate, 'writer not replaced')
    schema = job_receipt(release / 'schema-job', MIGRATOR + '.service', 'start', api_image, request['utc_ns'])
    overlay = decode(raw(release / 'schema-job/migrator-overlay'))
    require(request['utc_ns'] <= overlay['ctime_ns'] <= schema['request_utc_ns'], 'migrator overlay outside slot')
    match = re.fullmatch(r'([0-9a-f]{64}) ' + re.escape(api_image) + '\n', raw(release / 'schema-check').decode())
    require(match and hexid(match[1], 64), 'invalid schema-check receipt')
    schema_cid = match[1]
    cid, ended = sealed('sealed_schema_evidence', schema['since'], candidate, schema['invocation'], api_image).split()
    schema_end = int(ended)
    require(cid == schema_cid and schema['request_utc_ns'] <= schema_end <= time.time_ns(), 'schema execution evidence changed')
    writer = job_receipt(release / 'writer-job', service + '.service', 'try-restart', image, schema_end)
    schema['cid'] = schema_cid
    writer.update(cid=gen[1], previous_cid=request['old'][1])
    slot_journal(candidate, service, closing, {MIGRATOR + '.service': (schema, None), service + '.service': (writer, request['old'][0])}, release)
    fmt = '{{.State.Pid}} {{.State.ConmonPid}} {{.State.StartedAt.UnixNano}}'
    identity = inspect(gen[1], fmt)
    pid, conmon, started = identity.split()
    require(all(re.fullmatch('[1-9][0-9]*', v) for v in (pid, conmon, started)) and unit(service, 'MainPID') == conmon
            and writer['request_utc_ns'] <= int(started) <= time.time_ns(), 'unbound live writer process')
    binary = API if service == API else 'alarm-worker'
    binary_hash = manifest['binaries'][binary]['sha256']
    proc = Path('/proc') / pid
    with (proc / 'exe').open('rb') as stream:
        initial = os.fstat(stream.fileno())
        require(stat.S_ISREG(initial.st_mode) and os.path.samestat(initial, (proc / 'root/app/bin' / binary).stat())
                and hashlib.file_digest(stream, 'sha256').hexdigest() == binary_hash
                and os.path.samestat(initial, (proc / 'exe').stat()), 'accepted executable changed')
    require(inspect(gen[1], fmt) == identity, 'live writer process changed')
    sealed('sealed_ready', service, candidate, gen[1])
    live_peers({service: gen})
    protected = request['protected']
    proof = dict(schema_job=schema, schema_cid=schema_cid, schema_exit_utc_ns=schema_end, writer_job=writer, migrator_overlay=overlay,
                 started_utc_ns=int(started), cid=gen[1], exe_sha256=binary_hash, protected_bind_pairs=protected['protected_file_pairs'])
    return dict(recorded, generation=gen, proof=proof, journal_cursor=closing['cursor'], manager_jobs=closing)


def completed_slot(old, candidate, service, peers, protected, pair, earliest, terminal=False):
    stage = stage_for(service, candidate)
    request, release = stage_request(service, candidate, pair['entries'][WRITERS.index(service)], peers[service], protected, earliest)
    absent(stage / 'activation.not-accepted')
    require(release is not None, 'accepted release missing')
    exact(STORE / ('transition-' + old + '-' + candidate) / ('attempt-' + service) / 'release', str(release))
    exit_record = decode(raw(stage / 'coordinator.exit'))
    require(type(exit_record['rc']) is int and exit_record == {'rc': 0, 'markers': ['deployed']}
            and decode(raw(stage / 'coordinator.diagnostics')) == [], 'slot was not successfully deployed')
    accepted, info = record(stage / 'activation.accepted')
    actual = decode(accepted)
    if terminal:
        _, recorded = slot_records(candidate, service, request, pair, release)
        gen, proof = actual['generation'], actual['proof']
        accepted_generation(service, request, release, gen)
        exact(release / 'schema-check', proof['schema_cid'] + ' ' + pair['entries'][0]['image'])
        exact(release / 'writer-job' / 'invocation', proof['writer_job']['invocation'])
        require(actual == dict(recorded, generation=gen, proof=proof, journal_cursor=actual['journal_cursor'], manager_jobs=actual['manager_jobs']) and proof['cid'] == gen[1]
                and proof['writer_job']['invocation'] == gen[0] and type(proof['started_utc_ns']) is int,
                'external acceptance differs from recorded proof')
    else:
        require(actual == accepted_slot(candidate, service, request, pair, release, actual['manager_jobs']),
                'external acceptance differs from actual proof')
        inventory = raw(STORE / f'transition-{old}-{candidate}' / 'protected.after')
        writer_definition(service, candidate, request['image'], release / 'candidate', inventory)
        if service == WORKER:
            expected = decode(raw(STAGES / ('dv-tool-' + candidate) / 'source.after.json'))['migrator_commands']
            require(migrator_witness(STORE / candidate, pair['entries'][0]['image']) == expected, 'candidate manager witness changed')
            require(overlay_identity() == actual['proof']['migrator_overlay'], 'accepted worker migrator definition was replaced')
    require(actual['proof']['started_utc_ns'] <= authority(stage / 'coordinator.exit').st_mtime_ns
            <= info.st_mtime_ns <= time.time_ns(), 'external acceptance differs from actual proof')
    return actual, info.st_mtime_ns


def completed_api(old, candidate, peers, original, pair, earliest):
    running = raw(LEGACY / '.source-revision').decode().removesuffix('\n')
    supervisor_functions(candidate)['verify_transport']('api', old, candidate, running)
    actual, accepted = completed_slot(old, candidate, API, peers, original, pair, earliest)
    return actual['generation'], accepted


def worker_protection(peers, original, api):
    protected = {n: peers[n] for n in PEERS - {WORKER}}
    protected.update({API: api, 'ledger': original['ledger'], 'protected_file_pairs': original['protected_file_pairs']})
    return protected


def completed(old, candidate, successor=None):
    require(hexid(candidate, 40) and LIB == STORE / candidate / 'scripts/deploy/lib', 'terminal verifier must be sealed')
    running, peers, protected, pair, earliest = recovery_context(old, candidate, terminal=True, successor=successor)
    verify_chain = supervisor_functions(candidate)['verify_transport_chain']
    verify_chain(old, candidate, running)
    api, accepted = completed_slot(old, candidate, API, peers, protected, pair, earliest, terminal=True)
    completed_slot(old, candidate, WORKER, peers, worker_protection(peers, protected, api['generation']), pair, accepted,
                   terminal=True)
    verify_chain(old, candidate, running)


def slot_migrator(old, candidate, service, release, pair, request, step):
    transition = STORE / ('transition-' + old + '-' + candidate)
    expected = decode(raw(STAGES / ('dv-tool-' + candidate) / 'source.after.json'))['migrator_commands']
    reserved = transition / ('attempt-' + service)
    require(step in ('entry', 'before-overlay', 'schema', 'writer', 'writer-complete') and (release is None) == (step == 'entry'),
            'unexpected recovery release or job phase')
    if step == 'entry':
        absent(reserved)
    else:
        exact(reserved / 'release', str(release))
    approved = {}
    if step in ('writer', 'writer-complete'):
        schema = job_receipt(release / 'schema-job', MIGRATOR + '.service', 'start', pair['entries'][0]['image'], request['utc_ns'])
        require(overlay_identity() == decode(raw(release / 'schema-job/migrator-overlay')), 'schema overlay changed during slot')
        cid, image = raw(release / 'schema-check').decode().split()
        require(hexid(cid, 64) and image == pair['entries'][0]['image'], 'invalid schema execution identity')
        schema['cid'] = cid
        approved[MIGRATOR + '.service'] = schema, None
        if step == 'writer-complete':
            job = job_receipt(release / 'writer-job', service + '.service', 'try-restart', request['image'], schema['request_utc_ns'])
            job.update(cid=inspect(service, '{{.Id}} {{.Image}} {{.State.Health.Status}}').split()[0], previous_cid=request['old'][1])
            approved[service + '.service'] = job, request['old'][0]
    closing = job_boundary()
    slot_journal(candidate, service, closing, approved, release)
    original_overlay = step in ('entry', 'before-overlay')
    if service == API and original_overlay:
        data, s = record(MIGRATOR_OVERLAY)
        metadata = f'{MIGRATOR_OVERLAY} {s.st_dev} {s.st_ino} {s.st_mode} {s.st_mtime_ns} {s.st_ctime_ns} {digest(data)}'.encode()
        rows = [line for line in raw(transition / 'protected.after').splitlines() if line.startswith(os.fsencode(MIGRATOR_OVERLAY) + b' ')]
        require(rows == [metadata], 'original migrator changed since source publication')
        original_image = decode(raw(stage_for(API, old) / 'activation.requested'))['image']
        require(migrator_witness(STORE / old, original_image) == expected, 'original manager witness changed')
        return closing
    require(migrator_witness(STORE / candidate, pair['entries'][0]['image']) == expected, 'candidate manager witness changed')
    if service == WORKER and original_overlay:
        proof = decode(raw(stage_for(API, candidate) / 'activation.accepted'))['proof']
        require(overlay_identity() == proof['migrator_overlay'],
                'accepted API migrator definition was replaced')
    return closing


def slot(old, candidate, service, step='entry'):
    require(service in WRITERS, 'unknown selected slot')
    _, peers, original, pair, earliest = slot_context(old, candidate)
    if service == API:
        for key in (*ATTEMPT, 'activation.accepted'):
            absent(stage_for(WORKER, candidate) / key)
        absent(STORE / ('transition-' + old + '-' + candidate) / ('attempt-' + WORKER))
        protected = original
    else:
        api, earliest = completed_api(old, candidate, peers, original, pair, earliest)
        protected = worker_protection(peers, original, api)
    request, release = stage_request(service, candidate, pair['entries'][WRITERS.index(service)], peers[service], protected, earliest)
    if step == 'entry':
        transition = STORE / ('transition-' + old + '-' + candidate)
        inventory = raw(transition / 'protected.after')
        installed = raw(transition / 'pairs.after').decode().splitlines()
        for index, name in enumerate(WRITERS):
            current = installed[index * 3].split()[1] if name == service else raw(PROFILES / 'active' / (name + '.pair')).decode().strip()
            writer_definition(name, candidate, 'sha256:' + running_generation(name)[2], Path(current), inventory)
    closing = slot_migrator(old, candidate, service, release, pair, request, step)
    for key in ('activation.accepted', 'activation.not-accepted', 'coordinator.exit', 'coordinator.diagnostics'):
        absent(stage_for(service, candidate) / key)
    return dict(boundary=closing, overlay=overlay_identity())


def verify_snapshot(tree, revision, source, kind, backup='', record='', *originals):
    originals = [Path(p) for p in originals]
    copied = kind in ("backup", "sealed-backup")
    expected = {}
    for entry in Path(tree).read_bytes().split(b"\0")[:-1]:
        header, name = entry.split(b"\t", 1)
        mode, _, digest, size = header.decode().split()
        expected[Path(os.fsdecode(name))] = (mode == "100755", digest, int(size))
    stamp = (revision + "\n").encode()
    expected[Path(".source-revision")] = (False, hashlib.sha1(b"blob 41\0" + stamp).hexdigest(), len(stamp))
    excluded = {Path(p) for p in ("data", "logs", "backups", "runtime-config", "artifacts/worker-profile")} if kind in ("legacy", "backup") else set()
    expected = {p: value for p, value in expected.items() if not any(p == e or e in p.parents for e in excluded)}
    dirs = {parent for p in expected for parent in p.parents}
    if kind == "legacy":
        dirs.add(Path("artifacts"))
    root = Path(source)
    seen = set()
    def fail(path):
        sys.exit(f"protected-source {kind} archive or metadata mismatch: {path}")

    def inspect(fd, rel, directory):
        info = os.fstat(fd)
        attrs = os.listxattr(fd)
        mode = stat.S_IMODE(info.st_mode)
        if (info.st_uid or info.st_gid or mode & 0o7002 or
                any(k.startswith("system.posix_acl_") or k == "security.capability" for k in attrs) or
                (not stat.S_ISDIR(info.st_mode) if directory else not stat.S_ISREG(info.st_mode) or info.st_nlink != 1)):
            fail(rel)
        if kind == "sealed":
            required = (0o500 if rel == Path(".") else 0o555) if directory else (
                0o400 if rel == Path(".source-revision") else 0o555 if expected[rel][0] else 0o444)
            if mode != required:
                fail(rel)
        if copied and (mode != (0o700 if directory else 0o600) or
                       any(os.path.samestat(info, (p / rel).lstat()) for p in originals)):
            fail(rel)
        if backup or record:
            labels = {k: hashlib.sha256(os.getxattr(fd, k)).hexdigest() for k in sorted(attrs)}
            print(json.dumps([str(rel), info.st_dev, info.st_ino, mode, info.st_uid, info.st_gid,
                              info.st_nlink, info.st_mtime_ns, info.st_ctime_ns, labels]))
        return info

    def onerror(err):
        raise err

    for base, subdirs, files, dfd in os.fwalk(root, follow_symlinks=False, onerror=onerror):
        rel = Path(base).relative_to(root)
        if rel not in dirs:
            fail(rel)
        inspect(dfd, rel, True)
        subdirs[:] = sorted(d for d in subdirs if kind != "legacy" or rel / d not in excluded)
        for child in subdirs:
            if rel / child not in dirs or not stat.S_ISDIR(os.stat(child, dir_fd=dfd, follow_symlinks=False).st_mode):
                fail(rel / child)
        for name in sorted(files):
            p = rel / name
            if p not in expected or not stat.S_ISREG(os.stat(name, dir_fd=dfd, follow_symlinks=False).st_mode):
                fail(p)
            with os.fdopen(os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=dfd), "rb") as f:
                info = inspect(f.fileno(), p, False)
                executable, blob, size = expected[p]
                if info.st_size != size or (not copied and bool(info.st_mode & 0o111) != executable):
                    fail(p)
                data = f.read(size + 1)
                digest = hashlib.sha1(b"blob %d\0" % size)
                digest.update(data)
                if (len(data) != size or digest.hexdigest() != blob or
                        os.fstat(f.fileno()).st_ctime_ns != info.st_ctime_ns):
                    fail(p)
            seen.add(p)
            if backup:
                dest = Path(backup) / p
                dest.parent.mkdir(parents=True, exist_ok=True)
                with dest.open("xb") as dst:
                    dst.write(data)
    if seen != set(expected):
        fail(min(set(expected) - seen))


def sealed_tree(root):
    entries = []
    for base, _, files, dfd in os.fwalk(root, follow_symlinks=False):
        for name in files:
            rel = Path(base).relative_to(root) / name
            if rel == Path('.source-revision'):
                continue
            fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=dfd)
            with os.fdopen(fd, 'rb') as f:
                info = os.fstat(f.fileno())
                require(stat.S_ISREG(info.st_mode), 'sealed snapshot entry is not a regular file')
                data = f.read()
            blob = hashlib.sha1(b'blob %d\0' % len(data) + data).hexdigest()
            mode = '100755' if info.st_mode & 0o111 else '100644'
            entries.append(f'{mode} blob {blob} {len(data)}\t'.encode() + os.fsencode(str(rel)) + b'\0')
    return b''.join(sorted(entries))


def verify_backup(writer, source):
    sealed = STORE / writer
    with tempfile.NamedTemporaryFile() as listing:
        listing.write(sealed_tree(sealed))
        listing.flush()
        try:
            verify_snapshot(listing.name, writer, str(sealed), 'sealed')
            verify_snapshot(listing.name, writer, str(STORE / f'transition-{writer}-{source}' / 'previous.actual'), 'backup', '', '',
                            str(LEGACY), str(sealed))
        except SystemExit as error:
            raise Refusal(str(error)) from None


if __name__ == '__main__':
    try:
        if sys.argv[1:2] == ['--migrator']:
            rendered, root, image = sys.argv[2:]
            require(os.geteuid() == os.getegid() == 0, 'invalid migrator authority')
            migrator_witness(root, image, rendered)
        elif sys.argv[1:2] == ['--window']:
            window(*sys.argv[2:])
        elif sys.argv[1:2] == ['--completed']:
            completed(*sys.argv[2:])
        elif sys.argv[1:2] == ['--slot']:
            old, candidate, service, *step = sys.argv[2:]
            window(service, old, candidate)
            witness = slot(old, candidate, service, *step)
            window(service, old, candidate)
            print(json.dumps(witness, separators=(',', ':')))
        elif sys.argv[1:2] == ['--legacy-release']:
            legacy_release(Path(sys.argv[2]))
        elif sys.argv[1:2] == ['--before-pair']:
            snapshot, release, service = sys.argv[2:]
            release = Path(release)
            pair = decode(raw(release / 'previous/pair.json'))['captured_release']
            rows = raw(Path(snapshot)).decode().splitlines()
            offset = WRITERS.index(service) * 3
            require(rows[offset] == f'{service} {pair}' and raw(release / 'before.pair') == (pair + '\n').encode() and
                    rows[offset+2] == digest(raw(release / 'before.conf')) + '  ' + str(QUADLET / (service + '.container.d/50-worker-profile.conf')),
                    'original writer pair changed before intent')
        elif sys.argv[1:2] == ['--relay']:
            action, witness = sys.argv[2:]
            channel = int(os.environ['HOLOLIVE_MANAGER_FD'])
            packet = json.dumps([action, decode(witness)]).encode()
            require(len(packet) <= 4096 and os.write(channel, packet) == len(packet), 'incomplete manager request')
            reply = decode(os.read(channel, 4096))
            require(isinstance(reply, str), 'unconfirmed manager response')
            print(reply)
        elif sys.argv[1:] == ['--idle-writers']:
            pending_jobs()
        elif sys.argv[1:] == ['--overlay']:
            print(json.dumps(overlay_identity(), sort_keys=True))
        else:
            print(*prove(*sys.argv[1:]), sep='\n')
    except Refusal as error:
        sys.exit('unstarted source evidence rejected: ' + str(error))
    except (ValueError, OSError, subprocess.SubprocessError, TypeError, KeyError, IndexError, AttributeError):
        sys.exit('unstarted source evidence unavailable')
