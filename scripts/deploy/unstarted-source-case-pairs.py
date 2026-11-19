import json
from pathlib import Path
import runpy
import sys


tool, host, revision, mode = sys.argv[1:]
host = Path(host)
helpers = runpy.run_path(str(Path(tool) / 'scripts/deploy/lib/worker-profile-pair.py'))
compose = Path('/opt/hololive-bot/compose')
profiles = compose / 'current/artifacts/worker-profile'
active = host / 'opt' / profiles.relative_to(compose) / 'active'
active.mkdir(parents=True, exist_ok=True)
fixture = host / 'profile-fixture'
fixture.mkdir(exist_ok=True)
(fixture / 'accepted-revision').write_text(revision + '\n')
secrets = host / 'etc-stack-secrets/hololive-bot/worker-profiles'
secrets.mkdir(exist_ok=True)
api_image = 'sha256:%064d' % 2
for service, number, cid in [('hololive-api', 2, 8), ('hololive-alarm-worker', 5, 9)]:
    image = 'sha256:%064d' % number
    suffix = '20261119T070000Z-abc123' if mode == 'legacy' else 'reviewed'
    pair = profiles / 'releases' / f'{service}-v2-{suffix}' / 'candidate'
    (active / (service + '.pair')).write_text(str(pair) + '\n')
    local = host / 'opt' / pair.relative_to(compose)
    local.mkdir(parents=True)
    release = local.parent
    for name, value in [('service', service), ('candidate.revision', revision), ('candidate.image', image), ('migrator.image', api_image)]:
        (release / name).write_text(value + '\n')
    unit = host / 'etc-containers-systemd/hololive-bot' / (service + '.container')
    (local / 'unit.container').write_bytes(unit.read_bytes())
    (local / 'profile.json').write_text(json.dumps({'fixture': service}))
    identity = {'image': image, 'revision': revision, 'architecture': 'arm64', 'os': 'linux'}
    if mode == 'legacy':
        (release / 'previous').mkdir()
    if mode == 'sealed':
        identity['source_generation'] = revision
        (release / 'schema-check').write_text('%064d %s\n' % (12, api_image))
    if service == 'hololive-api':
        identity['app_version'] = (host / 'opt/current/hololive/hololive-api/VERSION').read_text().strip() if mode == 'legacy' else '1.2.3'
    helpers['save_pair'](local, service, identity)
    body = helpers['dropin_contents'](pair, service, identity, identity.get('app_version'))
    (local / helpers['DROPIN']).write_bytes(body)
    installed = unit.with_name(unit.name + '.d') / helpers['DROPIN']
    installed.parent.mkdir(exist_ok=True)
    installed.write_bytes(body)
    slot = helpers['TARGET'] + '/' + helpers['PROFILES'][service]
    release_target = helpers['RELEASE_TARGET']
    (secrets / helpers['PROFILES'][service]).write_bytes((local / 'profile.json').read_bytes())
    oci = {'process': {'env': ['STACK_WORKER_PROFILE_FILE=' + release_target + '/profile.json',
                               'STACK_WORKER_PROFILE_SOURCE_FILE=' + slot]},
           'mounts': [{'type': 'bind', 'source': helpers['SOURCE'], 'destination': helpers['TARGET']},
                      {'type': 'bind', 'source': str(pair), 'destination': release_target}]}
    oci['process'].update(args=['/app/bin/' + ('hololive-api' if service == 'hololive-api' else 'alarm-worker')],
                          cwd='/app', user={'uid': 1000, 'gid': 1000})
    oci['linux'] = {}
    (fixture / (service + '.oci')).write_text(json.dumps(oci))
    (fixture / ('%064d.loaded' % cid)).write_text(identity['profile_hash'])
