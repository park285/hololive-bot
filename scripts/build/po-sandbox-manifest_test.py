#!/usr/bin/env python3
"""Docker 저장소별 identity와 검증된 archive의 결합을 검사한다."""
import hashlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
MANIFEST = ROOT / "scripts/build/po-sandbox-manifest.py"
VERIFIER = ROOT / "scripts/deploy/lib/po-sandbox-image.sh"


def digest(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


class ImageIdentityTests(unittest.TestCase):
    def archive(self, directory, wrong_config=False):
        config = json.dumps({"architecture": "arm64", "os": "linux"}).encode()
        config_id = digest(config)
        descriptor = json.dumps({"config": {"digest": "sha256:" + "0" * 64 if wrong_config else config_id}}).encode()
        image_id = digest(descriptor)
        config_path = "blobs/sha256/" + config_id[7:]
        entries = {
            "manifest.json": json.dumps([{"Config": config_path}]).encode(),
            config_path: config,
            "blobs/sha256/" + image_id[7:]: descriptor,
        }
        archive = Path(directory) / "image.tar"
        with tarfile.open(archive, "w") as target:
            for name, data in entries.items():
                member = tarfile.TarInfo(name)
                member.size = len(data)
                target.addfile(member, io.BytesIO(data))
        return archive, image_id, config_id

    def export_ids(self, archive, image_id):
        return subprocess.run([sys.executable, "-B", str(MANIFEST), "image-ids", str(archive), image_id],
                              capture_output=True, text=True, timeout=10)

    def matches(self, actual, expected):
        return subprocess.run(["bash", "-uc", 'source "$1"; po_image_id_matches "$2" "$3"',
                               "identity-test", str(VERIFIER), actual, expected],
                              capture_output=True, text=True, timeout=10).returncode == 0

    def test_same_archive_admits_classic_and_containerd_identities(self):
        with tempfile.TemporaryDirectory() as directory:
            archive, manifest_id, config_id = self.archive(directory)
            result = self.export_ids(archive, manifest_id)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(set(result.stdout.splitlines()), {manifest_id, config_id})
        self.assertTrue(self.matches(manifest_id, result.stdout.strip()))
        self.assertTrue(self.matches(config_id, result.stdout.strip()))
        self.assertFalse(self.matches("sha256:" + "f" * 64, result.stdout.strip()))

    def test_descriptor_for_different_config_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            archive, manifest_id, _ = self.archive(directory, wrong_config=True)
            self.assertNotEqual(self.export_ids(archive, manifest_id).returncode, 0)

    def test_same_daemon_rollback_identity_remains_exact(self):
        recorded = "sha256:" + "a" * 64
        self.assertTrue(self.matches(recorded, recorded))
        self.assertFalse(self.matches("sha256:" + "b" * 64, recorded))

    def test_identity_set_rejects_malformed_or_extra_entries(self):
        recorded = "sha256:" + "a" * 64
        for expected in ["", recorded + "\ninvalid", recorded + "\n" + "sha256:" + "b" * 64 + "\n" + "sha256:" + "c" * 64]:
            with self.subTest(expected=expected):
                self.assertFalse(self.matches(recorded, expected))


if __name__ == "__main__":
    unittest.main()
