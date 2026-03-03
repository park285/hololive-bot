# Deprecated Deadline Gate Runbook

> 작성일: 2026-03-03

## 목적

`m6-gate` 실패 시(만료된 deprecated 제거 마커 존재) 운영자가 즉시 처리할 수 있도록 절차를 고정한다.

## 실패 조건

다음 패턴의 날짜가 현재 UTC 날짜보다 과거인 경우 실패한다.

- `TODO(YYYY-MM-DD)`
- `remove_after = "YYYY-MM-DD"`

검사 스크립트:

```bash
./scripts/architecture/check-rust-deprecated-deadline.sh
```

## 처리 절차

1. 실패 로그에서 만료된 파일/라인 확인
2. 해당 deprecated 경로를 우선 제거
   - 코드 제거가 어려운 경우, 최소 범위로 대체 경로를 먼저 이식
3. 제거 후 관련 테스트 실행
4. `./scripts/architecture/m6-gate.sh` 재실행
5. `./scripts/architecture/ci-boundary-gate.sh` 최종 통과 확인

## 예외 처리 원칙

- 날짜만 연장하는 PR 금지(근거 없는 연장 차단)
- 불가피한 연장은 아래를 반드시 포함
  - 연장 사유
  - 대체 완료 목표일
  - 제거 작업 이슈/마일스톤 링크

## 권장 커맨드

```bash
# 1) 단일 게이트 확인
./scripts/architecture/m6-gate.sh

# 2) 전체 경계 게이트 확인
./scripts/architecture/ci-boundary-gate.sh
```
