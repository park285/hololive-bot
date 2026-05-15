# PATTERN-10 — External API client 분리

## 권장 구조

request build, HTTP call, response decode, validation, pagination/scan을 분리합니다.

## 불변조건

- URL/path/query 유지.
- header 유지.
- timeout/context 유지.
- retry/fallback 유지.
- response field mapping 유지.
