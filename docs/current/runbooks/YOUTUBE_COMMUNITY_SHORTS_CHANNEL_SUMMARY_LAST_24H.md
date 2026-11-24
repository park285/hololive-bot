# YouTube Community Shorts Channel Summary Last 24h

최근 24시간 동안 실제 게시된 유튜브 커뮤니티/쇼츠 게시물을 채널별로 묶어 감지, 발송, 성공, 실패, 미발송 건수를 확인할 때 사용하는 운영 절차입니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- 최근 24시간 판단 기준은 `youtube_content_alarm_tracking.actual_published_at`이고, 실제 게시 시각이 비어 있으면 `detected_at`로 대체합니다.
- 집계 단위는 room 이벤트가 아니라 게시물(post)입니다. 같은 게시물에서 room별 재시도나 fan-out이 여러 번 있어도 채널 집계에서는 같은 post를 1건으로 계산합니다.
- `alarm_sent_post_count` 는 해당 post가 발송 파이프라인에 진입했거나(`outbox_count > 0`, 또는 outbox가 정리된 뒤 남은 원장 SENT·QUARANTINED room) tracking `alarm_sent_at`(최초 발송 완료 시각)이 남은 경우를 뜻합니다.
- `success_post_count` 는 SENT delivery 또는 원장 SENT room이 있는 post 수입니다. tracking `alarm_sent_at`은 최초 Iris 접수 시각이라 handoff 실패 뒤에도 남으므로 성공 근거로 쓰지 않습니다. handoff 확인을 기다리는 SENT도 포함하므로 확정 여부는 [Send Counts](YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md)의 `in_progress`로 봅니다.
- `failure_post_count` 는 `FAILED`·`QUARANTINED` delivery나 실패한 시도가 있었던(`attempt_count > 0`) delivery, delivery 없이 FAILED로 끝난 outbox(fan-out 실패), 또는 원장 QUARANTINED room이 하나라도 있는 post 수입니다. 원장 격리는 outbox가 정리된 뒤에도 남습니다. 이후 성공해도 실패 흔적은 채널 집계에 남습니다. 시도마다 남던 telemetry 행은 더 쓰지 않으므로, revive로 `attempt_count`가 0으로 돌아간 뒤 성공한 delivery의 이전 실패는 세지 못합니다.
- delivery 행은 outbox가 끝나고 `youtube_delivery.cleanup_after_ms`가 지나면 지워집니다.
- `detected_unsent_post_count` 는 감지됐지만 SENT delivery도 원장 SENT room도 없고 격리된 room도 없는 post 수입니다.
- `unknown_outcome_post_count` 는 성공 근거 없이 delivery 또는 원장이 QUARANTINED이거나, delivery가 SENDING(응답을 받지 못해 stale sweep 격리를 기다리는 경우 포함)이거나, outbox가 정리돼 tracking `alarm_sent_at`만 남은 post 수입니다([Send Counts](YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md)의 `tracking_only_outbox_cleaned`). 결과를 알 수 없어 미발송에서 뺍니다. 구독 room이 없어 delivery 없이 끝난 post(`no_target_post_count`)는 뺍니다.
- 게시물과 outbox는 outbox `content_id`를 worker와 같은 규칙으로 정규화한 `post_id`와 tracking `canonical_content_id`로 잇습니다. 공백이 다른 `content_id` 같은 별칭 outbox도 같은 post로 묶입니다.

## Execute

전용 리포트 CLI는 없으며 PostgreSQL을 직접 조회한다. 아래 SQL로 채널별 `detected`, `alarm_sent`, `success`, `failure`, `detected_unsent` 집계를 확인한다.

## SQL

DB 조회는 stack-platform-ops의 guarded read 경로(iris-stack
`.agents/skills/stack-platform-ops/references/postgres.md`)를 따릅니다. 중앙 `hololive-seoul`의
`holo-postgres`에 컨테이너 socket으로 접속하고, 조회 전에 read-only guard를 먼저 증명합니다.
비밀 env 파일을 셸에 source하거나 `PGPASSWORD`를 쓰지 않습니다.

