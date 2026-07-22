#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
export PGO_REPO_ROOT="${REPO_ROOT}"
cd "${REPO_ROOT}"

python3 -B - "$@" <<'PY'
import argparse
import datetime as dt
import json
import math
import os
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(os.environ["PGO_REPO_ROOT"]) / "scripts/perf/pgo"))
from compare_numeric import CompareNumericError, collect_live_metrics, parse_bench_ns

LIVE_FIELDS = ("cpuPercentDelta", "p95LatencyDelta", "p99LatencyDelta", "rssDelta")
COLLECT_FIELDS = (
    "cpuPercentDelta",
    "p95LatencyDelta",
    "p99LatencyDelta",
    "rssDelta",
    "binarySizeDelta",
    "hotBenchmarkPercentDelta",
)
LOAD_TOOL_NOTE = "측정 불가: 부하 도구 P5 선행 (라이브 부하 없이 CPU/p95/p99/RSS 측정 불가)"
COLLECTOR_NOTE = (
    "이 출력은 generate.sh collector 입력으로 사용 불가입니다 — comparison.json은 live 지표를 "
    "null로 둘 수 있으나 generate.sh는 전 필드 숫자를 요구합니다. live 지표 측정 후 전체 파이프라인 전용입니다."
)
DEFAULT_BENCHTIME = "100ms"
DEFAULT_MIN_COUNT = 6
HOT_BENCH_MAX_REGRESSION_PERCENT = 3.0
CPU_MAX_REGRESSION_PERCENT = 3.0
P95_MAX_REGRESSION_PERCENT = 3.0

EXIT_ADOPT = 0
EXIT_REJECT = 2
EXIT_HELD = 3

class CompareError(Exception):
    pass

def run(cmd, *, cwd=None, env=None):
    return subprocess.run(
        cmd,
        cwd=cwd,
        env=env,
        check=True,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )


def module_prefix(main: str) -> str:
    rel = main.lstrip("./")
    parts = rel.split("/")
    cmd_index = None
    for idx, part in enumerate(parts):
        if part == "cmd":
            cmd_index = idx
            break
    if cmd_index is None:
        return "./" + "/".join(parts[:-1])
    return "./" + "/".join(parts[:cmd_index])


def parse_perf_budget(path: Path) -> tuple[list[dict], int]:
    if not path.is_file():
        raise CompareError(f"perf-budget missing: {path}")
    benches = []
    min_count = DEFAULT_MIN_COUNT
    section = None
    for raw in path.read_text(encoding="utf-8").splitlines():
        if raw and not raw.startswith(" ") and raw.rstrip().endswith(":"):
            section = raw.rstrip()[:-1]
            continue
        if section == "benchmarks":
            match = re.match(r"\s+(\w+):\s*\{(.*)\}\s*$", raw)
            if not match:
                continue
            name = match.group(1)
            body = match.group(2)
            fields = {}
            for item in body.split(","):
                if ":" not in item:
                    continue
                key, value = item.split(":", 1)
                fields[key.strip()] = value.strip()
            benches.append(
                {
                    "name": name,
                    "package": fields.get("package", ""),
                    "class": fields.get("class", ""),
                    "gate": fields.get("gate", ""),
                }
            )
        elif section == "settings":
            match = re.match(r"\s+min_count:\s*(\d+)\s*$", raw)
            if match:
                min_count = int(match.group(1))
    if min_count <= 0:
        min_count = DEFAULT_MIN_COUNT
    return benches, min_count


def load_hotpaths(profile: Path) -> list[str]:
    globs_file = Path(str(profile) + ".hotpaths")
    if not globs_file.is_file():
        return []
    prefixes = []
    for line in globs_file.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line:
            continue
        prefixes.append(line.rstrip("*").rstrip("/"))
    return prefixes


def package_matches_globs(package: str, glob_prefixes: list[str]) -> bool:
    pkg = package.lstrip("./").rstrip("/")
    for prefix in glob_prefixes:
        if pkg == prefix or pkg.startswith(prefix + "/"):
            return True
    return False


