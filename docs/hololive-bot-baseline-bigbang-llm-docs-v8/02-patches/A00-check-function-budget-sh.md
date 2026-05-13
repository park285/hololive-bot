# A00 — check-function-budget.sh diff

```diff
diff --git a/scripts/architecture/check-function-budget.sh b/scripts/architecture/check-function-budget.sh
--- a/scripts/architecture/check-function-budget.sh
+++ b/scripts/architecture/check-function-budget.sh
@@
 python3 "${SCRIPT_DIR}/check-function-budget.py" \
-  --root "${ROOT_DIR}" \
-  --baseline "docs/architecture/go-function-budget-baseline.txt"
+  --root "${ROOT_DIR}" \
+  "$@"
```

의도는 두 가지입니다.

1. CI에서는 인자가 없으므로 strict 기본 mode로 실행됩니다.
2. local에서는 `--report-over-budget`, `--include-prefix`, `--output json` 같은 report 옵션을 사용할 수 있습니다.
