#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/go-tooling.sh"
source "${SCRIPT_DIR}/nilaway-inputs.sh"
source "${SCRIPT_DIR}/local-ci-nilaway.sh"

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT
ROOT_DIR="${work_dir}/module"
mkdir -p "${ROOT_DIR}/dep" "${ROOT_DIR}/consumer" "${ROOT_DIR}/independent"
cd "${ROOT_DIR}"
export GOWORK=off
export RUN_NILAWAY=true
export NILAWAY_PARALLEL=1
nilaway_bin="$(ensure_nilaway)"

owned_go_package_patterns() {
    printf '%s\n' ./consumer/... ./independent/...
}

cat >go.mod <<'GO'
module example.com/nilawayfixture

go 1.27.1
GO
cat >dep/value.go <<'GO'
package dep

func Value() *string { return new("safe") }
GO
cat >consumer/read.go <<'GO'
package consumer

import "example.com/nilawayfixture/dep"

func Read() int { return len(*dep.Value()) }
GO
cat >independent/read.go <<'GO'
package independent

func Read() int { return 1 }
GO

expect_pass() {
    local label="$1"
    shift
    if ! "$@" >"${work_dir}/output" 2>&1; then
        cat "${work_dir}/output" >&2
        echo "FAIL: ${label}" >&2
        exit 1
    fi
    echo "PASS: ${label}"
}

expect_failure() {
    local label="$1" diagnostic="$2"
    shift 2
    if "$@" >"${work_dir}/output" 2>&1; then
        cat "${work_dir}/output" >&2
        echo "FAIL: ${label} unexpectedly passed" >&2
        exit 1
    fi
    if ! grep -Fq "${diagnostic}" "${work_dir}/output"; then
        cat "${work_dir}/output" >&2
        echo "FAIL: ${label} failed without the expected diagnostic" >&2
        exit 1
    fi
    echo "PASS: ${label}"
}

standalone() {
    "${nilaway_bin}" -pretty-print ./consumer/... ./independent/...
}

expect_pass 'standalone baseline' standalone
expect_pass 'native driver cold success' check_nilaway
expect_pass 'native driver warm success' check_nilaway

# 성공했던 importer가 변경되지 않아도 dependency fact가 바뀌면 다시 실패해야 한다.
cat >dep/value.go <<'GO'
package dep

func Value() *string { return nil }
GO
expect_failure 'standalone cross-package nil flow' 'read.go' standalone
expect_failure 'dependency change invalidates successful analysis' 'read.go' check_nilaway
expect_failure 'warm failure remains blocking' 'read.go' check_nilaway

cat >dep/value.go <<'GO'
package dep

func Value() *string { return new("fixed") }
GO
expect_pass 'dependency repair invalidates the failed analysis' check_nilaway

# 별도 테스트 패키지의 진단도 원래 standalone과 동일하게 검사한다.
cat >independent/read_test.go <<'GO'
package independent_test

import "testing"

func TestNil(t *testing.T) {
    var value *string
    t.Log(*value)
}
GO
expect_failure 'standalone external test nil flow' 'read_test.go' standalone
expect_failure 'native driver external test nil flow' 'read_test.go' check_nilaway
NILAWAY_PARALLEL=2
expect_failure 'parallel driver preserves diagnostics' 'read_test.go' check_nilaway
rm independent/read_test.go

printf '\ninvalid Go source\n' >>independent/read.go
expect_failure 'compile errors remain blocking' 'expected declaration' check_nilaway

echo 'ok: local NilAway driver preserves diagnostics and cache invalidation'
