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
rsync_manifest="$ROOT_DIR/scripts/deploy/ap-rsync-files.txt"
preview_checker="$ROOT_DIR/scripts/deploy/check-ap-rsync-preview.sh"
literal_dollar='$'
grep -Fxq 'hololive/hololive-api/VERSION' "$rsync_manifest" \
  || fail "AP source transfer must include the API release version"
grep -Fxq 'hololive/hololive-alarm-worker/VERSION' "$rsync_manifest" \
  || fail "AP source transfer must include the alarm worker release version"
[[ "$(grep -Ec '^hololive/hololive-alarm-worker/' "$rsync_manifest")" -eq 1 ]] \
  || fail "AP source transfer must limit alarm worker scope to its release version"
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
grep -Fq 'rollback_image_tag="hololive-youtube-collector:rollback-$change_id"' "$deploy_script" \
  || fail "AP cutover must name a release-specific rollback image tag"
grep -Fq "sudo -n docker tag '\$IMAGE_REF' '\$rollback_image_tag'" "$deploy_script" \
  || fail "AP cutover must preserve the previous runtime image before loading the candidate"
grep -Fq "printf '%s\\n' '\$rollback_image_tag' > '\$backup_dir/rollback-image-tag'" "$deploy_script" \
  || fail "AP cutover must record the rollback image tag in the release backup"
grep -Fq 'AP_ROLLBACK_TAG_KEEP="${AP_ROLLBACK_TAG_KEEP:-5}"' "$deploy_script" \
  || fail "AP cutover must define a bounded rollback image tag retention count"
grep -Fq '^rollback-[0-9]{8}T[0-9]{6}Z' "$deploy_script" \
  || fail "AP cutover must prune only auto-generated timestamped rollback image tags"
grep -Fq 'stale_rollback_tags=' "$deploy_script" \
  || fail "AP cutover must collect superseded rollback image tags for pruning"
grep -Fq '  "$image_archive"' "$deploy_script" \
  || fail "AP cutover must transfer the image archive to the AP host"
grep -Fq '"$(ap_rsync_target "./$image_remote_path")"' "$deploy_script" \
  || fail "AP cutover must transfer the image archive through the canonical AP transport owner"
grep -Fq "image_archive='\$backup_dir/hololive-youtube-collector-prod.tar'" "$deploy_script" \
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
grep -Fq 'stop_retired_producer_runtime' "$deploy_script" \
  || fail "AP cutover must stop leftover youtube-producer before starting collector"
if grep -Fq -- "--remove-orphans" "$deploy_script"; then
  fail "AP deploy must preserve independently managed services in the shared Compose project"
fi
grep -Fq "actual_revision=\\${literal_dollar}(docker inspect -f" "$deploy_script" \
  || fail "AP completion must inspect the live image revision label"
grep -Fq 'org.opencontainers.image.revision' "$deploy_script" \
  || fail "AP completion must inspect the OCI revision label"
grep -Fq "[[ \\\"\\${literal_dollar}actual_revision\\\" == \\\"\\${literal_dollar}expected_revision\\\" ]]" "$deploy_script" \
  || fail "AP completion must require an exact live image revision match"
grep -Fq "if [[ \"\$AP_RUNTIME_MODE\" != \"compose\" ]]; then" "$deploy_script" \
  || fail "Compose AP deploy must reject non-Compose hosts before resolving the release version"
grep -Fq 'check-ap-rsync-manifest.sh" "$FILES_FROM"' "$deploy_script" \
  || fail "AP deploy must validate the repository-relative source manifest before path translation"
grep -Fq 'check-ap-rsync-preview.sh" "$preview_file" "$REMOTE_REPO_DIR"' "$deploy_script" \
  || fail "AP deploy must validate itemized rsync records with the exact preview scope checker"
if grep -Fq 'check-ap-rsync-manifest.sh" "$rsync_files_from"' "$deploy_script"; then
  fail "AP deploy must not validate the remote-path-translated rsync manifest"
fi

allowed_preview="$fixture_root/allowed-preview.txt"
printf '%s\n' \
  '.d..t...... hololive-bot/hololive/hololive-alarm-worker/' \
  '<f.st...... hololive-bot/hololive/hololive-alarm-worker/VERSION' \
  >"$allowed_preview"
"$preview_checker" "$allowed_preview"

forbidden_preview="$fixture_root/forbidden-preview.txt"
printf '%s\n' \
  'cL+++++++++ hololive-bot/hololive/hololive-alarm-worker/VERSION -> ../shadow/hololive/hololive-alarm-worker/VERSION' \
  >"$forbidden_preview"
expect_failure "AP preview must reject a VERSION symlink record" "$preview_checker" "$forbidden_preview"

printf '%s\n' \
  '>f+++++++++ hololive-bot/shadow/hololive/hololive-alarm-worker/VERSION' \
  >"$forbidden_preview"
expect_failure "AP preview must reject a suffix-matching nested path" "$preview_checker" "$forbidden_preview"

printf '%s\n' \
  '>f+++++++++ hololive-bot/hololive/hololive-alarm-worker/secret.go' \
  >"$forbidden_preview"
expect_failure "AP preview must reject any other alarm worker child" "$preview_checker" "$forbidden_preview"

readiness_source_count="$(grep -Fc '. scripts/deploy/lib/ap-collector-readiness.sh' "$deploy_script")"
[[ "$readiness_source_count" -eq 1 ]] \
  || fail "AP readiness helper must be sourced exactly once"
awk '
  /^remote "set -euo pipefail$/ { remote_block += 1 }
  /cd ~\/hololive-bot/ { cwd_block = remote_block; cwd_line = NR }
  /\. scripts\/deploy\/lib\/ap-collector-readiness\.sh/ { source_block = remote_block; source_line = NR }
  /collector_readiness_validate/ { validate_block = remote_block; validate_line = NR }
  END {
    if (cwd_block == 0 || source_block == 0 || validate_block == 0 ||
        cwd_block != source_block || source_block != validate_block ||
        cwd_line >= source_line || source_line >= validate_line) {
      exit 1
    }
  }
' "$deploy_script" \
  || fail "AP readiness helper must be sourced from the repository in the same remote block that validates readiness"

echo "all AP Compose release version checks passed"
