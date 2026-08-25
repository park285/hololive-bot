#!/usr/bin/env python3
"""Canonical advisory/hard structure budget analyzer for Hololive."""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from dataclasses import asdict, dataclass
from pathlib import Path, PurePosixPath
from typing import Any


class PolicyError(RuntimeError):
    pass


EXCLUDED_DIR_NAMES = {
    ".git",
    ".worktrees",
    ".tasklists",
    ".runlogs",
    ".codex",
    ".claude",
    ".serena",
    ".gemini",
    ".tmp",
    "artifacts",
    "coverage",
    "dist",
    "logs",
    "node_modules",
    "target",
    "vendor",
}
FUNC_RE = re.compile(
    r"^func\s+(?:\(([^)]*)\)\s*)?([A-Za-z_][A-Za-z0-9_]*)"
    r"(?:\[[^\]]+\])?\s*\("
)
CONTROL_RE = re.compile(r"\b(if|for|switch|select|case)\b")


@dataclass(frozen=True)
class Finding:
    id: str
    rule: str
    path: str
    actual: int
    advisory_limit: int
    hard_limit: int
    level: str
    message: str
    symbol: str | None = None

    def payload(self) -> dict[str, Any]:
        value = asdict(self)
        if self.symbol is None:
            value.pop("symbol")
        return value


@dataclass(frozen=True)
class FunctionMetric:
    path: str
    symbol: str
    lines: int
    complexity: int
    nesting: int


