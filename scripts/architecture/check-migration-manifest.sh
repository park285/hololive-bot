#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
. "${ROOT_DIR}/scripts/ci/python-runtime.sh"
repo_python_init

"${CI_PYTHON_BIN}" - "${ROOT_DIR}" <<'PY'
import re
import sys
from pathlib import Path


def sql_statement_count(text):
    count = 0
    buf = []
    i = 0

    def flush():
        nonlocal count, buf
        if "".join(buf).strip():
            count += 1
        buf = []

    def dollar_tag(pos):
        if text[pos] != "$":
            return None
        j = pos + 1
        while j < len(text):
            ch = text[j]
            if ch == "$":
                return text[pos : j + 1]
            if not (ch == "_" or ch.isalnum()):
                return None
            j += 1
        return None

    while i < len(text):
        if text.startswith("--", i):
            end = text.find("\n", i)
            if end < 0:
                break
            buf.append(" ")
            i = end
            continue
        if text.startswith("/*", i):
            end = text.find("*/", i + 2)
            if end < 0:
                break
            buf.append(" ")
            i = end + 2
            continue
        if text[i] in ("'", '"'):
            quote = text[i]
            buf.append(text[i])
            i += 1
            while i < len(text):
                buf.append(text[i])
                if text[i] == quote:
                    if i + 1 < len(text) and text[i + 1] == quote:
                        buf.append(text[i + 1])
                        i += 2
                        continue
                    i += 1
                    break
                i += 1
            continue
        if text[i] == "$":
            tag = dollar_tag(i)
            if tag is not None:
                end = text.find(tag, i + len(tag))
                if end < 0:
                    buf.append(text[i:])
                    i = len(text)
                else:
                    buf.append(text[i : end + len(tag)])
                    i = end + len(tag)
                continue
        if text[i] == ";":
            flush()
            i += 1
            continue
        buf.append(text[i])
        i += 1

    flush()
    return count


root = Path(sys.argv[1])
migrations = root / "hololive/hololive-api/scripts/migrations"
manifest = migrations / "manifest.txt"
baseline_name = "001_schema_epoch2_baseline.sql"
acl_path = migrations / "manual/epoch2_acl_tail.sql"
suffix_path = root / "scripts/architecture/epoch2_suffix_contract.txt"

if not manifest.is_file():
    raise SystemExit(f"FAIL: migration manifest missing: {manifest}")
files = []
for raw in manifest.read_text().splitlines():
    entry = raw.strip()
    if not entry or entry.startswith("#"):
        continue
    fields = entry.split()
    if len(fields) != 2:
        raise SystemExit(f"FAIL: invalid migration manifest entry: {entry}")
    order, filename = fields
    expected = f"{len(files) + 1:03d}"
    if order != expected:
        raise SystemExit(f"FAIL: migration manifest order drift: expected {expected}, got {order}")
    files.append(filename)

if not files:
    raise SystemExit("FAIL: migration manifest is empty")
if len(files) != len(set(files)):
    raise SystemExit("FAIL: duplicate filenames in migration manifest")
actual = sorted(
    path.name for path in migrations.glob("[0-9]*.sql")
    if path.is_file() and not path.is_symlink()
)
if sorted(files) != actual:
    print("FAIL: migration manifest and actual SQL files differ", file=sys.stderr)
    for label, missing in (
        ("manifest only", set(files) - set(actual)),
        ("sql only", set(actual) - set(files)),
    ):
        print(f"--- {label}", *sorted(missing), sep="\n", file=sys.stderr)
    raise SystemExit(1)
if files[0] != baseline_name:
    raise SystemExit(f"FAIL: epoch-2 manifest must begin with {baseline_name}")
for path in (acl_path, suffix_path):
    if not path.is_file() or path.stat().st_size == 0:
        raise SystemExit(f"FAIL: epoch-2 contract artifact missing or empty: {path}")
suffix = suffix_path.read_text().splitlines()
if len(files) - 1 < len(suffix):
    raise SystemExit("FAIL: epoch-2 retained suffix is incomplete")
for position, expected in enumerate(suffix, 1):
    if files[position] != expected:
        raise SystemExit(
            f"FAIL: epoch-2 retained suffix drift at position {position + 1}: "
            f"expected {expected}, got {files[position]}"
        )

