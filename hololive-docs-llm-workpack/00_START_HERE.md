# 00. 시작 문서

## 목적

이 작업팩은 `hololive-bot`의 문서 체계를 “현재 운영 기준”과 “서비스 간 계약 관리” 중심으로 재정렬하기 위한 것입니다.

현재 저장소에는 이미 좋은 기반이 있습니다.

- `docs/current/PROJECT_MAP.md`는 7개 runtime을 현재 기준으로 정리합니다.
- `docs/README.md`는 문서를 `current / history / design` 세 층으로 관리한다고 선언합니다.
- architecture gate workflow와 `ci-boundary-gate.sh`가 존재합니다.
- `hololive-shared/pkg/contracts/*` 아래에 일부 계약 패키지가 존재합니다.
- runtime split이 완료되어 `admin-api`, `alarm-worker` 등이 별도 모듈로 분리되었습니다.

하지만 문서가 아직 단단하지 않습니다.

- 루트 `README.md`와 `docs/current/PROJECT_MAP.md`의 runtime/module 설명이 다릅니다.
- `docs/current` 안에 historical 성격의 문서가 섞여 있습니다.
- 계약 문서가 코드의 contracts 패키지 수준을 따라가지 못합니다.
- runtime별 runbook coverage가 부족합니다.
- CI gate가 문서/계약 전체를 강제하지 못합니다.

## 작업 원칙

이 작업팩은 RPC/gRPC 전환을 다루지 않습니다. 내부 HTTP JSON, Valkey queue, Pub/Sub, Iris boundary를 현재 구조로 유지하면서 문서와 계약 관리 체계를 강화합니다.

작업은 문서 우선입니다. 문서가 기준이 된 뒤, 필요한 코드 리팩토링은 별도 작업으로 넘깁니다.

## LLM에게 지시할 때

한 번에 전체 작업팩을 넣지 마십시오. 먼저 `00_START_HERE.md`, `01_REVIEW_FINDINGS.md`, 그리고 실행할 task 문서 하나만 전달하는 것이 좋습니다.

예시:

```text
아래 작업팩 기준으로 TASK-D0-01만 수행하세요.
범위 밖 파일은 수정하지 마세요.
완료 후 변경 파일, 검증 명령, 남은 리스크를 보고하세요.
```

## 성공 기준

최종적으로 다음이 가능해야 합니다.

- 루트 README만 읽어도 현재 runtime 구조가 틀리지 않음
- `docs/current`에는 현재 운영 기준만 있음
- runtime 7개가 모두 Project Map, Service Ownership, Runbook에 연결됨
- 내부 API/Queue/PubSub 계약이 Contract Map에 등록됨
- 문서 불일치가 CI에서 잡힘
- 계약 변경 PR이 문서 없이 머지되기 어려움
