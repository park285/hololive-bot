#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P)"
fixture="$(mktemp -d)"
trap 'rm -rf -- "$fixture"' EXIT
cat > "$fixture/compose.yml" <<'YAML'
services:
  hololive-api:
    image: example.invalid/api:fixture
  hololive-alarm-worker:
    image: example.invalid/worker:fixture
YAML
export COMPOSE_ENV_FILE="$fixture/host.env"
unset HOLOLIVE_X_SPACES_ENABLED
: > "$COMPOSE_ENV_FILE"
bash "$repo_root/scripts/deploy/compose.sh" -f "$fixture/compose.yml" config --format json > "$fixture/disabled.json"
jq -e '.services["hololive-api"].environment.X_SPACES_KEY_FILE == null' "$fixture/disabled.json" >/dev/null
printf 'HOLOLIVE_X_SPACES_ENABLED=1\n' > "$COMPOSE_ENV_FILE"
for selection in implicit explicit; do
  files=(-f "$fixture/compose.yml")
  if [[ "$selection" == explicit ]]; then files+=(-f "$repo_root/deploy/compose/docker-compose.x-spaces.yml"); fi
  bash "$repo_root/scripts/deploy/compose.sh" "${files[@]}" config --format json > "$fixture/$selection.json"
  jq -e '.services["hololive-api"].environment.X_SPACES_KEY_FILE == "/run/hololive-bot/x-spaces/key" and
    .services["hololive-alarm-worker"].environment.X_SPACES_CONFIG_FILE == "/run/hololive-bot/x-spaces/config.json" and
    (.services["hololive-alarm-worker"].volumes | length == 2 and all(.[]; .read_only == true))' "$fixture/$selection.json" >/dev/null
done
printf 'HOLOLIVE_X_SPACES_ENABLED=invalid\n' > "$COMPOSE_ENV_FILE"
if bash "$repo_root/scripts/deploy/compose.sh" -f "$fixture/compose.yml" config --quiet > "$fixture/invalid.log" 2>&1; then
  echo 'invalid X Spaces activation was accepted' >&2
  exit 1
fi
echo 'X Spaces opt-in persists through the shared Compose entrypoint without duplicate mounts'
