#!/usr/bin/env python3
"""허용된 AP 전송 파일만 보존/복원합니다. 비밀·데이터·로그 디렉터리는 대상이 아닙니다."""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import shutil
import stat
import sys
import tarfile
import tempfile


def relative_path(raw):
    path = PurePosixPath(raw)
    if path.is_absolute() or not path.parts or ".." in path.parts or str(path) != raw:
        raise ValueError("invalid source snapshot path")
    if any(part in {".git", "backups", "data", "logs", "runtime-config"} for part in path.parts):
        raise ValueError("source snapshot exceeds deployment scope")
    if any(part.startswith(".env") or part.endswith((".key", ".pem")) for part in path.parts):
        raise ValueError("source snapshot includes a secret path")
    return path


def checked_path(root, raw):
    relative = relative_path(raw)
    current = root
    for part in relative.parts[:-1]:
        current = current / part
        if current.is_symlink() or (current.exists() and not current.is_dir()):
            raise ValueError("source snapshot ancestor is not an owned directory")
    return root.joinpath(*relative.parts)


def digest(path):
    with path.open("rb") as handle:
        return hashlib.file_digest(handle, "sha256").hexdigest()


def capture(root, manifest, backup):
    receipt_path = backup / "source-prechange.json"
    archive_path = backup / "source-prechange.tar"
    if receipt_path.exists() or archive_path.exists():
        raise ValueError("source snapshot already exists")
    paths = [line for line in manifest.read_text().splitlines() if line]
    if len(paths) != len(set(paths)) or len(paths) > 2000:
        raise ValueError("invalid source snapshot manifest")
    records = []
    absent_dirs = set()
    total = 0
    with tarfile.open(archive_path, "w", dereference=False) as archive:
        for raw in paths:
            path = checked_path(root, raw)
            for parent in path.parents:
                if parent == root:
                    break
                if not parent.exists():
                    absent_dirs.add(parent.relative_to(root).as_posix())
            try:
                info = path.lstat()
            except FileNotFoundError:
                records.append({"path": raw, "kind": "absent"})
                continue
            if stat.S_ISREG(info.st_mode):
                kind = "file"
                total += info.st_size
            elif stat.S_ISLNK(info.st_mode):
                kind = "symlink"
            else:
                raise ValueError("source snapshot target is not a file")
            if total > 64 * 1024 * 1024:
                raise ValueError("source snapshot exceeds size limit")
            archive.add(path, arcname=raw, recursive=False)
            records.append({"path": raw, "kind": kind})
    receipt = {"schema_version": 1, "archive_sha256": digest(archive_path), "files": records,
               "absent_directories": sorted(absent_dirs, key=lambda p: (-len(PurePosixPath(p).parts), p))}
    temporary = receipt_path.with_suffix(".tmp")
    temporary.write_text(json.dumps(receipt))
    os.replace(temporary, receipt_path)
    print("AP source snapshot captured")


def restore(root, backup):
    receipt = json.loads((backup / "source-prechange.json").read_text())
    archive_path = backup / "source-prechange.tar"
    if receipt.get("schema_version") != 1 or digest(archive_path) != receipt.get("archive_sha256"):
        raise ValueError("source snapshot integrity mismatch")
    records = receipt["files"]
    expected = {item["path"] for item in records if item["kind"] != "absent"}
    with tarfile.open(archive_path, "r") as archive:
        members = archive.getmembers()
        if len(members) != len(expected) or {item.name for item in members} != expected:
            raise ValueError("source snapshot inventory mismatch")
        for item in records:
            if item["kind"] not in {"absent", "file", "symlink"}:
                raise ValueError("invalid source snapshot type")
            path = checked_path(root, item["path"])
            if path.exists() and path.is_dir() and not path.is_symlink():
                raise ValueError("source restore would replace an unexpected directory")
            if item["kind"] == "absent":
                continue
            member = archive.getmember(item["path"])
            if (item["kind"] == "file" and not member.isfile()) or (item["kind"] == "symlink" and not member.issym()):
                raise ValueError("source snapshot type mismatch")
        for item in records:
            if item["kind"] == "absent":
                checked_path(root, item["path"]).unlink(missing_ok=True)
                continue
            path = checked_path(root, item["path"])
            member = archive.getmember(item["path"])
            path.parent.mkdir(parents=True, exist_ok=True)
            fd, temporary_name = tempfile.mkstemp(prefix=".ap-restore-", dir=path.parent)
            temporary = Path(temporary_name)
            try:
                if member.issym():
                    os.close(fd)
                    temporary.unlink()
                    temporary.symlink_to(member.linkname)
                else:
                    with os.fdopen(fd, "wb") as output, archive.extractfile(member) as source:
                        shutil.copyfileobj(source, output)
                        output.flush()
                        os.fsync(output.fileno())
                    temporary.chmod(member.mode)
                    os.utime(temporary, (member.mtime, member.mtime))
                os.replace(temporary, path)
            finally:
                temporary.unlink(missing_ok=True)
    for raw in receipt["absent_directories"]:
        path = checked_path(root, raw)
        if path.exists() and not path.is_symlink() and path.is_dir() and not any(path.iterdir()):
            path.rmdir()
    print("AP source snapshot restored")


def main():
    os.umask(0o077)
    if len(sys.argv) not in (4, 5):
        raise ValueError("expected capture/restore root backup [manifest]")
    mode, root, backup = sys.argv[1], Path(sys.argv[2]).resolve(), Path(sys.argv[3]).resolve()
    if mode == "capture" and len(sys.argv) == 5:
        capture(root, Path(sys.argv[4]), backup)
    elif mode == "restore" and len(sys.argv) == 4:
        restore(root, backup)
    else:
        raise ValueError("invalid source snapshot operation")


if __name__ == "__main__":
    main()
