#!/usr/bin/env bash
set -euo pipefail

readonly VIDEO_ID="${SHORTLINK_SMOKE_VIDEO_ID:-dQw4w9WgXcQ}"
readonly LISTENER_ORIGIN="${SHORTLINK_LISTENER_ORIGIN:-http://127.0.0.1:30101}"
readonly CENTRAL_ORIGIN="${SHORTLINK_CENTRAL_ORIGIN:-http://100.100.1.8:30192}"
readonly PUBLIC_ORIGIN="${SHORTLINK_PUBLIC_ORIGIN:-https://short.holoshi.com}"
readonly KAKAO_SCRAPER_USER_AGENT="facebookexternalhit/1.1; kakaotalk-scrap/1.0"
readonly EXPECTED_LOCATION="https://youtube.com/watch?v=${VIDEO_ID}"

tmp_dir="$(mktemp -d)"
cleanup() {
    rm -rf "${tmp_dir}"
}
trap cleanup EXIT

fail() {
    echo "[FAIL] $*" >&2
    exit 1
}

header_value() {
    local name="$1"
    local file="$2"
    awk -v name="${name}" '
        {
            field = $0
            sub(/:.*/, "", field)
        }
        tolower(field) == tolower(name) {
            value = $0
            sub(/^[^:]*:[[:space:]]*/, "", value)
            sub(/\r$/, "", value)
        }
        END { print value }
    ' "${file}"
}

probe() {
    local hop="$1"
    local case_name="$2"
    local origin="$3"
    local path="$4"
    local expected_status="$5"
    local user_agent="${6:-}"
    local headers="${tmp_dir}/${hop}-${case_name}.headers"
    local status
    local -a curl_args=(
        --silent
        --show-error
        --output /dev/null
        --dump-header "${headers}"
        --head
        --connect-timeout 5
        --max-time 10
        --write-out '%{http_code}'
    )

    if [[ -n "${user_agent}" ]]; then
        curl_args+=(--user-agent "${user_agent}")
    fi

    if ! status="$(curl "${curl_args[@]}" "${origin%/}${path}")"; then
        fail "${hop}/${case_name}: request failed"
    fi
    if [[ "${status}" != "${expected_status}" ]]; then
        fail "${hop}/${case_name}: status ${status}, expected ${expected_status}"
    fi

    local location
    location="$(header_value Location "${headers}")"
    if [[ "${expected_status}" == "302" ]]; then
        [[ "${location}" == "${EXPECTED_LOCATION}" ]] \
            || fail "${hop}/${case_name}: Location ${location:-<missing>}, expected ${EXPECTED_LOCATION}"
    elif [[ -n "${location}" ]]; then
        fail "${hop}/${case_name}: unexpected Location ${location}"
    fi

    local cache_control
    cache_control="$(header_value Cache-Control "${headers}")"
    [[ "${cache_control}" == "no-store, max-age=0" ]] \
        || fail "${hop}/${case_name}: Cache-Control ${cache_control:-<missing>}"

    local vary
    vary="$(header_value Vary "${headers}")"
    [[ "${vary,,}" == *"user-agent"* ]] \
        || fail "${hop}/${case_name}: Vary ${vary:-<missing>}"

    echo "[PASS] ${hop}/${case_name}: ${status}"
}

check_hop() {
    local hop="$1"
    local origin="$2"

    probe "${hop}" regular "${origin}" "/l/${VIDEO_ID}" 302
    probe "${hop}" kakao-scraper "${origin}" "/l/${VIDEO_ID}" 403 "${KAKAO_SCRAPER_USER_AGENT}"
    probe "${hop}" invalid "${origin}" /l/invalid 404
}

command -v curl >/dev/null 2>&1 || fail "curl is required"

echo "[SMOKE] short-link provider chain"
check_hop listener "${LISTENER_ORIGIN}"
check_hop central "${CENTRAL_ORIGIN}"
check_hop public "${PUBLIC_ORIGIN}"
echo "[PASS] short-link provider chain"
