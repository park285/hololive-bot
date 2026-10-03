#!/usr/bin/env python3
"""Reproduce the source layout probe in a fresh /tmp copy; never edit the checkout."""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

if os.uname().nodename.split('.')[0] != 'kapu':
    raise SystemExit('Compilation is restricted to kapu.')
repo = Path(sys.argv[1]).resolve()
output = Path(tempfile.mkdtemp(prefix='hololive-api-layout-repro-')).resolve()
module = output / 'hololive-api'
module.mkdir()
source = repo / 'hololive/hololive-api'
excluded = {'.git', 'data', 'logs', 'cache', '.cache', 'bin', 'node_modules', 'backups', 'artifacts'}
source_hashes = {}
for name in subprocess.check_output(['rg', '--files', '--hidden', str(source)], text=True).splitlines():
    path = Path(name)
    relative = path.relative_to(source)
    if any(part in excluded for part in relative.parts):
        continue
    if path.name.startswith('.env') or path.suffix in {'.key', '.pem', '.crt', '.md', '.sh'}:
        continue
    destination = module / relative
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(path, destination)
    source_hashes[str(relative)] = hashlib.sha256(path.read_bytes()).hexdigest()

replacements = {
    'github.com/kapu/hololive-shared': repo / 'hololive/hololive-shared',
    'github.com/kapu/hololive-dbtest': repo / 'hololive/hololive-dbtest',
    'github.com/park285/shared-go/v2': repo.parent / 'shared-go',
    'github.com/park285/iris-client-go/v3': repo.parent / 'iris-client-go',
}
work = output / 'go.work'
work.write_text('go 1.27.1\n\nuse ./hololive-api\n\nreplace (\n' + ''.join('\t' + name + ' => ' + str(path) + '\n' for name, path in replacements.items()) + ')\n')
go = os.environ.get('GO_BINARY', '/home/kapu/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.1.linux-amd64/bin/go')
env = os.environ.copy()
env.update(GOTOOLCHAIN='local', GOFLAGS='-mod=readonly', GOPROXY='off', GOSUMDB='off', GOWORK=str(work))
checks = []

def check(label, args, expected_failure=False):
    result = subprocess.run([go, *args], cwd=module, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=120)
    (output / (label + '.log')).write_text(result.stdout)
    item = {'name': label, 'exitCode': result.returncode, 'command': [go, *args]}
    if expected_failure:
        item['expectedRejected'] = result.returncode != 0 and 'use of internal package' in result.stdout
        if not item['expectedRejected']:
            raise RuntimeError('Expected internal visibility rejection was not reproduced')
    elif '-json' in args:
        events = [json.loads(line) for line in result.stdout.splitlines() if line.startswith('{')]
        item['passedTests'] = sum(event.get('Action') == 'pass' and 'Test' in event for event in events)
        item['passedPackages'] = sum(event.get('Action') == 'pass' and 'Test' not in event for event in events)
    checks.append(item)
    if not expected_failure and result.returncode:
        print(result.stdout)
        raise SystemExit(result.returncode)

baseline_pattern = '^Test(BotRuntimeCloseNilAndOnce|AdminAPIRuntimeCloseNilAndOnce|CorsOriginGuard_ForbiddenResponseContract|NormalizeCommandKey|CloneParamsWithAction|ReplyClientRequestIDShape|ReplyClientRequestIDIsStable|ReissuedReplyClientRequestID|NextReplyClientRequestID)$'
check('baseline', ['test', '-json', '-count=1', '-run', baseline_pattern, './internal/planes/bot/runtime', './internal/planes/admin/app', './internal/planes/admin/app/http', './internal/planes/bot/internal/bot/orchestration/orchcmd', './internal/planes/bot/internal/bot/orchestration/transport'])

move_rows = Path(__file__).with_name('api-move-map.tsv').read_text().splitlines()[1:]
for row in move_rows:
    _, old, new = row.split('\t')
    original = module / old
    destination = module / new
    if destination.exists():
        raise SystemExit('Destination already exists: ' + new)
    destination.parent.mkdir(parents=True, exist_ok=True)
    original.rename(destination)

