# TASK-D8-03. LLM 작업 규칙 문서 추가

## Phase

D8. PR/Release Governance

## 목표

`docs/current/architecture/llm-work-rules.md`를 추가하여 LLM이 문서/계약 작업을 할 때 지켜야 할 규칙을 명시합니다.

## 왜 필요한가

LLM은 context가 부족하면 current/history를 섞거나, 확인되지 않은 계약을 확정처럼 쓰기 쉽습니다. 작업 규칙을 문서화해야 반복 품질이 올라갑니다.

## 먼저 읽을 파일

- `docs/current/architecture/README.md`
- `docs/current/CONTRACT_MAP.md`
- `docs/current/SERVICE_OWNERSHIP.md`
- `03_GLOBAL_RULES.md`

## 수정 또는 생성할 파일

- `docs/current/architecture/llm-work-rules.md`
- `docs/current/architecture/README.md`

## 작업 단계

1. LLM에게 제공해야 할 최소 context를 적습니다.
2. 범위 밖 변경 금지 규칙을 적습니다.
3. 확인되지 않은 항목은 '검토 필요'로 표시하는 규칙을 적습니다.
4. 문서 작업 결과 보고 형식을 적습니다.
5. architecture README에 등록합니다.

## 금지 사항

- 저장소 루트의 AGENTS.md를 새로 만들지 마십시오. 별도 요청이 있을 때만 만듭니다.

## 완료 조건

- llm-work-rules.md가 생성됩니다.
- architecture README에서 발견 가능합니다.
- LLM 작업 prompt에 바로 사용할 수 있는 규칙이 있습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D8-03만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
