#!/usr/bin/env python3
import contextlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import stat
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('kapu_d', Path(__file__).with_name('kapu-d-cutover.py'))
d = importlib.util.module_from_spec(spec)
spec.loader.exec_module(d)


@unittest.skipUnless(os.geteuid() == 0, '실제 root 소유권과 서비스 UID를 분리하는 시험')
class ReceiptStore(unittest.TestCase):
    def test_first_quiesce_separates_receipts_from_service_owned_data(self):
        with tempfile.TemporaryDirectory(dir='/run') as tmp:
            root = Path(tmp)
            data = root / 'hololive-collectors'
            data.mkdir(mode=0o750)
            os.chown(data, 65534, 65534)
            before = data.stat()
            store = root / d.STORE.relative_to('/var/lib')
            with self.assertRaises(FileNotFoundError):
                d.prepare_store(store, create=False)
            self.assertFalse(store.exists())
            d.prepare_store(store, create=True)
            receipt = store / 'retained'
            receipt.write_bytes(b'original')
            d.prepare_store(store, create=False)
            self.assertEqual(receipt.read_bytes(), b'original')
            self.assertEqual((store.stat().st_uid, store.stat().st_mode & 0o777), (0, 0o700))
            self.assertEqual((before.st_uid, before.st_gid, before.st_mode, before.st_ctime_ns),
                             (data.stat().st_uid, data.stat().st_gid, data.stat().st_mode, data.stat().st_ctime_ns))

    def test_quiesce_does_not_adopt_foreign_or_linked_store(self):
        with tempfile.TemporaryDirectory(dir='/run') as tmp:
            foreign = Path(tmp) / 'foreign'
            foreign.mkdir(mode=0o700)
            os.chown(foreign, 65534, 65534)
            link = Path(tmp) / 'link'
            link.symlink_to(foreign, target_is_directory=True)
            for path in (foreign, link):
                with self.subTest(path=path.name), self.assertRaises(d.Refused):
                    d.prepare_store(path, create=True)
            self.assertEqual(foreign.stat().st_uid, 65534)
            self.assertTrue(link.is_symlink())


class Commands(unittest.TestCase):
    def test_clean_commands_refuse_diagnostics_even_on_success(self):
        self.assertEqual(d.run('sh', '-c', 'echo listed', clean=True), 'listed')
        with self.assertRaises(d.Refused):
            d.run('sh', '-c', 'echo listed; echo partial >&2', clean=True)
        self.assertEqual(d.run('sh', '-c', 'echo listed; echo noise >&2'), 'listed')


