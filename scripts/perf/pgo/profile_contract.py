import datetime as dt
import hashlib
import json
import math
import os
import re
import subprocess
from pathlib import Path

COMPARISON_FIELDS = (
    "cpuPercentDelta",
    "p95LatencyDelta",
    "p99LatencyDelta",
    "rssDelta",
    "binarySizeDelta",
    "hotBenchmarkPercentDelta",
)
ACCEPTED_BY = "scripts/perf/pgo/generate.sh"
MAX_EXPIRES_AFTER_DAYS = 45
MAX_GENERATED_AT_FUTURE_SKEW = dt.timedelta(minutes=5)
HOT_BENCH_MAX_REGRESSION_PERCENT = 3.0
CPU_MAX_REGRESSION_PERCENT = 3.0
P95_MAX_REGRESSION_PERCENT = 3.0
META_FIELDS = {
    "schemaVersion",
    "service",
    "mainPackage",
    "generatedAt",
    "profileCollectedAt",
    "sourceGitSha",
    "goVersion",
    "profileDurationSeconds",
    "profileSha256",
    "profileExecutable",
    "profileBuildId",
    "collectionBinarySha256",
    "profileMainPackage",
    "workloadName",
    "workloadDescription",
    "trafficMix",
    "comparison",
    "acceptedBy",
    "expiresAfterDays",
}


class PGOError(Exception):
    pass


def load_json_strict(path: Path, label: str):
    def reject_constant(value: str):
        raise PGOError(f"{label} contains invalid numeric constant: {value}")

    try:
        return json.loads(path.read_text(encoding="utf-8"), parse_constant=reject_constant)
    except OSError as exc:
        raise PGOError(f"read {label}: {exc}") from exc
    except json.JSONDecodeError as exc:
        raise PGOError(f"{label} json invalid: {exc}") from exc


def _require_mapping(value, path: str) -> dict:
    if not isinstance(value, dict):
        raise PGOError(f"{path} must be a mapping")
    return value


def _require_string(value, path: str) -> str:
    if not isinstance(value, str) or not value:
        raise PGOError(f"{path} must be a non-empty string")
    return value


def _require_number(value, path: str) -> float:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        raise PGOError(f"{path} must be a number")
    try:
        numeric = float(value)
    except (OverflowError, ValueError) as exc:
        raise PGOError(f"{path} must be a finite number") from exc
    if not math.isfinite(numeric):
        raise PGOError(f"{path} must be a finite number")
    return numeric


def verdict(comparison: dict) -> tuple[str, list[str]]:
    reasons: list[str] = []
    cpu_improvement = max(0.0, -comparison["cpuPercentDelta"])
    latency_improved = comparison["p95LatencyDelta"] < 0 or comparison["p99LatencyDelta"] < 0
    hot_bench_improved = comparison["hotBenchmarkPercentDelta"] >= 3.0
    if comparison["p99LatencyDelta"] > 0:
        reasons.append("p99 latency regression > 0%")
    if comparison["p95LatencyDelta"] > P95_MAX_REGRESSION_PERCENT:
        reasons.append("p95 latency regression > 3%")
    if comparison["cpuPercentDelta"] > CPU_MAX_REGRESSION_PERCENT:
        reasons.append("CPU delta > +3%")
    if comparison["rssDelta"] > 3.0:
        reasons.append("RSS delta > +3%")
    if comparison["binarySizeDelta"] > 5.0:
        reasons.append("binary size delta > +5%")
    if comparison["hotBenchmarkPercentDelta"] < -HOT_BENCH_MAX_REGRESSION_PERCENT:
        reasons.append("hot benchmark regression > 3%")
    if cpu_improvement < 2.0 and not latency_improved and not hot_bench_improved:
        reasons.append("CPU improvement < 2%, no p95/p99 improvement, hot bench improvement < 3%")
    if reasons:
        return "REJECTED", reasons
    if cpu_improvement >= 2.0 or latency_improved or hot_bench_improved:
        return "ACCEPTED", []
    return "REJECTED", ["no adoption criterion met"]


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def pprof_profile_info(profile: Path) -> dict:
    pprof_env = os.environ.copy()
    pprof_env["TZ"] = "UTC"
    try:
        summary = subprocess.run(
            ["go", "tool", "pprof", "-top", str(profile)],
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            env=pprof_env,
        ).stdout
        raw = subprocess.run(
            ["go", "tool", "pprof", "-raw", str(profile)],
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            env=pprof_env,
        ).stdout
    except subprocess.CalledProcessError as exc:
        raise PGOError(f"profile is not readable by go tool pprof: {profile}: {exc.stdout.strip()}") from exc

    def summary_field(prefix: str) -> str:
        for line in summary.splitlines():
            if line.startswith(prefix):
                return line.removeprefix(prefix).strip()
        raise PGOError(f"profile missing {prefix.rstrip(': ')}: {profile}")

    duration = None
    for line in raw.splitlines():
        if not line.startswith("Duration: "):
            continue
        raw_duration = line.removeprefix("Duration: ").strip()
        if re.fullmatch(r"\d+(?:\.\d*)?", raw_duration):
            duration = float(raw_duration)
        else:
            match = re.fullmatch(
                r"(?:(\d+(?:\.\d*)?)h)?(?:(\d+(?:\.\d*)?)m)?(\d+(?:\.\d*)?)s?",
                raw_duration,
            )
            if not match:
                raise PGOError(f"actual profile duration is invalid: {line}")
            hours, minutes, seconds = (float(value or 0) for value in match.groups())
            duration = hours * 3600 + minutes * 60 + seconds
        break
    if duration is None:
        raise PGOError(f"profile missing duration: {profile}")
    raw_time = summary_field("Time: ")
    try:
        collected_at = dt.datetime.strptime(raw_time, "%Y-%m-%d %H:%M:%S UTC").replace(tzinfo=dt.timezone.utc)
    except ValueError as exc:
        raise PGOError(f"profile collection time is invalid: {raw_time}") from exc
    return {
        "profileSha256": sha256_file(profile),
        "profileExecutable": Path(summary_field("File: ")).name,
        "profileBuildId": summary_field("Build ID: "),
        "actualProfileDurationSeconds": duration,
        "profileCollectedAt": collected_at.isoformat(),
    }


