# YouTube Community Shorts Operations Dashboard

관리자 대시보드의 `유튜브 운영` 탭(`/dashboard/youtube-ops`)에서 최근 24시간 커뮤니티/쇼츠 알람 상태를 채널별로 확인할 때 사용하는 기준 문서입니다.

## Scope

- 대상 알람은 `COMMUNITY_POST`, `NEW_SHORT`만 포함합니다.
- 조회 창은 고정 24시간이며 범위는 `[windowStart, windowEnd)` 입니다.
- 게시물 시각 기준은 `COALESCE(actual_published_at, detected_at)` 입니다.
  - `actual_published_at`가 있으면 실제 유튜브 게시 시각을 사용합니다.
  - 비어 있으면 `detected_at`을 fallback으로 사용합니다.
- 표와 카드의 시간 표시는 운영 가독성을 위해 KST로 변환하지만 저장/판정 기준은 UTC canonical data입니다.
- 전체 합계와 채널별 행 모두 커뮤니티/쇼츠 전용 텔레메트리만 사용하며 다른 알람 유형과 UI는 건드리지 않습니다.

## Dashboard Query

- bot API: `GET /api/holo/stats/youtube/community-shorts`
- admin-dashboard proxy: `GET /admin/api/holo/stats/youtube/community-shorts`
- 화면 갱신 주기: 60초
- 화면 데이터는 `youtube_content_alarm_tracking` 기반 24시간 post 집계를 한 번 조회한 뒤 서버에서 채널별/전체 요약으로 재구성합니다.

## Metric Meanings

| Metric | Meaning | Query Basis |
| --- | --- | --- |
| `channelCount` | 최근 24시간 안에 community/shorts 게시물이 1건 이상 관측된 채널 수 | 채널별 집계 결과의 행 수 |
| `detectedPostCount` | 최근 24시간 창에 들어온 게시물 수 | `COALESCE(actual_published_at, detected_at)` 가 창 안에 들어온 post |
| `alarmSentPostCount` | outbox 또는 delivery telemetry 활동이 한 번 이상 있었던 게시물 수 | `outbox_count > 0` 또는 event/success/failure 흔적 존재 |
| `successPostCount` | canonical success가 1회 이상 기록된 게시물 수 | `alarm_sent_at`, success telemetry, success 시각 중 하나 존재 |
| `failedPostCount` | 실패 시도가 1회 이상 있었던 게시물 수 | `failed_attempt_count > 0` |
| `detectedUnsentPostCount` | 감지는 됐지만 success가 아직 없는 게시물 수 | success count와 success 시각이 모두 비어 있는 post |
| `pendingPostCount` | `alarm_sent_at` 자체가 없는 게시물 수 | 내부 전달 완료 이전 단계 적체 확인용 |
| `latencyMeasuredPostCount` | 지연 수치 또는 초과 여부가 계산 가능한 게시물 수 | `alarm_latency_millis` 또는 `alarm_latency_exceeded` 존재 |
| `withinTargetPostCount` | 2분 이내로 판정된 게시물 수 | `alarm_latency_exceeded = false` |
| `exceededPostCount` | 실제 게시 시각 기준 2분 초과가 확정된 게시물 수 | `alarm_latency_exceeded = true` |
| `averageLatencyMillis` | 지연 계산이 가능한 게시물만 기준으로 한 평균 지연 | `latencyMeasuredPostCount` 집합 평균 |
| `maxLatencyMillis` | 지연 계산이 가능한 게시물만 기준으로 한 최대 지연 | `latencyMeasuredPostCount` 집합 최대 |
| `communityDetectedPostCount` / `shortsDetectedPostCount` | 감지 게시물을 알람 타입별로 나눈 값 | `alarm_type = COMMUNITY` / `SHORTS` |
| `communityExceededPostCount` / `shortsExceededPostCount` | 2분 초과 게시물을 알람 타입별로 나눈 값 | `alarm_type` + `alarm_latency_exceeded = true` |

## Channel Row Readout

- `채널`: 멤버명이 있으면 멤버명, 없으면 채널 ID를 표시합니다.
- `최신 관측`: 해당 채널에서 최근 24시간 내 가장 늦게 관측된 게시물 시각입니다.
- `감지`: 해당 채널에서 창 안에 들어온 게시물 수입니다.
- `성공`: 1회 발송 보장이 성립한 게시물 수입니다.
- `미발송`: 감지됐지만 아직 success가 없는 게시물 수입니다.
- `pending`: `alarm_sent_at`가 비어 있는 게시물 수입니다. 내부 파이프라인이 아직 닫히지 않은 경우가 많습니다.
- `2분 초과`: 실제 게시 시각 기준 2분 초과가 확정된 게시물 수입니다.
- `평균 / 최대`: 측정 가능한 게시물만 기준으로 한 채널별 평균/최대 지연입니다.

## Status Highlight Rules

- `미발송 + SLA 초과`: 같은 채널에서 미발송 후보와 2분 초과가 모두 존재합니다.
- `미발송 추적 필요`: success가 아직 없는 게시물이 있습니다.
- `SLA 초과 있음`: 성공은 됐지만 2분 초과가 있습니다.
- `실패 시도 존재`: 최종 성공과 별개로 실패 시도가 같이 기록됐습니다.
- `정상`: 위 조건이 모두 없습니다.

## Interpretation Order

1. `detectedUnsentPostCount`와 `pendingPostCount`로 누락/지연 진행형 후보를 먼저 본다.
2. `successPostCount`가 `detectedPostCount`와 같은지 확인해 1회 발송 보장 상태를 본다.
3. `exceededPostCount`와 채널별 `평균 / 최대`를 보고 2분 초과 원인 분석 대상을 좁힌다.
4. `community`/`shorts` 분해값으로 어떤 알람 타입에 문제가 몰렸는지 본다.

## Notes

- 내부 원인으로 2분을 초과해도 화면에는 계속 남아야 하며, 운영 알림이나 자동 중지는 하지 않습니다.
- 외부 수집 지연과 내부 전달 지연의 세부 원인 해석은 `YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md`와 `YOUTUBE_COMMUNITY_SHORTS_LATENCY_PERIOD_SUMMARY.md`를 함께 봅니다.
