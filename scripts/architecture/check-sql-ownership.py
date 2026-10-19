#!/usr/bin/env python3
from __future__ import annotations

import re
import sys
from dataclasses import dataclass
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
SCAN_ROOTS = (ROOT / "hololive",)
EXCLUDED_DIRS = {
    ".git",
    ".tmp",
    "artifacts",
    "dist",
    "build",
    "node_modules",
    "target",
    "vendor",
    "docs",
    "fixtures",
    "testdata",
    "dbtest",
    "hololive-dbtest",
}
SQL_KEYWORD_RE = re.compile(
    r"\b("
    r"SELECT\s|INSERT\s+(?:INTO\s+)?|UPDATE\s|DELETE\s+FROM|"
    r"WITH\s+[A-Za-z_][A-Za-z0-9_]*\s+AS|ON\s+CONFLICT|"
    r"CREATE\s+(?:TABLE|INDEX|ROLE|EXTENSION|SCHEMA|DATABASE)|ALTER\s+(?:TABLE|ROLE)|"
    r"DROP\s+(?:TABLE|INDEX|SCHEMA|DATABASE)?|TRUNCATE\b|GRANT\s|REVOKE\s|"
    r"set_config\s*\(|pg_try_advisory_lock\s*\(|pg_advisory_unlock\s*\(|pg_advisory_lock\s*\("
    r")"
)
DDL_RE = re.compile(
    r"\b("
    r"CREATE\s+(?:TABLE|INDEX|ROLE|EXTENSION|SCHEMA|DATABASE)|ALTER\s+(?:TABLE|ROLE)|"
    r"DROP\s+(?:TABLE|INDEX|SCHEMA|DATABASE)?|TRUNCATE\b|GRANT\s|REVOKE\s"
    r")\b",
    re.IGNORECASE,
)
SQL_WILDCARD_PATTERNS = (
    ("COUNT wildcard aggregate", re.compile(r"\bCOUNT\s*\(\s*\*\s*\)", re.IGNORECASE)),
)
SELECT_RE = re.compile(r"\bSELECT\b", re.IGNORECASE)
LEADING_SQL_FRAGMENT_RE = re.compile(r"^(?:ON\s+CONFLICT|VALUES|AND|OR|WHERE|SET|RETURNING)\b", re.IGNORECASE)
TRAILING_SQL_FRAGMENT_RE = re.compile(r"\b(?:VALUES|WHERE|AND|OR|SET|FROM|JOIN|ON)\s*$", re.IGNORECASE)

# lease 유효성은 wall clock, 후보·projection 조회는 문장 시각을 쓴다.
# 잠금 완료 뒤 평가되는지는 SQL 토큰만으로 증명하지 않고 기존 DB 경합 시험이 맡는다.
COLLECTOR_LEASE_QUERIES = "hololive/hololive-youtube-collector/internal/runtime/joblease/queries"
ALARM_DISPATCH_QUERIES = "hololive/hololive-alarm-worker/internal/service/alarm/dispatchoutbox/queries"
YOUTUBE_RUNTIME_QUERIES = "hololive/hololive-api/internal/planes/youtube/runtime/queries"
NOTIFICATION_DELIVERY_QUERIES = "hololive/hololive-alarm-worker/internal/egress/notificationdelivery/queries"
CLOCK_QUERY_POLICIES = (
    (f"{COLLECTOR_LEASE_QUERIES}/repository_lease_*.sql", "clock_timestamp"),
    (f"{ALARM_DISPATCH_QUERIES}/delivery_ready_*.sql", "clock_timestamp"),
    (f"{ALARM_DISPATCH_QUERIES}/handoff_*.sql", "clock_timestamp"),
    (f"{ALARM_DISPATCH_QUERIES}/repository_claim_*.sql", "clock_timestamp"),
    (f"{ALARM_DISPATCH_QUERIES}/repository_maintenance_*.sql", "clock_timestamp"),
    (f"{ALARM_DISPATCH_QUERIES}/repository_terminal_*.sql", "clock_timestamp"),
    (f"{ALARM_DISPATCH_QUERIES}/repository_transitions_*.sql", "clock_timestamp"),
    (f"{ALARM_DISPATCH_QUERIES}/send_request_*.sql", "clock_timestamp"),
    (f"{NOTIFICATION_DELIVERY_QUERIES}/outbox_repository_0279_09.sql", "statement_timestamp"),
    (f"{NOTIFICATION_DELIVERY_QUERIES}/outbox_*.sql", "clock_timestamp"),
    (f"{COLLECTOR_LEASE_QUERIES}/repository_candidates*.sql", "statement_timestamp"),
    (f"{COLLECTOR_LEASE_QUERIES}/repository_target_*.sql", "statement_timestamp"),
    (f"{COLLECTOR_LEASE_QUERIES}/repository_projection_current_*.sql", "statement_timestamp"),
    (f"{YOUTUBE_RUNTIME_QUERIES}/collection_target_observability.sql", "statement_timestamp"),
    (f"{YOUTUBE_RUNTIME_QUERIES}/live_check_videos.sql", "statement_timestamp"),
    (f"{YOUTUBE_RUNTIME_QUERIES}/time_finalization_*.sql", "statement_timestamp"),
)
CLOCK_FUNCTIONS = {"now", "transaction_timestamp", "statement_timestamp", "clock_timestamp"}
TRANSACTION_CLOCK_KEYWORDS = {"current_timestamp", "current_date", "current_time", "localtimestamp", "localtime"}
SQL_TOKEN_RE = re.compile(
    r"--[^\n]*|/\*|[eE]'(?:\\.|''|[^'])*'|'(?:''|[^'])*'|"
    r'"(?:""|[^"])*"|\$(?:[A-Za-z_][A-Za-z_0-9]*)?\$|'
    r"[A-Za-z_][A-Za-z_0-9$]*|[^\s]",
    re.DOTALL,
)
BLOCK_COMMENT_RE = re.compile(r"/\*|\*/")