helper = module / 'internal/planes/bot/runtime/http_server_helpers.go'
text = helper.read_text().replace('package runtime', 'package botruntime', 1)
names = ['StartHTTP3Server', 'ShutdownHTTP3Server', 'StartShortLinkServer', 'ShutdownShortLinkServer', 'StartMetricsServer', 'ShutdownMetricsServer', 'StartPprofServer', 'ShutdownPprofServer']
for name in names:
    text = text.replace(name, name[0].lower() + name[1:])
helper.write_text(text)
consumer = module / 'internal/planes/bot/runtime/runtime_http_server.go'
text = consumer.read_text().replace('\n\tappruntime "github.com/kapu/hololive-api/internal/planes/bot/internal/app/runtime"\n', '\n')
for name in names:
    text = text.replace('appruntime.' + name, name[0].lower() + name[1:])
consumer.write_text(text)
for path in (module / 'internal/planes/admin/runtime').glob('*.go'):
    path.write_text(re.sub(r'^package app$', 'package adminruntime', path.read_text(), flags=re.M))
external_test = module / 'internal/planes/bot/internal/orchestration/orchcmd/router_external_test.go'
external_test.write_text(re.sub(r'^package bot_test$', 'package orchcmd_test', external_test.read_text(), flags=re.M))
for path in module.rglob('*.go'):
    old = path.read_text()
    text = old.replace('github.com/kapu/hololive-api/internal/planes/admin/app/http', 'github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi')
    text = text.replace('github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration', 'github.com/kapu/hololive-api/internal/planes/bot/internal/orchestration')
    if path.relative_to(module).as_posix() == 'internal/app/runtime.go':
        text = text.replace('"github.com/kapu/hololive-api/internal/planes/admin/app"', 'adminruntime "github.com/kapu/hololive-api/internal/planes/admin/runtime"')
        text = text.replace('app.AdminAPIRuntime', 'adminruntime.AdminAPIRuntime').replace('app.BuildAdminAPIRuntime', 'adminruntime.BuildAdminAPIRuntime')
    if text != old:
        path.write_text(text)

# The disposable copy intentionally contains no VCS metadata; this is a compile probe, not a release artifact.
check('production-build', ['build', '-buildvcs=false', './...'])
pattern = '^Test(BotRuntimeCloseNilAndOnce|AdminAPIRuntimeCloseNilAndOnce|CorsOriginGuard_ForbiddenResponseContract|RuntimeStartPropagatesContextAndErrorChannel|RuntimeShutdownPreservesJoinedErrorsAndAttemptsAllPlanes|RuntimePlaneOrder|RuntimeCloseIsIdempotentAndOrdered|DependenciesViews_NilSafety|DependenciesViews_FieldMapping|NormalizeCommandKey|CloneParamsWithAction|CloneCommandBuildersNilSourceReturnsNil|CloneCommandBuildersProducesIndependentSlice|CommandRouterExecuteWithoutRegistryFails|CommandRouterExecutesRegisteredCommand|ReplyClientRequestIDShape|ReplyClientRequestIDIsStable|ReissuedReplyClientRequestID|NextReplyClientRequestID)$'
check('focused-final', ['test', '-json', '-count=1', '-run', pattern, './internal/planes/bot/runtime', './internal/planes/admin/runtime', './internal/planes/admin/internal/httpapi', './internal/app', './internal/planes/bot/internal/orchestration/...'])
visibility = module / 'internal/layoutvisibilityprobe'
visibility.mkdir()
(visibility / 'probe.go').write_text('package layoutvisibilityprobe\nimport _ "github.com/kapu/hololive-api/internal/planes/admin/internal/httpapi"\n')
check('visibility-negative', ['build', './internal/layoutvisibilityprobe'], expected_failure=True)
shutil.rmtree(visibility)
changed = [name for name, digest in source_hashes.items() if hashlib.sha256((source / name).read_bytes()).hexdigest() != digest]
summary = {'checks': checks, 'originalApiSourceChanged': changed, 'sourceCopiedFiles': len(source_hashes), 'moves': len(move_rows), 'scope': 'Cumulative C1+C2+C3; API production compile and focused tests; no DB/network integration or race'}
(output / 'summary.json').write_text(json.dumps(summary, indent=2))
print(json.dumps({'output': str(output), **summary}))
