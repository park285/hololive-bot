#!/usr/bin/env python3
"""검증한 프로필 바이트를 이미지와 보관하며 비밀 미러에는 복원하지 않는다."""
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile

LABEL = "io.hololive.worker-profile-pair"
TARGET = "/run/hololive-bot/worker-profiles"
RELEASE_TARGET = "/run/hololive-bot/worker-profile-release"
SOURCE = "/etc/stack-secrets/hololive-bot/worker-profiles"
PROFILES = {
    "hololive-api": "hololive-api.json",
    "hololive-alarm-worker": "alarm-worker.json",
    "youtube-collector": "youtube-collector-c.json",
    "youtube-collector-b": "youtube-collector-b.json",
}
# 모든 pair 서비스는 rootful Podman Quadlet이다. 중앙(hololive-seoul)은 API·worker·collector c, iris-seoul은
# collector b다(2026-11-06 Docker Compose에서 전환). release 고정은 이 unit의 drop-in(DROPIN)이다.
CLI = "podman"
QUADLET_UNITS = {
    "hololive-api": "hololive-api.container",
    "hololive-alarm-worker": "hololive-alarm-worker.container",
    "youtube-collector": "hololive-youtube-collector-c.container",
    "youtube-collector-b": "hololive-youtube-collector-b.container",
}
# HOLOLIVE_QUADLET_TEST_ROOT는 quadlet-prestart와 같은 테스트 전용 root 접두어다.
QUADLET_DIR = Path(os.environ.get("HOLOLIVE_QUADLET_TEST_ROOT", "") + "/etc/containers/systemd/hololive-bot")
DROPIN = "50-worker-profile.conf"
# 중앙 pair는 그 image가 실제로 돌던 기본 unit 정의도 함께 보존한다. sync-opt-current.sh는 배포 helper보다 먼저
# 새 revision의 기본 unit을 설치하므로, 롤백이 image만 되돌리면 이전 image가 새 unit 정의(env·HealthCmd·mount)로 뜬다.
# Docker 시절 compose-rollback-service.sh가 compose 스냅숏으로 이전 구성 전체를 되살린 것과 같은 경계다.
UNIT_RECORD = "unit.container"


def run(*args):
    return subprocess.run(args, check=True, text=True, stdout=subprocess.PIPE, timeout=30).stdout.strip()


def inspect(kind, name):
    return json.loads(run(CLI, kind, "inspect", name))[0]

def loaded_hash(diagnostics):
    # 프로필 식별은 무관한 메트릭의 개수에 의존하지 않는다.
    envelope = json.loads(diagnostics)
    if not isinstance(envelope, dict) or envelope.get("contractVersion") != 1:
        raise ValueError("loaded profile identity unavailable")
    profile = envelope.get("profile")
    value = profile.get("hash") if isinstance(profile, dict) else None
    if not isinstance(value, str) or re.fullmatch(r"[0-9a-f]{64}", value) is None:
        raise ValueError("loaded profile hash unavailable")
    return value


def container_profile_hash(service, container, data):
    environment = dict(item.split("=", 1) for item in data["Config"]["Env"])
    port = environment["HOLOLIVE_METRICS_ADDR"].rsplit(":", 1)[1]
    if not port.isdigit():
        raise ValueError("invalid metrics port")
    credential = {
        "hololive-api": "API_SECRET_KEY",
        "hololive-alarm-worker": "API_SECRET_KEY",
        "youtube-collector": "METRICS_API_KEY",
        "youtube-collector-b": "METRICS_API_KEY",
    }[service]
    return loaded_hash(run(CLI, "exec", container, "/app/bin/healthcheck",
                           "--body-api-key-env", credential,
                           "http://127.0.0.1:" + port + "/diagnostics/workers"))


def validate_container_pair(container, before, directory):
    environment = dict(item.split("=", 1) for item in before["Config"]["Env"])
    environment["STACK_WORKER_PROFILE_FILE"] = RELEASE_TARGET + "/profile.json"
    environment.pop("STACK_WORKER_PROFILE_SOURCE_FILE", None)
    command = [CLI, "run", "--rm", "--pull=never", "--network", "none", "--volumes-from", container + ":ro",
               "--mount", "type=bind,src=" + str(directory) + ",dst=" + RELEASE_TARGET + ",readonly"]
    for key in environment:
        command.extend(("--env", key))
    entrypoint = before["Config"].get("Entrypoint") or []
    if len(entrypoint) != 1:
        raise ValueError("previous image requires one binary entrypoint")
    command.extend(("--entrypoint", entrypoint[0], before["Image"], "--check-worker-profile"))
    subprocess.run(command, env={**os.environ, **environment}, check=True, timeout=30)


