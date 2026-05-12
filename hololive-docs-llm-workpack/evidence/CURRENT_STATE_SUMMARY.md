# Evidence summary

이 문서는 작업팩 작성 시 확인한 현재 상태 근거입니다.

## Current runtime 기준

- `docs/current/PROJECT_MAP.md`: 7 runtime을 현재 기준으로 정리합니다.
- `go.work`: admin-api, alarm-worker, dispatcher-go, bot, llm-sched, shared, stream-ingester, shared-go가 포함됩니다.

## README 불일치

- 루트 `README.md`: 일부 설명이 5 runtime/6 module 기준입니다.
- 따라서 D0 작업에서 gateway 문서로 재정렬해야 합니다.

## 문서 계층

- `docs/README.md`: current/history/design 세 층을 선언합니다.
- `docs/current/README.md`: current는 현재 운영 기준 문서만 둔다고 설명합니다.
- 그러나 current 안에는 historical 성격 문서가 존재합니다.

## Contracts

이미 존재하는 주요 contracts package:

- `contracts/majorevent`
- `contracts/membernews`
- `contracts/trigger`
- `contracts/settings`
- `contracts/alarm`

## Gates

현재 architecture gate:

- `.github/workflows/architecture-gates.yml`
- `scripts/architecture/ci-boundary-gate.sh`

현재 M1 gate 일부:

- `check-go-alarm-contracts.sh`
- `check-go-trigger-route-hardcoding.sh`

확장 필요 gate:

- current historical 문서 검사
- runbook coverage 검사
- contract map 검사
- internal route hardcoding 일반화
- error contract 검사
