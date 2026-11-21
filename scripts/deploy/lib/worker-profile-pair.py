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
    "collector-c": "collector-c.json",
    "collector-b": "collector-b.json",
}
CLI = "podman"
QUADLET_UNITS = {
    "hololive-api": "hololive-api.container",
    "hololive-alarm-worker": "hololive-alarm-worker.container",
    "collector-c": "hololive-collector-c.container",
    "collector-b": "hololive-collector-b.container",
}
QUADLET_DIR = Path(os.environ.get("HOLOLIVE_QUADLET_TEST_ROOT", "") + "/etc/containers/systemd/hololive-bot")
DROPIN = "50-worker-profile.conf"
COMPOSE_ROOT = (Path(os.environ["HOLOLIVE_QUADLET_TEST_ROOT"]) if os.geteuid() and
                os.environ.get("HOLOLIVE_QUADLET_TEST_ROOT") else Path("/")) / "opt/hololive-bot/compose"
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


def container_profile_hash(service, data):
    environment = dict(item.split("=", 1) for item in data["Config"]["Env"])
    port = environment["HOLOLIVE_METRICS_ADDR"].rsplit(":", 1)[1]
    if not port.isdigit():
        raise ValueError("invalid metrics port")
    credential = {
        "hololive-api": "API_SECRET_KEY",
        "hololive-alarm-worker": "API_SECRET_KEY",
        "collector-c": "METRICS_API_KEY",
        "collector-b": "METRICS_API_KEY",
    }[service]
    return loaded_hash(run(CLI, "exec", data["Id"], "/app/bin/healthcheck",
                           "--body-api-key-env", credential,
                           "http://127.0.0.1:" + port + "/diagnostics/workers"))


def running_generation(data):
    state = data["State"]
    if not state["Running"] or state.get("Paused") or state.get("Restarting") or state["Pid"] <= 0:
        raise ValueError("profile capture requires the running writer; use its retained release after stopping")
    return data["Id"], data["Image"], state["StartedAt"], state["Pid"]


def oci_spec(container):
    with Path(container["OCIConfigPath"]).open() as source:
        return json.load(source)


def bind_argument(source, destination, options):
    if not Path(source).is_absolute() or not Path(destination).is_absolute() or any(
            char in value for value in (source, destination) for char in (":", ",", "\n", "\r")):
        raise ValueError("unsupported profile check bind path")
    return source + ":" + destination + ":" + ",".join(["ro", "bind", *options])


def check_mounts(pid, destinations):
    table = {}
    for line in Path(f"/proc/{pid}/mountinfo").read_text().splitlines():
        _, _, _, _, path, flags, *_ = line.split()
        if path in destinations:
            if path in table:
                raise ValueError("ambiguous profile check mount")
            table[path] = set(flags.split(","))
        elif any(path.startswith(parent + "/") for parent in destinations):
            raise ValueError("unrecorded nested profile check mount")
    if table.keys() != destinations:
        raise ValueError("missing profile check mount")
    return table


def writer_profile(before, service, release):
    if (before["Config"].get("Labels") or {}).get(LABEL) != release:
        raise ValueError("writer does not use the expected profile release")
    spec = oci_spec(before)
    env = spec["process"]["env"]
    slot = TARGET + "/" + PROFILES[service]
    expected_env = {"STACK_WORKER_PROFILE_FILE": slot}
    if release is not None:
        expected_env.update(STACK_WORKER_PROFILE_FILE=RELEASE_TARGET + "/profile.json",
                            STACK_WORKER_PROFILE_SOURCE_FILE=slot)
    for key, expected in expected_env.items():
        if [item for item in env if item.startswith(key + "=")] != [key + "=" + expected]:
            raise ValueError("writer does not use its expected profile environment")
    pid = before["State"]["Pid"]
    root = Path(f"/proc/{pid}/root")
    sources = {TARGET: SOURCE}
    if release is not None:
        sources[RELEASE_TARGET] = release
    flags = check_mounts(pid, sources.keys())
    for dest, source in sources.items():
        mounts = [m for m in spec["mounts"] if m["destination"] == dest]
        if (len(mounts) != 1 or mounts[0]["type"] != "bind" or mounts[0]["source"] != source or
                "ro" not in flags[dest] or not (root / dest.lstrip("/")).samefile(source)):
            raise ValueError("writer profile differs from its expected read-only bind")
    return root / (RELEASE_TARGET + "/profile.json" if release else slot).lstrip("/")


