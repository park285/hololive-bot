#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
export PGO_REPO_ROOT="${REPO_ROOT}"
cd "${REPO_ROOT}"

python3 -B - "$@" <<'PY'
import argparse
import copy
import datetime as dt
import json
import math
import os
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(os.environ["PGO_REPO_ROOT"]) / "scripts/perf/pgo"))
from profile_contract import (
    ACCEPTED_BY,
    COMPARISON_FIELDS,
    MAX_EXPIRES_AFTER_DAYS,
    PGOError,
    collection_provenance,
    load_json_strict,
    validate_meta,
    validate_profile,
    verdict,
)

TOP_FIELDS = {"schemaVersion", "name", "service", "duration", "traffic", "drivers"}
COLLECT_COMPARISON_FIELDS = COMPARISON_FIELDS

def strip_comment(line: str) -> str:
    in_single = False
    in_double = False
    for idx, char in enumerate(line):
        if char == "'" and not in_double:
            in_single = not in_single
        elif char == '"' and not in_single:
            in_double = not in_double
        elif char == "#" and not in_single and not in_double:
            return line[:idx]
    return line

def parse_scalar(value: str):
    value = value.strip()
    if value in {"true", "True"}:
        return True
    if value in {"false", "False"}:
        return False
    if (value.startswith('"') and value.endswith('"')) or (
        value.startswith("'") and value.endswith("'")
    ):
        return value[1:-1]
    if re.fullmatch(r"-?\d+", value):
        return int(value)
    if re.fullmatch(r"-?\d+\.\d+", value):
        return float(value)
    return value

def split_inline_items(inner: str) -> list[str]:
    items: list[str] = []
    current: list[str] = []
    in_single = False
    in_double = False
    for char in inner:
        if char == "'" and not in_double:
            in_single = not in_single
        elif char == '"' and not in_single:
            in_double = not in_double
        if char == "," and not in_single and not in_double:
            item = "".join(current).strip()
            if item:
                items.append(item)
            current = []
            continue
        current.append(char)
    item = "".join(current).strip()
    if item:
        items.append(item)
    return items

def parse_inline_map(value: str) -> dict:
    inner = value.strip()[1:-1].strip()
    result = {}
    if not inner:
        return result
    for item in split_inline_items(inner):
        if ":" not in item:
            raise PGOError(f"invalid inline map item: {item}")
        key, raw_value = item.split(":", 1)
        result[key.strip()] = parse_scalar(raw_value)
    return result

def parse_value(value: str, anchors: dict[str, object]):
    value = value.strip()
    if value.startswith("*"):
        name = value[1:].strip()
        if name not in anchors:
            raise PGOError(f"unknown yaml alias: {name}")
        return copy.deepcopy(anchors[name])
    if value.startswith("{") and value.endswith("}"):
        return parse_inline_map(value)
    return parse_scalar(value)

def parse_yaml(path: Path) -> dict:
    root: dict = {}
    stack: list[tuple[int, dict]] = [(-1, root)]
    anchors: dict[str, object] = {}
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        raise PGOError(f"read workload: {exc}") from exc
    for lineno, raw in enumerate(lines, 1):
        line = strip_comment(raw).rstrip()
        if not line.strip():
            continue
        if "\t" in line[: len(line) - len(line.lstrip(" "))]:
            raise PGOError(f"{path}:{lineno}: tab indentation is not supported")
        indent = len(line) - len(line.lstrip(" "))
        text = line.strip()
        if ":" not in text:
            raise PGOError(f"{path}:{lineno}: expected key: value")
        key, raw_value = text.split(":", 1)
        key = key.strip()
        raw_value = raw_value.strip()
        while indent <= stack[-1][0]:
            stack.pop()
        parent = stack[-1][1]
        if not isinstance(parent, dict):
            raise PGOError(f"{path}:{lineno}: parent is not a mapping")
        if not raw_value or raw_value.startswith("&"):
            value: dict = {}
            parent[key] = value
            if raw_value.startswith("&"):
                anchor = raw_value[1:].split()[0]
                anchors[anchor] = value
            stack.append((indent, value))
            continue
        parent[key] = parse_value(raw_value, anchors)
    return root

def require_mapping(value, path: str) -> dict:
    if not isinstance(value, dict):
        raise PGOError(f"{path} must be a mapping")
    return value


