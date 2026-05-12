# TASK-D2-01. Service Ownership 인덱스 추가

## Phase

D2. 서비스 소유권

## 목표

`docs/current/SERVICE_OWNERSHIP.md`를 만들어 7개 runtime의 소유권을 한눈에 정리합니다.

## 왜 필요한가

runtime split은 완료되었지만, 현재 소유권을 정리한 운영 기준 문서가 부족합니다. handoff 문서는 이력이고, 현재 기준 문서가 필요합니다.

## 먼저 읽을 파일

- `docs/current/PROJECT_MAP.md`
- `docs/current/RUNTIME_SPLIT_HANDOFF_20260416.md`
- `runtime_split_multimodule_contract_test.go`

## 수정 또는 생성할 파일

- `docs/current/SERVICE_OWNERSHIP.md`
- `docs/current/README.md`

## 작업 단계

1. 7개 runtime 각각에 대해 Owns, Provides, Consumes, Must not own을 표로 정리합니다.
2. 각 runtime별 상세 service 문서 링크를 둡니다.
3. runtime split handoff의 결론은 요약하되, handoff 문서를 현재 기준으로 복사하지 않습니다.
4. 불명확한 경계는 '검토 필요'로 표시합니다.
5. current README에 등록합니다.

## 금지 사항

- 각 서비스 상세 문서를 이 task에서 완성하지 마십시오.
- 코드 import 경계를 이 task에서 변경하지 마십시오.

## 완료 조건

- SERVICE_OWNERSHIP.md가 생성됩니다.
- runtime 7개가 모두 포함됩니다.
- 각 runtime에 상세 service doc 링크가 있습니다.
- 현재와 과거 이력이 섞이지 않습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D2-01만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
