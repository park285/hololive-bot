#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
files_helper="${repo_root}/scripts/ci/local-ci-files.sh"
helper="${repo_root}/scripts/ci/local-ci-packages.sh"

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

setup_repo() {
  local workdir="$1"

  mkdir -p \
    "${workdir}/internal/workspace" \
    "${workdir}/cmd/probe" \
    "${workdir}/deploy/compose" \
    "${workdir}/shared-go/pkg/lib" \
    "${workdir}/hololive/hololive-shared/pkg/common" \
    "${workdir}/hololive/hololive-kakao-bot-go/internal/lib" \
    "${workdir}/hololive/hololive-kakao-bot-go/internal/app" \
    "${workdir}/scripts"

  git -C "${workdir}" init -q
  git -C "${workdir}" config user.email "test@example.invalid"
  git -C "${workdir}" config user.name "Local CI Test"

  printf 'package root\n' >"${workdir}/doc.go"
  printf 'package main\n' >"${workdir}/cmd/probe/main.go"
  printf 'package workspace\n' >"${workdir}/internal/workspace/a.go"
  printf 'package lib\n' >"${workdir}/shared-go/pkg/lib/a.go"
  printf 'package common\n' >"${workdir}/hololive/hololive-shared/pkg/common/a.go"
  printf 'package lib\n' >"${workdir}/hololive/hololive-kakao-bot-go/internal/lib/a.go"
  printf 'package app\n' >"${workdir}/hololive/hololive-kakao-bot-go/internal/app/a.go"
  printf '#!/usr/bin/env bash\n' >"${workdir}/scripts/run.sh"
  printf 'services: {}\n' >"${workdir}/deploy/compose/docker-compose.prod.yml"
  printf 'APP_ENV=production\n' >"${workdir}/.env.example"

  git -C "${workdir}" add -A
  git -C "${workdir}" commit -q -m base
}

run_scope() {
  local workdir="$1"
  local scope="$2"
  local base_ref="$3"

  (
    cd "${workdir}"

    GO_MODULES=(
      shared-go
      hololive/hololive-shared
      hololive/hololive-kakao-bot-go
    )
    source "${files_helper}"
    mapfile -t ROOT_GO_PACKAGES < <(root_go_package_patterns)
    WORKSPACE_GO_PACKAGES=(
      ./shared-go/...
      ./hololive/hololive-shared/...
      ./hololive/hololive-kakao-bot-go/...
    )
    GO_PACKAGES=()
    LOCAL_CI_GO_SCOPE="${scope}"
    BASE_REF="${base_ref}"
    HEAD_REF=HEAD

    source "${helper}"
    configure_go_packages
    printf '%s\n' "${GO_PACKAGES[@]}"
  )
}

expect_scope() {
  local label="$1"
  local actual="$2"
  local expected="$3"

  if [[ "${actual}" != "${expected}" ]]; then
    echo "unexpected package scope: ${label}" >&2
    diff -u <(printf '%s\n' "${expected}") <(printf '%s\n' "${actual}") >&2 || true
    exit 1
  fi
}

full_scope="$(printf '%s\n' \
  ./ \
  ./cmd/probe \
  ./internal/workspace \
  ./shared-go/... \
  ./hololive/hololive-shared/... \
  ./hololive/hololive-kakao-bot-go/...)"

root_scope="$(printf '%s\n' \
  ./ \
  ./cmd/probe \
  ./internal/workspace)"

workdir="${tmpdir}/all-scope"
setup_repo "${workdir}"
base_ref="$(git -C "${workdir}" rev-parse HEAD)"
expect_scope "all" "$(run_scope "${workdir}" all "${base_ref}")" "${full_scope}"

workdir="${tmpdir}/app-package"
setup_repo "${workdir}"
base_ref="$(git -C "${workdir}" rev-parse HEAD)"
printf 'const changed = true\n' >>"${workdir}/hololive/hololive-kakao-bot-go/internal/app/a.go"
expect_scope "changed app package" \
  "$(run_scope "${workdir}" changed "${base_ref}")" \
  "./hololive/hololive-kakao-bot-go/..."

workdir="${tmpdir}/runtime-library-package"
setup_repo "${workdir}"
base_ref="$(git -C "${workdir}" rev-parse HEAD)"
printf 'const changed = true\n' >>"${workdir}/hololive/hololive-kakao-bot-go/internal/lib/a.go"
expect_scope "changed runtime library package" \
  "$(run_scope "${workdir}" changed "${base_ref}")" \
  "./hololive/hololive-kakao-bot-go/..."

workdir="${tmpdir}/root-package"
setup_repo "${workdir}"
base_ref="$(git -C "${workdir}" rev-parse HEAD)"
printf 'const changed = true\n' >>"${workdir}/cmd/probe/main.go"
expect_scope "changed root package" "$(run_scope "${workdir}" changed "${base_ref}")" "${root_scope}"

workdir="${tmpdir}/shared-module"
setup_repo "${workdir}"
base_ref="$(git -C "${workdir}" rev-parse HEAD)"
printf 'const changed = true\n' >>"${workdir}/shared-go/pkg/lib/a.go"
expect_scope "changed shared module" "$(run_scope "${workdir}" changed "${base_ref}")" "${full_scope}"

workdir="${tmpdir}/go-mod"
setup_repo "${workdir}"
base_ref="$(git -C "${workdir}" rev-parse HEAD)"
printf 'module example.test/root\n' >"${workdir}/go.mod"
expect_scope "changed go.mod" "$(run_scope "${workdir}" changed "${base_ref}")" "${full_scope}"

workdir="${tmpdir}/scripts-only"
setup_repo "${workdir}"
base_ref="$(git -C "${workdir}" rev-parse HEAD)"
printf 'echo changed\n' >>"${workdir}/scripts/run.sh"
expect_scope "changed scripts only" "$(run_scope "${workdir}" changed "${base_ref}")" ""

workdir="${tmpdir}/compose-contract"
setup_repo "${workdir}"
base_ref="$(git -C "${workdir}" rev-parse HEAD)"
printf '# changed\n' >>"${workdir}/deploy/compose/docker-compose.prod.yml"
expect_scope "changed compose contract file" \
  "$(run_scope "${workdir}" changed "${base_ref}")" \
  "./hololive/hololive-shared/..."

workdir="${tmpdir}/env-example-contract"
setup_repo "${workdir}"
base_ref="$(git -C "${workdir}" rev-parse HEAD)"
printf 'NEW_KEY=\n' >>"${workdir}/.env.example"
expect_scope "changed .env.example contract file" \
  "$(run_scope "${workdir}" changed "${base_ref}")" \
  "./hololive/hololive-shared/..."

echo "ok: local-ci package scope tests passed"
