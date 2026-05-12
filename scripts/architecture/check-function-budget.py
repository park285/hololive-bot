#!/usr/bin/env python3
from __future__ import annotations

import argparse
import os
import re
import sys
from dataclasses import dataclass
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
    "coverage",
    "dist",
    "logs",
    "node_modules",
    "target",
    "vendor",
}

FUNC_RE = re.compile(r"^func\s+(?:\([^)]*\)\s*)?([A-Za-z_][A-Za-z0-9_]*)\s*\(")
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

    def over_default(self) -> bool:
        return (
            self.lines > DEFAULT_MAX_FUNCTION_LINES
            or self.complexity > DEFAULT_MAX_COGNITIVE_COMPLEXITY
            or self.nesting > DEFAULT_MAX_NESTING_DEPTH
        )


@dataclass(frozen=True)
class FunctionBudget:
    max_lines: int
    max_complexity: int
    max_nesting: int


def iter_go_files(root: Path) -> list[Path]:
    result: list[Path] = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = [name for name in dirnames if name not in EXCLUDED_DIR_NAMES and not name.startswith(".")]
        for filename in filenames:
            if not filename.endswith(".go") or filename.endswith("_test.go"):
                continue
            result.append(Path(dirpath) / filename)
    return sorted(result)


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
            for control in CONTROL_RE.finditer(code):
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


def load_budgets(path: Path) -> dict[str, FunctionBudget]:
    budgets: dict[str, FunctionBudget] = {}
    if not path.exists():
        return budgets
    for lineno, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        parts = [part.strip() for part in line.split(":")]
        if len(parts) != 6 or not all(part for part in parts):
            raise SystemExit(f"invalid budget line {path}:{lineno}: {raw}")
        rel, start_line, name, max_lines, max_complexity, max_nesting = parts
        if not (start_line.isdigit() and max_lines.isdigit() and max_complexity.isdigit() and max_nesting.isdigit()):
            raise SystemExit(f"invalid numeric budget {path}:{lineno}: {raw}")
        budgets[f"{rel}:{start_line}:{name}"] = FunctionBudget(int(max_lines), int(max_complexity), int(max_nesting))
    return budgets


def write_baseline(path: Path, metrics: list[FunctionMetric]) -> None:
    over = sorted((metric for metric in metrics if metric.over_default()), key=lambda item: item.key)
    lines = [
        "# Go function budget baseline.",
        "# Format: path:start_line:function:max_lines:max_complexity:max_nesting",
        "# Defaults match Iris strict gates: lines=60, cognitive_complexity=8, nesting=5.",
        "# Existing entries are debt ceilings; new functions must stay within defaults.",
    ]
    lines.extend(f"{m.path}:{m.line}:{m.name}:{m.lines}:{m.complexity}:{m.nesting}" for m in over)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\n".join(lines) + "\n", encoding="utf-8")


def check(metrics: list[FunctionMetric], budgets: dict[str, FunctionBudget]) -> list[str]:
    seen = {metric.key for metric in metrics}
    violations: list[str] = []
    for key in sorted(set(budgets) - seen):
        violations.append(f"stale-budget:{key}")

    for metric in metrics:
        budget = budgets.get(metric.key)
        if budget is None:
            if metric.over_default():
                violations.append(
                    f"new-over-budget:{metric.path}:{metric.line}:{metric.name}:"
                    f"lines={metric.lines}>{DEFAULT_MAX_FUNCTION_LINES},"
                    f"complexity={metric.complexity}>{DEFAULT_MAX_COGNITIVE_COMPLEXITY},"
                    f"nesting={metric.nesting}>{DEFAULT_MAX_NESTING_DEPTH}"
                )
            continue

        if metric.lines > budget.max_lines:
            violations.append(f"lines-increased:{metric.path}:{metric.line}:{metric.name}:{metric.lines}>{budget.max_lines}")
        if metric.complexity > budget.max_complexity:
            violations.append(
                f"complexity-increased:{metric.path}:{metric.line}:{metric.name}:{metric.complexity}>{budget.max_complexity}"
            )
        if metric.nesting > budget.max_nesting:
            violations.append(f"nesting-increased:{metric.path}:{metric.line}:{metric.name}:{metric.nesting}>{budget.max_nesting}")
    return violations


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=".")
    parser.add_argument("--baseline", default="docs/architecture/go-function-budget-baseline.txt")
    parser.add_argument("--write-baseline", action="store_true")
    args = parser.parse_args()

    root = Path(args.root).resolve()
    baseline = (root / args.baseline).resolve()
    metrics = scan_repo(root)

    if args.write_baseline:
        write_baseline(baseline, metrics)
        print(f"Wrote Go function budget baseline: {baseline}")
        return 0

    budgets = load_budgets(baseline)
    violations = check(metrics, budgets)
    if violations:
        print("FAIL: Go function budget violations detected", file=sys.stderr)
        for violation in violations:
            print(f" - {violation}", file=sys.stderr)
        print(f"\nbaseline file: {baseline}", file=sys.stderr)
        return 1

    print(
        "OK: Go function budgets are within limits "
        f"(defaults: lines<={DEFAULT_MAX_FUNCTION_LINES}, "
        f"complexity<={DEFAULT_MAX_COGNITIVE_COMPLEXITY}, nesting<={DEFAULT_MAX_NESTING_DEPTH}; "
        f"baseline entries={len(budgets)})"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
