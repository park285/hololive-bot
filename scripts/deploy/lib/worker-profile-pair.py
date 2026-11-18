#!/usr/bin/env python3
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
CLI = "podman"
QUADLET_UNITS = {
    "hololive-api": "hololive-api.container",
    "hololive-alarm-worker": "hololive-alarm-worker.container",
    "youtube-collector": "hololive-youtube-collector-c.container",
    "youtube-collector-b": "hololive-youtube-collector-b.container",
}
QUADLET_DIR = Path(os.environ.get("HOLOLIVE_QUADLET_TEST_ROOT", "") + "/etc/containers/systemd/hololive-bot")
DROPIN = "50-worker-profile.conf"
UNIT_RECORD = "unit.container"


def run(*args):
    return subprocess.run(args, check=True, text=True, stdout=subprocess.PIPE, timeout=30).stdout.strip()


def inspect(kind, name):
    return json.loads(run(CLI, kind, "inspect", name))[0]

def loaded_hash(diagnostics):
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
    command = [CLI, "run", "--rm", "--pull=never", "--network", "none", "--read-only",
               "--cap-drop", "all", "--security-opt", "no-new-privileges",
               "--volumes-from", container + ":ro",
               "--mount", "type=bind,src=" + str(directory) + ",dst=" + RELEASE_TARGET + ",readonly",
               "--user", before["Config"]["User"]]
    for group in before["HostConfig"]["GroupAdd"] or []:
        command.extend(("--group-add", group))
    for key in environment:
        command.extend(("--env", key))
    entrypoint = before["Config"].get("Entrypoint") or []
    if len(entrypoint) != 1:
        raise ValueError("previous image requires one binary entrypoint")
    command.extend(("--entrypoint", entrypoint[0], before["Image"], "--check-worker-profile"))
    subprocess.run(command, env={**os.environ, **environment}, check=True, timeout=30)


def check_pair(directory, pair):
    values = unit_values(directory / UNIT_RECORD)
    command = ["systemd-run", "--quiet", "--wait", "--pipe", "--collect",
               "--property=RuntimeMaxSec=60s"]
    for key, value in values["Service"]:
        if key == "EnvironmentFile":
            command.append("--property=EnvironmentFile=" + value)
        elif key == "Environment":
            command.append("--setenv=" + value)
    command.extend((sys.executable, str(Path(__file__).resolve()), "check-resolved",
                    pair["service"], str(directory)))
    subprocess.run(command, check=True, timeout=65)


def unit_values(path, sections=("Container", "Service")):
    values = {section: [] for section in sections}
    section = ""
    seen = set()
    for raw in path.read_text().splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("[") and line.endswith("]"):
            section = line[1:-1]
            if section in values:
                seen.add(section)
            continue
        if section in values:
            if "=" not in line or line.endswith("\\"):
                raise ValueError("profile preflight requires explicit single-line unit settings")
            values[section].append(line.split("=", 1))
    if seen != set(sections) or (sections == ("Container", "Service") and not all(values.values())):
        raise ValueError("profile preflight requires the retained runtime unit")
    return values


def expand_unit_value(value):
    return re.sub(r"\$\$|\$\{([A-Za-z_][A-Za-z0-9_]*)\}",
                  lambda match: "$" if match.group(0) == "$$" else os.environ.get(match.group(1), ""),
                  value)


def volume_source(source):
    if source.startswith("/"):
        path = Path(source)
    else:
        if re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]*", source) is None:
            raise ValueError("profile preflight volume source is invalid")
        if source.endswith(".volume"):
            values = unit_values(QUADLET_DIR / source, ("Volume",))["Volume"]
            name = next((value for key, value in reversed(values) if key == "VolumeName"), "")
            source = expand_unit_value(name or "systemd-" + source.removesuffix(".volume"))
        volume = inspect("volume", source)
        if volume["Name"] != source or not volume.get("Mountpoint"):
            raise ValueError("profile preflight native volume is unavailable")
        path = Path(volume["Mountpoint"])
    if not path.is_absolute() or not path.exists():
        raise ValueError("profile preflight bind source is unavailable")
    return str(path)


