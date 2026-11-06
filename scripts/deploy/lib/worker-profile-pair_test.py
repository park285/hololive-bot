#!/usr/bin/env python3
"""활성 pair가 release 저장소에서만 게시되고, 중앙 drop-in이 실제 Quadlet unit을 고정하는지 검사한다."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[3]
TOOL = ROOT / "scripts/deploy/lib/worker-profile-pair.py"
UNITS = ROOT / "deploy/quadlet/hololive-bot"
QUADLET = Path("/usr/libexec/podman/quadlet")
IMAGE_HEX = "a" * 64
IMAGE = "sha256:" + IMAGE_HEX
SERVICE = "youtube-collector"


def write_pair(directory, service=SERVICE, extra=None):
    directory.mkdir(parents=True)
    profile = b'{"workers":{}}\n'
    (directory / "profile.json").write_bytes(profile)
    profile_hash = hashlib.sha256(profile).hexdigest()
    pair = {"image": IMAGE, "revision": "b" * 40, "architecture": "arm64", "os": "linux",
            "service": service, "profile_hash": profile_hash, **(extra or {})}
    (directory / "pair.json").write_text(json.dumps(pair) + "\n")
    pair_hash = hashlib.sha256((directory / "pair.json").read_bytes()).hexdigest()
    (directory / "SHA256SUMS").write_text(f"{profile_hash}  profile.json\n{pair_hash}  pair.json\n")
    (directory / "50-worker-profile.conf").write_text("[Container]\n")


class PairToolTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name) / "current"
        self.quadlet_root = Path(self.tmp.name) / "host"
        bin_dir = Path(self.tmp.name) / "bin"
        bin_dir.mkdir()
        # 중앙 서비스는 podman으로 고정된다. Podman의 .Id는 sha256: 접두어가 없다.
        image = [{"Id": IMAGE_HEX, "Architecture": "arm64", "Os": "linux",
                  "Config": {"Labels": {"org.opencontainers.image.revision": "b" * 40}}}]
        # run은 후보 검사 container다. argv를 기록하고 지정한 종료 코드를 돌려준다.
        (bin_dir / "podman").write_text(
            "#!/bin/sh\n"
            "if [ \"$1\" = run ]; then printf '%s\\n' \"$@\" > \"$FAKE_RUN_LOG\"; exit \"${FAKE_RUN_STATUS:-0}\"; fi\n"
            "printf '%s\\n' '" + json.dumps(image) + "'\n")
        (bin_dir / "podman").chmod(0o755)
        self.run_log = Path(self.tmp.name) / "run.log"
        self.env = dict(os.environ, PATH=f"{bin_dir}:{os.environ['PATH']}",
                        HOLOLIVE_QUADLET_TEST_ROOT=str(self.quadlet_root), FAKE_RUN_LOG=str(self.run_log))

    def tearDown(self):
        self.tmp.cleanup()

    def tool(self, *args):
        return subprocess.run([sys.executable, str(TOOL), *args], env=self.env,
                              capture_output=True, text=True, timeout=30)

    def activate(self, directory):
        return self.tool("activate", SERVICE, str(directory), str(self.root))

    def active_pair(self):
        return self.root / "artifacts/worker-profile/active" / (SERVICE + ".pair")

    def installed_dropin(self):
        return (self.quadlet_root / "etc/containers/systemd/hololive-bot"
                / "hololive-youtube-collector-c.container.d/50-worker-profile.conf")


    def test_backup_pair_is_refused_before_publication(self):
        # 2026-10-26 중앙·서울 AP: 수용 후 백업 정리가 활성 pair를 지워 collector 재생성이 실패했다.
        directory = self.root.parent / "backups/po-c-20261027T000000Z/worker-profile-candidate"
        write_pair(directory)
        result = self.activate(directory)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.active_pair().exists())
        self.assertFalse(self.installed_dropin().exists())

    def test_symlink_out_of_release_storage_is_refused(self):
        outside = self.root.parent / "backups/elsewhere"
        write_pair(outside)
        link = self.root / "artifacts/worker-profile/releases/link"
        link.parent.mkdir(parents=True)
        link.symlink_to(outside)
        result = self.activate(link)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.active_pair().exists())

    @unittest.skipUnless(QUADLET.exists(), "podman quadlet generator is not installed")
    def test_rollback_dropin_pins_the_central_unit(self):
        directory = self.root / "artifacts/worker-profile/releases/rb-20261104T000000Z/hololive-api/previous"
        write_pair(directory, "hololive-api", {"app_version": "13.2.0"})
        units = Path(self.tmp.name) / "units"
        shutil.copytree(UNITS, units)
        output = units / "hololive-api.container.d/50-worker-profile.conf"
        output.parent.mkdir()
        result = self.tool("rollback", "hololive-api", str(directory), str(output))
        self.assertEqual(result.returncode, 0, result.stderr)

        generated = subprocess.run([str(QUADLET), "-dryrun"], env=dict(os.environ, QUADLET_UNIT_DIRS=str(units)),
                                   capture_output=True, text=True, timeout=30, check=True).stdout
        exec_start = next(line for line in generated.splitlines()
                          if line.startswith("ExecStart=") and " --name hololive-api " in line)
        argv = exec_start.split()
        self.assertEqual(argv[-1], IMAGE)
        self.assertNotIn("${HOLOLIVE_API_IMAGE}", argv)
        self.assertIn(f"{directory}:/run/hololive-bot/worker-profile-release:ro", argv)
        profile_envs = [arg for arg in argv if arg.startswith("STACK_WORKER_PROFILE_FILE=")]
        self.assertEqual(profile_envs, ["STACK_WORKER_PROFILE_FILE=/run/hololive-bot/worker-profile-release/profile.json"])
        self.assertIn("APP_VERSION=13.2.0", argv)


    def test_check_refuses_a_pair_whose_profile_changed(self):
        directory = self.root / "artifacts/worker-profile/releases/po-c-20261104T000000Z" / SERVICE / "previous"
        write_pair(directory)
        (directory / "profile.json").write_bytes(b'{"workers":{"changed":{}}}\n')
        result = self.tool("check", SERVICE, str(directory))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.run_log.exists())

    def test_recorded_base_unit_is_covered_by_the_pair_checksum(self):
        # 롤백은 pair가 기록한 기본 unit을 설치하므로, 그 파일이 바뀐 pair는 검증에서 거부되어야 한다.
        spec = importlib.util.spec_from_file_location("worker_profile_pair", TOOL)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        directory = self.root / "artifacts/worker-profile/releases/api-20261104T000000Z/previous"
        directory.mkdir(parents=True)
        (directory / "profile.json").write_bytes(b'{"workers":{}}\n')
        (directory / module.UNIT_RECORD).write_text((UNITS / "hololive-api.container").read_text())
        module.save_pair(directory, "hololive-api",
                         {"image": IMAGE, "revision": "b" * 40, "architecture": "arm64", "os": "linux"})
        self.assertEqual(self.tool("verify", "hololive-api", str(directory)).returncode, 0)
        (directory / module.UNIT_RECORD).chmod(0o644)
        (directory / module.UNIT_RECORD).write_text("[Container]\nImage=tampered\n")
        self.assertNotEqual(self.tool("verify", "hololive-api", str(directory)).returncode, 0)

    def test_unsafe_pair_path_is_refused_for_a_dropin(self):
        directory = self.root / "artifacts/worker-profile/releases/bad%n/hololive-alarm-worker/candidate"
        write_pair(directory, "hololive-alarm-worker")
        output = Path(self.tmp.name) / "out.conf"
        result = self.tool("rollback", "hololive-alarm-worker", str(directory), str(output))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(output.exists())

    def retained_case(self):
        spec = importlib.util.spec_from_file_location("retained_worker_profile_pair", TOOL)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        self.retained_module = module
        units = self.quadlet_root / "etc/containers/systemd/hololive-bot"
        shutil.copytree(UNITS, units)
        previous = self.root / "artifacts/worker-profile/releases/api-old/candidate"
        candidate = self.root / "artifacts/worker-profile/releases/api-new/candidate"
        self.retained_pair(previous, b'{"workers":{"previous":{}}}\n')
        self.retained_pair(candidate, b'{"workers":{"candidate":{}}}\n')
        result = self.tool("activate", "hololive-api", str(previous), str(self.root))
        self.assertEqual(result.returncode, 0, result.stderr)
        return previous, candidate, units

    def retained_pair(self, directory, profile):
        directory.mkdir(parents=True)
        (directory / "profile.json").write_bytes(profile)
        (directory / "unit.container").write_bytes((UNITS / "hololive-api.container").read_bytes())
        pair = self.retained_module.save_pair(
            directory, "hololive-api",
            {"image": IMAGE, "revision": "b" * 40, "architecture": "arm64", "os": "linux"})
        self.retained_module.dropin(directory, "hololive-api", pair)

    def verify_retained(self, previous, candidate):
        return self.tool("verify-retained", "hololive-api", str(previous), str(candidate), str(self.root))

    def test_modified_candidate_dropin_is_rejected_without_repair(self):
        previous, candidate, units = self.retained_case()
        installed = units / "hololive-api.container.d/50-worker-profile.conf"
        before = installed.read_bytes()
        dropin = candidate / "50-worker-profile.conf"
        changed = dropin.read_bytes() + b"Environment=UNREVIEWED=1\n"
        dropin.chmod(0o644)
        dropin.write_bytes(changed)
        result = self.verify_retained(previous, candidate)
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(installed.read_bytes(), before)
        self.assertEqual(dropin.read_bytes(), changed)

    def test_changed_installed_unit_is_rejected_before_resume(self):
        previous, candidate, units = self.retained_case()
        unit = units / "hololive-api.container"
        unit.write_bytes(unit.read_bytes() + b"\n[Container]\nEnvironment=STACK_WORKER_PROFILE_FILE=/unreviewed\n")
        result = self.verify_retained(previous, candidate)
        self.assertNotEqual(result.returncode, 0)
        pointer = self.root / "artifacts/worker-profile/active/hololive-api.pair"
        self.assertEqual(pointer.read_text(), f"{previous}\n")

    def test_unrelated_active_profile_cannot_consume_a_prepared_release(self):
        previous, candidate, _ = self.retained_case()
        unrelated = self.root / "artifacts/worker-profile/releases/api-other/candidate"
        self.retained_pair(unrelated, b'{"workers":{"unrelated":{}}}\n')
        result = self.tool("activate", "hololive-api", str(unrelated), str(self.root))
        self.assertEqual(result.returncode, 0, result.stderr)
        result = self.verify_retained(previous, candidate)
        self.assertNotEqual(result.returncode, 0)
        pointer = self.root / "artifacts/worker-profile/active/hololive-api.pair"
        self.assertEqual(pointer.read_text(), f"{unrelated}\n")

    @unittest.skipUnless(QUADLET.exists(), "podman quadlet generator is not installed")
    def test_partial_publication_resumes_to_the_candidate_unit(self):
        previous, candidate, units = self.retained_case()
        installed = units / "hololive-api.container.d/50-worker-profile.conf"
        installed.chmod(0o644)
        installed.write_bytes((candidate / "50-worker-profile.conf").read_bytes())
        # drop-in만 새 후보인 중단 지점에서 기존 포인터와 새 후보를 함께 검증한다.
        result = self.verify_retained(previous, candidate)
        self.assertEqual(result.returncode, 0, result.stderr)
        result = self.tool("activate", "hololive-api", str(candidate), str(self.root))
        self.assertEqual(result.returncode, 0, result.stderr)
        generated = subprocess.run(
            [str(QUADLET), "-dryrun"], env=dict(os.environ, QUADLET_UNIT_DIRS=str(units)),
            capture_output=True, text=True, timeout=30, check=True).stdout
        command = next(line for line in generated.splitlines()
                       if line.startswith("ExecStart=") and " --name hololive-api " in line)
        self.assertIn(f"{candidate}:/run/hololive-bot/worker-profile-release:ro", command.split())
        self.assertEqual(command.split()[-1], IMAGE)


if __name__ == "__main__":
    unittest.main()
