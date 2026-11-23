#!/bin/sh
set -eu
umask 077

mode="${1:-}"
case "$mode" in
    health|observe)
        [ "$#" -eq 4 ] || { echo 'valkey-runtime: expected password file, CA file and TLS port' >&2; exit 64; }
        exec <"$2"
        set -- valkey-cli -e --raw --tls --cacert "$3" --askpass --user health -h 127.0.0.1 -p "$4"
        if [ "$mode" = health ]; then
            exec "$@" ping
        fi
        info="$("$@" info persistence)"
        enabled=""
        carriage_return="$(printf '\r')"
        printf '%s\n' "$info" | {
            while IFS=: read -r key value; do
                value="${value%"$carriage_return"}"
                case "$key" in
                    aof_enabled)
                        [ -z "$enabled" ] || exit 1
                        case "$value" in 0|1) enabled="$value" ;; *) exit 1 ;; esac
                        ;;
                esac
            done
            [ -n "$enabled" ] || exit 1
            printf '%s\n' "$enabled"
        }
        ;;
    *)
        echo 'valkey-runtime: expected health or observe' >&2
        exit 64
        ;;
esac
