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

scripttest_packages_for() {
    case "$1" in
        scripts/deploy/lib/compose-env.sh|scripts/deploy/lib/compose-services.sh|scripts/deploy/lib/collector-slots.sh|\
        scripts/deploy/lib/collector-slots.tsv|scripts/deploy/lib/require-quic-udp-buffer.sh|scripts/deploy/lib/public-bind-mounts.sh|\
        deploy/nginx/*|scripts/deploy/lib/youtubejs-node-version.sh|scripts/deploy/lib/source-revision.sh|hololive/hololive-api/scripts/bot.sh)
            echo deployenv ;;
    esac
    case "$1" in
        build-all.sh|scripts/deploy/lib/source-revision.sh) echo buildscripts ;;
    esac
    case "$1" in
        scripts/logs/*) echo logscripts ;;
    esac
    case "$1" in
        scripts/runtime/set-iris-base-url.sh) echo runtimescripts ;;
    esac
    case "$1" in
        scripts/deploy/quadlet-prestart.sh|\
        scripts/deploy/lib/admin-bind.sh|scripts/deploy/lib/public-bind-mounts.sh|scripts/deploy/lib/compose-env.sh|\
        scripts/deploy/lib/postgres-capacity.sh|scripts/deploy/lib/collector-slots.sh|scripts/deploy/lib/collector-slots.tsv|\
        scripts/ci/postgres-capacity-policy.tsv|scripts/systemd/admin-dashboard-ingress.nft|scripts/deploy/testdata/ingress-firewall.json|\
        deploy/topology/hosts.conf|deploy/quadlet/hololive-bot/*)
            echo prestart ;;
    esac
    case "$1" in
        scripts/deploy/ap-completion-check.sh|scripts/deploy/ap-host-native-deploy.sh|scripts/deploy/ap-host-native-rollback.sh|\
        scripts/deploy/ap-hosts/*|scripts/deploy/lib/*|scripts/deploy/verify-exec-tree-ownership.sh|\
        scripts/logs/ap-host-native-status.sh|scripts/ci/postgres-capacity-policy.tsv|deploy/topology/hosts.conf)
            echo apnative ;;
    esac
    case "$1" in
        scripts/deploy/po-central-cutover.sh|scripts/deploy/po-central-files.txt|scripts/deploy/lib/*|deploy/quadlet/hololive-bot/*|\
        deploy/podman/iris-seoul/*|hololive/hololive-api/scripts/migrations/*|hololive/hololive-api/VERSION|\
        hololive/hololive-api/internal/migrationrunner/queries/lineage_head.sql|scripts/deploy/quadlet-prestart.sh|\
        scripts/ci/postgres-capacity-policy.tsv)
            echo pocentral ;;
    esac
    case "$1" in
        scripts/deploy/verify-exec-tree-ownership.sh|scripts/deploy/sync-opt-current.sh|scripts/deploy/quadlet-prestart.sh|\
        scripts/deploy/quadlet-redeploy-service.sh|scripts/deploy/quadlet-rollback-service.sh|scripts/deploy/lib/*|\
        scripts/ci/postgres-capacity-policy.tsv|scripts/systemd/*|deploy/*|hololive/hololive-api/scripts/init-db/*|\
        hololive/hololive-api/cmd/db-migrate/queries/bootstrap_writer_admission.sql)
            echo syncopt ;;
    esac
    case "$1" in
        scripts/deploy/quadlet-redeploy-service.sh|scripts/deploy/quadlet-rollback-service.sh|scripts/deploy/lib/*|\
        scripts/ci/postgres-capacity-policy.tsv|deploy/quadlet/*)
            echo quadlet ;;
    esac
    case "$1" in
        scripts/build/build-hololive-collector-go.sh|scripts/build/check-hololive-collector-go-artifact.sh|\
        scripts/ci/public-pr-go-gate.sh|scripts/ci/python-runtime.sh|scripts/ci/python-runner.sh|.python-version)
            echo collectorartifact ;;
    esac
    case "$1" in
        hololive/hololive-collector/Dockerfile|hololive/hololive-collector/Dockerfile.dockerignore|\
        hololive/hololive-collector/Dockerfile.po-sandbox|hololive/hololive-collector/Dockerfile.po-sandbox.dockerignore|\
        hololive/hololive-alarm-worker/Dockerfile|hololive/hololive-alarm-worker/Dockerfile.dockerignore|\
        hololive/hololive-collector/po-sandbox/package.json|hololive/hololive-collector/po-sandbox/package-lock.json|\
        hololive/hololive-collector/po-sandbox/src/*|hololive/hololive-collector/youtubejs/package.json|\
        hololive/hololive-collector/youtubejs/package-lock.json|hololive/hololive-collector/youtubejs/src/*|\
        hololive/hololive-collector/eventgrade/server.mjs|hololive/hololive-alarm-worker/xspaces/package.json|\
        hololive/hololive-alarm-worker/xspaces/package-lock.json|hololive/hololive-alarm-worker/xspaces/src/*)
            echo imagepermissions ;;
    esac
}

configure_go_packages() {
    local file module package
    local all_product=false all_scripttest=false
    local -A selected=() scripttest=()
    mapfile -t GO_PACKAGES < <(go_workspace_package_patterns)
    wait "$!"
    SCRIPTTEST_PACKAGES=(./...)
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
            scripts/scripttest/internal/*|scripts/scripttest/go.mod|scripts/scripttest/go.sum)
                all_scripttest=true
                continue
                ;;
            scripts/scripttest/*)
                package="${file#scripts/scripttest/}"
                scripttest[${package%%/*}]=1
                continue
                ;;
            go.work|go.work.sum|go.mod|go.sum|*/go.mod|*/go.sum|.golangci.yml|\
            scripts/ci/go-*.sh|scripts/ci/local-ci*.sh|scripts/ci/run-script-tests.sh|scripts/ci/nilaway-inputs.sh|scripts/ci/nilaway-models/*|scripts/ci/pre-push-gate.sh)
                return 0
                ;;
            hololive/hololive-shared/*|hololive/hololive-dbtest/*|hololive/hololive-api/scripts/migrations/*)
                all_product=true ;;
            hololive/hololive-collector/*)
                selected[hololive/hololive-api]=1 ;;
            docs/current/contracts/admin-api.openapi.json|scripts/maintenance/queries/live_runtime_state_drift.sql)
                selected[hololive/hololive-api]=1 ;;
            deploy/nginx/holoshi-public-shortlink.conf|deploy/nginx/admin-dashboard-ingress.conf.template)
                selected[hololive/hololive-shared]=1 ;;
        esac
        case "${file}" in
            hololive/hololive-api/*/queries/*|hololive/hololive-collector/*/queries/*|\
            deploy/quadlet/hololive-bot/*|scripts/maintenance/queries/*)
                selected[hololive/hololive-dbtest]=1 ;;
            scripts/ci/public-pr-collector-helper-install.sh|scripts/ci/public-pr-go-gate.sh|\
            scripts/ci/check-go-test-json.py|scripts/ci/collector-test-skip-allowlist.txt)
                selected[hololive/hololive-collector]=1 ;;
        esac
        for module in "${GO_PRODUCT_MODULES[@]}"; do
            if [[ "${file}" == "${module}/"* ]]; then
                selected[${module}]=1
                break
            fi
        done
        while read -r package; do
            scripttest[${package}]=1
        done < <(scripttest_packages_for "${file}")
    done

    GO_PACKAGES=()
    for module in "${GO_PRODUCT_MODULES[@]}"; do
        if [[ -n "${selected[${module}]:-}" || "${all_product}" == true ]]; then
            GO_PACKAGES+=("./${module}/...")
        fi
    done
    if [[ "${all_scripttest}" != true ]]; then
        SCRIPTTEST_PACKAGES=()
        for package in "${!scripttest[@]}"; do
            SCRIPTTEST_PACKAGES+=("./${package}/...")
        done
    fi
}

has_go_packages() {
    (( ${#GO_PACKAGES[@]} > 0 ))
}
