#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT_DIR/scripts/ci/python-runtime.sh"
repo_python_init

CHECKER="$ROOT_DIR/scripts/architecture/check-crosscutting-guardrails.py"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

fail() {
  echo "[FAIL] $*" >&2
  exit 1
}

guarded_root="$TMP_DIR/guarded"
mkdir -p "$guarded_root"
cat > "$guarded_root/guarded.go" <<'GO'
package guarded

import "github.com/park285/shared-go/v2/pkg/panicguard"

func start() {
	go panicguard.Run(nil, panicguard.BackgroundTask, "guarded", func() {})
}
GO

guarded_output="$("$CI_PYTHON_BIN" "$CHECKER" --root "$guarded_root" --profile generic 2>&1)"
if [[ "$guarded_output" == *"background goroutine has no visible panic recovery wrapper"* ]]; then
  fail "direct panicguard.Run must not be confused with a local Run function"
fi

no_import_root="$TMP_DIR/no-import"
mkdir -p "$no_import_root"
cat > "$no_import_root/no_import.go" <<'GO'
package noimport

var panicguard = struct {
	Run func()
}{}

func start() {
	go panicguard.Run()
}
GO

if no_import_output="$("$CI_PYTHON_BIN" "$CHECKER" --root "$no_import_root" --profile generic --strict 2>&1)"; then
  fail "a local panicguard variable without the canonical import must fail strict validation"
fi
if [[ "$no_import_output" != *"background goroutine has no visible panic recovery wrapper"* ]]; then
  fail "no-import local panicguard variable must retain its diagnostic"
fi

shadow_root="$TMP_DIR/shadow"
mkdir -p "$shadow_root"
cat > "$shadow_root/shadow.go" <<'GO'
package shadow

import "github.com/park285/shared-go/v2/pkg/panicguard"

func start() {
	_ = panicguard.BackgroundTask
	panicguard := struct {
		Run func()
	}{}
	go panicguard.Run()
}
GO

if shadow_output="$("$CI_PYTHON_BIN" "$CHECKER" --root "$shadow_root" --profile generic --strict 2>&1)"; then
  fail "a local binding shadowing the canonical panicguard import must fail strict validation"
fi
if [[ "$shadow_output" != *"background goroutine has no visible panic recovery wrapper"* ]]; then
  fail "shadowed canonical panicguard import must retain its diagnostic"
fi

comment_spoof_root="$TMP_DIR/comment-spoof"
mkdir -p "$comment_spoof_root"
cat > "$comment_spoof_root/comment_spoof.go" <<'GO'
package commentspoof

import "example.com/foreign/panicguard"

/*
import "github.com/park285/shared-go/v2/pkg/panicguard"
*/

func start() {
	go panicguard.Run()
}
GO

if comment_spoof_output="$("$CI_PYTHON_BIN" "$CHECKER" --root "$comment_spoof_root" --profile generic --strict 2>&1)"; then
  fail "a canonical import path inside a block comment must not authorize a foreign panicguard import"
fi
if [[ "$comment_spoof_output" != *"background goroutine has no visible panic recovery wrapper"* ]]; then
  fail "block-comment import spoof must retain its diagnostic"
fi

misleading_root="$TMP_DIR/misleading"
mkdir -p "$misleading_root"
cat > "$misleading_root/misleading.go" <<'GO'
package misleading

func start() {
	go worker.Run(panicguard.Run)
}
GO

if misleading_output="$("$CI_PYTHON_BIN" "$CHECKER" --root "$misleading_root" --profile generic --strict 2>&1)"; then
  fail "panicguard.Run in an argument must not authorize another direct target"
fi
if [[ "$misleading_output" != *"background goroutine has no visible panic recovery wrapper"* ]]; then
  fail "misleading direct goroutine must retain its diagnostic"
fi

local_run_root="$TMP_DIR/local-run"
mkdir -p "$local_run_root"
cat > "$local_run_root/local_run.go" <<'GO'
package localrun

type runner struct{}

func (runner) Run() {}

func start(r runner) {
	go r.Run()
}
GO

if local_run_output="$("$CI_PYTHON_BIN" "$CHECKER" --root "$local_run_root" --profile generic --strict 2>&1)"; then
  fail "local Run method must fail strict validation"
fi
if [[ "$local_run_output" != *"background goroutine has no visible panic recovery wrapper"* ]]; then
  fail "local Run method must retain its diagnostic"
fi

echo "[PASS] cross-cutting direct goroutine guard fixtures"
