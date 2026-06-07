#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

bash scripts/ci/check-workflow-secrets.sh

python3 - <<'PY'
from __future__ import annotations

import re
import sys
from pathlib import Path

WORKFLOW_DIR = Path(".github/workflows")
SECURITY_WORKFLOWS = {"security.yml", "security.yaml"}
PR_EVENT_RE = re.compile(r"(^|[^A-Za-z0-9_])pull_request(?:_target)?([^A-Za-z0-9_]|$)")

# PR fast gate는 secret-free smoke/static checks만 담당한다.
# local-ci, dependency hygiene, gosec/govulncheck, race, frontend full build는 로컬 pre-push 또는 non-PR security workflow 소유다.
PR_HEAVY_PATTERNS: list[tuple[str, str]] = [
    ("local full CI gate", r"\./scripts/ci/local-ci\.sh"),
    ("dependency hygiene", r"\bRUN_DEPENDENCY_HYGIENE\b|\bgo\s+list\s+-m\s+-u\b"),
    ("race test", r"\bgo\s+test\b[^\n]*\s-race\b|\bRUN_RACE_TESTS\b"),
    ("govulncheck", r"\bgovulncheck\b"),
    ("gosec", r"\bgosec\b"),
    ("private module token", r"\bMODULES_TOKEN\b"),
    ("admin frontend full gate", r"\bnpm\s+ci\b|\bnpm\s+run\s+build\b"),
]

REQUIRED_WORKFLOW_NEEDLES: dict[str, tuple[str, ...]] = {
    ".github/workflows/ci.yml": (
        "concurrency:",
        "timeout-minutes:",
        "check-quality-gate-contract.sh",
    ),
    ".github/workflows/security.yml": (
        "concurrency:",
        "timeout-minutes:",
        "GOVULNCHECK_VERSION:",
        "GOSEC_VERSION:",
        "govulncheck",
        "gosec",
    ),
}

REQUIRED_PRE_PUSH_NEEDLES: list[tuple[str, str]] = [
    ("local CI delegation", "./scripts/ci/local-ci.sh"),
    ("fast mode changed package routing", "LOCAL_CI_GO_SCOPE"),
    ("full mode override", "PRE_PUSH_MODE"),
    ("optional race full gate", "RUN_RACE_TESTS"),
    ("optional dependency hygiene full gate", "RUN_DEPENDENCY_HYGIENE"),
    ("admin frontend local gate", "npm ci"),
]

REQUIRED_LOCAL_CI_NEEDLES: list[tuple[str, str]] = [
    ("architecture gate", "scripts/architecture/ci-boundary-gate.sh"),
    ("sensitive log scan", "grep-sensitive-logs.sh"),
    ("go work sync drift", "check_go_work_sync"),
    ("go fix drift", "check_go_fix"),
    ("go mod tidy drift", "check_go_mod_tidy"),
    ("staticcheck", "check_staticcheck"),
    ("go build", "Go build"),
    ("go test", "Go test"),
    ("race tests", "RUN_RACE_TESTS"),
    ("dependency hygiene", "RUN_DEPENDENCY_HYGIENE"),
]


def meaningful(raw: str) -> bool:
    stripped = raw.strip()
    return bool(stripped) and not stripped.startswith("#")


def indent(raw: str) -> int:
    return len(raw) - len(raw.lstrip(" "))


def has_pull_request_trigger(text: str) -> bool:
    in_on = False
    on_indent = 0

    for raw in text.splitlines():
        if not meaningful(raw):
            continue

        current_indent = indent(raw)
        stripped = raw.strip()
        match = re.match(r"^(\s*)on\s*:\s*(.*)$", raw)
        if match:
            in_on = True
            on_indent = len(match.group(1))
            if PR_EVENT_RE.search(match.group(2).strip()):
                return True
            continue

        if in_on:
            if current_indent <= on_indent and re.match(r"^\S", raw):
                in_on = False
            elif (
                re.match(r"^\s*pull_request(?:_target)?\s*:", raw)
                or re.match(r"^\s*-\s*pull_request(?:_target)?\s*$", stripped)
            ):
                return True

    return False


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


def workflow_paths() -> list[Path]:
    paths = sorted(WORKFLOW_DIR.glob("*.yml")) + sorted(WORKFLOW_DIR.glob("*.yaml"))
    if not paths:
        raise SystemExit("no workflow files found under .github/workflows")
    return paths


def require_needles(path_s: str, needles: list[tuple[str, str]] | tuple[str, ...], failures: list[str]) -> None:
    path = Path(path_s)
    if not path.is_file():
        failures.append(f"{path_s}: required file is missing")
        return
    text = path.read_text(encoding="utf-8")
    for item in needles:
        if isinstance(item, tuple):
            desc, needle = item
        else:
            desc, needle = item, item
        if needle not in text:
            failures.append(f"{path_s}: missing quality-gate contract marker: {desc}")


def main() -> int:
    failures: list[str] = []

    for path in workflow_paths():
        text = path.read_text(encoding="utf-8")
        masked = mask_comment_lines(text)
        has_pr = has_pull_request_trigger(text)

        if path.name in SECURITY_WORKFLOWS and has_pr:
            failures.append(f"{path}: security workflow must not run on pull_request")

        if not has_pr:
            continue

        for desc, pattern in PR_HEAVY_PATTERNS:
            for match in re.finditer(pattern, masked, flags=re.IGNORECASE):
                failures.append(
                    f"{path}:{line_number_at(masked, match.start())}: "
                    f"PR fast gate must not reintroduce {desc}"
                )

    for path_s, needles in REQUIRED_WORKFLOW_NEEDLES.items():
        require_needles(path_s, needles, failures)

    require_needles("scripts/ci/pre-push-gate.sh", REQUIRED_PRE_PUSH_NEEDLES, failures)
    require_needles("scripts/ci/local-ci.sh", REQUIRED_LOCAL_CI_NEEDLES, failures)

    if failures:
        print("FAIL: quality gate contract violation", file=sys.stderr)
        for failure in failures:
            print(f" - {failure}", file=sys.stderr)
        return 1

    print("ok: quality gate contract check passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
PY