@dataclass(frozen=True)
class Finding:
    path: Path
    line: int
    reason: str
    excerpt: str


def rel(path: Path) -> str:
    return path.relative_to(ROOT).as_posix()


def line_number(source: str, offset: int) -> int:
    return source.count("\n", 0, offset) + 1


def iter_go_literals(source: str):
    i = 0
    n = len(source)
    in_line_comment = False
    in_block_comment = False
    while i < n:
        ch = source[i]
        if in_line_comment:
            if ch == "\n":
                in_line_comment = False
            i += 1
            continue
        if in_block_comment:
            if ch == "*" and i + 1 < n and source[i + 1] == "/":
                in_block_comment = False
                i += 2
                continue
            i += 1
            continue
        if ch == "/" and i + 1 < n and source[i + 1] == "/":
            in_line_comment = True
            i += 2
            continue
        if ch == "/" and i + 1 < n and source[i + 1] == "*":
            in_block_comment = True
            i += 2
            continue
        if ch == "`":
            start = i
            i += 1
            end = source.find("`", i)
            if end < 0:
                break
            yield start, source[i:end]
            i = end + 1
            continue
        if ch == '"':
            start = i
            i += 1
            value: list[str] = []
            while i < n:
                if source[i] == "\\":
                    value.append(source[i : i + 2])
                    i += 2
                    continue
                if source[i] == '"':
                    break
                value.append(source[i])
                i += 1
            yield start, "".join(value)
            i += 1
            continue
        i += 1


def scan_files(roots: tuple[Path, ...]):
    for root in roots:
        for directory, dirs, files in root.walk():
            dirs[:] = [name for name in dirs if name not in EXCLUDED_DIRS]
            for name in files:
                path = directory / name
                if path.is_file():
                    yield path


def source_files() -> list[Path]:
    return [
        path for path in scan_files(SCAN_ROOTS)
        if path.suffix == ".go" and not path.name.endswith("_test.go")
    ]


def sql_asset_files() -> list[Path]:
    return [
        path for path in scan_files(SCAN_ROOTS + (ROOT / "scripts",))
        if path.name.endswith((".sql", ".sql.tpl"))
    ]


def allowed_sql_asset(path: Path) -> bool:
    parts = path.relative_to(ROOT).parts
    if "queries" in parts or "testqueries" in parts:
        return True
    if parts[:4] == ("hololive", "hololive-api", "scripts", "migrations"):
        return True
    if parts[:4] == ("hololive", "hololive-api", "scripts", "init-db"):
        return True
    if parts[:2] == ("scripts", "maintenance"):
        return True
    return False


def migration_command_asset(path: Path) -> bool:
    path_text = rel(path)
    return path_text.startswith(
        (
            "hololive/hololive-api/cmd/db-migrate/queries/",
            "hololive/hololive-api/internal/migrationrunner/queries/",
        )
    )


