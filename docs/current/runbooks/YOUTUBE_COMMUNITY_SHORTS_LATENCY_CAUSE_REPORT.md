# YouTube Community Shorts Latency Cause Report

기간별 2분 초과 게시물을 외부 수집 구간과 내부 전달 구간으로 나눠 보는 운영 절차입니다. `youtube_content_alarm_tracking`에 저장된 시각만 씁니다.

## Scope

- 대상은 `COMMUNITY_POST`, `NEW_SHORT` 두 종류만 포함합니다.
- 기간 경계는 `[period_start_at, period_end_at)`이고, 판정 시각은 `actual_published_at`이며 비어 있으면 `detected_at`으로 대체합니다.
- 2분 초과 여부는 저장된 `alarm_latency_exceeded = true` 기준입니다.
- 외부 수집 구간은 `actual_published_at -> detected_at`, 내부 전달 구간은 `detected_at -> alarm_sent_at`입니다.
- 내부 세부 원인(큐 대기, 재시도 누적, job 실패)은 발송 시도 telemetry를 없애면서 더는 계산하지 않습니다. 필요하면 해당 게시물의 delivery 상태(`attempt_count`, `error`)와 attempt started·result 로그를 room과 시각으로 대조합니다([Delivery Logs](YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md)의 감사 기록 계약 참고).
- tracking의 `latency_classification_status`, `delay_source`, `internal_delay_cause` 열은 분류를 없애기 전 image가 쓴 값만 남은 동결 자료이므로 이 보고서는 읽지 않습니다.

## 판정 규칙

| `cause` | 조건 | 의미 |
| --- | --- | --- |
| `internal_over_target` | `internal_millis > 120000` | 감지 뒤 내부 전달만으로 2분을 넘겼습니다. 내부 원인입니다. |
| `internal_dominant` | 내부 구간이 외부 구간보다 김 | 외부 수집도 늦었지만 내부 전달이 더 길었습니다. 내부 원인 후보입니다. |
| `external_dominant` | 외부 구간이 내부 구간 이상 | YouTube 노출 또는 스크래핑 감지 지연이 우세합니다. 내부 실패로 판정하지 않습니다. |
| `insufficient_evidence` | 실제 게시 시각 또는 발송 시각이 없음 | 구간을 나눌 근거가 없습니다. |

## SQL

`periods` CTE의 행만 바꾸면 원하는 기간 조합으로 다시 집계할 수 있습니다.
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
WITH periods(label, period_start_at, period_end_at) AS (
    VALUES
        ('last_1h', NOW() - INTERVAL '1 hour', NOW()),
        ('last_24h', NOW() - INTERVAL '24 hours', NOW()),
        ('last_7d', NOW() - INTERVAL '7 days', NOW())
),
exceeded AS (
    SELECT
        track.kind,
        track.channel_id,
        track.content_id,
        COALESCE(track.actual_published_at, track.detected_at) AS observed_at,
        track.actual_published_at,
        track.detected_at,
        track.alarm_sent_at,
        track.alarm_latency_millis,
        ROUND(EXTRACT(EPOCH FROM (track.detected_at - track.actual_published_at)) * 1000)::BIGINT AS external_millis,
        ROUND(EXTRACT(EPOCH FROM (track.alarm_sent_at - track.detected_at)) * 1000)::BIGINT AS internal_millis
    FROM youtube_content_alarm_tracking AS track
    WHERE track.kind IN ('COMMUNITY_POST', 'NEW_SHORT')
      AND track.alarm_latency_exceeded = TRUE
),
judged AS (
    SELECT
        e.*,
        CASE
            WHEN e.external_millis IS NULL OR e.internal_millis IS NULL THEN 'insufficient_evidence'
            WHEN e.internal_millis > 120000 THEN 'internal_over_target'
            WHEN e.internal_millis > e.external_millis THEN 'internal_dominant'
            ELSE 'external_dominant'
        END AS cause
    FROM exceeded AS e
)
SELECT
    p.label AS period_label,
    COUNT(j.content_id) AS exceeded_post_count,
    COUNT(*) FILTER (WHERE j.cause = 'internal_over_target') AS internal_over_target_posts,
    COUNT(*) FILTER (WHERE j.cause = 'internal_dominant') AS internal_dominant_posts,
    COUNT(*) FILTER (WHERE j.cause = 'external_dominant') AS external_dominant_posts,
    COUNT(*) FILTER (WHERE j.cause = 'insufficient_evidence') AS insufficient_evidence_posts,
    MAX(j.internal_millis) AS max_internal_millis,
    MAX(j.external_millis) AS max_external_millis
FROM periods AS p
LEFT JOIN judged AS j
    ON j.observed_at >= p.period_start_at
   AND j.observed_at < p.period_end_at
GROUP BY p.label, p.period_start_at
ORDER BY p.period_start_at ASC;
SQL
```

게시물별 목록은 같은 CTE 뒤에 다음 SELECT를 붙여 조회합니다.

```sql
SELECT kind, channel_id, content_id, observed_at, actual_published_at, detected_at, alarm_sent_at,
       alarm_latency_millis, external_millis, internal_millis, cause
FROM judged
WHERE observed_at >= NOW() - INTERVAL '24 hours'
ORDER BY observed_at DESC;
```

## Readout

- `internal_over_target_posts > 0`: 감지 이후 내부 전달만으로 2분을 넘긴 게시물이 있습니다. 해당 게시물의 delivery 상태(`attempt_count`, `error`)로 재시도 흔적을 확인합니다. attempt started 줄의 `attempt_started_at`과 tracking의 `detected_at`의 차이는 첫 발송 시도까지 걸린 시간입니다. 발송 준비에서 실패한 claim은 이 줄을 남기지 않으므로 이 차이에는 그 뒤의 재시도 대기가 섞일 수 있고, 최초 대기 시간만 따로 복원할 수는 없습니다([Delivery Logs](YOUTUBE_COMMUNITY_SHORTS_DELIVERY_LOGS.md)).
- `internal_dominant_posts`: 외부와 내부가 함께 늦었고 내부가 더 길었던 건입니다. 내부 원인 후보로 봅니다.
- `external_dominant_posts`: 외부 수집 지연이 우세한 건입니다. 실패 집계에서 제외하는 참고 정보입니다.
- `insufficient_evidence_posts`: 실제 게시 시각이나 발송 시각이 없어 구간을 나누지 못한 건입니다.