```bash
# 1. read-only guard 증명: 결과가 정확히 `on`이 아니면 중단합니다.
ssh 100.100.1.8 \
  'sudo podman exec -e PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=5s" holo-postgres psql -U hololive_runtime -d hololive --no-psqlrc -v ON_ERROR_STOP=1 -At -c "show transaction_read_only"'

# 2. 같은 guard로 집계합니다. SQL을 표준 입력으로 넘기므로 podman exec에 -i를 씁니다.
ssh 100.100.1.8 \
  'sudo podman exec -i -e PGOPTIONS="-c default_transaction_read_only=on -c statement_timeout=5s" holo-postgres psql -U hololive_runtime -d hololive --no-psqlrc -v ON_ERROR_STOP=1' <<'SQL'
WITH outbox_post AS (
    SELECT
        o.*,
        CASE o.kind
            WHEN 'NEW_SHORT' THEN 'short:' || regexp_replace(regexp_replace(raw.id, '^short:', ''), space.re, '', 'g')
            ELSE 'community:' || regexp_replace(COALESCE(substring(community.id FROM '(?:^|/)post/([^"?#&/]+)'), community.id), space.re, '', 'g')
        END AS post_id
    FROM youtube_notification_outbox AS o
    CROSS JOIN (SELECT '^[ \t\n\v\f\r\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+|[ \t\n\v\f\r\u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+$' AS re) AS space
    CROSS JOIN LATERAL (SELECT regexp_replace(o.content_id, space.re, '', 'g') AS id) AS raw
    CROSS JOIN LATERAL (SELECT replace(regexp_replace(regexp_replace(raw.id, '^community:', ''), space.re, '', 'g'), '\/', '/') AS id) AS community
    WHERE o.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
),
per_post AS (
    SELECT
        track.channel_id AS channel_id,
        track.kind AS outbox_kind,
        track.content_id AS content_id,
        COALESCE(track.actual_published_at, track.detected_at) AS observed_at,
        track.alarm_sent_at AS alarm_sent_at,
        (SELECT COUNT(*) FROM youtube_notification_delivery_ledger AS l
          WHERE l.kind = track.kind AND l.logical_id = track.canonical_content_id AND l.status = 'SENT') AS ledger_sent_room_count,
        COUNT(DISTINCT o.id) AS outbox_count,
        COUNT(DISTINCT o.id) FILTER (WHERE o.status = 'SENT') AS sent_outbox_count,
        COUNT(DISTINCT o.id) FILTER (WHERE o.status = 'FAILED' AND d.id IS NULL) AS fanout_failed_outbox_count,
        COUNT(d.id) AS delivery_count,
        COUNT(d.id) FILTER (WHERE d.status = 'SENT') AS success_send_count,
        COUNT(d.id) FILTER (WHERE d.status = 'QUARANTINED') AS quarantined_delivery_count,
        COUNT(d.id) FILTER (WHERE d.status = 'SENDING') AS sending_delivery_count,
        (SELECT COUNT(*) FROM youtube_notification_delivery_ledger AS l
          WHERE l.kind = track.kind AND l.logical_id = track.canonical_content_id AND l.status = 'QUARANTINED') AS ledger_quarantined_room_count,
        COUNT(d.id) FILTER (WHERE d.status IN ('FAILED', 'QUARANTINED') OR d.attempt_count > 0) AS failed_attempt_count
    FROM youtube_content_alarm_tracking AS track
    LEFT JOIN outbox_post AS o
        ON o.kind = track.kind
       AND o.post_id = track.canonical_content_id
    LEFT JOIN youtube_notification_delivery AS d
        ON d.outbox_id = o.id
    WHERE track.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
      AND COALESCE(track.actual_published_at, track.detected_at) >= NOW() - INTERVAL '24 hours'
    GROUP BY
        track.channel_id,
        track.kind,
        track.content_id,
        track.actual_published_at,
        track.detected_at,
        track.alarm_sent_at,
        track.canonical_content_id
)
SELECT
    channel_id,
    COUNT(*) AS detected_post_count,
    COALESCE(SUM(CASE
        WHEN outbox_count > 0 OR success_send_count > 0 OR failed_attempt_count > 0 OR alarm_sent_at IS NOT NULL
             OR ledger_sent_room_count > 0 OR ledger_quarantined_room_count > 0 THEN 1
        ELSE 0
    END), 0) AS alarm_sent_post_count,
    COALESCE(SUM(CASE
        WHEN success_send_count > 0 OR ledger_sent_room_count > 0 THEN 1
        ELSE 0
    END), 0) AS success_post_count,
    COALESCE(SUM(CASE
        WHEN failed_attempt_count > 0 OR ledger_quarantined_room_count > 0 OR fanout_failed_outbox_count > 0 THEN 1
        ELSE 0
    END), 0) AS failure_post_count,
    COALESCE(SUM(CASE
        WHEN outbox_count > 0 AND sent_outbox_count = outbox_count AND delivery_count = 0
             AND ledger_sent_room_count = 0 AND ledger_quarantined_room_count = 0 THEN 1
        ELSE 0
    END), 0) AS no_target_post_count,
    COALESCE(SUM(CASE
        WHEN success_send_count = 0 AND ledger_sent_room_count = 0
             AND (quarantined_delivery_count > 0 OR ledger_quarantined_room_count > 0 OR sending_delivery_count > 0 OR (outbox_count = 0 AND alarm_sent_at IS NOT NULL)) THEN 1
        ELSE 0
    END), 0) AS unknown_outcome_post_count,
    COALESCE(SUM(CASE
        WHEN success_send_count = 0 AND ledger_sent_room_count = 0 AND quarantined_delivery_count = 0 AND ledger_quarantined_room_count = 0 AND sending_delivery_count = 0
             AND NOT (outbox_count = 0 AND alarm_sent_at IS NOT NULL)
             AND NOT (outbox_count > 0 AND sent_outbox_count = outbox_count AND delivery_count = 0) THEN 1
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

- `detected_post_count > alarm_sent_post_count`: outbox도 원장 room도 tracking `alarm_sent_at`도 없는 post가 있다는 뜻입니다. 발송 파이프라인에 진입하지 못했거나, fan-out 전에 `queue.max_age`가 지난 PENDING outbox가 정리됐거나, 실패로 끝난 outbox가 정리됐거나, 구독 room 없이 끝난 outbox가 tracking에 완료 시각이 옮겨지기 전에 정리된 post입니다. 넷 다 원장을 남기지 않아 DB로는 구분할 수 없으므로 [Send Counts](YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md)의 `outbox_missing` 기준대로 로그로 확인합니다.
- `success_post_count < detected_post_count`: 아직 성공하지 못한 post가 남아 있습니다. `detected_unsent_post_count` 와 같이 봅니다.
- `failure_post_count > 0`: 해당 채널에서 실패 또는 재시도 흔적이 남은 post가 있다는 뜻입니다. 성공 후 회복된 건도 포함될 수 있습니다. revive가 켜져 있으면 revive 창 안의 실패는 다시 발송될 수 있으므로 post별 최종 여부는 [Send Counts](YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md)의 운영 기준으로 판정합니다. 정리된 outbox의 실패는 원장 격리를 빼면 이 값에서 빠집니다.
- `unknown_outcome_post_count > 0`: 결과를 알 수 없는 post가 있습니다. 격리된 post는 재발송하지 않으므로 room에 도착했는지 직접 확인하고, SENDING이 남은 post는 stale sweep 뒤 다시 판정하고, tracking 시각만 남은 post는 [Send Counts](YOUTUBE_COMMUNITY_SHORTS_SEND_COUNTS_LAST_24H.md)의 `tracking_only_outbox_cleaned` 기준을 따릅니다.
- `no_target_post_count`: 구독 room이 없어 delivery 없이 outbox가 SENT로 끝났고 원장 SENT·QUARANTINED room도 없는 post 수입니다. 발송 대상이 없었으므로 `detected_unsent_post_count`에서 뺍니다. 같은 post의 다른 outbox가 실패한 뒤 정리됐으면 그 실패가 보이지 않아 여기에 들어가므로, `youtube_delivery.cleanup_after_ms`가 24시간보다 짧으면 이 post는 alarm-worker 실패 로그를 확인한 뒤 정상으로 셉니다.
- `detected_unsent_post_count > 0`: 감지 후 아직 성공한 room이 없는 post가 있습니다. 늦더라도 반드시 1회 발송돼야 하는 추적 대상입니다. 다만 `detected_post_count - alarm_sent_post_count`만큼은 근거가 모두 정리된 post라 구독 room 없는 정상 종료일 수도 있으므로 위 기준대로 확인한 뒤 누락으로 셉니다.
- `community_detected_post_count`, `shorts_detected_post_count`: 어떤 타입에서 채널 부하가 생겼는지 빠르게 나눠 볼 때 사용합니다.
