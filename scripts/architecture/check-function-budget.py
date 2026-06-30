#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from dataclasses import asdict, dataclass
from pathlib import Path

DEFAULT_MAX_FUNCTION_LINES = 60
DEFAULT_MAX_COGNITIVE_COMPLEXITY = 8
DEFAULT_MAX_NESTING_DEPTH = 5

EXCLUDED_DIR_NAMES = {
    ".git",
    ".worktrees",
    ".tasklists",
    ".runlogs",
    ".codex",
    ".claude",
    ".serena",
    ".gemini",
    "artifacts",
    "benchgate",
    "coverage",
    "dist",
    "logs",
    "node_modules",
    "target",
    "vendor",
}

FUNC_RE = re.compile(r"^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)(?:\[[^\]]+\])?\s*\(")
CONTROL_RE = re.compile(r"\b(if|for|switch|select|case)\b")


@dataclass(frozen=True)
class FunctionMetric:
    path: str
    name: str
    line: int
    lines: int
    complexity: int
    nesting: int

    @property
    def key(self) -> str:
        return f"{self.path}:{self.line}:{self.name}"

    def exceeded_dimensions(self) -> dict[str, dict[str, int]]:
        exceeded: dict[str, dict[str, int]] = {}
        if self.lines > DEFAULT_MAX_FUNCTION_LINES:
            exceeded["lines"] = {"actual": self.lines, "limit": DEFAULT_MAX_FUNCTION_LINES}
        if self.complexity > DEFAULT_MAX_COGNITIVE_COMPLEXITY:
            exceeded["complexity"] = {"actual": self.complexity, "limit": DEFAULT_MAX_COGNITIVE_COMPLEXITY}
        if self.nesting > DEFAULT_MAX_NESTING_DEPTH:
            exceeded["nesting"] = {"actual": self.nesting, "limit": DEFAULT_MAX_NESTING_DEPTH}
        return exceeded

    def over_default(self) -> bool:
        return bool(self.exceeded_dimensions())

    def excess_score(self) -> int:
        return (
            max(0, self.lines - DEFAULT_MAX_FUNCTION_LINES)
            + max(0, self.complexity - DEFAULT_MAX_COGNITIVE_COMPLEXITY) * 10
            + max(0, self.nesting - DEFAULT_MAX_NESTING_DEPTH) * 20
        )


def iter_go_files(root: Path) -> list[Path]:
    try:
        result = subprocess.run(
            [
                "git",
                "-C",
                str(root),
                "ls-files",
                "--cached",
                "--others",
                "--exclude-standard",
                "*.go",
                # benchgate는 shared-go 정본의 vendored perf 게이트 도구(Python 1:1 포팅)라 production 함수 budget 대상이 아님
                ":(exclude)scripts/perf/benchgate/**",
            ],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )
    except (OSError, subprocess.CalledProcessError):
        result = None

    if result is not None:
        return sorted(
            path
            for line in result.stdout.splitlines()
            if line.endswith(".go") and not line.endswith("_test.go")
            for path in [root / line]
            if path.exists()
        )

    result_paths: list[Path] = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [
            name
            for name in dirnames
            if name not in EXCLUDED_DIR_NAMES and not name.startswith(".")
        ]
        for filename in filenames:
            if not filename.endswith(".go") or filename.endswith("_test.go"):
                continue
            result_paths.append(Path(dirpath) / filename)
    return sorted(result_paths)


def strip_line_comment(line: str) -> str:
    in_string = False
    escaped = False
    for index, char in enumerate(line):
        if char == '"' and not escaped:
            in_string = not in_string
        if char == "/" and not in_string and index + 1 < len(line) and line[index + 1] == "/":
            return line[:index]
        escaped = char == "\\" and not escaped
    return line


def scan_file(root: Path, file_path: Path) -> list[FunctionMetric]:
    rel = file_path.relative_to(root).as_posix()
    lines = file_path.read_text(encoding="utf-8", errors="ignore").splitlines()
    metrics: list[FunctionMetric] = []
    index = 0
    while index < len(lines):
        match = FUNC_RE.match(lines[index].strip())
        if not match:
            index += 1
            continue

        name = match.group(1)
        start = index
        brace_depth = 0
        seen_body = False
        max_nesting = 0
        complexity = 0
        cursor = index
        while cursor < len(lines):
            code = strip_line_comment(lines[cursor])
            for _ in CONTROL_RE.finditer(code):
                complexity += 1 + max(0, brace_depth - 1)
            complexity += code.count("&&") + code.count("||")

            for char in code:
                if char == "{":
                    brace_depth += 1
                    seen_body = True
                    max_nesting = max(max_nesting, max(0, brace_depth - 1))
                elif char == "}":
                    brace_depth -= 1

            if seen_body and brace_depth <= 0:
                break
            cursor += 1

        metrics.append(
            FunctionMetric(
                path=rel,
                name=name,
                line=start + 1,
                lines=cursor - start + 1,
                complexity=complexity,
                nesting=max_nesting,
            )
        )
        index = max(cursor + 1, index + 1)
    return metrics