def check_pair(directory, pair):
    # 옛 `compose run --rm --no-deps <service> --check-worker-profile`에 해당하는 후보 검사다.
    # --check-worker-profile은 STACK_WORKER_PROFILE_FILE만 읽으므로(workercontract.LoadProfileFromEnv) 운영 env·
    # network·다른 mount 없이 pair image와 release 디렉터리만 붙인다. drop-in이 실행 unit에 붙이는 것과 같은
    # bind 원본이라 SELinux label 판정도 같다. 실행 중인 container가 없어도(정지·첫 도입) 검사할 수 있다.
    subprocess.run([CLI, "run", "--rm", "--pull=never", "--network", "none", "--read-only",
                    "--cap-drop", "all", "--security-opt", "no-new-privileges",
                    "--mount", "type=bind,src=" + str(directory) + ",dst=" + RELEASE_TARGET + ",readonly",
                    "--env", "STACK_WORKER_PROFILE_FILE=" + RELEASE_TARGET + "/profile.json",
                    pair["image"], "--check-worker-profile"], check=True, timeout=60)


def prepare_native(service, release, host_env, unit):
    if service != "youtube-collector-a":
        raise ValueError("unsupported native slot")
    if release.parent != Path("/opt/hololive-bot/youtube-collector/releases"):
        raise ValueError("native release must be release-owned")
    pid = run("systemctl", "show", unit, "-p", "MainPID", "--value")
    if not pid.isdigit() or int(pid) == 0:
        raise ValueError("prepare native profile before stopping the collector")
    process = Path("/proc") / pid
    if not os.path.samefile(process / "exe", release / "bin/youtube-collector"):
        raise ValueError("native process does not execute this release")
    start = (process / "stat").read_text().split(")", 1)[1].split()[19]
    environment = dict(item.decode().split("=", 1) for item in (process / "environ").read_bytes().split(b"\0") if item)
    source = Path(SOURCE) / (service + ".json")
    if environment.get("STACK_WORKER_PROFILE_FILE") != str(source):
        raise ValueError("native process does not use the canonical slot")
    address = environment["HOLOLIVE_METRICS_ADDR"]
    diagnostics = subprocess.run([str(release / "bin/healthcheck"), "--body-api-key-env", "METRICS_API_KEY",
                                  "http://" + address + "/diagnostics/workers"], env=environment, check=True,
                                 text=True, stdout=subprocess.PIPE, timeout=10).stdout
    temporary = release / "worker-profile.preparing.json"
    raw = profile_bytes(source)
    with temporary.open("xb") as output:
        output.write(raw)
    temporary.chmod(0o444)
    if loaded_hash(diagnostics) != digest(temporary):
        raise ValueError("native loaded profile does not match available bytes")
    subprocess.run(["sudo", "-n", "-u", "hololive", "env", "STACK_WORKER_PROFILE_FILE=" + str(temporary),
                    str(release / "bin/youtube-collector"), "--check-worker-profile"], check=True, timeout=30)
    if run("systemctl", "show", unit, "-p", "MainPID", "--value") != pid or (process / "stat").read_text().split(")", 1)[1].split()[19] != start:
        raise ValueError("native process changed during preparation")
    final = release / "worker-profile.json"
    if final.exists() or (release / "SHA256SUMS").exists():
        raise ValueError("native release already has profile artifacts")
    temporary.rename(final)
    sums = release / "SHA256SUMS"
    binary_hash = hashlib.sha256((release / "bin/youtube-collector").read_bytes()).hexdigest()
    sums.write_text(f'{binary_hash}  bin/youtube-collector\n{digest(final)}  worker-profile.json\n')
    sums.chmod(0o444)
    lines = host_env.read_text().splitlines()
    if sum(line.startswith("STACK_WORKER_PROFILE_FILE=") for line in lines) != 1:
        raise ValueError("native host env must have one profile path")
    lines = [line for line in lines if not line.startswith("STACK_WORKER_PROFILE_SOURCE_FILE=")]
    lines = ["STACK_WORKER_PROFILE_FILE=" + str(final) if line.startswith("STACK_WORKER_PROFILE_FILE=") else line for line in lines]
    lines.append("STACK_WORKER_PROFILE_SOURCE_FILE=" + str(source))
    staged_env = host_env.with_suffix(".profile-prepared")
    with staged_env.open("x") as output:
        output.write("\n".join(lines) + "\n")
    staged_env.chmod(0o640)
    staged_env.replace(host_env)


