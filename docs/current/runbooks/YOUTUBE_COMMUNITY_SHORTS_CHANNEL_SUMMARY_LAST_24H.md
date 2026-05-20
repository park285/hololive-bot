# YouTube Community Shorts Channel Summary Last 24h

최근 24시간 동안 실제 게시된 유튜브 커뮤니티/쇼츠 게시물을 채널별로 묶어 감지, 발송, 성공, 실패, 미발송 건수를 확인할 때 사용하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- 최근 24시간 판단 기준은 `youtube_content_alarm_tracking.actual_published_at`이고, 실제 게시 시각이 비어 있으면 `detected_at`로 대체합니다.
- 집계 단위는 room 이벤트가 아니라 게시물(post)입니다. 같은 게시물에서 room별 재시도나 fan-out이 여러 번 있어도 채널 집계에서는 같은 post를 1건으로 계산합니다.
- `alarm_sent_post_count` 는 해당 post가 발송 파이프라인에 진입했거나(`outbox_count > 0`) 실제 발송 이벤트가 남은 경우를 뜻합니다.
- `success_post_count` 는 `alarm_sent_at` 또는 성공 telemetry가 존재하는 post 수입니다.
- `failure_post_count` 는 실패 telemetry가 1회 이상 기록된 post 수입니다. 이후 성공해도 실패 흔적은 채널 집계에 남습니다.
- `detected_unsent_post_count` 는 감지됐지만 canonical 성공 시각(`alarm_sent_at`)이 아직 없는 post 수입니다.

## Execute

우선 경로는 저장소의 집계 로직을 그대로 사용하는 전용 명령입니다. repo root에서 실행합니다.

```bash
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts channel-summary -window 24h
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts channel-summary -window 24h -format json
```

- 기본 출력은 Markdown 표이며, 채널별 `detected`, `alarm_sent`, `success`, `failure`, `detected_unsent` 집계를 한 줄로 보여 줍니다.
- `-format json` 은 자동 수집이나 후처리에 사용할 수 있습니다.

## Fallback SQL

호스트에서 `psql` 을 사용할 수 있으면 아래 절차로 같은 집계를 직접 확인할 수 있습니다. compose 운영 기준 Postgres는 `localhost:5433`, 기본 읽기 계정은 `HOLOLIVE_DB_USER`입니다.

```bash
set -a
source "${HOLOLIVE_BOT_ENV_FILE:-/run/hololive-bot/env}"
set +a

PGPASSWORD="$DB_PASSWORD" psql -h localhost -p 5433 -U "${HOLOLIVE_DB_USER:-hololive_runtime}" -d hololive <<'SQL'
WITH per_post AS (
    SELECT
        track.channel_id AS channel_id,
        track.kind AS outbox_kind,
        track.content_id AS content_id,
        COALESCE(track.actual_published_at, track.detected_at) AS observed_at,
        track.alarm_sent_at AS alarm_sent_at,
        COUNT(DISTINCT o.id) AS outbox_count,
        COALESCE(SUM(CASE WHEN t.send_result = 'success' THEN 1 ELSE 0 END), 0) AS success_send_count,
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
        track.channel_id,
        track.kind,
        track.content_id,
        track.actual_published_at,
        track.detected_at,
        track.alarm_sent_at
)
SELECT
    channel_id,
    COUNT(*) AS detected_post_count,
    COALESCE(SUM(CASE
        WHEN outbox_count > 0 OR success_send_count > 0 OR failed_attempt_count > 0 OR alarm_sent_at IS NOT NULL THEN 1
        ELSE 0
    END), 0) AS alarm_sent_post_count,
    COALESCE(SUM(CASE
        WHEN success_send_count > 0 OR alarm_sent_at IS NOT NULL THEN 1
        ELSE 0
    END), 0) AS success_post_count,
    COALESCE(SUM(CASE
        WHEN failed_attempt_count > 0 THEN 1
        ELSE 0
    END), 0) AS failure_post_count,
    COALESCE(SUM(CASE
        WHEN alarm_sent_at IS NULL THEN 1
        ELSE 0
    END), 0) AS detected_unsent_post_count,
    COALESCE(SUM(CASE WHEN outbox_kind = 'COMMUNITY_POST' THEN 1 ELSE 0 END), 0) AS community_detected_post_count,
    COALESCE(SUM(CASE WHEN outbox_kind = 'NEW_SHORT' THEN 1 ELSE 0 END), 0) AS shorts_detected_post_count,
    MIN(observed_at) AS earliest_observed_at,
    MAX(observed_at) AS latest_observed_at
FROM per_post
GROUP BY channel_id
ORDER BY latest_observed_at DESC, channel_id ASC;
SQL
```

## Readout

- `detected_post_count > alarm_sent_post_count`: 감지된 post 중 발송 파이프라인에 아직 진입하지 못한 건이 있다는 뜻입니다.
- `success_post_count < detected_post_count`: 아직 성공하지 못한 post가 남아 있습니다. `detected_unsent_post_count` 와 같이 봅니다.
- `failure_post_count > 0`: 해당 채널에서 실패 또는 재시도 흔적이 남은 post가 있다는 뜻입니다. 성공 후 회복된 건도 포함될 수 있습니다.
- `detected_unsent_post_count > 0`: 감지 후 아직 canonical 성공 시각이 기록되지 않은 post가 있습니다. 늦더라도 반드시 1회 발송돼야 하는 추적 대상입니다.
- `community_detected_post_count`, `shorts_detected_post_count`: 어떤 타입에서 채널 부하가 생겼는지 빠르게 나눠 볼 때 사용합니다.