class Cutover(unittest.TestCase):
    def test_profile_and_sql_isolate_runtime_inputs(self):
        image = 'sha256:' + 'a' * 64
        with patch.object(d, 'run') as run:
            d.gate(image, 's0:c908,c909')
        profile, sql = [call.args for call in run.call_args_list]
        self.assertEqual(profile[-2:], (image, '--check-worker-profile'))
        self.assertIn('--network=none', profile)
        self.assertIn('--env-file=' + str(d.ENVFILE), profile)
        self.assertEqual([arg for arg in profile if arg.startswith(('--volume=', '--mount='))],
                         ['--volume=' + str(d.CA) + ':' + d.CERT + ':ro',
                          '--volume=' + str(d.D/'profile.json') + ':' + d.PROFILE_ENV['STACK_WORKER_PROFILE_FILE'] + ':ro'])
        self.assertEqual(sql[-2:], (image, '--check-database-endpoint'))
        self.assertEqual([arg for arg in sql if arg.startswith(('--volume=', '--mount='))],
                         ['--volume=' + str(d.CA) + ':' + d.CERT + ':ro'])
        for arg in ['--user=1000:1000', '--group-add=1002', '--userns=auto:size=65536,gidmapping=1002:1002:1',
                    '--security-opt=label=level:s0:c908,c909', '--read-only', '--network=' + d.NETWORK,
                    '--ip=10.89.15.14', '--env=POSTGRES_USER=hololive_collector_d', '--env=POSTGRES_PORT=5433',
                    '--env=COLLECTOR_INSTANCE_ID=' + d.SERVICE, '--env-file=' + str(d.ENVFILE)]:
            self.assertIn(arg, sql)
        self.assertFalse(any('PASSWORD' in arg or '.key' in arg for arg in sql))

    def setUp(self):
        self.stack = contextlib.ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory()))
        self.h = self.root / 'd-cutover-new'
        self.h.mkdir(mode=0o700)
        self.release = self.root / 'custody-bundle'
        self.input = self.release / 'deploy/podman/kapu'
        self.units = self.root / 'units'
        self.system = self.root / 'system'
        for path in self.input, self.units, self.system:
            path.mkdir(parents=True)
        self.rev = 'a' * 40
        self.ids = {d.C:'sha256:'+'1'*64, d.I:'sha256:'+'2'*64}
        self.state = {'level':'s0:c908,c909','profile':'3'*64,'images':{},'old':{}}
        self.digests = {}
        self.props = {n:{'ActiveState':'inactive','InvocationID':'','Result':'success','ExecMainCode':'1',
                         'ExecMainStatus':'0','NRestarts':'0','MainPID':'0','ControlPID':'0','Job':'',
                         'LoadState':'masked','FragmentPath':str(self.system / (n + '.service'))} for n in d.NAMES}
        self.guards = set()
        self.outcomes = {}
        self.exits = {}
        self.executions = {}
        self.start_events = {}
        self.on_start = lambda n:None
        self.journal = {}
        self.tags = {}
        self.unit_source, self.origin, self.retained = d.INPUT, d.origin, d.retained
        for k,v in [('UNIT',self.units),('INPUT',self.input),('RELEASE_ROOT',self.release),('SYSTEM',self.system)]:
            self.patch(k,v)
        for i,n in enumerate(d.NAMES):
            ident = {'id':'sha256:' + str(i+3)*64, 'rev':'b'*40}
            self.tags[d.prod(n)] = self.state['images'][n] = ident
            self.tags[self.ids[n]] = {'id':self.ids[n],'rev':self.rev}
            self.state['old'][n] = {'cid':str(i+6)*64,'inv':'b'*32,'image':ident['id'][7:]}
            self.executions[n] = {'cid':str(i+8)*64,'inv':'e'*32,'image':self.ids[n][7:],
                                  'started':'120','main':'123','pid':'124','cgroup':'/system.slice/'+n+'.service'}
            data = f'[Container]\nContainerName={n}\nEnvironment=POSTGRES_PORT=5433\n'.encode()
            (self.input / d.unit(n)).write_bytes(data)
            (self.units / d.unit(n)).write_bytes(b'old')
            self.digests[n] = d.sha(data)
            d.mask(n).symlink_to('/dev/null')
            os.link(d.mask(n),d.mask_anchor(self.h,n),follow_symlinks=False)
            d.metadata(self.h / (n + '.mask'), d.number(d.mask(n)))
            d.record(self.h / (n + '.stopped'), b'b'*32)
            d.metadata(self.h / (n + '.stopped.json'), {**self.props[n],'cid':self.state['old'][n]['cid'],
                       'invocation':'b'*32,'since':'@1234.000056','manager_stop':True,'container_exit':'0'})
        self.state['candidate'] = self.target()
        d.metadata(self.h/'previous.json',self.state)
        d.record(self.h/'quiesced', d.stop_receipt(json.loads((self.h/'previous.json').read_bytes())))
        self.vendor = self.root / 'vendor'
        self.vendor.mkdir()
        self.early = self.root / 'early'
        self.early.mkdir()
        self.patch('GEN', self.root / 'generator')
        d.GEN.mkdir()
        self.patch('unit_paths', lambda: [self.system, self.early, d.GEN, self.vendor])
        self.patch('trusted', lambda *args:None)
        self.patch('raw', Path.read_bytes)
        self.patch('retained', lambda *args,**kwargs:None)
        self.patch('origin', lambda *args,**kwargs:None)
        self.patch('generated', lambda *args:None)
        self.patch('absent', lambda *args:None)
        self.patch('image', lambda name,tag:self.tags[tag])
        self.patch('prop', lambda n,k:self.props[n][k])
        self.patch('generation', self.generation)
        self.calls = []
        self.unit_files = ''.join(f'ids: {n}.service \u2192 {self.system / (n + ".service")}\n' for n in d.NAMES)
        self.patch('run', self.command)
        self.stack.enter_context(patch.object(d.subprocess,'run',self.start_client))

    def patch(self, name, value):
        self.stack.enter_context(patch.object(d,name,value))

    def generation(self, n):
        self.assertEqual(self.props[n]['ActiveState'],'active')
        return dict(self.executions[n])

    def reload(self):
        for n in d.NAMES:
            self.props[n]['LoadState'] = 'masked' if os.path.lexists(d.mask(n)) else 'loaded'

    def start_client(self, argv, *, stdout, **kwargs):
        self.assertEqual(argv[:3],['systemctl','start','--show-transaction'])
        n = argv[-1].removesuffix('.service')
        self.assertNotIn(n,self.guards)
        self.assertEqual(self.props[n]['LoadState'],'loaded')
        self.calls.append(tuple(argv))
        job = str(100 + len(self.start_events))
        stdout.write(f'Enqueued anchor job {job} {argv[-1]}/start.\n'.encode())
        self.start_events[job] = (n,self.executions[n]['inv'])
        self.props[n].update(ActiveState='activating',MainPID='123',InvocationID=self.executions[n]['inv'],Job=job)
        self.on_start(n)
        self.props[n].update(ActiveState='active',Job='')
        return d.subprocess.CompletedProcess(argv,0)

    def command(self, *args, **kwargs):
        if args[:3] == ('systemctl','show','--all'):
            props = self.props[args[-1].removesuffix('.service')]
            return '\n'.join(k + '=' + props[k] for k in args[3].removeprefix('--property=').split(','))
        if args == ('systemd-analyze','unit-files'):
            self.assertEqual(kwargs, {'env': {'SYSTEMD_UNIT_PATH': ':'.join(map(str, d.unit_paths()))}, 'clean': True})
            return self.unit_files
        self.calls.append(args)
        if args[:4] == ('python3','-I','-B',str(d.GUARD)):
            if args[4] == 'exit-status':
                unit, cid, since = args[5:]
                n = unit.removesuffix('.service')
                self.assertEqual(cid,self.executions[n]['cid'])
                journal = next(c for c in reversed(self.calls) if c[0] == 'journalctl')
                self.assertIn('--since=' + since, journal)
                self.assertIn('UNIT=' + unit, journal)
                return self.exits.get(n,self.props[n]['ExecMainStatus'])
            action, unit, path, identity = args[4:]
            n, anchor = unit.removesuffix('.service'), Path(path)
            self.assertEqual(identity, ':'.join(map(str,d.number(anchor))))
            if action == 'start-budget':
                self.assertIn(n,self.guards)
                return '90'
            if action == 'undo-unmask':
                self.assertFalse(os.path.lexists(d.mask(n)))
                self.assertEqual(self.props[n]['LoadState'],'masked')
                os.link(anchor,d.mask(n),follow_symlinks=False)
                self.reload()
                return
            d.need(not os.path.lexists(d.mask(n)), '가드 충돌')
            if action == 'arm':
                d.need(n not in self.guards, '이전 가드')
                self.guards.add(n)
                was_loaded = self.props[n]['LoadState'] == 'loaded'
                self.reload()
                d.need(was_loaded, '기동 차단만 설치됨')
            else:
                self.assertIn(n,self.guards)
            if action == 'seal':
                self.assertIn(self.props[n]['ActiveState'], ('inactive','failed'))
                self.assertEqual((self.props[n]['MainPID'],self.props[n]['ControlPID']), ('0','0'))
                os.link(anchor,d.mask(n),follow_symlinks=False)
                self.guards.remove(n)
                self.props[n].update(ActiveState='inactive', Result='success', ExecMainStatus='0', InvocationID='')
            elif action != 'arm':
                self.assertEqual(action,'check')
            if action != 'check':
                self.reload()
        if args == ('systemctl','daemon-reload'):
            self.reload()
        if args[:2] == ('podman','tag'):
            self.tags[args[3]] = self.tags[args[2]]
        if args[:2] == ('podman','exec') and '--body' in args:
            return json.dumps({'status':'ready','instance_id':d.SERVICE,'helper':'ok',
                               'first_success':True,'handoff_status':'PROCESSED','handoff_processed':True})
        if args[:2] == ('systemctl','stop'):
            n = args[-1].removesuffix('.service')
            self.assertIn(n,self.guards)
            if not (self.h/'resume-attempt.json').exists():
                self.assertTrue(all(peer in self.guards or os.path.lexists(d.mask(peer)) for peer in d.NAMES))
            self.assertEqual('--job-mode=ignore-dependencies' in args, n == d.I)
            self.journal[n] = self.props[n]['InvocationID']
            self.props[n].update(ActiveState='inactive', MainPID='0', ControlPID='0')
            self.props[n].update(self.outcomes.get(n,{}))
        if args[0] == 'journalctl':
            n = next(x[5:-8] for x in args if x.startswith('UNIT='))
            if 'JOB_TYPE=start' in args:
                job = next(x[7:] for x in args if x.startswith('JOB_ID='))
                unit, inv = self.start_events[job]
                self.assertEqual(unit,n)
                return json.dumps({'INVOCATION_ID':inv})
            return json.dumps({'INVOCATION_ID': self.journal[n]})

    def target(self):
        return d.release_candidate(self.rev, self.ids, self.digests)

    def seal(self, target):
        (self.release / '.source-revision').write_text(target['revision'] + '\n')
        (self.release / 'd-card-candidate.json').write_text(json.dumps(target))

    def cli(self, action, target, *, anchor_uid=0):
        args = ['kapu-d-cutover.py',action,str(self.h),
                target['revision'],*(target[k][n] for k in ('images','units') for n in d.NAMES)]
        anchors = {d.mask_anchor(self.h,n) for n in d.NAMES}
        lstat = Path.lstat
        def fixture_lstat(path):
            st = lstat(path)
            if path not in anchors:
                return st
            fields = list(st)
            fields[stat.ST_UID] = anchor_uid
            return os.stat_result(fields)
        with (patch.object(d.sys,'argv',args), patch.object(d,'STORE',self.root),
              patch.object(d.os,'geteuid',return_value=0), patch.object(d.os,'umask'),
              patch.object(Path,'lstat',fixture_lstat),
              patch.object(d.os,'uname',return_value=SimpleNamespace(machine='x86_64')),
              patch.object(d.socket,'gethostname',return_value='kapu'), patch.object(d.signal,'signal')):
            d.main()

    def resume(self):
        d.resume(self.h,self.target())

    def running_pair(self, runtime_port=None):
        self.h = self.root / 'd-cutover-running'
        self.patch('origin',self.origin)
        self.patch('retained',self.retained)
        self.patch('prereqs',lambda:None)
        self.patch('bindings',lambda:{})
        self.patch('policy',lambda n: {'50-mcs-level.conf':b'[Container]\nSecurityLabelLevel=s0:c908,c909\n'} if n == d.C else {})
        self.patch('profile',lambda:b'profile')
        self.patch('healthy',lambda n,g,**kwargs:self.assertEqual(g,self.generation(n)))
        def collector(g,level):
            running = runtime_port if g['cid'] == self.state['old'][d.C]['cid'] else None
            d.need(running in (None, d.CANDIDATE_PORT),'collector 실행 환경 불일치')
            return b'profile'
        self.patch('collector',collector)
        self.patch('issuer',lambda *args:None)
        reload_masks = self.reload
        def reload():
            reload_masks()
            for n in d.NAMES:
                base = self.units / d.unit(n)
                f = d.GEN / (n + '.service')
                text = base.read_text() + '\n' + '\n'.join(v.decode() for v in d.policy(n).values())
                f.write_text('SourcePath=' + str(base) + '\n' + text.replace('[Container]', '[X-Container]'))
                self.props[n].update(FragmentPath=str(d.mask(n) if self.props[n]['LoadState'] == 'masked' else f),
                                     NeedDaemonReload='no', Type='notify', DropInPaths=(
                                         str(self.system / (n+'.service.d/90-hololive-stop-guard.conf')) if n in self.guards else ''))
        self.reload = reload
        for n in d.NAMES:
            candidate = (self.unit_source / d.unit(n)).read_bytes()
            (self.input / d.unit(n)).write_bytes(candidate)
            self.digests[n] = d.sha(candidate)
            (self.units / d.unit(n)).write_bytes(candidate)
            old_image = self.state['images'][n]
            self.tags[old_image['id']] = old_image
            d.mask(n).unlink()
            self.executions[n] = dict(self.state['old'][n])
            self.props[n].update(ActiveState='active', InvocationID=self.executions[n]['inv'], MainPID='123')
        self.reload()
        return self.h

    def test_quiesce_refuses_dependency_links_a_guard_reload_brings_in(self):
        h = self.running_pair()
        reload = self.reload
        def reload_with_link():
            reload()
            folder = self.system / (d.C + '.service.requires')
            if self.guards and not folder.exists():
                folder.mkdir()
                (folder / 'probe.service').symlink_to('../probe.service')
        self.reload = reload_with_link
        with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
            d.quiesce(h, self.target())
        self.assertFalse([c for c in self.calls if c[:2] == ('systemctl','stop')])

    def test_dependency_masks_are_only_exact_dev_null_links(self):
        target = self.target()
        self.seal(target)
        folder = self.system / (d.C + '.service.wants')
        folder.mkdir()
        (folder / 'probe.service').symlink_to('/dev/null/')
        with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
            self.cli('check', target)

    def test_higher_priority_masks_and_hidden_entries_hide_lower_dependencies(self):
        target = self.target()
        self.seal(target)
        upper = self.system / (d.C + '.service.wants')
        lower = self.vendor / (d.C + '.service.wants')
        upper.mkdir()
        lower.mkdir()
        (upper / 'probe.service').symlink_to('/dev/null')
        (lower / 'probe.service').symlink_to('../probe.service')
        drops = self.system / (d.C + '.service.d')
        drops.mkdir(exist_ok=True)
        (drops / '.pending.conf').write_bytes(b'[Service]\nRestart=no\n')
        (self.vendor / (d.C + '.service.d')).mkdir()
        (self.vendor / (d.C + '.service.d' ) / '20-x.conf').write_bytes(b'[Service]\nRestart=no\n')
        (drops / '20-x.conf').symlink_to('/dev/null')
        self.cli('check', target)
        (upper / 'probe.service').unlink()
        with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
            self.cli('check', target)

    def test_dependency_dirs_ignore_entries_systemd_ignores(self):
        target = self.target()
        self.seal(target)
        folder = self.system / (d.C + '.service.wants')
        folder.mkdir()
        (folder / 'README').write_bytes(b'note\n')
        (folder / 'masked.service').symlink_to('/dev/null')
        self.cli('check', target)
        (folder / 'probe.service').symlink_to('../probe.service')
        with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
            self.cli('check', target)

    def test_resume_refuses_a_dropin_on_the_open_issuer_before_collector_start(self):
        target = self.target()
        self.seal(target)
        reload = self.reload
        def reload_with_issuer_dropin():
            reload()
            folder = self.system / (d.I + '.service.d')
            if not os.path.lexists(d.mask(d.C)) and not folder.exists():
                folder.mkdir()
                (folder / '10-late.conf').write_bytes(b'[Service]\nRestart=no\n')
        self.reload = reload_with_issuer_dropin
        self.resume_inputs()
        with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
            self.resume()
        self.assertFalse([c for c in self.calls if c[:2] == ('systemctl','start') and c[-1] == d.C + '.service'])

    def test_quiesce_refuses_dependency_links_before_any_stop(self):
        h = self.running_pair()
        folder = self.system / (d.C + '.service.requires')
        folder.mkdir()
        (folder / (d.I + '.service')).symlink_to('../' + d.I + '.service')
        with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
            d.quiesce(h, self.target())
        self.assertFalse(os.path.lexists(h))
        self.assertFalse([c for c in self.calls if c[:2] == ('systemctl','stop')])

    def test_original_jobs_finish_before_any_seal(self):
        h = self.running_pair()
        self.outcomes[d.I] = {'ActiveState':'failed','Result':'exit-code','ExecMainStatus':'143'}
        self.outcomes[d.C] = {'InvocationID':'','ExecMainCode':'0'}
        d.quiesce(h,self.target())
        self.assertEqual((h/'quiesced').read_bytes(),d.stop_receipt(json.loads((h/'previous.json').read_bytes())))
        self.assertEqual([x[-1] for x in self.calls if x[:2] == ('systemctl','stop')],
                         [d.I+'.service',d.C+'.service'])
        for n in d.NAMES:
            evidence = json.loads((h/(n+'.stopped.json')).read_bytes())
            self.assertEqual(evidence['invocation'],self.state['old'][n]['inv'])
            self.assertTrue(evidence['manager_stop'])
            self.assertEqual(evidence['ExecMainStatus'],'143' if n == d.I else '0')
            self.assertEqual(evidence['container_exit'],'143' if n == d.I else '0')
        self.assertLess(self.calls.index(('systemctl','stop',d.C+'.service')),
                        next(i for i,c in enumerate(self.calls) if c[4:5] == ('seal',)))

    def test_issuer_stop_does_not_authorize_replaced_collector(self):
        h = self.running_pair()
        def swap(*args, **kwargs):
            result = self.command(*args,**kwargs)
            if args[:2] == ('systemctl','stop') and args[-1] == d.I+'.service':
                self.executions[d.C]['inv'] = 'd'*32
            return result
        self.patch('run',swap)
        with self.assertRaisesRegex(ValueError,'세대 변경'):
            d.quiesce(h,self.target())
        self.assertEqual(self.guards,set(d.NAMES))
        self.assertNotIn(('systemctl','stop',d.C+'.service'),self.calls)
        self.assertFalse((h/'quiesced').exists())

    def test_collector_143_cannot_be_hidden_by_seal_reload(self):
        h = self.running_pair()
        self.outcomes[d.C] = {'ActiveState':'failed','Result':'exit-code','ExecMainStatus':'143'}
        with self.assertRaisesRegex(ValueError,'정상 종료 미확인'):
            d.quiesce(h,self.target())
        result = json.loads((h/(d.C+'.stopped.json')).read_bytes())
        self.assertTrue(result['manager_stop'])
        self.assertEqual((result['Result'],result['ExecMainStatus']), ('exit-code','143'))
        self.assertEqual(self.guards,set(d.NAMES))
        self.assertFalse((h/(d.C+'.stopped')).exists())
        self.assertFalse((h/'quiesced').exists())

    def test_collector_nonzero_exit_survives_manager_collection(self):
        h = self.running_pair()
        self.exits[d.C] = '143'
        self.outcomes[d.C] = {'InvocationID':'','ExecMainCode':'0','ExecMainStatus':'0','Result':'success'}
        with self.assertRaisesRegex(ValueError,'정상 종료 미확인'):
            d.quiesce(h,self.target())
        self.assertFalse((h/'quiesced').exists())
        self.assertEqual(json.loads((h/(d.C+'.stopped.json')).read_bytes())['container_exit'],'143')
        self.assertIn(d.C,self.guards)

    def test_partial_arm_stops_neither_unit(self):
        h = self.running_pair()
        self.guards.add(d.I)
        with self.assertRaisesRegex(ValueError,'이전 가드'):
            d.quiesce(h,self.target())
        self.assertFalse(any(c[:2] == ('systemctl','stop') for c in self.calls))
        self.assertEqual(self.guards,set(d.NAMES))
        self.assertFalse((h/'quiesced').exists())

    def test_already_direct_release_keeps_installed_endpoint(self):
        candidates = dict(self.executions)
        h = self.running_pair()
        target = self.target()
        self.seal(target)
        self.cli('quiesce',target)
        previous = json.loads((h/'previous.json').read_bytes())
        self.assertEqual(previous['previous_endpoint']['port'],5433)
        self.assertEqual(previous['old'],self.executions)
        self.assertEqual(previous.get('candidate'),target)
        collector_unit = self.units / d.unit(d.C)
        inode = d.number(collector_unit)
        self.executions = candidates
        self.cli('resume',target)
        self.assertEqual(d.number(collector_unit),inode)
        self.assertEqual(collector_unit.read_bytes(),(d.INPUT / d.unit(d.C)).read_bytes())
        accepted = json.loads((h/'accepted.json').read_bytes())
        self.assertEqual((accepted['generations'],accepted['port']),(candidates,5433))
        self.assertEqual([x[-1] for x in self.calls if x[:2] == ('systemctl','start')],
                         [d.I+'.service',d.C+'.service'])

    def test_already_direct_refuses_pooled_running_collector(self):
        h = self.running_pair(runtime_port='6433')
        with self.assertRaisesRegex(d.Refused,'실행 환경 불일치'):
            d.quiesce(h,self.target())
        self.assertFalse(h.exists())
        self.assertFalse(self.calls)

    def test_check_runs_resume_gates_without_changing_the_hold(self):
        target = self.target()
        self.seal(target)
        before = sorted((p.name, p.read_bytes() if p.is_file() else None) for p in self.h.iterdir())
        masks = {n: os.readlink(d.mask(n)) for n in d.NAMES}
        units = {n: (self.units / d.unit(n)).read_bytes() for n in d.NAMES}
        self.cli('check', target)
        self.assertEqual(sorted((p.name, p.read_bytes() if p.is_file() else None) for p in self.h.iterdir()), before)
        self.assertEqual({n: os.readlink(d.mask(n)) for n in d.NAMES}, masks)
        self.assertEqual({n: (self.units / d.unit(n)).read_bytes() for n in d.NAMES}, units)
        self.assertEqual(self.calls, [])

    def test_check_and_resume_refuse_an_interrupted_resume_attempt(self):
        target = self.target()
        self.seal(target)
        d.metadata(self.h / 'resume-attempt.json', {**target, 'profile': self.state['profile']})
        attempt = (self.h / 'resume-attempt.json').read_bytes()
        with self.assertRaisesRegex(d.Refused, '재개 시도'):
            self.cli('check', target)
        with self.assertRaisesRegex(d.Refused, '재개 시도'):
            self.resume()
        self.assertEqual((self.h / 'resume-attempt.json').read_bytes(), attempt)
        self.assertEqual(self.calls, [])

    def test_check_and_resume_refuse_a_systemd_dropin_added_after_the_stop(self):
        target = self.target()
        self.seal(target)
        for folder in (self.system / (d.C + '.service.d'), self.vendor / 'hololive-.service.d', self.vendor / 'service.d'):
            folder.mkdir()
            (folder / '10-late.conf').write_bytes(b'[Service]\nEnvironment=LATE=1\n')
            with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
                self.cli('check', target)
            with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
                self.resume()
            (folder / '10-late.conf').unlink()
        self.assertEqual(self.calls, [])
        self.cli('check', target)

    def test_check_and_resume_see_dropins_attached_through_an_alias(self):
        target = self.target()
        self.seal(target)
        self.unit_files += (f'ids: collector-alias.service \u2192 {d.C}.service\n'
                            'ids: chained.service -> collector-alias.service\n'
                            f'ids: other.service \u2192 {self.vendor}/other.service\n'
                            'ids: unrelated.service \u2192 other.service\n'
                            f'aliases: {d.C}.service \u2190 {d.C}.service, collector-alias.service\n')
        for folder in (self.vendor / 'unrelated.service.d', self.vendor / 'other.service.d'):
            folder.mkdir()
            (folder / '10-own.conf').write_bytes(b'[Service]\nEnvironment=OWN=1\n')
        self.cli('check', target)
        for folder in (self.system / 'collector-alias.service.d', self.vendor / 'collector-.service.d', self.vendor / 'chained.service.d'):
            folder.mkdir()
            (folder / '10-late.conf').write_bytes(b'[Service]\nEnvironment=LATE=1\n')
            with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
                self.cli('check', target)
            with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
                self.resume()
            (folder / '10-late.conf').unlink()
        self.assertEqual(self.calls, [])
        self.assertFalse((self.h / 'resume-attempt.json').exists())

    def test_check_and_resume_refuse_dependency_links_added_after_the_stop(self):
        target = self.target()
        self.seal(target)
        self.unit_files += f'ids: collector-alias.service \u2192 {d.C}.service\n'
        for folder in (self.system / (d.C + '.service.requires'), self.vendor / 'hololive-.service.wants',
                       self.vendor / 'service.upholds', self.system / 'collector-alias.service.wants'):
            folder.mkdir()
            (folder / 'probe.service').symlink_to('../probe.service')
            with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
                self.cli('check', target)
            with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
                self.resume()
            (folder / 'probe.service').unlink()
        self.assertEqual(self.calls, [])
        self.assertFalse((self.h / 'resume-attempt.json').exists())
        self.cli('check', target)

    def test_check_and_resume_verify_generated_fragments_before_the_attempt_record(self):
        target = self.target()
        self.seal(target)
        checked = []
        def generated(name):
            checked.append(name)
            d.need(False, '생성된 unit 원천 불일치')
        self.patch('generated', generated)
        with self.assertRaisesRegex(d.Refused, '생성된 unit 원천'):
            self.cli('check', target)
        with self.assertRaisesRegex(d.Refused, '생성된 unit 원천'):
            self.resume()
        self.assertEqual(checked, [d.NAMES[0]] * 2)
        self.assertEqual(self.calls, [])
        self.assertFalse((self.h / 'resume-attempt.json').exists())

    def test_check_accepts_a_sealed_hold_whose_manager_still_lists_the_removed_guard(self):
        target = self.target()
        self.seal(target)
        self.patch('origin', self.origin)
        for n in d.NAMES:
            self.props[n]['DropInPaths'] = str(self.system / (n + '.service.d/90-hololive-stop-guard.conf'))
        self.cli('check', target)
        self.assertEqual(self.calls, [])

    def test_resume_refuses_dependency_links_the_resume_reload_creates(self):
        target = self.target()
        self.seal(target)
        reload = self.reload
        def reload_with_generated_link():
            reload()
            if not os.path.lexists(d.mask(d.I)):
                folder = self.system / (d.I + '.service.wants')
                folder.mkdir(exist_ok=True)
                if not os.path.lexists(folder / 'probe.service'):
                    (folder / 'probe.service').symlink_to('../probe.service')
        self.reload = reload_with_generated_link
        with self.assertRaisesRegex(d.Refused, 'systemd drop-in'):
            self.resume()
        self.assertFalse([c for c in self.calls if c[:2] == ('systemctl','start')])

    def test_check_and_resume_refuse_unit_files_hidden_behind_the_mask(self):
        target = self.target()
        self.seal(target)
        for name in d.NAMES:
            entry = self.early / (name + '.service')
            entry.symlink_to('x.service')
            with self.assertRaisesRegex(d.Refused, 'systemd unit 파일'):
                self.cli('check', target)
            with self.assertRaisesRegex(d.Refused, 'systemd unit 파일'):
                self.resume()
            entry.unlink()
            (self.vendor / (name + '.service')).write_bytes(b'[Service]\nExecStart=/bin/true\n')
        self.cli('check', target)
        self.assertEqual(self.calls, [])
        self.assertFalse((self.h / 'resume-attempt.json').exists())

    def test_check_and_resume_refuse_an_unreadable_systemd_name_map(self):
        target = self.target()
        self.seal(target)
        for listing in ('', 'Failed to resolve symlink x\n', self.unit_files.replace(d.C + '.service \u2192', 'x.service \u2192')):
            self.unit_files = listing
            with self.assertRaisesRegex(d.Refused, 'systemd unit 이름 맵'):
                self.cli('check', target)
            with self.assertRaisesRegex(d.Refused, 'systemd unit 이름 맵'):
                self.resume()
        self.assertEqual(self.calls, [])
        self.assertFalse((self.h / 'resume-attempt.json').exists())

    def test_check_refuses_what_resume_refuses(self):
        target = self.target()
        self.seal(target)
        def rebooted(h, s):
            d.need(False, '다른 hold/boot')
        with patch.object(d, 'retained', rebooted), self.assertRaises(d.Refused):
            self.cli('check', target)
        (self.h / 'quiesced').write_bytes(b'stale\n')
        with self.assertRaises(d.Refused):
            self.cli('check', target)
        self.assertEqual(self.calls, [])

    def test_direct_resume_refuses_changed_previous_endpoint(self):
        h = self.running_pair()
        d.quiesce(h,self.target())
        state = json.loads((h/'previous.json').read_bytes())
        state['previous_endpoint']['port'] = 6433
        (h/'previous.json').write_text(json.dumps(state))
        with self.assertRaisesRegex(d.Refused,'quiesce 영수증'):
            self.resume()
        self.assertFalse((h/'resume-attempt.json').exists())
        self.assertTrue(all(d.mask(n).is_symlink() for n in d.NAMES))

    def test_direct_quiesce_requires_a_new_verified_pair_before_either_stop(self):
        h = self.running_pair()
        target = self.target()
        old_ids = {n:i['id'] for n,i in self.state['images'].items()}
        old_revision = self.state['images'][d.C]['rev']
        cases = (
            (None,'별도 후보 identity'),
            ({**target, 'revision':old_revision, 'images':old_ids},'재사용'),
            ({**target, 'revision':old_revision},'재사용'),
            ({**target, 'images':{**self.ids,d.I:old_ids[d.I]}},'재사용'),
            ({**target, 'revision':'c'*40},'이미지 revision'),
            ({**target, 'units':{**self.digests,d.I:'c'*64}},'unit digest'),
        )
        for invalid,reason in cases:
            with self.subTest(reason=reason), self.assertRaisesRegex(d.Refused,reason):
                d.quiesce(h,invalid)
            self.assertFalse(h.exists())
            self.assertFalse(self.calls)

    def test_card_cli_rejects_earlier_complete_release_and_same_revision_rebuild(self):
        h = self.running_pair()
        card = self.target()
        self.seal(card)
        prior_ids = {d.C:'sha256:'+'5'*64,d.I:'sha256:'+'6'*64}
        prior = d.release_candidate('c'*40,prior_ids,self.digests)
        rebuilt_id = 'sha256:'+'7'*64
        rebuilt = d.release_candidate(self.rev,{**self.ids,d.C:rebuilt_id},self.digests)
        for n in d.NAMES:
            self.tags[prior_ids[n]] = {'id':prior_ids[n],'rev':prior['revision']}
        self.tags[rebuilt_id] = {'id':rebuilt_id,'rev':self.rev}

        for candidate,reason in ((prior,'source revision'),(rebuilt,'승인 후보')):
            with self.subTest(reason=reason), self.assertRaisesRegex(d.Refused,reason):
                self.cli('quiesce',candidate)
            self.assertFalse(h.exists())
            self.assertFalse(self.calls)

    def test_card_cli_requires_both_protected_files_before_quiesce(self):
        h = self.running_pair()
        target = self.target()
        for name in ('.source-revision','d-card-candidate.json'):
            self.seal(target)
            (self.release / name).unlink()
            with self.subTest(missing=name), self.assertRaises(FileNotFoundError) as exc:
                self.cli('quiesce',target)
            self.assertEqual(Path(exc.exception.filename),self.release / name)
            self.assertFalse(h.exists())
            self.assertFalse(self.calls)

    def test_card_cli_rechecks_protected_inputs_before_resume(self):
        h = self.running_pair()
        target = self.target()
        self.seal(target)
        self.cli('quiesce',target)
        self.calls.clear()
        rebuilt = {**target,'images':{**self.ids,d.C:'sha256:'+'5'*64}}
        (self.release / '.source-revision').write_text('c'*40 + '\n')
        with self.assertRaisesRegex(d.Refused,'source revision'):
            self.cli('resume',target)
        self.seal(rebuilt)
        for candidate,reason in ((target,'배포 트리의 승인 후보'),(rebuilt,'이 hold에서 정지 전에 승인한 후보')):
            with self.subTest(reason=reason), self.assertRaisesRegex(d.Refused,reason):
                self.cli('resume',candidate)
        self.assertFalse(self.calls)
        self.assertFalse((h/'resume-attempt.json').exists())
        for n in d.NAMES:
            self.assertEqual(d.number(d.mask(n)),d.number(d.mask_anchor(h,n)))

    def test_card_cli_refuses_foreign_mask_anchor_before_resume(self):
        candidates = dict(self.executions)
        h = self.running_pair()
        target = self.target()
        self.seal(target)
        self.cli('quiesce',target)
        self.executions = candidates
        self.calls.clear()
        with self.assertRaisesRegex(d.Refused,'소유 mask anchor 변경'):
            self.cli('resume',target,anchor_uid=65534)
        self.assertFalse((h/'resume-attempt.json').exists())
        self.assertFalse(self.calls)
        for n in d.NAMES:
            self.assertEqual(d.number(d.mask(n)),d.number(d.mask_anchor(h,n)))

    def test_direct_resume_only_opens_the_candidate_from_this_hold(self):
        h = self.running_pair()
        target = self.target()
        d.quiesce(h,target)
        alternate = 'sha256:' + 'c'*64
        self.tags[alternate] = {'id':alternate,'rev':self.rev}
        old = self.state['images']
        candidates = [
            {**target, 'revision':old[d.C]['rev'], 'images':{n:i['id'] for n,i in old.items()}},
            {**target, 'revision':'f'*40},
            *({**target, 'images':{**self.ids,n:alternate}} for n in d.NAMES),
            *({**target, 'units':{**self.digests,n:'f'*64}} for n in d.NAMES),
        ]
        self.calls.clear()
        for replacement in candidates:
            with self.subTest(candidate=replacement), self.assertRaisesRegex(d.Refused,'승인한 후보와 불일치'):
                d.resume(h,replacement)
            self.assertFalse(self.calls)
            self.assertFalse((h/'resume-attempt.json').exists())
            for n in d.NAMES:
                self.assertEqual(self.tags[d.prod(n)],old[n])
                self.assertEqual(d.number(d.mask(n)),d.number(d.mask_anchor(h,n)))

        foreign = self.root / 'd-cutover-other'
        foreign.mkdir(mode=0o700)
        for f in ('previous.json','quiesced'):
            (foreign/f).write_bytes((h/f).read_bytes())
        with self.assertRaisesRegex(d.Refused,'다른 hold/boot'):
            d.resume(foreign,target)
        self.assertFalse(self.calls)
        self.assertFalse((foreign/'resume-attempt.json').exists())

        state = json.loads((h/'previous.json').read_bytes())
        replacement = {**target, 'images':{**self.ids,d.C:alternate}}
        state['candidate'] = replacement
        (h/'previous.json').write_text(json.dumps(state))
        with self.assertRaisesRegex(d.Refused,'quiesce 영수증'):
            d.resume(h,replacement)
        self.assertFalse(self.calls)
        self.assertFalse((h/'resume-attempt.json').exists())

    def resume_inputs(self):
        self.patch('gate',lambda *args:None)
        self.patch('healthy',lambda *args:None)
        self.patch('issuer',lambda *args:None)
        self.patch('acceptance',lambda *args:None)

    def test_accepted_candidate_is_not_reheld(self):
        self.resume_inputs()
        self.resume()
        self.assertEqual(json.loads((self.h/'accepted.json').read_bytes())['generations'],self.executions)
        self.assertTrue(all(not os.path.lexists(d.mask(n)) for n in d.NAMES))
        self.assertFalse(any(c[:2] == ('systemctl','stop') or c[4:5] == ('arm',) for c in self.calls))

    def starts_and_stops(self, stops):
        for action,names in (('start',(d.I,d.C)),('stop',stops)):
            self.assertEqual([c[-1] for c in self.calls if c[:2] == ('systemctl',action)],
                             [n+'.service' for n in names])
        self.assertFalse((self.h/'accepted.json').exists())

    def interrupted_notify(self, sig):
        self.resume_inputs()
        captures, checks = [], []
        def generation(n):
            captures.append((n,n in self.guards))
            return self.generation(n)
        def interrupt(signum,frame):
            self.assertIn((d.C,False),captures)
            raise InterruptedError('cancelled')
        def notify(n):
            if n == d.C:
                self.assertEqual(self.props[n]['ActiveState'],'activating')
                signal.raise_signal(sig)
        self.addCleanup(signal.signal,sig,signal.signal(sig,interrupt))
        self.on_start = notify
        self.patch('generation',generation)
        self.patch('acceptance',lambda h,s,n,g:checks.append(n))
        with self.assertRaisesRegex(InterruptedError,'cancelled'):
            self.resume()
        self.assertEqual(checks,[d.I])
        self.starts_and_stops((d.I,d.C))
        for n in d.NAMES:
            self.assertEqual((self.h/(n+'.candidate-stop')).read_bytes(),self.executions[n]['inv'].encode())
            self.assertTrue(d.mask(n).is_symlink())

    def test_term_during_notify_keeps_original_start_until_capture(self):
        self.interrupted_notify(signal.SIGTERM)

    def test_hup_during_notify_keeps_original_start_until_capture(self):
        self.interrupted_notify(signal.SIGHUP)

    def lost_client(self, exc):
        self.resume_inputs()
        def lost(n):
            if n == d.C:
                raise exc
        self.on_start = lost

    def test_timeout_observes_same_notify_job_under_guard(self):
        self.lost_client(d.subprocess.TimeoutExpired('systemctl',245))
        def settle(delay):
            self.assertIn(d.C,self.guards)
            self.assertEqual(self.props[d.C]['ActiveState'],'activating')
            self.props[d.C].update(ActiveState='active',Job='')
        with patch.object(d.time,'sleep',settle), self.assertRaises(d.subprocess.TimeoutExpired):
            self.resume()
        self.starts_and_stops((d.I,d.C))
        self.assertEqual((self.h/(d.C+'.candidate-stop')).read_bytes(),self.executions[d.C]['inv'].encode())

    def test_client_loss_does_not_follow_replacement_job(self):
        self.lost_client(OSError('client gone'))
        def replace(delay):
            self.assertIn(d.C,self.guards)
            self.executions[d.C].update(inv='f'*32,cid='f'*64)
            self.props[d.C].update(ActiveState='active',Job='',InvocationID='f'*32)
        with patch.object(d.time,'sleep',replace), self.assertRaisesRegex(d.Refused,'복원 미확인'):
            self.resume()
        self.starts_and_stops((d.I,))
        self.assertEqual(self.guards,{d.C})
        self.assertFalse((self.h/(d.C+'.candidate-stop.json')).exists())

    def test_surviving_notify_observation_is_bounded(self):
        self.lost_client(d.subprocess.TimeoutExpired('systemctl',245))
        with patch.object(d.time,'monotonic',side_effect=[100,191]), self.assertRaisesRegex(d.Refused,'복원 미확인'):
            self.resume()
        self.starts_and_stops((d.I,))
        self.assertEqual(self.guards,{d.C})
        self.assertFalse((self.h/(d.C+'.candidate-stop.json')).exists())

    def test_first_capture_rejects_active_replacement_of_completed_job(self):
        self.resume_inputs()
        checks = []
        self.patch('acceptance',lambda h,s,n,g:checks.append(n))
        def replace(n):
            if n == d.C:
                self.executions[n].update(inv='f'*32,cid='f'*64)
                self.props[n]['InvocationID'] = 'f'*32
        self.on_start = replace
        with self.assertRaisesRegex(d.Refused,'복원 미확인'):
            self.resume()
        self.assertEqual(checks,[d.I])
        self.starts_and_stops((d.I,))
        self.assertEqual(self.guards,{d.C})

    def test_business_failure_cannot_authorize_changed_candidate_cid(self):
        self.resume_inputs()
        def reject(h,s,n,g):
            if n == d.C:
                self.executions[n].update(cid='f'*64,started='480',pid='125')
                self.fail_candidate()
        self.patch('acceptance',reject)
        with self.assertRaisesRegex(d.Refused,'복원 미확인'):
            self.resume()
        self.starts_and_stops((d.I,))
        self.assertEqual(self.guards,{d.C})

    def fail_write(self, filename):
        metadata = d.metadata
        def full(path,value):
            if path.name == filename:
                raise OSError('disk full')
            metadata(path,value)
        self.patch('metadata',full)

    def rejected_but_stopped(self):
        with self.assertRaisesRegex(d.Refused,'복원 미확인'):
            self.resume()
        self.starts_and_stops((d.I,d.C))
        self.assertEqual(self.guards,set(d.NAMES))
        self.assertTrue(all(self.props[n]['ActiveState'] == 'inactive' for n in d.NAMES))
        self.assertFalse(any(c[4:5] == ('seal',) for c in self.calls))

    def test_before_rehold_write_alone_does_not_skip_physical_stop(self):
        self.resume_inputs()
        self.patch('acceptance',lambda h,s,n,g:None if n == d.I else self.fail_candidate())
        self.fail_write(d.C+'.before-rehold.json')
        self.rejected_but_stopped()
        self.assertFalse((self.h/(d.C+'.before-rehold.json')).exists())
        self.assertTrue((self.h/(d.I+'.before-rehold.json')).is_file())

    def test_acceptance_receipt_write_failure_does_not_skip_physical_stop(self):
        self.resume_inputs()
        self.fail_write('.accepted-pending.json')
        self.rejected_but_stopped()

    def test_accepted_directory_sync_failure_retracts_only_owned_link(self):
        self.resume_inputs()
        sync = d.sync_dir
        def fail_publish(path):
            if path == self.h and (self.h/'accepted.json').exists():
                raise OSError('sync failed')
            sync(path)
        self.patch('sync_dir',fail_publish)
        self.rejected_but_stopped()

    def test_static_receipt_cannot_open_candidates(self):
        for marker in (b'kapu-d-v1\n',b'kapu-d-guarded-stop-v1\n',b'kapu-d-guarded-stop-v4 pooled-to-direct\n',d.QUIESCED):
            with self.subTest(marker=marker):
                (self.h/'quiesced').write_bytes(marker)
                with self.assertRaisesRegex(ValueError,'quiesce 영수증'):
                    self.resume()
                self.assertFalse(self.calls)
                self.assertFalse((self.h/'resume-attempt.json').exists())

    def test_new_marker_requires_both_genuine_stop_receipts(self):
        path = self.h/(d.I+'.stopped.json')
        result = json.loads(path.read_bytes())
        result['cid'] = self.state['old'][d.C]['cid']
        path.write_text(json.dumps(result))
        with self.assertRaisesRegex(ValueError,'정지 영수증 불일치'):
            self.resume()
        self.assertFalse(self.calls)

    def interrupt_unapplied_unlink(self):
        self.patch('gate',lambda *args:None)
        def interrupt(*args, **kwargs):
            if args == ('systemctl','daemon-reload') and not os.path.lexists(d.mask(d.I)):
                raise InterruptedError()
            return self.command(*args,**kwargs)
        self.patch('run',interrupt)

    def test_unapplied_unlink_restores_only_same_mask_without_stop(self):
        self.interrupt_unapplied_unlink()
        with self.assertRaises(InterruptedError):
            self.resume()
        self.assertFalse(any(x[:2] in (('systemctl','stop'),('systemctl','start')) or x[4:5] == ('arm',)
                             for x in self.calls))
        for n in d.NAMES:
            self.assertEqual(d.number(d.mask(n)), d.number(d.mask_anchor(self.h,n)))
        self.assertFalse((self.h/(d.C+'.before-rehold.json')).exists())

    def partial_unlink_with_container(self, subject):
        self.interrupt_unapplied_unlink()
        def absent(kind,name):
            if not os.path.lexists(d.mask(d.I)) and name == subject:
                raise d.Refused('컨테이너가 남음')
        self.patch('absent',absent)
        with self.assertRaisesRegex(d.Refused,'복원 미확인'):
            self.resume()
        self.assertFalse(os.path.lexists(d.mask(d.I)))
        self.assertTrue(d.mask_anchor(self.h,d.I).is_symlink())
        self.reload()
        self.assertIn(d.I,self.guards)
        self.assertFalse(any(x[:2] in (('systemctl','stop'),('systemctl','start')) or x[4:5] == ('undo-unmask',)
                             for x in self.calls))

    def test_partial_unlink_cannot_seal_over_retained_cid(self):
        self.partial_unlink_with_container(self.state['old'][d.I]['cid'])

    def test_partial_unlink_cannot_seal_over_occupied_name(self):
        self.partial_unlink_with_container(d.I)

    def test_uncertain_second_unmask_protects_reboot_and_stops_accepted_issuer(self):
        self.resume_inputs()
        def interrupt(*args, **kwargs):
            if args == ('systemctl','daemon-reload') and not os.path.lexists(d.mask(d.C)):
                raise InterruptedError()
            return self.command(*args,**kwargs)
        def absent(kind,name):
            if not os.path.lexists(d.mask(d.C)) and name == self.state['old'][d.C]['cid']:
                raise d.Refused('이전 CID 부재 미확인')
        self.patch('run',interrupt)
        self.patch('absent',absent)
        with self.assertRaisesRegex(d.Refused,'복원 미확인'):
            self.resume()
        self.reload()
        self.assertIn(d.C,self.guards)
        self.assertEqual([c[-1] for c in self.calls if c[:2] == ('systemctl','stop')], [d.I+'.service'])
        self.assertEqual(self.props[d.I]['MainPID'],'0')
        self.assertFalse((self.h/'accepted.json').exists())

    def test_candidate_stop_cannot_reuse_original_issuer_143(self):
        self.patch('gate',lambda *args:None)
        self.patch('acceptance',lambda *args: self.fail_candidate())
        self.outcomes[d.I] = {'ActiveState':'failed','InvocationID':self.state['old'][d.I]['inv'],
                              'Result':'exit-code','ExecMainStatus':'143'}
        with self.assertRaisesRegex(ValueError,'복원 미확인'):
            self.resume()
        self.assertEqual(self.guards,{d.I})
        self.assertTrue(d.mask(d.C).is_symlink())
        self.assertFalse((self.h/(d.I+'.candidate-stop')).exists())
        result = json.loads((self.h/(d.I+'.candidate-stop.json')).read_bytes())
        self.assertEqual((result['invocation'],result['InvocationID']), ('e'*32,'b'*32))

    def fail_candidate(self):
        raise ValueError('후보 거부')

    def test_second_open_failure_preserves_both_candidate_stop_results(self):
        self.patch('gate',lambda *args:None)
        self.patch('healthy',lambda *args:None)
        self.patch('issuer',lambda *args:None)
        self.patch('acceptance',lambda h,s,n,g:None if n == d.I else self.fail_candidate())
        self.outcomes[d.C] = {'ActiveState':'failed','Result':'exit-code','ExecMainStatus':'143'}
        with self.assertRaisesRegex(ValueError,'복원 미확인'):
            self.resume()
        self.assertEqual([c[-1] for c in self.calls if c[:2] == ('systemctl','stop')],
                         [d.I+'.service',d.C+'.service'])
        self.assertEqual((self.h/(d.I+'.candidate-stop')).read_bytes(),b'e'*32)
        self.assertEqual(self.guards,{d.C})
        self.assertTrue(d.mask(d.I).is_symlink())
        result = json.loads((self.h/(d.C+'.candidate-stop.json')).read_bytes())
        self.assertEqual((result['invocation'],result['Result'],result['ExecMainStatus']), ('e'*32,'exit-code','143'))
        self.assertFalse((self.h/'accepted.json').exists())

    def test_wrong_second_digest_rejects_before_mutation(self):
        self.digests[d.I] = 'f'*64
        with self.assertRaises(ValueError):
            self.resume()
        self.assertFalse(self.calls)
        self.assertFalse((self.h/'resume-attempt.json').exists())

    def test_failed_sql_cannot_release_either_service(self):
        def refuse(*args):
            self.assertTrue(all(d.mask(n).is_symlink() for n in d.NAMES))
            self.assertEqual([self.tags[d.prod(n)]['id'] for n in d.NAMES], list(self.ids.values()))
            raise ValueError('SQL')
        self.patch('gate',refuse)
        with self.assertRaisesRegex(ValueError,'SQL'):
            self.resume()
        self.assertFalse(any(c[:2] == ('systemctl','start') for c in self.calls))
        self.assertFalse((self.h/'sql-passed').exists())
        self.assertTrue((self.h/'resume-attempt.json').is_file())

    def test_failed_issuer_never_releases_collector_and_restores_exact_inode(self):
        self.patch('gate',lambda *args:None)
        def unhealthy(*args):
            self.assertTrue(d.mask(d.C).is_symlink())
            self.assertFalse(os.path.lexists(d.mask(d.I)))
            raise ValueError('issuer')
        self.patch('acceptance',unhealthy)
        with self.assertRaisesRegex(ValueError,'issuer'):
            self.resume()
        self.assertFalse((self.h/'accepted.json').exists())
        self.assertEqual([c for c in self.calls if c[:2] == ('systemctl','start')],
                         [('systemctl','start','--show-transaction',d.I+'.service')])
        self.assertEqual([x[5] for x in self.calls if x[4:5] == ('arm',)], [d.I+'.service'])
        for n in d.NAMES:
            self.assertEqual(d.number(d.mask(n)), json.loads((self.h/(n+'.mask')).read_bytes()))
            self.assertEqual(self.tags[d.prod(n)]['id'], self.ids[n])

    def test_release_uses_d_profile_bytes_not_historical_pair_identity(self):
        released, source = self.root/'release', self.root/'source'
        released.mkdir()
        source.mkdir()
        self.patch('D',released)
        self.patch('SRC',source)
        contents = b'{"version": 1}\n'
        legacy = b'{"service":"collector-b","architecture":"arm64"}\n'
        (released/'profile.json').write_bytes(contents)
        (source/(d.SERVICE + '.json')).write_bytes(contents)
        (released/'pair.json').write_bytes(legacy)
        (released/'SHA256SUMS').write_text(d.sha(contents) + '  profile.json\n' + d.sha(legacy) + '  pair.json\n')
        self.assertEqual(d.profile(),contents)
        (source/(d.SERVICE + '.json')).write_bytes(contents.strip())
        with self.assertRaisesRegex(ValueError,'drift'):
            d.profile()

    def test_foreign_mask_not_removed_or_stopped(self):
        self.patch('gate',lambda *args:None)
        def replace(*args):
            d.mask(d.I).symlink_to('/dev/null')
            raise ValueError('replaced')
        self.patch('acceptance',replace)
        with self.assertRaisesRegex(ValueError,'미확인'):
            self.resume()
        self.assertNotEqual(d.number(d.mask(d.I)), json.loads((self.h/(d.I+'.mask')).read_bytes()))
        self.assertFalse(any(c[:2] == ('systemctl','stop') for c in self.calls))

    def test_completed_stop_without_matching_manager_event_is_not_a_receipt(self):
        for result in ('', json.dumps({'INVOCATION_ID': 'c'*32})):
            with patch.object(d, 'run', side_effect=['', result]), self.assertRaisesRegex(ValueError, 'stop 미확인'):
                d.stop_invocation(d.I, 'b'*32, '@1234.000056')

    def test_143_requires_exact_deliberate_issuer_stop(self):
        self.props[d.I].update(ActiveState='failed', InvocationID='b'*32, Result='exit-code', ExecMainCode='1',
                               ExecMainStatus='143', MainPID='0', ControlPID='0')
        d.held_one(self.h,self.state,d.I)
        for key,value in [('InvocationID','c'*32),('Result','timeout'),('ExecMainStatus','1'),('MainPID','1')]:
            with patch.dict(self.props[d.I],{key:value}), self.assertRaises(ValueError):
                d.held_one(self.h,self.state,d.I)
        self.props[d.C].update(self.props[d.I])
        with self.assertRaises(ValueError):
            d.held_one(self.h,self.state,d.C)


if __name__ == '__main__':
    unittest.main()