def check_resolved_pair(directory, pair):
    values = unit_values(directory / UNIT_RECORD)["Container"]
    environment = dict(os.environ)
    command = [CLI, "run", "--rm", "--pull=never", "--network", "none", "--read-only",
               "--cap-drop", "all", "--security-opt", "no-new-privileges"]
    unit = Path(QUADLET_UNITS[pair["service"]]).stem
    generated_env = "/run/hololive-bot/quadlet-env/" + unit + ".env"
    user, group = "", ""
    with tempfile.TemporaryDirectory(prefix="hololive-profile-env-") as output:
        rendered = None
        for key, raw in values:
            value = expand_unit_value(raw)
            if key == "EnvironmentFile":
                if value == generated_env:
                    if rendered is None:
                        renderer = ["bash", str(Path(__file__).resolve().parents[1] / "quadlet-prestart.sh"),
                                    "--render-env", unit, output]
                        if pair["service"] == "hololive-api" and "app_version" in pair:
                            renderer.extend(("--app-version", pair["app_version"]))
                        subprocess.run(renderer, check=True, timeout=30)
                        rendered = str(Path(output) / (unit + ".env"))
                    value = rendered
                command.extend(("--env-file", value))
            elif key == "Environment":
                name, content = value.split("=", 1)
                environment[name] = content
                command.extend(("--env", name))
            elif key == "Volume":
                source, target, *options = value.split(":")
                source = volume_source(source)
                flags = [flag for option in options for flag in option.split(",") if flag not in ("rw", "ro")]
                command.extend(("--volume", ":".join((source, target, ",".join(["ro", *flags])))))
            elif key == "User":
                user = value
            elif key == "Group":
                group = value
            elif key == "GroupAdd":
                command.extend(("--group-add", value))
            elif key == "UserNS":
                command.extend(("--userns", value))
        if not user or not group:
            raise ValueError("profile preflight requires the runtime user and group")
        if pair["service"] == "hololive-api" and "app_version" in pair:
            environment["APP_VERSION"] = pair["app_version"]
            command.extend(("--env", "APP_VERSION"))
        fragment = run("systemctl", "show", unit + ".service", "--property=FragmentPath", "--value")
        native = unit_values(Path(fragment), sections=("X-Container",))["X-Container"]
        level = dict(native).get("SecurityLabelLevel")
        if level:
            command.extend(("--security-opt", "label=level:" + expand_unit_value(level)))
        command.extend(("--user", user + ":" + group,
                        "--mount", "type=bind,src=" + str(directory) + ",dst=" + RELEASE_TARGET + ",readonly",
                        "--env", "STACK_WORKER_PROFILE_FILE=" + RELEASE_TARGET + "/profile.json",
                        "--unsetenv", "STACK_WORKER_PROFILE_SOURCE_FILE",
                        pair["image"], "--check-worker-profile"))
        subprocess.run(command, env=environment, check=True, timeout=60)


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
    subprocess.run(["systemd-run", "--quiet", "--wait", "--pipe", "--collect",
                    "--property=User=hololive", "--property=Group=opc", "--property=PrivateNetwork=yes",
                    "--property=NoNewPrivileges=yes", "--property=RuntimeMaxSec=30s",
                    "--property=WorkingDirectory=" + str(release), "--property=ProtectHome=yes",
                    "--property=ProtectSystem=strict", "--property=PrivateTmp=yes",
                    "--property=EnvironmentFile=/etc/stack-secrets/hololive-bot/youtube-collector.env",
                    "--property=EnvironmentFile=" + str(host_env),
                    "/usr/bin/env", "STACK_WORKER_PROFILE_FILE=" + str(temporary),
                    str(release / "bin/youtube-collector-wrapper"),
                    str(release / "bin/youtube-collector"), "--check-worker-profile"], check=True, timeout=35)
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
        for temporary, path in staged:
            os.replace(temporary, path)
    finally:
        for temporary, _ in staged:
            temporary.unlink(missing_ok=True)


def dropin_contents(directory, service, pair, app_version=None):
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
        if re.fullmatch(r"[0-9A-Za-z.+-]+", app_version) is None:
            raise ValueError("retained APP_VERSION is not safe for a Quadlet drop-in")
        lines.append("Environment=APP_VERSION=" + app_version)
    return ("\n".join(lines) + "\n").encode("utf-8")


def dropin(directory, service, pair, output=None, app_version=None):
    atomic_publish([(output or directory / DROPIN, dropin_contents(directory, service, pair, app_version))])


def quadlet_slot(service, unit):
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
        if not before["State"]["Running"]:
            raise ValueError("prepare the previous profile pair before entering maintenance-stopped mode")
        env = dict(item.split("=", 1) for item in before["Config"]["Env"])
        profile_path = env["STACK_WORKER_PROFILE_FILE"]
        if profile_path != TARGET + "/" + PROFILES[service]:
            raise ValueError("unexpected previous profile path")
        running_hash = container_profile_hash(service, container, before)
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
    releases = (Path(root) / "artifacts/worker-profile/releases").resolve()
    if not directory.resolve().is_relative_to(releases):
        raise ValueError("active profile pair must live under artifacts/worker-profile/releases")
    verify(directory, service)
    contents = (prepared_overlay or directory / DROPIN).read_bytes()
    active = Path(root) / "artifacts/worker-profile/active"
    active.mkdir(mode=0o755, parents=True, exist_ok=True)
    files = [(directory / DROPIN, contents)] if prepared_overlay is not None else []
    unit_dropins = QUADLET_DIR / (QUADLET_UNITS[service] + ".d")
    unit_dropins.mkdir(mode=0o755, parents=True, exist_ok=True)
    files.append((unit_dropins / DROPIN, contents))
    files.append((active / (service + ".pair"), (str(directory) + "\n").encode("utf-8")))
    atomic_publish(files)


def verify_retained(directory, service, candidate, root):
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
    expected_active = [dropin_contents(active, service, current)]
    if service == "hololive-api" and "app_version" in current:
        expected_active.append(dropin_contents(active, service, current, current["app_version"]))
    if active_dropin not in expected_active:
        raise ValueError("retained active drop-in changed")
    installed = QUADLET_DIR / (QUADLET_UNITS[service] + ".d") / DROPIN
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
        unit_text = sys.stdin.read()
        quadlet_slot(service, unit_text)
        image = rest[0]
        directory.mkdir(mode=0o755, parents=True, exist_ok=False)
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
    elif action == "check-resolved":
        check_resolved_pair(directory, verify(directory, service))
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
