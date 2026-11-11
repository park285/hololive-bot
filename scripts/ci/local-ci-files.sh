#!/usr/bin/env bash

workspace_metadata_files() {
    {
        git ls-files --cached --others --exclude-standard \
            go.work go.work.sum \
            '*/go.mod' '*/go.sum'

        local module
        for module in "${GO_MODULES[@]}"; do
            case "${module}" in
                ../*)
                    printf '%s/go.mod\n%s/go.sum\n' "${module}" "${module}"
                    ;;
            esac
        done
    } | awk 'NF && !seen[$0]++'
}
