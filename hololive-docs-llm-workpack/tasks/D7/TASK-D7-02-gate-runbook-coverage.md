# TASK-D7-02. 문서 gate 추가: runbook-coverage

## Phase

D7. 문서 CI Gate

## 목표

`scripts/architecture/check-runbook-coverage.sh`를 추가하여 Project Map runtime별 runbook link 존재 검사를 수행합니다.

## 왜 필요한가

문서가 단단하려면 사람이 기억해서 갱신하는 것이 아니라 CI가 불일치를 잡아야 합니다.

## 먼저 읽을 파일

- `scripts/architecture/ci-boundary-gate.sh`
- `docs/current/PROJECT_MAP.md`
- `docs/current/README.md`

## 수정 또는 생성할 파일

- `scripts/architecture/check-runbook-coverage.sh`
- `docs/current/architecture/ci-gates.md`

## 작업 단계

1. 기존 architecture gate 스크립트 스타일을 따릅니다.
2. `set -euo pipefail`을 사용합니다.
3. root path 계산은 기존 스크립트와 동일하게 작성합니다.
4. 처음부터 너무 공격적인 검사는 warning mode와 fail mode를 분리합니다.
5. ci-gates 문서에 새 gate 목적과 실패 조건을 기록합니다.

## 금지 사항

- ci-boundary-gate에 연결하는 것은 TASK-D7-06에서 수행합니다.
- 문서 내용을 이 task에서 대규모 수정하지 마십시오.

## 완료 조건

- `check-runbook-coverage.sh`가 생성됩니다.
- 로컬에서 실행 가능합니다.
- ci-gates 문서에 등록됩니다.
- 오탐을 줄이기 위한 예외 규칙이 문서화됩니다.

## 검증 명령

```bash
./scripts/architecture/check-runbook-coverage.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D7-02만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
