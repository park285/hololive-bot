#!/usr/bin/env python3
from __future__ import annotations

import re
import sys
from pathlib import Path
from typing import Iterable

CONFIG = {'repo': 'hololive-bot', 'security_workflows': ['security.yml', 'security.yaml'], 'pr_heavy_patterns': [('local full CI gate', '\\./scripts/ci/local-ci\\.sh'), ('dependency hygiene', '\\bRUN_DEPENDENCY_HYGIENE\\b|\\bgo\\s+list\\s+-m\\s+-u\\b'), ('race test', '\\bgo\\s+test\\b[^\\n]*\\s-race\\b|\\bRUN_RACE_TESTS\\b'), ('govulncheck', '\\bgovulncheck\\b'), ('gosec', '\\bgosec\\b'), ('private module token', '\\bMODULES_TOKEN\\b'), ('admin frontend full gate', '\\bnpm\\s+ci\\b|\\bnpm\\s+run\\s+build\\b')], 'required_workflow_needles': {'.github/workflows/ci.yml': ('concurrency:', 'timeout-minutes:', 'check-quality-gate-contract.sh', 'persist-credentials: false'), '.github/workflows/security.yml': ('concurrency:', 'timeout-minutes:', 'check-quality-gate-contract.sh', 'GOVULNCHECK_VERSION:', 'GOSEC_VERSION:', 'govulncheck', 'gosec', 'MODULES_TOKEN')}, 'required_file_needles': {'scripts/ci/pre-push-gate.sh': (('contract check', 'check-quality-gate-contract.sh'), ('local CI delegation', './scripts/ci/local-ci.sh'), ('fast mode changed package routing', 'LOCAL_CI_GO_SCOPE'), ('full mode override', 'PRE_PUSH_MODE'), ('optional race full gate', 'RUN_RACE_TESTS'), ('optional dependency hygiene full gate', 'RUN_DEPENDENCY_HYGIENE'), ('admin frontend local gate', 'npm ci')), 'scripts/ci/local-ci.sh': (('architecture gate', 'scripts/architecture/ci-boundary-gate.sh'), ('sensitive log scan', 'grep-sensitive-logs.sh'), ('go work sync drift', 'check_go_work_sync'), ('go fix drift', 'check_go_fix'), ('go mod tidy drift', 'check_go_mod_tidy'), ('staticcheck', 'check_staticcheck'), ('go build', 'Go build'), ('go test', 'Go test'), ('race tests', 'RUN_RACE_TESTS'), ('dependency hygiene', 'RUN_DEPENDENCY_HYGIENE'))}}

WORKFLOW_DIR = Path(".github/workflows")
SECRET_EXPR_RE = re.compile(r"\$\{\{(?P<body>.*?)\}\}", re.DOTALL)
DOT_SECRET_RE = re.compile(r"secrets\s*\.\s*([A-Za-z_][A-Za-z0-9_]*)")
BRACKET_SECRET_RE = re.compile(r"secrets\s*\[\s*['\"]([A-Za-z_][A-Za-z0-9_]*)['\"]\s*\]")
SECRETS_KEY_RE = re.compile(r"^\s*secrets\s*:\s*(?:inherit|\{|$)", re.MULTILINE)
CHECKOUT_RE = re.compile(r"uses\s*:\s*[^\n#]*actions/checkout@", re.IGNORECASE)
PERSIST_FALSE_RE = re.compile(r"persist-credentials\s*:\s*false\b", re.IGNORECASE)


def meaningful(raw: str) -> bool:
    stripped = raw.strip()
    return bool(stripped) and not stripped.startswith("#")


def indent(raw: str) -> int:
    return len(raw) - len(raw.lstrip(" "))


def mask_comment_lines(text: str) -> str:
    masked: list[str] = []
    for raw in text.splitlines(keepends=True):
        if raw.strip().startswith("#"):
            masked.append(re.sub(r"[^\n]", " ", raw))
        else:
            masked.append(raw)
    return "".join(masked)


def line_number_at(text: str, offset: int) -> int:
    return text.count("\n", 0, offset) + 1


def event_present(text: str, event_name: str) -> bool:
    event_re = re.compile(rf"(^|[^A-Za-z0-9_]){re.escape(event_name)}([^A-Za-z0-9_]|$)")
    in_on = False
    on_indent = 0

    for raw in text.splitlines():
        if not meaningful(raw):
            continue

        current_indent = indent(raw)
        stripped = raw.strip()
        match = re.match(r"^(\s*)[\"']?on[\"']?\s*:\s*(.*)$", raw)
        if match:
            in_on = True
            on_indent = len(match.group(1))
            if event_re.search(match.group(2).strip()):
                return True
            continue

        if in_on:
            if current_indent <= on_indent and re.match(r"^\S", raw):
                in_on = False
                continue
            if (
                re.match(rf"^\s*{re.escape(event_name)}\s*:", raw)
                or re.match(rf"^\s*-\s*{re.escape(event_name)}\s*$", stripped)
                or event_re.search(stripped)
            ):
                return True

    return False