def default_idmap(mount, linux):
    keys = ("uidMappings", "gidMappings")
    mapped = any(mount.get(key) for key in keys)
    if any(option.partition("=")[0] in ("idmap", "ridmap") and (option != "idmap" or not mapped)
           for option in mount.get("options", [])):
        raise ValueError("unsupported named mount ID mapping")
    if mapped and any(mount.get(key) != linux.get(key) for key in keys):
        raise ValueError("unsupported custom mount ID mapping")
    return mapped


def user_maps(proc):
    return {kind: sorted(tuple(map(int, line.split())) for line in (proc / (kind + "_map")).read_text().splitlines())
            for kind in ("uid", "gid")}


def validate_container_pair(before, directory):
    spec = oci_spec(before)
    process, linux = spec["process"], spec["linux"]
    user = process["user"]
    entrypoint = before["Config"]["Entrypoint"]
    if not entrypoint or len(entrypoint) != 1 or entrypoint[0] != process["args"][0]:
        raise ValueError("previous image requires one binary entrypoint")
    environment = dict(item.split("=", 1) for item in process["env"])
    if len(environment) != len(process["env"]):
        raise ValueError("previous process has duplicate environment keys")
    environment["STACK_WORKER_PROFILE_FILE"] = RELEASE_TARGET + "/profile.json"
    environment.pop("STACK_WORKER_PROFILE_SOURCE_FILE", None)
    if any(not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key) or "\n" in value or "\r" in value
           for key, value in environment.items()):
        raise ValueError("previous environment cannot be passed losslessly to the profile check")
    command = [CLI, "--runtime", before["OCIRuntime"], "create", "--rm", "--pull=never", "--no-healthcheck",
               "--image-volume=ignore", "--log-driver=none", "--timeout=30", "--network=none",
               "--read-only", "--read-only-tmpfs=false", "--cap-drop=all", "--security-opt=no-new-privileges",
               "--http-proxy=false", "--unsetenv-all", "--env-file=/dev/stdin",
               "--user", f'{user["uid"]}:{user["gid"]}', "--workdir", process["cwd"]]
    for group in user.get("additionalGids", []):
        command.extend(("--group-add", str(group)))
    label = process.get("selinuxLabel", "")
    if label:
        command.extend(("--security-opt", "label=level:" + label.split(":", 3)[3]))
    if process.get("apparmorProfile") and process["apparmorProfile"] != "unconfined":
        command.extend(("--security-opt", "apparmor=" + process["apparmorProfile"]))
    old_pid = before["State"]["Pid"]
    old_proc = Path(f"/proc/{old_pid}")
    mappings = user_maps(old_proc)
    if (old_proc / "ns/user").samefile("/proc/self/ns/user"):
        command.append("--userns=host")
    else:
        for kind, ranges in mappings.items():
            for start, host, size in ranges:
                command.extend(("--" + kind + "map", f"{start}:{host}:{size}"))
    destinations = {mount["Destination"] for mount in before["Mounts"] if mount["Type"] in ("bind", "volume")}
    scratch = set(before["HostConfig"].get("Tmpfs") or {})
    scratch.update(m["Destination"] for m in before["Mounts"] if m["Type"] == "tmpfs")
    if scratch - {"/tmp"} or any(m["Type"] not in ("bind", "volume", "tmpfs") for m in before["Mounts"]):
        raise ValueError("unsupported previous profile mount")
    source_flags = check_mounts(old_pid, destinations)
    mounts = {mount["destination"]: mount for mount in spec["mounts"]}
    if RELEASE_TARGET in destinations and default_idmap(mounts[RELEASE_TARGET], linux):
        raise ValueError("unsupported mapped profile release")
    probe_destinations = destinations | {RELEASE_TARGET}
    mapped = set()
    for dest in sorted(destinations - {RELEASE_TARGET}):
        mount = mounts[dest]
        if mount["type"] != "bind":
            raise ValueError("previous volume has no resolved bind source")
        flags = sorted(source_flags[dest] & {"nosuid", "nodev", "noexec"})
        if default_idmap(mount, linux):
            mapped.add(dest)
            flags.append("idmap")
        command.extend(("--volume", bind_argument(mount["source"], dest, flags)))
    command.extend(("--volume", bind_argument(str(directory), RELEASE_TARGET, [])))

    with tempfile.TemporaryDirectory(prefix="hololive-profile-check-") as temporary:
        cidfile = Path(temporary) / "cid"
        probe = None
        command.extend(("--cidfile", str(cidfile), "--entrypoint", entrypoint[0], before["Image"], "--check-worker-profile"))
        try:
            subprocess.run(command, input="".join(f"{k}={v}\n" for k, v in environment.items()),
                           text=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=True, timeout=30)
            probe = cidfile.read_text().strip()
            run(CLI, "init", probe)
            initialized = inspect("container", probe)
            if initialized["State"]["Status"] != "initialized" or initialized["Image"] != before["Image"]:
                raise ValueError("profile check was not initialized with the previous image")
            initialized_spec = oci_spec(initialized)
            actual = initialized_spec["process"]
            actual_mapped = {m["destination"] for m in initialized_spec["mounts"]
                             if m["destination"] in probe_destinations and default_idmap(m, initialized_spec["linux"])}
            if actual_mapped != mapped:
                raise ValueError("profile check mount ID mappings differ from the writer")
            if (actual["args"] != [entrypoint[0], "--check-worker-profile"] or actual["cwd"] != process["cwd"] or
                    dict(item.split("=", 1) for item in actual["env"]) != environment or
                    actual.get("selinuxLabel", "") != label or
                    actual.get("apparmorProfile") != process.get("apparmorProfile") or
                    any(actual["user"][key] != user[key] for key in ("uid", "gid")) or
                    set(actual["user"].get("additionalGids", [])) != set(user.get("additionalGids", []))):
                raise ValueError("profile check process credentials or environment differ from the writer")
            pid = initialized["State"]["Pid"]
            proc = Path(f"/proc/{pid}")
            if user_maps(proc) != mappings:
                raise ValueError("profile check user namespace mappings differ from the writer")
            flags = check_mounts(pid, probe_destinations)
            for dest, options in flags.items():
                original = directory if dest == RELEASE_TARGET else old_proc / ("root" + dest)
                if "ro" not in options or not (proc / ("root" + dest)).samefile(original):
                    raise ValueError("profile check bind differs from the mounted input")
                if dest != RELEASE_TARGET and options & {"idmapped", "nosuid", "nodev", "noexec"} != (
                        source_flags[dest] & {"idmapped", "nosuid", "nodev", "noexec"}):
                    raise ValueError("profile check bind policy differs from the writer")
            if running_generation(inspect("container", before["Id"])) != running_generation(before):
                raise ValueError("previous process changed before the profile check")
            result = subprocess.run([CLI, "start", "--attach", probe], stdout=subprocess.DEVNULL,
                                    stderr=subprocess.DEVNULL, timeout=35)
            if result.returncode:
                raise ValueError(f"previous image --check-worker-profile exited with status {result.returncode}")
        finally:
            if probe is None and cidfile.is_file():
                probe = cidfile.read_text().strip()
            if probe:
                run(CLI, "rm", "--force", "--ignore", "--time=0", probe)