def select_hot_benches(budget: list[dict], main: str, glob_prefixes: list[str]) -> list[dict]:
    prefix = module_prefix(main)
    selected = []
    for bench in budget:
        if bench["gate"] != "pr":
            continue
        if bench["class"] not in ("critical", "hotpath"):
            continue
        pkg = bench["package"]
        by_prefix = pkg == prefix or pkg.startswith(prefix + "/")
        by_glob = package_matches_globs(pkg, glob_prefixes)
        if by_prefix or by_glob:
            selected.append(bench)
    return selected


def default_build(repo_root: Path, main: str, mode: str, profile: Path, output: Path) -> None:
    module = repo_root / module_prefix(main).lstrip("./")
    pkg = "./" + "/".join(main.lstrip("./").split("/")[len(module_prefix(main).lstrip("./").split("/")):])
    pgo_arg = "off" if mode == "off" else str(profile)
    env = os.environ.copy()
    env["CGO_ENABLED"] = "0"
    run(
        ["go", "build", "-tags", "sonic", "-trimpath", f"-pgo={pgo_arg}", "-o", str(output), pkg],
        cwd=module,
        env=env,
    )


def run_build(args, repo_root: Path, mode: str, profile: Path, output: Path) -> int:
    if args.build_cmd:
        env = os.environ.copy()
        env["PGO_BUILD_MODE"] = mode
        env["PGO_BUILD_OUTPUT"] = str(output)
        env["PGO_BUILD_PROFILE"] = str(profile)
        env["PGO_MAIN"] = args.main
        run(["bash", "-c", args.build_cmd], cwd=repo_root, env=env)
    else:
        default_build(repo_root, args.main, mode, profile, output)
    if not output.is_file():
        raise CompareError(f"build did not produce binary ({mode}): {output}")
    return output.stat().st_size


def bench_settings(min_count: int) -> tuple[str, str]:
    benchtime = os.environ.get("PGO_BENCHTIME", DEFAULT_BENCHTIME)
    if benchtime.startswith("-"):
        raise CompareError(
            "PGO_BENCHTIME must be a pure value like '100ms' or '2s', not a flag form"
        )
    count_raw = os.environ.get("PGO_BENCHCOUNT")
    if count_raw is None:
        count = str(min_count)
    else:
        if count_raw.startswith("-") or not count_raw.isdigit():
            raise CompareError(
                "PGO_BENCHCOUNT must be a pure integer value like '4', not a flag form"
            )
        count = count_raw
    return benchtime, count


def default_bench(repo_root: Path, package: str, name: str, mode: str, profile: Path,
                  benchtime: str, count: str) -> str:
    pgo_arg = "off" if mode == "off" else str(profile)
    pkg_path = repo_root / package.lstrip("./")
    bench_re = f"^{name}$"
    result = run(
        [
            "go",
            "test",
            "-tags",
            "sonic",
            "-run",
            "^$",
            f"-bench={bench_re}",
            f"-pgo={pgo_arg}",
            f"-benchtime={benchtime}",
            f"-count={count}",
            ".",
        ],
        cwd=pkg_path,
    )
    return result.stdout


def run_bench(args, repo_root: Path, package: str, name: str, mode: str, profile: Path, output: Path,
              benchtime: str, count: str) -> str:
    if args.bench_cmd:
        env = os.environ.copy()
        env["PGO_BENCH_MODE"] = mode
        env["PGO_BENCH_OUTPUT"] = str(output)
        env["PGO_BENCH_NAME"] = name
        env["PGO_BENCH_PACKAGE"] = package
        env["PGO_BENCH_PROFILE"] = str(profile)
        env["PGO_BENCH_BENCHTIME"] = benchtime
        env["PGO_BENCH_COUNT"] = count
        run(["bash", "-c", args.bench_cmd], cwd=repo_root, env=env)
        text = output.read_text(encoding="utf-8")
    else:
        text = default_bench(repo_root, package, name, mode, profile, benchtime, count)
        output.write_text(text, encoding="utf-8")
    return text


