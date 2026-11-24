#!/usr/bin/env bash

postgres_endpoint_validate() {
  local host="$1" port="$2"
  [[ -n "$host" && -n "$port" ]] || { echo '[postgres-endpoint] missing host or port' >&2; return 1; }
  python3 - "$host" "$port" <<'PY'
import ipaddress
import re
import sys

host, port = sys.argv[1:]
try:
    if not re.fullmatch(r"[0-9]{1,5}", port) or not 0 < int(port) <= 65535:
        raise ValueError("port")
    if ":" in host or re.fullmatch(r"[0-9.]+", host):
        host = str(ipaddress.ip_address(host))
    else:
        host = host.lower().removesuffix(".")
        if len(host) > 253 or any(
            not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label)
            for label in host.split(".")
        ):
            raise ValueError("host")
except ValueError:
    sys.exit("[postgres-endpoint] invalid host or port")
print(f"{host}|{int(port)}")
PY
}

postgres_endpoint_env() {
  local file="$1" host port socket
  compose_env_validate_file_format "$file" || return
  host="$(compose_env_read_value_from_file "$file" POSTGRES_HOST)" || return
  port="$(compose_env_read_value_from_file "$file" POSTGRES_PORT)" || return
  socket="$(compose_env_read_value_from_file "$file" POSTGRES_SOCKET_PATH)" || return
  [[ -z "$socket" ]] || { echo '[postgres-endpoint] socket route is not an app TCP endpoint' >&2; return 1; }
  postgres_endpoint_validate "$host" "$port"
}

postgres_endpoint_container() {
  local values key value host="" port="" socket=""
  values="$(podman inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$1")" || return
  while IFS='=' read -r key value; do
    case "$key" in
      POSTGRES_HOST) host="$value" ;;
      POSTGRES_PORT) port="$value" ;;
      POSTGRES_SOCKET_PATH) socket="$value" ;;
    esac
  done <<< "$values"
  [[ -z "$socket" ]] || { echo '[postgres-endpoint] socket route is not an app TCP endpoint' >&2; return 1; }
  postgres_endpoint_validate "$host" "$port"
}