def collector_probe_network(directory, pair, values):
    networks = {"collector-c": ("hololive-net.network", "hololive-bot_hololive-net"),
                "collector-b": ("hololive-collector-b.network", "hololive-collector-b")}
    service = pair["service"]
    if (service not in networks or directory.name != "candidate" or directory.parent.name != service or
            "container_id" in pair):
        raise ValueError("pooled database check requires a collector candidate")
    definition = QUADLET_UNITS[service]
    if profile_bytes(directory / UNIT_RECORD) != profile_bytes(QUADLET_DIR / definition):
        raise ValueError("pooled database check requires the installed candidate unit")
    name = Path(definition).stem
    network_unit, network = networks[service]
    for key, expected in (("ContainerName", name), ("Entrypoint", "./bin/hololive-collector"),
                          ("Network", network_unit + ":alias=" + name.removeprefix("hololive-"))):
        if [v for k, v in values if k == key] != [expected]:
            raise ValueError("unexpected collector probe unit or network")
    declared = unit_values(QUADLET_DIR / network_unit, ("Network",))["Network"]
    if [v for k, v in declared if k == "NetworkName"] != [network]:
        raise ValueError("unexpected installed collector probe network")
    return network


def check_pair(directory, pair, *, pooled=False):
    values = unit_values(directory / UNIT_RECORD)
    if pooled:
        collector_probe_network(directory, pair, values["Container"])
    command = ["systemd-run", "--quiet", "--wait", "--pipe", "--collect",
               "--property=RuntimeMaxSec=60s"]
    for key, value in values["Service"]:
        if key == "EnvironmentFile":
            command.append("--property=EnvironmentFile=" + value)
        elif key == "Environment":
            command.append("--setenv=" + value)
    command.extend((sys.executable, str(Path(__file__).resolve()), "check-pooled-resolved" if pooled else "check-resolved",
                    pair["service"], str(directory)))
    subprocess.run(command, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=65)


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