def _expected_main_import_path(repo_root: Path, main: str) -> str:
    main_path = Path(main)
    cwd = main_path if main_path.is_absolute() else repo_root
    package = "." if main_path.is_absolute() else main
    try:
        result = subprocess.run(
            ["go", "list", "-buildvcs=false", "-f", "{{.ImportPath}}", package],
            cwd=cwd,
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
        )
    except subprocess.CalledProcessError as exc:
        raise PGOError(f"resolve main package import path: {exc.stdout.strip()}") from exc
    return result.stdout.strip()


def _binary_main_import_path(binary: Path) -> str:
    try:
        output = subprocess.run(
            ["go", "version", "-m", str(binary)],
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
        ).stdout
    except subprocess.CalledProcessError as exc:
        raise PGOError(f"read collection binary build info: {exc.stdout.strip()}") from exc
    for line in output.splitlines():
        fields = line.strip().split("\t")
        if len(fields) == 2 and fields[0] == "path":
            return fields[1]
    raise PGOError(f"collection binary has no Go main package path: {binary}")


def _binary_elf_build_id(binary: Path) -> str:
    try:
        output = subprocess.run(
            ["readelf", "-n", str(binary)],
            check=True,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
        ).stdout
    except FileNotFoundError as exc:
        raise PGOError("readelf is required to verify profile provenance") from exc
    except subprocess.CalledProcessError as exc:
        raise PGOError(f"read collection binary build ID: {exc.stdout.strip()}") from exc
    for line in output.splitlines():
        if "Build ID:" in line:
            return line.split("Build ID:", 1)[1].strip()
    raise PGOError(f"collection binary has no ELF build ID: {binary}")


def collection_provenance(repo_root: Path, main: str, profile: Path, binary: Path) -> dict:
    if not binary.is_file():
        raise PGOError(f"collector did not create profiling binary: {binary}")
    profile_info = pprof_profile_info(profile)
    expected_import = _expected_main_import_path(repo_root, main)
    actual_import = _binary_main_import_path(binary)
    if actual_import != expected_import:
        raise PGOError(f"collection binary main package {actual_import!r} != expected {expected_import!r}")
    binary_build_id = _binary_elf_build_id(binary)
    if profile_info["profileBuildId"] != binary_build_id:
        raise PGOError("profile build ID does not match collection binary")
    if profile_info["profileExecutable"] != binary.name:
        raise PGOError("profile executable does not match collection binary")
    return {
        "profileSha256": profile_info["profileSha256"],
        "profileExecutable": profile_info["profileExecutable"],
        "profileBuildId": profile_info["profileBuildId"],
        "profileCollectedAt": profile_info["profileCollectedAt"],
        "collectionBinarySha256": sha256_file(binary),
        "profileMainPackage": actual_import,
    }


