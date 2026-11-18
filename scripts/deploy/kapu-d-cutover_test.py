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
        self.props = {n:{'ActiveState':'inactive', 'NRestarts':'0'} for n in d.NAMES}
        self.tags = {}
        for k,v in [('UNIT',self.units),('INPUT',self.input),('SYSTEM',self.system)]:
            self.patch(k,v)
        for i,n in enumerate(d.NAMES):
            ident = {'id':'sha256:' + str(i+3)*64, 'rev':'b'*40}
            self.tags[d.prod(n)] = self.state['images'][n] = ident
            self.tags[self.ids[n]] = {'id':self.ids[n],'rev':self.rev}
            self.state['old'][n] = {'cid':str(i+6)*64}
            data = (n + '-candidate').encode()
            (self.input / d.unit(n)).write_bytes(data)
            (self.units / d.unit(n)).write_bytes(b'old')
            self.digests[n] = d.sha(data)
            d.mask(n).symlink_to('/dev/null')
            os.link(d.mask(n),d.mask_anchor(self.h,n),follow_symlinks=False)
            d.metadata(self.h / (n + '.mask'), d.number(d.mask(n)))
            d.record(self.h / (n + '.stopped'), b'b'*32)
        d.record(self.h/'quiesced', b'kapu-d-v1\n')
        d.metadata(self.h/'previous.json',self.state)
        self.patch('trusted', lambda *args:None)
        self.patch('raw', Path.read_bytes)
        self.patch('retained', lambda *args,**kwargs:None)
        self.patch('origin', lambda *args:None)
        self.patch('absent', lambda *args:None)
        self.patch('image', lambda name,tag:self.tags[tag])
        self.patch('prop', lambda n,k: ('masked' if d.mask(n).is_symlink() else 'loaded') if k == 'LoadState' else self.props[n][k])
        self.calls = []
        self.patch('run', self.command)

    def patch(self, name, value):
        self.stack.enter_context(patch.object(d,name,value))

    def command(self, *args, **kwargs):
        self.calls.append(args)
        if args[:2] == ('podman','tag'):
            self.tags[args[3]] = self.tags[args[2]]
        if args[:2] in [('systemctl','start'),('systemctl','stop')]:
            n = args[2].removesuffix('.service')
            self.props[n].update(ActiveState='active' if args[1] == 'start' else 'inactive', InvocationID='e'*32)

    def resume(self):
        d.resume(self.h,self.rev,self.ids,self.digests)

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
