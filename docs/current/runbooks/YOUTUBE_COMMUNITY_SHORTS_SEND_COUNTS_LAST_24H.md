# YouTube Community Shorts Send Counts

최근 24시간 동안 실제 게시된 유튜브 커뮤니티/쇼츠 게시물별 알람 발송 횟수와 중복 여부를 운영자가 확인할 때 사용하는 조회 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- recent window 조회는 `youtube_content_alarm_tracking.actual_published_at`를 우선 사용하고, 값이 비어 있으면 `detected_at`으로 대체해 최근 24시간 게시물을 고릅니다.
- `0회 발송` 게시물도 결과에 남아야 하므로 추적 테이블(`youtube_content_alarm_tracking`)을 기준 집합으로 사용합니다.
- 발송 횟수는 room별 delivery(`youtube_notification_delivery`)의 현재 상태로 셉니다. 시도마다 남던 telemetry 행은 더 쓰지 않으므로, 실패 흔적은 `FAILED`·`QUARANTINED` 상태와 `attempt_count > 0`으로만 남습니다. `attempt_count`는 실패한 시도에서만 올라가고 성공은 올리지 않으며, revive가 0으로 되돌립니다.
- DB에는 같은 게시물·room의 SENT가 둘 생길 수 없습니다. delivery는 `(outbox_id, room_id)`, outbox는 `(kind, content_id)`가 유일하고, 원장 `youtube_notification_delivery_ledger`는 `(kind, logical_id, room_id)`가 기본 키입니다. 중복 발송 위험은 provider가 받았는지 모르는 시도에 남으며, 이런 delivery는 `QUARANTINED`로 격리되고 재발송하지 않습니다.
- delivery 행은 outbox가 끝나고 `youtube_delivery.cleanup_after_ms`가 지나면 지워집니다. 이 값이 24시간보다 짧으면 오래된 게시물이 `outbox_missing`으로 보일 수 있습니다.

## Execute

전용 리포트 CLI는 없으며 PostgreSQL을 직접 조회합니다.
DB 조회는 stack-platform-ops의 guarded read 경로(iris-stack
`.agents/skills/stack-platform-ops/references/postgres.md`)를 따릅니다. 중앙 `hololive-seoul`의
`holo-postgres`에 컨테이너 socket으로 접속하고, 조회 전에 read-only guard를 먼저 증명합니다.
비밀 env 파일을 셸에 source하거나 `PGPASSWORD`를 쓰지 않습니다.