def reject_duplicate_pairs(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    result: dict[str, Any] = {}
    for key, value in pairs:
        if key in result:
            raise PolicyError(f"duplicate policy key: {key}")
        result[key] = value
    return result


def exact_object(value: Any, keys: set[str], label: str) -> dict[str, Any]:
    if not isinstance(value, dict) or set(value) != keys:
        raise PolicyError(f"{label} must contain exactly: {', '.join(sorted(keys))}")
    return value


def budget_pair(value: Any, label: str) -> dict[str, int]:
    pair = exact_object(value, {"advisory", "hard"}, label)
    advisory, hard = pair["advisory"], pair["hard"]
    if (
        not isinstance(advisory, int)
        or isinstance(advisory, bool)
        or not isinstance(hard, int)
        or isinstance(hard, bool)
        or advisory < 1
        or hard < advisory
    ):
        raise PolicyError(f"{label} is invalid")
    return {"advisory": advisory, "hard": hard}


def relative_path(value: Any, label: str) -> str:
    if not isinstance(value, str) or not value or "\x00" in value or "\\" in value:
        raise PolicyError(f"{label} must be a POSIX relative path")
    parsed = PurePosixPath(value)
    if parsed.is_absolute() or any(part in {"", ".", ".."} for part in parsed.parts):
        raise PolicyError(f"{label} escapes the repository: {value}")
    return value


def load_policy(path: Path) -> dict[str, Any]:
    try:
        policy = json.loads(
            path.read_text(encoding="utf-8"), object_pairs_hook=reject_duplicate_pairs
        )
    except (OSError, UnicodeError, json.JSONDecodeError, PolicyError) as exc:
        raise PolicyError(f"read policy {path}: {exc}") from exc
    policy = exact_object(policy, {"schema_version", "file", "functions"}, "policy")
    if policy["schema_version"] != 1:
        raise PolicyError("policy.schema_version must be 1")
    file_policy = exact_object(
        policy["file"], {"default", "threshold_file", "extensions"}, "policy.file"
    )
    budget_pair(file_policy["default"], "policy.file.default")
    relative_path(file_policy["threshold_file"], "policy.file.threshold_file")
    extensions = file_policy["extensions"]
    if (
        not isinstance(extensions, list)
        or not extensions
        or extensions != sorted(set(extensions))
        or any(not isinstance(item, str) or not item.startswith(".") for item in extensions)
    ):
        raise PolicyError("policy.file.extensions must be a unique sorted extension array")
    functions = exact_object(
        policy["functions"], {"lines", "complexity", "nesting"}, "policy.functions"
    )
    for rule, pair in functions.items():
        budget_pair(pair, f"policy.functions.{rule}")
    return policy


def load_thresholds(root: Path, policy: dict[str, Any], override: Path | None) -> dict[str, int]:
    configured: dict[str, int] = {}
    threshold_path = override or root / policy["file"]["threshold_file"]
    if not threshold_path.is_absolute():
        threshold_path = root / threshold_path
    try:
        threshold_real = threshold_path.resolve(strict=True)
        root_real = root.resolve(strict=True)
    except OSError as exc:
        raise PolicyError(f"threshold file is missing: {threshold_path}: {exc}") from exc
    if not threshold_real.is_file() or root_real not in threshold_real.parents:
        raise PolicyError(f"threshold file escapes the repository: {threshold_path}")
    try:
        lines = threshold_real.read_text(encoding="utf-8").splitlines()
    except (OSError, UnicodeError) as exc:
        raise PolicyError(f"read threshold file {threshold_path}: {exc}") from exc
    hard_limit = int(policy["file"]["default"]["hard"])
    for number, raw in enumerate(lines, 1):
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        if line.count(":") != 1:
            raise PolicyError(f"invalid threshold line {number}: {raw}")
        raw_path, raw_limit = (item.strip() for item in line.split(":", 1))
        path = relative_path(raw_path, f"threshold line {number}")
        if path in configured:
            raise PolicyError(f"duplicate threshold path: {path}")
        if not raw_limit.isdigit() or int(raw_limit) < 1 or int(raw_limit) > hard_limit:
            raise PolicyError(f"invalid threshold limit at line {number}: {raw_limit}")
        target = root / path
        try:
            target_real = target.resolve(strict=True)
        except OSError as exc:
            raise PolicyError(f"stale threshold path: {path}: {exc}") from exc
        if not target_real.is_file() or root_real not in target_real.parents:
            raise PolicyError(f"threshold path escapes or is not a file: {path}")
        configured[path] = int(raw_limit)
    return configured


def git_visible_files(root: Path) -> list[str] | None:
    try:
        result = subprocess.run(
            ["git", "-C", str(root), "ls-files", "--cached", "--others", "--exclude-standard"],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
        )
    except (OSError, subprocess.CalledProcessError):
        return None
    return result.stdout.splitlines()


def excluded_path(rel: str) -> bool:
    parts = PurePosixPath(rel).parts
    if any(part in EXCLUDED_DIR_NAMES for part in parts):
        return True
    return (
        rel.endswith(".d.ts")
        or rel.endswith("_test.go")
        or rel.endswith((".test.ts", ".test.tsx", ".test.mjs"))
    )


def iter_source_files(root: Path, extensions: set[str]) -> list[tuple[str, Path]]:
    visible = git_visible_files(root)
    if visible is not None:
        return sorted(
            (rel, root / rel)
            for rel in visible
            if PurePosixPath(rel).suffix in extensions
            and not excluded_path(rel)
            and (root / rel).is_file()
        )
    files: list[tuple[str, Path]] = []
    for dirpath, dirnames, filenames in os.walk(root):
        dirnames[:] = sorted(
            name
            for name in dirnames
            if name not in EXCLUDED_DIR_NAMES and not name.startswith(".")
        )
        directory = Path(dirpath)
        for filename in sorted(filenames):
            path = directory / filename
            rel = path.relative_to(root).as_posix()
            if path.suffix in extensions and not excluded_path(rel) and path.is_file():
                files.append((rel, path))
    return sorted(files)


def mask_non_code(source: str) -> str:
    out: list[str] = []
    index = 0
    while index < len(source):
        char = source[index]
        next_char = source[index + 1] if index + 1 < len(source) else ""
        if char == "/" and next_char == "/":
            while index < len(source) and source[index] != "\n":
                out.append(" ")
                index += 1
            continue
        if char == "/" and next_char == "*":
            out.extend("  ")
            index += 2
            while index < len(source):
                if source[index : index + 2] == "*/":
                    out.extend("  ")
                    index += 2
                    break
                out.append("\n" if source[index] == "\n" else " ")
                index += 1
            continue
        if char in {'"', "'", "`"}:
            quote = char
            out.append(" ")
            index += 1
            while index < len(source):
                if quote != "`" and source[index] == "\\":
                    out.extend("  ")
                    index += 2
                    continue
                current = source[index]
                out.append("\n" if current == "\n" else " ")
                index += 1
                if current == quote:
                    break
            continue
        out.append(char)
        index += 1
    return "".join(out)


def receiver_type(receiver: str | None) -> str | None:
    if receiver is None:
        return None
    fields = receiver.strip().split()
    if not fields:
        return "unknown"
    raw = fields[-1].lstrip("*")
    match = re.match(r"([A-Za-z_][A-Za-z0-9_]*)", raw)
    return match.group(1) if match else raw


def scan_functions(root: Path, files: list[tuple[str, Path]]) -> list[FunctionMetric]:
    metrics: list[FunctionMetric] = []
    for rel, path in files:
        if path.suffix != ".go":
            continue
        try:
            lines = mask_non_code(path.read_text(encoding="utf-8")).splitlines()
        except (OSError, UnicodeError) as exc:
            raise PolicyError(f"read source {rel}: {exc}") from exc
        index = 0
        while index < len(lines):
            match = FUNC_RE.match(lines[index].strip())
            if match is None:
                index += 1
                continue
            receiver = receiver_type(match.group(1))
            name = match.group(2)
            symbol = f"{receiver}.{name}" if receiver else name
            start = index
            brace_depth = 0
            seen_body = False
            max_nesting = 0
            complexity = 0
            cursor = index
            while cursor < len(lines):
                code = lines[cursor]
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
                    symbol=symbol,
                    lines=cursor - start + 1,
                    complexity=complexity,
                    nesting=max_nesting,
                )
            )
            index = max(cursor + 1, index + 1)
    return metrics


