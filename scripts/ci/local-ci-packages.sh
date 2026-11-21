#!/usr/bin/env bash

require_checkout_head() {
    [[ "$1" != HEAD ]] || return 0
    local expected current
    expected="$(git rev-parse --verify "$1^{commit}")" || return 1
    current="$(git rev-parse --verify HEAD)" || return 1
    if [[ "${expected}" != "${current}" ]]; then
        echo "gate target $1 does not match the current checkout" >&2
        return 1
    fi
}

checkout_changed_paths() {
    git diff --no-renames --name-only -z "$1" -- &&
        git diff --no-renames --name-only -z --cached HEAD -- &&
        git diff --no-renames --name-only -z -- &&
        git ls-files --others --exclude-standard -z
}

load_changed_files() {
    mapfile -d '' -t changed_files < <(checkout_changed_paths "$1")
    wait "$!"
}

is_metadata_path() {
    case "$1" in
        hololive/*/queries/*|hololive/*/testqueries/*|hololive/*/testdata/*) return 1 ;;
        *.md|VERSION|hololive/hololive-api/VERSION|hololive/hololive-alarm-worker/VERSION) return 0 ;;
        *) return 1 ;;
    esac
}

configure_go_packages() {
    local file module
    local -A selected=()
    mapfile -t GO_PACKAGES < <(go_workspace_package_patterns)
    wait "$!"
    case "${LOCAL_CI_GO_SCOPE}" in
        all) return 0 ;;
        changed) ;;
        *)
            echo "unsupported LOCAL_CI_GO_SCOPE=${LOCAL_CI_GO_SCOPE}; expected all or changed" >&2
            return 1
            ;;
    esac

    for file in "${changed_files[@]}"; do
        if is_metadata_path "${file}"; then
            continue
        fi
        case "${file}" in
            go.work|go.work.sum|go.mod|go.sum|*/go.mod|*/go.sum|.golangci.yml|\
            scripts/ci/go-*.sh|scripts/ci/local-ci*.sh|scripts/ci/nilaway-inputs.sh|scripts/ci/nilaway-models/*|scripts/ci/pre-push-gate.sh|\
            hololive/hololive-shared/*|hololive/hololive-dbtest/*|hololive/hololive-api/scripts/migrations/*)
                return 0
                ;;
            hololive/hololive-youtube-collector/*)
                selected[hololive/hololive-api]=1 ;;
            docs/current/contracts/admin-api.openapi.json|scripts/maintenance/queries/live_runtime_state_drift.sql)
                selected[hololive/hololive-api]=1 ;;
            deploy/nginx/holoshi-public-shortlink.conf|deploy/nginx/admin-dashboard-ingress.conf.template)
                selected[hololive/hololive-shared]=1 ;;
        esac
        case "${file}" in
            hololive/hololive-api/*/queries/*|hololive/hololive-youtube-collector/*/queries/*|\
            deploy/quadlet/hololive-bot/*|scripts/maintenance/queries/*)
                selected[hololive/hololive-dbtest]=1 ;;
            scripts/ci/public-pr-collector-helper-install.sh|scripts/ci/public-pr-go-gate.sh|\
            scripts/ci/check-go-test-json.py|scripts/ci/collector-test-skip-allowlist.txt)
                selected[hololive/hololive-youtube-collector]=1 ;;
        esac
        for module in "${GO_OWNED_MODULES[@]}"; do
            if [[ "${file}" == "${module}/"* ]]; then
                selected[${module}]=1
                break
            fi
        done
    done

    GO_PACKAGES=()
    for module in "${GO_OWNED_MODULES[@]}"; do
        if [[ -n "${selected[${module}]:-}" ]]; then
            GO_PACKAGES+=("./${module}/...")
        fi
    done
}

has_go_packages() {
    (( ${#GO_PACKAGES[@]} > 0 ))
}