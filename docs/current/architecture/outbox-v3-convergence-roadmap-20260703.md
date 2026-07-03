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

- v1→v3 브리지는 의도만 존재: `docs/superpowers/plans/2026-05-14-youtube-alarm-ssot-realignment.md`
  ("Final send truth lives in `alarm_dispatch_events`/`_deliveries`"). 도메인 계층에
  `AlarmDispatchSourceKindYouTubeOutbox` + `YouTubeOutboxDispatchPayload`(`pkg/domain/alarm_dispatch_source.go:12,21`)가
  준비돼 있으나 이를 채우는 브리지 워커는 미구현이고, v1은 여전히 `SENT`로 자체 종결한다.
- v2→v3 수렴 계획은 부재하며 데이터 모델이 비호환이다: v3 이벤트는 room-agnostic
  (058의 payload check가 room 키를 금지), v2는 room별 렌더 완료 메시지를 저장한다.

## Phase 1 — v1 → v3 브리지 (SSOT 재정렬 플랜의 완성)

1. 브리지 워커 구현: `youtube_notification_outbox` 클레임 행을 `alarm_dispatch_events`(+deliveries)로
   변환 게시. 소스 페이로드는 기존 `YouTubeOutboxDispatchPayload` 사용.
2. v1 종결 상태를 `SENT` 대신 handoff 상태로 전환(SSOT 플랜 원문 요구) — 발송 진실은 v3 원장으로 이동.
3. 듀얼런: 브리지 shadow 게시(v3 `shadowed`) ↔ 기존 dispatcher 발송을 텔레메트리로 대조.
4. 컷오버: `YOUTUBE_OUTBOX_DISPATCHER_ENABLED=false`, 브리지+v3 consumer가 유일 발송 경로.
5. 정리(별도 승인 필요한 파괴적 단계): v1 dispatcher 코드 서브트리
   (`youtube/outbox/internal/delivery/{dispatch,store}/`), compose/CI 게이트 플래그
   (`docker-compose.prod.yml:357`, `ci-notification-egress-gate.sh:112`), 그리고 outbox/delivery 테이블 DROP.
   `youtube_notification_delivery_telemetry`와 `youtube_content_alarm_tracking`은 감사·dedup으로
   존치 여부를 별도 판단(096에서 FK를 끊어 outbox와 수명 분리 완료).

## Phase 2 — v2 → v3 (스케줄러 재설계 동반)

1. major-event/member-news 스케줄러 산출을 room별 렌더 메시지에서 room-agnostic 이벤트로 재설계
   (렌더를 v3 delivery fan-out 단계로 이동).
2. v3 `kind`에 `MAJOR_EVENT_WEEKLY/MONTHLY`, `MEMBER_NEWS_WEEKLY/MONTHLY` 편입.
3. 듀얼런 → `DELIVERY_DISPATCHER_ENABLED=false` 컷오버 → `notification_delivery_outbox` +
   `pkg/service/delivery/` 제거(096의 `idx_ndo_pending_due_created_id`는 이때 함께 소멸).

## Phase 3 — v3 원장 파티셔닝 (보존정책의 복잡도 정리)

deliveries에는 인덱스 10+개가 걸려 있어 100만 행 DELETE = 힙 100만 + 인덱스 엔트리 1,000만+ 정리 +
vacuum 후불이다. 파티션 DROP은 O(1). 월 단위 RANGE 파티셔닝(created_at) 전환 시 059의 상태별
retention 인덱스와 `alarm_dispatch_maintenance.go`의 배치 DELETE가 파티션 관리로 대체된다.
전환은 무중단 재작성(신 테이블 병행 + 스왑)이 필요한 별도 플랜.

## 지금 하지 않는 것

- v1/v2 테이블·코드의 물리 제거(위 컷오버 완료 전까지). 제거 대상 전수 목록은 Phase 1/2의 정리 단계에 명시.
- v3로의 강제 통합을 위한 스키마 개조(페이로드 check 완화 등) — room-agnostic 불변식은 유지한다.