def profile_security_level(unit):
    service = unit + ".service"
    fragment = Path(run("systemctl", "show", service, "--property=FragmentPath", "--value"))
    if fragment.is_symlink() and fragment.readlink() == Path("/dev/null"):
        prefix = Path(os.environ.get("HOLOLIVE_QUADLET_TEST_ROOT") or "/")
        if fragment != prefix / "etc/systemd/system" / service:
            raise ValueError("unexpected profile preflight mask")
        generated = prefix / "run/systemd/generator" / service
        if generated.is_symlink() or not generated.is_file():
            raise ValueError("masked profile preflight requires the generated unit")
        values = unit_values(generated, ("Unit", "X-Container"))
        if [v for k, v in values["Unit"] if k == "SourcePath"] != [str(QUADLET_DIR / (unit + ".container"))]:
            raise ValueError("masked profile preflight unit source changed")
        native = values["X-Container"]
    else:
        native = unit_values(fragment, ("X-Container",))["X-Container"]
    return dict(native).get("SecurityLabelLevel")


def check_resolved_pair(directory, pair, *, pooled=False):
    values = unit_values(directory / UNIT_RECORD)["Container"]
    environment = dict(os.environ)
    network = collector_probe_network(directory, pair, values) if pooled else "none"
    command = [CLI, "run", "--rm", "--pull=never", "--network", network, "--read-only",
               "--read-only-tmpfs=false", "--log-driver=none", "--no-healthcheck", "--image-volume=ignore",
               "--cap-drop", "all", "--security-opt", "no-new-privileges"]
    if pooled:
        command.extend(("--entrypoint=./bin/hololive-collector", "--http-proxy=false", "--timeout=45"))
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
                        subprocess.run(renderer, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
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
                if any(flag != "idmap" for flag in flags):
                    raise ValueError("unsupported profile preflight bind option")
                command.extend(("--volume", bind_argument(source, target, flags)))
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
        level = profile_security_level(unit)
        if level:
            command.extend(("--security-opt", "label=level:" + expand_unit_value(level)))
        command.extend(("--user", user + ":" + group,
                        "--mount", "type=bind,src=" + str(directory) + ",dst=" + RELEASE_TARGET + ",readonly",
                        "--env", "STACK_WORKER_PROFILE_FILE=" + RELEASE_TARGET + "/profile.json",
                        "--unsetenv", "STACK_WORKER_PROFILE_SOURCE_FILE",
                        pair["image"], "--check-pooled-database" if pooled else "--check-worker-profile"))
        subprocess.run(command, env=environment, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=60)


def prepare_native(service, release, host_env, unit):
    if service != "collector-a":
        raise ValueError("unsupported native slot")
    if release.parent != Path("/opt/hololive-bot/collector/releases"):
        raise ValueError("native release must be release-owned")
    pid = run("systemctl", "show", unit, "-p", "MainPID", "--value")
    if not pid.isdigit() or int(pid) == 0:
        raise ValueError("prepare native profile before stopping the collector")
    process = Path("/proc") / pid
    if not os.path.samefile(process / "exe", release / "bin/hololive-collector"):
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
                    "--property=EnvironmentFile=/etc/stack-secrets/hololive-bot/collector.env",
                    "--property=EnvironmentFile=" + str(host_env),
                    "/usr/bin/env", "STACK_WORKER_PROFILE_FILE=" + str(temporary),
                    str(release / "bin/hololive-collector-wrapper"),
                    str(release / "bin/hololive-collector"), "--check-worker-profile"], check=True, timeout=35)
    if run("systemctl", "show", unit, "-p", "MainPID", "--value") != pid or (process / "stat").read_text().split(")", 1)[1].split()[19] != start:
        raise ValueError("native process changed during preparation")
    final = release / "worker-profile.json"
    if final.exists() or (release / "SHA256SUMS").exists():
        raise ValueError("native release already has profile artifacts")
    temporary.rename(final)
    sums = release / "SHA256SUMS"
    binary_hash = hashlib.sha256((release / "bin/hololive-collector").read_bytes()).hexdigest()
    sums.write_text(f'{binary_hash}  bin/hololive-collector\n{digest(final)}  worker-profile.json\n')
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
    if service != "collector-a":
        raise ValueError("unsupported native slot")
    if release.parent != Path("/opt/hololive-bot/collector/releases"):
        raise ValueError("native release must be release-owned")
    subprocess.run(["sha256sum", "--check", "--strict", "SHA256SUMS"], cwd=release,
                   check=True, stdout=subprocess.DEVNULL, timeout=10)
    pid = run("systemctl", "show", unit, "-p", "MainPID", "--value")
    if not pid.isdigit() or int(pid) == 0:
        raise ValueError("native collector is not running")
    process = Path("/proc") / pid
    if not os.path.samefile(process / "exe", release / "bin/hololive-collector"):
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