def collect_hot_bench(args, repo_root: Path, benches: list[dict], profile: Path, artifact_dir: Path,
                      benchtime: str, count: str):
    if not benches:
        return None, []
    deltas = []
    rows = []
    bench_before = []
    bench_after = []
    for bench in benches:
        name = bench["name"]
        pkg = bench["package"]
        off_text = run_bench(args, repo_root, pkg, name, "off", profile, artifact_dir / f"bench-{name}-off.txt", benchtime, count)
        on_text = run_bench(args, repo_root, pkg, name, "on", profile, artifact_dir / f"bench-{name}-on.txt", benchtime, count)
        bench_before.append(f"# {name} (pgo=off)\n{off_text.strip()}")
        bench_after.append(f"# {name} (pgo=on)\n{on_text.strip()}")
        off_ns = parse_bench_ns(off_text, name)
        on_ns = parse_bench_ns(on_text, name)
        if off_ns is None or on_ns is None or off_ns == 0:
            rows.append({"name": name, "offNs": off_ns, "onNs": on_ns, "improvementPercent": None})
            continue
        improvement = (off_ns - on_ns) / off_ns * 100.0
        if not math.isfinite(improvement):
            raise CompareError(f"benchmark {name} improvement delta overflowed")
        deltas.append(improvement)
        rows.append({"name": name, "offNs": off_ns, "onNs": on_ns, "improvementPercent": improvement})
    (artifact_dir / "bench-before.txt").write_text("\n\n".join(bench_before) + "\n", encoding="utf-8")
    (artifact_dir / "bench-after.txt").write_text("\n\n".join(bench_after) + "\n", encoding="utf-8")
    overall = min(deltas) if deltas else None
    return overall, rows


def verdict(comparison: dict) -> tuple[str, list[str], int]:
    reasons: list[str] = []

    binary = comparison["binarySizeDelta"]
    hot_bench = comparison["hotBenchmarkPercentDelta"]
    p99 = comparison["p99LatencyDelta"]
    p95 = comparison["p95LatencyDelta"]
    rss = comparison["rssDelta"]
    cpu = comparison["cpuPercentDelta"]

    if binary is None:
        raise CompareError("binarySizeDelta is None; binary size is always locally measurable")

    if binary > 5.0:
        reasons.append("binary size delta > +5%")
    if hot_bench is not None and hot_bench < -HOT_BENCH_MAX_REGRESSION_PERCENT:
        reasons.append("hot benchmark regression > 3%")
    if p99 is not None and p99 > 0:
        reasons.append("p99 latency regression > 0%")
    if p95 is not None and p95 > P95_MAX_REGRESSION_PERCENT:
        reasons.append("p95 latency regression > 3%")
    if cpu is not None and cpu > CPU_MAX_REGRESSION_PERCENT:
        reasons.append("CPU delta > +3%")
    if rss is not None and rss > 3.0:
        reasons.append("RSS delta > +3%")
    if reasons:
        return "REJECTED", reasons, EXIT_REJECT

    cpu_improved = cpu is not None and cpu <= -2.0
    latency_improved = (p95 is not None and p95 < 0) or (p99 is not None and p99 < 0)
    hot_bench_improved = hot_bench is not None and hot_bench >= 3.0
    adoption_signal = cpu_improved or latency_improved or hot_bench_improved

    # 07 §채택 기준: adoption 신호 + p99/RSS 모두 측정·통과해야 ACCEPTED 단정 가능.
    rejection_metrics_measured = p99 is not None and rss is not None

    if adoption_signal and rejection_metrics_measured:
        return "ACCEPTED", [], EXIT_ADOPT

    if adoption_signal and not rejection_metrics_measured:
        return (
            "HELD",
            [
                "adoption signal present; rejection metrics unmeasured (p99/RSS N/A) — "
                f"{LOAD_TOOL_NOTE}",
            ],
            EXIT_HELD,
        )

    if not rejection_metrics_measured:
        return (
            "HELD",
            [
                "no adoption signal and rejection metrics unmeasured (p99/RSS N/A) — "
                f"{LOAD_TOOL_NOTE}",
            ],
            EXIT_HELD,
        )

    return "REJECTED", ["no adoption criterion met"], EXIT_REJECT


