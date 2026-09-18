#!/usr/bin/env bash

check_nilaway() {
    if [[ "${RUN_NILAWAY}" != "true" ]]; then
        echo "[LOCAL CI] Skip NilAway: RUN_NILAWAY=${RUN_NILAWAY}"
        echo
        return 0
    fi

    local packages=()
    mapfile -t packages < <(owned_go_package_patterns)
    if (( ${#packages[@]} == 0 )); then
        echo "[LOCAL CI] Skip NilAway: no owned Go packages in scope"
        echo
        return 0
    fi

    local nilaway_parallel="${NILAWAY_PARALLEL:-1}"
    local nilaway_gomemlimit="${NILAWAY_GOMEMLIMIT:-10GiB}"
    validate_nilaway_parallel "${nilaway_parallel}" || return 1
    validate_nilaway_gomemlimit "${nilaway_gomemlimit}" || return 1
    local nilaway_bin
    nilaway_bin="$(ensure_nilaway)" || return 1

    # 같은 고정 바이너리의 unitchecker 경로로 의존성 fact를 Go 캐시에 재사용한다.
    # 2026-07-04 OOM 뒤 제한한 분석 프로세스 상한(1/2)을 -p로 유지한다.
    # vet는 패키지별 cwd로 실행하므로 진단 범위를 저장소 루트로 고정해야 한다.
    echo "[LOCAL CI] NilAway: go vet -p ${nilaway_parallel} (${#packages[@]} package patterns)"
    env GOMEMLIMIT="${nilaway_gomemlimit}" GOFLAGS="${GOFLAGS:+${GOFLAGS} }-mod=readonly" \
        go vet -p "${nilaway_parallel}" -vettool="${nilaway_bin}" -pretty-print \
        -include-errors-in-files="${ROOT_DIR}" "${packages[@]}"
}
