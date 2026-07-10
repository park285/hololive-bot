#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DEFAULT_POLICY_FILE="${ROOT_DIR}/scripts/perf/pgo/default-policy.tsv"
DEFAULT_COMPOSE_FILE="${ROOT_DIR}/deploy/compose/docker-compose.prod.yml"
POLICY_FILE="${DEFAULT_POLICY_FILE}"
COMPOSE_FILE="${DEFAULT_COMPOSE_FILE}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --policy)
      POLICY_FILE="$2"
      shift 2
      ;;
    --compose)
      COMPOSE_FILE="$2"
      shift 2
      ;;
    *)
      echo "unknown argument: $1" >&2
      exit 2
      ;;
  esac
done

[[ -f "${POLICY_FILE}" ]] || { echo "[pgo-gate] policy missing: ${POLICY_FILE}" >&2; exit 1; }
[[ -f "${COMPOSE_FILE}" ]] || { echo "[pgo-gate] Compose file missing: ${COMPOSE_FILE}" >&2; exit 1; }

tmp_dir="$(mktemp -d)"
entries_file="${tmp_dir}/entries.tsv"
policy_rows_file="${tmp_dir}/policy-rows.tsv"
trap 'rm -rf "${tmp_dir}"' EXIT
declare -A managed_artifacts=()

