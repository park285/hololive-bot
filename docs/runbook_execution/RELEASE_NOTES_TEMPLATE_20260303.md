<!-- release-governance-template-version: 2026-03-03.v1 -->
<!-- render helper: ./scripts/architecture/render-release-notes.sh -->

# 릴리즈 노트 양식 (M6)

- 릴리즈 버전: {{RELEASE_VERSION}}
- 릴리즈 일시: {{RELEASE_AT}}
- 작성자: {{AUTHOR}}
- 관련 PR/이슈: {{PR_LINK}}

## 변경 요약
- 기능:
- 버그 수정:
- 운영/설정 변경:

## 영향 범위
- 서비스/모듈:
- 사용자 영향:
- 데이터/마이그레이션:
- 배포 대상 환경:

## 리스크/롤백
- 주요 리스크:
- 대응책:
- 롤백 조건:
- 롤백 절차:

## 검증 로그
- 사전 검증:
- 배포 후 검증:
- 모니터링 지표/알람:
- 로그/증적 링크: {{CI_EVIDENCE_LINK}}
- CI 아티팩트 링크: {{CI_ARTIFACT_URL}}

## 아키텍처 게이트 증적
- [ ] `./scripts/architecture/ci-boundary-gate.sh` 성공
- [ ] M0/M1/M4/M6 통과
- [ ] CI 링크 또는 실행 로그 첨부

## 승인
- 검토자:
- 승인 일시:
