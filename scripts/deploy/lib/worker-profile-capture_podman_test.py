#!/usr/bin/env python3
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile


def podman(*args):
    return subprocess.check_output(["podman", *args], text=True, timeout=60).strip()


SPEC = importlib.util.spec_from_file_location("pair", Path(__file__).with_name("worker-profile-pair.py"))
pair = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(pair)


def exercise(root, image, userns, volume=None):
    service = "hololive-alarm-worker"
    profile = root / "desired" / pair.PROFILES[service]
    profile.parent.mkdir()
    profile.parent.chmod(0o755)
    profile.write_text('{"fixture":true}\n')
    profile.chmod(0o444)
    digest = hashlib.sha256(profile.read_bytes()).hexdigest()
    pair.SOURCE = str(profile.parent)
    pair.QUADLET_DIR = root / "units"
    pair.QUADLET_DIR.mkdir()
    token = root / "group-input"
    token.write_text("fixture-only")
    token.chmod(0o640)
    os.chown(token, 0, 1002)
    data = Path(podman("volume", "inspect", "-f", "{{.Mountpoint}}", volume)) if volume else root / "data"
    if not volume:
        data.mkdir()
    os.chown(data, 1000, 1000)
    data.chmod(0o700)
    (data / "input").write_text("mapped")
    os.chown(data / "input", 1000, 1000)
    (data / "input").chmod(0o600)
    writer = root / "writer"
    writer.write_text('''#!/bin/sh
set -eu
if [ "$1" = serve ]; then
  [ "$(sha256sum "$STACK_WORKER_PROFILE_FILE" | cut -d' ' -f1)" = "$LOADED_HASH" ] || exit 10
  exec sleep 180
fi
[ "$1" = --check-worker-profile ] && [ "$#" = 1 ] || exit 11
[ "$(cat "$CACHE_PASSWORD_FILE")" = fixture-only ] || exit 12
[ "$(cat /data/input)" = mapped ] || exit 13
[ "$(sha256sum "$STACK_WORKER_PROFILE_FILE" | cut -d' ' -f1)" = "$LOADED_HASH" ] || exit 14
[ "${STACK_WORKER_PROFILE_SOURCE_FILE+x}" != x ] || exit 15
[ "$FIXTURE_VALUE" = 'literal $HOME = value' ] || exit 16
[ "$(ls /sys/class/net)" = lo ] || exit 17
if (echo changed > /data/input) 2>/dev/null; then exit 18; fi
if (echo changed > /tmp/fixture-write) 2>/dev/null; then exit 19; fi
if [ -p /data/check-in-flight ]; then IFS= read -r done < /data/check-in-flight; fi
''')
    writer.chmod(0o755)
    health = root / "healthcheck"
    health.write_text('''#!/bin/sh
set -eu
[ "$1" = --body-api-key-env ] && [ "$2" = API_SECRET_KEY ] || exit 1
if [ -p /data/diagnostics-in-flight ]; then IFS= read -r done < /data/diagnostics-in-flight; fi
printf '{"contractVersion":1,"profile":{"hash":"%s"}}\\n' "$LOADED_HASH"
''')
    health.chmod(0o755)
    (pair.QUADLET_DIR / pair.QUADLET_UNITS[service]).write_text("[Container]\n")
    name = "holo-capture-" + root.parent.name + ("-mapped" if userns else "-host")
    release = root / "artifacts/worker-profile/releases"
    release.mkdir(parents=True)
    subprocess.run(["chcon", "-t", "container_file_t", "-l", "s0", release], check=True)
    namespace = "auto:size=65536,gidmapping=1002:1002:1" if userns else "host"
    mount = "Z,idmap" if userns else "Z,nocopy"
    args = ["--pull=never", "--network=none", "--read-only", "--no-healthcheck", "--image-volume=ignore",
            "--cap-drop=all", "--security-opt=no-new-privileges", "--user=1000:1000", "--group-add=1002",
            "--userns=" + namespace, "--name", name, "--entrypoint=/fixture-writer",
            "-v", f"{token}:/fixture-group:ro,Z", "-v", f"{volume or data}:/data:{mount}",
            "-v", f"{writer}:/fixture-writer:ro,Z", "-v", f"{health}:/app/bin/healthcheck:ro,Z",
            "-v", f"{profile.parent}:{pair.TARGET}:ro,Z", "--tmpfs=/tmp:size=1m,noexec,nosuid,nodev",
            "-e", "HOLOLIVE_METRICS_ADDR=:12345", "-e", "CACHE_PASSWORD_FILE=/fixture-group",
            "-e", "STACK_WORKER_PROFILE_FILE=" + pair.TARGET + "/" + profile.name,
            "-e", "LOADED_HASH=" + digest,
            "-e", "FIXTURE_VALUE=literal $HOME = value"]
    if userns:
        prior = release / "prior"
        pair.create_pair_directory(prior)
        (prior / "profile.json").write_bytes(profile.read_bytes())
        pair.save_pair(prior, service, pair.image_identity(image))
        args.extend(("--label", pair.LABEL + "=" + str(prior), "-v", f"{prior}:{pair.RELEASE_TARGET}:ro",
                     "-e", "STACK_WORKER_PROFILE_FILE=" + pair.RELEASE_TARGET + "/profile.json",
                     "-e", "STACK_WORKER_PROFILE_SOURCE_FILE=" + pair.TARGET + "/" + profile.name))
        profile.write_text('{"desired":"not-loaded"}\n')
    cid = podman("run", "-d", *args, image, "serve")
    retained = None
    try:
        before = pair.inspect("container", cid)
        assert before["ProcessLabel"].split(":", 3)[3].startswith("s0:c")
        accepted = release / "accepted"
        pair.capture(name, service, accepted, root)
        receipt = json.loads((accepted / "pair.json").read_text())
        retained = receipt["retained_image"]
        assert receipt["container_id"] == cid and receipt["profile_hash"] == digest
        assert receipt["container_name"] == name and receipt["image"] == "sha256:" + before["Image"]
        assert receipt["container_started_at"] == before["State"]["StartedAt"]
        assert receipt["container_pid"] == before["State"]["Pid"]
        pair.verify_writer(accepted, service, name, captured=True)
        if userns:
            assert receipt["captured_release"] == str(prior)
            assert hashlib.sha256(profile.read_bytes()).hexdigest() != digest
            pair.verify_writer(prior, service, name, captured=False)
            try:
                pair.verify_writer(accepted, service, name, captured=False)
            except ValueError as error:
                assert "expected profile release" in str(error), error
            else:
                raise AssertionError("동일 바이트의 잘못된 활성 release를 수용함")
            original_mirror = root / "original-mirror"
            profile.parent.rename(original_mirror)
            subprocess.run(["cp", "-a", original_mirror, profile.parent], check=True)
            try:
                pair.verify_writer(prior, service, name, captured=False)
            except ValueError as error:
                assert "expected read-only bind" in str(error), error
            else:
                raise AssertionError("교체된 mirror inode를 수용함")
            profile.unlink()
            profile.parent.rmdir()
            original_mirror.rename(profile.parent)
            fifo = data / "diagnostics-in-flight"
            os.mkfifo(fifo)
            os.chown(fifo, 1000, 1000)
            fifo.chmod(0o600)
            subprocess.run(["chcon", "--reference", data, fifo], check=True)
            other = podman("run", "-d", "--pull=never", "--network=none", "--read-only", "--no-healthcheck",
                           "--entrypoint=/bin/sleep", image, "60")
            try:
                swap = '''exec 3>"$1"
podman rename "$2" "$3-original"
podman rename "$4" "$3"
printf 'done\\n' >&3'''
                with subprocess.Popen(["timeout", "20s", "sh", "-ec", swap, "--", str(fifo), cid, name, other]) as change:
                    try:
                        pair.verify_writer(prior, service, name, captured=False)
                    except ValueError as error:
                        assert "generation changed" in str(error), error
                    else:
                        raise AssertionError("진단 도중 이름을 넘긴 writer를 수용함")
                    assert change.wait(timeout=22) == 0
            finally:
                podman("rm", "--force", "--time=0", other)
                if pair.inspect("container", cid)["Name"] != name:
                    podman("rename", cid, name)
                fifo.unlink()
        assert (data / "input").read_text() == "mapped"
        print(f"{namespace}: MCS, group-only input, mapped owner, profile, read-only and no-network: pass", flush=True)

        token.rename(root / "original")
        subprocess.run(["cp", "--preserve=all", root / "original", token], check=True)
        bad = release / "replaced-bind"
        try:
            pair.capture(name, service, bad, root)
        except ValueError as error:
            assert "differs from the mounted input" in str(error), error
        else:
            raise AssertionError("accepted a new bind inode")
        assert not (bad / "SHA256SUMS").exists()
        token.unlink()
        (root / "original").rename(token)
        token.chmod(0o600)
        bad = release / "denied"
        try:
            pair.capture(name, service, bad, root)
        except ValueError as error:
            assert "exited with status 12" in str(error), error
        else:
            raise AssertionError("accepted a profile check without group read access")
        assert not (bad / "SHA256SUMS").exists()
        assert pair.running_generation(pair.inspect("container", name)) == pair.running_generation(before)
        token.chmod(0o640)
        podman("restart", "--time=0", cid)
        try:
            pair.verify_writer(accepted, service, name, captured=True)
        except ValueError as error:
            assert "captured writer generation" in str(error), error
        else:
            raise AssertionError("같은 ID로 재시작한 writer를 기존 정지 영수증으로 수용함")
        if userns:
            pair.verify_writer(prior, service, name, captured=False)
        fifo = data / "check-in-flight"
        os.mkfifo(fifo)
        os.chown(fifo, 1000, 1000)
        fifo.chmod(0o600)
        subprocess.run(["chcon", "--reference", data, fifo], check=True)
        replacement = root / "replacement.cid"
        replace = '''exec 3>"$1"
podman stop --time=0 "$2" >/dev/null
podman rename "$2" "$3-old"
podman run -d --pull=never --network=none --read-only --no-healthcheck --image-volume=ignore \
--cap-drop=all --user=1000:1000 --name "$3" --cidfile "$4" --entrypoint=/bin/sleep "$5" 60 >/dev/null
printf 'done\\n' >&3'''
        with subprocess.Popen(["timeout", "20s", "sh", "-ec", replace, "--", str(fifo), cid, name,
                               str(replacement), image]) as change:
            try:
                pair.capture(name, service, release / "in-flight", root)
            except ValueError as error:
                assert "changed during profile capture" in str(error), error
            else:
                raise AssertionError("accepted the replacement writer")
            assert change.wait(timeout=22) == 0 and not (release / "in-flight/pair.json").exists()
        try:
            pair.capture(cid, service, release / "stopped", root)
        except ValueError as error:
            assert "running writer" in str(error), error
        else:
            raise AssertionError("captured a stopped writer")
        assert pair.inspect("container", cid)["State"]["Status"] == "exited"
        print(f"{namespace}: replaced inode, denied read, in-flight replacement and stopped writer refused: pass", flush=True)
    finally:
        if (root / "replacement.cid").exists():
            podman("rm", "--force", "--time=0", "--cidfile", str(root / "replacement.cid"))
        podman("rm", "--force", "--time=0", cid)
        if retained is not None:
            podman("untag", image, retained)