def fmt_delta(value) -> str:
    if value is None:
        return "N/A"
    return f"{value:+.3f}%"


def write_report(path: Path, args, benches: list[dict], bench_rows: list[dict],
                 off_size: int, on_size: int, comparison: dict,
                 result: str, reasons: list[str]) -> None:
    lines = [
        "# PGO Compare Report",
        "",
        f"- service: {args.service}",
        f"- mainPackage: {args.main}",
        f"- profile: {args.profile}",
        f"- workload: {args.workload}",
        f"- verdict: {result}",
        "",
        "## Binary Size",
        "",
        f"- pgo=off: {off_size} bytes",
        f"- pgo=on:  {on_size} bytes",
        f"- binarySizeDelta: {fmt_delta(comparison['binarySizeDelta'])}",
        "",
        "## Hot Benchmarks (pr-gate critical/hotpath)",
        "",
    ]
    if not benches:
        lines.append("- (none matched this main package in scripts/perf/perf-budget.yaml)")
    else:
        for row in bench_rows:
            off_ns = row["offNs"]
            on_ns = row["onNs"]
            imp = row["improvementPercent"]
            off_s = "N/A" if off_ns is None else f"{off_ns:g} ns/op"
            on_s = "N/A" if on_ns is None else f"{on_ns:g} ns/op"
            imp_s = "N/A" if imp is None else f"{imp:+.3f}%"
            lines.append(f"- {row['name']}: off={off_s} on={on_s} improvement={imp_s}")
        lines.append(f"- hotBenchmarkPercentDelta (worst): {fmt_delta(comparison['hotBenchmarkPercentDelta'])}")
    lines.extend(
        [
            "",
            "## Live Load Metrics",
            "",
            f"- {LOAD_TOOL_NOTE}",
            f"- cpuPercentDelta: {fmt_delta(comparison['cpuPercentDelta'])}",
            f"- p95LatencyDelta: {fmt_delta(comparison['p95LatencyDelta'])}",
            f"- p99LatencyDelta: {fmt_delta(comparison['p99LatencyDelta'])}",
            f"- rssDelta: {fmt_delta(comparison['rssDelta'])}",
            "",
            "## Verdict",
            "",
            f"- result: {result}",
        ]
    )
    label = {"ACCEPTED": "채택", "REJECTED": "거부", "HELD": "판정 보류(insufficient data)"}[result]
    lines.append(f"- 판정: {label}")
    if reasons:
        lines.append("")
        heading = "## Hold Reasons" if result == "HELD" else "## Rejection Reasons"
        lines.append(heading)
        lines.append("")
        for reason in reasons:
            lines.append(f"- {reason}")
    lines.extend(["", "## Output Compatibility", "", f"- {COLLECTOR_NOTE}"])
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def write_json(path: Path, comparison: dict, result: str, reasons: list[str], bench_rows: list[dict]) -> None:
    payload = dict(comparison)
    payload["generator"] = "compare-pgo"
    payload["verdict"] = result
    payload["reasons"] = reasons
    payload["hotBenchmarks"] = bench_rows
    payload["liveMetricsNote"] = LOAD_TOOL_NOTE
    payload["collectorCompatibility"] = COLLECTOR_NOTE
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(payload, ensure_ascii=False, indent=2, allow_nan=False) + "\n",
        encoding="utf-8",
    )


