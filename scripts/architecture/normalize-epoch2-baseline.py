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

DOLLAR_TAG = re.compile(r"\$[A-Za-z_][A-Za-z0-9_]*\$|\$\$")
SOURCE_COMMIT = re.compile(r"[0-9a-f]{40}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path)
    parser.add_argument("--output", type=Path)
    parser.add_argument("--acl-tail", type=Path)
    parser.add_argument("--source-commit")
    parser.add_argument("--check-existing", type=Path)
    return parser.parse_args()


def normalize_dump(text: str) -> str:
    output: list[str] = []
    active_tag: str | None = None

    for line in text.splitlines():
        outside_dollar_quote = active_tag is None
        stripped = line.strip()
        if outside_dollar_quote and (
            stripped.startswith("\\")
            or re.fullmatch(r"SET\s+.+;", stripped, re.IGNORECASE)
            or re.fullmatch(r"SELECT\s+pg_catalog\.set_config\(.+;", stripped, re.IGNORECASE)
            or re.fullmatch(r'CREATE\s+SCHEMA\s+"?public"?\s*;', stripped, re.IGNORECASE)
            or re.fullmatch(r'COMMENT\s+ON\s+SCHEMA\s+"?public"?\s+IS\s+.+;', stripped, re.IGNORECASE)
        ):
            continue

        output.append(line.rstrip())
        for match in DOLLAR_TAG.finditer(line):
            tag = match.group(0)
            if active_tag is None:
                active_tag = tag
            elif active_tag == tag:
                active_tag = None

    if active_tag is not None:
        raise ValueError(f"unterminated dollar quote {active_tag}")

    return canonicalize_constraints("\n".join(output).strip())


def canonicalize_constraints(text: str) -> str:
    replacements = {
        "chk_acl_rooms_list_type_vocab": "CHECK (list_type IN ('whitelist', 'blacklist'))",
        "chk_bot_reply_outbox_replay_audit_reason": (
            "CHECK (octet_length(reason) BETWEEN 1 AND 256 "
            "AND reason !~ '[[:cntrl:]]')"
        ),
        "chk_youtube_content_watermarks_watermark_type_vocab": (
            "CHECK (watermark_type IN ('VIDEO', 'SHORT', 'COMMUNITY_POST'))"
        ),
    }
    for name, definition in replacements.items():
        pattern = re.compile(
            rf"^(?P<indent>\s*)CONSTRAINT\s+{re.escape(name)}\s+CHECK\s+.+$",
            re.MULTILINE,
        )
        text, count = pattern.subn(
            rf"\g<indent>CONSTRAINT {name} {definition}", text
        )
        if count != 1:
            raise ValueError(
                f"expected exactly one dump constraint {name}, found {count}"
            )
    return text


def validate(text: str) -> None:
    for pattern, label in FORBIDDEN:
        if pattern.search(text):
            raise ValueError(f"forbidden baseline content: {label}")


def main() -> None:
    args = parse_args()
    generation_args = (args.input, args.output, args.acl_tail, args.source_commit)
    if args.check_existing is not None:
        if any(value is not None for value in generation_args):
            raise ValueError("--check-existing cannot be combined with generation arguments")
        validate(args.check_existing.read_text())
        return
    if any(value is None for value in generation_args):
        raise ValueError(
            "generation requires --input, --output, --acl-tail, and --source-commit"
        )
    if SOURCE_COMMIT.fullmatch(args.source_commit) is None:
        raise ValueError("source commit must be a full lowercase SHA-1")

    dump = normalize_dump(args.input.read_text())
    acl_tail = args.acl_tail.read_text().strip()
    validate(dump)
    validate(acl_tail)

    header = f"""-- GENERATED: holobot migration epoch-2 baseline
-- Source commit: {args.source_commit}
-- Legacy cutoff: 139_trust_alarm_short_links.sql
-- Compatibility checkpoint: 140_epoch2_checkpoint.sql
-- IMPORTANT:
--   - execute only on a fresh database
--   - existing databases must skip this via the R1 checkpoint marker
--   - immutable after production exposure
"""
    content = f"{header}\nBEGIN;\n\n{dump}\n\n{acl_tail}\n\nCOMMIT;\n"
    validate(content)
    args.output.write_text(content)


if __name__ == "__main__":
    main()
