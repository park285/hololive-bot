#!/usr/bin/env python3
import contextlib
import importlib.util
import json
import os
from pathlib import Path
import tempfile
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


class Cutover(unittest.TestCase):
    def test_sql_runs_once_with_only_public_ca_and_runtime_isolation(self):
        image = 'sha256:' + 'a' * 64
        with patch.object(d, 'run') as run:
            d.gate(image, 's0:c908,c909')
        profile, sql = [call.args for call in run.call_args_list]
        self.assertEqual(profile[-2:], (image, '--check-worker-profile'))
        self.assertNotIn('--env-file=' + str(d.ENVFILE), profile)
        self.assertEqual(sql[-2:], (image, '--check-pooled-database'))
        self.assertEqual([arg for arg in sql if arg.startswith(('--volume=', '--mount='))],
                         ['--volume=' + str(d.CA) + ':' + d.CERT + ':ro'])
        for arg in ['--user=1000:1000', '--group-add=1002', '--userns=auto:size=65536,gidmapping=1002:1002:1',
                    '--security-opt=label=level:s0:c908,c909', '--read-only', '--network=hololive-collector',
                    '--ip=10.89.15.14', '--env=POSTGRES_USER=hololive_collector_d', '--env=POSTGRES_PORT=6433',
                    '--env=YOUTUBE_COLLECTOR_INSTANCE_ID=youtube-collector-d', '--env-file=' + str(d.ENVFILE)]:
            self.assertIn(arg, sql)
        self.assertFalse(any('PASSWORD' in arg or '.key' in arg for arg in sql))

    def setUp(self):
        self.stack = contextlib.ExitStack()
        self.addCleanup(self.stack.close)
        self.root = Path(self.stack.enter_context(tempfile.TemporaryDirectory()))
        self.h = self.root / 'd-cutover-new'
        self.h.mkdir(mode=0o700)
        self.input = self.root / 'candidate'
        self.units = self.root / 'units'
        self.system = self.root / 'system'
        for path in self.input, self.units, self.system:
            path.mkdir()
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
        self.journal = {}
        self.tags = {}
        for k,v in [('UNIT',self.units),('INPUT',self.input),('SYSTEM',self.system)]:
            self.patch(k,v)
        for i,n in enumerate(d.NAMES):
            ident = {'id':'sha256:' + str(i+3)*64, 'rev':'b'*40}
            self.tags[d.prod(n)] = self.state['images'][n] = ident
            self.tags[self.ids[n]] = {'id':self.ids[n],'rev':self.rev}
            self.state['old'][n] = {'cid':str(i+6)*64,'inv':'b'*32,'image':ident['id'][7:]}
            self.executions[n] = {'cid':str(i+8)*64,'inv':'e'*32,'image':self.ids[n][7:]}
            data = (n + '-candidate').encode()
            (self.input / d.unit(n)).write_bytes(data)
            (self.units / d.unit(n)).write_bytes(b'old')
            self.digests[n] = d.sha(data)
            d.mask(n).symlink_to('/dev/null')
            os.link(d.mask(n),d.mask_anchor(self.h,n),follow_symlinks=False)
            d.metadata(self.h / (n + '.mask'), d.number(d.mask(n)))
            d.record(self.h / (n + '.stopped'), b'b'*32)
            d.metadata(self.h / (n + '.stopped.json'), {**self.props[n],'cid':self.state['old'][n]['cid'],
                       'invocation':'b'*32,'since':'@1234.000056','manager_stop':True,'container_exit':'0'})
        d.record(self.h/'quiesced', d.QUIESCED)
        d.metadata(self.h/'previous.json',self.state)
        self.patch('trusted', lambda *args:None)
        self.patch('raw', Path.read_bytes)
        self.patch('retained', lambda *args,**kwargs:None)
        self.patch('origin', lambda *args,**kwargs:None)
        self.patch('absent', lambda *args:None)
        self.patch('image', lambda name,tag:self.tags[tag])
        self.patch('prop', lambda n,k:self.props[n][k])
        self.patch('generation', self.generation)
        self.calls = []
        self.patch('run', self.command)

    def patch(self, name, value):
        self.stack.enter_context(patch.object(d,name,value))

    def generation(self, n):
        self.assertEqual(self.props[n]['ActiveState'],'active')
        return self.executions[n]

    def reload(self):
        for n in d.NAMES:
            self.props[n]['LoadState'] = 'masked' if os.path.lexists(d.mask(n)) else 'loaded'

    def command(self, *args, **kwargs):
        if args[:3] == ('systemctl','show','--all'):
            props = self.props[args[-1].removesuffix('.service')]
            return '\n'.join(k + '=' + props[k] for k in args[3].removeprefix('--property=').split(','))
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
            d.need(not os.path.lexists(d.mask(n)) and self.props[n]['LoadState'] == 'loaded', '가드 충돌')
            if action == 'arm':
                d.need(n not in self.guards, '이전 가드')
                self.guards.add(n)
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
        if args[:2] in [('systemctl','start'),('systemctl','stop')]:
            n = args[-1].removesuffix('.service')
            if args[1] == 'start':
                self.assertNotIn(n,self.guards)
                self.props[n].update(ActiveState='active', MainPID='123', InvocationID=self.executions[n]['inv'])
            else:
                self.assertIn(n,self.guards)
                self.assertTrue(all(peer in self.guards or os.path.lexists(d.mask(peer)) for peer in d.NAMES))
                self.assertEqual('--job-mode=ignore-dependencies' in args, n == d.I)
                self.journal[n] = self.props[n]['InvocationID']
                self.props[n].update(ActiveState='inactive', MainPID='0', ControlPID='0')
                self.props[n].update(self.outcomes.get(n,{}))
        if args[0] == 'journalctl':
            n = next(x[5:-8] for x in args if x.startswith('UNIT='))
            return json.dumps({'INVOCATION_ID': self.journal[n]})

    def resume(self):
        d.resume(self.h,self.rev,self.ids,self.digests)

    def direct_hold(self):
        h = self.root / 'd-cutover-direct'
        def snapshot(path):
            path.mkdir()
            d.record(h/'profile.json',b'profile')
            return self.state
        for n in d.NAMES:
            d.mask(n).unlink()
            self.executions[n] = dict(self.state['old'][n])
            self.props[n].update(ActiveState='active', InvocationID=self.executions[n]['inv'], MainPID='123')
        self.reload()
        self.patch('snapshot',snapshot)
        self.patch('collector',lambda *args:b'profile')
        self.patch('issuer',lambda *args:None)
        return h

    def test_original_jobs_finish_before_any_seal(self):
        h = self.direct_hold()
        self.outcomes[d.I] = {'ActiveState':'failed','Result':'exit-code','ExecMainStatus':'143'}
        self.outcomes[d.C] = {'InvocationID':'','ExecMainCode':'0'}
        d.quiesce(h)
        self.assertEqual((h/'quiesced').read_bytes(),d.QUIESCED)
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
        h = self.direct_hold()
        def swap(*args, **kwargs):
            result = self.command(*args,**kwargs)
            if args[:2] == ('systemctl','stop') and args[-1] == d.I+'.service':
                self.executions[d.C]['inv'] = 'd'*32
            return result
        self.patch('run',swap)
        with self.assertRaisesRegex(ValueError,'세대 변경'):
            d.quiesce(h)
        self.assertEqual(self.guards,set(d.NAMES))
        self.assertNotIn(('systemctl','stop',d.C+'.service'),self.calls)
        self.assertFalse((h/'quiesced').exists())

    def test_collector_143_cannot_be_hidden_by_seal_reload(self):
        h = self.direct_hold()
        self.outcomes[d.C] = {'ActiveState':'failed','Result':'exit-code','ExecMainStatus':'143'}
        with self.assertRaisesRegex(ValueError,'정상 종료 미확인'):
            d.quiesce(h)
        result = json.loads((h/(d.C+'.stopped.json')).read_bytes())
        self.assertTrue(result['manager_stop'])
        self.assertEqual((result['Result'],result['ExecMainStatus']), ('exit-code','143'))
        self.assertEqual(self.guards,set(d.NAMES))
        self.assertFalse((h/(d.C+'.stopped')).exists())
        self.assertFalse((h/'quiesced').exists())

    def test_collector_nonzero_exit_survives_manager_collection(self):
        h = self.direct_hold()
        self.exits[d.C] = '143'
        self.outcomes[d.C] = {'InvocationID':'','ExecMainCode':'0','ExecMainStatus':'0','Result':'success'}
        with self.assertRaisesRegex(ValueError,'정상 종료 미확인'):
            d.quiesce(h)
        self.assertFalse((h/'quiesced').exists())
        self.assertEqual(json.loads((h/(d.C+'.stopped.json')).read_bytes())['container_exit'],'143')
        self.assertIn(d.C,self.guards)

    def test_partial_arm_stops_neither_unit(self):
        h = self.direct_hold()
        self.guards.add(d.I)
        with self.assertRaisesRegex(ValueError,'이전 가드'):
            d.quiesce(h)
        self.assertFalse(any(c[:2] == ('systemctl','stop') for c in self.calls))
        self.assertEqual(self.guards,set(d.NAMES))
        self.assertFalse((h/'quiesced').exists())

    def test_accepted_candidate_is_not_reheld(self):
        self.patch('gate',lambda *args:None)
        self.patch('healthy',lambda *args:None)
        self.patch('issuer',lambda *args:None)
        self.patch('acceptance',lambda h,s,ids,n:self.executions[n])
        self.resume()
        self.assertEqual(json.loads((self.h/'accepted.json').read_bytes())['generations'],self.executions)
        self.assertTrue(all(not os.path.lexists(d.mask(n)) for n in d.NAMES))
        self.assertFalse(any(c[:2] == ('systemctl','stop') or c[4:5] == ('arm',) for c in self.calls))

    def test_static_receipt_cannot_open_candidates(self):
        (self.h/'quiesced').write_bytes(b'kapu-d-v1\n')
        with self.assertRaisesRegex(ValueError,'quiesce 영수증'):
            self.resume()
        self.assertFalse(self.calls)

    def test_new_marker_requires_both_genuine_stop_receipts(self):
        path = self.h/(d.I+'.stopped.json')
        result = json.loads(path.read_bytes())
        result['cid'] = self.state['old'][d.C]['cid']
        path.write_text(json.dumps(result))
        with self.assertRaisesRegex(ValueError,'정지 영수증 불일치'):
            self.resume()
        self.assertFalse(self.calls)

    def test_unapplied_unlink_restores_only_same_mask_without_stop(self):
        self.patch('gate',lambda *args:None)
        def interrupt(*args, **kwargs):
            if args == ('systemctl','daemon-reload') and not os.path.lexists(d.mask(d.I)):
                raise InterruptedError()
            return self.command(*args,**kwargs)
        self.patch('run',interrupt)
        with self.assertRaises(InterruptedError):
            self.resume()
        self.assertFalse(any(x[:2] in (('systemctl','stop'),('systemctl','start')) or x[4:5] == ('arm',)
                             for x in self.calls))
        for n in d.NAMES:
            self.assertEqual(d.number(d.mask(n)), d.number(d.mask_anchor(self.h,n)))
        self.assertFalse((self.h/(d.C+'.before-rehold.json')).exists())

    def test_candidate_stop_cannot_reuse_direct_issuer_143(self):
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
        self.patch('acceptance',lambda h,s,ids,n: self.executions[n] if n == d.I else self.fail_candidate())
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
        self.assertEqual([c for c in self.calls if c[:2] == ('systemctl','start')], [('systemctl','start', d.I+'.service')])
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
        legacy = b'{"service":"youtube-collector-b","architecture":"arm64"}\n'
        (released/'profile.json').write_bytes(contents)
        (source/'youtube-collector-d.json').write_bytes(contents)
        (released/'pair.json').write_bytes(legacy)
        (released/'SHA256SUMS').write_text(d.sha(contents) + '  profile.json\n' + d.sha(legacy) + '  pair.json\n')
        self.assertEqual(d.profile(),contents)
        (source/'youtube-collector-d.json').write_bytes(contents.strip())
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