def workflow_paths() -> list[Path]:
    paths = sorted(WORKFLOW_DIR.glob("*.yml")) + sorted(WORKFLOW_DIR.glob("*.yaml"))
    if not paths:
        raise SystemExit("no workflow files found under .github/workflows")
    return paths


def secret_refs(masked: str) -> list[tuple[int, str]]:
    refs: list[tuple[int, str]] = []
    for expr in SECRET_EXPR_RE.finditer(masked):
        body = expr.group("body")
        body_offset = expr.start("body")
        for pattern in (DOT_SECRET_RE, BRACKET_SECRET_RE):
            for match in pattern.finditer(body):
                refs.append((line_number_at(masked, body_offset + match.start()), match.group(1)))
    return refs


def permission_blocks(text: str) -> list[tuple[int, int, str, list[tuple[int, str]]]]:
    blocks: list[tuple[int, int, str, list[tuple[int, str]]]] = []
    lines = text.splitlines()
    i = 0
    while i < len(lines):
        raw = lines[i]
        if not meaningful(raw):
            i += 1
            continue
        match = re.match(r"^(\s*)permissions\s*:\s*(.*)$", raw)
        if not match:
            i += 1
            continue
        block_indent = len(match.group(1))
        inline_value = match.group(2).strip()
        entries: list[tuple[int, str]] = []
        line_no = i + 1
        i += 1
        while i < len(lines):
            entry = lines[i]
            if meaningful(entry) and indent(entry) <= block_indent:
                break
            entries.append((i + 1, entry))
            i += 1
        blocks.append((line_no, block_indent, inline_value, entries))
    return blocks


def permissions_block_is_readonly(inline_value: str, entries: list[tuple[int, str]]) -> bool:
    if inline_value:
        return inline_value in {"read-all", "{}"}
    saw_entry = False
    for _, raw in entries:
        if not meaningful(raw):
            continue
        match = re.match(r"^\s*[A-Za-z0-9_-]+\s*:\s*([A-Za-z0-9_-]+)\s*$", raw)
        if not match:
            continue
        saw_entry = True
        if match.group(1) not in {"read", "none"}:
            return False
    return saw_entry


def top_level_permissions_readonly(text: str) -> bool:
    for _, block_indent, inline_value, entries in permission_blocks(text):
        if block_indent == 0:
            return permissions_block_is_readonly(inline_value, entries)
    return False


def require_needles(path_s: str, needles: Iterable[object], failures: list[str]) -> None:
    path = Path(path_s)
    if not path.is_file():
        failures.append(f"{path_s}: required file is missing")
        return
    text = path.read_text(encoding="utf-8")
    for item in needles:
        if isinstance(item, tuple):
            desc, needle = item
        else:
            desc, needle = str(item), str(item)
        if str(needle) not in text:
            failures.append(f"{path_s}: missing quality-gate contract marker: {desc}")


def check_workflows(failures: list[str]) -> None:
    security_workflows = set(CONFIG.get("security_workflows", []))
    heavy_patterns = [(desc, re.compile(pattern, re.IGNORECASE)) for desc, pattern in CONFIG.get("pr_heavy_patterns", [])]

    for path in workflow_paths():
        text = path.read_text(encoding="utf-8")
        masked = mask_comment_lines(text)
        has_pr = event_present(text, "pull_request")
        has_pr_target = event_present(text, "pull_request_target")

        if has_pr_target:
            failures.append(f"{path}: pull_request_target is not allowed for this repository")

        if path.name in security_workflows and (has_pr or has_pr_target):
            failures.append(f"{path}: security workflow must not run on pull_request")

        if not (has_pr or has_pr_target):
            continue

        if not top_level_permissions_readonly(text):
            failures.append(f"{path}: PR workflow must define top-level read-only permissions")

        if CHECKOUT_RE.search(masked) and not PERSIST_FALSE_RE.search(masked):
            failures.append(f"{path}: PR checkout must set persist-credentials: false")

        for line_no, name in secret_refs(masked):
            if name != "GITHUB_TOKEN":
                failures.append(f"{path}:{line_no}: PR workflow must not reference secrets.{name}")

        if SECRETS_KEY_RE.search(masked):
            failures.append(f"{path}: PR workflow must not pass reusable workflow secrets")

        for desc, pattern in heavy_patterns:
            for match in pattern.finditer(masked):
                failures.append(
                    f"{path}:{line_number_at(masked, match.start())}: "
                    f"PR fast gate must not reintroduce {desc}"
                )


def main() -> int:
    failures: list[str] = []
    check_workflows(failures)

    for path_s, needles in CONFIG.get("required_workflow_needles", {}).items():
        require_needles(path_s, needles, failures)

    for path_s, needles in CONFIG.get("required_file_needles", {}).items():
        require_needles(path_s, needles, failures)

    if failures:
        print(f"FAIL: {CONFIG.get('repo', 'repo')} quality gate contract violation", file=sys.stderr)
        for failure in failures:
            print(f" - {failure}", file=sys.stderr)
        return 1

    print(f"ok: {CONFIG.get('repo', 'repo')} quality gate contract check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