def main():
    if len(sys.argv) != 2 or os.geteuid() != 0 or os.uname().nodename.split(".")[0] != "kapu":
        sys.exit("usage on kapu: sudo python3 -B worker-profile-capture_podman_test.py LOCAL_ALPINE_IMAGE")
    if subprocess.check_output(["getenforce"], text=True).strip() != "Enforcing":
        sys.exit("SELinux Enforcing required")
    image = pair.inspect("image", sys.argv[1])
    if image["Architecture"] != {"x86_64": "amd64", "aarch64": "arm64"}[platform.machine()]:
        sys.exit("host-native fixture image required")
    with tempfile.TemporaryDirectory(prefix="holo-capture-", dir="/var/tmp") as path:
        temp = Path(path)
        (temp / "Containerfile").write_text("FROM " + image["Id"] + "\nLABEL org.opencontainers.image.revision=" + "a" * 40 + "\n")
        fixture = podman("build", "--pull=never", "--network=none", "--quiet", "--layers=false", "-t", "localhost/" + temp.name, str(temp))
        try:
            volume = podman("volume", "create", temp.name)
            try:
                for name, mapped in (("auto", True), ("host", False)):
                    root = temp / name
                    root.mkdir()
                    exercise(root, fixture, mapped, None if mapped else volume)
            finally:
                podman("volume", "rm", volume)
        finally:
            podman("rmi", "--no-prune", "localhost/" + temp.name)


if __name__ == "__main__":
    main()
