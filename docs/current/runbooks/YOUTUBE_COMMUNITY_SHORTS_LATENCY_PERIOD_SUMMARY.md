# YouTube Community Shorts Latency Period Summary

지정한 기간 목록에 대해 실제 게시 시각 기준의 유튜브 커뮤니티/쇼츠 지연 집계를 확인할 때 사용하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- 기간 경계는 `[period_start_at, period_end_at)` 입니다.
- 기간 판정 시작점은 `youtube_content_alarm_tracking.actual_published_at` 이고, 실제 게시 시각이 비어 있으면 `detected_at` 로 대체합니다.
- 지연 값은 `alarm_sent_at - actual_published_at` 기준이며, 실제 게시 시각이 없는 경우에는 저장된 `alarm_latency_millis` 가 비어 있을 수 있습니다.
- `alarm_latency_exceeded = true` 는 실제 게시 시각 기준 2분 초과 건을 뜻합니다.
- `alarm_sent_at IS NULL` 인 게시물도 집계에 남아야 하므로 기준 집합은 `youtube_content_alarm_tracking` 입니다.

## Execute

우선 경로는 저장소의 집계 로직을 그대로 사용하는 전용 명령입니다. repo root에서 실행합니다.

```bash
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts latency-period-summary
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts latency-period-summary -format json
go run ./hololive/hololive-youtube-producer/cmd/ops/youtube-community-shorts latency-period-summary \
  -period last_15m=15m \
  -period last_2h=2h \
  -period last_24h=24h
```

- `-period` 를 주지 않으면 기본 기간은 `last_1h=1h`, `last_24h=24h`, `last_7d=168h` 입니다.
- 각 `-period` 는 `label=duration` 형식이며 `duration` 은 Go `time.ParseDuration` 규약을 따릅니다.
- 기본 출력은 Markdown 표이며, 기간별 `total_posts`, `alarm_sent_posts`, `pending_posts`, `measured_posts`, `avg_latency_ms`, `p95_latency_ms`, `max_latency_ms`, `over_2m_posts` 를 바로 읽을 수 있습니다.
- `-format json` 은 자동 수집이나 후처리에 사용할 수 있습니다. 각 period row는 저장된 지연 결과를 재사용하므로 수동 SQL과 동일한 기준 집합을 따릅니다.

## Fallback SQL

호스트에서 `psql` 을 사용할 수 있으면 아래 절차로 같은 집계를 직접 확인할 수 있습니다. `periods` CTE 의 행만 바꾸면 원하는 기간 조합으로 집계를 다시 만들 수 있습니다.

```bash
set -a
source "${HOLOLIVE_BOT_ENV_FILE:-/run/hololive-bot/env}"
set +a

PGPASSWORD="$DB_PASSWORD" \
psql -h localhost -p 5433 -U "${HOLOLIVE_DB_USER:-hololive_runtime}" -d hololive <<'SQL'
WITH periods(label, period_start_at, period_end_at) AS (
    VALUES
        ('last_1h', NOW() - INTERVAL '1 hour', NOW()),
        ('last_24h', NOW() - INTERVAL '24 hours', NOW()),
        ('last_7d', NOW() - INTERVAL '7 days', NOW())
),
base AS (
    SELECT
        CASE track.kind
            WHEN 'COMMUNITY_POST' THEN 'COMMUNITY'
            WHEN 'NEW_SHORT' THEN 'SHORTS'
            ELSE 'LIVE'
        END AS alarm_type,
        COALESCE(track.actual_published_at, track.detected_at) AS observed_at,
        track.alarm_sent_at AS alarm_sent_at,
        track.alarm_latency_millis AS alarm_latency_millis,
        track.alarm_latency_exceeded AS alarm_latency_exceeded
    FROM youtube_content_alarm_tracking AS track
    WHERE track.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
)
SELECT
    p.label AS period_label,
    p.period_start_at,
    p.period_end_at,
    COUNT(b.observed_at) AS total_post_count,
    COUNT(*) FILTER (WHERE b.alarm_sent_at IS NOT NULL) AS alarm_sent_post_count,
    COUNT(*) FILTER (WHERE b.alarm_sent_at IS NULL AND b.observed_at IS NOT NULL) AS pending_post_count,
    COUNT(*) FILTER (WHERE b.alarm_latency_exceeded IS NOT NULL) AS latency_measured_post_count,
    COUNT(*) FILTER (WHERE b.alarm_latency_exceeded = FALSE) AS within_target_post_count,
    COUNT(*) FILTER (WHERE b.alarm_latency_exceeded = TRUE) AS exceeded_post_count,
    COUNT(*) FILTER (WHERE b.alarm_type = 'COMMUNITY') AS community_post_count,
    COUNT(*) FILTER (WHERE b.alarm_type = 'COMMUNITY' AND b.alarm_latency_exceeded = TRUE) AS community_exceeded_post_count,
    COUNT(*) FILTER (WHERE b.alarm_type = 'SHORTS') AS shorts_post_count,
    COUNT(*) FILTER (WHERE b.alarm_type = 'SHORTS' AND b.alarm_latency_exceeded = TRUE) AS shorts_exceeded_post_count,
    ROUND(AVG(b.alarm_latency_millis))::BIGINT AS average_latency_millis,
    PERCENTILE_DISC(0.95) WITHIN GROUP (ORDER BY b.alarm_latency_millis) AS p95_latency_millis,
    MAX(b.alarm_latency_millis) AS max_latency_millis
FROM periods AS p
LEFT JOIN base AS b
    ON b.observed_at >= p.period_start_at
   AND b.observed_at < p.period_end_at
GROUP BY p.label, p.period_start_at, p.period_end_at
ORDER BY p.period_start_at ASC;
SQL
```

## Readout

- `total_post_count`: 기간 내 실제 게시 또는 감지된 community/shorts 게시물 수입니다.
- `alarm_sent_post_count`: 최초 성공 발송 시각(`alarm_sent_at`)이 기록된 게시물 수입니다.
- `pending_post_count`: 기간 안에 게시물은 있었지만 아직 발송 완료 시각이 기록되지 않은 건수입니다. 늦더라도 1회 발송돼야 하는 미완료 후보입니다.
- `latency_measured_post_count`: 실제 게시 시각과 발송 시각이 모두 있어 지연 계산이 확정된 게시물 수입니다.
- `average_latency_millis`, `p95_latency_millis`, `max_latency_millis`: 측정 가능한 게시물만 기준으로 계산된 평균, p95, 최대 지연입니다.
- `exceeded_post_count > 0`: 2분 초과 지연이 해당 기간에 존재합니다. 운영 알림 대상은 아니지만 원인 분석 후보입니다.
- `community_exceeded_post_count`, `shorts_exceeded_post_count`: 어떤 알람 타입에서 2분 초과가 집중됐는지 빠르게 구분하는 값입니다.
