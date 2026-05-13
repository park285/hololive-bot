# PATTERN-07 — DB repository 분리

## 권장 구조

query build, row scan, transaction operation, post-processing을 분리합니다.

## 불변조건

- SQL text 의미 유지.
- bind parameter order 유지.
- transaction begin/commit/rollback order 유지.
- row scan column order 유지.
- error wrapping 유지.
