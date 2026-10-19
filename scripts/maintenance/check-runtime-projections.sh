#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# libpq의 승인된 접속 설정만 사용한다. 비밀 파일을 직접 읽거나 접속 대상을 추측하지 않는다.
# 같은 RR snapshot에서 두 모델을 비교하고, 오류 시 부분 결과를 정상 진단으로 출력하지 않는다.
# 서비스 파일은 PGOPTIONS보다 우선하므로 제한을 접속 인자와 세션 SQL로 확정한다.
if ! result=$(psql -X -w -qAt --dbname='connect_timeout=5' -v ON_ERROR_STOP=1 \
    -c 'SET default_transaction_read_only = on' \
    -c "BEGIN ISOLATION LEVEL REPEATABLE READ READ ONLY;
        SET LOCAL statement_timeout = '10s';
        SET LOCAL lock_timeout = '1s';
        SET LOCAL idle_in_transaction_session_timeout = '15s';
        DO \$guard\$ BEGIN
            IF current_setting('transaction_read_only') <> 'on'
                OR current_setting('default_transaction_read_only') <> 'on'
                OR current_setting('statement_timeout')::interval <> interval '10 seconds'
                OR current_setting('lock_timeout')::interval <> interval '1 second'
                OR current_setting('idle_in_transaction_session_timeout')::interval <> interval '15 seconds'
            THEN
                RAISE EXCEPTION 'read-only or timeout guard required';
            END IF;
        END \$guard\$;
        SELECT current_setting('transaction_read_only');" \
    -f "${SCRIPT_DIR}/queries/collection_runtime_bundles_drift.sql" \
    -f "${SCRIPT_DIR}/queries/live_runtime_state_drift.sql" \
    -c 'COMMIT'); then
    echo '집계 정합 확인 실패: 조회 결과를 확정할 수 없습니다.' >&2
    exit 2
fi

mapfile -t rows <<<"${result}"
if (( ${#rows[@]} != 3 )) || [[ ${rows[0]} != on ]] \
    || [[ ! ${rows[1]} =~ ^[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+$ ]] \
    || [[ ! ${rows[2]} =~ ^[0-9]+\|[0-9]+\|[0-9]+$ ]]; then
    echo '집계 정합 확인 실패: 읽기 전용 guard 또는 결과 형식이 잘못되었습니다.' >&2
    exit 2
fi

IFS='|' read -r bundle_missing bundle_extra bundle_changed mapping_changed <<<"${rows[1]}"
IFS='|' read -r live_missing live_extra live_changed <<<"${rows[2]}"
printf 'transaction_read_only=on\n'
printf 'collection_runtime_bundles missing=%s extra=%s changed=%s mapping_changed=%s\n' \
    "${bundle_missing}" "${bundle_extra}" "${bundle_changed}" "${mapping_changed}"
printf 'live_runtime_state missing=%s extra=%s changed=%s\n' \
    "${live_missing}" "${live_extra}" "${live_changed}"

if [[ ${rows[1]} != '0|0|0|0' || ${rows[2]} != '0|0|0' ]]; then
    exit 1
fi