def verify_native(service, release, unit):
    if service != "youtube-collector-a":
        raise ValueError("unsupported native slot")
    if release.parent != Path("/opt/hololive-bot/youtube-collector/releases"):
        raise ValueError("native release must be release-owned")
    subprocess.run(["sha256sum", "--check", "--strict", "SHA256SUMS"], cwd=release,
                   check=True, stdout=subprocess.DEVNULL, timeout=10)
    pid = run("systemctl", "show", unit, "-p", "MainPID", "--value")
    if not pid.isdigit() or int(pid) == 0:
        raise ValueError("native collector is not running")
    process = Path("/proc") / pid
    if not os.path.samefile(process / "exe", release / "bin/youtube-collector"):
        raise ValueError("native process does not execute this release")
    start = (process / "stat").read_text().split(")", 1)[1].split()[19]
    environment = dict(item.decode().split("=", 1) for item in (process / "environ").read_bytes().split(b"\0") if item)
    if environment.get("STACK_WORKER_PROFILE_FILE") != str(release / "worker-profile.json"):
        raise ValueError("native process does not use its release profile")
    if environment.get("STACK_WORKER_PROFILE_SOURCE_FILE") != str(Path(SOURCE) / (service + ".json")):
        raise ValueError("native process does not observe its canonical source")
    diagnostics = subprocess.run([str(release / "bin/healthcheck"), "--body-api-key-env", "METRICS_API_KEY",
                                  "http://" + environment["HOLOLIVE_METRICS_ADDR"] + "/diagnostics/workers"], env=environment,
                                 check=True, text=True, stdout=subprocess.PIPE, timeout=10).stdout
    if loaded_hash(diagnostics) != digest(release / "worker-profile.json"):
        raise ValueError("native loaded profile differs from its release pair")
    if run("systemctl", "show", unit, "-p", "MainPID", "--value") != pid or (process / "stat").read_text().split(")", 1)[1].split()[19] != start:
        raise ValueError("native process changed during verification")


def profile_bytes(path):
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, "rb") as source:
        if not stat.S_ISREG(os.fstat(source.fileno()).st_mode):
            raise ValueError("profile artifact must be a regular file")
        raw = source.read(64 * 1024 + 1)
    if not 0 < len(raw) <= 64 * 1024:
        raise ValueError("profile artifact size is invalid")
    return raw


def digest(path):
    return hashlib.sha256(profile_bytes(path)).hexdigest()


def image_identity(image):
    data = inspect("image", image)
    revision = data["Config"]["Labels"].get("org.opencontainers.image.revision", "")
    if not re.fullmatch(r"[0-9a-f]{40}", revision):
        raise ValueError("profile pair requires a full image revision")
    # Podman .Id는 접두어 없는 hex다. identity는 sha256:<hex> 하나로 기록한다.
    image_id = "sha256:" + data["Id"]
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", image_id):
        raise ValueError("profile pair requires a sha256 image identity")
    return {"image": image_id, "revision": revision, "architecture": data["Architecture"], "os": data["Os"]}


def save_pair(directory, service, identity):
    identity.update(service=service, profile_hash=digest(directory / "profile.json"))
    (directory / "pair.json").write_text(json.dumps(identity) + "\n")
    names = ["profile.json", "pair.json"] + ([UNIT_RECORD] if (directory / UNIT_RECORD).is_file() else [])
    (directory / "SHA256SUMS").write_text(
        "".join(f"{hashlib.sha256((directory / name).read_bytes()).hexdigest()}  {name}\n" for name in names))
    for name in names + ["SHA256SUMS"]:
        (directory / name).chmod(0o444)
    return identity


def verify(directory, service, image=None):
    subprocess.run(["sha256sum", "--check", "--strict", "SHA256SUMS"], cwd=directory,
                   check=True, stdout=subprocess.DEVNULL, timeout=10)
    pair = json.loads((directory / "pair.json").read_text())
    if pair["service"] != service or pair["profile_hash"] != digest(directory / "profile.json"):
        raise ValueError("profile pair identity mismatch")
    actual = image_identity(image or pair["image"])
    if any(pair[key] != value for key, value in actual.items()):
        raise ValueError("profile pair image identity mismatch")
    return pair


