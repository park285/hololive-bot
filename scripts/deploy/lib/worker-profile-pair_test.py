#!/usr/bin/env python3
"""활성 pair가 release 저장소에서만 게시되고, 중앙 drop-in이 실제 Quadlet unit을 고정하는지 검사한다."""
import hashlib
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

    def test_release_storage_pair_is_published(self):
        directory = self.root / "artifacts/worker-profile/releases/po-c-20261027T000000Z" / SERVICE / "candidate"
        write_pair(directory)
        result = self.activate(directory)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.active_pair().read_text(), f"{directory}\n")
        self.assertEqual(self.installed_dropin().read_bytes(),
                         (directory / "50-worker-profile.conf").read_bytes())

    def test_backup_pair_is_refused_before_publication(self):
        # 2026-10-26 중앙·서울 AP: 수용 후 백업 정리가 활성 pair를 지워 collector 재생성이 실패했다.
        directory = self.root.parent / "backups/po-c-20261027T000000Z/worker-profile-candidate"
        write_pair(directory)
        result = self.activate(directory)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("artifacts/worker-profile/releases", result.stderr)
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

    def test_check_runs_the_pair_image_against_its_release_profile(self):
        directory = self.root / "artifacts/worker-profile/releases/po-c-20261104T000000Z" / SERVICE / "candidate"
        write_pair(directory)
        result = self.tool("check", SERVICE, str(directory))
        self.assertEqual(result.returncode, 0, result.stderr)
        argv = self.run_log.read_text().splitlines()
        self.assertEqual(argv[-2:], [IMAGE, "--check-worker-profile"])
        self.assertEqual(argv[argv.index("--network") + 1], "none")
        self.assertIn(f"type=bind,src={directory},dst=/run/hololive-bot/worker-profile-release,readonly", argv)
        self.assertIn("STACK_WORKER_PROFILE_FILE=/run/hololive-bot/worker-profile-release/profile.json", argv)

        self.env["FAKE_RUN_STATUS"] = "1"
        self.assertNotEqual(self.tool("check", SERVICE, str(directory)).returncode, 0)

    def test_check_refuses_a_pair_whose_profile_changed(self):
        directory = self.root / "artifacts/worker-profile/releases/po-c-20261104T000000Z" / SERVICE / "previous"
        write_pair(directory)
        (directory / "profile.json").write_bytes(b'{"workers":{"changed":{}}}\n')
        result = self.tool("check", SERVICE, str(directory))
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(self.run_log.exists())

    def test_recorded_base_unit_is_covered_by_the_pair_checksum(self):
        # 롤백은 pair가 기록한 기본 unit을 설치하므로, 그 파일이 바뀐 pair는 검증에서 거부되어야 한다.
        import importlib.util
        spec = importlib.util.spec_from_file_location("worker_profile_pair", TOOL)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        directory = self.root / "artifacts/worker-profile/releases/api-20261104T000000Z/previous"
        directory.mkdir(parents=True)
        (directory / "profile.json").write_bytes(b'{"workers":{}}\n')
        (directory / module.UNIT_RECORD).write_text((UNITS / "hololive-api.container").read_text())
        module.save_pair(directory, "hololive-api",
                         {"image": IMAGE, "revision": "b" * 40, "architecture": "arm64", "os": "linux"})
        self.assertIn(module.UNIT_RECORD, (directory / "SHA256SUMS").read_text())
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
        self.assertIn("not safe for a Quadlet drop-in", result.stderr)
        self.assertFalse(output.exists())


if __name__ == "__main__":
    unittest.main()
