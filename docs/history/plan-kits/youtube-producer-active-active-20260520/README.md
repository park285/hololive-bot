# YouTube Producer Active-Active 작업 패키지

상태: 2026-05-20 현재 `main` 기준 재판정 후 작성한 LLM 실행용 plan-kit

## 목적

이 패키지는 과거 작업문서의 `youtube-scraper active-active` 요구사항을 현재 정본 런타임인 `youtube-producer` 기준으로 재정리하고, 남은 운영 검증과 보강 작업을 phase별로 나눕니다.

핵심 결론:

- 코드 기준 active-active 핵심은 `hololive-youtube-producer`와 shared poller에 구현되어 있습니다.
- 운영 완료 판정은 아직 smoke, readiness, metrics, log, DB duplicate evidence가 필요합니다.
- 이후 LLM 작업자는 retired `youtube-scraper` 또는 `hololive-stream-ingester` YouTube 경로를 기준으로 작업하면 안 됩니다.

## 읽기 순서

1. `READ.md`
2. `phases/00_INDEX.md`
3. `phases/phase-00-state-freeze-and-evidence.md`
4. `phases/phase-01-local-regression-gates.md`
5. `phases/phase-02-osaka-smoke-and-operational-evidence.md`
6. `phases/phase-03-proactive-valkey-readiness.md`
7. `phases/phase-04-photo-sync-failover-policy.md`
8. `phases/phase-05-two-scheduler-regression-test.md`
9. `phases/phase-06-readiness-metrics-and-ttl-docs.md`
10. `appendix/evidence-template.md`
11. `prompts/LLM_WORKER_PROMPT.md`
12. `prompts/PHASE_PROMPTS.md`

## 패키지 구성

- `READ.md`: LLM 작업자가 반드시 먼저 읽어야 하는 경계, 명칭, 금지사항, 성공 기준
- `phases/`: phase별 상세 작업문서
- `prompts/`: LLM 작업자에게 그대로 붙여넣을 수 있는 공통/phase별 prompt
- `appendix/`: 검증 증거 기록 템플릿

## 최종 성공 기준

운영 완료라고 말하려면 다음 증거가 모두 있어야 합니다.

- local compose render 성공
- targeted Go tests 성공
- deployment helper tests 성공
- Osaka 양쪽 AP `/ready`가 `mode=active-active`, `job_lease_enabled=true`, `valkey_available=true`, `scraping_paused=false`
- claim metrics에 `acquired`, `peer_owned`, `already_completed` 관측
- 양쪽 AP 로그에 rollout 이후 고위험 오류 없음
- 최근 30분 `youtube_notification_outbox` duplicate query 결과 `0 rows`

## 기존 요약 문서

초기 단일 요약 문서는 아래에 남아 있습니다. 실제 LLM 실행은 이 plan-kit을 우선 사용하세요.

- `docs/agent-workflows/plans/2026-05-20-youtube-producer-active-active-operationalization.md`
