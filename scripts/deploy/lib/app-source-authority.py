#!/usr/bin/env python3
import os
from pathlib import Path
import re
import stat
import sys


def checked(path, directory, modes=None):
    info = path.lstat()
    if (not (stat.S_ISDIR(info.st_mode) if directory else stat.S_ISREG(info.st_mode) and info.st_nlink == 1)
            or info.st_uid != os.geteuid() or info.st_gid != os.getegid() or info.st_mode & 0o7022
            or (modes is not None and stat.S_IMODE(info.st_mode) not in modes)
            or any(key.startswith('system.posix_acl_') or key == 'security.capability'
                   for key in os.listxattr(path, follow_symlinks=False))):
        raise ValueError('unsafe app source authority: ' + str(path))


def fail(error):
    raise error


def source(revision):
    if not re.fullmatch('[0-9a-f]{40}', revision):
        raise ValueError('expected full app source revision')
    prefix = os.environ.get('HOLOLIVE_QUADLET_TEST_ROOT', '')
    if bool(prefix) != (os.geteuid() != 0):
        raise ValueError('invalid deployment authority')
    boundary = Path(prefix or '/')
    if not boundary.is_absolute() or boundary != boundary.resolve(strict=True):
        raise ValueError('invalid deployment prefix')
    legacy = boundary / 'opt/hololive-bot/compose/current'
    if legacy.resolve(strict=True) != legacy or not legacy.is_dir():
        raise ValueError('invalid legacy data root')
    root = legacy.parent / 'app-sources' / revision
    for parent in reversed(legacy.parents):
        if parent.is_relative_to(boundary):
            checked(parent, True)
    checked(legacy, True, {0o700})
    try:
        root.parent.lstat()
    except FileNotFoundError:
        sys.exit(3)
    checked(root.parent, True, {0o700})
    checked(root, True, {0o500})
    for base, dirs, files in os.walk(root, onerror=fail):
        for name in dirs:
            checked(Path(base, name), True, {0o555})
        for name in files:
            path = Path(base, name)
            checked(path, False, {0o400} if path == root / '.source-revision' else {0o444, 0o555})
    for record in [root.parent / 'active', root / '.source-revision']:
        checked(record, False, {0o600} if record.name == 'active' else {0o400})
        if record.read_bytes() != (revision + '\n').encode('ascii'):
            raise ValueError('active app source revision mismatch')
    return root


if __name__ == '__main__':
    try:
        revision, = sys.argv[1:]
        print(source(revision))
    except (ValueError, OSError) as error:
        sys.exit(str(error))
