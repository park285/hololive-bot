#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE_PATH="${ROOT_DIR}/deploy/compose/docker-compose.prod.yml"
BOOTSTRAP_CONTRACT_PATH="${ROOT_DIR}/hololive/hololive-api/scripts/init-db/00-assert-pg18-runtime.sql"
EXTENSION_BOOTSTRAP_PATH="${ROOT_DIR}/hololive/hololive-api/scripts/init-db/05-create-pg-stat-statements.sql"
RUNTIME_AUDIT_PATH="${ROOT_DIR}/scripts/maintenance/pg18_runtime_contract.sql"

python3 - \
  "${COMPOSE_PATH}" \
  "${BOOTSTRAP_CONTRACT_PATH}" \
  "${EXTENSION_BOOTSTRAP_PATH}" \
  "${RUNTIME_AUDIT_PATH}" <<'PY'
from __future__ import annotations

import pathlib
import re
import sys

compose_path, bootstrap_contract_path, extension_bootstrap_path, runtime_audit_path = map(
    pathlib.Path,
    sys.argv[1:],
)
compose = compose_path.read_text(encoding="utf-8")
bootstrap_contract = bootstrap_contract_path.read_text(encoding="utf-8")
extension_bootstrap = extension_bootstrap_path.read_text(encoding="utf-8")
runtime_audit = runtime_audit_path.read_text(encoding="utf-8")
errors: list[str] = []

image_pattern = re.compile(
    r"^[ \t]*image:[ \t]*\$\{POSTGRES_IMAGE:-postgres:18\.([0-9]+)(?:-[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}\}[ \t]*$",
    re.MULTILINE,
)
image_matches = image_pattern.findall(compose)
if len(image_matches) != 1:
    errors.append("expected exactly one digest-pinned PostgreSQL 18 image default")
elif int(image_matches[0]) < 6:
    errors.append(f"PostgreSQL image default must be 18.6 or newer, got 18.{image_matches[0]}")

pgdata_pattern = re.compile(
    r"^[ \t]*PGDATA:[ \t]*/var/lib/postgresql/pgdata[ \t]*$",
    re.MULTILINE,
)
if len(pgdata_pattern.findall(compose)) != 1:
    errors.append("expected exactly one compatibility PGDATA contract")

parent_mount_pattern = re.compile(
    r"^[ \t]*-[ \t]*[\"']?[^\"'\n]+:/var/lib/postgresql[\"']?[ \t]*$",
    re.MULTILINE,
)
if not parent_mount_pattern.search(compose):
    errors.append("PostgreSQL 18 volume must mount the /var/lib/postgresql parent directory")

child_mount_pattern = re.compile(
    r"^[ \t]*-[ \t]*[\"']?[^\"'\n]+:/var/lib/postgresql/(?:data|pgdata)(?::[^\"'\n]+)?[\"']?[ \t]*$",
    re.MULTILINE,
)
if child_mount_pattern.search(compose):
    errors.append("direct child data-directory mounts defeat the PostgreSQL 18 parent-volume upgrade contract")

if "--locale-provider=builtin" not in compose or "--builtin-locale=C.UTF-8" not in compose:
    errors.append("PostgreSQL volume must be initialized with the builtin C.UTF-8 locale provider")

for token in (
    "server_version_num",
    "180006",
    "datlocprovider",
    "data_checksums",
    "data_directory",
    "/var/lib/postgresql/pgdata",
    "io_method",
    "track_io_timing",
    "track_wal_io_timing",
    "compute_query_id",
    "shared_preload_libraries",
):
    if token not in bootstrap_contract:
        errors.append(f"first bootstrap contract is missing {token!r}")

if "CREATE EXTENSION IF NOT EXISTS pg_stat_statements" not in extension_bootstrap:
    errors.append("application database bootstrap must create pg_stat_statements after the contract assertion")

for token in (
    "server_version_num",
    "180006",
    "datlocprovider",
    "data_checksums",
    "data_directory",
    "io_method",
    "pg_stat_statements",
    "FROM pg_stat_io",
    "FROM pg_aios",
):
    if token not in runtime_audit:
        errors.append(f"runtime audit is missing {token!r}")

if errors:
    for error in errors:
        print(f"ERROR: {error}", file=sys.stderr)
    raise SystemExit(1)

print("ok: PostgreSQL 18 runtime and data-layout contract")
PY
