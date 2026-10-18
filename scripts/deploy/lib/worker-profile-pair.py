#!/usr/bin/env python3
"""Retain validated profile bytes with an image; never restore into the secret mirror."""
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
CLI = os.environ.get("CONTAINER_CLI", "docker")


def run(*args):
    return subprocess.run(args, check=True, text=True, stdout=subprocess.PIPE, timeout=30).stdout.strip()


def inspect(kind, name):
    return json.loads(run(CLI, kind, "inspect", name))[0]

def loaded_hash(metrics):
    samples = [line for line in metrics.splitlines() if line.startswith("iris_stack_worker_profile_info{")]
    if len(samples) != 1:
        raise ValueError("loaded profile identity unavailable")
    match = re.search(r'profile_hash="([0-9a-f]{64})"', samples[0])
    if match is None:
        raise ValueError("loaded profile hash unavailable")
    return match.group(1)


def container_profile_hash(service, container, data):
    if service in ("hololive-api", "hololive-alarm-worker"):
        credential = "API_SECRET_KEY"
    elif service in ("youtube-collector", "youtube-collector-b"):
        credential = "METRICS_API_KEY"
    else:
        raise ValueError("unsupported profile service")
    environment = dict(item.split("=", 1) for item in data["Config"]["Env"])
    port = environment["HOLOLIVE_METRICS_ADDR"].rsplit(":", 1)[1]
    if not port.isdigit():
        raise ValueError("invalid metrics port")
    return loaded_hash(run(CLI, "exec", container, "/app/bin/healthcheck",
                           "--body-api-key-env", credential,
                           "http://127.0.0.1:" + port + "/metrics"))


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


def prepare_native(service, release, host_env, unit):
    if service not in ("youtube-collector-a", "youtube-collector-d"):
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
    metrics = subprocess.run([str(release / "bin/healthcheck"), "--body-api-key-env", "METRICS_API_KEY",
                              "http://" + address + "/metrics"], env=environment, check=True,
                             text=True, stdout=subprocess.PIPE, timeout=10).stdout
    temporary = release / "worker-profile.preparing.json"
    raw = profile_bytes(source)
    with temporary.open("xb") as output:
        output.write(raw)
    temporary.chmod(0o444)
    if loaded_hash(metrics) != digest(temporary):
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
    if service not in ("youtube-collector-a", "youtube-collector-d"):
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
    metrics = subprocess.run([str(release / "bin/healthcheck"), "--body-api-key-env", "METRICS_API_KEY",
                              "http://" + environment["HOLOLIVE_METRICS_ADDR"] + "/metrics"], env=environment,
                             check=True, text=True, stdout=subprocess.PIPE, timeout=10).stdout
    if loaded_hash(metrics) != digest(release / "worker-profile.json"):
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
    return {"image": data["Id"], "revision": revision, "architecture": data["Architecture"], "os": data["Os"]}


def save_pair(directory, service, identity):
    identity.update(service=service, profile_hash=digest(directory / "profile.json"))
    (directory / "pair.json").write_text(json.dumps(identity) + "\n")
    (directory / "SHA256SUMS").write_text(
        f'{identity["profile_hash"]}  profile.json\n'
        f'{hashlib.sha256((directory / "pair.json").read_bytes()).hexdigest()}  pair.json\n'
    )
    for name in ("profile.json", "pair.json", "SHA256SUMS"):
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
        # Stage every file first; each rename is atomic, not the whole bundle.
        for temporary, path in staged:
            os.replace(temporary, path)
    finally:
        for temporary, _ in staged:
            temporary.unlink(missing_ok=True)


