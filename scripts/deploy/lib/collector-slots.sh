#!/usr/bin/env bash

collector_slots_file() {
    local dir=.
    [[ "${BASH_SOURCE[0]}" != */* ]] || dir="${BASH_SOURCE[0]%/*}"
    printf '%s\n' "${COLLECTOR_SLOTS_FILE:-$(cd "${dir:-/}" && pwd -P)/collector-slots.tsv}"
}

collector_slot() {
    local slot="$1" column="$2" file="${3:-$(collector_slots_file)}"
    awk -F '\t' -v slot="$slot" -v column="$column" '
        NR == 1 { for (i = 1; i <= NF; i++) if ($i == column) field = i; width = NF; next }
        NF != width { exit 2 }
        $1 == slot { rows++; value = $field }
        END { if (!field || rows != 1) exit 1; print (value == "-" ? "" : value) }
    ' "$file"
}

collector_slots_where() {
    local column="$1" value="$2" file="${3:-$(collector_slots_file)}"
    awk -F '\t' -v column="$column" -v value="$value" '
        NR == 1 { for (i = 1; i <= NF; i++) if ($i == column) field = i; next }
        field && $field == value { print $1 }
        END { if (!field) exit 1 }
    ' "$file"
}

collector_slot_preamble() {
    local slot="$1" file="${2:-$(collector_slots_file)}"
    awk -F '\t' -v slot="$slot" '
        NR == 1 { width = NF; for (i = 1; i <= NF; i++) name[i] = $i; next }
        NF != width { exit 2 }
        $1 == slot { rows++; for (i = 1; i <= NF; i++) value[i] = ($i == "-" ? "" : $i) }
        END {
            if (rows != 1) exit 1
            for (i = 1; i <= width; i++) {
                if (name[i] !~ /^[a-z_]+$/ || value[i] !~ /^[A-Za-z0-9@._\/-]*$/) exit 3
                print "slot_" name[i] "=" value[i]
            }
        }
    ' "$file"
}

collector_slot_matches() {
    local slot="$1" file="$2" pairs pair name
    pairs="$(awk -F '\t' -v slot="$slot" '
        NR == 1 { width = NF; for (i = 1; i <= NF; i++) { name[i] = $i; field[$i] = i }; next }
        NF != width { exit 2 }
        $1 == slot { rows++; for (i = 1; i <= NF; i++) value[i] = ($i == "-" ? "" : $i) }
        END {
            if (rows != 1) exit 1
            for (i = 1; i <= width; i++) print name[i] "\t" value[field[name[i]]]
        }
    ' "$file")" || return 1
    while IFS= read -r pair; do
        name="slot_${pair%%$'\t'*}"
        [[ "${!name-}" == "${pair#*$'\t'}" ]] || return 1
    done <<< "$pairs"
}
