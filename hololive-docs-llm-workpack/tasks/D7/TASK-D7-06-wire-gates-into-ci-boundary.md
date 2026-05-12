# TASK-D7-06. 문서 gate를 ci-boundary-gate에 연결

## Phase

D7. 문서 CI Gate

## 목표

새로 추가한 문서 gate들을 `scripts/architecture/ci-boundary-gate.sh`와 architecture workflow에 연결합니다.

## 왜 필요한가

gate가 존재만 하고 CI에서 실행되지 않으면 문서 기준을 강제할 수 없습니다.

## 먼저 읽을 파일

- `scripts/architecture/ci-boundary-gate.sh`
- `.github/workflows/architecture-gates.yml`
- `docs/current/architecture/ci-gates.md`

## 수정 또는 생성할 파일

- `scripts/architecture/ci-boundary-gate.sh`
- `.github/workflows/architecture-gates.yml`
- `docs/current/architecture/ci-gates.md`

## 작업 단계

1. D7-01~D7-05에서 만든 gate들을 ci-boundary-gate에 순서대로 추가합니다.
2. workflow 자체는 기존 boundary gate 실행 구조를 유지합니다.
3. CI summary에 새 gate 이름이 드러나도록 echo 메시지를 추가합니다.
4. ci-gates 문서에 실행 순서를 갱신합니다.

## 금지 사항

- 새 gate의 내부 로직을 이 task에서 크게 바꾸지 마십시오.
- workflow job 구조를 불필요하게 변경하지 마십시오.

## 완료 조건

- ci-boundary-gate가 새 문서 gate를 실행합니다.
- architecture-gates workflow에서 간접 실행됩니다.
- 로컬 실행 시 전체 gate가 통과합니다.

## 검증 명령

```bash
./scripts/architecture/ci-boundary-gate.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D7-06만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
