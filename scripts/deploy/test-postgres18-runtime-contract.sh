#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "${ROOT_DIR}/scripts/ci/python-runtime.sh"
repo_python_init
COMPOSE_PATH="${ROOT_DIR}/deploy/compose/docker-compose.prod.yml"
IMAGE_RECIPE_PATH="${ROOT_DIR}/deploy/images/postgres/Dockerfile"

"${CI_PYTHON_BIN}" - \
  "${COMPOSE_PATH}" \
  "${IMAGE_RECIPE_PATH}" <<'PY'
from __future__ import annotations

import pathlib
import re
import sys

compose_path, image_recipe_path = map(
    pathlib.Path,
    sys.argv[1:],
)
compose = compose_path.read_text(encoding="utf-8")
image_recipe = image_recipe_path.read_text(encoding="utf-8")
errors: list[str] = []

image_pattern = re.compile(
    r"^FROM postgres:18\.([0-9]+)(?:-[A-Za-z0-9._-]+)?@sha256:[0-9a-f]{64}[ \t]*$",
    re.MULTILINE,
)
image_matches = image_pattern.findall(image_recipe)
if len(image_matches) != 1:
    errors.append("expected exactly one digest-pinned PostgreSQL 18 base image")
elif int(image_matches[0]) < 6:
    errors.append(f"PostgreSQL image default must be 18.6 or newer, got 18.{image_matches[0]}")

if compose.count("image: ${POSTGRES_IMAGE:-hololive-postgres:prod}") != 1 or "context: ../images/postgres" not in compose:
    errors.append("PostgreSQL must use the source-built gosu-hardened image")

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

if errors:
    for error in errors:
        print(f"ERROR: {error}", file=sys.stderr)
    raise SystemExit(1)

print("ok: PostgreSQL 18 runtime and data-layout contract")
PY