def add_finding(
    findings: list[Finding], rule: str, path: str, actual: int, budget: dict[str, int],
    symbol: str | None = None,
) -> None:
    advisory, hard = int(budget["advisory"]), int(budget["hard"])
    if actual <= advisory:
        return
    stable_id = f"{rule}:{path}" + (f":{symbol}" if symbol else "")
    findings.append(
        Finding(
            id=stable_id,
            rule=rule,
            path=path,
            symbol=symbol,
            actual=actual,
            advisory_limit=advisory,
            hard_limit=hard,
            level="hard_ceiling" if actual > hard else "advisory",
            message=f"{rule} actual={actual} advisory={advisory} hard={hard}",
        )
    )


def read_changed_paths(path: Path | None) -> set[str] | None:
    if path is None:
        return None
    try:
        return {item.decode("utf-8") for item in path.read_bytes().split(b"\0") if item}
    except (OSError, UnicodeError) as exc:
        raise PolicyError(f"read changed paths: {exc}") from exc


def emit_text(findings: list[Finding]) -> None:
    for finding in findings[:10]:
        print(f"[{finding.level}] {finding.id}: {finding.message}")
    if len(findings) > 10:
        print(f"... {len(findings) - 10} finding(s) omitted")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path("."))
    parser.add_argument("--policy", type=Path)
    parser.add_argument("--threshold-file", type=Path)
    parser.add_argument("--mode", choices=("advisory", "hard"), required=True)
    parser.add_argument("--format", choices=("text", "json"), default="text")
    parser.add_argument("--component", choices=("all", "files", "functions"), default="all")
    parser.add_argument("--changed-paths-file", type=Path)
    parser.add_argument("--include-prefix", action="append", default=[])
    args = parser.parse_args()
    try:
        root = args.root.resolve(strict=True)
        policy_path = args.policy or root / "scripts" / "architecture" / "structure-budget-policy.json"
        if not policy_path.is_absolute():
            policy_path = root / policy_path
        policy = load_policy(policy_path)
        thresholds = load_thresholds(root, policy, args.threshold_file)
        changed = read_changed_paths(args.changed_paths_file)
        files = iter_source_files(root, set(policy["file"]["extensions"]))
        if args.include_prefix:
            files = [
                item for item in files
                if any(item[0].startswith(prefix) for prefix in args.include_prefix if prefix)
            ]
        findings: list[Finding] = []
        if args.component in {"all", "files"}:
            default = policy["file"]["default"]
            for rel, path in files:
                try:
                    actual = len(path.read_text(encoding="utf-8").splitlines())
                except (OSError, UnicodeError) as exc:
                    raise PolicyError(f"read source {rel}: {exc}") from exc
                budget = {
                    "advisory": thresholds.get(rel, default["advisory"]),
                    "hard": default["hard"],
                }
                add_finding(findings, "file_lines", rel, actual, budget)
        function_metrics = scan_functions(root, files) if args.component in {"all", "functions"} else []
        if args.component in {"all", "functions"}:
            for metric in function_metrics:
                for rule, actual in (
                    ("function_lines", metric.lines),
                    ("function_complexity", metric.complexity),
                    ("function_nesting", metric.nesting),
                ):
                    policy_rule = rule.removeprefix("function_")
                    add_finding(
                        findings,
                        rule,
                        metric.path,
                        actual,
                        policy["functions"][policy_rule],
                        metric.symbol,
                    )
        findings.sort(key=lambda finding: finding.id)
        ids = [finding.id for finding in findings]
        if len(ids) != len(set(ids)):
            raise PolicyError("analyzer produced duplicate stable finding IDs")
        if args.mode == "advisory" and changed is not None:
            findings = [finding for finding in findings if finding.path in changed]
        report = {
            "schema_version": 1,
            "status": "findings" if findings else "ok",
            "mode": args.mode,
            "scanned_files": len(files),
            "scanned_functions": len(function_metrics),
            "findings": [finding.payload() for finding in findings],
            "errors": [],
        }
        if args.format == "json":
            print(json.dumps(report, ensure_ascii=False, sort_keys=True, separators=(",", ":")))
        else:
            emit_text(findings)
        hard_failure = any(finding.level == "hard_ceiling" for finding in findings)
        return 1 if args.mode == "hard" and hard_failure else 0
    except (OSError, PolicyError) as exc:
        report = {
            "schema_version": 1,
            "status": "error",
            "mode": args.mode,
            "scanned_files": 0,
            "scanned_functions": 0,
            "findings": [],
            "errors": [str(exc)],
        }
        if args.format == "json":
            print(json.dumps(report, ensure_ascii=False, sort_keys=True, separators=(",", ":")))
        else:
            print(f"[policy_error] {exc}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
