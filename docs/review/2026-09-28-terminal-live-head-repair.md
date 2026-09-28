# 과거 종료 세션 대조 상태 복구 근거

결정: `DEC-20260928-terminal-live-head-alignment`. 계획: `PLN-20260928-terminal-live-head-repair`.

## T01 / V01 — 준비와 실제 검증

- read-only guard `on`을 증명한 hololive-osaka/holo-postgres/hololive 조회에서 정본 ENDED/head LIVE 24건을 확인했다. 모두 2026-08-15~16 기록이며 정본 ended_at이 마지막 positive 이후였다.
- head의 ended_at/end_reason/end candidate는 모두 비어 있었고 수정 범위 밖의 source/pending/dispatch 기록은 보존 대상으로 확정했다.
- 현재 loader는 정본 session 상태를 우선하고 이미 ENDED인 세션을 되살리지 않는다. 복구는 이 정본을 head에 반영할 뿐 새 외부 종료 관측을 주장하지 않는다.
- 21건의 보존 EXPLICIT_END에는 ended_at이 비어 있었다. 이들은 종료 시각을 제공한 근거로 사용하지 않았다. 초기 리뷰 과정에서 NULL 비교를 과거 시각으로 잘못 설명한 것은 정정했다.
- PostgreSQL 18.6 격리 테스트: 정확한 2행 갱신, 잘못된 count/digest 거절, 새 positive 뒤 전체 거절, 이미 적용된 snapshot 거절, 정본/비대상/dispatch 보존 통과.
- 테스트 초기 DB 생성 readiness 경합은 fixture에서 수정했다. 운영 DB에는 테스트 실패 중 어떤 쓰기도 하지 않았다.
- `check-stack-db-access-policy.sh`, `check-stack-projection-tables.sh` 및 diff 공백 검사 통과.

## T02 / AC01 / AC02 / V02 — 운영 적용

- selected gate strict passed/gate_passed=true 이후 적용했다.
- private 원본: `/tmp/terminal-live-head-repair-20260928/before.json` (상위 0700, 파일 0600).
- 원본 count=24, digest=`af2ccfe4d4d9794ec6ef00f2c7a26b85`.
- row lock 및 현재 snapshot 재검증 후 정확히 24개 head의 status/ended_at/updated_at만 변경했고 transaction exit=0으로 완료했다.
- 적용 후 digest=`b12669f6c86e9e4d5c9f3b25a89f7586`.
- 새 read-only 연결의 24행 대조: status=ENDED, head ended_at=원래 canonical ended_at, 나머지 모든 head 필드 동일. 정본 종료 시각 동일.
- receipt 및 후속 원본은 같은 private 디렉터리의 `apply-receipt.json`, `after.json`에 보존했다. rollback은 실행하지 않았다.
- Prometheus `hololive_youtube_collection_live_state_review_targets{reason="state_mismatch"}`=3 확인. 남은 3건은 head 없는 UPCOMING(2026-03-27~04-01 예정), availability 기록도 0건이다. 종료 상태를 추정하거나 새 head를 조작하지 않았다.
- Iris DEAD=18, Hololive quarantined=2를 보존했다. 적용 도구는 pending/source/dispatch 테이블을 쓰지 않으며 대상 테이블의 사용자 trigger/rule 0개도 사전 확인했다.
- 31/31 scrape up, PostgreSQL exporter 3개 pg_up=1. 관련 앱 readiness는 구 CA 재로딩 후 통과한 상태다.
- Fallback delta: none. 재발송·자동 retry·대체 종료 근거를 추가하지 않았다. Git publication 없음.
