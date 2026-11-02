#!/bin/sh
set -eu
umask 077

mode="${1:-}"
case "$mode" in
    server|sentinel)
        [ "$#" -eq 3 ] || { echo 'valkey-runtime: expected mode, seed and runtime config paths' >&2; exit 64; }
        seed="$2"
        runtime="$3"
        [ -f "$seed" ] && [ ! -L "$seed" ] && [ -r "$seed" ] || { echo 'valkey-runtime: seed config is not a readable regular file' >&2; exit 1; }
        [ ! -L "$runtime" ] || { echo 'valkey-runtime: runtime config must not be a symlink' >&2; exit 1; }
        if [ ! -e "$runtime" ]; then
            # 첫 기동만 seed를 복사한다. 재시작 때 Sentinel이 저장한 역할·주소를 덮어쓰지 않는다.
            temporary="$(mktemp "${runtime}.XXXXXX")"
            trap 'rm -f "$temporary"' EXIT HUP INT TERM
            cat "$seed" > "$temporary"
            mv "$temporary" "$runtime"
            trap - EXIT HUP INT TERM
        fi
        [ -f "$runtime" ] && [ -w "$runtime" ] || { echo 'valkey-runtime: runtime config must be writable by the service user' >&2; exit 1; }
        if [ "$mode" = sentinel ]; then
            exec valkey-server "$runtime" --sentinel
        fi
        exec valkey-server "$runtime"
        ;;
    health|observe)
        [ "$#" -eq 4 ] || { echo 'valkey-runtime: expected password file, CA file and TLS port' >&2; exit 64; }
        # 암호는 자식 프로세스 환경에만 넘기며 인자·로그에는 넣지 않는다.
        REDISCLI_AUTH="$(cat "$2")"
        export REDISCLI_AUTH
        set -- valkey-cli -e --raw --tls --cacert "$3" --user health -h 127.0.0.1 -p "$4"
        if [ "$mode" = health ]; then
            exec "$@" ping
        fi
        # 원문은 출력하지 않고 완전한 영속성 상태만 숫자로 내보낸다.
        info="$("$@" info persistence)"
        enabled="" write_status="" rewrite_status=""
        carriage_return="$(printf '\r')"
        while IFS=: read -r key value; do
            value="${value%"$carriage_return"}"
            case "$key" in
                aof_enabled) enabled="$value" ;;
                aof_last_write_status) write_status="$value" ;;
                aof_last_bgrewrite_status) rewrite_status="$value" ;;
            esac
        done <<EOF
$info
EOF
        case "$enabled" in 0|1) ;; *) exit 1 ;; esac
        case "$write_status" in ok|err) ;; *) exit 1 ;; esac
        case "$rewrite_status" in ok|err) ;; *) exit 1 ;; esac
        healthy=0
        if [ "$enabled" = 1 ] && [ "$write_status" = ok ] && [ "$rewrite_status" = ok ]; then
            healthy=1
        fi
        printf '%s %s\n' "$enabled" "$healthy"
        ;;
    *)
        echo 'valkey-runtime: expected server, sentinel or health' >&2
        exit 64
        ;;
esac
