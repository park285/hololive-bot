#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "${script_dir}/../.." && pwd)"
. "${script_dir}/python-runtime.sh"
repo_python_init
compose_file="${1:-${root}/deploy/compose/docker-compose.prod.yml}"
policy_file="${2:-${root}/scripts/ci/postgres-capacity-policy.tsv}"
target_env_file="${3:-}"
mode="${4:---verify-compose}"
tmp="$(mktemp)"
trap 'rm -f "${tmp}"' EXIT

case "${mode}" in
    --verify-compose)
        container_cli="${CONTAINER_CLI:-docker}"
        compose_cmd=("${container_cli}" compose)
        if [[ "${container_cli}" == "podman" ]] && command -v podman-compose >/dev/null 2>&1; then
            compose_cmd=(podman-compose)
        fi
        "${compose_cmd[@]}" -f "${compose_file}" config --no-interpolate --format json >"${tmp}"
        ;;
    --target-env-only)
        [[ -n "${target_env_file}" ]] || { echo "[pg-capacity] --target-env-only requires a target env file" >&2; exit 2; }
        printf '{"services":{}}\n' >"${tmp}"
        ;;
    *)
        echo "usage: $0 [compose-file] [policy-file] [target-env-file] [--verify-compose|--target-env-only]" >&2
        exit 2
        ;;
esac

"${CI_PYTHON_BIN}" - "${tmp}" "${policy_file}" "${target_env_file}" "${mode}" "${@:5}" <<'PY'
import json
import hashlib
import re
import sys
from pathlib import Path

compose = json.loads(Path(sys.argv[1]).read_text())
policy = Path(sys.argv[2]).read_text().splitlines()
services = compose.get("services", {})
target_env_path = Path(sys.argv[3]) if sys.argv[3] else None
verify_compose = sys.argv[4] == "--verify-compose"
scale_overrides: dict[str, int] = {}
for argument in sys.argv[5:]:
    match = re.fullmatch(r"--scale=([A-Za-z0-9][A-Za-z0-9_.-]*)=([0-9]+)", argument)
    if not match:
        raise SystemExit(f"[pg-capacity] malformed scale override: {argument!r}")
    service_name, replicas_text = match.groups()
    if service_name in scale_overrides:
        if scale_overrides[service_name] != int(replicas_text):
            raise SystemExit(f"[pg-capacity] conflicting scale overrides for service: {service_name}")
        raise SystemExit(f"[pg-capacity] duplicate scale override for service: {service_name}")
    scale_overrides[service_name] = int(replicas_text)
if target_env_path is not None and not target_env_path.is_file():
    raise SystemExit(f"[pg-capacity] target env file is not readable: {target_env_path}")

def target_override(key: str) -> str | None:
    if target_env_path is None:
        return None
    found = None
    with target_env_path.open() as lines:
        for raw_line in lines:
            line = raw_line.strip()
            if not line or line.startswith("#"):
                continue
            match = re.fullmatch(r"(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)=(.*)", line)
            if not match or match.group(1) != key:
                continue
            value = match.group(2).strip()
            if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
                value = value[1:-1]
            found = value
    return found

limit_rows = [line for line in policy if line.startswith("@server-limit|")]
if len(limit_rows) != 1:
    raise SystemExit("[pg-capacity] policy must pin exactly one @server-limit")
server_limit = int(limit_rows[0].split("|", 1)[1])
# superuser 예약 슬롯은 비슈퍼유저 앱 역할이 쓸 수 없으므로 reserve에서 뺀다.
superuser_rows = [line for line in policy if line.startswith("@superuser-reserved|")]
if len(superuser_rows) != 1:
    raise SystemExit("[pg-capacity] policy must pin exactly one @superuser-reserved")
superuser_reserved_text = superuser_rows[0].split("|", 1)[1]
if not re.fullmatch(r"0|[1-9][0-9]*", superuser_reserved_text):
    raise SystemExit("[pg-capacity] policy has an invalid @superuser-reserved")
