#!/usr/bin/env python3
"""원 Quadlet의 정지 명령을 보존하면서 같은 writer의 재기동을 먼저 차단한다."""
import argparse
import errno
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time


UNITS = {'hololive-api.service', 'hololive-alarm-worker.service'} | {
    f'hololive-youtube-{kind}-{slot}.service' for kind in ('po', 'collector') for slot in 'bcd'
}
BUS = 'org.freedesktop.systemd1'


def require(ok, message):
    if not ok:
        raise ValueError(message)


def identity(st):
    return f'{st.st_dev}:{st.st_ino}'


def directory(path, owner):
    st = path.lstat()
    require(stat.S_ISDIR(st.st_mode) and st.st_uid == owner and not st.st_mode & 0o022,
            f'신뢰할 수 없는 디렉터리: {path}')
    return st


def sync_dir(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


class Manager:
    def __init__(self, unit, system):
        self.unit = unit
        self.scope = '--system' if system else '--user'
        self.env = {'PATH': '/usr/bin:/bin', 'LC_ALL': 'C'}
        if not system:
            self.env.update({k: os.environ[k] for k in ('XDG_RUNTIME_DIR', 'DBUS_SESSION_BUS_ADDRESS')})
        self.obj = '/org/freedesktop/systemd1/unit/' + ''.join(
            c if c.isalnum() else f'_{ord(c):02x}' for c in unit)

    def run(self, cmd, *args):
        p = subprocess.run(['/usr/bin/' + cmd, self.scope, *args], stdin=subprocess.DEVNULL,
                           stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, env=self.env, timeout=45)
        require(p.returncode == 0, f'{cmd} {self.unit}: 명령 실패 ({p.returncode})')
        return p.stdout.decode()

    def reload(self):
        self.run('systemctl', 'daemon-reload')

    def fields(self, *keys):
        out = self.run('systemctl', 'show', self.unit, '--property=' + ','.join(keys))
        result = dict(line.split('=', 1) for line in out.splitlines())
        return tuple(result[k] for k in keys)

    def property(self, interface, name, signature):
        obj = json.loads(self.run('busctl', '--json=short', 'get-property', BUS, self.obj,
                                  BUS + '.' + interface, name))
        require(obj['type'] == signature, f'{name}: 지원하지 않는 D-Bus 형식')
        return obj['data']

    def stop_definition(self):
        load, canonical, source, *policy = self.fields(
            'LoadState', 'Id', 'FragmentPath', 'KillMode', 'KillSignal', 'FinalKillSignal',
            'SendSIGKILL', 'TimeoutStopUSec', 'TimeoutStopFailureMode')
        require(load == 'loaded' and canonical == self.unit and source and source != '/dev/null',
                '원래 unit이 로드되지 않음')
        commands = []
        for prop in ('ExecStopEx', 'ExecStopPostEx'):
            rows = self.property('Service', prop, 'a(sasasttttuii)')
            # 뒤쪽 PID·시각·반환 코드는 정지 결과이며 명령 정의가 아니다.
            commands.append([row[:3] for row in rows])
        require(commands[0] and all(row[0] and row[1] for row in commands[0]), '원래 ExecStop 없음')
        raw = json.dumps([source, policy, commands], separators=(',', ':')).encode()
        return hashlib.sha256(raw).hexdigest()

    def condition(self, anchor):
        conditions = self.property('Unit', 'Conditions', 'a(sbbsi)')
        require(any(row[:4] == ['ConditionPathExists', False, True, str(anchor)] for row in conditions),
                'manager에 소유 anchor의 필수 부정 기동 조건이 없음')

    def terminal(self):
        state, main, control, job = self.fields('ActiveState', 'MainPID', 'ControlPID', 'Job')
        require(state in ('inactive', 'failed') and main == control == '0' and job == '',
                '실행 PID, 진행 중 job 또는 미확인 상태가 남음')


class Guard:
    def __init__(self, unit, anchor, expected):
        require(unit in UNITS, '지원하지 않는 writer unit')
        self.owner = os.geteuid()
        test_root = os.environ.get('HOLOLIVE_QUADLET_TEST_ROOT')
        if self.owner == 0:
            require(not test_root, 'root에 대한 시험 경로 금지')
            root = Path('/')
        else:
            require(test_root, 'root 또는 명시적인 비root 시험 경로 필요')
            root = Path(test_root)
            require(root.is_absolute() and root.resolve(strict=True) == root and
                    re.fullmatch(r'/[A-Za-z0-9._/-]+', str(root)), '시험 루트 불일치')
            runtime = Path(os.environ['XDG_RUNTIME_DIR'])
            require(runtime.parent == root and runtime.resolve(strict=True) == runtime and
                    os.environ['DBUS_SESSION_BUS_ADDRESS'] == f'unix:path={runtime}/bus', '독립 시험 bus 필요')
            directory(runtime, self.owner)
        directory(root, self.owner)
        self.system = root / 'etc/systemd/system'
        for path in (root/'etc', root/'etc/systemd', self.system):
            directory(path, self.owner)
        self.anchor = Path(anchor)
        require(self.anchor.parent == self.system and re.fullmatch(r'\.[A-Za-z0-9][A-Za-z0-9_.-]*\.mask', self.anchor.name),
                'unit 디렉터리 내 전용 mask anchor 필요')
        self.expected = expected
        self.check_anchor()
        self.mask = self.system / unit
        self.dropdir = self.system / (unit + '.d')
        self.drop = self.dropdir / '90-hololive-stop-guard.conf'
        self.side = self.system / (self.anchor.name + '.guard')
        self.manager = Manager(unit, self.owner == 0)
        if test_root:
            paths = self.manager.run('systemctl', 'show', '--property=UnitPath', '--value').split()
            require(str(self.system) in paths, '시험 경로가 독립 manager의 unit 경로가 아님')

    def check_anchor(self):
        st = self.anchor.lstat()
        require(stat.S_ISLNK(st.st_mode) and st.st_uid == self.owner and identity(st) == self.expected and
                os.readlink(self.anchor) == '/dev/null', '소유 anchor 변경')

    def payload(self, created, stop_digest):
        d = directory(self.dropdir, self.owner)
        return (f'# anchor={self.expected} directory={identity(d)} created={int(created)} stop={stop_digest}\n'
                f'[Unit]\nConditionPathExists=!{self.anchor}\n').encode()

    def owned_dropin(self, stop_digest):
        self.check_anchor()
        directory(self.dropdir, self.owner)
        st = self.side.lstat()
        require(stat.S_ISREG(st.st_mode) and stat.S_IMODE(st.st_mode) == 0o600 and st.st_uid == self.owner and
                st.st_nlink == 2 and os.path.samestat(st, self.drop.lstat()), '소유 기동 차단 파일 불일치')
        regular, created = self.payload(False, stop_digest), self.payload(True, stop_digest)
        require(st.st_size == len(regular), '기동 차단 파일 크기 불일치')
        with os.fdopen(os.open(self.side, os.O_RDONLY | os.O_NOFOLLOW), 'rb') as f:
            require(os.path.samestat(st, os.fstat(f.fileno())), '기동 차단 파일 교체')
            data = f.read(len(regular) + 1)
        require(data in (regular, created), '기동 차단 또는 원래 unit의 정지 정의 변경')
        return data == created

    def arm(self):
        require(not any(os.path.lexists(p) for p in (self.mask, self.drop, self.side)), '기존 mask/기동 차단 인수 거부')
        before = self.manager.stop_definition()
        created = not os.path.lexists(self.dropdir)
        if created:
            self.dropdir.mkdir(mode=0o755)
        data = self.payload(created, before)
        with os.fdopen(os.open(self.side, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600), 'wb') as f:
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        os.link(self.side, self.drop, follow_symlinks=False)
        sync_dir(self.dropdir)
        sync_dir(self.system)
        self.manager.reload()
        self.check()

    def check(self):
        require(not os.path.lexists(self.mask), '정지 전 원래 unit이 가려짐')
        before = self.manager.stop_definition()
        self.owned_dropin(before)
        self.manager.condition(self.anchor)
        return before

    def seal(self):
        before = self.check()
        self.manager.terminal()
        os.link(self.anchor, self.mask, follow_symlinks=False)
        sync_dir(self.system)
        self.manager.reload()
        self.check_anchor()
        require(os.path.samestat(self.mask.lstat(), self.anchor.lstat()), '최종 mask inode 변경')
        load, installed = self.manager.fields('LoadState', 'UnitFileState')
        require((load, installed) == ('masked', 'masked'), f'최종 mask 미확인: {load}/{installed}')
        self.manager.terminal()
        created = self.owned_dropin(before)
        self.drop.unlink()
        self.side.unlink()
        sync_dir(self.dropdir)
        if created:
            try:
                self.dropdir.rmdir()
            except OSError as exc:
                if exc.errno != errno.ENOTEMPTY:
                    raise
        sync_dir(self.system)


def podman_exit_status(unit, cid, since):
    stamp = re.fullmatch(r'@([1-9][0-9]{0,10})\.([0-9]{6})', since)
    require(re.fullmatch(r'[0-9a-f]{64}', cid) and stamp, '원래 컨테이너 ID와 stop epoch 마이크로초 필요')
    seconds, micros = stamp.groups()
    lower = int(seconds) * 1_000_000_000 + int(micros) * 1000
    cmd = ['/usr/bin/podman', '--remote=false', 'events', '--stream=false', '--since', seconds,
           '--filter', 'type=container', '--filter', 'container=' + cid, '--filter', 'event=died',
           '--format', '{{.ID}}|{{.Name}}|{{.TimeNano}}|{{.ContainerExitCode}}']
    env = {'PATH': '/usr/bin:/bin', 'HOME': '/root', 'LC_ALL': 'C'} if os.geteuid() == 0 else None
    p = subprocess.run(cmd, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                       env=env, timeout=25)
    require(p.returncode == 0, f'{unit}: Podman 종료 이벤트 조회 실패 ({p.returncode})')
    upper = time.time_ns()
    results = []
    for row in p.stdout.decode().splitlines():
        fields = row.split('|')
        require(len(fields) == 4 and fields[0] == cid and fields[1] == unit.removesuffix('.service') and
                re.fullmatch(r'[1-9][0-9]*', fields[2]), '원래 컨테이너의 종료 이벤트 식별 불가')
        if int(fields[2]) >= lower:
            require(int(fields[2]) <= upper, '컨테이너 종료 시각이 현재 시각보다 늦음')
            require(re.fullmatch(r'0|[1-9][0-9]{0,2}', fields[3]) and int(fields[3]) <= 255,
                    'Podman 종료 코드 미확인')
            results.append(int(fields[3]))
    require(len(results) == 1, f'{unit}: 이번 stop에 해당하는 Podman 종료 이벤트 {len(results)}건')
    return results[0]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    actions = parser.add_subparsers(dest='action', required=True)
    for name in ('arm', 'check', 'seal'):
        guard = actions.add_parser(name)
        guard.add_argument('unit', choices=sorted(UNITS))
        guard.add_argument('anchor')
        guard.add_argument('device_inode')
    exited = actions.add_parser('exit-status')
    exited.add_argument('unit', choices=sorted(UNITS))
    exited.add_argument('cid')
    exited.add_argument('since')
    args = parser.parse_args()
    if args.action == 'exit-status':
        print(podman_exit_status(args.unit, args.cid, args.since))
        return
    g = Guard(args.unit, args.anchor, args.device_inode)
    getattr(g, args.action)()


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, KeyError, subprocess.SubprocessError) as e:
        sys.exit(f'[stop-guard] {e}')
