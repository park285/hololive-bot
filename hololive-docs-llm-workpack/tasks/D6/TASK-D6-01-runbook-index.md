# TASK-D6-01. Runbook index 재정리

## Phase

D6. Runbook

## 목표

`docs/current/runbooks/README.md`를 runtime별 runbook 중심으로 재정리합니다.

## 왜 필요한가

현재 runbook index는 일부 운영 문서와 YouTube observation 문서 중심입니다. 7 runtime 각각의 장애 대응 문서가 index에서 바로 보여야 합니다.

## 먼저 읽을 파일

- `docs/current/runbooks/README.md`
- `docs/current/PROJECT_MAP.md`

## 수정 또는 생성할 파일

- `docs/current/runbooks/README.md`

## 작업 단계

1. runtime별 runbook 섹션을 추가합니다.
2. infra runbook 섹션을 추가합니다.
3. release/rollback/DLQ replay runbook 섹션을 추가합니다.
4. 기존 YouTube observation 문서는 별도 observation/archive 섹션으로 분리합니다.
5. 새 runbook 파일이 아직 없으면 후속 task에서 생성할 것임을 명시하되, 깨진 링크를 만들지 않도록 순서를 조정합니다.

## 금지 사항

- 개별 runtime runbook 본문을 이 task에서 작성하지 마십시오.

## 완료 조건

- Runbook index가 runtime별 구조를 갖습니다.
- Project Map의 runbook link와 일치할 준비가 됩니다.
- 기존 runbook 링크가 사라지지 않습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D6-01만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
