# PATTERN-04 — Parser/formatter 분리

## 권장 구조

Parser는 normalize/tokenize/dispatch/parse branch로 나눕니다. Formatter는 header/body/item/footer helper로 나눕니다.

## 불변조건

- command alias 유지.
- whitespace와 newline 의미 유지.
- sort order 유지.
- fallback label 유지.
- emoji key/value 유지.
