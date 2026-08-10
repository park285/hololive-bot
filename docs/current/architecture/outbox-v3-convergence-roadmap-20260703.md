# Outbox v3 수렴 로드맵 (2026-07-03)

2026-07 SQL 리뷰의 "outbox 3세대 공존 → v3 수렴" 지적에 대한 실행 계획.
**즉시 제거는 불가하다** — 세 계열은 같은 outbox의 신·구 버전이 아니라 도메인이 다른 병렬
파이프라인이고, 셋 다 프로덕션에서 유일 경로로 살아 있다. 이 문서가 수렴의 단계와 선행 조건을 고정한다.

## 현황 (2026-07-03 실코드 근거)

| 계열 | 테이블 | 도메인 | 상태 어휘 | 생산자 | 소비자 |
|---|---|---|---|---|---|
| v1 | `youtube_notification_outbox` + `youtube_notification_delivery` + `youtube_notification_delivery_telemetry` | YouTube 콘텐츠 알림 (LIVE/NEW_VIDEO/COMMUNITY_POST/NEW_SHORT) | 대문자 `PENDING/SENT/FAILED` | youtube-producer 폴러 (`poller/internal/batchrepo/repository_batch_writes.go:235`) | alarm-worker youtube outbox dispatcher (`workerapp/build_egress.go:163`, prod `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=true`) |
| v2 | `notification_delivery_outbox` | major-event / member-news 다이제스트 | 대문자 `PENDING/SENDING/SENT/FAILED` | LLM 플레인 스케줄러 (`majorevent/scheduler/notification_guard.go:51`, `membernews/scheduler/digest_helper.go:85`) | alarm-worker delivery dispatcher (`build_egress.go:69`) |
| v3 | `alarm_dispatch_events`/`_deliveries`/`_admin_actions`/`_event_collisions` | `!알람` 라이브·기념일 디스패치 원장 | 소문자 `shadowed/pending/retry/leased/sending/sent/dlq/quarantined/cancelled` | alarm 스케줄러 + celebration publisher (`dispatchoutbox/repository_insert.go:105,205`) | alarm-worker dispatch consumer (`build_egress.go:96`) |

- v1 dispatcher는 `YOUTUBE_OUTBOX_V3_HANDOFF_MODE=off|shadow|cutover`를 소유합니다. `shadow`는
  v3 `shadowed` row와 기존 direct send를 병행하고, `cutover`는 v3 `pending` 저장 성공 후 v1 delivery를
  완료 처리합니다. 따라서 cutover의 v1 `SENT`는 외부 발송 완료가 아니라 durable handoff 완료를 뜻하며,
  최종 발송 진실은 v3 delivery에 있습니다.
- v2 producer repository는 `DELIVERY_OUTBOX_V3_HANDOFF_MODE=off|shadow|cutover` adapter를 가집니다.
  `DeliveryDigestDispatchPayload`가 kind/period/message를 room-agnostic event로 저장하고 room은 delivery에만
  남습니다. pre-rendered message 호환은 legacy formatter 제거 전까지 유지하는 bounded compatibility seam이며,
  비교 전용 `shadowed` delivery는 14일 retention으로 제한됩니다.

## Phase 1 — v1 → v3 브리지 (SSOT 재정렬 플랜의 완성)

1. 기존 claim owner 안에서 `youtube_notification_outbox` 행을 `alarm_dispatch_events`(+deliveries)로
   변환 게시합니다. 소스 페이로드는 `YouTubeOutboxDispatchPayload`를 사용합니다.
2. v1 `SENT`는 cutover에서 handoff 완료를 뜻하며 발송 진실은 v3 원장으로 이동합니다.
3. 듀얼런: `shadow` 게시 ↔ 기존 dispatcher 발송을 DB와 `hololive_youtube_outbox_v3_handoff_total`로 대조합니다.
4. 컷오버: `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=true`, `YOUTUBE_OUTBOX_V3_HANDOFF_MODE=cutover`,
   `ALARM_DISPATCH_CONSUMER_ENABLED=true`를 함께 유지합니다.
5. 정리(별도 승인 필요한 파괴적 단계): v1 dispatcher 코드 서브트리
   (`youtube/outbox/internal/delivery/{dispatch,store}/`), compose/CI 게이트 플래그
   (`docker-compose.prod.yml:357`, `ci-notification-egress-gate.sh:112`), 그리고 outbox/delivery 테이블 DROP.
   `youtube_notification_delivery_telemetry`와 `youtube_content_alarm_tracking`은 감사·dedup으로
   존치 여부를 별도 판단(096에서 FK를 끊어 outbox와 수명 분리 완료).

## Phase 2 — v2 → v3 (스케줄러 재설계 동반)

1. major-event/member-news producer 산출을 `DeliveryDigestDispatchPayload` room-agnostic 이벤트로 handoff합니다.
   현재 pre-rendered message는 기존 formatter의 deterministic output을 보존하는 compatibility seam입니다.
2. `shadow`에서 `hololive_delivery_outbox_v3_handoff_total`과 room/kind/period 집합을 대조한 뒤 producer를
   `cutover`로 전환합니다.
3. 기존 backlog가 0이 된 뒤 `DELIVERY_DISPATCHER_ENABLED=false` 컷오버 → `notification_delivery_outbox` +
   `pkg/service/delivery/` 제거(096의 `idx_ndo_pending_due_created_id`는 이때 함께 소멸).

## Phase 3 — v3 원장 파티셔닝 (보존정책의 복잡도 정리)

deliveries에는 인덱스 10+개가 걸려 있어 100만 행 DELETE = 힙 100만 + 인덱스 엔트리 1,000만+ 정리 +
vacuum 후불이다. 파티션 DROP은 O(1). 월 단위 RANGE 파티셔닝(created_at) 전환 시 059의 상태별
retention 인덱스와 `alarm_dispatch_maintenance.go`의 배치 DELETE가 파티션 관리로 대체된다.
전환은 무중단 재작성(신 테이블 병행 + 스왑)이 필요한 별도 플랜.

## 지금 하지 않는 것

- v1/v2 테이블·코드의 물리 제거(위 컷오버 완료 전까지). 제거 대상 전수 목록은 Phase 1/2의 정리 단계에 명시.
- v3로의 강제 통합을 위한 스키마 개조(페이로드 check 완화 등) — room-agnostic 불변식은 유지한다.

## Legacy 제거 체크리스트

1. Shadow 기간의 v1/v2 handoff failure가 0이고 source identity별 row 집합이 일치합니다.
2. Cutover 후 legacy pending/failed backlog가 0이며 v3 pending/retry oldest age가 정상 범위입니다.
3. Rollback 창 동안 legacy 테이블과 dispatcher 코드를 유지합니다.
4. rollback 창 종료와 telemetry 보존 범위를 승인한 뒤에만 legacy 코드·flag·table DROP을 별도 변경으로 수행합니다.