```bash
# 1. read-only guard 증명: 결과가 정확히 `on`이 아니면 중단합니다.
ssh 100.100.1.8 \
  'sudo podman exec -e PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=5s" holo-postgres psql -U hololive_runtime -d hololive --no-psqlrc -v ON_ERROR_STOP=1 -At -c "show transaction_read_only"'

# 2. 같은 guard로 조회합니다. SQL을 표준 입력으로 넘기므로 podman exec에 -i를 씁니다.
ssh 100.100.1.8 \
  'sudo podman exec -i -e PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=5s" holo-postgres psql -U hololive_runtime -d hololive --no-psqlrc -v ON_ERROR_STOP=1' <<'SQL'
WITH per_post AS (
    SELECT
        track.kind,
        track.channel_id,
        track.content_id,
        track.actual_published_at,
        track.detected_at,
        track.alarm_sent_at,
        track.alarm_latency_millis,
        track.alarm_latency_exceeded,
        COUNT(DISTINCT o.id) AS outbox_count,
        COUNT(d.id) FILTER (WHERE d.status = 'SENT') AS success_send_count,
        COUNT(DISTINCT d.room_id) FILTER (WHERE d.status = 'SENT') AS success_room_count,
        COUNT(DISTINCT d.room_id) AS target_room_count,
        COUNT(d.id) FILTER (WHERE d.status = 'FAILED') AS failed_delivery_count,
        COUNT(d.id) FILTER (WHERE d.status = 'QUARANTINED') AS quarantined_delivery_count,
        COUNT(d.id) FILTER (WHERE d.attempt_count > 0) AS failed_attempt_delivery_count,
        MAX(d.sent_at) AS last_sent_at
    FROM youtube_content_alarm_tracking AS track
    LEFT JOIN youtube_notification_outbox AS o
        ON o.kind = track.kind
       AND o.content_id = track.content_id
    LEFT JOIN youtube_notification_delivery AS d
        ON d.outbox_id = o.id
    WHERE track.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
      AND COALESCE(track.actual_published_at, track.detected_at) >= NOW() - INTERVAL '24 hours'
    GROUP BY
        track.kind,
        track.channel_id,
        track.content_id,
        track.actual_published_at,
        track.detected_at,
        track.alarm_sent_at,
        track.alarm_latency_millis,
        track.alarm_latency_exceeded
)
SELECT
    CASE
        WHEN outbox_count = 0 THEN 'outbox_missing'
        WHEN quarantined_delivery_count > 0 THEN 'quarantined'
        WHEN success_send_count = 0 THEN 'no_success'
        WHEN failed_delivery_count > 0 OR failed_attempt_delivery_count > 0 THEN 'failed_attempts'
        ELSE 'ok'
    END AS status,
    CASE kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' ELSE 'SHORTS' END AS alarm_type,
    channel_id,
    content_id,
    actual_published_at,
    detected_at,
    alarm_sent_at,
    ROUND(alarm_latency_millis / 1000.0) AS delay_seconds,
    alarm_latency_exceeded,
    ROUND(EXTRACT(EPOCH FROM (detected_at - actual_published_at)) * 1000)::BIGINT AS publish_to_detect_ms,
    ROUND(EXTRACT(EPOCH FROM (alarm_sent_at - detected_at)) * 1000)::BIGINT AS detect_to_sent_ms,
    outbox_count,
    target_room_count,
    success_send_count,
    success_room_count,
    quarantined_delivery_count,
    failed_delivery_count,
    failed_attempt_delivery_count,
    last_sent_at
FROM per_post
ORDER BY COALESCE(actual_published_at, detected_at) DESC, channel_id ASC, content_id ASC;
SQL
```

운영 기준은 `quarantined` 행 0건입니다. 같은 CTE 뒤에 `SELECT COUNT(*) FILTER (WHERE quarantined_delivery_count > 0) AS quarantined_posts FROM per_post;`를 붙이면 한 줄로 판정할 수 있습니다.

## Readout

- `status = ok`: 성공 발송이 room별 1회씩만 기록된 정상 상태입니다.
- `quarantined_posts = 0`: 집계 창에 결과 불명으로 격리된 delivery가 없으므로 중복 발송 후보도 없습니다.
- `status = quarantined`: provider가 받았는지 모르는 시도가 격리됐습니다. 재발송하지 않으므로 room에 도착했는지 전송 증거로 검토합니다. 받았다면 1회, 받지 못했다면 누락입니다.
- `status = no_success`: SENT delivery가 없습니다. `alarm_sent_at`도 비어 있으면 아직 성공하지 못한 게시물입니다.
- `status = outbox_missing`: 게시물 추적 레코드는 있으나 outbox row가 없습니다. 전송 파이프라인 진입 전 누락 후보이거나, 끝난 outbox가 보존 기간을 넘겨 지워진 경우입니다.
- `status = failed_attempts`: 성공은 있었지만 `FAILED` delivery나 실패한 시도가 있었던 delivery도 있습니다. 시도별 사유는 [Delivery Logs](YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md)의 로그로 확인합니다.
- `publish_to_detect_ms`, `detect_to_sent_ms`: 외부 수집 구간과 내부 전달 구간입니다. `delay_seconds > 120`이면 2분 초과이며, 구간 판정은 [Latency Cause Report](YOUTUBE_COMMUNITY_SHORTS_LATENCY_CAUSE_REPORT.md)를 따릅니다.
