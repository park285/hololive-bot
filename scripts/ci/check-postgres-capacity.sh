#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
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

python3 - "${tmp}" "${policy_file}" "${target_env_file}" "${mode}" "${@:5}" <<'PY'
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
if verify_compose:
    postgres = services.get("holo-postgres", {})
    command = [str(value) for value in postgres.get("command", [])]
    matches = [re.fullmatch(r"max_connections=(\d+)", value) for value in command]
    limits = [int(match.group(1)) for match in matches if match]
    if limits != [server_limit]:
        raise SystemExit(f"[pg-capacity] holo-postgres max_connections={limits}, want [{server_limit}]")

used = 0
seen = set()
scaled_services_seen = set()
expected_owner_inventory_sha256 = "0334b4307870302200baec8745062302cb535fd6980afca99113caf03a104e25"
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
reserve = server_limit - used
if reserve < 5:
    raise SystemExit(f"[pg-capacity] connection budget exhausted: max={server_limit} allocated={used} reserve={reserve}, want reserve >= 5")
source = f"target-env:{target_env_path}" if target_env_path is not None else "compose-defaults"
print(f"[pg-capacity] source={source} max={server_limit} allocated={used} reserve={reserve}; all central and four AP pools are inventoried")
PY