def complete_query_required(path: Path) -> bool:
    return rel(path).startswith(
        (
            "hololive/hololive-shared/pkg/service/delivery/queries/",
            "hololive/hololive-alarm-worker/internal/egress/notificationdelivery/queries/",
        )
    )


def excerpt(value: str) -> str:
    return " ".join(value.strip().split())[:140]


def keyword_at(sql: str, offset: int, keyword: str) -> bool:
    end = offset + len(keyword)
    return (
        sql[offset:end].upper() == keyword
        and (offset == 0 or not (sql[offset - 1].isalnum() or sql[offset - 1] == "_"))
        and (end >= len(sql) or not (sql[end].isalnum() or sql[end] == "_"))
    )


def skip_space_and_comments(sql: str, offset: int) -> int:
    n = len(sql)
    while offset < n:
        if sql[offset].isspace():
            offset += 1
            continue
        if sql.startswith("--", offset):
            newline = sql.find("\n", offset + 2)
            if newline < 0:
                return n
            offset = newline + 1
            continue
        if sql.startswith("/*", offset):
            end = sql.find("*/", offset + 2)
            if end < 0:
                return n
            offset = end + 2
            continue
        break
    return offset


def skip_balanced_parentheses(sql: str, offset: int) -> int:
    if offset >= len(sql) or sql[offset] != "(":
        return offset
    depth = 0
    i = offset
    quote: str | None = None
    while i < len(sql):
        ch = sql[i]
        if quote:
            if ch == quote:
                quote = None
            elif ch == "\\" and quote in {"'", '"'}:
                i += 1
            i += 1
            continue
        if ch in {"'", '"'}:
            quote = ch
        elif ch == "(":
            depth += 1
        elif ch == ")":
            depth -= 1
            if depth == 0:
                return i + 1
        i += 1
    return i


def select_projection_start(sql: str, offset: int) -> int:
    i = skip_space_and_comments(sql, offset)
    if keyword_at(sql, i, "ALL"):
        return skip_space_and_comments(sql, i + len("ALL"))
    if not keyword_at(sql, i, "DISTINCT"):
        return i
    i = skip_space_and_comments(sql, i + len("DISTINCT"))
    if keyword_at(sql, i, "ON"):
        i = skip_space_and_comments(sql, i + len("ON"))
        i = skip_space_and_comments(sql, skip_balanced_parentheses(sql, i))
    return i


def iter_select_wildcard_offsets(sql: str):
    for select in SELECT_RE.finditer(sql):
        i = select_projection_start(sql, select.end())
        depth = 0
        quote: str | None = None
        line_comment = False
        block_comment = False
        last_top_level_significant: str | None = None
        while i < len(sql):
            ch = sql[i]
            if line_comment:
                if ch == "\n":
                    line_comment = False
                i += 1
                continue
            if block_comment:
                if sql.startswith("*/", i):
                    block_comment = False
                    i += 2
                    continue
                i += 1
                continue
            if quote:
                if ch == quote:
                    quote = None
                elif ch == "\\" and quote in {"'", '"'}:
                    i += 1
                i += 1
                continue
            if sql.startswith("--", i):
                line_comment = True
                i += 2
                continue
            if sql.startswith("/*", i):
                block_comment = True
                i += 2
                continue
            if ch in {"'", '"'}:
                quote = ch
                i += 1
                continue
            if depth == 0 and keyword_at(sql, i, "FROM"):
                break
            if ch == "(":
                depth += 1
            elif ch == ")" and depth > 0:
                depth -= 1
            elif depth == 0 and ch == "*":
                if last_top_level_significant in {None, ",", "."}:
                    yield i
            if depth == 0 and not ch.isspace():
                last_top_level_significant = ch
            i += 1


def check_sql_wildcards(path: Path, base_line: int, sql: str) -> list[Finding]:
    findings: list[Finding] = []
    for offset in iter_select_wildcard_offsets(sql):
        findings.append(
            Finding(
                path=path,
                line=base_line + line_number(sql, offset) - 1,
                reason="SELECT wildcard projection is not allowed; name the required column(s)",
                excerpt=excerpt(sql[max(0, offset - 80) : offset + 80]),
            )
        )
    for reason, pattern in SQL_WILDCARD_PATTERNS:
        for match in pattern.finditer(sql):
            findings.append(
                Finding(
                    path=path,
                    line=base_line + line_number(sql, match.start()) - 1,
                    reason=f"{reason} is not allowed; name the required column(s)",
                    excerpt=excerpt(sql[max(0, match.start() - 80) : match.end() + 80]),
                )
            )
    return findings