def require_number(value, path: str) -> float:
    if not isinstance(value, (int, float)) or isinstance(value, bool):
        raise PGOError(f"{path} must be a number")
    try:
        numeric = float(value)
    except (OverflowError, ValueError) as exc:
        raise PGOError(f"{path} must be a finite number") from exc
    if not math.isfinite(numeric):
        raise PGOError(f"{path} must be a finite number")
    return numeric


def require_string(value, path: str) -> str:
    if not isinstance(value, str) or not value:
        raise PGOError(f"{path} must be a non-empty string")
    return value


def parse_duration_seconds(raw: str, path: str) -> int:
    value = require_string(raw, path)
    match = re.fullmatch(r"(\d+)([smh]?)", value)
    if not match:
        raise PGOError(f"{path} must be a duration like 600s")
    amount = int(match.group(1))
    unit = match.group(2) or "s"
    multiplier = {"s": 1, "m": 60, "h": 3600}[unit]
    return amount * multiplier


def validate_workload(path: Path, service: str) -> dict:
    workload = parse_yaml(path)
    unknown = [key for key in workload if key not in TOP_FIELDS]
    if unknown:
        raise PGOError(f"unknown field: workload.{unknown[0]}")
    for field in ("schemaVersion", "name", "service", "duration", "traffic", "drivers"):
        if field not in workload:
            raise PGOError(f"missing workload.{field}")
    if workload["schemaVersion"] != 1:
        raise PGOError("schemaVersion must be 1")
    require_string(workload["name"], "workload.name")
    workload_service = require_string(workload["service"], "workload.service")
    if workload_service != service:
        raise PGOError(f"workload.service mismatch: {workload_service} != {service}")
    parse_duration_seconds(workload["duration"], "workload.duration")

    traffic = require_mapping(workload["traffic"], "workload.traffic")
    if not traffic:
        raise PGOError("workload.traffic must not be empty")
    total = 0.0
    for key, value in traffic.items():
        require_string(key, f"workload.traffic key {key}")
        ratio = require_number(value, f"workload.traffic.{key}")
        if ratio < 0:
            raise PGOError(f"workload.traffic.{key} must be non-negative")
        total += ratio
    if abs(total - 100.0) > 0.000001:
        raise PGOError(f"traffic ratios sum must be 100, got {total:g}")

    drivers = require_mapping(workload["drivers"], "workload.drivers")
    extra_drivers = [key for key in drivers if key not in traffic]
    if extra_drivers:
        raise PGOError(f"driver without traffic key: {extra_drivers[0]}")
    for key in traffic:
        if key not in drivers:
            raise PGOError(f"missing driver for traffic key: {key}")
        require_mapping(drivers[key], f"workload.drivers.{key}")
    return workload


def go_version(repo_root: Path) -> str:
    gomod = repo_root / "go.mod"
    toolchain = None
    go_line = None
    for line in gomod.read_text(encoding="utf-8").splitlines():
        if line.startswith("toolchain "):
            toolchain = line.split()[1]
        elif line.startswith("go "):
            go_line = "go" + line.split()[1]
    if toolchain:
        return toolchain
    if go_line:
        return go_line
    raise PGOError("go.mod missing go/toolchain version")


