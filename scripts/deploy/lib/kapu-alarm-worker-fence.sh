#!/usr/bin/env bash

compose_action_starts_alarm_worker() {
    local command_index="$1"
    shift
    local -a args=("$@")

    if (( command_index < 0 || command_index >= ${#args[@]} )); then
        return 1
    fi

    local command="${args[command_index]}"
    case "${command}" in
        up|start|restart|run) ;;
        *) return 1 ;;
    esac

    local no_deps=false
    local option_requires_value=false
    local after_separator=false
    local token=""
    local -a targets=()
    local index
    for ((index = command_index + 1; index < ${#args[@]}; index++)); do
        token="${args[index]}"
        if [[ "${option_requires_value}" == true ]]; then
            option_requires_value=false
            continue
        fi
        if [[ "${after_separator}" == true ]]; then
            targets+=("${token}")
            continue
        fi
        case "${token}" in
            --)
                after_separator=true
                ;;
            --no-deps)
                no_deps=true
                ;;
            --scale|--wait-timeout|--timeout|-t|--exit-code-from|--pull|--attach|--no-attach|--name|--entrypoint|-e|--env|-u|--user|-w|--workdir|-v|--volume|-p|--publish)
                option_requires_value=true
                ;;
            --scale=*|--wait-timeout=*|--timeout=*|--exit-code-from=*|--pull=*|--attach=*|--no-attach=*|--name=*|--entrypoint=*|--env=*|--user=*|--workdir=*|--volume=*|--publish=*)
                ;;
            -*)
                ;;
            *)
                targets+=("${token}")
                ;;
        esac
    done

    case "${command}" in
        up)
            if [[ ${#targets[@]} -eq 0 ]]; then
                return 0
            fi
            ;;
        start|restart)
            if [[ ${#targets[@]} -eq 0 ]]; then
                return 0
            fi
            no_deps=true
            ;;
        run)
            if [[ ${#targets[@]} -eq 0 ]]; then
                return 1
            fi
            targets=("${targets[0]}")
            ;;
    esac

    local target=""
    for target in "${targets[@]}"; do
        if [[ "${target}" == "hololive-alarm-worker" ]]; then
            return 0
        fi
        if [[ "${no_deps}" == false ]]; then
            case "${target}" in
                hololive-api|admin-dashboard|admin-dashboard-ingress)
                    return 0
                    ;;
            esac
        fi
    done
    return 1
}

assert_kapu_alarm_worker_start_allowed() {
    local host="$1"
    local command_index="$2"
    shift 2

    if [[ "${host}" != "kapu" ]]; then
        return 0
    fi
    if ! compose_action_starts_alarm_worker "${command_index}" "$@"; then
        return 0
    fi
    if [[ "${HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED:-}" == "1" ]]; then
        return 0
    fi

    echo "[ERROR] Refusing to start hololive-alarm-worker on build-control host kapu." >&2
    echo "        Set HOLOLIVE_KAPU_ALARM_WORKER_ROLLBACK_APPROVED=1 only for an explicitly approved rollback." >&2
    return 1
}
