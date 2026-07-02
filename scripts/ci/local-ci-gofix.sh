#!/usr/bin/env bash
# go fix drift 는 (Go 소스 트리들, go 버전, 패키지 스코프)의 순수 함수라 동일 키 재실행을 건너뛴다.
# 키를 못 만드는 상황(비추적/unstaged Go 변경, sibling dirty, git 실패)은 빈 출력 → 무조건 실행.
go_fix_memo_repo_tree() {
    local porcelain
    porcelain="$(git -C "${ROOT_DIR}" status --porcelain 2>/dev/null)" || return 0
    if [[ -z "${porcelain}" ]]; then
        git -C "${ROOT_DIR}" rev-parse 'HEAD^{tree}' 2>/dev/null || true
        return 0
    fi
    local go_unstaged
    go_unstaged="$(git -C "${ROOT_DIR}" status --porcelain -- '*.go' 'go.mod' 'go.sum' 'go.work' 'go.work.sum' | grep -v '^[ACDMRT]  ' || true)"
    if [[ -n "${go_unstaged}" ]]; then
        return 0
    fi
    git -C "${ROOT_DIR}" write-tree 2>/dev/null || true
}

go_fix_memo_sibling_tree() {
    local dir="$1"
    local porcelain
    porcelain="$(git -C "${dir}" status --porcelain 2>/dev/null)" || return 0
    if [[ -n "${porcelain}" ]]; then
        return 0
    fi
    git -C "${dir}" rev-parse 'HEAD^{tree}' 2>/dev/null || true
}

go_fix_memo_key() {
    local repo_tree
    repo_tree="$(go_fix_memo_repo_tree)"
    if [[ -z "${repo_tree}" ]]; then
        return 0
    fi
    local shared_tree="-"
    local iris_tree="-"
    if grep -q '../shared-go' "${ROOT_DIR}/go.work"; then
        shared_tree="$(go_fix_memo_sibling_tree "${SHARED_GO_WORKSPACE_PATH:-${ROOT_DIR}/../shared-go}")"
        if [[ -z "${shared_tree}" ]]; then
            return 0
        fi
    fi
    if grep -q '../iris-client-go' "${ROOT_DIR}/go.work"; then
        iris_tree="$(go_fix_memo_sibling_tree "${IRIS_CLIENT_GO_WORKSPACE_PATH:-${ROOT_DIR}/../iris-client-go}")"
        if [[ -z "${iris_tree}" ]]; then
            return 0
        fi
    fi
    printf '%s|%s|%s|%s|%s' "${repo_tree}" "${shared_tree}" "${iris_tree}" "$(go version)" "${GO_PACKAGES[*]}"
}

go_fix_memo_stamp_file() {
    local git_dir
    git_dir="$(git -C "${ROOT_DIR}" rev-parse --absolute-git-dir 2>/dev/null)" || return 0
    if [[ -n "${git_dir}" ]]; then
        printf '%s/gate-cache/go-fix-drift.ok' "${git_dir}"
    fi
}

