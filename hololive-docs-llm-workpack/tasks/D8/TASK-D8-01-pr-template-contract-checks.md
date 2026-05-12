# TASK-D8-01. PR template에 계약/문서 체크 추가

## Phase

D8. PR/Release Governance

## 목표

`.github/pull_request_template.md`에 계약, runtime, runbook 영향 체크리스트를 추가합니다.

## 왜 필요한가

현재 PR template은 architecture gate 실행을 묻지만, 계약 문서와 runbook 갱신 여부를 구체적으로 묻지 않습니다.

## 먼저 읽을 파일

- `.github/pull_request_template.md`
- `docs/current/CONTRACT_MAP.md`
- `docs/current/SERVICE_OWNERSHIP.md`

## 수정 또는 생성할 파일

- `.github/pull_request_template.md`

## 작업 단계

1. Contract / Runtime 문서 영향 섹션을 추가합니다.
2. 내부 API path/request/response/error 변경 여부를 묻습니다.
3. Queue/PubSub 변경 여부를 묻습니다.
4. Runtime ownership 변경 여부를 묻습니다.
5. Runbook 영향 여부를 묻습니다.
6. 해당 문서 갱신 여부를 체크리스트로 둡니다.

## 금지 사항

- CI workflow를 이 task에서 수정하지 마십시오.

## 완료 조건

- PR template에 계약/문서 영향 섹션이 있습니다.
- CONTRACT_MAP, SERVICE_OWNERSHIP, runbooks 갱신 여부를 묻습니다.
- 기존 architecture gate 체크는 유지됩니다.

## 검증 명령

```bash
./scripts/architecture/check-release-governance-assets.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D8-01만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