quadlet_override_origins() {
  python3 - "$1" <<'PY'
import os
import pathlib
import re
import subprocess
import sys


def reject_origin(path):
    raise ValueError(f"unsupported Quadlet/systemd override origin: {path}")


def security_label_only(path):
    if path.is_symlink() or not path.is_file():
        return False
    lines = [line.strip() for line in path.read_text().splitlines()
             if line.strip() and not line.lstrip().startswith(("#", ";"))]
    return len(lines) == 2 and lines[0] == "[Container]" and re.fullmatch(
        r"SecurityLabelLevel=s[0-9]+:c[0-9]+(?:,c[0-9]+)*", lines[1],
    ) is not None


def check_origins(installed):
    prefix = pathlib.Path("/")
    test_root = os.environ.get("HOLOLIVE_QUADLET_TEST_ROOT") if os.geteuid() != 0 else None
    if test_root:
        prefix = pathlib.Path(test_root).absolute()
    roots = {prefix / path for path in (
        "run/containers/systemd", "etc/containers/systemd", "usr/share/containers/systemd",
    )}
    installed_tree = bool(test_root) or any(installed.is_relative_to(root) for root in roots)
    if not installed_tree:
        roots.clear()
    roots.add(installed.parent)
    for key in ("QUADLET_UNIT_DIRS", "SYSTEMD_UNIT_PATH"):
        if installed_tree and os.environ.get(key):
            reject_origin(key)
    system_dirs = [
        "etc/systemd/system.control", "run/systemd/system.control",
        "run/systemd/transient", "run/systemd/generator.early",
        "etc/systemd/system", "etc/systemd/system.attached",
        "run/systemd/system", "run/systemd/system.attached",
        "run/systemd/generator", "usr/local/lib/systemd/system",
        "usr/lib/systemd/system", "lib/systemd/system", "run/systemd/generator.late",
    ]
    system_roots = {prefix / path for path in system_dirs} if installed_tree else set()
    if installed_tree:
        manager_env = subprocess.run(
            ["systemctl", "show-environment"], check=True, capture_output=True, text=True,
        ).stdout
        for line in manager_env.splitlines():
            if line.partition("=")[0] in ("QUADLET_UNIT_DIRS", "SYSTEMD_UNIT_PATH"):
                reject_origin("systemd manager search environment")
        unit_paths = subprocess.run(
            ["systemctl", "show", "--property=UnitPath", "--value"],
            check=True, capture_output=True, text=True,
        ).stdout.split()
        if not unit_paths or any(not path.startswith("/") or "\\" in path for path in unit_paths):
            reject_origin("systemd manager UnitPath")
        system_roots.update(map(pathlib.Path, unit_paths))
        service = installed.stem + ".service"

        def loaded(prop):
            return subprocess.run(
                ["systemctl", "show", service, "--property=" + prop, "--value"],
                check=True, capture_output=True, text=True,
            ).stdout.strip()
        if loaded("NeedDaemonReload") != "no":
            reject_origin("systemd loaded unit requires daemon-reload")
        load_state = loaded("LoadState")
        if load_state == "masked":
            mask = prefix / "etc/systemd/system" / service
            if loaded("FragmentPath") != "/etc/systemd/system/" + service or not (
                mask.is_symlink() and os.readlink(mask) == "/dev/null"
            ):
                reject_origin("systemd masked unit fragment")
        elif load_state != "loaded":
            reject_origin("systemd unit load state " + load_state)
        elif loaded("Names") != service or loaded("DropInPaths"):
            reject_origin("systemd loaded unit aliases or drop-ins")
        elif loaded("FragmentPath") != "/run/systemd/generator/" + service:
            reject_origin("systemd loaded unit fragment")
    name = installed.stem
    inherited = [name[:index + 1] for index, char in enumerate(name) if char == "-"]
    quadlet_dropins = {"container.d", *(part + ".container.d" for part in inherited),
                       installed.name + ".d"}
    allowed_dropin = pathlib.Path(str(installed) + ".d")
    if installed.is_symlink():
        reject_origin(installed)
    user_roots = {prefix / path for path in (
        "etc/containers/systemd/users", "usr/share/containers/systemd/users",
    )}
    visited = set()
    for root in sorted(roots):
        if root.is_symlink():
            reject_origin(root)
        if not root.exists():
            continue
        def walk_error(error):
            raise error
        for directory, children, files in os.walk(root, onerror=walk_error):
            current = pathlib.Path(directory)
            if current in visited:
                children[:] = []
                continue
            visited.add(current)
            children[:] = [child for child in children if current / child not in user_roots]
            if current.name.endswith(".d"):
                children[:] = []
                if current.name not in quadlet_dropins:
                    continue
            for child in children:
                if (current / child).is_symlink():
                    reject_origin(current / child)
            for filename in files:
                path = current / filename
                if not current.name.endswith(".d") and filename == installed.name and path != installed:
                    reject_origin(path)
                if current.name in quadlet_dropins and filename.endswith(".conf"):
                    if path.is_symlink() or not path.is_file():
                        reject_origin(path)
                    if current != allowed_dropin:
                        local_dropin = allowed_dropin / filename
                        if not security_label_only(path) or (
                            os.path.lexists(local_dropin) and not security_label_only(local_dropin)
                        ):
                            reject_origin(path)
    service_name = name + ".service"
    service_dropins = ["service.d", *(part + ".service.d" for part in inherited),
                       service_name + ".d"]
    search_dirs = system_roots | {pathlib.Path(os.path.realpath(root)) for root in system_roots}
    for root in sorted(system_roots):
        if root.is_dir():
            for entry in root.iterdir():
                if entry.name == service_name or not entry.is_symlink():
                    continue
                link = os.path.join(entry.parent, os.readlink(entry))
                while len(link) > 1 and link.endswith(("/", "/.")):
                    link = link[:-1] if link.endswith("/") else link[:-2]
                head, tail = os.path.split(link)
                target = pathlib.Path(os.path.realpath(head), tail)
                if tail == service_name and any(target.is_relative_to(directory) for directory in search_dirs):
                    reject_origin(entry)
        for dropin in service_dropins:
            directory = root / dropin
            if directory.is_symlink():
                reject_origin(directory)
            if directory.exists():
                for path in directory.iterdir():
                    if path.name.endswith(".conf"):
                        reject_origin(path)
        path = root / service_name
        if not os.path.lexists(path):
            continue
        if path == prefix / "etc/systemd/system" / service_name and path.is_symlink() and os.readlink(path) == "/dev/null":
            continue
        if root != prefix / "run/systemd/generator" or path.is_symlink():
            reject_origin(path)
        source = re.findall(r"^SourcePath=(.*)$", path.read_text(), re.MULTILINE)
        if source != [str(installed)]:
            reject_origin(path)

try:
    check_origins(pathlib.Path(sys.argv[1]).absolute())
except ValueError as error:
    sys.exit("[postgres-endpoint] " + str(error))
except (OSError, subprocess.CalledProcessError):
    sys.exit("[postgres-endpoint] cannot verify Quadlet/systemd override origins")
PY
}

