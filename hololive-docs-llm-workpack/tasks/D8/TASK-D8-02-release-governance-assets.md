# TASK-D8-02. Release governance assets 확장

## Phase

D8. PR/Release Governance

## 목표

`docs/architecture/release-governance-assets.txt`에 current 기준 핵심 문서를 추가합니다.

## 왜 필요한가

현재 release governance는 README, Project Map, deployment guide, release note template, PR template 등을 추적합니다. 새로 만든 contract/service/runbook 문서도 governance asset에 포함해야 합니다.

## 먼저 읽을 파일

- `docs/architecture/release-governance-assets.txt`
- `docs/current/PROJECT_MAP.md`
- `.github/pull_request_template.md`

## 수정 또는 생성할 파일

- `docs/architecture/release-governance-assets.txt`

## 작업 단계

1. SERVICE_OWNERSHIP, CONTRACT_MAP, ERROR_CONTRACT, QUEUE_AND_PUBSUB_CONTRACTS, DEPLOYMENT_BASELINE을 assets에 추가합니다.
2. 각 문서에 필요한 token을 선정합니다.
3. PR template에도 새 문서명이 포함되어 있는지 token으로 검사할 수 있게 합니다.
4. release governance assets gate를 실행합니다.

## 금지 사항

- release note template 본문을 이 task에서 바꾸지 마십시오.

## 완료 조건

- 새 current 핵심 문서가 release governance assets에 들어갑니다.
- release governance gate가 통과합니다.
- 문서 삭제나 rename이 CI에서 잡힐 수 있습니다.

## 검증 명령

```bash
./scripts/architecture/check-release-governance-assets.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D8-02만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
