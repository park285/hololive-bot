<!-- release-governance-template-version: 2026-03-03.v1 -->

# PR 요약

- 변경 목적:
- 주요 변경점:
- 리스크/롤백:

## 검증

- [ ] 로컬 테스트/검증 실행
- [ ] 관련 서비스 로그/메트릭 확인

## Architecture Gate / Release 체크리스트 (아키텍처·릴리즈 영향 PR 필수)

- [ ] `./scripts/architecture/ci-boundary-gate.sh` 실행 성공
- [ ] 필수 게이트(M0/M1/M4/M6) 통과 확인
- [ ] 실행 로그 또는 CI 성공 링크를 본 PR에 첨부
- [ ] 릴리즈 노트 작성 시 `docs/runbook_execution/RELEASE_NOTES_TEMPLATE_20260303.md` 사용
