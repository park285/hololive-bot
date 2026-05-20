#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
TODAY="${1:-$(date -u +%F)}"

python3 - "$TODAY" \
    "${ROOT_DIR}/hololive/hololive-shared/pkg" \
    "${ROOT_DIR}/hololive/hololive-kakao-bot-go/internal" \
    "${ROOT_DIR}/hololive/hololive-llm-sched/internal" \
    "${ROOT_DIR}/hololive/hololive-youtube-producer/internal" <<'PY'
import datetime
import re
import sys
from pathlib import Path

if len(sys.argv) < 2:
    print("ERROR: missing required arguments", file=sys.stderr)
    sys.exit(2)

try:
    today = datetime.date.fromisoformat(sys.argv[1])
except ValueError as err:
    print(f"ERROR: invalid date argument: {sys.argv[1]}: {err}", file=sys.stderr)
    sys.exit(2)

roots = [Path(arg) for arg in sys.argv[2:]]
patterns = [
    ("todo", re.compile(r"TODO\((\d{4}-\d{2}-\d{2})\)")),
    ("remove_after", re.compile(r"remove_after\s*=\s*\"(\d{4}-\d{2}-\d{2})\"")),
]
allowed_suffixes = {".go"}

overdue = []
invalid = []
pending = []

for root in roots:
    if not root.exists():
        continue

    for path in root.rglob("*"):
        if not path.is_file() or path.suffix not in allowed_suffixes:
            continue

        try:
            lines = path.read_text(encoding="utf-8").splitlines()
        except UnicodeDecodeError:
            continue

        for lineno, line in enumerate(lines, start=1):
            for kind, pattern in patterns:
                for match in pattern.finditer(line):
                    raw_date = match.group(1)
                    try:
                        marker_date = datetime.date.fromisoformat(raw_date)
                    except ValueError:
                        invalid.append((path, lineno, kind, raw_date))
                        continue

                    if marker_date < today:
                        overdue.append((path, lineno, kind, raw_date, line.strip()))
                    else:
                        pending.append((path, lineno, kind, marker_date, line.strip()))

if invalid:
    print("ERROR: invalid deprecated marker date format")
    for path, lineno, kind, raw_date in invalid:
        print(f" - {path}:{lineno}: {kind}={raw_date}")
    sys.exit(1)

if overdue:
    print(f"ERROR: found overdue deprecated removal markers (today={today})")
    for path, lineno, kind, marker_date, line in overdue:
        print(f" - {path}:{lineno}: {kind}={marker_date}: {line}")
    sys.exit(1)

print(f"OK: no overdue deprecated removal markers (today={today}, pending_markers={len(pending)})")
for path, lineno, kind, marker_date, line in sorted(pending, key=lambda item: item[3]):
    days_left = (marker_date - today).days
    print(f" - {path}:{lineno}: {kind}={marker_date} (D-{days_left}): {line}")
PY
