<!-- release-governance-template-version: 2026-03-03.v1 -->

# PR 요약

- 변경 목적:
- 주요 변경점:
- 리스크/롤백:

## 검증

- [ ] 로컬 테스트/검증 실행
- [ ] 관련 서비스 로그/메트릭 확인

## 라벨/태그

- [ ] PR 라벨(태그) 설정 완료 (`bug` / `enhancement` / `documentation` 중 해당 항목)

## Architecture Gate / Release 체크리스트 (아키텍처·릴리즈 영향 PR 필수)

- [ ] `./scripts/architecture/ci-boundary-gate.sh` 실행 성공
- [ ] 필수 게이트(M0/M1/M2/M4/M6) 통과 확인
- [ ] 실행 로그 또는 CI 성공 링크를 본 PR에 첨부
- [ ] 릴리즈 노트 작성 시 `docs/runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md` 사용

## Contract / Runtime 문서 영향

- [ ] 내부 HTTP path/request/response/error 변경 여부 확인
- [ ] Queue/PubSub key/envelope/event/payload 변경 여부 확인
- [ ] Runtime ownership, compose service, port, health/readiness 변경 여부 확인
- [ ] Runbook 영향 여부 확인
- [ ] 변경이 있는 경우 `docs/current/CONTRACT_MAP.md` 갱신
- [ ] 변경이 있는 경우 `docs/current/SERVICE_OWNERSHIP.md` 갱신
- [ ] 변경이 있는 경우 `docs/current/QUEUE_AND_PUBSUB_CONTRACTS.md` 또는 `docs/current/ERROR_CONTRACT.md` 갱신
- [ ] 변경이 있는 경우 `docs/current/runbooks/README.md` 및 runtime runbook 갱신
- [ ] 문서/계약 변경이 없으면 사유를 PR 설명에 기록
