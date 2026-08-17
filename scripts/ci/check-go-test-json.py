#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path

PANIC_RE = re.compile(r"(?m)^\s*panic:")
NO_TEST_FILES_RE = re.compile(r"^\?\s+([^\s]+)\s+\[no test files\]\n$")


class CheckError(ValueError):
    pass


def load_allowlist(path: Path | None) -> set[str]:
    if path is None:
        return set()
    allowed: set[str] = set()
    for raw in path.read_text(encoding="utf-8").splitlines():
        line = raw.strip()
        if not line or line.startswith("#"):
            continue
        allowed.add(line)
    return allowed


def parse_jsonl(path: Path) -> list[dict[str, object]]:
    raw = path.read_text(encoding="utf-8")
    if raw == "":
        return []
    decoder = json.JSONDecoder()
    events: list[dict[str, object]] = []
    for lineno, line in enumerate(raw.splitlines(), 1):
        if line.strip() == "":
            continue
        try:
            obj, end = decoder.raw_decode(line)
        except json.JSONDecodeError as exc:
            raise CheckError(f"JSON parse error at line {lineno}: {exc}") from exc
        trailing = line[end:]
        if trailing.strip():
            raise CheckError(f"trailing garbage at line {lineno}")
        if not isinstance(obj, dict):
            raise CheckError(f"JSON object required at line {lineno}")
        events.append(obj)
    return events


def evaluate(
    events: list[dict[str, object]],
    allow_skip: set[str],
    require_pass: bool,
) -> None:
    packages: dict[str, str] = {}
    tests: dict[tuple[str, str], str] = {}
    seen_packages: set[str] = set()
    started_tests: set[tuple[str, str]] = set()
    panics: list[str] = []
    unexpected_skips: list[str] = []
    failures: list[str] = []
    no_test_file_packages: set[str] = set()

    for event in events:
        action = event.get("Action")
        package = event.get("Package")
        test = event.get("Test")
        package_name = package if isinstance(package, str) and package else ""
        test_name = test if isinstance(test, str) and test else ""
        if package_name:
            seen_packages.add(package_name)
        if action == "output":
            output = event.get("Output")
            if isinstance(output, str):
                if PANIC_RE.search(output):
                    panics.append(f"{package_name}/{test_name}" if test_name else (package_name or "<unknown>"))
                no_tests = NO_TEST_FILES_RE.fullmatch(output)
                if no_tests is not None and no_tests.group(1) == package_name:
                    no_test_file_packages.add(package_name)
            continue
        if action == "run" and package_name and test_name:
            started_tests.add((package_name, test_name))
            continue
        if action not in {"pass", "fail", "skip"}:
            continue
        if package_name and test_name:
            tests[(package_name, test_name)] = action
            key = f"{package_name}/{test_name}"
            if action == "fail":
                failures.append(key)
            if action == "skip" and key not in allow_skip:
                unexpected_skips.append(key)
            continue
        if package_name:
            packages[package_name] = action
            if action == "fail":
                failures.append(package_name)
            if (
                action == "skip"
                and package_name not in allow_skip
                and package_name not in no_test_file_packages
            ):
                unexpected_skips.append(package_name)

    missing_package = sorted(seen_packages - set(packages))
    missing_tests = sorted(started_tests - set(tests))
    errors: list[str] = []
    if panics:
        errors.append("panic: " + ", ".join(panics))
    if failures:
        errors.append("fail: " + ", ".join(failures))
    if unexpected_skips:
        errors.append("unexpected skip: " + ", ".join(unexpected_skips))
    if missing_package:
        errors.append("missing package result: " + ", ".join(missing_package))
    if missing_tests:
        errors.append("missing test result: " + ", ".join(f"{pkg}/{name}" for pkg, name in missing_tests))
    if require_pass and not packages:
        errors.append("no package results")
    if errors:
        raise CheckError("; ".join(errors))


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", required=True)
    parser.add_argument("--require-pass", action="store_true")
    parser.add_argument("--allow-skip-file", default="")
    args = parser.parse_args(argv)
    path = Path(args.input)
    if not path.is_file():
        print(f"input is not a file: {path}", file=sys.stderr)
        return 2
    allow = load_allowlist(Path(args.allow_skip_file) if args.allow_skip_file else None)
    try:
        evaluate(parse_jsonl(path), allow, args.require_pass)
    except CheckError as exc:
        print(f"[go-test-json] {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
