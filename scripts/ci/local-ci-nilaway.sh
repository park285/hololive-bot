#!/usr/bin/env bash

check_nilaway() {
    if [[ "${RUN_NILAWAY}" != "true" ]]; then
        echo "[LOCAL CI] Skip NilAway: RUN_NILAWAY=${RUN_NILAWAY}"
        echo
        return 0
    fi

    if ! has_go_packages; then
        echo "[LOCAL CI] Skip NilAway: no Go packages in scope"
        echo
        return 0
    fi

    local nilaway_parallel="${NILAWAY_PARALLEL:-1}"
    local nilaway_gomemlimit="${NILAWAY_GOMEMLIMIT:-10GiB}"
    validate_nilaway_parallel "${nilaway_parallel}" || return 1
    validate_nilaway_gomemlimit "${nilaway_gomemlimit}" || return 1
    local nilaway_bin
    nilaway_bin="$(ensure_nilaway)" || return 1

    echo "[LOCAL CI] NilAway: go vet -p ${nilaway_parallel} (${#GO_PACKAGES[@]} package patterns)"
    env GOMEMLIMIT="${nilaway_gomemlimit}" GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly" \
        go vet -p "${nilaway_parallel}" -vettool="${nilaway_bin}" -pretty-print \
        -include-errors-in-files="${ROOT_DIR}" "${GO_PACKAGES[@]}"
}
