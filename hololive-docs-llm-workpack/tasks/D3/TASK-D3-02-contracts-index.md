# TASK-D3-02. contracts 인덱스 추가

## Phase

D3. 내부 계약 문서

## 목표

`docs/current/contracts/README.md`를 추가하여 계약 문서군을 탐색 가능하게 만듭니다.

## 왜 필요한가

Contract Map은 전체 지도이고, contracts README는 도메인별 계약 문서로 들어가는 관문입니다.

## 먼저 읽을 파일

- `docs/current/CONTRACT_MAP.md`
- `docs/current/README.md`

## 수정 또는 생성할 파일

- `docs/current/contracts/README.md`
- `docs/current/README.md`

## 작업 단계

1. contracts README에 각 계약 문서 링크를 추가합니다.
2. 계약 문서 작성 규칙을 요약합니다.
3. 계약 변경 PR이 수정해야 하는 파일 목록을 적습니다.
4. 계약 버전 변경 기준을 적습니다.

## 금지 사항

- 개별 계약 문서 본문을 이 task에서 작성하지 마십시오.

## 완료 조건

- contracts README가 생성됩니다.
- membernews, majorevent, trigger, alarm, settings, iris-boundary 문서 링크가 있습니다.
- 계약 변경 규칙이 명시됩니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D3-02만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