postgres_quadlet_environment() {
  local unit_file="$1" compose_file="$2" installed_unit="$3" values key value source
  shift 3
  local -A container=() service_env=()
  local use_compose=false
  quadlet_override_origins "$installed_unit" || return
  values="$(python3 - "$unit_file" "$installed_unit" "$@" <<'PY'
import pathlib
import re
import shlex
import sys

try:
    unit, installed = (pathlib.Path(path).absolute() for path in sys.argv[1:3])
    wanted = sys.argv[3:]
    unsupported = set()
    environment = {"Container": {}, "Service": {}}
    service_files = []
    files = [unit, *sorted(pathlib.Path(str(installed) + ".d").glob("*.conf"))]
    for path in files:
        section = ""
        for line in path.read_text().splitlines():
            line = line.strip()
            if not line or line.startswith(("#", ";")):
                continue
            if line.endswith("\\"):
                raise ValueError(f"unsupported Quadlet continuation: {path}")
            if line.startswith("[") and line.endswith("]"):
                section = line[1:-1]
                continue
            directive, separator, raw = line.partition("=")
            if not separator:
                continue
            directive, raw = directive.strip(), raw.strip()
            if (section == "Service" and directive in ("ExecStart", "ExecStartEx")) or (
                section == "Container" and directive in ("PodmanArgs", "GlobalArgs", "EnvironmentHost")
            ) or directive == "ServiceName":
                raise ValueError(f"unsupported Quadlet execution override: {path}")
            if section in environment and directive == "Environment":
                if not raw:
                    environment[section].clear()
                for item in shlex.split(raw):
                    key, value = item.split("=", 1)
                    environment[section][key] = value
            elif section == "Service" and directive == "EnvironmentFile":
                if not raw:
                    service_files.clear()
                else:
                    service_files.extend(shlex.split(raw))
            elif section == "Service" and directive in ("PassEnvironment", "UnsetEnvironment"):
                unsupported.update(item.split("=", 1)[0] for item in shlex.split(raw))
    if any(path != "/etc/stack-secrets/hololive-bot/compose.env" for path in service_files):
        raise ValueError("unreviewed service environment source")
    print("COMPOSE_ENV_USED=" + ("true" if service_files else "false"))
    container, service = environment["Container"], environment["Service"]
    sources = set()
    for key in wanted:
        value = container.get(key, "")
        match = re.fullmatch(r"\$\{([A-Za-z_][A-Za-z0-9_]*)\}", value)
        if match:
            sources.add(match[1])
        print("CONTAINER_" + key + "=" + value)
    if sources & unsupported:
        raise ValueError("unsupported environment directive")
    for key in sources:
        print("SERVICE_" + key + "=" + service.get(key, ""))
except (OSError, ValueError) as error:
    detail = str(error) if isinstance(error, ValueError) and str(error).startswith("unsupported Quadlet") else "unreviewed environment source or syntax"
    sys.exit("[postgres-endpoint] cannot interpret Quadlet environment: " + detail)
PY
)" || return
  while IFS='=' read -r key value; do
    case "$key" in
      CONTAINER_*) container["${key#CONTAINER_}"]="$value" ;;
      SERVICE_*) service_env["${key#SERVICE_}"]="$value" ;;
      COMPOSE_ENV_USED) use_compose="$value" ;;
    esac
  done <<< "$values"
  if [[ "$use_compose" == true ]]; then
    [[ "$compose_file" != - ]] || { echo '[postgres-endpoint] missing compose environment source' >&2; return 1; }
    compose_env_validate_file_format "$compose_file" || return
    for key in "${!service_env[@]}"; do
      if compose_env_key_exists_in_file "$compose_file" "$key"; then
        service_env["$key"]="$(compose_env_read_value_from_file "$compose_file" "$key")" || return
      fi
    done
  fi
  for key in "$@"; do
    value="${container[$key]}"
    if [[ "$value" =~ ^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$ ]]; then
      source="${BASH_REMATCH[1]}"
      value="${service_env[$source]}"
    fi
    if [[ "$value" == *'$'* || "$value" == *'%'* || "$value" == *$'\n'* ]]; then
      echo '[postgres-endpoint] unresolved Quadlet endpoint expression' >&2
      return 1
    fi
    printf '%s=%s\n' "$key" "$value"
  done
}

postgres_endpoint_quadlet() {
  local values key value host="" port="" socket=""
  values="$(postgres_quadlet_environment "$1" "$2" "${3:-$1}" POSTGRES_HOST POSTGRES_PORT POSTGRES_SOCKET_PATH)" || return
  while IFS='=' read -r key value; do
    case "$key" in
      POSTGRES_HOST) host="$value" ;;
      POSTGRES_PORT) port="$value" ;;
      POSTGRES_SOCKET_PATH) socket="$value" ;;
    esac
  done <<< "$values"
  [[ -z "$socket" ]] || { echo '[postgres-endpoint] socket route is not an app TCP endpoint' >&2; return 1; }
  postgres_endpoint_validate "$host" "$port"
}

postgres_endpoint_require_same() {
  [[ -n "$1" && "$1" == "$2" ]] || {
    echo '[postgres-endpoint] endpoint transition: single-service rollback refused; retain the recovery point and restore the prior PostgreSQL HBA/config and effective endpoint together before retrying check/apply' >&2
    return 1
  }
}