def atomic_publish(files):
    staged = []
    try:
        for path, contents in files:
            fd, name = tempfile.mkstemp(prefix="." + path.name + "-", dir=path.parent)
            temporary = Path(name)
            staged.append((temporary, path))
            with os.fdopen(fd, "wb") as output:
                output.write(contents)
            temporary.chmod(0o444)
        # 모두 준비한 뒤 교체한다. 이름 변경 각각만 원자적이며 묶음 전체는 아니다.
        for temporary, path in staged:
            os.replace(temporary, path)
    finally:
        for temporary, _ in staged:
            temporary.unlink(missing_ok=True)


def dropin_contents(directory, service, pair, app_version=None):
    # Quadlet drop-in의 Image=와 같은 키의 Environment=는 기본 unit 값을 교체하고 Volume=은 더해진다.
    # 기본 unit의 원본 mirror mount는 남겨 desired profile 관측을 유지한다. 값은 unit 문법에 그대로
    # 들어가므로 systemd specifier(%)·공백·mount 구분자(:)가 없는 경로만 받는다.
    if re.fullmatch(r"/[A-Za-z0-9._/@+-]+", str(directory)) is None:
        raise ValueError("profile pair path is not safe for a Quadlet drop-in")
    lines = [
        "# worker-profile-pair.py가 생성한 release 고정 drop-in이다. 직접 고치지 않는다.",
        "[Container]",
        "Image=" + pair["image"],
        "Label=" + LABEL + "=" + str(directory),
        "Environment=STACK_WORKER_PROFILE_FILE=" + RELEASE_TARGET + "/profile.json",
        "Environment=STACK_WORKER_PROFILE_SOURCE_FILE=" + TARGET + "/" + PROFILES[service],
        "Volume=" + str(directory) + ":" + RELEASE_TARGET + ":ro",
    ]
    if app_version is not None:
        # 이전 image로 되돌릴 때 prestart가 배포 트리 VERSION에서 넣는 값 대신 그 image의 값을 보고한다.
        # Podman은 --env를 --env-file보다 우선한다.
        if re.fullmatch(r"[0-9A-Za-z.+-]+", app_version) is None:
            raise ValueError("retained APP_VERSION is not safe for a Quadlet drop-in")
        lines.append("Environment=APP_VERSION=" + app_version)
    return ("\n".join(lines) + "\n").encode("utf-8")


def dropin(directory, service, pair, output=None, app_version=None):
    atomic_publish([(output or directory / DROPIN, dropin_contents(directory, service, pair, app_version))])


def quadlet_slot(service, unit):
    # 후보 profile은 기본 unit이 원본 mirror를 읽기 전용으로 붙이고 서비스 slot 파일을 가리킬 때만 고정한다.
    lines = [line.strip() for line in unit.splitlines()]
    if lines.count("Environment=STACK_WORKER_PROFILE_FILE=" + TARGET + "/" + PROFILES[service]) != 1:
        raise ValueError("candidate profile env does not match its service slot")
    mounts = [line for line in lines if line.startswith("Volume=") and line.split(":")[1:2] == [TARGET]]
    if mounts != ["Volume=" + SOURCE + ":" + TARGET + ":ro"]:
        raise ValueError("candidate profile directory does not match its service slot")


