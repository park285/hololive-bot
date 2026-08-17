#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
RULES_FILE="${SCRIPT_DIR}/youtube-collector-hardening-contract.tsv"
REQUIRE_CANONICAL_IDS=0

usage() {
  echo "usage: $0 [--root <dir>] [--rules <tsv>]" >&2
  exit 2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --root)
      [[ $# -ge 2 ]] || usage
      ROOT_DIR="$(cd "$2" && pwd)"
      shift 2
      ;;
    --rules)
      [[ $# -ge 2 ]] || usage
      RULES_FILE="$2"
      shift 2
      ;;
    -h|--help)
      usage
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage
      ;;
  esac
done

[[ -f "${RULES_FILE}" ]] || {
  echo "rules file missing: ${RULES_FILE}" >&2
  exit 1
}
[[ -d "${ROOT_DIR}" ]] || {
  echo "root directory missing: ${ROOT_DIR}" >&2
  exit 1
}

if [[ "${RULES_FILE}" -ef "${SCRIPT_DIR}/youtube-collector-hardening-contract.tsv" ]]; then
  REQUIRE_CANONICAL_IDS=1
fi

python3 - "${ROOT_DIR}" "${RULES_FILE}" "${REQUIRE_CANONICAL_IDS}" <<'PY'
from __future__ import annotations

import fnmatch
import re
import sys
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(sys.argv[1]).resolve()
RULES = Path(sys.argv[2])
REQUIRE_CANONICAL_IDS = sys.argv[3] == "1"
CANONICAL_IDS = {f"HC-{index:03d}" for index in range(1, 16)}


@dataclass(frozen=True)
class Rule:
    rule_id: str
    enabled: bool
    owner_pr: str
    path_glob: str
    polarity: str
    regex: re.Pattern[str]
    minimum: int
    maximum: int
    allowlist: tuple[str, ...]
    line_no: int


def meaningful(raw: str) -> bool:
    value = raw.strip()
    return bool(value) and not value.startswith("#")


def strip_c_like(source: str) -> str:
    out: list[str] = []
    i = 0
    n = len(source)
    state = "code"
    while i < n:
        ch = source[i]
        nxt = source[i + 1] if i + 1 < n else ""
        if state == "code":
            if ch == "/" and nxt == "/":
                state = "line"
                out.append(" ")
                i += 2
                continue
            if ch == "/" and nxt == "*":
                state = "block"
                out.append(" ")
                i += 2
                continue
            if ch == "'":
                state = "squote"
            elif ch == '"':
                state = "dquote"
            elif ch == "`":
                state = "backtick"
            out.append(ch)
            i += 1
            continue
        if state == "line":
            if ch == "\n":
                state = "code"
                out.append(ch)
            else:
                out.append(" ")
            i += 1
            continue
        if state == "block":
            if ch == "*" and nxt == "/":
                state = "code"
                out.append("  ")
                i += 2
                continue
            out.append("\n" if ch == "\n" else " ")
            i += 1
            continue
        quote = {"squote": "'", "dquote": '"', "backtick": "`"}[state]
        if ch == "\\" and state != "backtick" and i + 1 < n:
            out.append(ch)
            out.append(source[i + 1])
            i += 2
            continue
        if ch == quote:
            state = "code"
        out.append(ch)
        i += 1
    return "".join(out)


def strip_hash(source: str) -> str:
    out: list[str] = []
    i = 0
    n = len(source)
    state = "code"
    while i < n:
        ch = source[i]
        if state == "code":
            if ch == "#":
                state = "line"
                out.append(" ")
                i += 1
                continue
            if ch in {"'", '"'}:
                state = ch
            out.append(ch)
            i += 1
            continue
        if state == "line":
            if ch == "\n":
                state = "code"
                out.append(ch)
            else:
                out.append(" ")
            i += 1
            continue
        if ch == "\\" and i + 1 < n:
            out.append(ch)
            out.append(source[i + 1])
            i += 2
            continue
        if ch == state:
            state = "code"
        out.append(ch)
        i += 1
    return "".join(out)


def strip_comments(path: Path, source: str) -> str:
    name = path.name
    suffix = path.suffix
    if suffix in {".go", ".js", ".mjs", ".cjs", ".ts", ".tsx"}:
        return strip_c_like(source)
    if (
        suffix in {".sh", ".bash", ".yml", ".yaml", ".py", ".tsv"}
        or name in {"Makefile", "Dockerfile"}
        or name.endswith("Dockerfile")
    ):
        return strip_hash(source)
    return source


def load_allowlist(root: Path, raw: str) -> tuple[str, ...]:
    if not raw or raw == "-":
        return ()
    path = Path(raw)
    if not path.is_absolute():
        path = root / raw
    if not path.is_file():
        raise SystemExit(f"[hardening] allow-list missing: {path}")
    entries: list[str] = []
    for line in path.read_text(encoding="utf-8").splitlines():
        value = line.strip()
        if meaningful(value):
            entries.append(value)
    return tuple(entries)


def parse_rules(path: Path, root: Path) -> list[Rule]:
    rows: list[Rule] = []
    for line_no, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        if not meaningful(raw):
            continue
        fields = raw.split("\t")
        if len(fields) != 9:
            raise SystemExit(f"[hardening] {path}:{line_no}: expected 9 tab-separated fields, got {len(fields)}")
        rule_id, enabled_raw, owner_pr, path_glob, polarity, pattern, min_raw, max_raw, allow_raw = fields
        if enabled_raw not in {"true", "false"}:
            raise SystemExit(f"[hardening] {path}:{line_no}: enabled must be true or false")
        if polarity not in {"required", "forbidden"}:
            raise SystemExit(f"[hardening] {path}:{line_no}: polarity must be required or forbidden")
        if not rule_id or not owner_pr or not path_glob:
            raise SystemExit(f"[hardening] {path}:{line_no}: rule_id, owner_pr, and path_glob are required")
        try:
            minimum = int(min_raw)
            maximum = int(max_raw)
        except ValueError as exc:
            raise SystemExit(f"[hardening] {path}:{line_no}: min/max must be integers") from exc
        if minimum < 0 or maximum < 0 or minimum > maximum:
            raise SystemExit(f"[hardening] {path}:{line_no}: invalid min/max range")
        try:
            regex = re.compile(pattern)
        except re.error as exc:
            raise SystemExit(f"[hardening] {path}:{line_no}: invalid regex: {exc}") from exc
        rows.append(
            Rule(
                rule_id=rule_id,
                enabled=enabled_raw == "true",
                owner_pr=owner_pr,
                path_glob=path_glob,
                polarity=polarity,
                regex=regex,
                minimum=minimum,
                maximum=maximum,
                allowlist=load_allowlist(root, allow_raw.strip()),
                line_no=line_no,
            )
        )
    if not rows:
        raise SystemExit(f"[hardening] {path}: no rules")
    return rows


def match_files(root: Path, glob_pat: str) -> list[Path]:
    matches: list[Path] = []
    for path in root.glob(glob_pat):
        if path.is_file():
            matches.append(path)
    return sorted(matches)


def allowed(rel: str, patterns: tuple[str, ...]) -> bool:
    return any(fnmatch.fnmatch(rel, pattern) for pattern in patterns)


def count_rule(root: Path, rule: Rule) -> int:
    total = 0
    for path in match_files(root, rule.path_glob):
        rel = path.relative_to(root).as_posix()
        if allowed(rel, rule.allowlist):
            continue
        text = strip_comments(path, path.read_text(encoding="utf-8"))
        total += len(rule.regex.findall(text))
    return total


def main() -> int:
    rules = parse_rules(RULES, ROOT)
    present = {rule.rule_id for rule in rules}
    if REQUIRE_CANONICAL_IDS:
        missing = sorted(CANONICAL_IDS - present)
        extra_empty = not CANONICAL_IDS.issubset(present)
        if extra_empty:
            print(f"[hardening] canonical rule IDs missing: {', '.join(missing)}", file=sys.stderr)
            return 1
    failed = 0
    for rule in rules:
        count = count_rule(ROOT, rule)
        in_range = rule.minimum <= count <= rule.maximum
        status = "OK"
        if not in_range:
            status = "SKIP" if not rule.enabled else "FAIL"
            if rule.enabled:
                failed += 1
        elif not rule.enabled:
            status = "SKIP"
        print(
            f"[hardening] {rule.rule_id} enabled={str(rule.enabled).lower()} "
            f"owner={rule.owner_pr} path={rule.path_glob} {rule.polarity} "
            f"count={count} expected={rule.minimum}..{rule.maximum} {status}"
        )
        if rule.enabled and not in_range:
            print(
                f"[hardening] {rule.rule_id} count {count} outside {rule.minimum}..{rule.maximum}",
                file=sys.stderr,
            )
    if failed:
        print(f"[hardening] {failed} enabled rule(s) failed", file=sys.stderr)
        return 1
    print("[hardening] enabled path/count rules passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
PY
