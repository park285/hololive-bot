#!/usr/bin/env python3
"""활성 pair가 release 저장소에서만 게시되고, 중앙 drop-in이 실제 Quadlet unit을 고정하는지 검사한다."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

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

    def test_collector_dropin_changes_are_refused_before_publication(self):
        previous = self.root / "artifacts/worker-profile/releases/collector-old/previous"
        candidate = self.root / "artifacts/worker-profile/releases/collector-new/candidate"
        for directory in (previous, candidate):
            write_pair(directory)
            result = self.tool("rollback", SERVICE, str(directory))
            self.assertEqual(result.returncode, 0, result.stderr)
        result = self.activate(previous)
        self.assertEqual(result.returncode, 0, result.stderr)
        before = self.installed_dropin().read_bytes()
        pointer = self.active_pair().read_bytes()
        dropin = candidate / "50-worker-profile.conf"
        canonical = dropin.read_bytes()
        result = self.tool("verify-dropin", SERVICE, str(candidate))
        self.assertEqual(result.returncode, 0, result.stderr)
        for addition in (b"Image=unreviewed\n", b"Environment=POSTGRES_PORT=5432\n"):
            with self.subTest(addition=addition):
                dropin.chmod(0o644)
                dropin.write_bytes(canonical + addition)
                for result in (self.tool("verify-dropin", SERVICE, str(candidate)), self.activate(candidate)):
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(self.installed_dropin().read_bytes(), before)
                    self.assertEqual(self.active_pair().read_bytes(), pointer)
                    self.assertEqual(dropin.read_bytes(), canonical + addition)
        dropin.write_bytes(canonical)
        result = self.activate(candidate)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.installed_dropin().read_bytes(), canonical)

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


class CaptureRaceTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = Path(tmp.name)
        spec = importlib.util.spec_from_file_location("capture_worker_profile_pair", TOOL)
        self.tool = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.tool)
        self.tool.SOURCE = str(self.root / "mirror")
        self.tool.QUADLET_DIR = self.root / "units"
        self.release = self.root / "artifacts/worker-profile/releases/new/previous"
        self.pointer = self.root / "artifacts/worker-profile/active" / (SERVICE + ".pair")
        unit = self.tool.QUADLET_UNITS[SERVICE]
        self.overlay = self.tool.QUADLET_DIR / (unit + ".d") / self.tool.DROPIN
        self.tool.QUADLET_DIR.mkdir()
        shutil.copyfile(UNITS / unit, self.tool.QUADLET_DIR / unit)
        self.name = Path(unit).stem
        profile = Path(self.tool.SOURCE) / self.tool.PROFILES[SERVICE]
        profile.parent.mkdir()
        profile.write_bytes(b'{"workers":{}}\n')
        self.original, self.other = "c" * 64, "d" * 64
        self.named = self.original
        self.hashes = dict.fromkeys((self.original, self.other), hashlib.sha256(profile.read_bytes()).hexdigest())
        self.state = {"Running": True, "Pid": 4242, "StartedAt": "2026-11-18T00:00:00Z"}
        self.config = {"Labels": {}, "Env": ["HOLOLIVE_METRICS_ADDR=:8080",
                       f"STACK_WORKER_PROFILE_FILE={self.tool.TARGET}/{profile.name}"]}
        self.tags = set()
        self.enterContext(mock.patch.object(self.tool, "run", side_effect=self.runtime))
        self.checker = self.enterContext(mock.patch.object(self.tool, "validate_container_pair"))

    def runtime(self, cli, action, *args):
        if action == "container":
            identity = self.named if args[-1] == self.name else args[-1]
            self.assertIn(identity, self.hashes)
            return json.dumps([{"Id": identity, "Image": IMAGE_HEX, "State": self.state, "Config": self.config,
                                "Mounts": [{"Source": self.tool.SOURCE, "Destination": self.tool.TARGET}]}])
        if action == "image":
            return json.dumps([{"Id": IMAGE_HEX, "Architecture": "arm64", "Os": "linux",
                                "Config": {"Labels": {"org.opencontainers.image.revision": "b" * 40}}}])
        if action == "exec":
            identity = self.named if args[0] == self.name else args[0]
            return json.dumps({"contractVersion": 1, "profile": {"hash": self.hashes[identity]}})
        if action == "tag":
            self.tags.add(args[-1])
            return ""
        self.fail(f"예상 밖의 실행: {cli} {action}")

    def retain(self, *, labelled):
        prior = self.release.parents[1] / "old/previous"
        # 라벨은 이전 세대의 receipt도 가리킬 수 있지만, prepare 포인터는 원 ID여야 한다.
        write_pair(prior, extra={"container_id": self.other if labelled else self.original})
        pair = json.loads((prior / "pair.json").read_text())
        self.tool.dropin(prior, SERVICE, pair)
        self.tool.activate(prior, SERVICE, self.root)
        if labelled:
            self.config["Labels"][self.tool.LABEL] = str(prior)

    def assert_prepare_rejected(self):
        def installed():
            return [path.read_bytes() if path.exists() else None for path in (self.pointer, self.overlay)]

        before = installed()
        argv = [str(TOOL), "prepare", SERVICE, str(self.release), self.name, str(self.root)]
        with mock.patch.object(sys, "argv", argv), self.assertRaises(ValueError):
            self.tool.main()
        self.assertEqual(installed(), before)
        self.assertFalse(self.tags)
        for name in ("pair.json", "SHA256SUMS", self.tool.DROPIN):
            self.assertFalse((self.release / name).exists(), name)

    def reject_change_during_check(self, change):
        self.checker.side_effect = lambda *args, **kwargs: change()
        self.assert_prepare_rejected()
        self.checker.assert_called_once()

    def test_named_replacement_cannot_publish_while_original_still_exists(self):
        self.reject_change_during_check(lambda: setattr(self, "named", self.other))

    def test_stopped_source_cannot_publish(self):
        self.reject_change_during_check(lambda: self.state.update(Running=False, Pid=0))

    def test_prepared_source_restart_cannot_replace_active_pair(self):
        self.retain(labelled=False)
        self.reject_change_during_check(lambda: self.state.update(StartedAt="2026-11-18T00:00:01Z"))

    def test_labelled_source_replacement_cannot_replace_active_pair(self):
        self.retain(labelled=True)
        self.reject_change_during_check(lambda: setattr(self, "named", self.other))

    def test_replaced_retained_bytes_cannot_attest_the_loaded_profile(self):
        self.retain(labelled=True)
        profile = Path(self.config["Labels"][self.tool.LABEL]) / "profile.json"

        def during_diagnostics(cli, action, *args):
            if action == "exec":
                profile.write_bytes(b'{"workers":{"changed":{}}}\n')
            return self.runtime(cli, action, *args)

        self.tool.run.side_effect = during_diagnostics
        self.assert_prepare_rejected()
        self.checker.assert_not_called()

    def test_diagnostics_from_temporary_name_owner_cannot_attest_original(self):
        # 진단 때만 이름을 빌린 다른 컨테이너가 원본 파일과 일치하는 해시를 보고한다.
        self.hashes[self.original] = "0" * 64
        observed = []

        def borrowed_name(cli, action, *args):
            if action == "exec":
                with mock.patch.object(self, "named", self.other):
                    result = self.runtime(cli, action, *args)
                    observed.append(json.loads(result)["profile"]["hash"])
                    return result
            return self.runtime(cli, action, *args)

        self.tool.run.side_effect = borrowed_name
        self.assert_prepare_rejected()
        self.assertEqual(observed, [self.hashes[self.original]])
        self.checker.assert_not_called()


@unittest.skipIf(os.geteuid() == 0, "renderer fixtures require the non-root test path contract")
class ResolvedPairTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.units = self.root / "units"
        shutil.copytree(UNITS, self.units)
        remote = ROOT / "deploy/podman/iris-seoul"
        for source in remote.iterdir():
            if source.is_file():
                shutil.copy2(source, self.units / source.name)
        # 원격 배포와 같은 파일 목록으로 만든 payload에서 실제 렌더러를 실행한다.
        payload = self.root / "payload"
        payload.mkdir()
        subprocess.run(["rsync", "-a", "--files-from=" + str(ROOT / "scripts/deploy/po-central-files.txt"),
                        str(ROOT) + "/", str(payload) + "/"],
                       check=True, capture_output=True, text=True)
        self.tool = payload / "scripts/deploy/lib/worker-profile-pair.py"
        spec = importlib.util.spec_from_file_location("resolved_worker_profile_pair", self.tool)
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        self.module.QUADLET_DIR = self.units
        self.real_run = subprocess.run
        self.volumes = {}
        self.calls = []
        self.consumed = []
        self.rendered = []
        self.check_status = 0
        self.host = {
            "PATH": os.environ["PATH"],
            "HOLOLIVE_QUADLET_TEST_ROOT": str(self.root),
            "LIVE_LOGS_PATH": str(self.root / "logs"),
            "VALKEY_SENTINEL_ADDRS": "sentinel.invalid:26379",
            "HOLOLIVE_API_PORT_BIND_IP": "127.0.0.1",
            "HOLOLIVE_ADMIN_API_PORT_BIND_IP": "127.0.0.1",
            "HOLOLIVE_H3_SERVER_NAME": "api.example.test",
            "HOLOLIVE_DB_PASSWORD": "synthetic-api-$password",
            "HOLOLIVE_ALARM_WORKER_PASSWORD": "synthetic-worker-password",
            "HOLOLIVE_COLLECTOR_C_PASSWORD": "synthetic-collector-password",
            "HOLODEX_API_KEY": "synthetic-holodex-key",
        }
        (self.root / "logs").mkdir()
        (self.root / "secrets").mkdir()
        for name in ("bot.env", "alarm-worker.env", "youtube-collector.env"):
            (self.root / "secrets" / name).write_text(
                "POSTGRES_PASSWORD=obsolete-file-password\nHOLODEX_API_KEY=obsolete-file-key\n")
        version = self.root / "deploy/hololive/hololive-api/VERSION"
        version.parent.mkdir(parents=True)
        version.write_text("13.4.0\n")
        # 실제 렌더러가 서비스·네트워크 경계를 넘으면 즉시 실패한다.
        binary = self.root / "bin"
        binary.mkdir()
        for name in ("podman", "systemctl", "ip", "nft", "tailscale"):
            command = binary / name
            command.write_text("#!/bin/sh\nexit 99\n")
            command.chmod(0o755)
        self.host["PATH"] = f"{binary}:{self.host['PATH']}"

    def case(self, service):
        unit = self.module.QUADLET_UNITS[service]
        original = self.units / unit
        values = self.module.unit_values(original)
        defaults = dict(value.split("=", 1) for key, value in values["Service"] if key == "Environment")
        self.host = {**defaults, **self.host}
        self.directory = self.root / "pair"
        self.directory.mkdir()
        (self.directory / "profile.json").write_text('{"workers":{}}\n')
        lines = []
        for line in original.read_text().splitlines():
            if line.startswith("Volume=/"):
                source, target = line.removeprefix("Volume=").split(":", 1)
                path = self.root / "binds" / source.removeprefix("/")
                path.mkdir(parents=True, exist_ok=True)
                line = f"Volume={path}:{target}"
            elif line.startswith("EnvironmentFile=/etc/"):
                name = Path(line.split("=", 1)[1]).name
                path = self.root / ("compose.env" if name == "compose.env" else "secrets/" + name)
                line = f"EnvironmentFile={path}"
            lines.append(line)
        self.record = self.directory / self.module.UNIT_RECORD
        self.record.write_text("\n".join(lines) + "\n")
        self.pair = {"service": service, "image": IMAGE}
        self.fragments = self.root / "generated"
        self.fragments.mkdir()
        self.fragment = self.fragments / (Path(unit).stem + ".service")
        self.fragment.write_text("[X-Container]\n")
        return values

    def existing_volume(self, name):
        mountpoint = self.root / "storage/volumes" / name / "_data"
        mountpoint.mkdir(parents=True)
        (mountpoint / "socket-fixture").write_bytes(b"unchanged")
        self.volumes[name] = {"Name": name, "Mountpoint": str(mountpoint)}
        return mountpoint

    def lifecycle(self, command, **kwargs):
        args = list(command)
        self.calls.append(args)
        if args[:2] == ["systemctl", "show"]:
            unit = Path(self.module.QUADLET_UNITS[self.pair["service"]]).stem
            self.assertEqual(args[2:], [unit + ".service", "--property=FragmentPath", "--value"])
            return subprocess.CompletedProcess(args, 0, str(self.fragment))
        if args[:3] == ["podman", "volume", "inspect"]:
            if args[3] not in self.volumes:
                raise subprocess.CalledProcessError(1, args)
            return subprocess.CompletedProcess(args, 0, json.dumps([self.volumes[args[3]]]))
        if args[:2] == ["podman", "run"]:
            env = {}
            files = [args[index + 1] for index, arg in enumerate(args) if arg == "--env-file"]
            for name in files:
                path = Path(name)
                # 회귀가 생겨도 운영 /run 파일은 읽지 않고 합성 live 경로에서만 재현한다.
                if path.parent == Path("/run/hololive-bot/quadlet-env"):
                    path = self.root / "run/quadlet-env" / path.name
                content = path.read_text()
                env.update(line.split("=", 1) for line in content.splitlines() if line and not line.startswith("#"))
                if path.parent.name.startswith("hololive-profile-env-"):
                    self.assertEqual(path.stat().st_mode & 0o777, 0o600)
                    self.assertEqual(path.parent.stat().st_mode & 0o777, 0o700)
                    self.rendered.append((path, content))
            for index, arg in enumerate(args):
                if arg == "--env":
                    name, separator, value = args[index + 1].partition("=")
                    env[name] = value if separator else kwargs["env"][name]
                elif arg == "--unsetenv":
                    env.pop(args[index + 1], None)
            self.consumed.append((args, env, files))
            if self.check_status:
                raise subprocess.CalledProcessError(self.check_status, args)
            return subprocess.CompletedProcess(args, 0)
        self.assertEqual(args[:2], ["bash", str(self.tool.parent.parent / "quadlet-prestart.sh")])
        self.assertEqual(args[2], "--render-env")
        self.assertEqual(Path(args[4]).stat().st_mode & 0o777, 0o700)
        self.assertEqual(list(Path(args[4]).iterdir()), [])
        result = self.real_run(args, capture_output=True, text=True, **kwargs)
        for key in ("HOLOLIVE_DB_PASSWORD", "HOLOLIVE_ALARM_WORKER_PASSWORD",
                    "HOLOLIVE_COLLECTOR_C_PASSWORD", "HOLODEX_API_KEY"):
            if self.host.get(key):
                self.assertNotIn(self.host[key], result.stdout + result.stderr)
        return result

    def check(self):
        (self.root / "compose.env").write_text("SYNTHETIC_FIXTURE=1\n")
        with mock.patch.dict(os.environ, self.host, clear=True), \
                mock.patch.object(self.module.subprocess, "run", side_effect=self.lifecycle):
            self.module.check_resolved_pair(self.directory, self.pair)

    def assert_private_inputs(self):
        for args in self.calls:
            for secret in ("synthetic-api-$password", "synthetic-worker-password",
                           "synthetic-collector-password", "synthetic-holodex-key"):
                self.assertNotIn(secret, "\0".join(args))
        for path, _ in self.rendered:
            self.assertFalse(path.exists())

    @unittest.skipUnless(QUADLET.exists(), "podman quadlet generator is not installed")
    def test_current_collector_volumes_match_native_quadlet_without_volume_lifecycle(self):
        for service, name, flags in (
                ("youtube-collector", "hololive-bot_youtube-po-c-socket", "ro,idmap"),
                ("youtube-collector-b", "hololive-youtube-po-b-socket", "ro")):
            with self.subTest(service=service):
                # 각 사례는 별도 fixture에서 실제 unit과 .volume 선언을 사용한다.
                fixture = ResolvedPairTest()
                fixture.setUp()
                try:
                    fixture.case(service)
                    # 기본 unit 밖의 호스트 MCS 정책도 실제 generator를 거친다.
                    dropins = fixture.units / (fixture.module.QUADLET_UNITS[service] + ".d")
                    dropins.mkdir()
                    (dropins / "50-mcs-level.conf").write_text(
                        "[Container]\nSecurityLabelLevel=s0:c900,c901\n")
                    mountpoint = fixture.existing_volume(name)
                    fixture.real_run(
                        [str(QUADLET), "-no-kmsg-log", str(fixture.fragments)],
                        env=dict(os.environ, QUADLET_UNIT_DIRS=str(fixture.units)),
                        capture_output=True, text=True, timeout=30, check=True)
                    generated = fixture.fragment.read_text()
                    unit = Path(fixture.module.QUADLET_UNITS[service]).stem
                    runtime = next(line for line in generated.splitlines()
                                   if line.startswith("ExecStart=") and f" --name {unit} " in line)
                    self.assertIn(f"{name}:/run/hololive-youtube-po:{flags}", shlex.split(runtime))
                    fixture.check()
                    args, env, _ = fixture.consumed[-1]
                    self.assertIn(f"{mountpoint}:/run/hololive-youtube-po:{flags}", args)
                    level = next(value for value in shlex.split(runtime) if value.startswith("label=level:"))
                    self.assertIn(level, args)
                    self.assertEqual(env["YOUTUBE_COLLECTOR_INSTANCE_ID"], unit.removeprefix("hololive-"))
                    self.assertEqual("--userns" in args, service == "youtube-collector")
                    if service == "youtube-collector":
                        self.assertIn("auto:size=65536,gidmapping=1002:1002:1", args)
                        self.assertEqual(env["POSTGRES_PASSWORD"], fixture.host["HOLOLIVE_COLLECTOR_C_PASSWORD"])
                    self.assertEqual((mountpoint / "socket-fixture").read_bytes(), b"unchanged")
                    self.assertEqual([call[1:3] for call in fixture.calls if call[0] == "podman"],
                                     [["volume", "inspect"], ["run", "--rm"]])
                    fixture.assert_private_inputs()
                finally:
                    fixture.doCleanups()

    def test_masked_unit_uses_only_its_generated_security_policy(self):
        self.case("hololive-alarm-worker")
        name = self.fragment.name
        self.fragment = self.root / "etc/systemd/system" / name
        self.fragment.parent.mkdir(parents=True)
        self.fragment.symlink_to("/dev/null")
        generated = self.root / "run/systemd/generator" / name
        generated.parent.mkdir(parents=True)
        source = self.units / self.module.QUADLET_UNITS[self.pair["service"]]
        policy = "[X-Container]\nSecurityLabelLevel=s0:c900,c901\n"
        generated.write_text(f"[Unit]\nSourcePath={source}\n{policy}")
        self.check()
        self.assertIn("label=level:s0:c900,c901", self.consumed[-1][0])
        self.consumed.clear()
        generated.write_text(f"[Unit]\nSourcePath={self.units}/foreign.container\n{policy}")
        with self.assertRaisesRegex(ValueError, "unit source changed"):
            self.check()
        self.assertFalse(self.consumed)
        self.assertEqual(self.fragment.readlink(), Path("/dev/null"))

    def test_ordinary_named_volume_uses_existing_storage_readonly(self):
        self.case("youtube-collector-b")
        self.record.write_text(self.record.read_text().replace("hololive-youtube-po-b.volume:", "ordinary-volume:"))
        mountpoint = self.existing_volume("ordinary-volume")
        self.check()
        args, env, _ = self.consumed[-1]
        self.assertIn(f"{mountpoint}:/run/hololive-youtube-po:ro", args)
        self.assertEqual(env["POSTGRES_USER"], "hololive_collector_b")
        self.assertEqual((mountpoint / "socket-fixture").read_bytes(), b"unchanged")

    @unittest.skipUnless(QUADLET.exists(), "podman quadlet generator is not installed")
    def test_default_volume_name_matches_native_quadlet(self):
        self.case("youtube-collector-b")
        (self.units / "hololive-youtube-po-b.volume").write_text("[Volume]\n")
        name = "systemd-hololive-youtube-po-b"
        generated = self.real_run(
            [str(QUADLET), "-dryrun"], env=dict(os.environ, QUADLET_UNIT_DIRS=str(self.units)),
            capture_output=True, text=True, timeout=30, check=True).stdout
        runtime = next(line for line in generated.splitlines()
                       if line.startswith("ExecStart=") and " --name hololive-youtube-collector-b " in line)
        self.assertIn(f"{name}:/run/hololive-youtube-po:ro", shlex.split(runtime))
        mountpoint = self.existing_volume(name)
        self.check()
        self.assertIn(f"{mountpoint}:/run/hololive-youtube-po:ro", self.consumed[-1][0])

    def test_missing_volume_declaration_cannot_fall_back_to_a_named_volume(self):
        self.case("youtube-collector-b")
        (self.units / "hololive-youtube-po-b.volume").unlink()
        self.existing_volume("hololive-youtube-po-b.volume")
        with self.assertRaises(FileNotFoundError):
            self.check()
        self.assertEqual(self.calls, [])
        self.assertEqual(self.consumed, [])

    def test_missing_named_volume_rejects_before_container_creation(self):
        self.case("youtube-collector-b")
        with self.assertRaises(subprocess.CalledProcessError):
            self.check()
        self.assertEqual(self.consumed, [])
        self.assertEqual(self.calls, [["podman", "volume", "inspect", "hololive-youtube-po-b-socket"]])

    def test_missing_bind_rejects_before_container_creation(self):
        self.case("hololive-api")
        self.host["LIVE_LOGS_PATH"] = str(self.root / "absent")
        with self.assertRaisesRegex(ValueError, "bind source is unavailable"):
            self.check()
        self.assertEqual(self.consumed, [])

    def test_optional_settings_are_empty_and_dollars_are_not_recursively_expanded(self):
        self.case("hololive-api")
        self.record.write_text(self.record.read_text().replace(
            "[Container]\n", "[Container]\nEnvironment=LITERAL=$${NOT_EXPANDED}/$5/$NAME\n"
            "Environment=SUBSTITUTED=${PRESENT}/${ABSENT}\n"))
        self.host["PRESENT"] = "spaces and $dollars"
        self.host["NAME"] = "must-not-expand-inside-a-word"
        self.check()
        _, env, _ = self.consumed[-1]
        self.assertEqual(env["BOT_POSTGRES_POOL_MIN_CONNS"], "")
        self.assertEqual(env["ADMIN_API_POSTGRES_POOL_MIN_CONNS"], "")
        self.assertEqual(env["SUBSTITUTED"], "spaces and $dollars/")
        self.assertEqual(env["LITERAL"], "${NOT_EXPANDED}/$5/$NAME")
        self.assertEqual(env["APP_VERSION"], "13.4.0")
        self.assertEqual(env["POSTGRES_PASSWORD"], self.host["HOLOLIVE_DB_PASSWORD"])
        self.assert_private_inputs()

    def test_generated_env_is_fresh_when_live_output_is_absent_or_stale(self):
        self.case("hololive-alarm-worker")
        self.check()
        self.assertEqual(self.consumed[-1][1]["POSTGRES_PASSWORD"], self.host["HOLOLIVE_ALARM_WORKER_PASSWORD"])
        self.assertFalse((self.root / "run").exists())
        live = self.root / "run/quadlet-env/hololive-alarm-worker.env"
        live.parent.mkdir(parents=True)
        stale = b"POSTGRES_PASSWORD=stale\nHOLODEX_API_KEY=stale-key\n"
        live.write_bytes(stale)
        self.host["HOLOLIVE_ALARM_WORKER_PASSWORD"] = "rotated-synthetic-password"
        self.check()
        self.assertEqual(self.consumed[-1][1]["POSTGRES_PASSWORD"], "rotated-synthetic-password")
        self.assertEqual(live.read_bytes(), stale)
        self.assertNotEqual(self.rendered[0][0], self.rendered[1][0])
        self.assert_private_inputs()

    def test_retained_version_and_environment_precedence_match_runtime(self):
        self.case("hololive-api")
        self.pair["app_version"] = "12.3.0+retained"
        later = self.root / "later.env"
        later.write_text("HOLODEX_API_KEY=later-file-key\nPOSTGRES_PASSWORD=later-file-password\n")
        self.record.write_text(self.record.read_text().replace(
            "[Container]\n", "[Container]\nEnvironment=HOLODEX_API_KEY=explicit-key\n"
            "Environment=APP_VERSION=overridden-by-retained-dropin\n").replace(
            "EnvironmentFile=/run/hololive-bot/quadlet-env/hololive-api.env\n",
            "EnvironmentFile=/run/hololive-bot/quadlet-env/hololive-api.env\n"
            f"EnvironmentFile={later}\n"))
        self.check()
        _, env, files = self.consumed[-1]
        self.assertEqual(env["APP_VERSION"], "12.3.0+retained")
        self.assertIn("APP_VERSION=12.3.0+retained\n", self.rendered[-1][1])
        self.assertEqual(env["HOLODEX_API_KEY"], "explicit-key")
        self.assertEqual(env["POSTGRES_PASSWORD"], "later-file-password")
        self.assertEqual(files[-1], str(later))
        self.assert_private_inputs()

    def test_missing_required_secret_rejects_even_with_stale_generated_file(self):
        self.case("hololive-api")
        self.host.pop("HOLOLIVE_DB_PASSWORD")
        live = self.root / "run/quadlet-env/hololive-api.env"
        live.parent.mkdir(parents=True)
        stale = b"POSTGRES_PASSWORD=stale-but-nonempty\n"
        live.write_bytes(stale)
        with self.assertRaises(subprocess.CalledProcessError) as failure:
            self.check()
        self.assertIn("HOLOLIVE_DB_PASSWORD", failure.exception.stderr)
        self.assertEqual(self.consumed, [])
        self.assertEqual(live.read_bytes(), stale)

    def test_arbitrary_missing_environment_file_is_not_rendered_or_replaced(self):
        self.case("hololive-api")
        self.record.write_text(self.record.read_text().replace(
            "/run/hololive-bot/quadlet-env/hololive-api.env", str(self.root / "missing.env")))
        with self.assertRaises(FileNotFoundError):
            self.check()
        self.assertFalse(any(call[0] == "bash" for call in self.calls))
        self.assertEqual(self.consumed, [])

    def test_image_checker_failure_is_propagated_with_private_inputs_removed(self):
        self.case("hololive-api")
        self.check_status = 23
        with self.assertRaises(subprocess.CalledProcessError) as failure:
            self.check()
        self.assertEqual(failure.exception.returncode, 23)
        self.assertEqual(self.consumed[-1][1]["POSTGRES_PASSWORD"], self.host["HOLOLIVE_DB_PASSWORD"])
        self.assert_private_inputs()


if __name__ == "__main__":
    unittest.main()
