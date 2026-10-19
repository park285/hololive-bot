#!/usr/bin/env bash
# 이전 image/profile만 복원한다. DB migration과 secret mirror는 되돌리지 않는다.
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
backup="${1:?usage: compose-rollback-service.sh BACKUP [--check|--apply]}"
mode="${2:---check}"
[[ "$mode" == --check || "$mode" == --apply ]] || exit 2
backup="$(realpath "$backup")"
service="$(cat "$backup/service")"
case "$service" in hololive-api|hololive-alarm-worker) ;; *) exit 2 ;; esac
state="$(cat "$backup/previous-state")"
case "$state" in running|exited|created) ;; *) echo 'unsupported previous writer state' >&2; exit 1 ;; esac
pair_tool="$ROOT_DIR/scripts/deploy/lib/worker-profile-pair.py"
python3 "$pair_tool" verify "$service" "$backup/previous"
files=()
for file in "$backup"/compose/[0-9][0-9][0-9].yml; do
    [[ -r "$file" ]] || exit 1
    files+=(-f "$file")
done
compose=("$ROOT_DIR/scripts/deploy/compose.sh" --project-directory "$(cat "$backup/project-directory")" "${files[@]}")
export WORKER_PROFILE_OVERRIDE_FILE="$backup/previous/compose.yml"
"${compose[@]}" config --quiet
"${compose[@]}" run --rm --no-deps --pull never "$service" --check-worker-profile
[[ "$mode" == --apply ]] || exit 0
python3 "$pair_tool" activate "$service" "$backup/previous" "$ROOT_DIR"
if [[ "$state" == running ]]; then
    "${compose[@]}" up -d --no-build --no-deps --force-recreate "$service"
    container_id="$("${compose[@]}" ps -q "$service")"
    python3 "$pair_tool" verify-live "$service" "$backup/previous" "$container_id"
else
    "${compose[@]}" up --no-start --no-build --no-deps --force-recreate "$service"
    container_id="$("${compose[@]}" ps -aq "$service")"
    [[ "$("${CONTAINER_CLI:-docker}" inspect -f '{{.State.Status}}' "$container_id")" == created ]]
    python3 "$pair_tool" verify "$service" "$backup/previous" "$("${CONTAINER_CLI:-docker}" inspect -f '{{.Image}}' "$container_id")"
    echo 'Previous image/profile restored stopped; start only when its schema is compatible.'
fi
