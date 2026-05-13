# PATTERN-08 — Cache/queue 분리

## 권장 구조

key build, batch split, command execution, result mapping을 분리합니다.

## 불변조건

- key 이름 유지.
- TTL 유지.
- pipeline/transaction order 유지.
- retry delayed queue score 유지.
- claim/release semantics 유지.
