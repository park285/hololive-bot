# 05. LLM 작업 프롬프트 모음

## 기본 프롬프트

```text
너는 hololive-bot 저장소 문서 안정화 작업을 수행하는 시니어 엔지니어다.
RPC/gRPC는 범위 밖이다.
아래 Task 문서 하나만 수행한다.
범위 밖 파일은 수정하지 않는다.
작업 후 변경 파일, 검증 명령, 남은 리스크를 보고한다.
```

## 문서 정리 작업 프롬프트

```text
현재 문서가 current/history/design 규칙을 지키는지 확인하고,
Task에 지정된 문서만 정리하라.
과거 문서는 삭제하지 말고 history로 이동하거나 bridge를 남겨라.
```

## Contract 문서 작업 프롬프트

```text
코드의 contracts package를 기준으로 문서화하라.
확인되지 않은 path, request, response, error code를 새로 만들지 마라.
확실하지 않은 항목은 '검토 필요'로 표시하라.
```

## Runbook 작업 프롬프트

```text
runtime별 runbook은 동일한 섹션 구조를 사용하라.
Role, Dependencies, Health, Ready, Logs, Metrics, Failure modes, Diagnosis, Mitigation, Rollback, Smoke test를 포함하라.
```

## CI gate 작업 프롬프트

```text
새 gate는 기존 ci-boundary-gate.sh 스타일을 따른다.
처음부터 과도하게 깨뜨릴 수 있는 검사는 warn 모드와 fail 모드를 분리하라.
스크립트는 set -euo pipefail을 사용하라.
```