def git_sha(repo_root: Path) -> str:
    result = subprocess.run(
        ["git", "rev-parse", "HEAD"],
        cwd=repo_root,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return result.stdout.strip()


def comparison_from_json(path: Path) -> dict:
    data = load_json_strict(path, "comparison")
    if not isinstance(data, dict):
        raise PGOError("comparison json must be an object")
    result = {}
    for field in COLLECT_COMPARISON_FIELDS:
        if field not in data:
            raise PGOError(f"comparison json missing {field}")
        result[field] = require_number(data[field], f"comparison.{field}")
    return result


def traffic_mix(workload: dict) -> dict:
    return {key: float(value) / 100.0 for key, value in workload["traffic"].items()}


def comparison_for_meta(comparison: dict) -> dict:
    return {field: comparison[field] for field in COMPARISON_FIELDS}


def write_report(path: Path, service: str, main: str, workload: dict, comparison: dict, result: str, reasons: list[str]) -> None:
    lines = [
        "# PGO Compare Report",
        "",
        f"- service: {service}",
        f"- mainPackage: {main}",
        f"- workload: {workload['name']}",
        f"- verdict: {result}",
        "",
        "## Comparison",
        "",
    ]
    for field in COLLECT_COMPARISON_FIELDS:
        lines.append(f"- {field}: {comparison[field]:g}")
    if reasons:
        lines.extend(["", "## Rejection Reasons", ""])
        for reason in reasons:
            lines.append(f"- {reason}")
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def build_meta(args, repo_root: Path, workload: dict, comparison: dict, generated_at: str, provenance: dict) -> dict:
    return {
        "schemaVersion": 1,
        "service": args.service,
        "mainPackage": args.main,
        "generatedAt": generated_at,
        "sourceGitSha": git_sha(repo_root),
        "goVersion": go_version(repo_root),
        "profileDurationSeconds": parse_duration_seconds(args.duration, "--duration"),
        **provenance,
        "workloadName": workload["name"],
        "workloadDescription": f"{workload['name']} workload generated from {args.source} source",
        "trafficMix": traffic_mix(workload),
        "comparison": comparison_for_meta(comparison),
        "acceptedBy": ACCEPTED_BY,
        "expiresAfterDays": MAX_EXPIRES_AFTER_DAYS,
    }


def write_json(path: Path, data: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(data, ensure_ascii=False, indent=2, allow_nan=False) + "\n",
        encoding="utf-8",
    )


def validate_output_contract(output: Path, meta_path: Path, artifact_dir: Path) -> None:
    required = [
        output,
        meta_path,
        artifact_dir / "pgo-compare-report.md",
        artifact_dir / "pprof-before.pb.gz",
        artifact_dir / "pprof-after.pb.gz",
        artifact_dir / "bench-before.txt",
        artifact_dir / "bench-after.txt",
    ]
    missing = [str(path) for path in required if not path.is_file()]
    if missing:
        raise PGOError("output contract missing: " + ", ".join(missing))


def check_driver_path(repo_root: Path, raw_path: str, label: str) -> Path:
    candidate = Path(raw_path)
    if not candidate.is_absolute():
        candidate = repo_root / candidate
    if not candidate.exists():
        raise PGOError(f"driver {label} missing: {raw_path}")
    return candidate


def invoke_driver(repo_root: Path, key: str, driver: dict, env: dict[str, str]) -> None:
    tool = require_string(driver.get("tool"), f"workload.drivers.{key}.tool")
    if tool == "builtin":
        return
    if tool == "mock":
        fixture = require_string(driver.get("fixture"), f"workload.drivers.{key}.fixture")
        check_driver_path(repo_root, fixture, key)
        return
    if tool == "curl-script":
        script = require_string(driver.get("path"), f"workload.drivers.{key}.path")
        path = check_driver_path(repo_root, script, key)
        if not os.access(path, os.X_OK):
            raise PGOError(f"driver {key} is not executable: {script}")
        subprocess.run([str(path)], cwd=repo_root, env=env, check=True)
        return
    path = check_driver_path(repo_root, tool, key)
    if not os.access(path, os.X_OK):
        raise PGOError(f"driver {key} is not executable: {tool}")
    command = [str(path)]
    if "args" in driver:
        command.extend(shlex.split(str(driver["args"])))
    subprocess.run(command, cwd=repo_root, env=env, check=True)


def run_collection(
    args,
    repo_root: Path,
    workload: dict,
    candidate_profile: Path,
    collection_binary: Path,
    comparison_json: Path,
    artifact_dir: Path,
) -> None:
    artifact_dir.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env.update(
        {
            "PGO_SERVICE": args.service,
            "PGO_MAIN": args.main,
            "PGO_SOURCE": args.source,
            "PGO_DURATION_SECONDS": str(parse_duration_seconds(args.duration, "--duration")),
            "PGO_WORKLOAD": str(Path(args.workload).resolve()),
            "PGO_OUTPUT_PATH": str(Path(args.output).resolve()),
            "PGO_CANDIDATE_PROFILE": str(candidate_profile),
            "PGO_PROFILE_BINARY": str(collection_binary),
            "PGO_COMPARISON_JSON": str(comparison_json),
            "PGO_ARTIFACT_DIR": str(artifact_dir),
        }
    )
    if args.collect_cmd:
        subprocess.run(shlex.split(args.collect_cmd), cwd=repo_root, env=env, check=True)
        return
    for key in workload["traffic"]:
        invoke_driver(repo_root, key, workload["drivers"][key], env)
    if not candidate_profile.is_file() or not comparison_json.is_file():
        raise PGOError(
            "real collection did not produce candidate profile and comparison json; "
            "wire workload drivers or pass --collect-cmd"
        )


def parse_args(argv: list[str]):
    parser = argparse.ArgumentParser(description="Generate and validate PGO profiles")
    parser.add_argument("--service", required=True)
    parser.add_argument("--main", required=True)
    parser.add_argument("--duration", required=True)
    parser.add_argument("--workload", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--source", choices=("live", "synthetic"), default="synthetic")
    parser.add_argument("--collect-cmd")
    return parser.parse_args(argv)


def generate(argv: list[str]) -> int:
    repo_root = Path(os.environ["PGO_REPO_ROOT"])
    args = parse_args(argv)
    duration_seconds = parse_duration_seconds(args.duration, "--duration")
    if duration_seconds < 600:
        raise PGOError("대표성 미달: --duration must be at least 600s")
    main_path = Path(args.main)
    if not main_path.is_absolute():
        main_path = repo_root / main_path
    if not main_path.is_dir() or not any(main_path.glob("*.go")):
        raise PGOError(f"main package does not exist: {args.main}")
    workload = validate_workload(Path(args.workload), args.service)
    generated_at = dt.datetime.now().astimezone().isoformat(timespec="seconds")
    artifact_date = os.environ.get("PGO_ARTIFACT_DATE") or generated_at[:10]
    artifact_root = Path(os.environ.get("PGO_ARTIFACT_ROOT", "artifacts/perf/pgo"))
    if not artifact_root.is_absolute():
        artifact_root = repo_root / artifact_root
    artifact_dir = artifact_root / args.service / artifact_date
    output = Path(args.output)
    if not output.is_absolute():
        output = repo_root / output
    meta_path = Path(str(output) + ".meta.json")

    with tempfile.TemporaryDirectory(prefix="pgo-generate-") as tmp:
        tmp_dir = Path(tmp)
        candidate_profile = tmp_dir / "candidate.pgo"
        collection_binary = tmp_dir / Path(args.main).name
        comparison_json = tmp_dir / "comparison.json"
        run_collection(
            args,
            repo_root,
            workload,
            candidate_profile,
            collection_binary,
            comparison_json,
            artifact_dir,
        )
        if not candidate_profile.is_file():
            raise PGOError(f"collector did not create candidate profile: {candidate_profile}")
        comparison = comparison_from_json(comparison_json)
        result, reasons = verdict(comparison)
        report = artifact_dir / "pgo-compare-report.md"
        write_report(report, args.service, args.main, workload, comparison, result, reasons)
        if result == "REJECTED":
            print("REJECTED: " + "; ".join(reasons), file=sys.stderr)
            return 2

        required_artifacts = [
            artifact_dir / "pgo-compare-report.md",
            artifact_dir / "pprof-before.pb.gz",
            artifact_dir / "pprof-after.pb.gz",
            artifact_dir / "bench-before.txt",
            artifact_dir / "bench-after.txt",
        ]
        missing_artifacts = [str(path) for path in required_artifacts if not path.is_file()]
        if missing_artifacts:
            raise PGOError("output contract missing: " + ", ".join(missing_artifacts))

        provenance = collection_provenance(repo_root, args.main, candidate_profile, collection_binary)
        meta = build_meta(args, repo_root, workload, comparison, generated_at, provenance)
        tmp_meta = tmp_dir / "default.pgo.meta.json"
        write_json(tmp_meta, meta)
        validate_profile(candidate_profile, tmp_meta)
        output.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(candidate_profile, output)
        shutil.copyfile(tmp_meta, meta_path)
        validate_output_contract(output, meta_path, artifact_dir)
        print(f"ACCEPTED: wrote {output}")
        print(f"report: {report}")
    return 0


def main(argv: list[str]) -> int:
    if len(argv) == 2 and argv[0] == "validate-meta":
        validate_meta(Path(argv[1]))
        print(f"ok: {argv[1]}")
        return 0
    if argv and argv[0] == "validate-meta":
        raise PGOError("usage: generate.sh validate-meta <file>")
    if len(argv) == 3 and argv[0] == "validate-profile":
        validate_profile(Path(argv[1]), Path(argv[2]))
        print(f"ok: {argv[1]}")
        return 0
    if argv and argv[0] == "validate-profile":
        raise PGOError("usage: generate.sh validate-profile <profile> <meta>")
    return generate(argv)


try:
    raise SystemExit(main(sys.argv[1:]))
except PGOError as exc:
    print(f"ERROR: {exc}", file=sys.stderr)
    raise SystemExit(1)
except subprocess.CalledProcessError as exc:
    print(f"ERROR: command failed with exit {exc.returncode}: {exc.cmd}", file=sys.stderr)
    raise SystemExit(exc.returncode or 1)
PY
