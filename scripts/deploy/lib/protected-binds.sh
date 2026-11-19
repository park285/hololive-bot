#!/usr/bin/env bash

verify_running_binds() {
  python3 -ISB - "$@" <<'PY'
import hashlib
import os
from pathlib import Path
import stat
import sys

pid, *pairs = sys.argv[1:]
if not pid.isdecimal() or int(pid) == 0 or not pairs:
    sys.exit('invalid protected bind inventory')
proc = Path('/proc') / pid
mounts = [row.split() for row in (proc / 'mountinfo').read_text().splitlines()]
for pair in pairs:
    host, destination = pair.split('|')
    host = Path(host)
    if not host.is_absolute() or host.resolve(strict=True) != host or not destination.startswith('/'):
        sys.exit('invalid protected bind source')
    live = proc / 'root' / destination[1:]
    entries = [row for row in mounts if row[4] == destination]
    if len(entries) != 1 or 'ro' not in entries[0][5].split(','):
        sys.exit(f'running protected bind is not read-only: {destination}')
    a, b = host.stat(), live.stat()
    if not os.path.samestat(a, b):
        if (destination not in {'/run/hololive-bot/postgres/pg_hba.conf', '/etc/pgbouncer/pg_hba.conf', '/etc/pgbouncer/stats-users.txt'}
                or not stat.S_ISREG(b.st_mode) or (a.st_uid, a.st_gid, a.st_mode) != (b.st_uid, b.st_gid, b.st_mode)):
            sys.exit(f'running protected bind differs: {destination}')
        def attrs(path):
            return {k: os.getxattr(path, k) for k in os.listxattr(path)}
        host_attrs = attrs(host)
        if not host_attrs.get('security.selinux') or host_attrs != attrs(live):
            sys.exit(f'running protected bind labels differ: {destination}')
        with host.open('rb') as canonical, live.open('rb') as mounted:
            if hashlib.file_digest(canonical, 'sha256').digest() != hashlib.file_digest(mounted, 'sha256').digest():
                sys.exit(f'running protected bind content differs: {destination}')
    print(destination, entries[0][0], a.st_dev, a.st_ino, a.st_ctime_ns, a.st_mtime_ns,
          b.st_dev, b.st_ino, b.st_nlink, b.st_ctime_ns, b.st_mtime_ns)
PY
}