def parse_args(argv: list[str]):
    parser = argparse.ArgumentParser(
        description="Compare PGO on/off and emit verdict",
        epilog=COLLECTOR_NOTE,
    )
    parser.add_argument("--service", required=True)
    parser.add_argument("--main", required=True)
    parser.add_argument("--profile", required=True)
    parser.add_argument("--workload", required=True)
    parser.add_argument("--output-dir")
    parser.add_argument("--build-cmd")
    parser.add_argument("--bench-cmd")
    parser.add_argument("--live-cmd")
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    repo_root = Path(os.environ["PGO_REPO_ROOT"])
    args = parse_args(argv)

    profile = Path(args.profile)
    if not profile.is_absolute():
        profile = repo_root / profile
    if not args.build_cmd and not profile.is_file():
        raise CompareError(f"profile missing: {profile}")

    if args.output_dir:
        output_dir = Path(args.output_dir)
        if not output_dir.is_absolute():
            output_dir = repo_root / output_dir
    else:
        date = os.environ.get("PGO_ARTIFACT_DATE") or dt.date.today().isoformat()
        output_dir = repo_root / "artifacts" / "perf" / "pgo" / args.service / date
    output_dir.mkdir(parents=True, exist_ok=True)

    budget_path = os.environ.get("PGO_PERF_BUDGET")
    if budget_path:
        budget_file = Path(budget_path)
        if not budget_file.is_absolute():
            budget_file = repo_root / budget_file
    else:
        budget_file = repo_root / "scripts" / "perf" / "perf-budget.yaml"
    budget, min_count = parse_perf_budget(budget_file)
    glob_prefixes = load_hotpaths(profile)
    benches = select_hot_benches(budget, args.main, glob_prefixes)
    benchtime, count = bench_settings(min_count)

    with tempfile.TemporaryDirectory(prefix="pgo-compare-") as tmp:
        tmp_dir = Path(tmp)
        off_bin = tmp_dir / "bin-off"
        on_bin = tmp_dir / "bin-on"
        off_size = run_build(args, repo_root, "off", profile, off_bin)
        on_size = run_build(args, repo_root, "on", profile, on_bin)
        binary_delta = (on_size - off_size) / off_size * 100.0 if off_size else None
        hot_bench_delta, bench_rows = collect_hot_bench(
            args, repo_root, benches, profile, output_dir, benchtime, count
        )

    live = collect_live_metrics(args.live_cmd, repo_root, LIVE_FIELDS)
    comparison = {
        "cpuPercentDelta": live["cpuPercentDelta"],
        "p95LatencyDelta": live["p95LatencyDelta"],
        "p99LatencyDelta": live["p99LatencyDelta"],
        "rssDelta": live["rssDelta"],
        "binarySizeDelta": binary_delta,
        "hotBenchmarkPercentDelta": hot_bench_delta,
    }

    result, reasons, exit_code = verdict(comparison)
    report = output_dir / "pgo-compare-report.md"
    write_report(report, args, benches, bench_rows, off_size, on_size, comparison, result, reasons)
    write_json(output_dir / "comparison.json", comparison, result, reasons, bench_rows)

    print(f"{result}: binarySizeDelta={fmt_delta(binary_delta)} hotBench={fmt_delta(hot_bench_delta)}")
    print(f"report: {report}")
    print(f"comparison: {output_dir / 'comparison.json'}")
    if reasons:
        for reason in reasons:
            print(f"  - {reason}", file=sys.stderr)
    return exit_code


try:
    raise SystemExit(main(sys.argv[1:]))
except (CompareError, CompareNumericError) as exc:
    print(f"ERROR: {exc}", file=sys.stderr)
    raise SystemExit(1)
except subprocess.CalledProcessError as exc:
    print(f"ERROR: command failed with exit {exc.returncode}: {exc.cmd}", file=sys.stderr)
    if exc.stdout:
        print(exc.stdout, file=sys.stderr)
    raise SystemExit(exc.returncode or 1)
PY