def atomic_publish(files, before_replace=None):
    staged = []
    try:
        for path, contents in files:
            fd, name = tempfile.mkstemp(prefix="." + path.name + "-", dir=path.parent)
            temporary = Path(name)
            staged.append((temporary, path))
            with os.fdopen(fd, "wb") as output:
                output.write(contents)
            temporary.chmod(0o444)
        if before_replace is not None:
            before_replace()
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
    if "source_generation" in pair:
        revision = pair["source_generation"]
        if (service not in ("hololive-api", "hololive-alarm-worker") or revision != pair["revision"]
                or not re.fullmatch("[0-9a-f]{40}", revision)):
            raise ValueError("invalid app prestart generation")
        script = COMPOSE_ROOT / "app-sources" / revision / "scripts/deploy/quadlet-prestart.sh"
        lines.extend(("[Service]", "ExecStartPre=", f"ExecStartPre=/usr/bin/bash {script} {service}"))
    return ("\n".join(lines) + "\n").encode("utf-8")


def dropin(directory, service, pair, output=None, app_version=None):
    atomic_publish([(output or directory / DROPIN, dropin_contents(directory, service, pair, app_version))])


def verify_dropin(directory, service, pair, overlay=None):
    contents = profile_bytes(overlay or directory / DROPIN)
    expected = [dropin_contents(directory, service, pair)]
    if service == "hololive-api" and "app_version" in pair:
        expected.append(dropin_contents(directory, service, pair, pair["app_version"]))
    if contents not in expected:
        raise ValueError("retained drop-in changed")
    return contents


