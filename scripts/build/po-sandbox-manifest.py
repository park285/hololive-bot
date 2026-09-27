#!/usr/bin/env python3
"""Exact, symlink-safe inventory of every file shipped in the isolated issuer rootfs."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys
import tarfile


def inventory(root):
    entries = {}
    for base, dirs, files in os.walk(root, topdown=True, followlinks=False):
        for name in sorted(dirs + files):
            path = Path(base) / name
            relative = path.relative_to(root).as_posix()
            metadata = path.lstat()
            mode = stat.S_IMODE(metadata.st_mode)
            if stat.S_ISDIR(metadata.st_mode):
                entries[relative] = {"type": "directory", "mode": mode}
            elif stat.S_ISREG(metadata.st_mode):
                entries[relative] = {"type": "file", "mode": mode,
                                     "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
            elif stat.S_ISLNK(metadata.st_mode):
                target = os.readlink(path)
                resolved = ((root / target.lstrip("/")) if target.startswith("/")
                            else (path.parent / target)).resolve(strict=False)
                if not resolved.is_relative_to(root.resolve()):
                    raise ValueError(f"escaping rootfs symlink: {relative}")
                entries[relative] = {"type": "symlink", "target": target}
                if name in dirs:
                    dirs.remove(name)
            else:
                raise ValueError(f"unsupported rootfs entry: {relative}")
    return dict(sorted(entries.items()))


def verify_socket_owner(archive):
    with tarfile.open(archive) as payload:
        entries = [entry for entry in payload
                   if entry.name.rstrip("/") == "run/hololive-youtube-po"]
    if len(entries) != 1 or not entries[0].isdir() or (entries[0].uid, entries[0].gid,
                                                        entries[0].mode & 0o7777) != (65532, 1000, 0o770):
        raise ValueError("issuer socket directory ownership/mode must be 65532:1000 0770")


def main():
    if len(sys.argv) == 3 and sys.argv[1] == "verify-socket-owner":
        verify_socket_owner(sys.argv[2])
        print("issuer image socket directory ownership verified")
        return
    if len(sys.argv) != 6 or sys.argv[1] not in ("create", "verify"):
        raise ValueError("usage: po-sandbox-manifest.py create|verify ROOT MANIFEST REVISION ARCH")
    _, action, root_arg, manifest_arg, revision, arch = sys.argv
    if re.fullmatch(r"[0-9a-f]{40}", revision) is None or arch not in ("amd64", "arm64"):
        raise ValueError("full lowercase source SHA and supported architecture required")
    root = Path(root_arg).resolve(strict=True)
    if not root.is_dir():
        raise ValueError("rootfs must be a directory")
    manifest = Path(manifest_arg)
    files = inventory(root)
    required = {"app/bin/po-broker", "usr/local/bin/node", "app/po-sandbox/src/worker.mjs",
                "app/po-sandbox/package.json", "app/po-sandbox/package-lock.json",
                "run/hololive-youtube-po", "tmp"}
    if not required.issubset(files):
        raise ValueError(f"rootfs missing required issuer entries: {sorted(required - files.keys())}")
    if files["run/hololive-youtube-po"] != {"type": "directory", "mode": 0o770}:
        raise ValueError("socket directory must have mode 0770")
    if files["tmp"] != {"type": "directory", "mode": 0o1777}:
        raise ValueError("issuer temporary directory must have mode 1777")
    if action == "create":
        if manifest.exists():
            raise ValueError("refusing to overwrite immutable issuer manifest")
        manifest.write_text(json.dumps({"schema_version": 1, "source_revision": revision,
                                        "architecture": arch, "files": files}, sort_keys=True,
                                       indent=2) + "\n")
    else:
        expected = json.loads(manifest.read_text())
        if expected != {"schema_version": 1, "source_revision": revision,
                        "architecture": arch, "files": files}:
            raise ValueError("issuer rootfs manifest mismatch (revision/arch/mode/path/hash)")
    print(f"issuer rootfs {action}: revision={revision} arch={arch} entries={len(files)}")


if __name__ == "__main__":
    try:
        main()
    except (ValueError, OSError, tarfile.TarError, json.JSONDecodeError) as error:
        print(f"issuer artifact rejected: {error}", file=sys.stderr)
        sys.exit(1)
