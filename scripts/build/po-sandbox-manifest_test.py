#!/usr/bin/env python3
"""Podman archive identity와 런타임별 적재 identity 대조를 검사한다."""
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
MANIFEST = ROOT / "scripts/build/po-sandbox-manifest.py"
VERIFIER = ROOT / "scripts/deploy/lib/po-sandbox-image.sh"
REVISION = "b" * 40


def digest(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()


class ImageIdentityTests(unittest.TestCase):
    def archive(self, directory):
        # podman save --format docker-archive 배치: manifest.json, <config hex>.json, <layer>.tar
        config = json.dumps({"architecture": "arm64", "os": "linux"}).encode()
        config_id = digest(config)
        entries = {
            "manifest.json": json.dumps([{"Config": config_id[7:] + ".json", "RepoTags": ["localhost/x:prod"],
                                          "Layers": []}]).encode(),
            config_id[7:] + ".json": config,
        }
        archive = Path(directory) / "image.tar"
        with tarfile.open(archive, "w") as target:
            for name, data in entries.items():
                member = tarfile.TarInfo(name)
                member.size = len(data)
                target.addfile(member, io.BytesIO(data))
        return archive, config_id

    def export_ids(self, archive, image_id):
        return subprocess.run([sys.executable, "-B", str(MANIFEST), "image-ids", str(archive), image_id],
                              capture_output=True, text=True, timeout=10)

    def matches(self, actual, expected):
        return subprocess.run(["bash", "-uc", 'source "$1"; po_image_id_matches "$2" "$3"',
                               "identity-test", str(VERIFIER), actual, expected],
                              capture_output=True, text=True, timeout=10).returncode == 0

    def verify_loaded(self, runtime, reported_id, recorded):
        # 적재 호스트의 sudo·런타임 CLI를 가짜로 두고 po_verify_image의 런타임별 identity 정규화를 실행한다.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "bin").mkdir()
            (root / "bin/sudo").write_text('#!/bin/sh\n[ "$1" != -n ] || shift\nexec "$@"\n')
            (root / "bin" / runtime).write_text(
                '#!/bin/sh\ncase "$4" in\n'
                f'  *revision*) echo {REVISION} ;;\n'
                "  *Architecture*) echo linux/arm64 ;;\n"
                f"  '{{{{.Id}}}}') echo {reported_id} ;;\n"
                "  *) exit 2 ;;\nesac\n")
            for name in ("sudo", runtime):
                (root / "bin" / name).chmod(0o755)
            (root / "manifest.json").write_text(json.dumps({"schema_version": 1, "source_revision": REVISION,
                                                            "architecture": "arm64"}))
            (root / "image-id").write_text(recorded + "\n")
            return subprocess.run(["bash", "-uc", 'source "$1"; po_verify_image "$2" image:prod "$3" "$4" arm64 "$5"',
                                   "verify-test", str(VERIFIER), runtime, str(root / "manifest.json"), REVISION,
                                   str(root / "image-id")],
                                  env=dict(os.environ, PATH=f"{root / 'bin'}:{os.environ['PATH']}"),
                                  capture_output=True, text=True, timeout=10)

    def test_podman_archive_identity_is_its_config_digest(self):
        with tempfile.TemporaryDirectory() as directory:
            archive, config_id = self.archive(directory)
            result = self.export_ids(archive, config_id)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.splitlines(), [config_id])
        self.assertTrue(self.matches(config_id, result.stdout.strip()))
        self.assertFalse(self.matches("sha256:" + "f" * 64, result.stdout.strip()))

    def test_identity_that_is_not_the_archive_config_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            archive, _ = self.archive(directory)
            result = self.export_ids(archive, "sha256:" + "0" * 64)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("config digest differs", result.stderr)

    def test_podman_loaded_identity_gains_the_sha256_prefix(self):
        recorded = "sha256:" + "a" * 64
        self.assertEqual(self.verify_loaded("podman", "a" * 64, recorded).returncode, 0)
        result = self.verify_loaded("podman", "b" * 64, recorded)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("runtime=podman", result.stderr)

    def test_docker_loaded_identity_is_compared_as_reported(self):
        recorded = "sha256:" + "a" * 64
        self.assertEqual(self.verify_loaded("docker", recorded, recorded).returncode, 0)
        # 접두어 없는 값은 Docker identity가 아니다. 다른 런타임 규칙으로 고쳐 맞추지 않는다.
        self.assertNotEqual(self.verify_loaded("docker", "a" * 64, recorded).returncode, 0)

    def docker_store(self, driver_output, status=0):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "sudo").write_text('#!/bin/sh\n[ "$1" != -n ] || shift\nexec "$@"\n')
            (root / "docker").write_text(f"#!/bin/sh\necho {driver_output}\nexit {status}\n")
            for name in ("sudo", "docker"):
                (root / name).chmod(0o755)
            return subprocess.run(["bash", "-uc", 'source "$1"; po_require_docker_config_digest_store',
                                   "store-test", str(VERIFIER)],
                                  env=dict(os.environ, PATH=f"{root}:{os.environ['PATH']}"),
                                  capture_output=True, text=True, timeout=10)

    def test_ap_docker_identity_requires_the_classic_overlay2_store(self):
        self.assertEqual(self.docker_store("overlay2").returncode, 0)
        # containerd image store는 load 때 만든 manifest digest를 .Id로 보고해 kapu identity와 대조할 수 없다.
        result = self.docker_store("overlayfs")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("driver=overlayfs", result.stderr)
        self.assertNotEqual(self.docker_store("overlay2", status=1).returncode, 0)

    def test_legacy_two_line_rollback_receipt_remains_exact(self):
        recorded = "sha256:" + "a" * 64
        legacy = recorded + "\n" + "sha256:" + "c" * 64
        self.assertTrue(self.matches(recorded, recorded))
        self.assertTrue(self.matches(recorded, legacy))
        self.assertFalse(self.matches("sha256:" + "b" * 64, legacy))

    def test_identity_set_rejects_malformed_or_extra_entries(self):
        recorded = "sha256:" + "a" * 64
        for expected in ["", recorded + "\ninvalid", recorded + "\n" + "sha256:" + "b" * 64 + "\n" + "sha256:" + "c" * 64]:
            with self.subTest(expected=expected):
                self.assertFalse(self.matches(recorded, expected))


if __name__ == "__main__":
    unittest.main()