def overlay(directory, service, pair, config=None, output=None):
    settings = {
        "image": pair["image"],
        "pull_policy": "never",
        "labels": {LABEL: str(directory)},
        "environment": {
            "STACK_WORKER_PROFILE_FILE": RELEASE_TARGET + "/profile.json",
            "STACK_WORKER_PROFILE_SOURCE_FILE": TARGET + "/" + PROFILES[service],
        },
    }
    release_mount = {"type": "bind", "source": str(directory), "target": RELEASE_TARGET,
                     "read_only": True, "bind": {"create_host_path": False}}
    volumes = [release_mount]
    if config is not None:
        old = config["services"][service]
        old_path = old["environment"]["STACK_WORKER_PROFILE_FILE"]
        if service == "hololive-api":
            settings["environment"]["APP_VERSION"] = pair["app_version"]
        volumes = [v for v in old.get("volumes", [])
                   if v["target"] not in (TARGET, old_path, RELEASE_TARGET)
                   and not v["target"].startswith(TARGET + "/")]
        volumes.extend([
            {"type": "bind", "source": SOURCE, "target": TARGET, "read_only": True,
             "bind": {"create_host_path": False}},
            release_mount,
        ])
    lines = ["services:", "  " + json.dumps(service) + ":"]
    for key, value in settings.items():
        lines.append("    " + key + ": " + json.dumps(value))
    merge = "!override " if config is not None else ""
    lines.append("    volumes: " + merge + json.dumps(volumes))
    atomic_publish([(output or directory / "compose.yml", ("\n".join(lines) + "\n").encode("utf-8"))])


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
    else:
        # First adoption is permitted only while the old process can prove its loaded bytes.
        if not before["State"]["Running"]:
            raise ValueError("prepare the previous profile pair before entering maintenance-stopped mode")
        env = dict(item.split("=", 1) for item in before["Config"]["Env"])
        profile_path = env["STACK_WORKER_PROFILE_FILE"]
        if profile_path != TARGET + "/" + PROFILES[service]:
            raise ValueError("unexpected previous profile path")
        running_hash = container_profile_hash(service, container, before)
        run(CLI, "cp", container + ":" + profile_path, str(directory / "profile.json"))
        if running_hash != digest(directory / "profile.json"):
            raise ValueError("previous profile bytes differ from the running process; writer unchanged")
        after = inspect("container", container)
        if (before["Id"], before["State"]["StartedAt"], before["Image"]) != (
                after["Id"], after["State"]["StartedAt"], after["Image"]):
            raise ValueError("previous process changed during profile capture")
        pair = image_identity(before["Image"])
    (directory / "profile.json").chmod(0o444)
    validate_container_pair(container, before, directory)
    pair["retained_image"] = "hololive-worker-profile:pair-" + hashlib.sha256(str(directory).encode()).hexdigest()[:24]
    run(CLI, "tag", before["Image"], pair["retained_image"])
    pair["container_id"] = before["Id"]
    if service == "hololive-api":
        pair["app_version"] = dict(item.split("=", 1) for item in before["Config"]["Env"])["APP_VERSION"]
    save_pair(directory, service, pair)


def activate(directory, service, root, prepared_overlay=None):
    verify(directory, service)
    contents = (prepared_overlay or directory / "compose.yml").read_bytes()
    active = Path(root) / "artifacts/worker-profile/active"
    active.mkdir(mode=0o755, parents=True, exist_ok=True)
    files = [(directory / "compose.yml", contents)] if prepared_overlay is not None else []
    files.extend([
        (active / (service + ".yml"), contents),
        (active / (service + ".pair"), (str(directory) + "\n").encode("utf-8")),
    ])
    atomic_publish(files)


def main():
    action, service, raw_directory, *rest = sys.argv[1:]
    if action == "prepare-native":
        prepare_native(service, Path(raw_directory).resolve(), Path(rest[0]), rest[1])
        return
    if action == "verify-native":
        verify_native(service, Path(raw_directory).resolve(), rest[0])
        return
    if service not in PROFILES:
        raise ValueError("unsupported profile service")
    directory = Path(raw_directory).resolve()
    if action == "candidate":
        config = json.load(sys.stdin)
        svc = config["services"][service]
        expected = TARGET + "/" + PROFILES[service]
        if svc["environment"]["STACK_WORKER_PROFILE_FILE"] != expected:
            raise ValueError("candidate profile env does not match its service slot")
        mounts = [v for v in svc["volumes"] if v["target"] == TARGET]
        if len(mounts) != 1 or mounts[0]["source"] != SOURCE or not mounts[0].get("read_only"):
            raise ValueError("candidate profile directory does not match its service slot")
        directory.mkdir(mode=0o755, parents=True, exist_ok=False)
        source = Path(SOURCE) / PROFILES[service]
        (directory / "profile.json").write_bytes(profile_bytes(source))
        pair = save_pair(directory, service, image_identity(svc["image"]))
        overlay(directory, service, pair)
    elif action in ("capture", "prepare"):
        capture(rest[0], service, directory, rest[1])
        if action == "prepare":
            pair = verify(directory, service)
            overlay(directory, service, pair)
            activate(directory, service, rest[1])
    elif action == "rollback":
        overlay(directory, service, verify(directory, service), json.load(sys.stdin),
                Path(rest[0]) if rest else None)
    elif action == "verify-source":
        pair = verify(directory, service)
        if pair["profile_hash"] != digest(Path(SOURCE) / PROFILES[service]):
            raise ValueError("candidate source changed during profile validation; writer unchanged")
    elif action == "verify":
        verify(directory, service, rest[0] if rest else None)
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