def capture(container, service, directory, root):
    before = inspect("container", container)
    previous = (before["Config"].get("Labels") or {}).get(LABEL)
    prepared = Path(root) / "artifacts/worker-profile/active" / (service + ".pair")
    if not previous and prepared.is_file() and not prepared.is_symlink():
        prepared_pair = Path(prepared.read_text().strip())
        receipt = verify(prepared_pair, service, before["Image"])
        if receipt.get("container_id") != before["Id"]:
            raise ValueError("prepared profile belongs to a different container")
        previous = str(prepared_pair)
    directory.mkdir(mode=0o755, parents=True, exist_ok=False)
    if previous:
        prior = Path(previous)
        pair = verify(prior, service, before["Image"])
        if before["State"]["Running"] and container_profile_hash(service, container, before) != pair["profile_hash"]:
            raise ValueError("running process differs from its retained profile pair")
        (directory / "profile.json").write_bytes(profile_bytes(prior / "profile.json"))
        if (prior / UNIT_RECORD).is_file():
            (directory / UNIT_RECORD).write_bytes((prior / UNIT_RECORD).read_bytes())
    else:
        # 첫 도입은 기존 프로세스가 로드한 바이트를 증명할 수 있을 때만 허용한다.
        if not before["State"]["Running"]:
            raise ValueError("prepare the previous profile pair before entering maintenance-stopped mode")
        env = dict(item.split("=", 1) for item in before["Config"]["Env"])
        profile_path = env["STACK_WORKER_PROFILE_FILE"]
        if profile_path != TARGET + "/" + PROFILES[service]:
            raise ValueError("unexpected previous profile path")
        running_hash = container_profile_hash(service, container, before)
        # UserNS=auto container에서는 idmap 없는 bind의 root 소유 파일을 podman cp가 꺼내지 못한다
        # (host ID 0 cannot be mapped). profile 디렉터리는 bind라 그 host 원본이 container가 읽는 파일과 같다.
        sources = [mount.get("Source") for mount in before.get("Mounts") or [] if mount.get("Destination") == TARGET]
        if sources != [SOURCE]:
            raise ValueError("unexpected previous profile mount")
        (directory / "profile.json").write_bytes(profile_bytes(Path(SOURCE) / PROFILES[service]))
        if running_hash != digest(directory / "profile.json"):
            raise ValueError("previous profile bytes differ from the running process; writer unchanged")
        after = inspect("container", container)
        if (before["Id"], before["State"]["StartedAt"], before["Image"]) != (
                after["Id"], after["State"]["StartedAt"], after["Image"]):
            raise ValueError("previous process changed during profile capture")
        pair = image_identity(before["Image"])
        # 첫 도입은 설치된 기본 unit을 기록한다. 컷오버 직후처럼 실행 중 container가 그 unit으로 기동했을 때만 정확하므로
        # 첫 도입(prepare)은 기본 unit을 설치하고 기동한 직후에 한다.
        installed = QUADLET_DIR / QUADLET_UNITS[service]
        if installed.is_file():
            (directory / UNIT_RECORD).write_bytes(installed.read_bytes())
    (directory / "profile.json").chmod(0o444)
    validate_container_pair(container, before, directory)
    pair["retained_image"] = "hololive-worker-profile:pair-" + hashlib.sha256(str(directory).encode()).hexdigest()[:24]
    run(CLI, "tag", before["Image"], pair["retained_image"])
    pair["container_id"] = before["Id"]
    if service == "hololive-api":
        pair["app_version"] = dict(item.split("=", 1) for item in before["Config"]["Env"])["APP_VERSION"]
    save_pair(directory, service, pair)


def activate(directory, service, root, prepared_overlay=None):
    # 활성 overlay는 재생성·재부팅마다 이 디렉터리를 bind하므로, 수용 후 정리되는 백업이 아니라
    # 배포 동기화와 보존 정리에서 빠지는 release 저장소에만 둔다.
    releases = (Path(root) / "artifacts/worker-profile/releases").resolve()
    if not directory.resolve().is_relative_to(releases):
        raise ValueError("active profile pair must live under artifacts/worker-profile/releases")
    verify(directory, service)
    contents = (prepared_overlay or directory / DROPIN).read_bytes()
    active = Path(root) / "artifacts/worker-profile/active"
    active.mkdir(mode=0o755, parents=True, exist_ok=True)
    files = [(directory / DROPIN, contents)] if prepared_overlay is not None else []
    # unit은 이 drop-in을 읽는다. daemon-reload와 재시작은 승인된 배포 절차가 한다.
    unit_dropins = QUADLET_DIR / (QUADLET_UNITS[service] + ".d")
    unit_dropins.mkdir(mode=0o755, parents=True, exist_ok=True)
    files.append((unit_dropins / DROPIN, contents))
    files.append((active / (service + ".pair"), (str(directory) + "\n").encode("utf-8")))
    atomic_publish(files)


