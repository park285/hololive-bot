#!/usr/bin/env bash

dedupe_lines() {
    awk 'NF && !seen[$0]++'
}

all_go_package_patterns() {
    printf '%s\n' "${ROOT_GO_PACKAGES[@]}" "${WORKSPACE_GO_PACKAGES[@]}" | dedupe_lines
}

changed_paths() {
    local base_ref="${BASE_REF:-origin/main}"
    local head_ref="${HEAD_REF:-HEAD}"

    if git rev-parse --verify "${base_ref}" >/dev/null 2>&1 && git rev-parse --verify "${head_ref}" >/dev/null 2>&1; then
        git diff --name-only -M "${base_ref}...${head_ref}" 2>/dev/null || true
    elif git rev-parse --verify HEAD~1 >/dev/null 2>&1; then
        git diff --name-only -M HEAD~1..HEAD 2>/dev/null || true
    fi

    git diff --name-only -M --cached 2>/dev/null || true
    git diff --name-only -M 2>/dev/null || true
    git ls-files --others --exclude-standard 2>/dev/null || true
}

go_package_patterns_for_file() {
    local file="$1"
    local module

    [[ "${file}" == *.go ]] || return 0

    for module in "${GO_MODULES[@]}"; do
        if [[ "${file}" == "${module}/"* ]]; then
            printf './%s/...\n' "${module}"
            return 0
        fi
    done

    printf '%s\n' "${ROOT_GO_PACKAGES[@]}"
}

is_shared_module_file() {
    local file="$1"

    case "${file}" in
        shared-go/*|hololive/hololive-shared/*)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

changed_go_package_patterns() {
    local file
    local needs_all=false
    local package_patterns=()

    while IFS= read -r file; do
        case "${file}" in
            go.work|go.work.sum|go.mod|go.sum|*/go.mod|*/go.sum)
                needs_all=true
                ;;
            *.go)
                if is_shared_module_file "${file}"; then
                    needs_all=true
                else
                    while IFS= read -r package_pattern; do
                        package_patterns+=("${package_pattern}")
                    done < <(go_package_patterns_for_file "${file}")
                fi
                ;;
        esac
    done < <(changed_paths | dedupe_lines)

    if [[ "${needs_all}" == "true" ]]; then
        all_go_package_patterns
        return 0
    fi

    printf '%s\n' "${package_patterns[@]}" | dedupe_lines
}

configure_go_packages() {
    case "${LOCAL_CI_GO_SCOPE}" in
        all)
            mapfile -t GO_PACKAGES < <(all_go_package_patterns)
            ;;
        changed)
            mapfile -t GO_PACKAGES < <(changed_go_package_patterns)
            ;;
        *)
            echo "unsupported LOCAL_CI_GO_SCOPE=${LOCAL_CI_GO_SCOPE}; expected all or changed" >&2
            exit 1
            ;;
    esac
}

has_go_packages() {
    (( ${#GO_PACKAGES[@]} > 0 ))
}