def check_source_literals() -> list[Finding]:
    findings: list[Finding] = []
    for path in source_files():
        source = path.read_text(encoding="utf-8")
        for start, value in iter_go_literals(source):
            match = SQL_KEYWORD_RE.search(value)
            if not match:
                continue
            base_line = line_number(source, start)
            findings.append(
                Finding(
                    path=path,
                    line=base_line,
                    reason=f"SQL string literal contains {match.group(1).strip()}",
                    excerpt=excerpt(value),
                )
            )
            findings.extend(check_sql_wildcards(path, base_line, value))
    return findings


def iter_sql_code_tokens(sql: str):
    offset = 0
    while match := SQL_TOKEN_RE.search(sql, offset):
        token = match.group()
        offset = match.end()
        if token == "/*":
            depth = 1
            for boundary in BLOCK_COMMENT_RE.finditer(sql, offset):
                depth += 1 if boundary.group() == "/*" else -1
                offset = boundary.end()
                if depth == 0:
                    break
            if depth:
                return
        elif token.startswith("$") and token.endswith("$") and len(token) > 1:
            end = sql.find(token, offset)
            if end < 0:
                return
            offset = end + len(token)
        elif token.startswith(("--", "'")) or token[:2].lower() == "e'":
            continue
        else:
            yield match.start(), token


def check_sql_clocks(path: Path, sql: str) -> list[Finding]:
    relative = path.relative_to(ROOT)
    required = next((clock for pattern, clock in CLOCK_QUERY_POLICIES if relative.match(pattern)), None)
    if required is None:
        return []

    tokens = list(iter_sql_code_tokens(sql))
    findings: list[Finding] = []
    for index, (offset, token) in enumerate(tokens):
        quoted = token.startswith('"')
        name = token[1:-1].replace('""', '"') if quoted else token.lower()
        # PostgreSQL 시계 함수는 모두 인자가 없다. CTE·별칭의 열 목록 선언은 호출이 아니다.
        function_call = (
            index + 2 < len(tokens)
            and tokens[index + 1][1] == "("
            and tokens[index + 2][1] == ")"
        )
        if not ((name in CLOCK_FUNCTIONS and function_call) or
                (not quoted and name in TRANSACTION_CLOCK_KEYWORDS)):
            continue
        if name != required:
            findings.append(
                Finding(
                    path=path,
                    line=line_number(sql, offset),
                    reason=f"SQL clock {name} is not allowed here; use {required}()",
                    excerpt=excerpt(sql[max(0, offset - 40) : offset + 80]),
                )
            )
    return findings


def check_sql_assets() -> list[Finding]:
    findings: list[Finding] = []
    for path in sql_asset_files():
        text = path.read_text(encoding="utf-8")
        if not allowed_sql_asset(path):
            findings.append(Finding(path, 1, "SQL asset is outside allowed locations", ""))
        elif "queries" in path.relative_to(ROOT).parts and DDL_RE.search(text) and not migration_command_asset(path):
            findings.append(Finding(path, 1, "runtime query asset contains DDL/operator SQL", excerpt(text)))
        findings.extend(check_sql_wildcards(path, 1, text))
        findings.extend(check_sql_clocks(path, text))

        stripped = text.strip().rstrip(";").strip()
        if not stripped:
            findings.append(Finding(path, 1, "SQL asset is empty", ""))
            continue
        if not complete_query_required(path):
            continue
        if LEADING_SQL_FRAGMENT_RE.search(stripped):
            findings.append(Finding(path, 1, "SQL asset starts with a continuation fragment", excerpt(stripped)))
        if TRAILING_SQL_FRAGMENT_RE.search(stripped):
            findings.append(Finding(path, 1, "SQL asset ends with a continuation fragment", excerpt(stripped)))
    return findings


def main() -> int:
    findings = check_source_literals() + check_sql_assets()
    if not findings:
        print("SQL ownership check passed")
        return 0
    print("SQL ownership violations:", file=sys.stderr)
    for finding in findings:
        print(
            f"{rel(finding.path)}:{finding.line}: {finding.reason}: {finding.excerpt}",
            file=sys.stderr,
        )
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