superuser_reserved = int(superuser_reserved_text)
# compose가 superuser_reserved_connections를 지정하지 않으면 PostgreSQL 기본값 3이 적용된다.
postgres_default_superuser_reserved = 3
if verify_compose:
    postgres = services.get("holo-postgres", {})
    command = [str(value) for value in postgres.get("command", [])]
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

used = 0
seen = set()
scaled_services_seen = set()
expected_owner_inventory_sha256 = "73c0d2caaa84f46159d651c06ddcf81b010e5d2054c6ddd84715d70487eba27e"
for line in policy:
    if not line or line.startswith("#") or line.startswith("@"):
        continue
    fields = line.split("|")
    if len(fields) != 6:
        raise SystemExit(f"[pg-capacity] malformed policy row: {line}")
    owner, service_name, env_key, source_key, instances_text, default_text = fields
    if owner in seen:
        raise SystemExit(f"[pg-capacity] duplicate owner: {owner}")
    seen.add(owner)
    instances, default = int(instances_text), int(default_text)
    if instances <= 0 or default <= 0:
        raise SystemExit(f"[pg-capacity] non-positive capacity row: {line}")
    if verify_compose:
        service = services.get(service_name)
        if service is None:
            raise SystemExit(f"[pg-capacity] missing Compose service: {service_name}")
        if env_key:
            actual = str(service.get("environment", {}).get(env_key, ""))
            match = re.fullmatch(r"\$\{([^}:]+):-([^}]+)\}", actual)
            if not match or match.group(1) != source_key or int(match.group(2)) != default:
                raise SystemExit(
                    f"[pg-capacity] {service_name}.{env_key}={actual!r}, "
                    f"want ${{{source_key}:-{default}}}"
                )
    if source_key:
        override = target_override(source_key)
        if override is not None:
            if not re.fullmatch(r"[1-9][0-9]*", override):
                raise SystemExit(f"[pg-capacity] {source_key} must be a positive integer")
            capacity = int(override)
            effective_instances = instances + scale_overrides.get(service_name, 1) - 1
            if effective_instances > 1 and capacity != default:
                raise SystemExit(
                    f"[pg-capacity] {source_key} is shared by multiple independently rendered "
                    "instances; change the reviewed policy default and roll it out uniformly"
                )
        else:
            capacity = default
    else:
        capacity = default
    effective_instances = instances + scale_overrides.get(service_name, 1) - 1
    if effective_instances < 0:
        raise SystemExit(f"[pg-capacity] invalid effective instance count for service: {service_name}")
    if service_name in scale_overrides:
        scaled_services_seen.add(service_name)
    used += effective_instances * capacity

unknown_scaled_services = sorted(set(scale_overrides) - scaled_services_seen)
if unknown_scaled_services:
    raise SystemExit(
        "[pg-capacity] scale override references service absent from the reviewed capacity policy: "
        + ", ".join(unknown_scaled_services)
    )

owner_inventory = "\n".join(sorted(seen)) + "\n"
owner_inventory_sha256 = hashlib.sha256(owner_inventory.encode()).hexdigest()
if owner_inventory_sha256 != expected_owner_inventory_sha256:
    raise SystemExit(
        "[pg-capacity] owner inventory mismatch: policy owner set changed; "
        "review the topology and update expected_owner_inventory_sha256 intentionally"
    )
# reserve는 비슈퍼유저 역할이 실제로 접속 거부당하기 전까지 남는 슬롯이다(PG 거부 조건과 같은 기준).
# 하한 2는 현재 할당 55에서 보장되는 값이며, 하한 상향은 풀 사용 지표로 collector max 축소를
# 결정한 뒤 같은 변경에서 한다.
min_reserve = 2
reserve = server_limit - superuser_reserved - used
if reserve < min_reserve:
    raise SystemExit(
        f"[pg-capacity] connection budget exhausted: max={server_limit} superuser_reserved={superuser_reserved} "
        f"allocated={used} reserve={reserve}, want reserve >= {min_reserve}"
    )
source = f"target-env:{target_env_path}" if target_env_path is not None else "compose-defaults"
print(
    f"[pg-capacity] source={source} max={server_limit} superuser_reserved={superuser_reserved} "
    f"allocated={used} reserve={reserve}; central and four AP pools are inventoried"
)
PY
