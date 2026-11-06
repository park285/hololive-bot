#!/usr/bin/env bash

compose_env_validate_file_format() {
    local env_file="$1"

    awk '
        /^[[:space:]]*(#|$)/ { next }

        /^[[:space:]]*export[[:space:]]+/ {
            print "[ERROR] Compose env file must not contain leading export: " FILENAME ":" NR > "/dev/stderr"
            exit 1
        }

        /[`]|[$][(]/ {
            print "[ERROR] Compose env file must not contain command substitution: " FILENAME ":" NR > "/dev/stderr"
            exit 1
        }

        index($0, "=") == 0 {
            print "[ERROR] Compose env file line must be KEY=VALUE: " FILENAME ":" NR > "/dev/stderr"
            exit 1
        }

        {
            line = $0
            sub(/\r$/, "", line)
            if (line ~ /[[:cntrl:]]/) {
                print "[ERROR] Compose env file line must not contain control characters: " FILENAME ":" NR > "/dev/stderr"
                exit 1
            }

            key = substr(line, 1, index(line, "=") - 1)
            raw_key = key
            sub(/^[[:space:]]+/, "", key)
            sub(/[[:space:]]+$/, "", key)

            if (key != raw_key) {
                print "[ERROR] Env key must not contain surrounding whitespace: " FILENAME ":" NR ":" raw_key > "/dev/stderr"
                exit 1
            }
            if (key !~ /^[A-Za-z_][A-Za-z0-9_]*$/) {
                print "[ERROR] Invalid env key: " FILENAME ":" NR ":" key > "/dev/stderr"
                exit 1
            }
            if (seen[key]++) {
                print "[ERROR] Duplicate env key: " FILENAME ":" NR ":" key > "/dev/stderr"
                exit 1
            }
        }
    ' "${env_file}"
}

compose_env_list_keys_from_file() {
    local env_file="$1"

    awk '
        /^[[:space:]]*(#|$)/ { next }
        {
            line = $0
            sub(/\r$/, "", line)
            if (index(line, "=") == 0) {
                next
            }
            key = substr(line, 1, index(line, "=") - 1)
            sub(/^[[:space:]]+/, "", key)
            sub(/[[:space:]]+$/, "", key)
            if (key != "") {
                print key
            }
        }
    ' "${env_file}" | sort -u
}

compose_env_read_value_from_file() {
    local env_file="$1"
    local key="$2"
    local value=""

    value="$(awk -v k="${key}" '
        /^[[:space:]]*(#|$)/ { next }
        {
            line = $0
            sub(/\r$/, "", line)
            if (index(line, "=") == 0) {
                next
            }
            key = substr(line, 1, index(line, "=") - 1)
            sub(/^[[:space:]]+/, "", key)
            sub(/[[:space:]]+$/, "", key)
            if (key == k) {
                value = substr(line, index(line, "=") + 1)
            }
        }
        END { print value }
    ' "${env_file}")"

    value="${value%$'\r'}"
    if [[ "${value}" == \"*\" && "${value}" == *\" ]]; then
        value="${value#\"}"
        value="${value%\"}"
    elif [[ "${value}" == \'*\' && "${value}" == *\' ]]; then
        value="${value#\'}"
        value="${value%\'}"
    fi

    printf '%s\n' "${value}"
}

compose_env_key_exists_in_file() {
    local env_file="$1"
    local want="$2"
    compose_env_list_keys_from_file "${env_file}" | grep -qx -- "${want}"
}
