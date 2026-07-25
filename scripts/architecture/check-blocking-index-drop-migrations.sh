#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
MIGRATIONS_DIR="${1:-${ROOT_DIR}/hololive/hololive-api/scripts/migrations}"
GRANDFATHERED_THROUGH=114

if [[ ! -d "${MIGRATIONS_DIR}" ]]; then
  echo "FAIL: migrations directory missing: ${MIGRATIONS_DIR}" >&2
  exit 1
fi

python3 - "${MIGRATIONS_DIR}" "${GRANDFATHERED_THROUGH}" <<'PY'
import re
import sys
from pathlib import Path

migrations_dir = Path(sys.argv[1])
grandfathered_through = int(sys.argv[2])
blocking_drop = re.compile(r"^\s*DROP\s+INDEX\b", re.IGNORECASE | re.DOTALL)
concurrent_drop = re.compile(r"^\s*DROP\s+INDEX\s+CONCURRENTLY\b", re.IGNORECASE | re.DOTALL)


def dollar_tag(text: str, pos: int) -> str | None:
    if text[pos] != "$":
        return None
    end = pos + 1
    while end < len(text):
        char = text[end]
        if char == "$":
            return text[pos : end + 1]
        if char != "_" and not char.isalnum():
            return None
        end += 1
    return None


def scan_quoted(text: str, pos: int) -> int:
    quote = text[pos]
    escape_string = quote == "'" and pos > 0 and text[pos - 1] in {"E", "e"}
    pos += 1
    while pos < len(text):
        if escape_string and text[pos] == "\\" and pos + 1 < len(text):
            pos += 2
            continue
        if text[pos] != quote:
            pos += 1
            continue
        if pos + 1 < len(text) and text[pos + 1] == quote:
            pos += 2
            continue
        return pos + 1
    return pos


def scan_block_comment(text: str, pos: int) -> int:
    depth = 1
    pos += 2
    while pos < len(text) and depth > 0:
        if text.startswith("/*", pos):
            depth += 1
            pos += 2
        elif text.startswith("*/", pos):
            depth -= 1
            pos += 2
        else:
            pos += 1
    return pos


def statements(text: str) -> list[str]:
    result: list[str] = []
    buffer: list[str] = []
    pos = 0

    def flush() -> None:
        statement = "".join(buffer).strip()
        buffer.clear()
        if statement:
            result.append(statement)

    while pos < len(text):
        if text.startswith("--", pos):
            newline = text.find("\n", pos + 2)
            buffer.append(" ")
            pos = len(text) if newline < 0 else newline + 1
            continue
        if text.startswith("/*", pos):
            buffer.append(" ")
            pos = scan_block_comment(text, pos)
            continue
        if text[pos] in {"'", '"'}:
            end = scan_quoted(text, pos)
            buffer.append(text[pos:end])
            pos = end
            continue
        if text[pos] == "$":
            tag = dollar_tag(text, pos)
            if tag is not None:
                end = text.find(tag, pos + len(tag))
                if end < 0:
                    buffer.append(text[pos:])
                    pos = len(text)
                else:
                    end += len(tag)
                    buffer.append(text[pos:end])
                    pos = end
                continue
        if text[pos] == ";":
            flush()
            pos += 1
            continue
        buffer.append(text[pos])
        pos += 1

    flush()
    return result


for path in sorted(migrations_dir.glob("[0-9]*.sql")):
    prefix_match = re.match(r"(\d+)", path.name)
    if prefix_match is None:
        continue
    if int(prefix_match.group(1)) <= grandfathered_through:
        continue

    for statement in statements(path.read_text(encoding="utf-8")):
        if blocking_drop.match(statement) and not concurrent_drop.match(statement):
            print(
                f"FAIL: {path.name} uses blocking DROP INDEX; "
                "use DROP INDEX CONCURRENTLY or an explicit maintenance-only non-migration procedure",
                file=sys.stderr,
            )
            raise SystemExit(1)

print("OK: new migrations avoid blocking DROP INDEX")
PY