def quadlet_slot(service, unit):
    lines = [line.strip() for line in unit.splitlines()]
    if lines.count("Environment=STACK_WORKER_PROFILE_FILE=" + TARGET + "/" + PROFILES[service]) != 1:
        raise ValueError("candidate profile env does not match its service slot")
    mounts = [line for line in lines if line.startswith("Volume=") and line.split(":")[1:2] == [TARGET]]
    if mounts != ["Volume=" + SOURCE + ":" + TARGET + ":ro"]:
        raise ValueError("candidate profile directory does not match its service slot")


def create_pair_directory(directory):
    directory.mkdir(mode=0o755, parents=True, exist_ok=False)
    directory.chmod(0o755)


def require_captured(pair, container, before):
    cid, image, started, pid = running_generation(before)
    if (pair.get("container_name") != container or before["Name"] != container or
            pair.get("container_id") != cid or pair["image"] != "sha256:" + image or
            pair.get("container_started_at") != started or pair.get("container_pid") != pid or
            "captured_release" not in pair):
        raise ValueError("captured writer generation does not match the running container")


def verify_writer(directory, service, container, *, captured):
    before = inspect("container", container)
    generation = running_generation(before)
    pair = verify(directory, service, before["Image"])
    if captured:
        require_captured(pair, container, before)
    release = pair["captured_release"] if captured else str(directory)
    if service == "hololive-api" and "source_generation" in pair:
        if [v for v in before["Config"]["Env"] if v.startswith("APP_VERSION=")] != ["APP_VERSION=" + pair["app_version"]]:
            raise ValueError("running APP_VERSION differs from the pinned generation")
    loaded = container_profile_hash(service, before)
    mounted = writer_profile(before, service, release)
    if loaded != pair["profile_hash"] or (release is not None and digest(mounted) != loaded):
        raise ValueError("running profile differs from its validated release pair")
    if running_generation(inspect("container", container)) != generation:
        raise ValueError("writer generation changed during profile verification")


def capture(container, service, directory, root):
    before = inspect("container", container)
    generation = running_generation(before)
    if before["Name"] != container:
        raise ValueError("profile capture requires the writer's container name")
    labelled = (before["Config"].get("Labels") or {}).get(LABEL)
    mounted = writer_profile(before, service, labelled)
    previous = labelled
    prepared = Path(root) / "artifacts/worker-profile/active" / (service + ".pair")
    if not labelled and os.path.lexists(prepared):
        previous = profile_bytes(prepared).decode().strip()
        if not previous:
            raise ValueError("empty prepared profile pointer")
    create_pair_directory(directory)
    if previous:
        prior = Path(previous).resolve()
        if not prior.is_relative_to((Path(root) / "artifacts/worker-profile/releases").resolve()):
            raise ValueError("previous profile escaped the release store")
        pair = verify(prior, service, before["Image"])
        if not labelled:
            installed = QUADLET_DIR / (QUADLET_UNITS[service] + ".d") / DROPIN
            require_captured(pair, container, before)
            if pair["captured_release"] is not None or verify_dropin(prior, service, pair) != profile_bytes(installed):
                raise ValueError("prepared profile does not belong to the running container")
        loaded = container_profile_hash(service, before)
        raw = profile_bytes(mounted if labelled else prior / "profile.json")
        if (labelled and labelled != str(prior)) or loaded != pair["profile_hash"] or hashlib.sha256(raw).hexdigest() != loaded:
            raise ValueError("retained profile bytes differ from the running process")
        (directory / "profile.json").write_bytes(raw)
        if (prior / UNIT_RECORD).is_file():
            (directory / UNIT_RECORD).write_bytes((prior / UNIT_RECORD).read_bytes())
    else:
        running_hash = container_profile_hash(service, before)
        (directory / "profile.json").write_bytes(profile_bytes(mounted))
        if running_hash != digest(directory / "profile.json"):
            raise ValueError("previous profile bytes differ from the running process; writer unchanged")
        pair = image_identity(before["Image"])
        installed = QUADLET_DIR / QUADLET_UNITS[service]
        if installed.is_file():
            (directory / UNIT_RECORD).write_bytes(installed.read_bytes())
    (directory / "profile.json").chmod(0o444)
    validate_container_pair(before, directory)
    if running_generation(inspect("container", container)) != generation:
        raise ValueError("previous process changed during profile capture")
    pair["retained_image"] = "hololive-worker-profile:pair-" + hashlib.sha256(str(directory).encode()).hexdigest()[:24]
    run(CLI, "tag", before["Image"], pair["retained_image"])
    pair.update(container_name=container, container_id=before["Id"], container_started_at=generation[2],
                container_pid=generation[3], captured_release=labelled)
    if service == "hololive-api":
        pair["app_version"] = dict(item.split("=", 1) for item in before["Config"]["Env"])["APP_VERSION"]
    require_captured(pair, container, inspect("container", container))
    save_pair(directory, service, pair)


