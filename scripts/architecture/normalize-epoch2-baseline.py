#!/usr/bin/env python3

import argparse
import re
from pathlib import Path


FORBIDDEN = (
    (re.compile(r"\bCREATE\s+DATABASE\b", re.IGNORECASE), "CREATE DATABASE"),
    (re.compile(r"\bALTER\s+DATABASE\b", re.IGNORECASE), "ALTER DATABASE"),
    (re.compile(r"\\connect\b", re.IGNORECASE), r"\connect"),
    (re.compile(r"\bCREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:\"?public\"?)", re.IGNORECASE), "CREATE SCHEMA public"),
    (re.compile(r"\bALTER\s+SCHEMA\s+(?:\"?public\"?)\s+OWNER\b", re.IGNORECASE), "ALTER SCHEMA public OWNER"),
    (re.compile(r"\bOWNER\s+TO\b", re.IGNORECASE), "OWNER TO"),
    (re.compile(r"\bSET\s+(?:LOCAL\s+|SESSION\s+)?ROLE\b", re.IGNORECASE), "SET ROLE"),
    (re.compile(r"\bschema_migrations\b", re.IGNORECASE), "schema_migrations"),
    (re.compile(r"\bschema_migration_checksums\b", re.IGNORECASE), "schema_migration_checksums"),
    (re.compile(r"\bCONCURRENTLY\b", re.IGNORECASE), "CONCURRENTLY"),
)


def validate(text: str) -> None:
    for pattern, label in FORBIDDEN:
        if pattern.search(text):
            raise ValueError(f"forbidden baseline content: {label}")


def main() -> None:
    parser = argparse.ArgumentParser(
        description=(
            "epoch-2 baseline 금지 구문 검사. baseline 재생성 모드와 generate-epoch2-baseline.sh는 "
            "DEC-20260926-hololive-retired-rollback-tooling로 닫았다(001은 운영 노출 뒤 불변)."
        )
    )
    parser.add_argument("--check-existing", type=Path, required=True)
    args = parser.parse_args()
    validate(args.check_existing.read_text())


if __name__ == "__main__":
    main()