check_go_fix() {
    local tmp_dir
    local tmp_parent
    tmp_parent="${LOCAL_CI_TMPDIR:-${ROOT_DIR}/.tmp/local-ci}"
    local iris_client_go_dir
    iris_client_go_dir="${IRIS_CLIENT_GO_WORKSPACE_PATH:-${ROOT_DIR}/../iris-client-go}"
    local tar_excludes=(
        --exclude=.git
        --exclude=.worktrees
        --exclude=.tasklists
        --exclude=.runlogs
        --exclude=.codex
        --exclude=.claude
        --exclude=.serena
        --exclude=.gemini
        --exclude=.tmp
        --exclude=./artifacts
        --exclude=./artifacts/*
        --exclude=./backups
        --exclude=./backups/*
        --exclude=./data
        --exclude=./data/*
        --exclude=./logs
        --exclude=./logs/*
        --exclude=./runtime-config
        --exclude=./runtime-config/*
        --exclude=target
        --exclude='*/target'
        --exclude='*/target/*'
        --exclude=node_modules
        --exclude='*/node_modules'
        --exclude='*/node_modules/*'
        --exclude='*.key'
        --exclude='*.pem'
        --exclude='*.p12'
        --exclude=.env
        --exclude='.env.*'
    )

    if ! has_go_packages; then
        echo "[LOCAL CI] Skip go fix drift: no Go packages in scope"
        return 0
    fi

    local memo_key
    local stamp_file
    memo_key="$(go_fix_memo_key)"
    stamp_file="$(go_fix_memo_stamp_file)"
    if [[ -n "${memo_key}" && -n "${stamp_file}" && -f "${stamp_file}" ]] \
        && [[ "$(cat "${stamp_file}")" == "${memo_key}" ]]; then
        echo "[LOCAL CI] Skip go fix drift: tree unchanged since last pass"
        return 0
    fi

    mkdir -p "${tmp_parent}"
    find "${tmp_parent}" -mindepth 1 -maxdepth 1 -type d -name 'go-fix.*' -mmin +60 -exec rm -rf {} +
    tmp_dir="$(mktemp -d "${tmp_parent%/}/go-fix.XXXXXX")"

    cleanup_go_fix_tmp() {
        [[ -n "${tmp_dir:-}" ]] && rm -rf "${tmp_dir}"
        trap - RETURN
    }
    trap cleanup_go_fix_tmp RETURN

    mkdir -p "${tmp_dir}/repo"
    if ! tar "${tar_excludes[@]}" -C "${ROOT_DIR}" -cf - . | tar -C "${tmp_dir}/repo" -xf -; then
        return 1
    fi

    local shared_go_dir
    shared_go_dir="${SHARED_GO_WORKSPACE_PATH:-${ROOT_DIR}/../shared-go}"
    if grep -q '../shared-go' "${ROOT_DIR}/go.work"; then
        if [[ ! -d "${shared_go_dir}" ]]; then
            echo "active shared-go workspace not found: ${shared_go_dir}" >&2
            return 1
        fi
        mkdir -p "${tmp_dir}/shared-go"
        if ! tar "${tar_excludes[@]}" -C "${shared_go_dir}" -cf - . | tar -C "${tmp_dir}/shared-go" -xf -; then
            return 1
        fi
    fi

    if grep -q '../iris-client-go' "${ROOT_DIR}/go.work"; then
        if [[ ! -d "${iris_client_go_dir}" ]]; then
            echo "active iris-client-go workspace not found: ${iris_client_go_dir}" >&2
            return 1
        fi
        mkdir -p "${tmp_dir}/iris-client-go"
        if ! tar "${tar_excludes[@]}" -C "${iris_client_go_dir}" -cf - . | tar -C "${tmp_dir}/iris-client-go" -xf -; then
            return 1
        fi
    fi

    if ! (cd "${tmp_dir}/repo" && go fix "${GO_PACKAGES[@]}"); then
        return 1
    fi

    local changed=()
    local file
    while IFS= read -r file; do
        if [[ -f "${tmp_dir}/repo/${file}" ]] && ! cmp -s "${ROOT_DIR}/${file}" "${tmp_dir}/repo/${file}"; then
            changed+=("${file}")
        fi
    done < <(go_source_files)

    if (( ${#changed[@]} > 0 )); then
        echo "go fix would update modern Go compatibility rewrites:" >&2
        printf ' - %s\n' "${changed[@]}" >&2
        echo "Run go fix on the listed packages/files and commit the result." >&2
        return 1
    fi

    if [[ -n "${memo_key}" && -n "${stamp_file}" ]]; then
        mkdir -p "$(dirname "${stamp_file}")"
        printf '%s' "${memo_key}" >"${stamp_file}"
    fi
}