resolve_module() {
  if [[ "$1" = /* ]]; then
    printf '%s\n' "$1"
  else
    printf '%s/%s\n' "${ROOT_DIR}" "$1"
  fi
}

entry_count=0
while IFS='|' read -r mode module pkg compose_services extra; do
  [[ -z "${mode}" || "${mode}" == \#* ]] && continue
  if [[ "${mode}" == "on" ]]; then
    echo "[pgo-gate] PGO default-on policy is forbidden until a verifiable adoption design is implemented" >&2
    exit 1
  fi
  if [[ -n "${extra}" || "${mode}" != "off" || -z "${module}" || "${pkg}" != ./* || ! "${compose_services}" =~ ^[a-z0-9][a-z0-9_-]*(,[a-z0-9][a-z0-9_-]*)*$ ]]; then
    echo "[pgo-gate] invalid off-only policy row: ${mode}|${module}|${pkg}|${compose_services}${extra:+|${extra}}" >&2
    exit 1
  fi

  module_dir="$(resolve_module "${module}")"
  [[ -d "${module_dir}" ]] || { echo "[pgo-gate] module missing: ${module_dir}" >&2; exit 1; }
  profile="${module_dir}/${pkg#./}/default.pgo"
  for artifact in "${profile}" "${profile}.meta.json" "${profile}.hotpaths"; do
    managed_artifacts["${artifact}"]=1
    if [[ -e "${artifact}" || -L "${artifact}" ]]; then
      echo "[pgo-gate] PGO is off but artifact exists: ${artifact}" >&2
      exit 1
    fi
  done

  out="${tmp_dir}/binary-${entry_count}"
  echo "[pgo-gate] building ${module} ${pkg} (policy=off)"
  (cd "${module_dir}" && CGO_ENABLED=0 go build -pgo=off -tags sonic -trimpath -buildvcs=false -o "${out}" "${pkg}")
  if go version -m "${out}" | grep -q -- '-pgo='; then
    echo "[pgo-gate] ${module} ${pkg}: off policy build carries a -pgo stamp" >&2
    exit 1
  fi
  echo "[pgo-gate] ${module} ${pkg} built without a -pgo build stamp"
  printf '%s\n' "${compose_services}" >>"${entries_file}"
  printf '%s|%s|%s\n' "${module}" "${pkg}" "${compose_services}" >>"${policy_rows_file}"
  entry_count=$((entry_count + 1))
done <"${POLICY_FILE}"

(( entry_count > 0 )) || { echo "[pgo-gate] policy has no service entries" >&2; exit 1; }

if [[ "${POLICY_FILE}" == "${DEFAULT_POLICY_FILE}" && "${COMPOSE_FILE}" == "${DEFAULT_COMPOSE_FILE}" ]]; then
  cat >"${tmp_dir}/expected-policy-rows.tsv" <<'EOF'
hololive/hololive-api|./cmd/hololive-api|hololive-api,hololive-db-migrate
hololive/hololive-alarm-worker|./cmd/alarm-worker|hololive-alarm-worker
EOF
  sort "${policy_rows_file}" -o "${policy_rows_file}"
  sort "${tmp_dir}/expected-policy-rows.tsv" -o "${tmp_dir}/expected-policy-rows.tsv"
  if ! cmp -s "${policy_rows_file}" "${tmp_dir}/expected-policy-rows.tsv"; then
    echo "[pgo-gate] production off-only policy must cover the exact API, migrate, and alarm rows" >&2
    exit 1
  fi
fi

artifacts_file="${tmp_dir}/artifacts.txt"
if ! find "${ROOT_DIR}/hololive" \( -type f -o -type l \) \( -name 'default.pgo' -o -name 'default.pgo.meta.json' -o -name 'default.pgo.hotpaths' \) -print >"${artifacts_file}"; then
  echo "[pgo-gate] failed to scan for default PGO artifacts" >&2
  exit 1
fi
while IFS= read -r artifact; do
  if [[ -z "${managed_artifacts[${artifact}]+x}" ]]; then
    echo "[pgo-gate] unmanaged default PGO artifact: ${artifact}" >&2
    exit 1
  fi
done <"${artifacts_file}"

if ! docker compose -f "${COMPOSE_FILE}" config --no-interpolate --format json >"${tmp_dir}/compose.json"; then
  echo "[pgo-gate] failed to structurally render Compose file: ${COMPOSE_FILE}" >&2
  exit 1
fi

python3 - "${tmp_dir}/compose.json" "${entries_file}" "${COMPOSE_FILE}" <<'PY'
import json
import re
import sys
from pathlib import Path

compose = json.loads(Path(sys.argv[1]).read_text())
services = compose.get("services", {})
managed_services = set()
compose_path = Path(sys.argv[3]).resolve()


def validate_dockerfile(service_name, service):
    build = service.get("build", {})
    context = Path(build.get("context", "."))
    if not context.is_absolute():
        context = (compose_path.parent / context).resolve()
    dockerfile = Path(build.get("dockerfile", "Dockerfile"))
    if not dockerfile.is_absolute():
        dockerfile = context / dockerfile
    try:
        source = dockerfile.read_text(encoding="utf-8")
    except OSError as exc:
        print(f"[pgo-gate] service {service_name} Dockerfile unreadable: {dockerfile}: {exc}", file=sys.stderr)
        raise SystemExit(1)
    if re.search(r"(?m)^\s*ARG\s+GO_PGO_FILE(?:\s*=|\s*$)", source):
        print(f"[pgo-gate] service {service_name} Dockerfile exposes GO_PGO_FILE", file=sys.stderr)
        raise SystemExit(1)
    logical_source = re.sub(r"\\\r?\n", " ", source)
    commands = []
    for line in logical_source.splitlines():
        if not re.match(r"^\s*RUN\b", line):
            continue
        commands.extend(re.findall(r"\bgo\s+build\b.*?(?=\s*(?:&&|;|\|\||$))", line))
    if not commands:
        print(f"[pgo-gate] service {service_name} Dockerfile has no Go build command", file=sys.stderr)
        raise SystemExit(1)
    for command in commands:
        if not re.search(r"(?:^|\s)-pgo=off(?:\s|$)", command):
            print(f"[pgo-gate] service {service_name} Dockerfile Go build must use -pgo=off", file=sys.stderr)
            raise SystemExit(1)

for service_list in Path(sys.argv[2]).read_text().splitlines():
    for service_name in service_list.split(","):
        if service_name in managed_services:
            print(f"[pgo-gate] duplicate Compose PGO service in policy: {service_name}", file=sys.stderr)
            raise SystemExit(1)
        managed_services.add(service_name)
        service = services.get(service_name)
        if service is None:
            print(f"[pgo-gate] Compose service missing: {service_name}", file=sys.stderr)
            raise SystemExit(1)
        args = service.get("build", {}).get("args", {})
        if "GO_PGO_FILE" in args:
            print(
                f"[pgo-gate] service {service_name} must not expose GO_PGO_FILE build args",
                file=sys.stderr,
            )
            raise SystemExit(1)
        validate_dockerfile(service_name, service)

for service_name, service in services.items():
    args = service.get("build", {}).get("args", {}) if isinstance(service, dict) else {}
    if "GO_PGO_FILE" in args and service_name not in managed_services:
        print(f"[pgo-gate] unmanaged Compose PGO service: {service_name}", file=sys.stderr)
        raise SystemExit(1)
PY

echo "[pgo-gate] off-only PGO policy, artifact absence, build stamps, Dockerfiles, and Compose args agree"
