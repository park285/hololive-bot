#!/usr/bin/env python3
"""활성 pair가 백업 정리 뒤에도 bind 원본으로 남는 release 저장소에서만 게시되는지 검사한다."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]
TOOL = ROOT / "scripts/deploy/lib/worker-profile-pair.py"
IMAGE = "sha256:" + "a" * 64
SERVICE = "youtube-collector"


def write_pair(directory):
    directory.mkdir(parents=True)
    profile = b'{"workers":{}}\n'
    (directory / "profile.json").write_bytes(profile)
    profile_hash = hashlib.sha256(profile).hexdigest()
    pair = {"image": IMAGE, "revision": "b" * 40, "architecture": "arm64", "os": "linux",
            "service": SERVICE, "profile_hash": profile_hash}
    (directory / "pair.json").write_text(json.dumps(pair) + "\n")
    pair_hash = hashlib.sha256((directory / "pair.json").read_bytes()).hexdigest()
    (directory / "SHA256SUMS").write_text(f"{profile_hash}  profile.json\n{pair_hash}  pair.json\n")
    (directory / "compose.yml").write_text("services: {}\n")


class ActivateStorageTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name) / "current"
        cli = Path(self.tmp.name) / "fake-docker"
        image = [{"Id": IMAGE, "Architecture": "arm64", "Os": "linux",
                  "Config": {"Labels": {"org.opencontainers.image.revision": "b" * 40}}}]
        cli.write_text("#!/bin/sh\nprintf '%s\\n' '" + json.dumps(image) + "'\n")
        cli.chmod(0o755)
        self.env = dict(os.environ, CONTAINER_CLI=str(cli))

    def tearDown(self):
        self.tmp.cleanup()

    def activate(self, directory):
        return subprocess.run([sys.executable, str(TOOL), "activate", SERVICE, str(directory), str(self.root)],
                              env=self.env, capture_output=True, text=True, timeout=30)

    def active_pair(self):
        return self.root / "artifacts/worker-profile/active" / (SERVICE + ".pair")

    def test_release_storage_pair_is_published(self):
        directory = self.root / "artifacts/worker-profile/releases/po-c-20261027T000000Z" / SERVICE / "candidate"
        write_pair(directory)
        result = self.activate(directory)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.active_pair().read_text(), f"{directory}\n")

    def test_backup_pair_is_refused_before_publication(self):
        # 2026-10-26 중앙·서울 AP: 수용 후 백업 정리가 활성 pair를 지워 collector 재생성이 실패했다.
        directory = self.root.parent / "backups/po-c-20261027T000000Z/worker-profile-candidate"
        write_pair(directory)
        result = self.activate(directory)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("artifacts/worker-profile/releases", result.stderr)
        self.assertFalse(self.active_pair().exists())

    def test_symlink_out_of_release_storage_is_refused(self):
        outside = self.root.parent / "backups/elsewhere"
        write_pair(outside)
        link = self.root / "artifacts/worker-profile/releases/link"
        link.parent.mkdir(parents=True)
        link.symlink_to(outside)
        result = self.activate(link)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.active_pair().exists())


if __name__ == "__main__":
    unittest.main()
