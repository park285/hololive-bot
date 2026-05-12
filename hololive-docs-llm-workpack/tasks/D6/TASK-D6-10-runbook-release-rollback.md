# TASK-D6-10. Release/Rollback runbook 추가

## Phase

D6. Runbook

## 목표

`docs/current/runbooks/release.md`와 `rollback.md`를 추가하여 runtime별 배포/롤백 기준을 문서화합니다.

## 왜 필요한가

현재 PR template과 release governance는 존재하지만, runtime별 문서 변경/계약 변경을 포함한 release 기준이 부족합니다.

## 먼저 읽을 파일

- `docs/runbook_execution/DOCKER_COMPOSE_DEPLOYMENT_GUIDE.md`
- `.github/pull_request_template.md`
- `docs/architecture/release-governance-assets.txt`
- `docs/current/PROJECT_MAP.md`

## 수정 또는 생성할 파일

- `docs/current/runbooks/release.md`
- `docs/current/runbooks/rollback.md`
- `docs/current/runbooks/README.md`

## 작업 단계

1. Compose service 단위 재배포 기준을 요약합니다.
2. 문서/계약 변경 PR의 release note 기준을 작성합니다.
3. runtime별 rollback 명령 위치를 링크합니다.
4. 계약 변경 시 dual-read/dual-write 또는 compatibility 유지 원칙을 적습니다.
5. Runbook index에 등록합니다.

## 금지 사항

- 기존 deployment guide를 중복 작성하지 마십시오.
- 배포 스크립트를 수정하지 마십시오.

## 완료 조건

- release.md와 rollback.md가 생성됩니다.
- 계약 변경 시 release checklist가 포함됩니다.
- rollback 기준이 runtime별로 연결됩니다.

## 검증 명령

```bash
./scripts/architecture/check-project-map.sh
```

## LLM 작업 프롬프트

```text
Task TASK-D6-10만 수행하세요. 범위 밖 파일은 수정하지 마세요.
```