sources = {name: (migrations / name).read_text() for name in actual}
baseline = sources[baseline_name]
for pattern, label in (
    (r"\bCREATE\s+DATABASE\b", "CREATE DATABASE"),
    (r"\bALTER\s+DATABASE\b", "ALTER DATABASE"),
    (r"\\connect\b", r"\connect"),
    (r'\bCREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:"?public"?)', "CREATE SCHEMA public"),
    (r'\bALTER\s+SCHEMA\s+(?:"?public"?)\s+OWNER\b', "ALTER SCHEMA public OWNER"),
    (r"\bOWNER\s+TO\b", "OWNER TO"),
    (r"\bSET\s+(?:LOCAL\s+|SESSION\s+)?ROLE\b", "SET ROLE"),
    (r"\bschema_migrations\b", "schema_migrations"),
    (r"\bschema_migration_checksums\b", "schema_migration_checksums"),
    (r"\bCONCURRENTLY\b", "CONCURRENTLY"),
):
    if re.search(pattern, baseline, re.IGNORECASE):
        raise SystemExit(f"FAIL: forbidden baseline content: {label}")
if acl_path.read_text().strip() not in baseline:
    raise SystemExit("FAIL: epoch-2 ACL tail is not embedded verbatim in baseline")
if len(re.findall(r"(?m)^BEGIN;$", baseline)) != 1 or len(re.findall(r"(?m)^COMMIT;$", baseline)) != 1:
    raise SystemExit("FAIL: epoch-2 baseline must be one top-level transaction")
if baseline.rstrip().splitlines()[-1] != "COMMIT;":
    raise SystemExit("FAIL: epoch-2 baseline must end with COMMIT;")

prefixes = set()
for name in actual:
    prefix = re.match(r"[0-9]+", name).group()
    if prefix in prefixes:
        raise SystemExit(f"FAIL: duplicate migration number prefix {prefix} (새 파일은 마지막 번호+1을 사용)")
    prefixes.add(prefix)

# 줄 단위 SQL 규칙은 기존 grep처럼 개행을 넘지 않는다.
space = r"[^\S\n]"
begin = re.compile(rf"^{space}*(BEGIN({space}+(WORK|TRANSACTION))?|START{space}+TRANSACTION){space}*;", re.I | re.M)
end = re.compile(rf"^{space}*(COMMIT|END)({space}+(WORK|TRANSACTION))?{space}*;", re.I | re.M)
index = rf"^{space}*CREATE{space}+(UNIQUE{space}+)?INDEX{space}+"
concurrently = re.compile(r"\bCONCURRENTLY\b", re.I)
for name, sql in sources.items():
    # 유효한 CHECK를 먼저 만들어 SET NOT NULL의 전체 스캔을 피한다.
    if re.search(rf"SET{space}+NOT{space}+NULL", sql):
        if "NOT VALID" not in sql or "VALIDATE CONSTRAINT" not in sql:
            raise SystemExit(f"FAIL: {name} 에 무방비 SET NOT NULL — NOT VALID CHECK + VALIDATE CONSTRAINT 선행 필요 (CONVENTIONS.md 참고)")
    uncommented = re.sub(r"--[^\n]*", "", sql)
    if begin.search(sql):
        if concurrently.search(uncommented):
            raise SystemExit(f"FAIL: {name} wraps CONCURRENTLY in a top-level BEGIN;/COMMIT; block (mutually exclusive — runner runs the block as a real transaction)")
        if not end.search(sql):
            raise SystemExit(f"FAIL: {name} has a top-level BEGIN; without a matching COMMIT;")
    if concurrently.search(sql):
        count = sql_statement_count(sql)
        if count != 1:
            raise SystemExit(f"FAIL: {name} uses CONCURRENTLY with {count} SQL statements; keep CONCURRENTLY migrations single-statement")
    # 빈 DB에 적용하는 baseline만 트랜잭션 안에서 인덱스를 만든다.
    if name != baseline_name and re.search(index, uncommented, re.I | re.M):
        if not re.search(index + rf"CONCURRENTLY{space}+", uncommented, re.I | re.M):
            raise SystemExit(f"FAIL: {name} creates a blocking index; use CREATE INDEX CONCURRENTLY in a single-statement migration")

print("OK: migration manifest matches SQL files")
PY