def scan_repo(root: Path) -> list[FunctionMetric]:
    metrics: list[FunctionMetric] = []
    for file_path in iter_go_files(root):
        metrics.extend(scan_file(root, file_path))
    return metrics


def apply_prefix_filter(metrics: list[FunctionMetric], prefixes: list[str]) -> list[FunctionMetric]:
    clean = [prefix for prefix in prefixes if prefix]
    if not clean:
        return metrics
    return [metric for metric in metrics if any(metric.path.startswith(prefix) for prefix in clean)]


def sort_metrics(metrics: list[FunctionMetric], sort_by: str) -> list[FunctionMetric]:
    if sort_by == "score":
        return sorted(metrics, key=lambda item: (-item.excess_score(), item.path, item.line, item.name))
    if sort_by == "lines":
        return sorted(metrics, key=lambda item: (-item.lines, item.path, item.line, item.name))
    if sort_by == "complexity":
        return sorted(metrics, key=lambda item: (-item.complexity, item.path, item.line, item.name))
    if sort_by == "nesting":
        return sorted(metrics, key=lambda item: (-item.nesting, item.path, item.line, item.name))
    return sorted(metrics, key=lambda item: (item.path, item.line, item.name))


def metric_payload(metric: FunctionMetric) -> dict[str, object]:
    return {
        **asdict(metric),
        "key": metric.key,
        "score": metric.excess_score(),
        "exceeded": metric.exceeded_dimensions(),
    }


def print_text_report(metrics: list[FunctionMetric], over: list[FunctionMetric], limit: int | None) -> None:
    print("Go function budget report")
    print(
        "defaults: "
        f"lines<={DEFAULT_MAX_FUNCTION_LINES}, "
        f"complexity<={DEFAULT_MAX_COGNITIVE_COMPLEXITY}, "
        f"nesting<={DEFAULT_MAX_NESTING_DEPTH}"
    )
    print(f"scanned_functions={len(metrics)}")
    print(f"over_budget={len(over)}")
    shown = over[:limit] if limit is not None else over
    for metric in shown:
        exceeded = ",".join(
            f"{name}={values['actual']}>{values['limit']}"
            for name, values in metric.exceeded_dimensions().items()
        )
        print(
            f" - {metric.path}:{metric.line}:{metric.name}:"
            f"lines={metric.lines},complexity={metric.complexity},nesting={metric.nesting},"
            f"score={metric.excess_score()} [{exceeded}]"
        )
    if limit is not None and len(over) > limit:
        print(f"... truncated: {len(over) - limit} more")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=".")
    parser.add_argument("--report-over-budget", action="store_true")
    parser.add_argument("--include-prefix", action="append", default=[])
    parser.add_argument("--output", choices=("text", "json"), default="text")
    parser.add_argument("--sort-by", choices=("path", "score", "lines", "complexity", "nesting"), default="path")
    parser.add_argument("--limit", type=int, default=0)
    args = parser.parse_args()

    root = Path(args.root).resolve()
    metrics = apply_prefix_filter(scan_repo(root), args.include_prefix)
    over = sort_metrics([metric for metric in metrics if metric.over_default()], args.sort_by)
    limit = args.limit if args.limit > 0 else None

    if args.output == "json":
        payload = {
            "defaults": {
                "max_lines": DEFAULT_MAX_FUNCTION_LINES,
                "max_complexity": DEFAULT_MAX_COGNITIVE_COMPLEXITY,
                "max_nesting": DEFAULT_MAX_NESTING_DEPTH,
            },
            "scanned_functions": len(metrics),
            "over_budget": len(over),
            "items": [metric_payload(metric) for metric in (over[:limit] if limit else over)],
        }
        print(json.dumps(payload, ensure_ascii=False, indent=2))
    elif args.report_over_budget:
        print_text_report(metrics, over, limit)

    if args.report_over_budget:
        return 0

    if over:
        print("FAIL: Go function budget violations detected", file=sys.stderr)
        for metric in over:
            exceeded = ",".join(
                f"{name}={values['actual']}>{values['limit']}"
                for name, values in metric.exceeded_dimensions().items()
            )
            print(f" - over-budget:{metric.path}:{metric.line}:{metric.name}:{exceeded}", file=sys.stderr)
        print(
            "\ndefaults: "
            f"lines<={DEFAULT_MAX_FUNCTION_LINES}, "
            f"complexity<={DEFAULT_MAX_COGNITIVE_COMPLEXITY}, "
            f"nesting<={DEFAULT_MAX_NESTING_DEPTH}",
            file=sys.stderr,
        )
        return 1

    if args.output == "text":
        print(
            "OK: Go function budgets are within limits "
            f"(defaults: lines<={DEFAULT_MAX_FUNCTION_LINES}, "
            f"complexity<={DEFAULT_MAX_COGNITIVE_COMPLEXITY}, "
            f"nesting<={DEFAULT_MAX_NESTING_DEPTH})"
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
