#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${script_dir}/../.." && pwd)"
. "${script_dir}/python-runtime.sh"
# shellcheck source=scripts/deploy/lib/postgres-capacity.sh
. "${root}/scripts/deploy/lib/postgres-capacity.sh"
repo_python_init
if (( $# > 2 )); then
    echo "usage: $0 [quadlet-unit-dir] [policy-file]" >&2
    exit 2
fi
unit_dir="${1:-${root}/deploy/quadlet/hololive-bot}"
policy_file="${2:-${root}/scripts/ci/postgres-capacity-policy.tsv}"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

# 예산 산식은 배포 preflight와 같은 lib가 소유한다. 빈 target env는 policy 기본값만으로 예산을 센다.
: >"${tmp}/quadlet-defaults.env"
postgres_capacity_assert_policy_target "${policy_file}" "${tmp}/quadlet-defaults.env"

# 배포 원본인 Quadlet unit의 Exec 인자와 Environment 기본값이 policy pin·기본값과 같은지 본다.
# [Container] Environment=KEY=${VAR}와 [Service] Environment=VAR=기본값을 ${VAR:-기본값}으로 합친다.
"${CI_PYTHON_BIN}" - "${unit_dir}" "${policy_file}" <<'PY'
import re
import shlex
import sys
from pathlib import Path

unit_dir = Path(sys.argv[1])
services = {}
for unit in sorted(unit_dir.glob("*.container")):
    sections: dict[str, list[str]] = {}
    current = None
    pending = ""
    for raw in unit.read_text().splitlines():
        line = pending + raw.strip()
        if line.endswith("\\"):
            pending = line[:-1] + " "
            continue
        pending = ""
        if not line or line.startswith(("#", ";")):
            continue
        if line.startswith("[") and line.endswith("]"):
            current = line[1:-1]
            continue
        sections.setdefault(current, []).append(line)

    def assignments(section: str, key: str) -> list[str]:
        return [line.split("=", 1)[1] for line in sections.get(section, []) if line.split("=", 1)[0] == key]

    def env_pairs(section: str) -> dict[str, str]:
        pairs = {}
        for value in assignments(section, "Environment"):
            for item in shlex.split(value):
                name, _, val = item.partition("=")
                pairs[name] = val
        return pairs

    defaults = env_pairs("Service")
    environment = {}
    for name, value in env_pairs("Container").items():
        match = re.fullmatch(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}", value)
        if match and match.group(1) in defaults:
            value = f"${{{match.group(1)}:-{defaults[match.group(1)]}}}"
        environment[name] = value
    command = []
    for value in assignments("Container", "Exec"):
        command.extend(shlex.split(value))
    services[unit.stem] = {"command": command, "environment": environment}

policy = [line.strip() for line in Path(sys.argv[2]).read_text().splitlines()]


def pinned(name: str) -> int:
    rows = [line.split("|", 1)[1] for line in policy if line.startswith(f"@{name}|")]
    if len(rows) != 1:
        raise SystemExit(f"[pg-capacity] policy must pin exactly one @{name}")
    return int(rows[0])


server_limit = pinned("server-limit")
superuser_reserved = pinned("superuser-reserved")
# unit이 superuser_reserved_connections를 지정하지 않으면 PostgreSQL 기본값 3이 적용된다.
postgres_default_superuser_reserved = 3
command = [str(value) for value in services.get("holo-postgres", {}).get("command", [])]
matches = [re.fullmatch(r"max_connections=(\d+)", value) for value in command]
limits = [int(match.group(1)) for match in matches if match]
if limits != [server_limit]:
    raise SystemExit(f"[pg-capacity] holo-postgres max_connections={limits}, want [{server_limit}]")
matches = [re.fullmatch(r"superuser_reserved_connections=(\d+)", value) for value in command]
reserved = [int(match.group(1)) for match in matches if match] or [postgres_default_superuser_reserved]
if reserved != [superuser_reserved]:
    raise SystemExit(
        f"[pg-capacity] holo-postgres superuser_reserved_connections={reserved}, want [{superuser_reserved}]"
    )

for line in policy:
    if not line or line.startswith(("#", "@")):
        continue
    _owner, service_name, env_key, source_key, _instances, default_text, _role = line.split("|")
    if not service_name:
        continue
    service = services.get(service_name)
    if service is None:
        raise SystemExit(f"[pg-capacity] missing Quadlet unit: {service_name}")
    if not env_key:
        continue
    actual = service["environment"].get(env_key, "")
    match = re.fullmatch(r"\$\{([^}:]+):-([0-9]+)\}", actual)
    if not match or match.group(1) != source_key or int(match.group(2)) != int(default_text):
        raise SystemExit(
            f"[pg-capacity] {service_name}.{env_key}={actual!r}, "
            f"want ${{{source_key}:-{default_text}}}"
        )
print(f"[pg-capacity] Quadlet units match the policy pins and pool defaults: {unit_dir}")
PY
