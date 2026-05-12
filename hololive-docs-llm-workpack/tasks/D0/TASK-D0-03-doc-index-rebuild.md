# TASK-D0-03. 문서 인덱스 재구성

## Phase

D0. 문서 기준선 복구

## 목표

`docs/README.md`, `docs/current/README.md`, `docs/current/architecture/README.md`, `docs/current/runbooks/README.md`를 새 구조에 맞춰 정리합니다.

## 왜 필요한가

문서가 많아질수록 인덱스가 SSOT 역할을 해야 합니다. 현재 인덱스는 일부 문서만 나열하고, contract/service/runbook 문서군의 위치가 아직 없습니다.

## 먼저 읽을 파일

- `docs/README.md`
- `docs/current/README.md`
- `docs/current/architecture/README.md`
- `docs/current/runbooks/README.md`

## 수정 또는 생성할 파일

- `docs/README.md`
- `docs/current/README.md`
- `docs/current/architecture/README.md`
- `docs/current/runbooks/README.md`

## 작업 단계

1. `docs/README.md`에 current/history/design 분류 규칙을 명확히 유지합니다.
2. `docs/current/README.md`에 새로 만들 문서군 placeholder 또는 실제 링크를 추가합니다.
3. `docs/current/architecture/README.md`에 문서 governance gate를 추가 예정 항목으로 등록합니다.
4. `docs/current/runbooks/README.md`는 runtime별 runbook을 기준으로 재정렬합니다.
5. 링크가 아직 없는 문서는 생성 예정으로 쓰지 말고 후속 task에서 생성한 뒤 연결합니다.

## 금지 사항

- 각 runbook 본문을 이 task에서 작성하지 마십시오.
- Contract Map 본문을 이 task에서 작성하지 마십시오.

## 완료 조건

- 문서 인덱스만 봐도 current, history, design의 역할이 명확합니다.
- runtime별 runbook 섹션이 준비됩니다.
- contract 문서군 위치가 명확합니다.
- 깨진 링크가 없습니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D0-03만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
