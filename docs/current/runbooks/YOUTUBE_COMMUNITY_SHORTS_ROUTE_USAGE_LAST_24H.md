# YouTube Community Shorts Route Usage Last 24h

최근 24시간 동안 실제 게시된 유튜브 커뮤니티/쇼츠 게시물이 alarm-worker 발송 경로를 거쳤는지 채널별로 확인하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- 최근 24시간 판단 기준은 `youtube_content_alarm_tracking.actual_published_at`이고, 실제 게시 시각이 비어 있으면 `detected_at`로 대체합니다.
- 발송 경로 값은 행에 저장하지 않습니다. 경로 근거는 두 가지입니다.
  - alarm-worker 감사 로그의 `delivery_path`입니다. 값은 `youtube_outbox_dispatcher` 하나이며 [Delivery Logs](YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md)의 최종 결과 줄에서 읽습니다.
  - `youtube_notification_delivery` 행입니다. 이 표의 INSERT 권한은 `hololive_alarm_worker`에만 있고 alarm-worker는 `youtube_outbox_dispatcher` 경로로만 delivery를 만듭니다. 따라서 SENT delivery가 있으면 이 경로로 발송된 것입니다.

## Execute
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
SELECT
    track.kind AS outbox_kind,
    CASE track.kind WHEN 'COMMUNITY_POST' THEN 'COMMUNITY' ELSE 'SHORTS' END AS alarm_type,
    track.channel_id,
    track.content_id,
    track.actual_published_at,
    track.detected_at,
    COUNT(DISTINCT o.id) AS outbox_count,
    COUNT(d.id) AS delivery_count,
    MIN(d.sent_at) FILTER (WHERE d.status = 'SENT') AS first_success_at,
    MAX(d.sent_at) FILTER (WHERE d.status = 'SENT') AS last_success_at,
    COUNT(d.id) FILTER (WHERE d.status = 'SENT') AS success_send_count,
    COUNT(DISTINCT d.room_id) FILTER (WHERE d.status = 'SENT') AS success_room_count
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
    track.detected_at
ORDER BY track.channel_id ASC, track.content_id ASC;
SQL
```

## Readout

- `success_send_count > 0`: alarm-worker `youtube_outbox_dispatcher` 경로로 발송이 성공했습니다.
- `outbox_count > 0`이고 `delivery_count = 0`: outbox는 있지만 room별 delivery가 아직 만들어지지 않았습니다. 미발송 후보입니다.
- `outbox_count = 0`: alarm-worker 경로에 진입하지 않았습니다. 끝난 outbox가 `youtube_delivery.cleanup_after_ms`를 넘겨 지워진 경우도 여기에 해당하므로 tracking의 `alarm_sent_at`과 함께 봅니다.
- 감사 로그의 `delivery_path`가 `youtube_outbox_dispatcher` 외 값이면 예상 밖 경로이므로 별도 조사합니다.
