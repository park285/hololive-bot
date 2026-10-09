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

# 확장 build stage와 runtime stage가 같은 기준 이미지를 쓰므로 stage 이름(AS)과 --platform을 허용하되,
# postgres 이미지를 이름으로 참조하는 모든 FROM(registry 접두사·대소문자·들여쓰기 포함)은 digest로 고정되고
# 서로 같은 이미지여야 한다. 수집한 줄이 아래 고정 형식이 아니면 실패한다. ARG로 이미지를 고르는 FROM은 해석하지 않는다.
postgres_from_pattern = re.compile(
    r"^[ \t]*FROM[ \t]+(?:--\S+[ \t]+)*(?:\S*/)?postgres(?:[:@]\S*)?(?:[ \t].*)?$",
    re.MULTILINE | re.IGNORECASE,
)
image_pattern = re.compile(
    r"^[ \t]*(?i:FROM)[ \t]+(?:--platform=\S+[ \t]+)?postgres:18\.([0-9]+)(?:-[A-Za-z0-9._-]+)?"
    r"@sha256:([0-9a-f]{64})(?:[ \t]+(?i:AS)[ \t]+[A-Za-z0-9._-]+)?[ \t]*$",
)
image_matches = []
for line in postgres_from_pattern.findall(image_recipe):
    match = image_pattern.match(line)
    if match is None:
        errors.append(f"PostgreSQL base image must be a digest-pinned PostgreSQL 18 image: {line.strip()}")
        continue
    image_matches.append(match.groups())
base_images = set(image_matches)
if len(base_images) != 1:
    errors.append("expected exactly one digest-pinned PostgreSQL 18 base image")
elif int(next(iter(base_images))[0]) < 6:
    errors.append(f"PostgreSQL image default must be 18.6 or newer, got 18.{next(iter(base_images))[0]}")

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
