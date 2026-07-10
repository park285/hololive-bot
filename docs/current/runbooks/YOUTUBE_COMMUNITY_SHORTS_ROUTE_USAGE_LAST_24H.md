# YouTube Community Shorts Route Usage Last 24h

최근 24시간 동안 실제 게시된 유튜브 커뮤니티/쇼츠 게시물이 채널별로 어떤 발송 경로를 사용했는지 확인하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- 최근 24시간 판단 기준은 `youtube_content_alarm_tracking.actual_published_at`이고, 실제 게시 시각이 비어 있으면 `detected_at`로 대체합니다.
- 실제 발송 경로 근거는 `youtube_notification_delivery_telemetry.delivery_path` 입니다.
- 신규 경로 기준값은 `youtube_outbox_dispatcher` 입니다.

## Execute

호스트에서 `psql`을 사용할 수 있으면 아래 절차를 그대로 실행합니다. compose 운영 기준 Postgres는 `localhost:5433`, 기본 읽기 계정은 `HOLOLIVE_DB_USER`입니다.

```bash
set -a
source "${HOLOLIVE_BOT_ENV_FILE:-/run/hololive-bot/env}"
set +a

PGPASSWORD="$DB_PASSWORD" \
psql -h localhost -p 5433 -U "${HOLOLIVE_DB_USER:-hololive_runtime}" -d hololive <<'SQL'
SELECT
    track.kind AS outbox_kind,
    CASE track.kind
        WHEN 'COMMUNITY_POST' THEN 'COMMUNITY'
        WHEN 'NEW_SHORT' THEN 'SHORTS'
        ELSE 'LIVE'
    END AS alarm_type,
    track.channel_id AS channel_id,
    COALESCE(MAX(NULLIF(t.post_id, '')), track.content_id) AS post_id,
    COALESCE(t.delivery_path, '') AS delivery_path,
    track.actual_published_at AS actual_published_at,
    track.detected_at AS detected_at,
    MIN(t.event_at) AS first_event_at,
    MAX(t.event_at) AS last_event_at,
    MIN(CASE WHEN t.send_result = 'success' THEN t.event_at END) AS first_success_at,
    MAX(CASE WHEN t.send_result = 'success' THEN t.event_at END) AS last_success_at,
    COALESCE(SUM(CASE WHEN t.send_result = 'success' THEN 1 ELSE 0 END), 0) AS success_send_count,
    COUNT(DISTINCT CASE WHEN t.send_result = 'success' THEN t.room_id END) AS success_room_count,
    COALESCE(SUM(CASE WHEN t.send_result <> 'success' THEN 1 ELSE 0 END), 0) AS failed_attempt_count
FROM youtube_content_alarm_tracking AS track
LEFT JOIN youtube_notification_outbox AS o
    ON o.kind = track.kind
   AND o.content_id = track.content_id
LEFT JOIN youtube_notification_delivery_telemetry AS t
    ON t.outbox_id = o.id
   AND t.event_at >= NOW() - INTERVAL '24 hours'
WHERE track.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
  AND COALESCE(track.actual_published_at, track.detected_at) >= NOW() - INTERVAL '24 hours'
GROUP BY
    track.kind,
    track.channel_id,
    track.content_id,
    COALESCE(t.delivery_path, ''),
    track.actual_published_at,
    track.detected_at
ORDER BY track.channel_id ASC, COALESCE(MAX(NULLIF(t.post_id, '')), track.content_id) ASC, COALESCE(t.delivery_path, '') ASC;
SQL
```

legacy 경로 차단 흔적은 운영 로그에서 별도로 확인합니다. compose 로그 파일 미러링 기준 예시는 다음과 같습니다.

```bash
rg 'YouTube community/shorts legacy route audit' logs/hololive-api.log
```

## Readout

- `delivery_path = 'youtube_outbox_dispatcher'` and `success_send_count > 0`: 실제 발송이 신규 경로에서 관측됐습니다.
- 동일 `channel_id + post_id` 조합에서 `delivery_path` 가 2개 이상 나오면 게시물이 복수 경로 흔적을 남긴 것입니다. 정상 상태가 아닙니다.
- `delivery_path` 가 빈 문자열이고 `success_send_count = 0`: 최근 24시간 창에서 실제 발송 경로가 아직 관측되지 않았습니다. 미발송 또는 미시도 후보입니다.
- `delivery_path` 가 `youtube_outbox_dispatcher` 외 다른 값이면 예상 밖 경로 흔적입니다. legacy 또는 비정상 fan-out 의심 건으로 봅니다.
- `logs/hololive-api.log` 에서 `YouTube community/shorts legacy route audit` 가 0건이면 legacy 알람 큐 진입 시도가 관측되지 않은 것입니다. 한 건이라도 나오면 `delivery_path=legacy_alarm_queue` 차단 흔적입니다.
