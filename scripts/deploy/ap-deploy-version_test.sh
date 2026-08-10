#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT_DIR/scripts/deploy/lib/ap-compose-version.sh"

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

expect_failure() {
  local label="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    fail "$label"
  fi
}

fixture_root="$(mktemp -d)"
trap 'rm -rf "$fixture_root"' EXIT
mkdir -p "$fixture_root/hololive/hololive-api"

printf '2.0.46\n' > "$fixture_root/VERSION"
printf '2.0.46\n' > "$fixture_root/hololive/hololive-api/VERSION"
[[ "$(ap_compose_release_version "$fixture_root")" == "2.0.46" ]] \
  || fail "matching release versions must resolve to HOLO_API_VERSION"

printf '9.8.7\n' > "$fixture_root/VERSION"
[[ "$(ap_compose_release_version "$fixture_root")" == "2.0.46" ]] \
  || fail "independent root/runtime releases must use hololive-api VERSION"

printf 'release-2.0.46\n' > "$fixture_root/hololive/hololive-api/VERSION"
expect_failure "invalid runtime VERSION must fail closed" ap_compose_release_version "$fixture_root"

deploy_script="$ROOT_DIR/scripts/deploy/ap-deploy.sh"
literal_dollar='$'
[[ "$(grep -Fc "sudo -n env HOLO_API_VERSION='\$HOLO_API_VERSION'" "$deploy_script")" -eq 3 ]] \
  || fail "every remote sudo Compose config/up invocation must propagate HOLO_API_VERSION"
[[ "$(grep -Fc "HOLO_API_VERSION='\$HOLO_API_VERSION' REVISION='\$REVISION'" "$deploy_script")" -eq 2 ]] \
  || fail "post-rsync Compose config/up must propagate the exact source revision"
if grep -Eq 'compose\.sh .* build( |$)' "$deploy_script"; then
  fail "AP Compose deploy must not invoke a remote Compose build"
fi
grep -Fq "docker info --format '{{.Architecture}}'" "$deploy_script" \
  || fail "AP cutover must resolve the target architecture from a read-only remote Docker preflight"
grep -Fq 'docker buildx build' "$deploy_script" \
  || fail "AP cutover must build the producer image on the build host with buildx"
grep -Fq "  --platform \"\$TARGET_PLATFORM\"" "$deploy_script" \
  || fail "AP build must target the resolved AP platform"
grep -Fq '  --load' "$deploy_script" \
  || fail "AP build must load the verified image into the build host image store"
grep -Fq "  --build-arg \"REVISION=\$REVISION\"" "$deploy_script" \
  || fail "AP build must pass the exact source revision"
grep -Fq "built_revision=\"${literal_dollar}(docker image inspect -f" "$deploy_script" \
  || fail "AP build must inspect the locally built image revision label"
grep -Fq '[[ "$built_revision" == ' "$deploy_script" \
  || fail "AP build must require the exact locally built image revision"
grep -Fq 'docker save --output' "$deploy_script" \
  || fail "AP cutover must archive the verified producer image for transfer"
grep -Fq 'rollback_image_tag="hololive-youtube-producer:rollback-$change_id"' "$deploy_script" \
  || fail "AP cutover must name a release-specific rollback image tag"
grep -Fq "sudo -n docker tag '\$IMAGE_REF' '\$rollback_image_tag'" "$deploy_script" \
  || fail "AP cutover must preserve the previous runtime image before loading the candidate"
grep -Fq "printf '%s\\n' '\$rollback_image_tag' > '\$backup_dir/rollback-image-tag'" "$deploy_script" \
  || fail "AP cutover must record the rollback image tag in the release backup"
grep -Fq '  "$image_archive"' "$deploy_script" \
  || fail "AP cutover must transfer the image archive to the AP host"
grep -Fq '"ubuntu@$AP_SSH_HOST:~/$image_remote_path"' "$deploy_script" \
  || fail "AP cutover must transfer the image archive into the remote backup workspace"
grep -Fq "image_archive='\$backup_dir/hololive-youtube-producer-prod.tar'" "$deploy_script" \
  || fail "AP cutover must load the image archive from the remote backup workspace"
grep -Fq 'docker load --input' "$deploy_script" \
  || fail "AP cutover must load the transferred producer image on the AP host"
grep -Fq "loaded_revision=\\${literal_dollar}(sudo -n docker image inspect -f" "$deploy_script" \
  || fail "AP cutover must inspect the loaded image revision label"
grep -Fq "loaded_platform=\\${literal_dollar}(sudo -n docker image inspect -f" "$deploy_script" \
  || fail "AP cutover must inspect the loaded image platform"
build_line="$(grep -n "docker buildx build" "$deploy_script" | cut -d: -f1)"
built_verify_line="$(grep -nF "built_revision=\"${literal_dollar}(docker image inspect" "$deploy_script" | cut -d: -f1)"
save_line="$(grep -n "docker save --output" "$deploy_script" | cut -d: -f1)"
rollback_tag_line="$(grep -nF "sudo -n docker tag '\$IMAGE_REF' '\$rollback_image_tag'" "$deploy_script" | cut -d: -f1)"
transfer_line="$(grep -nF '  "$image_archive"' "$deploy_script" | cut -d: -f1)"
load_line="$(grep -nF "sudo -n docker load --input" "$deploy_script" | cut -d: -f1)"
up_line="$(grep -n "up -d --no-build" "$deploy_script" | cut -d: -f1)"
[[ "${build_line}" -lt "${built_verify_line}" && "${built_verify_line}" -lt "${save_line}" && \
  "${save_line}" -lt "${rollback_tag_line}" && "${rollback_tag_line}" -lt "${transfer_line}" && \
  "${transfer_line}" -lt "${load_line}" && \
  "${load_line}" -lt "${up_line}" ]] \
  || fail "AP image build, verification, transfer, load, and no-build up must remain ordered"
grep -Fq "up -d --no-build" "$deploy_script" \
  || fail "AP cutover must explicitly disable remote Compose builds"
grep -Fq "actual_revision=\\${literal_dollar}(docker inspect -f" "$deploy_script" \
  || fail "AP completion must inspect the live image revision label"
grep -Fq 'org.opencontainers.image.revision' "$deploy_script" \
  || fail "AP completion must inspect the OCI revision label"
grep -Fq "[[ \\\"\\${literal_dollar}actual_revision\\\" == \\\"\\${literal_dollar}expected_revision\\\" ]]" "$deploy_script" \
  || fail "AP completion must require an exact live image revision match"
grep -Fq "if [[ \"\$AP_RUNTIME_MODE\" != \"compose\" ]]; then" "$deploy_script" \
  || fail "Compose AP deploy must reject non-Compose hosts before resolving the release version"

echo "all AP Compose release version checks passed"