def activate(directory, service, root, prepared_overlay=None, *, captured_container=None):
    releases = (Path(root) / "artifacts/worker-profile/releases").resolve()
    if not directory.resolve().is_relative_to(releases):
        raise ValueError("active profile pair must live under artifacts/worker-profile/releases")
    pair = verify(directory, service)
    contents = verify_dropin(directory, service, pair, prepared_overlay)
    active = Path(root) / "artifacts/worker-profile/active"
    active.mkdir(mode=0o755, parents=True, exist_ok=True)
    files = [(directory / DROPIN, contents)] if prepared_overlay is not None else []
    unit_dropins = QUADLET_DIR / (QUADLET_UNITS[service] + ".d")
    unit_dropins.mkdir(mode=0o755, parents=True, exist_ok=True)
    files.append((unit_dropins / DROPIN, contents))
    files.append((active / (service + ".pair"), (str(directory) + "\n").encode("utf-8")))
    atomic_publish(files, None if captured_container is None else
                   lambda: verify_writer(directory, service, captured_container, captured=True))


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
        create_pair_directory(directory)
        (directory / UNIT_RECORD).write_text(unit_text)
        source = Path(SOURCE) / PROFILES[service]
        (directory / "profile.json").write_bytes(profile_bytes(source))
        identity = image_identity(image)
        source = Path(__file__).resolve().parents[3]
        if service in ("hololive-api", "hololive-alarm-worker") and source.parent == COMPOSE_ROOT / "app-sources":
            revision = identity["revision"]
            if (not re.fullmatch("[0-9a-f]{40}", revision) or source.name != revision
                    or (source / ".source-revision").read_bytes() != (revision + "\n").encode("ascii")):
                raise ValueError("candidate image does not belong to its sealed source")
            identity["source_generation"] = revision
            if service == "hololive-api":
                version = (source / "hololive/hololive-api/VERSION").read_text()
                if not re.fullmatch(r"(?:0|[1-9][0-9]*)(?:\.(?:0|[1-9][0-9]*)){2}\n", version):
                    raise ValueError("invalid sealed API version")
                identity["app_version"] = version.rstrip("\n")
        pair = save_pair(directory, service, identity)
        dropin(directory, service, pair)
    elif action in ("capture", "prepare"):
        capture(rest[0], service, directory, rest[1])
        if action == "prepare":
            pair = verify(directory, service)
            dropin(directory, service, pair)
            activate(directory, service, rest[1], captured_container=rest[0])
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
    elif action == "verify-dropin":
        verify_dropin(directory, service, verify(directory, service), Path(rest[0]) if rest else None)
    elif action in ("check", "check-pooled"):
        check_pair(directory, verify(directory, service), pooled=action == "check-pooled")
    elif action in ("check-resolved", "check-pooled-resolved"):
        check_resolved_pair(directory, verify(directory, service), pooled=action == "check-pooled-resolved")
    elif action in ("verify-live", "verify-captured"):
        verify_writer(directory, service, rest[0], captured=action == "verify-captured")
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
