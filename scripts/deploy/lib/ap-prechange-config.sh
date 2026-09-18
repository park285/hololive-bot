#!/usr/bin/env bash

# token-free AP 전환 전 compose에만 허용한다. 후속 config 검사는 예외 없이 필수다.
ap_prechange_config() {
    local diagnostic status line key pattern
    diagnostic="$(mktemp)" || return 1
    if "$@" 2>"$diagnostic"; then
        rm -f "$diagnostic"
        return 0
    else
        status=$?
    fi
    line="$(cat "$diagnostic")"
    rm -f "$diagnostic"
    # Compose required-variable 진단 한 줄만 허용한다. 경로/권한/YAML/추가 오류는 유지한다.
    pattern='^error while interpolating [[:alnum:]_.]+(\[\])?: required variable ([A-Z_]+) is missing a value: ([A-Z_]+) is required$'
    if [[ ${#line} -le 1024 && "$line" != *$'\n'* && "$line" =~ $pattern ]]; then
        key="${BASH_REMATCH[2]}"
        if [[ "$key" == "${BASH_REMATCH[3]}" ]]; then
            case "$key" in
                IRIS_WEBHOOK_TOKEN|IRIS_BOT_TOKEN|SESSION_SECRET|ADMIN_PASS_BCRYPT)
                    echo "AP prechange compose config skipped: removed required variable ${key}; post-change config remains required" >&2
                    return 0
                    ;;
            esac
        fi
    fi
    printf '%s\n' "$line" >&2
    return "$status"
}