def validate_meta(path: Path) -> dict:
    data = load_json_strict(path, "meta")
    if not isinstance(data, dict):
        raise PGOError("meta must be an object")
    for field in META_FIELDS:
        if field not in data:
            raise PGOError(f"missing field: {field}")
    if data["schemaVersion"] != 1:
        raise PGOError("schemaVersion must be 1")
    string_fields = (
        "service", "mainPackage", "generatedAt", "profileCollectedAt", "sourceGitSha", "goVersion",
        "profileExecutable", "profileBuildId", "profileMainPackage", "workloadName",
        "workloadDescription", "acceptedBy",
    )
    for field in string_fields:
        _require_string(data[field], field)
    if Path(data["profileExecutable"]).name != data["profileExecutable"]:
        raise PGOError("profileExecutable must be a basename")
    if data["acceptedBy"] != ACCEPTED_BY:
        raise PGOError(f"acceptedBy must be {ACCEPTED_BY!r}")
    for field in ("profileSha256", "collectionBinarySha256"):
        if not re.fullmatch(r"[0-9a-f]{64}", _require_string(data[field], field)):
            raise PGOError(f"{field} must be a lowercase SHA-256 digest")
    if not isinstance(data["profileDurationSeconds"], int) or isinstance(data["profileDurationSeconds"], bool):
        raise PGOError("profileDurationSeconds must be an integer")
    if data["profileDurationSeconds"] < 600:
        raise PGOError("profileDurationSeconds must be at least 600")
    if not isinstance(data["expiresAfterDays"], int) or isinstance(data["expiresAfterDays"], bool):
        raise PGOError("expiresAfterDays must be an integer")
    if not 1 <= data["expiresAfterDays"] <= MAX_EXPIRES_AFTER_DAYS:
        raise PGOError(f"expiresAfterDays must be between 1 and {MAX_EXPIRES_AFTER_DAYS}")
    traffic = _require_mapping(data["trafficMix"], "trafficMix")
    if not traffic:
        raise PGOError("trafficMix must not be empty")
    for key, value in traffic.items():
        _require_string(key, f"trafficMix key {key}")
        _require_number(value, f"trafficMix.{key}")
    if abs(sum(traffic.values()) - 1.0) > 0.000001:
        raise PGOError("trafficMix ratios must sum to 1")
    comparison = _require_mapping(data["comparison"], "comparison")
    for field in COMPARISON_FIELDS:
        if field not in comparison:
            raise PGOError(f"missing field: comparison.{field}")
        _require_number(comparison[field], f"comparison.{field}")
    result, reasons = verdict(comparison)
    if result != "ACCEPTED":
        raise PGOError("metadata comparison verdict REJECTED: " + "; ".join(reasons))
    try:
        generated_at = dt.datetime.fromisoformat(data["generatedAt"])
    except ValueError as exc:
        raise PGOError("generatedAt must be an ISO-8601 timestamp") from exc
    if generated_at.tzinfo is None or generated_at.utcoffset() is None:
        raise PGOError("generatedAt must include a timezone offset")
    if generated_at.astimezone(dt.timezone.utc) > dt.datetime.now(dt.timezone.utc) + MAX_GENERATED_AT_FUTURE_SKEW:
        raise PGOError("generatedAt is more than 5 minutes in the future")
    try:
        collected_at = dt.datetime.fromisoformat(data["profileCollectedAt"])
    except ValueError as exc:
        raise PGOError("profileCollectedAt must be an ISO-8601 timestamp") from exc
    if collected_at.tzinfo is None or collected_at.utcoffset() is None:
        raise PGOError("profileCollectedAt must include a timezone offset")
    if collected_at.astimezone(dt.timezone.utc) > generated_at.astimezone(dt.timezone.utc) + MAX_GENERATED_AT_FUTURE_SKEW:
        raise PGOError("profileCollectedAt is after generatedAt by more than 5 minutes")
    return data


def validate_profile(profile: Path, meta_path: Path) -> None:
    data = validate_meta(meta_path)
    info = pprof_profile_info(profile)
    for field in ("profileSha256", "profileExecutable", "profileBuildId", "profileCollectedAt"):
        if info[field] != data[field]:
            raise PGOError(f"{field} mismatch: actual {info[field]!r}, metadata {data[field]!r}")
    actual = info["actualProfileDurationSeconds"]
    metadata = float(data["profileDurationSeconds"])
    if actual < 600:
        raise PGOError(f"actual profile duration {actual:g}s is below 600s")
    tolerance = max(5.0, min(60.0, metadata * 0.01))
    if abs(actual - metadata) > tolerance:
        raise PGOError(
            f"actual profile duration {actual:g}s differs from metadata {metadata:g}s by more than {tolerance:g}s"
        )