def verify_retained(directory, service, candidate, root):
    """정지된 중앙 서비스가 보존한 이전 pair나 이번 후보에만 결합돼 있는지 확인한다."""
    if service not in ("hololive-api", "hololive-alarm-worker"):
        raise ValueError("coordinated resume supports central Iris consumers only")
    previous = verify(directory, service)
    prepared = verify(candidate, service)
    releases = (Path(root) / "artifacts/worker-profile/releases").resolve()
    pointer = Path(root) / "artifacts/worker-profile/active" / (service + ".pair")
    active = Path(profile_bytes(pointer).decode("utf-8").strip()).resolve()
    if not active.is_relative_to(releases):
        raise ValueError("retained active pair escaped the release store")
    current = verify(active, service)
    identity = ("image", "revision", "architecture", "os", "profile_hash")
    if not any(all(current[key] == expected[key] for key in identity) for expected in (previous, prepared)):
        raise ValueError("installed pair changed after coordinated preparation")
    candidate_dropin = dropin_contents(candidate, service, prepared)
    if profile_bytes(candidate / DROPIN) != candidate_dropin:
        raise ValueError("prepared candidate drop-in changed")
    active_dropin = profile_bytes(active / DROPIN)
    # 일반 활성화와 명시적 rollback이 만드는 두 형태만 인정한다.
    expected_active = [dropin_contents(active, service, current)]
    if service == "hololive-api" and "app_version" in current:
        expected_active.append(dropin_contents(active, service, current, current["app_version"]))
    if active_dropin not in expected_active:
        raise ValueError("retained active drop-in changed")
    installed = QUADLET_DIR / (QUADLET_UNITS[service] + ".d") / DROPIN
    # drop-in 게시 뒤 active 포인터 갱신 전에 중단된 경우도 두 unit을 정지한 채 재개할 수 있다.
    if profile_bytes(installed) not in (active_dropin, candidate_dropin):
        raise ValueError("installed drop-in no longer belongs to the retained deployment")
    if profile_bytes(candidate / UNIT_RECORD) != profile_bytes(QUADLET_DIR / QUADLET_UNITS[service]):
        raise ValueError("installed base unit changed after coordinated preparation")


def main():
    action, service, raw_directory, *rest = sys.argv[1:]
    if action == "prepare-native":
        prepare_native(service, Path(raw_directory).resolve(), Path(rest[0]), rest[1])
        return
    if action == "verify-native":
        verify_native(service, Path(raw_directory).resolve(), rest[0])
        return
    if service not in QUADLET_UNITS:
        raise ValueError("unsupported profile service")
    directory = Path(raw_directory).resolve()
    if action == "candidate":
        # 기본 unit 텍스트를 stdin으로, 배포할 image identity를 인자로 받는다.
        unit_text = sys.stdin.read()
        quadlet_slot(service, unit_text)
        image = rest[0]
        directory.mkdir(mode=0o755, parents=True, exist_ok=False)
        # 이 candidate가 활성화되면 이 기본 unit으로 돈다. 다음 release의 capture가 롤백용으로 이어받는다.
        (directory / UNIT_RECORD).write_text(unit_text)
        source = Path(SOURCE) / PROFILES[service]
        (directory / "profile.json").write_bytes(profile_bytes(source))
        pair = save_pair(directory, service, image_identity(image))
        dropin(directory, service, pair)
    elif action in ("capture", "prepare"):
        capture(rest[0], service, directory, rest[1])
        if action == "prepare":
            pair = verify(directory, service)
            dropin(directory, service, pair)
            activate(directory, service, rest[1])
    elif action == "rollback":
        pair = verify(directory, service)
        output = Path(rest[0]) if rest else None
        dropin(directory, service, pair, output,
               pair["app_version"] if service == "hololive-api" else None)
    elif action == "verify-source":
        pair = verify(directory, service)
        if pair["profile_hash"] != digest(Path(SOURCE) / PROFILES[service]):
            raise ValueError("candidate source changed during profile validation; writer unchanged")
    elif action == "verify-retained":
        verify_retained(directory, service, Path(rest[0]).resolve(), rest[1])
    elif action == "verify":
        verify(directory, service, rest[0] if rest else None)
    elif action == "check":
        check_pair(directory, verify(directory, service))
    elif action == "verify-live":
        before = inspect("container", rest[0])
        pair = verify(directory, service, before["Image"])
        if container_profile_hash(service, rest[0], before) != pair["profile_hash"]:
            raise ValueError("running profile differs from its validated release pair")
    elif action == "activate":
        activate(directory, service, rest[0], Path(rest[1]) if len(rest) > 1 else None)
    else:
        raise ValueError("unknown profile pair operation")


if __name__ == "__main__":
    try:
        main()
    except ValueError as error:
        sys.exit("worker profile pairing: " + str(error))
    except (KeyError, OSError, subprocess.SubprocessError):
        sys.exit("worker profile pairing failed; retain release artifacts for recovery")
